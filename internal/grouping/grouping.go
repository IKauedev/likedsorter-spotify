// Package grouping organiza as curtidas em grupos segundo uma Strategy.
//
// Uma Strategy só diz em quais chaves uma faixa entra (Keys). Todo o resto é
// comum e vive em Build: tamanho mínimo, divisão em partes, ordenação interna
// e ordem dos grupos.
package grouping

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

const (
	// OtherKey é o grupo que recebe os grupos pequenos (--small-groups=other).
	OtherKey = "Outros"
	// MaxPlaylistTracks é o limite do Spotify por playlist.
	MaxPlaylistTracks = 10000
	// NoGenre reexporta o bucket de artistas sem gênero.
	NoGenre = enrich.NoGenre

	SplitDecade = "decade"
	SplitYear   = "year"

	SmallToOther = "other"
	SmallSkip    = "skip"
)

// Opções de ordenação interna (--sort).
const (
	SortAdded   = "added"   // curtida mais recente primeiro
	SortRelease = "release" // lançamento mais recente primeiro
	SortTitle   = "title"   // A-Z
)

// Options reúne as regras comuns a todas as estratégias.
type Options struct {
	MinSize         int    // grupos menores vão para "Outros" ou são ignorados (0 = desligado)
	SmallGroups     string // "other" (padrão) ou "skip"
	MaxSize         int    // tamanho máximo por playlist (0 = 10.000); acima disso divide em partes
	MultiGenre      bool   // a faixa pode entrar em mais de um grupo (gênero/macro-gênero)
	IncludeFeatured bool   // estratégia artist: considerar participações
	Sort            string // added | release | title (popularity foi removida pelo Spotify)
	MacroMap        *MacroMap
	Rules           *RuleSet // regras do usuário (--by=rules)
	SplitOver       int      // grupos com mais de N faixas são divididos por SplitBy (0 = desligado)
	SplitBy         string   // decade (padrão) | year

	// Modo --by=listening (frequência de reprodução).
	Plays         PlayStats
	Now           time.Time // padrão: agora
	ListenTop     int       // tamanho dos "Mais ouvidas" (padrão 50)
	ListenDays    int       // janela dos "Mais ouvidas" recentes (padrão 30)
	ForgottenDays int       // sem tocar há tantos dias = "Esquecidas" (padrão 180)
}

// Normalize preenche padrões e valida.
func (o *Options) Normalize() error {
	if o.SmallGroups == "" {
		o.SmallGroups = SmallToOther
	}
	if o.SmallGroups != SmallToOther && o.SmallGroups != SmallSkip {
		return fmt.Errorf("--small-groups deve ser %q ou %q, recebi %q", SmallToOther, SmallSkip, o.SmallGroups)
	}
	if o.Sort == "" {
		o.Sort = SortAdded
	}
	switch o.Sort {
	case SortAdded, SortRelease, SortTitle:
	case "popularity":
		return fmt.Errorf("--sort=popularity não está disponível: o Spotify removeu o campo popularity da API em fev/2026 (use added, release ou title)")
	default:
		return fmt.Errorf("--sort deve ser added, release ou title, recebi %q", o.Sort)
	}
	if o.SplitBy == "" {
		o.SplitBy = SplitDecade
	}
	if o.SplitBy != SplitDecade && o.SplitBy != SplitYear {
		return fmt.Errorf("--split-by deve ser %q ou %q, recebi %q", SplitDecade, SplitYear, o.SplitBy)
	}
	if o.SplitOver < 0 {
		return fmt.Errorf("--split-over não pode ser negativo")
	}
	if o.MinSize < 0 || o.MaxSize < 0 {
		return fmt.Errorf("--min-size e --max-size não podem ser negativos")
	}
	if o.MaxSize == 0 || o.MaxSize > MaxPlaylistTracks {
		o.MaxSize = MaxPlaylistTracks
	}
	return nil
}

// Context dá às estratégias acesso aos metadados dos artistas.
type Context struct {
	Info map[string]enrich.Info // por ID de artista
	Opts Options
}

// PrimaryGenres devolve os gêneros do artista principal da faixa (pode ser vazio).
func (c *Context) PrimaryGenres(t spotify.SavedTrack) []string {
	if len(t.Artists) == 0 {
		return nil
	}
	return c.Info[t.Artists[0].ID].Genres
}

// Strategy decide em quais grupos uma faixa entra. Deve devolver ao menos uma
// chave; devolver mais de uma só quando as opções permitirem (ex.: MultiGenre).
type Strategy interface {
	Name() string
	Keys(t spotify.SavedTrack, c *Context) []string
}

// Experimental marca estratégias sem garantia de qualidade (ex.: language).
type Experimental interface{ Experimental() bool }

// Group é uma playlist em potencial.
type Group struct {
	Key    string // identidade estável (sem o sufixo de parte)
	Name   string // nome de exibição; "Rock (Parte 2)" quando dividido
	Part   int    // 1..Parts
	Parts  int
	Tracks []spotify.SavedTrack
}

// Result é a saída de Build.
type Result struct {
	Groups   []Group
	Total    int // faixas de entrada
	Assigned int // faixas únicas que entraram em algum grupo
	Skipped  int // faixas que ficaram sem grupo (--small-groups=skip)
}

