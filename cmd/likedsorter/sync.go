package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/ikauedeveloper/likedsorter/internal/ai"
	"github.com/ikauedeveloper/likedsorter/internal/auth"
	"github.com/ikauedeveloper/likedsorter/internal/cache"
	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// dataFlags são as flags de coleta/cache/enriquecimento, compartilhadas por
// sync, groups (e, nas próximas fases, plan/apply).
type dataFlags struct {
	market      string
	fullSync    bool
	refresh     bool
	noCache     bool
	tracksTTL   time.Duration
	noEnrich    bool
	genreSource string
	artistsTTL  time.Duration
	featured    bool
	concurrency int
}

func (d *dataFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&d.market, "market", cur.Market, "market enviado ao Spotify (from_token ou código ISO, ex.: BR)")
	fs.BoolVar(&d.fullSync, "full-sync", false, "reler todas as curtidas (ignora a parada incremental)")
	fs.BoolVar(&d.refresh, "refresh", false, "ignorar o cache existente e reconstruí-lo")
	fs.BoolVar(&d.noCache, "no-cache", false, "não ler nem gravar cache")
	fs.DurationVar(&d.tracksTTL, "tracks-ttl", cur.TracksTTL, "idade máxima do cache de faixas antes de reler tudo (0 = nunca)")
	fs.BoolVar(&d.noEnrich, "no-enrich", false, "não buscar gêneros dos artistas")
	fs.StringVar(&d.genreSource, "genre-source", cur.GenreSource, "fontes de gênero em ordem de prioridade: spotify,musicbrainz,lastfm,deezer,discogs")
	fs.DurationVar(&d.artistsTTL, "artists-ttl", cur.ArtistsTTL, "validade do cache de artistas (0 = nunca expira)")
	fs.BoolVar(&d.featured, "include-featured", cur.IncludeFeatured, "considerar artistas de participação (enriquecimento e estratégia artist)")
	fs.IntVar(&d.concurrency, "concurrency", cur.Concurrency, "chamadas simultâneas ao Spotify para artistas")
}

// session é um cliente Spotify autenticado (token do disco, com renovação automática).
type session struct {
	Cfg    config.Config
	Client *spotify.Client
	Logger *slog.Logger
}

func newSession(ctx context.Context, market string) (*session, error) {
	store, err := tokenStore()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	ts, err := auth.TokenSource(ctx, auth.OAuthConfig(cfg.ClientID, cfg.RedirectURI), store)
	if err != nil {
		return nil, err
	}
	hc := oauth2.NewClient(ctx, ts)
	hc.Timeout = 30 * time.Second
	logger := appLogger
	client := spotify.New(hc, spotify.Options{Market: market, UserAgent: "likedsorter/" + version, Logger: logger})
	return &session{Cfg: cfg, Client: client, Logger: logger}, nil
}

// loaded é o resultado de loadData.
type loaded struct {
	Sync    *library.SyncResult
	Infos   map[string]enrich.Info
	Enrich  *enrich.Stats // nil se o enriquecimento foi pulado
	Cfg     config.Config
	Enabled bool // enriquecimento executado
	Client  *spotify.Client
}

// loadData autentica, sincroniza as curtidas e (se enrich) completa os artistas.
func loadData(ctx context.Context, d *dataFlags, enrichNow bool) (*loaded, error) {
	sources, err := enrich.ParseSources(d.genreSource)
	if err != nil {
		return nil, err
	}
	sess, err := newSession(ctx, d.market)
	if err != nil {
		return nil, err
	}
	cfg, client, logger := sess.Cfg, sess.Client, sess.Logger
	cacheDir, err := config.CacheDir()
	if err != nil {
		return nil, err
	}
	cstore := &cache.Store{Dir: cacheDir, Disabled: d.noCache, NoRead: d.refresh}

	interactive := isTerminal(os.Stderr)
	progress := func(label string) func(done, total int) {
		return func(done, total int) {
			if interactive {
				fmt.Fprintf(os.Stderr, "\r%s", progressBar(label, done, total))
			}
		}
	}
	endLine := func() {
		if interactive {
			fmt.Fprintln(os.Stderr)
		}
	}

	res, err := library.Sync(ctx, client, cstore, library.SyncOptions{Full: d.fullSync, TTL: d.tracksTTL, Progress: progress("Lendo curtidas")})
	endLine()
	if err != nil {
		return nil, err
	}
	out := &loaded{Sync: res, Infos: map[string]enrich.Info{}, Cfg: cfg, Client: client}
	if d.noEnrich || !enrichNow {
		return out, nil
	}

	en, err := buildEnricher(cfg, sources, cstore, d.artistsTTL, d.concurrency, logger)
	if err != nil {
		return nil, err
	}
	if slices.Contains(sources, enrich.SourceMusicBrainz) {
		fmt.Fprintln(os.Stderr, "Nota: o MusicBrainz limita a ~1 req/s; o progresso é salvo e a execução pode ser retomada.")
	}
	infos, st, err := en.Enrich(ctx, client, enrich.Artists(res.Tracks, d.featured), progress("Artistas"))
	endLine()
	if err != nil {
		return nil, err
	}
	if p, err := aiStorePath(); err == nil {
		if st, err := ai.LoadStore(p); err == nil {
			if n := st.Apply(infos); n > 0 {
				appLogger.Debug("gêneros da IA aplicados", "artistas", n)
			}
		}
	}
	if ov, err := loadOverrides(); err != nil {
		return nil, err
	} else if n := ov.Apply(infos); n > 0 {
		appLogger.Debug("gêneros personalizados aplicados", "artistas", n)
	}
	out.Infos, out.Enrich, out.Enabled = infos, &st, true
	return out, nil
}

