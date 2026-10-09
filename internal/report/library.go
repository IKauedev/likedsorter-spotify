package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// LibraryStats descreve a biblioteca de curtidas (comando `stats`).
type LibraryStats struct {
	Liked        int           `json:"liked"`
	Usable       int           `json:"usable"`
	Filter       string        `json:"filter,omitempty"`
	Ignored      library.Stats `json:"ignored"`
	Hours        float64       `json:"hours"`
	Explicit     int           `json:"explicit"`
	GenresLoaded bool          `json:"genres_loaded"`
	NoGenre      int           `json:"no_genre_tracks"`
	ByDecade     []Count       `json:"by_release_decade"`
	AddedByYear  []Count       `json:"added_by_year"`
	TopArtists   []Count       `json:"top_artists"`
	TopGenres    []Count       `json:"top_genres"`
}

// ComputeLibrary calcula as estatísticas gerais das faixas (já filtradas).
func ComputeLibrary(liked int, ignored library.Stats, tracks []spotify.SavedTrack, infos map[string]enrich.Info, genresLoaded bool, filter string) LibraryStats {
	s := LibraryStats{Liked: liked, Usable: len(tracks), Ignored: ignored, GenresLoaded: genresLoaded, Filter: filter}
	decades, years := map[string]int{}, map[string]int{}
	artists, genres := map[string]int{}, map[string]int{}
	var ms int64
	for _, t := range tracks {
		ms += int64(t.DurationMs)
		if t.Explicit {
			s.Explicit++
		}
		if len(t.Album.ReleaseDate) >= 4 {
			var y int
			if _, err := fmt.Sscanf(t.Album.ReleaseDate[:4], "%d", &y); err == nil && y >= 1000 {
				decades[fmt.Sprintf("%ds", y/10*10)]++
			}
		}
		if !t.AddedAt.IsZero() {
			years[t.AddedAt.UTC().Format("2006")]++
		}
		if len(t.Artists) == 0 {
			s.NoGenre++
			continue
		}
		artists[t.Artists[0].Name]++
		if gs := infos[t.Artists[0].ID].Genres; len(gs) > 0 {
			genres[gs[0]]++
		} else {
			s.NoGenre++
		}
	}
	s.Hours = float64(ms) / float64(time.Hour/time.Millisecond)
	s.ByDecade, s.AddedByYear = byKey(decades), byKey(years)
	s.TopArtists, s.TopGenres = top(artists), top(genres)
	return s
}

// byKey ordena por chave (cronológico).
func byKey(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RenderLibrary escreve as estatísticas em "table" ou "json".
func RenderLibrary(w io.Writer, format string, s LibraryStats) error {
	switch strings.ToLower(format) {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(s)
	case FormatTable, "":
	default:
		return fmt.Errorf("formato desconhecido %q para stats (use table ou json)", format)
	}

	fmt.Fprintln(w, "likedsorter | estatísticas da biblioteca")
	if s.Filter != "" {
		fmt.Fprintf(w, "Filtro ativo: %s\n", s.Filter)
	}
	fmt.Fprintf(w, "\nCurtidas no Spotify: %d | consideradas: %d (ignoradas: %s)\n",
		s.Liked, s.Usable, fmt.Sprintf("%d locais, %d indisponíveis, %d sem ID, %d duplicadas",
			s.Ignored.Local, s.Ignored.Unavailable, s.Ignored.NoID, s.Ignored.Duplicates))
	fmt.Fprintf(w, "Duração total: %.1f h | explícitas: %d\n", s.Hours, s.Explicit)
	if s.GenresLoaded {
		fmt.Fprintf(w, "Faixas sem gênero: %d\n", s.NoGenre)
	} else {
		fmt.Fprintln(w, "Faixas sem gênero: n/d (gêneros não carregados)")
	}
	fmt.Fprintln(w, "\nPor década de lançamento:", countsInline(s.ByDecade))
	fmt.Fprintln(w, "Curtidas por ano:        ", countsInline(s.AddedByYear))
	fmt.Fprintln(w, "\nTop artistas:", countsInline(s.TopArtists))
	if s.GenresLoaded {
		fmt.Fprintln(w, "Top gêneros: ", countsInline(s.TopGenres))
	}
	return nil
}

// ------------------------------------------------------------------ dedupe

type dupJSON struct {
	ISRC   string     `json:"isrc"`
	Tracks []dupTrack `json:"tracks"`
}

type dupTrack struct {
	ID      string `json:"id"`
	Artists string `json:"artists"`
	Name    string `json:"name"`
	Album   string `json:"album"`
	Release string `json:"release"`
	AddedAt string `json:"added_at"`
}

func toDup(g library.DupGroup) dupJSON {
	out := dupJSON{ISRC: g.ISRC}
	for _, t := range g.Tracks {
		names := make([]string, len(t.Artists))
		for i, a := range t.Artists {
			names[i] = a.Name
		}
		out.Tracks = append(out.Tracks, dupTrack{
			ID: t.ID, Artists: strings.Join(names, ", "), Name: t.Name, Album: t.Album.Name,
			Release: t.Album.ReleaseDate, AddedAt: t.AddedAt.UTC().Format("2006-01-02"),
		})
	}
	return out
}

// RenderDuplicates escreve o resultado de library.FindDuplicates ("table" ou "json").
func RenderDuplicates(w io.Writer, format string, r library.DedupeResult, total int) error {
	switch strings.ToLower(format) {
	case FormatJSON:
		out := struct {
			Checked int       `json:"tracks_checked"`
			NoISRC  int       `json:"without_isrc"`
			Groups  []dupJSON `json:"groups"`
		}{Checked: total, NoISRC: r.NoISRC, Groups: []dupJSON{}}
		for _, g := range r.Groups {
			out.Groups = append(out.Groups, toDup(g))
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(out)
	case FormatTable, "":
	default:
		return fmt.Errorf("formato desconhecido %q para dedupe (use table ou json)", format)
	}

	fmt.Fprintf(w, "likedsorter | duplicatas por ISRC (somente listagem; nada é removido)\n")
	fmt.Fprintf(w, "Faixas verificadas: %d | sem ISRC (não comparáveis): %d | grupos duplicados: %d\n", total, r.NoISRC, len(r.Groups))
	for _, g := range r.Groups {
		d := toDup(g)
		fmt.Fprintf(w, "\nISRC %s (%d versões)\n", d.ISRC, len(d.Tracks))
		for _, t := range d.Tracks {
			fmt.Fprintf(w, "  %s: %s | %s (%s) | curtida em %s | %s\n", t.Artists, t.Name, t.Album, t.Release, t.AddedAt, t.ID)
		}
	}
	return nil
}
