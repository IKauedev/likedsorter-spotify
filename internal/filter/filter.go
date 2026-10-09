// Package filter restringe o processamento a parte da biblioteca
// (--filter-artist, --filter-genre, --since).
package filter

import (
	"fmt"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// Filter combina critérios com E entre tipos e OU dentro de cada lista.
// Artistas e gêneros casam por trecho (sem diferenciar maiúsculas).
type Filter struct {
	Artists []string  // qualquer artista creditado na faixa contém algum trecho
	Genres  []string  // qualquer gênero do artista principal contém algum trecho
	Since   time.Time // faixas curtidas a partir desta data (inclusive); zero = sem limite
}

// SplitList transforma "a, b ,c" em ["a","b","c"] (minúsculas, sem vazios).
func SplitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseSince aceita YYYY-MM-DD (UTC) ou RFC 3339. Vazio = sem filtro.
func ParseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--since %q inválido (use AAAA-MM-DD, ex.: 2023-01-01)", s)
}

// Active diz se algum critério está ligado.
func (f Filter) Active() bool {
	return len(f.Artists) > 0 || len(f.Genres) > 0 || !f.Since.IsZero()
}

// NeedsGenres indica que o filtro depende do enriquecimento de gêneros.
func (f Filter) NeedsGenres() bool { return len(f.Genres) > 0 }

// Describe resume o filtro para os relatórios ("" se inativo).
func (f Filter) Describe() string {
	var parts []string
	if len(f.Artists) > 0 {
		parts = append(parts, "artista~"+strings.Join(f.Artists, "|"))
	}
	if len(f.Genres) > 0 {
		parts = append(parts, "gênero~"+strings.Join(f.Genres, "|"))
	}
	if !f.Since.IsZero() {
		parts = append(parts, "desde "+f.Since.Format("2006-01-02"))
	}
	return strings.Join(parts, ", ")
}

// Apply devolve as faixas que passam por todos os critérios, preservando a ordem.
// Sem filtro ativo devolve a própria fatia.
func (f Filter) Apply(tracks []spotify.SavedTrack, infos map[string]enrich.Info) []spotify.SavedTrack {
	if !f.Active() {
		return tracks
	}
	var out []spotify.SavedTrack
	for _, t := range tracks {
		if f.match(t, infos) {
			out = append(out, t)
		}
	}
	return out
}

func (f Filter) match(t spotify.SavedTrack, infos map[string]enrich.Info) bool {
	if !f.Since.IsZero() && t.AddedAt.Before(f.Since) {
		return false
	}
	if len(f.Artists) > 0 {
		var names []string
		for _, a := range t.Artists {
			names = append(names, a.Name)
		}
		if !anyContains(names, f.Artists) {
			return false
		}
	}
	if len(f.Genres) > 0 {
		if len(t.Artists) == 0 || !anyContains(infos[t.Artists[0].ID].Genres, f.Genres) {
			return false
		}
	}
	return true
}

func anyContains(haystack, needles []string) bool {
	for _, h := range haystack {
		h = strings.ToLower(h)
		for _, n := range needles {
			if strings.Contains(h, n) {
				return true
			}
		}
	}
	return false
}
