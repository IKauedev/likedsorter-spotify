// Package executor aplica um planner.Plan no Spotify de forma idempotente e segura:
//   - só cria playlists e adiciona faixas; remover faixas exige AllowRemove;
//   - nunca apaga playlists; só altera playlists com o marcador/registrado (Managed);
//   - falhas ambíguas (5xx/rede) são resolvidas relendo a playlist antes de repetir,
//     então uma escrita aplicada pela metade não duplica faixas;
//   - o andamento é gravado no estado após cada playlist.
package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/planner"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
	"github.com/ikauedeveloper/likedsorter/internal/state"
)

// Modos de execução.
const (
	ModeCreateOnly = "create-only" // só cria playlists novas (e as popula)
	ModeSync       = "sync"        // cria e adiciona o que falta; remove só com AllowRemove
	ModeRecreate   = "recreate"    // reescreve o conteúdo das gerenciadas, na ordem do grupo
)

const maxRecoveries = 3

// API é o que o executor usa do Spotify (mockável).
type API interface {
	planner.Source
	CreatePlaylist(ctx context.Context, name, description string, public bool) (*spotify.Playlist, error)
	AddItems(ctx context.Context, id string, uris []string) error
	RemoveItems(ctx context.Context, id string, uris []string) error
	ReplaceItems(ctx context.Context, id string, uris []string) error
}

// Event é um aviso de progresso.
type Event struct {
	Index, Total int // posição da playlist entre as que serão processadas
	Name, Action string
	Phase        string // start | batch | done | skip
	Done, Count  int    // faixas já enviadas / total a enviar (Phase batch)
	Note         string
}

// Options configura Run.
type Options struct {
	Mode        string
	AllowRemove bool
	Public      bool   // visibilidade das playlists CRIADAS (as existentes não mudam)
	Description string // descrição das criadas (deve terminar com o marcador)
	Strategy    string
	Backup      string // caminho do backup feito antes (informativo, vai ao estado)
	MaxCreates  int    // trava de segurança: máximo de playlists novas (0 = sem limite)
	BatchSize   int    // padrão 100 (máximo do Spotify)
	Progress    func(Event)
	Now         func() time.Time
}

// Result resume a execução.
type Result struct {
	Created, Updated, Recreated, Unchanged, Skipped int
	Added, Removed                                  int
	NotRemoved                                      int // faixas fora do desejado mantidas por falta de AllowRemove
}

// Validate confere as opções contra o plano ANTES de qualquer escrita.
func Validate(plan planner.Plan, opt Options) error {
	switch opt.Mode {
	case ModeCreateOnly, ModeSync:
	case ModeRecreate:
		if !opt.AllowRemove {
			return errors.New("--mode=recreate reescreve o conteúdo das playlists gerenciadas (removendo o que não pertence ao grupo): exige também --allow-remove")
		}
	default:
		return fmt.Errorf("modo desconhecido %q (use create-only, sync ou recreate)", opt.Mode)
	}
	if opt.BatchSize < 0 || opt.BatchSize > spotify.MaxItemsPerRequest {
		return fmt.Errorf("tamanho de lote deve estar entre 1 e %d", spotify.MaxItemsPerRequest)
	}
	if n := plan.Totals().Create; opt.MaxCreates > 0 && n > opt.MaxCreates {
		return fmt.Errorf("o plano criaria %d playlists, acima do limite de segurança de %d (--max-playlists). Use --min-size/--small-groups para reduzir os grupos ou aumente o limite conscientemente", n, opt.MaxCreates)
	}
	return nil
}

