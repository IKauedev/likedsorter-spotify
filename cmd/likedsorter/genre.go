package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
)

func overridesPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "genre_overrides.yaml"), nil
}

func loadOverrides() (*enrich.Overrides, error) {
	p, err := overridesPath()
	if err != nil {
		return nil, err
	}
	return enrich.LoadOverrides(p)
}

// runGenre gerencia gêneros personalizados por artista e lista artistas sem gênero.
func runGenre(ctx context.Context, args []string, out io.Writer) error {
	const uso = "uso: likedsorter genre list | set \"Artista\" gênero[,gênero] | unset \"Artista\" | missing"
	if len(args) == 0 {
		return errors.New(uso)
	}
	path, err := overridesPath()
	if err != nil {
		return err
	}
	ov, err := loadOverrides()
	if err != nil {
		return err
	}
	save := func() error { return ov.Save(path, fsutil.WriteFileAtomic) }

	switch args[0] {
	case "list":
		if hasJSON(args[1:]) {
			rows := []map[string]string{}
			for _, e := range ov.Entries() {
				rows = append(rows, map[string]string{"artist": e[0], "genres": e[1]})
			}
			return writeJSON(out, rows)
		}
		if ov.Len() == 0 {
			fmt.Fprintf(out, "Nenhum gênero personalizado (arquivo: %s).\n", path)
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ARTISTA\tGÊNEROS")
		for _, e := range ov.Entries() {
			fmt.Fprintf(tw, "%s\t%s\n", e[0], e[1])
		}
		return tw.Flush()

	case "set":
		if len(args) < 3 {
			return errors.New(uso)
		}
		genres := strings.Split(strings.Join(args[2:], ","), ",")
		ov.Set(args[1], genres)
		if !ov.Has(args[1]) {
			return errors.New("informe ao menos um gênero válido")
		}
		if err := save(); err != nil {
			return err
		}
		fmt.Fprintf(out, "Gênero de %q definido. Vale a partir do próximo plan/apply/groups.\n", args[1])
		return nil

	case "unset":
		if len(args) != 2 {
			return errors.New(uso)
		}
		if !ov.Unset(args[1]) {
			return fmt.Errorf("%q não tem gênero personalizado", args[1])
		}
		if err := save(); err != nil {
			return err
		}
		fmt.Fprintf(out, "Removido: %s\n", args[1])
		return nil

	case "missing":
		fl := flag.NewFlagSet("genre missing", flag.ContinueOnError)
		var d dataFlags
		d.register(fl)
		top := fl.Int("top", 30, "quantos artistas mostrar")
		asJSON := fl.Bool("json", false, "saída em JSON")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		l, err := loadData(ctx, &d, true)
		if err != nil {
			return err
		}
		count := map[string]int{}
		for _, t := range l.Sync.Tracks {
			if len(t.Artists) == 0 || len(l.Infos[t.Artists[0].ID].Genres) > 0 {
				continue
			}
			count[t.Artists[0].Name]++
		}
		type row struct {
			name string
			n    int
		}
		var rows []row
		for k, v := range count {
			rows = append(rows, row{k, v})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].name < rows[j].name
		})
		if *asJSON {
			type rowJSON struct {
				Artist string `json:"artist"`
				Tracks int    `json:"tracks"`
			}
			js := []rowJSON{}
			for i, r := range rows {
				if i >= *top {
					break
				}
				js = append(js, rowJSON{r.name, r.n})
			}
			return writeJSON(out, map[string]any{"artists_without_genre": len(rows), "top": js})
		}
		if len(rows) == 0 {
			fmt.Fprintln(out, "Todos os artistas principais têm gênero.")
			return nil
		}
		fmt.Fprintf(out, "%d artistas sem gênero (mais faixas primeiro). Defina com: likedsorter genre set \"Artista\" rock\n\n", len(rows))
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "FAIXAS\tARTISTA")
		for i, r := range rows {
			if i >= *top {
				break
			}
			fmt.Fprintf(tw, "%d\t%s\n", r.n, r.name)
		}
		return tw.Flush()
	}
	return fmt.Errorf("subcomando genre desconhecido %q\n%s", args[0], uso)
}
