package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// suggestion é um candidato a nova playlist tirado do grupo "Outros"/"Sem gênero".
type suggestion struct {
	Kind   string `json:"kind"` // artista | gênero | década
	Name   string `json:"name"`
	Tracks int    `json:"tracks"`
}

// tally conta faixas por chave e devolve as maiores (>= min), em ordem.
func tally(tracks []spotify.SavedTrack, keys func(spotify.SavedTrack) []string, min, top int) []suggestion {
	count := map[string]int{}
	for _, t := range tracks {
		seen := map[string]bool{}
		for _, k := range keys(t) {
			if k != "" && !seen[k] {
				seen[k] = true
				count[k]++
			}
		}
	}
	var out []suggestion
	for k, n := range count {
		if n >= min {
			out = append(out, suggestion{Name: k, Tracks: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tracks != out[j].Tracks {
			return out[i].Tracks > out[j].Tracks
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > top {
		out = out[:top]
	}
	return out
}

// runSuggest olha o que sobrou em "Outros"/"Sem gênero" e sugere novos grupos (somente leitura).
func runSuggest(ctx context.Context, args []string, out io.Writer) error {
	fl := flag.NewFlagSet("suggest", flag.ContinueOnError)
	var d dataFlags
	var g groupFlags
	var ff filterFlags
	d.register(fl)
	g.register(fl)
	ff.register(fl)
	top := fl.Int("top", 8, "quantas sugestões por tipo")
	minCount := fl.Int("min-count", 5, "mínimo de faixas para virar sugestão")
	emit := fl.Bool("emit-rules", false, "imprimir as sugestões como regras YAML prontas para o rules.yaml")
	asJSON := fl.Bool("json", false, "saída em JSON")
	if err := fl.Parse(args); err != nil {
		return err
	}
	opt, _, err := g.options(d.featured)
	if err != nil {
		return err
	}
	flt, err := ff.build()
	if err != nil {
		return err
	}
	strat, err := grouping.New(g.by, opt)
	if err != nil {
		return err
	}
	l, err := loadData(ctx, &d, true)
	if err != nil {
		return err
	}
	tracks := flt.Apply(l.Sync.Tracks, l.Infos)
	res, err := grouping.Build(tracks, l.Infos, strat, opt)
	if err != nil {
		return err
	}

	var leftovers []spotify.SavedTrack
	for _, gr := range res.Groups {
		if gr.Key == grouping.OtherKey || strings.HasSuffix(gr.Key, " · "+grouping.OtherKey) || gr.Key == grouping.NoGenre {
			leftovers = append(leftovers, gr.Tracks...)
		}
	}
	primary := func(t spotify.SavedTrack) string {
		if len(t.Artists) == 0 {
			return ""
		}
		return t.Artists[0].Name
	}
	var sugs []suggestion
	add := func(kind string, s []suggestion) {
		for _, x := range s {
			x.Kind = kind
			sugs = append(sugs, x)
		}
	}
	add("artista", tally(leftovers, func(t spotify.SavedTrack) []string { return []string{primary(t)} }, *minCount, *top))
	add("gênero", tally(leftovers, func(t spotify.SavedTrack) []string {
		if len(t.Artists) == 0 {
			return nil
		}
		return l.Infos[t.Artists[0].ID].Genres
	}, *minCount, *top))
	add("década", tally(leftovers, func(t spotify.SavedTrack) []string { return []string{grouping.DecadeOf(t)} }, *minCount, 3))

	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"leftover_tracks": len(leftovers), "total_tracks": len(tracks), "suggestions": sugs})
	}
	if len(leftovers) == 0 {
		fmt.Fprintln(out, "Nada sobrou em \"Outros\"/\"Sem gênero\": não há o que sugerir.")
		return nil
	}
	fmt.Fprintf(out, "Sobraram %d de %d faixas em \"Outros\"/\"Sem gênero\" (%.0f%%).\n\n", len(leftovers), len(tracks), 100*float64(len(leftovers))/float64(max(len(tracks), 1)))
	if len(sugs) == 0 {
		fmt.Fprintf(out, "Nenhum artista, gênero ou década com %d+ faixas entre as sobras. Tente --min-count menor.\n", *minCount)
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIPO\tSUGESTÃO\tFAIXAS")
	for _, s := range sugs {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", s.Kind, s.Name, s.Tracks)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if *emit {
		fmt.Fprintln(out, "\n# Cole em rules.yaml (likedsorter rules path) e use: likedsorter plan --by=rules")
		fmt.Fprintln(out, "regras:")
		for _, s := range sugs {
			switch s.Kind {
			case "artista":
				fmt.Fprintf(out, "  - nome: %s\n    artista: [%q]\n", s.Name, strings.ToLower(s.Name))
			case "gênero":
				fmt.Fprintf(out, "  - nome: %s\n    genero: [%q]\n", strings.Title(s.Name), s.Name) //nolint:staticcheck // títulos simples
			}
		}
		return nil
	}
	fmt.Fprintln(out, "\nPróximos passos:")
	fmt.Fprintln(out, "  likedsorter suggest --emit-rules       gera regras YAML com estas sugestões")
	fmt.Fprintln(out, "  likedsorter genre set \"Artista\" rock   define o gênero de quem ficou sem")
	fmt.Fprintln(out, "  likedsorter plan --by=artist --min-size=5   uma playlist por artista frequente")
	return nil
}