// Run aplica o plano. existing deve conter as playlists carregadas pelo planner.
// Para no primeiro erro (o estado fica salvo; rodar de novo retoma o que falta).
func Run(ctx context.Context, api API, plan planner.Plan, existing []planner.Existing, st *state.State, store *state.Store, opt Options) (*Result, error) {
	if opt.BatchSize == 0 {
		opt.BatchSize = spotify.MaxItemsPerRequest
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if err := Validate(plan, opt); err != nil {
		return nil, err
	}
	byID := map[string]*planner.Existing{}
	for i := range existing {
		byID[existing[i].ID] = &existing[i]
	}

	// Seleciona o que será processado e valida a segurança antes de escrever.
	var todo []planner.Item
	res := &Result{}
	for _, it := range plan.Items {
		switch {
		case it.Action == planner.Orphan:
			res.Skipped++
		case it.Action == planner.Create:
			todo = append(todo, it)
		case opt.Mode == ModeCreateOnly:
			res.Skipped++
		case it.Action == planner.Update || (it.Action == planner.Keep && opt.Mode == ModeRecreate):
			e := byID[it.ExistingID]
			if e == nil || !e.Managed {
				return nil, fmt.Errorf("recusando alterar %q: não é uma playlist gerenciada pelo likedsorter", it.Name)
			}
			todo = append(todo, it)
		default:
			res.Unchanged++
		}
	}

	run := &state.Run{StartedAt: opt.Now(), Strategy: opt.Strategy, Mode: opt.Mode, Backup: opt.Backup}
	for _, it := range todo {
		run.Items = append(run.Items, state.RunItem{Name: it.Name, PlaylistID: it.ExistingID, Action: string(it.Action), Status: state.StatusPending})
	}
	st.LastRun = run
	if err := store.Save(st); err != nil {
		return res, err
	}
	finish := func(err error) (*Result, error) {
		run.FinishedAt = opt.Now()
		run.Completed = err == nil
		if serr := store.Save(st); serr != nil && err == nil {
			err = serr
		}
		return res, err
	}

	for i, it := range todo {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		ri := &run.Items[i]
		ev := Event{Index: i + 1, Total: len(todo), Name: it.Name, Action: string(it.Action)}
		emit(opt, ev, "start", 0, 0, "")

		var err error
		switch {
		case it.Action == planner.Create:
			err = createItem(ctx, api, it, st, store, opt, ri, ev)
			if err == nil {
				res.Created++
			}
		case opt.Mode == ModeRecreate:
			var changed bool
			changed, err = recreateItem(ctx, api, it, byID[it.ExistingID], opt, ri, ev)
			if err == nil {
				if changed {
					res.Recreated++
				} else {
					res.Unchanged++
				}
			}
		default:
			err = updateItem(ctx, api, it, byID[it.ExistingID], opt, ri, ev, res)
			if err == nil {
				res.Updated++
			}
		}
		res.Added += ri.Added
		res.Removed += ri.Removed
		if err != nil {
			ri.Status, ri.Error = state.StatusFailed, err.Error()
			return finish(fmt.Errorf("%q: %w", it.Name, err))
		}
		ri.Status = state.StatusDone
		emit(opt, ev, "done", ri.Added, ri.Added, "")
		if serr := store.Save(st); serr != nil {
			return finish(serr)
		}
	}
	return finish(nil)
}

func emit(opt Options, ev Event, phase string, done, count int, note string) {
	if opt.Progress != nil {
		ev.Phase, ev.Done, ev.Count, ev.Note = phase, done, count, note
		opt.Progress(ev)
	}
}

func createItem(ctx context.Context, api API, it planner.Item, st *state.State, store *state.Store, opt Options, ri *state.RunItem, ev Event) error {
	pl, err := api.CreatePlaylist(ctx, it.Name, opt.Description, opt.Public)
	if err != nil {
		if spotify.IsTransient(err) {
			return fmt.Errorf("a criação pode ter sido aplicada apesar do erro (%w); rode novamente: playlists já criadas são reconhecidas pelo marcador e reaproveitadas", err)
		}
		return err
	}
	// Registra ANTES de popular: se algo falhar daqui em diante, a playlist é reconhecida.
	st.Playlists[pl.ID] = state.PlaylistRecord{Name: it.Name, Key: it.Key, CreatedAt: opt.Now()}
	ri.PlaylistID = pl.ID
	if err := store.Save(st); err != nil {
		return err
	}
	n, err := addAll(ctx, api, pl.ID, it.Name, it.Add, opt, ev)
	ri.Added = n
	return err
}

func updateItem(ctx context.Context, api API, it planner.Item, e *planner.Existing, opt Options, ri *state.RunItem, ev Event, res *Result) error {
	n, err := addAll(ctx, api, it.ExistingID, it.Name, it.Add, opt, ev)
	ri.Added = n
	if err != nil {
		return err
	}
	if len(it.Remove) == 0 {
		return nil
	}
	if !opt.AllowRemove {
		res.NotRemoved += len(it.Remove)
		emit(opt, ev, "batch", 0, 0, fmt.Sprintf("%d faixa(s) fora do grupo mantida(s) (use --allow-remove para removê-las)", len(it.Remove)))
		return nil
	}
	n, err = removeAll(ctx, api, it.ExistingID, it.Name, it.Remove, opt, ev)
	ri.Removed = n
	return err
}

// recreateItem reescreve o conteúdo na ordem do grupo. Não faz nada se já é idêntico.
func recreateItem(ctx context.Context, api API, it planner.Item, e *planner.Existing, opt Options, ri *state.RunItem, ev Event) (bool, error) {
	current := make([]string, len(e.Items))
	for i, m := range e.Items {
		current[i] = m.URI
	}
	if slices.Equal(current, it.DesiredURIs) {
		return false, nil
	}
	want := make(map[string]bool, len(it.DesiredURIs))
	for _, u := range it.DesiredURIs {
		want[u] = true
	}
	for _, u := range current {
		if !want[u] {
			ri.Removed++
		}
	}
	n, err := Rebuild(ctx, api, it.ExistingID, it.Name, it.DesiredURIs, opt.BatchSize, func(done, total int) {
		emit(opt, ev, "batch", done, total, "")
	})
	ri.Added = n
	return true, err
}

// Rebuild substitui todo o conteúdo da playlist por uris (na ordem): PUT com o
// primeiro lote (que também esvazia o resto) e POST dos demais. Reutilizado por restore.
func Rebuild(ctx context.Context, api API, id, name string, uris []string, batch int, progress func(done, total int)) (int, error) {
	if batch <= 0 || batch > spotify.MaxItemsPerRequest {
		batch = spotify.MaxItemsPerRequest
	}
	first := uris[:min(batch, len(uris))]
	// PUT é idempotente (mesmo conteúdo): pode ser repetido em falha ambígua.
	var err error
	for attempt := 0; attempt <= maxRecoveries; attempt++ {
		if err = api.ReplaceItems(ctx, id, first); err == nil || !spotify.IsTransient(err) {
			break
		}
	}
	if err != nil {
		return 0, err
	}
	if progress != nil {
		progress(len(first), len(uris))
	}
	n, err := addAll(ctx, api, id, name, uris[len(first):], Options{BatchSize: batch, Progress: nil}, Event{})
	if progress != nil && err == nil {
		progress(len(uris), len(uris))
	}
	return len(first) + n, err
}

// addAll envia uris em lotes. Em falha ambígua relê a playlist e continua só com o que falta.
func addAll(ctx context.Context, api API, id, name string, uris []string, opt Options, ev Event) (int, error) {
	return batched(ctx, api, id, name, uris, opt, ev, api.AddItems, func(have map[string]bool, rem []string) []string {
		return slices.DeleteFunc(slices.Clone(rem), func(u string) bool { return have[u] }) // já presentes
	})
}

// removeAll remove uris em lotes, com a mesma recuperação.
func removeAll(ctx context.Context, api API, id, name string, uris []string, opt Options, ev Event) (int, error) {
	return batched(ctx, api, id, name, uris, opt, ev, api.RemoveItems, func(have map[string]bool, rem []string) []string {
		return slices.DeleteFunc(slices.Clone(rem), func(u string) bool { return !have[u] }) // já ausentes
	})
}

type call func(ctx context.Context, id string, uris []string) error

func batched(ctx context.Context, api API, id, name string, uris []string, opt Options, ev Event, do call,
	pending func(have map[string]bool, remaining []string) []string) (int, error) {
	size := opt.BatchSize
	if size <= 0 || size > spotify.MaxItemsPerRequest {
		size = spotify.MaxItemsPerRequest
	}
	remaining := slices.Clone(uris)
	recoveries := 0
	for len(remaining) > 0 {
		if err := ctx.Err(); err != nil {
			return len(uris) - len(remaining), err
		}
		batch := remaining[:min(size, len(remaining))]
		err := do(ctx, id, batch)
		if err == nil {
			remaining = remaining[len(batch):]
			emit(opt, ev, "batch", len(uris)-len(remaining), len(uris), "")
			continue
		}
		if !spotify.IsTransient(err) || recoveries >= maxRecoveries {
			return len(uris) - len(remaining), err
		}
		recoveries++
		ex := planner.Existing{ID: id, Name: name}
		if lerr := planner.LoadItems(ctx, api, &ex); lerr != nil {
			return len(uris) - len(remaining), fmt.Errorf("%w (e não foi possível reler a playlist: %w)", err, lerr)
		}
		have := make(map[string]bool, len(ex.Items))
		for _, m := range ex.Items {
			have[m.URI] = true
		}
		remaining = pending(have, remaining)
	}
	return len(uris), nil
}
