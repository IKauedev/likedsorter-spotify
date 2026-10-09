package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/plays"
)

// needsGenres diz se a estratégia depende de gêneros (evita chamadas externas à toa).
func needsGenres(by string) bool {
	return strings.Contains(by, "genre") || strings.Contains(by, "language") || strings.Contains(by, "rules")
}

// resolveMacroMap: --macro-map > <config dir>/macro_genres.yaml > mapa embutido.
func resolveMacroMap(path string) (*grouping.MacroMap, string, error) {
	if path != "" {
		m, err := grouping.LoadMacroMap(path)
		return m, path, err
	}
	if dir, err := config.Dir(); err == nil {
		p := filepath.Join(dir, "macro_genres.yaml")
		if _, err := os.Stat(p); err == nil {
			m, err := grouping.LoadMacroMap(p)
			return m, p, err
		}
	}
	return grouping.DefaultMacroMap(), "embutido", nil
}

// resolveRules: --rules > <config dir>/rules.yaml. Só é exigido quando --by usa "rules".
func resolveRules(path string, by string) (*grouping.RuleSet, error) {
	if !strings.Contains(by, "rules") {
		return nil, nil
	}
	if path == "" {
		dir, err := config.Dir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "rules.yaml")
	}
	rs, err := grouping.LoadRules(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("arquivo de regras não encontrado: %s (crie com `likedsorter rules init`)", path)
	}
	return rs, err
}

// groupFlags são as opções de agrupamento (reaproveitadas por plan/apply).
type groupFlags struct {
	by          string
	minSize     int
	smallGroups string
	maxSize     int
	multiGenre  bool
	sortBy      string
	macroMap    string
	rules       string
	splitOver   int
	listenTop   int
	listenDays  int
	forgotten   int
	splitBy     string
}

func (g *groupFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&g.by, "by", cur.By, "estratégia: artist, genre, macro-genre, decade, year, added-period, rules, language (experimental) ou combinação com +")
	fs.IntVar(&g.minSize, "min-size", cur.MinSize, "grupos menores que N vão para \"Outros\" (ou são ignorados, veja --small-groups)")
	fs.StringVar(&g.smallGroups, "small-groups", cur.SmallGroups, "destino dos grupos pequenos: other | skip")
	fs.IntVar(&g.maxSize, "max-size", cur.MaxSize, "divide grupos maiores que N em partes (máx. e padrão: 10000)")
	fs.BoolVar(&g.multiGenre, "multi-genre", cur.MultiGenre, "permitir que a faixa entre em mais de um grupo de gênero")
	fs.StringVar(&g.sortBy, "sort", cur.Sort, "ordem dentro do grupo: added | release | title")
	fs.IntVar(&g.listenTop, "listen-top", 0, "--by=listening: tamanho das playlists \"Mais ouvidas\" (padrão 50)")
	fs.IntVar(&g.listenDays, "listen-days", 0, "--by=listening: janela dos \"Mais ouvidas\" recentes, em dias (padrão 30)")
	fs.IntVar(&g.forgotten, "forgotten-days", 0, "--by=listening: sem tocar há N dias = \"Esquecidas\" (padrão 180)")
	fs.IntVar(&g.splitOver, "split-over", cur.SplitOver, "divide grupos com mais de N faixas por década/ano de lançamento (0 = desligado)")
	fs.StringVar(&g.splitBy, "split-by", cur.SplitBy, "como dividir os grupos grandes: decade | year")
	fs.StringVar(&g.rules, "rules", "", "YAML com suas regras para --by=rules (padrão: <config>/rules.yaml)")
	fs.StringVar(&g.macroMap, "macro-map", cur.MacroMap, "YAML com o mapa de macro-gêneros (padrão: <config>/macro_genres.yaml ou o embutido)")
}

