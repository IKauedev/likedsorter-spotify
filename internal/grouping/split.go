package grouping

import (
	"sort"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// splitMinSub é o menor tamanho de uma subdivisão; menores viram "<grupo> · Outros".
const splitMinSub = 10

// splitLarge divide os grupos com mais de opt.SplitOver faixas por década ou ano de
// lançamento ("Rock" → "Rock · Anos 90", "Rock · Anos 2000"...). Subgrupos pequenos
// são reunidos em "<grupo> · Outros". Se a divisão não separar nada, o grupo fica como está.
func splitLarge(buckets map[string][]spotify.SavedTrack, order []string, opt Options) []string {
	if opt.SplitOver <= 0 {
		return order
	}
	var keyFn Strategy = decade{}
	if opt.SplitBy == SplitYear {
		keyFn = year{}
	}
	minSub := max(opt.MinSize, splitMinSub)

	var out []string
	for _, k := range order {
		ts := buckets[k]
		if len(ts) <= opt.SplitOver {
			out = append(out, k)
			continue
		}
		subs := map[string][]spotify.SavedTrack{}
		for _, t := range ts {
			sk := k + " · " + keyFn.Keys(t, nil)[0]
			subs[sk] = append(subs[sk], t)
		}
		names := make([]string, 0, len(subs))
		for sk := range subs {
			names = append(names, sk)
		}
		sort.Strings(names)

		rest := k + " · " + OtherKey
		var kept []string
		for _, sk := range names {
			if len(subs[sk]) < minSub {
				subs[rest] = append(subs[rest], subs[sk]...)
				delete(subs, sk)
				continue
			}
			kept = append(kept, sk)
		}
		if len(kept) == 0 || (len(kept) == 1 && len(subs[rest]) == 0) {
			out = append(out, k) // nada a separar de verdade
			continue
		}
		delete(buckets, k)
		for _, sk := range kept {
			buckets[sk] = subs[sk]
			out = append(out, sk)
		}
		if len(subs[rest]) > 0 {
			buckets[rest] = subs[rest]
			out = append(out, rest)
		}
	}
	return out
}

// DecadeOf devolve o nome da década de lançamento da faixa ("Anos 90").
func DecadeOf(t spotify.SavedTrack) string { return decade{}.Keys(t, nil)[0] }
