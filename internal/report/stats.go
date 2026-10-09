// Package report calcula estatísticas e renderiza o plano em tabela, JSON, CSV e Markdown.
package report

import (
	"sort"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// Count é um item de ranking.
type Count struct {
	Name   string `json:"name"`
	Tracks int    `json:"tracks"`
}

// Bucket é uma faixa da distribuição de tamanhos.
type Bucket struct {
	Label     string `json:"label"`
	Playlists int    `json:"playlists"`
}

// Stats resume a biblioteca e o agrupamento.
type Stats struct {
	Liked        int           `json:"liked"`  // total informado pelo Spotify
	Usable       int           `json:"usable"` // faixas úteis após filtros
	Ignored      library.Stats `json:"ignored"`
	Groups       int           `json:"groups"` // playlists (partes contam separadamente)
	Assigned     int           `json:"assigned"`
	SkippedSmall int           `json:"skipped_small_groups"`
	NoGenre      int           `json:"no_genre_tracks"`
	Sizes        []Bucket      `json:"size_distribution"`
	TopArtists   []Count       `json:"top_artists"`
	TopGenres    []Count       `json:"top_genres"`
	GenresLoaded bool          `json:"genres_loaded"` // false se o enriquecimento foi pulado
}

// StatsInput reúne as fontes das estatísticas.
type StatsInput struct {
	Liked        int
	Ignored      library.Stats
	Tracks       []spotify.SavedTrack
	Infos        map[string]enrich.Info
	Result       *grouping.Result
	GenresLoaded bool
}

var bucketEdges = []struct {
	min   int
	label string
}{{1, "1-4"}, {5, "5-9"}, {10, "10-24"}, {25, "25-49"}, {50, "50-99"}, {100, "100-499"}, {500, "500-999"}, {1000, "1000+"}}

const topN = 10

// Compute calcula as estatísticas.
func Compute(in StatsInput) Stats {
	s := Stats{
		Liked: in.Liked, Usable: len(in.Tracks), Ignored: in.Ignored,
		Groups: len(in.Result.Groups), Assigned: in.Result.Assigned, SkippedSmall: in.Result.Skipped,
		GenresLoaded: in.GenresLoaded,
	}

	counts := make([]int, len(bucketEdges))
	for _, g := range in.Result.Groups {
		n := len(g.Tracks)
		for i := len(bucketEdges) - 1; i >= 0; i-- {
			if n >= bucketEdges[i].min {
				counts[i]++
				break
			}
		}
	}
	for i, e := range bucketEdges {
		s.Sizes = append(s.Sizes, Bucket{Label: e.label, Playlists: counts[i]})
	}

	artists, genres := map[string]int{}, map[string]int{}
	for _, t := range in.Tracks {
		if len(t.Artists) == 0 {
			s.NoGenre++
			continue
		}
		artists[t.Artists[0].Name]++
		if gs := in.Infos[t.Artists[0].ID].Genres; len(gs) > 0 {
			genres[gs[0]]++
		} else {
			s.NoGenre++
		}
	}
	s.TopArtists, s.TopGenres = top(artists), top(genres)
	return s
}

func top(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tracks != out[j].Tracks {
			return out[i].Tracks > out[j].Tracks
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}
