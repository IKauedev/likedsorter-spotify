package library

import (
	"sort"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// DupGroup é um conjunto de curtidas com o mesmo ISRC (mesma gravação) mas IDs
// do Spotify diferentes, tipicamente a mesma música em álbuns/versões distintos.
type DupGroup struct {
	ISRC   string
	Tracks []spotify.SavedTrack // mais recentemente curtida primeiro
}

// DedupeResult é a saída de FindDuplicates.
type DedupeResult struct {
	Groups []DupGroup
	NoISRC int // faixas sem ISRC (não dá para comparar)
}

// FindDuplicates agrupa por ISRC e devolve só os grupos com 2+ faixas, do maior
// para o menor. Somente leitura: não remove nada.
func FindDuplicates(tracks []spotify.SavedTrack) DedupeResult {
	var res DedupeResult
	by := map[string][]spotify.SavedTrack{}
	var order []string
	for _, t := range tracks {
		if t.ISRC == "" {
			res.NoISRC++
			continue
		}
		if _, ok := by[t.ISRC]; !ok {
			order = append(order, t.ISRC)
		}
		by[t.ISRC] = append(by[t.ISRC], t)
	}
	for _, isrc := range order {
		ts := by[isrc]
		if len(ts) < 2 {
			continue
		}
		sort.SliceStable(ts, func(i, j int) bool { return ts[i].AddedAt.After(ts[j].AddedAt) })
		res.Groups = append(res.Groups, DupGroup{ISRC: isrc, Tracks: ts})
	}
	sort.SliceStable(res.Groups, func(i, j int) bool {
		if len(res.Groups[i].Tracks) != len(res.Groups[j].Tracks) {
			return len(res.Groups[i].Tracks) > len(res.Groups[j].Tracks)
		}
		return res.Groups[i].ISRC < res.Groups[j].ISRC
	})
	return res
}
