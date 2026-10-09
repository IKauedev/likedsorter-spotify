package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"text/tabwriter"

	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/report"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// runStats mostra estatísticas da biblioteca (somente leitura).
func runStats(ctx context.Context, args []string, out io.Writer) error {
	fl := flag.NewFlagSet("stats", flag.ContinueOnError)
	var d dataFlags
	var ff filterFlags
	d.register(fl)
	ff.register(fl)
	format := fl.String("format", "table", "table | json")
	if err := fl.Parse(args); err != nil {
		return err
	}
	flt, err := ff.build()
	if err != nil {
		return err
	}
	if err := report.RenderLibrary(io.Discard, *format, report.LibraryStats{}); err != nil { // formato válido?
		return err
	}
	l, err := loadData(ctx, &d, true) // gêneros fazem parte das estatísticas
	if err != nil {
		return err
	}
	tracks := flt.Apply(l.Sync.Tracks, l.Infos)
	return report.RenderLibrary(out, *format,
		report.ComputeLibrary(l.Sync.Total, l.Sync.Stats, tracks, l.Infos, l.Enabled, flt.Describe()))
}

// runDedupe lista curtidas duplicadas por ISRC (somente leitura; nunca remove).
func runDedupe(ctx context.Context, args []string, out io.Writer) error {
	fl := flag.NewFlagSet("dedupe", flag.ContinueOnError)
	var d dataFlags
	d.register(fl)
	format := fl.String("format", "table", "table | json")
	outFile := fl.String("out", "", "gravar em arquivo em vez de imprimir")
	toPlaylist := fl.String("to-playlist", "", "adicionar as cópias mais antigas a esta playlist (criada se não existir) para você revisar; não remove nada das curtidas")
	yes := fl.Bool("yes", false, "com --to-playlist: não pedir confirmação")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if err := report.RenderDuplicates(io.Discard, *format, library.DedupeResult{}, 0); err != nil {
		return err
	}
	l, err := loadData(ctx, &d, false)
	if err != nil {
		return err
	}
	res := library.FindDuplicates(l.Sync.Tracks)
	if res.NoISRC == len(l.Sync.Tracks) && len(l.Sync.Tracks) > 0 {
		warnf("Aviso: nenhuma faixa tem ISRC no cache; rode `sync --refresh` ou verifique se a API ainda devolve external_ids.")
	}
	if *toPlaylist != "" {
		var extra []spotify.TrackInfo
		for _, g := range res.Groups {
			for _, t := range g.Tracks[1:] { // a mais recente fica de fora
				names := make([]string, len(t.Artists))
				for i, a := range t.Artists {
					names[i] = a.Name
				}
				extra = append(extra, spotify.TrackInfo{ID: t.ID, URI: t.URI, Name: t.Name, Artists: names, Album: t.Album.Name})
			}
		}
		if len(extra) == 0 {
			fmt.Fprintln(out, "Nenhuma duplicada encontrada.")
			return nil
		}
		if err := report.RenderDuplicates(out, *format, res, len(l.Sync.Tracks)); err != nil {
			return err
		}
		return addToPlaylist(ctx, l.Client, addOptions{To: *toPlaylist, Create: true, Resolved: extra}, terminalUI(out, *yes), out)
	}
	if *outFile == "" {
		return report.RenderDuplicates(out, *format, res, len(l.Sync.Tracks))
	}
	f, err := os.Create(*outFile)
	if err != nil {
		return err
	}
	if err := report.RenderDuplicates(f, *format, res, len(l.Sync.Tracks)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Duplicatas (%d grupos) salvas em %s\n", len(res.Groups), *outFile)
	return nil
}

// runConfig: config show | path | init [--force]
func runConfig(g globals, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: likedsorter config show | get CHAVE | set CHAVE VALOR | unset CHAVE | path | init [--force]")
	}
	path := g.configPath
	if path == "" {
		p, err := config.SettingsPath()
		if err != nil {
			return err
		}
		path = p
	}

	switch args[0] {
	case "path":
		fmt.Fprintln(out, path)
		return nil

	case "get":
		if len(args) != 2 {
			return errors.New("uso: likedsorter config get CHAVE")
		}
		_, entries, err := config.LoadSettings(g.configPath)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Key == args[1] {
				fmt.Fprintf(out, "%s = %s  (origem: %s)\n", e.Key, e.Value, e.Source)
				return nil
			}
		}
		return fmt.Errorf("chave desconhecida %q (veja `likedsorter config show`)", args[1])

	case "set":
		if len(args) != 3 {
			return errors.New("uso: likedsorter config set CHAVE VALOR   (ex.: config set group.min_size 10)")
		}
		if err := config.SetSetting(path, args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s = %s  (gravado em %s)\n", args[1], args[2], path)
		return nil

	case "unset":
		if len(args) != 2 {
			return errors.New("uso: likedsorter config unset CHAVE")
		}
		was, err := config.UnsetSetting(path, args[1])
		if err != nil {
			return err
		}
		if !was {
			fmt.Fprintf(out, "%s já estava no padrão (%s).\n", args[1], config.DefaultValue(args[1]))
			return nil
		}
		fmt.Fprintf(out, "%s voltou ao padrão (%s).\n", args[1], config.DefaultValue(args[1]))
		return nil

	case "show":
		_, entries, err := config.LoadSettings(g.configPath)
		if err != nil {
			return err
		}
		exists := "não existe (usando padrões; `likedsorter config init` cria um modelo)"
		_, statErr := os.Stat(path)
		if statErr == nil {
			exists = "carregado"
		}
		if hasJSON(args) {
			return writeJSON(out, map[string]any{"path": path, "exists": statErr == nil, "entries": entries})
		}
		fmt.Fprintf(out, "Arquivo: %s (%s)\n", path, exists)
		fmt.Fprint(out, "Precedência: flags > ambiente > arquivo > padrões\n\n")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "CHAVE\tVALOR\tORIGEM\tVARIÁVEL DE AMBIENTE")
		for _, e := range entries {
			v := e.Value
			if v == "" {
				v = `""`
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Key, v, e.Source, e.Env)
		}
		return tw.Flush()

	case "init":
		fl := flag.NewFlagSet("config init", flag.ContinueOnError)
		force := fl.Bool("force", false, "sobrescrever se já existir")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil && !*force {
			return fmt.Errorf("%s já existe (use --force para sobrescrever; o arquivo atual será perdido)", path)
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := fsutil.WriteFileAtomic(path, []byte(config.SampleFile())); err != nil {
			return err
		}
		fmt.Fprintf(out, "Modelo criado em %s\n", path)
		return nil
	}
	return fmt.Errorf("subcomando config desconhecido %q (use show, get, set, unset, path ou init)", args[0])
}
