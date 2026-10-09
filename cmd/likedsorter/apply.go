package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/backup"
	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/executor"
	"github.com/ikauedeveloper/likedsorter/internal/planner"
	"github.com/ikauedeveloper/likedsorter/internal/report"
)

// confirm exige --yes ou uma resposta "sim" digitada em um terminal.
func confirm(out io.Writer, question string, yes bool) error {
	if yes {
		return nil
	}
	if !isTerminal(os.Stdin) {
		return errors.New("confirmação necessária: rode em um terminal ou passe --yes")
	}
	fmt.Fprintf(out, "%s Digite \"sim\" para continuar: ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "sim", "s", "yes", "y":
		return nil
	}
	return errors.New("cancelado: nada foi alterado")
}

// runApply cria/atualiza as playlists no Spotify. É a única operação que escreve na conta.
func runApply(ctx context.Context, args []string, out io.Writer) error {
	return withJSON(args, out, func(a []string, text io.Writer) (any, error) { return applyRun(ctx, a, text) })
}

func applyRun(ctx context.Context, args []string, out io.Writer) (any, error) {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	var d dataFlags
	var g groupFlags
	var pf planFlags
	var ff filterFlags
	d.register(fs)
	g.register(fs)
	ff.register(fs)
	pf.registerTemplate(fs)
	mode := fs.String("mode", cur.Mode, "create-only | sync (padrão) | recreate")
	public := fs.Bool("public", cur.Public, "criar playlists públicas")
	private := fs.Bool("private", false, "criar playlists privadas (padrão)")
	allowRemove := fs.Bool("allow-remove", false, "permitir remover faixas das playlists gerenciadas (sem isso nada é removido)")
	yes := fs.Bool("yes", false, "não pedir confirmação")
	dryRun := fs.Bool("dry-run", false, "só mostrar o plano e o que seria feito, sem confirmar nem escrever (igual ao `plan`)")
	maxPlaylists := fs.Int("max-playlists", cur.MaxPlaylists, "trava de segurança: máximo de playlists NOVAS por execução")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *public && *private {
		return nil, errors.New("use --public ou --private, não os dois")
	}

	opt := executor.Options{
		Mode: *mode, AllowRemove: *allowRemove, Public: *public, Strategy: g.by,
		Description: planner.Description(g.by), MaxCreates: *maxPlaylists,
	}
	// Valida modo/flags ANTES de qualquer chamada de rede.
	if err := executor.Validate(planner.Plan{}, opt); err != nil {
		return nil, err
	}
	flt, err := ff.build()
	if err != nil {
		return nil, err
	}
	if flt.Active() && (*allowRemove || *mode == executor.ModeRecreate) {
		// Com filtro, as faixas fora dele pareceriam "indesejadas" e seriam apagadas das playlists.
		return nil, errors.New("filtros (--filter-artist/--filter-genre/--since) não podem ser combinados com --allow-remove nem --mode=recreate: faixas fora do filtro seriam removidas das playlists")
	}

	p, err := computePlan(ctx, &d, &g, &pf, &ff)
	if err != nil {
		return nil, err
	}
	if err := executor.Validate(p.Plan, opt); err != nil { // agora com o tamanho real do plano (--max-playlists)
		return nil, err
	}

	if err := report.Render(out, report.FormatTable, p.Data); err != nil {
		return nil, err
	}
	tot := p.Plan.Totals()
	work := tot.Create > 0 || (opt.Mode != executor.ModeCreateOnly && (tot.Update > 0 || (opt.Mode == executor.ModeRecreate && tot.Keep > 0)))
	if !work {
		fmt.Fprintln(out, "\nNada a fazer: as playlists já estão como o planejado.")
		return map[string]any{"changes": false}, nil
	}
	if p.State.Interrupted() {
		fmt.Fprintf(out, "\nA execução anterior (início %s) foi interrompida; o apply é idempotente e fará só o que ainda falta.\n",
			p.State.LastRun.StartedAt.Local().Format("2006-01-02 15:04"))
	}

	if *dryRun {
		fmt.Fprintln(out, "\n--dry-run: nada foi alterado. Rode sem --dry-run para aplicar.")
		return map[string]any{"dry_run": true, "create": tot.Create, "update": tot.Update, "keep": tot.Keep}, nil
	}

	vis := "privadas"
	if *public {
		vis = "PÚBLICAS"
	}
	fmt.Fprintf(out, "\nModo %s: criar %d playlist(s) %s", opt.Mode, tot.Create, vis)
	if opt.Mode != executor.ModeCreateOnly {
		fmt.Fprintf(out, ", atualizar %d", tot.Update)
	}
	if opt.AllowRemove {
		fmt.Fprint(out, " | remoção de faixas HABILITADA")
	} else {
		fmt.Fprint(out, " | nenhuma faixa será removida")
	}
	fmt.Fprintln(out, " | nenhuma playlist será apagada.")
	if err := confirm(out, "Isto vai ALTERAR sua conta do Spotify.", *yes); err != nil {
		return nil, err
	}

	// Backup das playlists existentes que serão modificadas, antes de qualquer escrita.
	if affected := affectedExisting(p, opt.Mode); len(affected) > 0 {
		dir, err := config.Dir()
		if err != nil {
			return nil, err
		}
		snap, err := backup.FromExisting(affected, time.Now())
		if err != nil {
			return nil, err
		}
		opt.Backup = backup.NewPath(dir, time.Now())
		if err := backup.Save(opt.Backup, snap); err != nil {
			return nil, fmt.Errorf("não consegui salvar o backup; nada foi alterado: %w", err)
		}
		fmt.Fprintf(out, "Backup de %d playlist(s) salvo em %s\n", len(affected), opt.Backup)
	}

	opt.Progress = func(ev executor.Event) {
		switch ev.Phase {
		case "start":
			fmt.Fprintf(out, "[%d/%d] %s: %s ... ", ev.Index, ev.Total, actionPT(ev.Action), ev.Name)
		case "batch":
			if ev.Note != "" {
				fmt.Fprintf(out, "\n    %s\n", ev.Note)
			}
		case "done":
			fmt.Fprintf(out, "ok (+%d)\n", ev.Done)
		}
	}
	res, err := executor.Run(ctx, p.L.Client, p.Plan, p.Existing, p.State, p.Store, opt)
	if res != nil {
		fmt.Fprintf(out, "\nCriadas: %d | atualizadas: %d | recriadas: %d | inalteradas: %d | puladas: %d | +%d faixas, -%d faixas",
			res.Created, res.Updated, res.Recreated, res.Unchanged, res.Skipped, res.Added, res.Removed)
		if res.NotRemoved > 0 {
			fmt.Fprintf(out, " | %d faixa(s) fora do grupo mantida(s) (--allow-remove para removê-las)", res.NotRemoved)
		}
		fmt.Fprintln(out)
	}
	if err != nil {
		return nil, fmt.Errorf("%w\n(a execução foi interrompida; rode o mesmo comando de novo para retomar do ponto em que parou)", err)
	}
	return res, nil
}

func actionPT(a string) string {
	switch a {
	case string(planner.Create):
		return "criando"
	case string(planner.Update):
		return "atualizando"
	}
	return "reescrevendo"
}

// affectedExisting devolve as playlists existentes que o modo vai modificar.
func affectedExisting(p *planned, mode string) []planner.Existing {
	if mode == executor.ModeCreateOnly {
		return nil
	}
	byID := map[string]planner.Existing{}
	for _, e := range p.Existing {
		byID[e.ID] = e
	}
	var out []planner.Existing
	for _, it := range p.Plan.Items {
		e, ok := byID[it.ExistingID]
		if !ok {
			continue
		}
		if it.Action == planner.Update || (it.Action == planner.Keep && mode == executor.ModeRecreate) {
			out = append(out, e)
		}
	}
	return out
}