func runSync(ctx context.Context, args []string, out io.Writer) error {
	return withJSON(args, out, func(a []string, text io.Writer) (any, error) { return syncRun(ctx, a, text) })
}

func syncRun(ctx context.Context, args []string, out io.Writer) (any, error) {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	var d dataFlags
	d.register(fs)
	preview := fs.Int("preview", 5, "quantas curtidas mais recentes mostrar no resumo")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	l, err := loadData(ctx, &d, true)
	if err != nil {
		return nil, err
	}
	res := l.Sync

	if res.Mode == "incremental" {
		fmt.Fprintf(out, "Sincronização incremental: %d novas\n", res.New)
	} else {
		fmt.Fprintf(out, "Sincronização completa (%s): %d faixas\n", res.Reason, res.New)
	}
	if d.noCache {
		fmt.Fprintln(out, "Cache desativado (--no-cache): nada foi salvo.")
	}
	st := res.Stats
	fmt.Fprintln(out, "Curtidas no Spotify:", res.Total)
	fmt.Fprintln(out, "Úteis:", len(res.Tracks))
	fmt.Fprintln(out, "Ignoradas:", st.Local, "locais,", st.Unavailable, "indisponíveis no seu mercado,", st.NoID, "sem ID,", st.Duplicates, "duplicadas")
	for i, t := range res.Tracks {
		if i >= *preview {
			break
		}
		fmt.Fprintln(out, " ", t.AddedAt.Local().Format("2006-01-02"), artistNames(t)+":", t.Name)
	}
	if l.Enabled {
		printEnrichSummary(out, res.Tracks, l.Infos, *l.Enrich)
	}
	result := map[string]any{
		"mode": res.Mode, "reason": res.Reason, "new": res.New, "liked_total": res.Total, "useful": len(res.Tracks),
		"ignored": map[string]int{"local": st.Local, "unavailable": st.Unavailable, "no_id": st.NoID, "duplicates": st.Duplicates},
	}
	if l.Enabled {
		result["artists"] = l.Enrich
	}
	return result, nil
}

func artistNames(t spotify.SavedTrack) string {
	names := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

func buildEnricher(cfg config.Config, sources []string, cstore *cache.Store, ttl time.Duration, conc int,
	logger *slog.Logger) (*enrich.Enricher, error) {
	ua := fmt.Sprintf("likedsorter/%s ( %s )", version, cfg.Contact)
	provs := map[string]enrich.GenreProvider{}
	for _, s := range sources {
		switch s {
		case enrich.SourceMusicBrainz:
			p, err := enrich.NewMusicBrainz(enrich.MusicBrainzOptions{UserAgent: ua, HTTP: &http.Client{Timeout: 20 * time.Second}})
			if err != nil {
				return nil, err
			}
			provs[s] = p
		case enrich.SourceDeezer:
			provs[s] = enrich.NewDeezer(enrich.DeezerOptions{UserAgent: ua})
		case enrich.SourceDiscogs:
			p, err := enrich.NewDiscogs(enrich.DiscogsOptions{Token: cfg.DiscogsKey, UserAgent: ua})
			if err != nil {
				return nil, fmt.Errorf("%w (defina no .env ou remova \"discogs\" de --genre-source)", err)
			}
			provs[s] = p
		case enrich.SourceLastFM:
			p, err := enrich.NewLastFM(enrich.LastFMOptions{APIKey: cfg.LastFMKey, UserAgent: ua})
			if err != nil {
				return nil, fmt.Errorf("%w (defina no .env ou remova \"lastfm\" de --genre-source)", err)
			}
			provs[s] = p
		}
	}
	m, err := cache.OpenTTLMap[enrich.Info](cstore, "artists.json", ttl, nil)
	if err != nil {
		return nil, err
	}
	return enrich.New(enrich.Config{Sources: sources, Providers: provs, Concurrency: conc, Logger: logger}, m)
}

func printEnrichSummary(out io.Writer, tracks []spotify.SavedTrack, infos map[string]enrich.Info, st enrich.Stats) {
	fmt.Fprintf(out, "Artistas: %d únicos | %d do cache", st.Artists, st.Cached)
	for _, src := range []string{enrich.SourceSpotify, enrich.SourceMusicBrainz, enrich.SourceLastFM, enrich.SourceNone} {
		if n := st.BySource[src]; n > 0 {
			label := src
			if src == enrich.SourceNone {
				label = "sem gênero"
			}
			fmt.Fprintf(out, " | %d %s", n, label)
		}
	}
	fmt.Fprintln(out)
	if st.Failures > 0 {
		fmt.Fprintf(out, "Atenção: %d consultas a fontes externas falharam (veja os avisos acima).\n", st.Failures)
	}
	for _, d := range st.Disabled {
		fmt.Fprintf(out, "Atenção: a fonte %s foi desligada nesta execução por falhas consecutivas.\n", d)
	}

	// Quantas faixas ficam sem gênero (pelo artista principal)?
	noGenre := 0
	for _, t := range tracks {
		if len(t.Artists) == 0 || len(infos[t.Artists[0].ID].Genres) == 0 {
			noGenre++
		}
	}
	fmt.Fprintf(out, "Faixas sem gênero (artista principal): %d de %d\n", noGenre, len(tracks))
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
