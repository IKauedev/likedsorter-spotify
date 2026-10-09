package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/filter"
	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/planner"
	"github.com/ikauedeveloper/likedsorter/internal/report"
	"github.com/ikauedeveloper/likedsorter/internal/state"
)

// planFlags são as flags de planejamento compartilhadas por plan e apply.
type planFlags struct {
	tpl          string
	skipExisting bool
	detail       bool
}

func (p *planFlags) registerTemplate(fs *flag.FlagSet) {
	fs.StringVar(&p.tpl, "name-template", cur.NameTemplate, "nome das playlists; {group} é o nome do grupo")
}

// planned reúne tudo que plan e apply precisam depois de planejar.
type planned struct {
	Filter   filter.Filter
	L        *loaded
	Res      *grouping.Result
	Existing []planner.Existing
	Plan     planner.Plan
	Data     report.Data
	State    *state.State
	Store    *state.Store
}

func stateStore() (*state.Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return &state.Store{Path: filepath.Join(dir, "state.json")}, nil
}

// computePlan lê as curtidas, agrupa, lê as playlists gerenciadas existentes e
// calcula o plano. Nunca escreve no Spotify.
func computePlan(ctx context.Context, d *dataFlags, g *groupFlags, pf *planFlags, ff *filterFlags) (*planned, error) {
	// Validações antes de qualquer chamada de rede.
	if err := planner.ValidateTemplate(pf.tpl); err != nil {
		return nil, err
	}
	opt, _, err := g.options(d.featured)
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
	sstore, err := stateStore()
	if err != nil {
		return nil, err
	}
	st, err := sstore.Load()
	if err != nil {
		return nil, err
	}

	l, err := loadData(ctx, d, needsGenres(g.by) || flt.NeedsGenres())
	if err != nil {
		return nil, err
	}
	tracks := flt.Apply(l.Sync.Tracks, l.Infos)
	if flt.Active() {
		appLogger.Info("filtro aplicado", "filtro", flt.Describe(), "antes", len(l.Sync.Tracks), "depois", len(tracks))
	}
	res, err := grouping.Build(tracks, l.Infos, strat, opt)
	if err != nil {
		return nil, err
	}

	var existing []planner.Existing
	if !pf.skipExisting {
		existing, err = planner.FetchExisting(ctx, l.Client, res.Groups, pf.tpl, st.KnownIDs())
		if err != nil {
			return nil, fmt.Errorf("%w\n(dica: `auth status` mostra os escopos; no `plan` use --skip-existing para planejar sem consultar o Spotify)", err)
		}
	}
	plan := planner.Build(planner.Input{Groups: res.Groups, Existing: existing, NameTemplate: pf.tpl})
	if flt.Active() {
		// Faixas fora do filtro não são "indesejadas": nunca viram remoção.
		plan.IgnoreRemovals()
		plan.Warnings = append(plan.Warnings, "filtro ativo ("+flt.Describe()+"): o plano considera só as faixas filtradas e não calcula remoções")
	}
	if res.Skipped > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d faixa(s) ficaram de fora por pertencerem só a grupos menores que --min-size (--small-groups=skip)", res.Skipped))
	}
	return &planned{
		Filter: flt, L: l, Res: res, Existing: existing, Plan: plan, State: st, Store: sstore,
		Data: report.Data{
			Strategy: g.by, GeneratedAt: time.Now(), Plan: plan, Detail: pf.detail, NoRemote: pf.skipExisting, Filter: flt.Describe(),
			Stats: report.Compute(report.StatsInput{
				Liked: l.Sync.Total, Ignored: l.Sync.Stats, Tracks: tracks, Infos: l.Infos, Result: res,
				GenresLoaded: l.Enabled,
			}),
		},
	}, nil
}

// runPlan calcula o que seria feito (dry-run). Nunca altera nada no Spotify.
func runPlan(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	var d dataFlags
	var g groupFlags
	var pf planFlags
	var ff filterFlags
	d.register(fs)
	g.register(fs)
	ff.register(fs)
	pf.registerTemplate(fs)
	format := fs.String("format", "table", "formato do relatório: table | json | csv | md")
	outFile := fs.String("out", "", "gravar o relatório neste arquivo em vez de imprimir")
	fs.BoolVar(&pf.skipExisting, "skip-existing", false, "não consultar playlists existentes (tudo aparece como \"criar\"; dispensa o escopo playlist-read-private)")
	fs.BoolVar(&pf.detail, "detail", false, "JSON: incluir as URIs a adicionar/remover")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := report.Render(io.Discard, *format, report.Data{}); err != nil { // formato válido?
		return err
	}

	p, err := computePlan(ctx, &d, &g, &pf, &ff)
	if err != nil {
		return err
	}
	if *outFile == "" {
		return report.Render(out, *format, p.Data)
	}
	f, err := os.Create(*outFile)
	if err != nil {
		return err
	}
	if err := report.Render(f, *format, p.Data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Relatório (%s) salvo em %s\n", *format, *outFile)
	return nil
}