func (g *groupFlags) options(featured bool) (grouping.Options, string, error) {
	mm, src, err := resolveMacroMap(g.macroMap)
	if err != nil {
		return grouping.Options{}, "", err
	}
	rules, err := resolveRules(g.rules, g.by)
	if err != nil {
		return grouping.Options{}, "", err
	}
	var playStats grouping.PlayStats
	if strings.Contains(g.by, "listening") {
		path, err := playsPath()
		if err != nil {
			return grouping.Options{}, "", err
		}
		data, err := plays.Load(path)
		if err != nil {
			return grouping.Options{}, "", err
		}
		playStats = data.Stats()
	}
	opt := grouping.Options{
		Rules: rules,
		Plays: playStats, ListenTop: g.listenTop, ListenDays: g.listenDays, ForgottenDays: g.forgotten,
		SplitOver: g.splitOver, SplitBy: g.splitBy,
		MinSize: g.minSize, SmallGroups: g.smallGroups, MaxSize: g.maxSize,
		MultiGenre: g.multiGenre, IncludeFeatured: featured, Sort: g.sortBy, MacroMap: mm,
	}
	if err := opt.Normalize(); err != nil { // falha cedo, antes de qualquer chamada de rede
		return grouping.Options{}, "", err
	}
	return opt, src, nil
}

// runGroups mostra como as curtidas ficariam agrupadas (somente leitura).
// O comando `plan` (Fase 6) usará o mesmo agrupamento para comparar com as playlists existentes.
func runGroups(ctx context.Context, args []string, out io.Writer) error {
	return withJSON(args, out, func(a []string, text io.Writer) (any, error) { return groupsRun(ctx, a, text) })
}

func groupsRun(ctx context.Context, args []string, out io.Writer) (any, error) {
	fs := flag.NewFlagSet("groups", flag.ContinueOnError)
	var d dataFlags
	var g groupFlags
	var ff filterFlags
	d.register(fs)
	g.register(fs)
	ff.register(fs)
	examples := fs.Int("examples", 0, "mostrar N faixas de exemplo por grupo")
	dump := fs.Bool("dump-macro-map", false, "imprimir o mapa de macro-gêneros embutido (para você editar) e sair")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *dump {
		_, err := out.Write(grouping.DefaultMacroYAML())
		return nil, err
	}

	opt, mapSrc, err := g.options(d.featured)
	if err != nil {
		return nil, err
	}
	flt, err := ff.build()
	if err != nil {
		return nil, err
	}
	strat, err := grouping.New(g.by, opt)
	if err != nil {
		return nil, err
	}
	if e, ok := strat.(grouping.Experimental); ok && e.Experimental() {
		fmt.Fprintln(os.Stderr, "Aviso: a estratégia language é EXPERIMENTAL (heurística simples; muitos títulos ficam \"Indefinido\").")
	}

	l, err := loadData(ctx, &d, needsGenres(g.by) || flt.NeedsGenres())
	if err != nil {
		return nil, err
	}
	tracks := flt.Apply(l.Sync.Tracks, l.Infos)
	if flt.Active() {
		fmt.Fprintf(out, "Filtro ativo: %s (%d de %d faixas)\n", flt.Describe(), len(tracks), len(l.Sync.Tracks))
		fmt.Fprintln(out)
	}
	res, err := grouping.Build(tracks, l.Infos, strat, opt)
	if err != nil {
		return nil, err
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FAIXAS\tGRUPO")
	for _, gr := range res.Groups {
		fmt.Fprintf(tw, "%d\t%s\n", len(gr.Tracks), gr.Name)
		for i, t := range gr.Tracks {
			if i >= *examples {
				break
			}
			fmt.Fprintf(tw, "\t    %s: %s\n", artistNames(t), t.Name)
		}
	}
	if err := tw.Flush(); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "\n%d grupos | %d de %d faixas atribuídas", len(res.Groups), res.Assigned, res.Total)
	if res.Skipped > 0 {
		fmt.Fprintf(out, " | %d ignoradas (grupos pequenos)", res.Skipped)
	}
	fmt.Fprintln(out)
	if strings.Contains(g.by, "macro-genre") {
		fmt.Fprintln(out, "Mapa de macro-gêneros:", mapSrc)
	}
	if !l.Enabled && needsGenres(g.by) {
		fmt.Fprintln(out, "Aviso: --no-enrich: gêneros não carregados; todas as faixas caem em \"Sem gênero\".")
	}
	type groupJSON struct {
		Name   string `json:"name"`
		Key    string `json:"key"`
		Tracks int    `json:"tracks"`
	}
	gj := make([]groupJSON, len(res.Groups))
	for i, gr := range res.Groups {
		gj[i] = groupJSON{gr.Name, gr.Key, len(gr.Tracks)}
	}
	return map[string]any{"groups": gj, "assigned": res.Assigned, "total": res.Total, "skipped": res.Skipped}, nil
}