// Build aplica a estratégia e as regras comuns. Não altera tracks.
func Build(tracks []spotify.SavedTrack, infos map[string]enrich.Info, s Strategy, opt Options) (*Result, error) {
	if err := opt.Normalize(); err != nil {
		return nil, err
	}
	ctx := &Context{Info: infos, Opts: opt}

	buckets := map[string][]spotify.SavedTrack{}
	var order []string
	add := func(key string, t spotify.SavedTrack) {
		if _, ok := buckets[key]; !ok {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], t)
	}
	for _, t := range tracks {
		keys := s.Keys(t, ctx)
		if len(keys) == 0 {
			keys = []string{OtherKey}
		}
		seen := map[string]bool{}
		for _, k := range keys {
			if k == SkipKey {
				continue
			}
			k = strings.TrimSpace(k)
			if k == "" {
				k = OtherKey
			}
			if !seen[k] {
				seen[k] = true
				add(k, t)
			}
		}
	}

	order = splitLarge(buckets, order, opt)
	if opt.MinSize > 0 {
		order = applyMinSize(buckets, order, opt)
	}

	less := trackLess(opt.Sort)
	var groups []Group
	assigned := map[string]struct{}{}
	for _, key := range order {
		ts := buckets[key]
		sort.SliceStable(ts, func(i, j int) bool { return less(ts[i], ts[j]) })
		parts := (len(ts) + opt.MaxSize - 1) / opt.MaxSize
		for p := 0; p < parts; p++ {
			lo, hi := p*opt.MaxSize, min((p+1)*opt.MaxSize, len(ts))
			g := Group{Key: key, Name: key, Part: p + 1, Parts: parts, Tracks: ts[lo:hi]}
			if parts > 1 {
				g.Name = fmt.Sprintf("%s (Parte %d)", key, p+1)
			}
			groups = append(groups, g)
		}
		for _, t := range ts {
			assigned[t.ID] = struct{}{}
		}
	}
	sortGroups(groups)
	return &Result{Groups: groups, Total: len(tracks), Assigned: len(assigned), Skipped: len(tracks) - len(assigned)}, nil
}

// applyMinSize move (other) ou descarta (skip) os grupos abaixo do mínimo.
// Os tamanhos são medidos antes de qualquer movimentação. Em modo "other" o
// próprio grupo "Outros" nunca é descartado.
func applyMinSize(buckets map[string][]spotify.SavedTrack, order []string, opt Options) []string {
	var small []string
	for _, k := range order {
		if len(buckets[k]) < opt.MinSize && (k != OtherKey || opt.SmallGroups != SmallToOther) {
			small = append(small, k)
		}
	}
	if len(small) == 0 {
		return order
	}
	isSmall := map[string]bool{}
	for _, k := range small {
		isSmall[k] = true
	}

	var moved []spotify.SavedTrack
	if opt.SmallGroups == SmallToOther {
		inOther := map[string]bool{}
		for _, t := range buckets[OtherKey] {
			inOther[t.ID] = true
		}
		for _, k := range small {
			for _, t := range buckets[k] {
				if !inOther[t.ID] {
					inOther[t.ID] = true
					moved = append(moved, t)
				}
			}
		}
	}

	var kept []string
	for _, k := range order {
		if isSmall[k] {
			delete(buckets, k)
		} else {
			kept = append(kept, k)
		}
	}
	if len(moved) > 0 {
		if _, ok := buckets[OtherKey]; !ok {
			kept = append(kept, OtherKey)
		}
		buckets[OtherKey] = append(buckets[OtherKey], moved...)
	}
	return kept
}

func trackLess(mode string) func(a, b spotify.SavedTrack) bool {
	byAdded := func(a, b spotify.SavedTrack) bool {
		if !a.AddedAt.Equal(b.AddedAt) {
			return a.AddedAt.After(b.AddedAt)
		}
		return a.ID < b.ID
	}
	switch mode {
	case SortRelease:
		return func(a, b spotify.SavedTrack) bool {
			if a.Album.ReleaseDate != b.Album.ReleaseDate {
				return a.Album.ReleaseDate > b.Album.ReleaseDate
			}
			return byAdded(a, b)
		}
	case SortTitle:
		return func(a, b spotify.SavedTrack) bool {
			ta, tb := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if ta != tb {
				return ta < tb
			}
			return byAdded(a, b)
		}
	default:
		return byAdded
	}
}

// sortGroups: maiores primeiro; "Sem gênero" e "Outros" por último. Partes do
// mesmo grupo ficam juntas e em ordem.
func sortGroups(gs []Group) {
	rank := func(k string) int {
		switch k {
		case OtherKey:
			return 2
		case NoGenre:
			return 1
		}
		return 0
	}
	size := map[string]int{}
	for _, g := range gs {
		size[g.Key] += len(g.Tracks)
	}
	sort.SliceStable(gs, func(i, j int) bool {
		a, b := gs[i], gs[j]
		if ra, rb := rank(a.Key), rank(b.Key); ra != rb {
			return ra < rb
		}
		if size[a.Key] != size[b.Key] {
			return size[a.Key] > size[b.Key]
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Part < b.Part
	})
}
