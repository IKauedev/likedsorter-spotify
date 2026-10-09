package enrich

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/ikauedeveloper/likedsorter/internal/cache"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// ArtistFetcher é o que o enriquecimento precisa do cliente Spotify.
type ArtistFetcher interface {
	Artist(ctx context.Context, id string) (*spotify.ArtistDetail, error)
}

// Info são os metadados guardados por artista (e persistidos no cache).
type Info struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Genres   []string `json:"genres,omitempty"` // minúsculas, até 3 (fontes externas) ou os do Spotify
	ImageURL string   `json:"image,omitempty"`
	Source   string   `json:"source"`          // de onde vieram os gêneros (SourceNone se de nenhuma)
	Tried    []string `json:"tried,omitempty"` // fontes consultadas com sucesso
}

// Stats resume uma execução de Enrich.
type Stats struct {
	Artists  int            // artistas únicos processados
	Cached   int            // resolvidos pelo cache
	BySource map[string]int // artistas novos por fonte de gênero (inclui SourceNone)
	Failures int            // falhas não fatais de provedores externos
	Disabled []string       // provedores desligados por falhas consecutivas
}

// Config monta um Enricher.
type Config struct {
	Sources     []string                 // prioridade, ex.: [spotify musicbrainz]
	Providers   map[string]GenreProvider // musicbrainz / lastfm
	Concurrency int                      // padrão 4
	SaveEvery   int                      // persiste o cache a cada N artistas novos (padrão 50)
	BreakAfter  int                      // desliga um provedor após N falhas seguidas (padrão 5)
	Logger      *slog.Logger
}

// Enricher completa os dados dos artistas.
type Enricher struct {
	cfg   Config
	cache *cache.TTLMap[Info]
}

// New valida a configuração. Toda fonte que não seja "spotify" precisa de provider.
func New(cfg Config, c *cache.TTLMap[Info]) (*Enricher, error) {
	if len(cfg.Sources) == 0 {
		return nil, errors.New("nenhuma fonte de gênero configurada")
	}
	for _, s := range cfg.Sources {
		if s != SourceSpotify && cfg.Providers[s] == nil {
			return nil, fmt.Errorf("fonte %q sem provider configurado", s)
		}
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.SaveEvery <= 0 {
		cfg.SaveEvery = 50
	}
	if cfg.BreakAfter <= 0 {
		cfg.BreakAfter = 5
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Enricher{cfg: cfg, cache: c}, nil
}

// Artists extrai os artistas únicos das faixas. Por padrão só o principal
// (primeiro creditado); com includeFeatured, todos.
func Artists(tracks []spotify.SavedTrack, includeFeatured bool) []spotify.Artist {
	seen := map[string]bool{}
	var out []spotify.Artist
	for _, t := range tracks {
		for i, a := range t.Artists {
			if i > 0 && !includeFeatured {
				break
			}
			if a.ID == "" || seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			out = append(out, a)
		}
	}
	return out
}

type breaker struct {
	fails    atomic.Int32
	disabled atomic.Bool
}

// Enrich resolve cada artista (cache → Spotify → provedores externos em ordem).
// Erros fatais (rate limit prolongado, 401/403, contexto cancelado) abortam;
// falhas de provedores externos só são contadas. O cache é salvo de tempos
// em tempos e ao final, mesmo em caso de erro, para a execução ser retomável.
func (e *Enricher) Enrich(ctx context.Context, f ArtistFetcher, artists []spotify.Artist, progress func(done, total int)) (map[string]Info, Stats, error) {
	st := Stats{BySource: map[string]int{}}
	var (
		mu      sync.Mutex
		results = make(map[string]Info, len(artists))
		done    int
		fresh   int
		brk     = map[string]*breaker{}
	)
	for _, s := range e.cfg.Sources {
		brk[s] = &breaker{}
	}
	st.Artists = len(artists)
	defer func() { _ = e.cache.Save() }()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(e.cfg.Concurrency)
	for _, a := range artists {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			info, isCached := e.cached(a)
			if !isCached {
				var err error
				if info, err = e.resolve(gctx, f, a, brk, &mu, &st); err != nil {
					return err
				}
				e.cache.Put(a.ID, info)
			}

			mu.Lock()
			results[a.ID] = info
			done++
			if isCached {
				st.Cached++
			} else {
				st.BySource[info.Source]++
				fresh++
			}
			needSave := !isCached && fresh%e.cfg.SaveEvery == 0
			if progress != nil {
				progress(done, len(artists))
			}
			mu.Unlock()
			if needSave {
				if err := e.cache.Save(); err != nil {
					e.cfg.Logger.Warn("falha ao salvar cache de artistas", "erro", err)
				}
			}
			return nil
		})
	}
	err := g.Wait()
	for name, b := range brk {
		if b.disabled.Load() {
			st.Disabled = append(st.Disabled, name)
		}
	}
	slices.Sort(st.Disabled)
	return results, st, err
}

// cached devolve o cache se ele ainda serve: tem gênero, ou já tentou todas
// as fontes hoje configuradas (assim, ligar o Last.fm depois reprocessa os
// artistas que tinham ficado sem gênero).
func (e *Enricher) cached(a spotify.Artist) (Info, bool) {
	info, ok := e.cache.Get(a.ID)
	if !ok {
		return Info{}, false
	}
	if len(info.Genres) > 0 {
		return info, true
	}
	for _, s := range e.cfg.Sources {
		if !slices.Contains(info.Tried, s) {
			return Info{}, false
		}
	}
	return info, true
}

func (e *Enricher) resolve(ctx context.Context, f ArtistFetcher, a spotify.Artist, brk map[string]*breaker, mu *sync.Mutex, st *Stats) (Info, error) {
	info := Info{ID: a.ID, Name: a.Name, Source: SourceNone}
	// reaproveita imagem/nome de uma tentativa anterior
	if old, ok := e.cache.Get(a.ID); ok {
		info.ImageURL = old.ImageURL
		info.Tried = old.Tried
	}

	for _, src := range e.cfg.Sources {
		var genres []string
		if src == SourceSpotify {
			d, err := f.Artist(ctx, a.ID)
			switch {
			case err == nil:
				info.ImageURL = d.ImageURL
				if d.Name != "" {
					info.Name = d.Name
				}
				genres = NormalizeGenres(d.Genres, 0)
			case spotify.IsNotFound(err):
				// artista removido do catálogo: segue para os outros provedores
			default:
				return Info{}, fmt.Errorf("artista %s (%s): %w", a.Name, a.ID, err)
			}
		} else {
			b := brk[src]
			if b.disabled.Load() {
				continue
			}
			var err error
			genres, err = e.cfg.Providers[src].Genres(ctx, info.Name)
			if err != nil {
				if ctx.Err() != nil {
					return Info{}, ctx.Err()
				}
				e.cfg.Logger.Warn("provedor de gênero falhou", "fonte", src, "artista", info.Name, "erro", err)
				mu.Lock()
				st.Failures++
				mu.Unlock()
				if int(b.fails.Add(1)) >= e.cfg.BreakAfter && b.disabled.CompareAndSwap(false, true) {
					e.cfg.Logger.Warn("provedor desligado após falhas consecutivas", "fonte", src)
				}
				continue // não conta como "tentado"
			}
			b.fails.Store(0)
		}
		if !slices.Contains(info.Tried, src) {
			info.Tried = append(info.Tried, src)
		}
		if len(genres) > 0 {
			info.Genres, info.Source = genres, src
			break
		}
	}
	return info, nil
}
