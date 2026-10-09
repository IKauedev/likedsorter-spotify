package grouping

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// StrategyNames lista as estratégias simples aceitas em --by (composite = nomes unidos por "+").
var StrategyNames = []string{"artist", "genre", "macro-genre", "decade", "year", "added-period", "language", "rules", "listening"}

const (
	unknownDate   = "Data desconhecida"
	unknownArtist = "Artista desconhecido"
	maxMultiGenre = 3
)

// New cria a estratégia a partir de --by. "macro-genre+decade" cria uma composta
// (produto cartesiano das chaves, unidas por " · ").
func New(spec string, opt Options) (Strategy, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(spec)), "+")
	var subs []Strategy
	for _, p := range parts {
		s, err := newSimple(strings.TrimSpace(p), opt)
		if err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	if len(subs) == 1 {
		return subs[0], nil
	}
	return composite{subs}, nil
}

func newSimple(name string, opt Options) (Strategy, error) {
	switch name {
	case "artist":
		return artist{}, nil
	case "genre":
		return genre{}, nil
	case "macro-genre":
		m := opt.MacroMap
		if m == nil {
			m = DefaultMacroMap()
		}
		return macroGenre{m}, nil
	case "decade":
		return decade{}, nil
	case "year":
		return year{}, nil
	case "added-period":
		return addedPeriod{}, nil
	case "language":
		return language{}, nil
	case "listening":
		return newListening(opt)
	case "rules":
		if opt.Rules == nil {
			return nil, fmt.Errorf("--by=rules exige um arquivo de regras (--rules arquivo.yaml ou <config>/rules.yaml); veja `likedsorter help regras`")
		}
		return rulesStrategy{opt.Rules}, nil
	}
	return nil, fmt.Errorf("estratégia desconhecida %q (opções: %s; combine com +, ex.: macro-genre+decade)",
		name, strings.Join(StrategyNames, ", "))
}

// ------------------------------------------------------------------- artist

type artist struct{}

func (artist) Name() string { return "artist" }
func (artist) Keys(t spotify.SavedTrack, c *Context) []string {
	if len(t.Artists) == 0 || strings.TrimSpace(t.Artists[0].Name) == "" {
		return []string{unknownArtist}
	}
	if !c.Opts.IncludeFeatured {
		return []string{t.Artists[0].Name}
	}
	var keys []string
	for _, a := range t.Artists {
		if n := strings.TrimSpace(a.Name); n != "" {
			keys = append(keys, n)
		}
	}
	return keys
}

// -------------------------------------------------------------------- genre

type genre struct{}

func (genre) Name() string { return "genre" }
func (genre) Keys(t spotify.SavedTrack, c *Context) []string {
	gs := c.PrimaryGenres(t)
	if len(gs) == 0 {
		return []string{NoGenre}
	}
	if !c.Opts.MultiGenre {
		return []string{displayGenre(gs[0])}
	}
	var keys []string
	for i, g := range gs {
		if i == maxMultiGenre {
			break
		}
		keys = append(keys, displayGenre(g))
	}
	return keys
}

// -------------------------------------------------------------- macro-genre

type macroGenre struct{ m *MacroMap }

func (macroGenre) Name() string { return "macro-genre" }
func (s macroGenre) Keys(t spotify.SavedTrack, c *Context) []string {
	gs := c.PrimaryGenres(t)
	if len(gs) == 0 {
		return []string{NoGenre}
	}
	var fams []string
	seen := map[string]bool{}
	for _, g := range gs {
		if f, ok := s.m.Family(g); ok && !seen[f] {
			seen[f] = true
			fams = append(fams, f)
		}
	}
	if len(fams) == 0 {
		return []string{s.m.Fallback}
	}
	if !c.Opts.MultiGenre {
		return fams[:1] // família do primeiro gênero que casou
	}
	return fams
}

// ------------------------------------------------------------ decade / year

type decade struct{}

func (decade) Name() string { return "decade" }
func (decade) Keys(t spotify.SavedTrack, _ *Context) []string {
	y := releaseYear(t)
	switch {
	case y == 0:
		return []string{unknownDate}
	case y < 1950:
		return []string{"Até 1949"}
	case y < 2000:
		return []string{fmt.Sprintf("Anos %02d", y%100/10*10)}
	default:
		return []string{fmt.Sprintf("Anos %d", y/10*10)}
	}
}

type year struct{}

func (year) Name() string { return "year" }
func (year) Keys(t spotify.SavedTrack, _ *Context) []string {
	if y := releaseYear(t); y != 0 {
		return []string{fmt.Sprint(y)}
	}
	return []string{unknownDate}
}

// releaseYear extrai o ano de "2020", "2020-05" ou "2020-05-17" (0 se inválido).
func releaseYear(t spotify.SavedTrack) int {
	d := t.Album.ReleaseDate
	if len(d) < 4 {
		return 0
	}
	y := 0
	for _, r := range d[:4] {
		if r < '0' || r > '9' {
			return 0
		}
		y = y*10 + int(r-'0')
	}
	if y < 1000 {
		return 0
	}
	return y
}

// ------------------------------------------------------------- added-period

type addedPeriod struct{}

func (addedPeriod) Name() string { return "added-period" }
func (addedPeriod) Keys(t spotify.SavedTrack, _ *Context) []string {
	if t.AddedAt.IsZero() {
		return []string{unknownDate}
	}
	return []string{t.AddedAt.UTC().Format("2006-01")} // UTC: resultado independe do fuso
}

// ---------------------------------------------------------------- composite

type composite struct{ subs []Strategy }

func (c composite) Name() string {
	names := make([]string, len(c.subs))
	for i, s := range c.subs {
		names[i] = s.Name()
	}
	return strings.Join(names, "+")
}

func (c composite) Experimental() bool {
	for _, s := range c.subs {
		if e, ok := s.(Experimental); ok && e.Experimental() {
			return true
		}
	}
	return false
}

func (c composite) Keys(t spotify.SavedTrack, ctx *Context) []string {
	keys := []string{""}
	for i, s := range c.subs {
		var next []string
		for _, prefix := range keys {
			for _, k := range s.Keys(t, ctx) {
				if i == 0 {
					next = append(next, k)
				} else {
					next = append(next, prefix+" · "+k)
				}
			}
		}
		keys = next
	}
	return keys
}

// ------------------------------------------------------------------ helpers

// acronyms mantém siglas em maiúsculas ao formatar gêneros.
var acronyms = map[string]string{"r&b": "R&B", "edm": "EDM", "mpb": "MPB", "idm": "IDM", "uk": "UK", "us": "US", "ccm": "CCM", "k-pop": "K-Pop", "j-pop": "J-Pop"}

// displayGenre transforma "hip hop" em "Hip Hop" (siglas preservadas).
func displayGenre(g string) string {
	words := strings.Fields(g)
	for i, w := range words {
		if a, ok := acronyms[w]; ok {
			words[i] = a
			continue
		}
		words[i] = capitalize(w)
	}
	return strings.Join(words, " ")
}

func capitalize(w string) string {
	rs := []rune(w)
	up := true
	for i, r := range rs {
		if up {
			rs[i] = unicode.ToUpper(r)
		}
		up = r == '-'
	}
	return string(rs)
}
