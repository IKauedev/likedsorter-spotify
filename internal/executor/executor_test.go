package executor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/planner"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
	"github.com/ikauedeveloper/likedsorter/internal/state"
	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

// ---------------------------------------------------------------- fake API

type fakePlaylist struct {
	id, name, desc string
	public         bool
	items          []string
}

type fakeAPI struct {
	pls     map[string]*fakePlaylist
	order   []string
	next    int
	calls   map[string]int
	batches []int // tamanho de cada AddItems
	// fault: chamado antes de cada escrita; devolve (erro, aplicar). aplicar=true simula
	// "o servidor aplicou mas a resposta se perdeu" (falha ambígua).
	fault func(op string, n int) (error, bool)
}

func newFake() *fakeAPI { return &fakeAPI{pls: map[string]*fakePlaylist{}, calls: map[string]int{}} }

func (f *fakeAPI) hook(op string) (error, bool) {
	f.calls[op]++
	if f.fault != nil {
		return f.fault(op, f.calls[op])
	}
	return nil, true
}

func (f *fakeAPI) CurrentUserID(context.Context) (string, error) { return "me", nil }
func (f *fakeAPI) MyPlaylists(_ context.Context, off, lim int) (*spotify.PlaylistPage, error) {
	var all []spotify.Playlist
	for _, id := range f.order {
		p := f.pls[id]
		all = append(all, spotify.Playlist{ID: p.id, Name: p.name, Description: p.desc, OwnerID: "me", Public: p.public, Total: len(p.items)})
	}
	end := min(off+lim, len(all))
	return &spotify.PlaylistPage{Items: all[off:end], Total: len(all), HasNext: end < len(all)}, nil
}
func (f *fakeAPI) PlaylistItems(_ context.Context, id string, off, lim int) (*spotify.PlaylistItemsPage, error) {
	p := f.pls[id]
	if p == nil {
		return nil, &spotify.APIError{Status: 404}
	}
	end := min(off+lim, len(p.items))
	var out []spotify.PlaylistItem
	for _, u := range p.items[off:end] {
		out = append(out, spotify.PlaylistItem{URI: u, ID: strings.TrimPrefix(u, "spotify:track:"), Type: "track"})
	}
	return &spotify.PlaylistItemsPage{Items: out, Total: len(p.items), HasNext: end < len(p.items)}, nil
}
func (f *fakeAPI) CreatePlaylist(_ context.Context, name, desc string, public bool) (*spotify.Playlist, error) {
	err, apply := f.hook("create")
	if apply {
		f.next++
		id := fmt.Sprintf("pl%d", f.next)
		f.pls[id] = &fakePlaylist{id: id, name: name, desc: desc, public: public}
		f.order = append(f.order, id)
		if err == nil {
			return &spotify.Playlist{ID: id, Name: name}, nil
		}
	}
	return nil, err
}
func (f *fakeAPI) AddItems(_ context.Context, id string, uris []string) error {
	if len(uris) > 100 {
		return errors.New("lote > 100")
	}
	f.batches = append(f.batches, len(uris))
	err, apply := f.hook("add")
	if apply {
		f.pls[id].items = append(f.pls[id].items, uris...)
	}
	return err
}
func (f *fakeAPI) RemoveItems(_ context.Context, id string, uris []string) error {
	err, apply := f.hook("remove")
	if apply {
		p := f.pls[id]
		p.items = slices.DeleteFunc(p.items, func(u string) bool { return slices.Contains(uris, u) })
	}
	return err
}
func (f *fakeAPI) ReplaceItems(_ context.Context, id string, uris []string) error {
	err, apply := f.hook("replace")
	if apply {
		f.pls[id].items = slices.Clone(uris)
	}
	return err
}

// ------------------------------------------------------------------ helpers

func grp(name string, n int) grouping.Group {
	g := grouping.Group{Key: name, Name: name, Part: 1, Parts: 1}
	for i := 0; i < n; i++ {
		g.Tracks = append(g.Tracks, spotify.SavedTrack{ID: fmt.Sprintf("%s-%03d", strings.ToLower(name), i)})
	}
	return g
}

func uri(g grouping.Group, i int) string { return "spotify:track:" + g.Tracks[i].ID }

type env struct {
	api   *fakeAPI
	st    *state.State
	store *state.Store
}

func newEnv(t *testing.T) *env {
	return &env{api: newFake(), st: state.New(), store: &state.Store{Path: filepath.Join(testutil.TempDir(t), "state.json")}}
}

var opts = Options{Mode: ModeSync, Description: planner.Description("test"), Strategy: "test"}

// apply planeja contra o estado atual do fake e executa.
func (e *env) apply(t *testing.T, groups []grouping.Group, o Options) (*Result, error) {
	t.Helper()
	ctx := context.Background()
	ex, err := planner.FetchExisting(ctx, e.api, groups, "", e.st.KnownIDs())
	if err != nil {
		t.Fatal(err)
	}
	plan := planner.Build(planner.Input{Groups: groups, Existing: ex})
	return Run(ctx, e.api, plan, ex, e.st, e.store, o)
}

// ------------------------------------------------------------------- testes

func TestCreateThenSecondRunIsNoop(t *testing.T) {
	e := newEnv(t)
	groups := []grouping.Group{grp("Rock", 250), grp("Jazz", 3)}

	res, err := e.apply(t, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 || res.Added != 253 {
		t.Errorf("1ª: %+v", res)
	}
	if !slices.Equal(e.api.batches, []int{100, 100, 50, 3}) { // lotes de 100
		t.Errorf("lotes: %v", e.api.batches)
	}
	for _, p := range e.api.pls {
		if !strings.HasSuffix(p.desc, planner.Marker) || p.public {
			t.Errorf("%s: descrição=%q public=%v", p.name, p.desc, p.public)
		}
	}
	if len(e.st.Playlists) != 2 || !e.st.LastRun.Completed {
		t.Errorf("estado: %+v", e.st)
	}

	// segunda execução: nada a fazer, sem duplicar playlists nem faixas
	writes := e.api.calls["create"] + e.api.calls["add"] + e.api.calls["remove"] + e.api.calls["replace"]
	res, err = e.apply(t, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created+res.Updated+res.Recreated != 0 || res.Unchanged != 2 || res.Added != 0 {
		t.Errorf("2ª: %+v", res)
	}
	if w := e.api.calls["create"] + e.api.calls["add"] + e.api.calls["remove"] + e.api.calls["replace"]; w != writes {
		t.Errorf("2ª execução fez %d escritas", w-writes)
	}
	if len(e.api.pls) != 2 || len(e.api.pls["pl1"].items) != 250 {
		t.Error("duplicou playlists ou faixas")
	}
}

func TestSyncAddsNewTracksAndGatesRemoval(t *testing.T) {
	e := newEnv(t)
	rock := grp("Rock", 5)
	if _, err := e.apply(t, []grouping.Group{rock}, opts); err != nil {
		t.Fatal(err)
	}
	// usuário adicionou uma faixa manualmente; e o grupo ganhou uma nova
	pl := e.api.pls["pl1"]
	pl.items = append(pl.items, "spotify:track:manual")
	bigger := grp("Rock", 6)

	res, err := e.apply(t, []grouping.Group{bigger}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != 1 || res.Added != 1 || res.Removed != 0 || res.NotRemoved != 1 {
		t.Errorf("sem --allow-remove: %+v", res)
	}
	if !slices.Contains(pl.items, "spotify:track:manual") {
		t.Error("removeu sem permissão")
	}

	o := opts
	o.AllowRemove = true
	res, err = e.apply(t, []grouping.Group{bigger}, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || slices.Contains(pl.items, "spotify:track:manual") || len(pl.items) != 6 {
		t.Errorf("com --allow-remove: %+v items=%d", res, len(pl.items))
	}
}

func TestCreateOnlyLeavesExistingAlone(t *testing.T) {
	e := newEnv(t)
	if _, err := e.apply(t, []grouping.Group{grp("Rock", 2)}, opts); err != nil {
		t.Fatal(err)
	}
	o := opts
	o.Mode = ModeCreateOnly
	res, err := e.apply(t, []grouping.Group{grp("Rock", 4), grp("Pop", 1)}, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.Skipped != 1 || len(e.api.pls["pl1"].items) != 2 {
		t.Errorf("%+v rock=%d", res, len(e.api.pls["pl1"].items))
	}
}

func TestRecreateRewritesInOrderAndRequiresAllowRemove(t *testing.T) {
	e := newEnv(t)
	rock := grp("Rock", 150)
	if _, err := e.apply(t, []grouping.Group{rock}, opts); err != nil {
		t.Fatal(err)
	}
	pl := e.api.pls["pl1"]
	slices.Reverse(pl.items)                          // ordem trocada
	pl.items = append(pl.items, "spotify:track:lixo") // e uma intrusa

	o := opts
	o.Mode = ModeRecreate
	if _, err := e.apply(t, []grouping.Group{rock}, o); err == nil || !strings.Contains(err.Error(), "--allow-remove") {
		t.Fatalf("recreate sem --allow-remove deveria falhar: %v", err)
	}
	o.AllowRemove = true
	res, err := e.apply(t, []grouping.Group{rock}, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Recreated != 1 || res.Removed != 1 {
		t.Errorf("%+v", res)
	}
	want := make([]string, 150)
	for i := range want {
		want[i] = uri(rock, i)
	}
	if !slices.Equal(pl.items, want) {
		t.Error("conteúdo/ordem não restaurados")
	}
	// rodar de novo: já idêntico → nada
	before := e.api.calls["replace"]
	if res, _ = e.apply(t, []grouping.Group{rock}, o); res.Recreated != 0 || e.api.calls["replace"] != before {
		t.Errorf("recreate idempotente: %+v", res)
	}
}

func TestAmbiguousFailureDoesNotDuplicate(t *testing.T) {
	e := newEnv(t)
	// o 2º AddItems é APLICADO no servidor, mas a resposta falha com 502
	e.api.fault = func(op string, n int) (error, bool) {
		if op == "add" && n == 2 {
			return &spotify.APIError{Status: 502, Message: "bad gateway"}, true
		}
		return nil, true
	}
	g := grp("Rock", 350)
	res, err := e.apply(t, []grouping.Group{g}, opts)
	if err != nil {
		t.Fatal(err)
	}
	items := e.api.pls["pl1"].items
	if len(items) != 350 || res.Added != 350 {
		t.Fatalf("itens=%d added=%d", len(items), res.Added)
	}
	seen := map[string]bool{}
	for _, u := range items {
		if seen[u] {
			t.Fatalf("duplicou %s", u)
		}
		seen[u] = true
	}
}

func TestAmbiguousFailureNotApplied(t *testing.T) {
	e := newEnv(t)
	e.api.fault = func(op string, n int) (error, bool) {
		if op == "add" && n == 1 {
			return &spotify.APIError{Status: 503}, false // não aplicou
		}
		return nil, true
	}
	if _, err := e.apply(t, []grouping.Group{grp("Rock", 120)}, opts); err != nil {
		t.Fatal(err)
	}
	if n := len(e.api.pls["pl1"].items); n != 120 {
		t.Errorf("itens=%d", n)
	}
}

func TestHardErrorStopsAndResumes(t *testing.T) {
	e := newEnv(t)
	groups := []grouping.Group{grp("Rock", 150), grp("Jazz", 10)}
	e.api.fault = func(op string, n int) (error, bool) {
		if op == "add" && n == 2 { // 2º lote do Rock: 403 definitivo
			return &spotify.APIError{Status: 403, Message: "forbidden"}, false
		}
		return nil, true
	}
	_, err := e.apply(t, groups, opts)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("esperava 403: %v", err)
	}
	if e.st.LastRun.Completed || !e.st.Interrupted() || e.st.LastRun.Items[0].Status != state.StatusFailed {
		t.Errorf("estado deveria mostrar interrupção: %+v", e.st.LastRun)
	}
	if len(e.api.pls) != 1 || len(e.api.pls["pl1"].items) != 100 {
		t.Fatalf("parou no meio: pls=%d items=%d", len(e.api.pls), len(e.api.pls["pl1"].items))
	}

	// retoma: falha some; completa Rock (sem recriar) e cria Jazz
	e.api.fault = nil
	res, err := e.apply(t, groups, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.api.pls) != 2 || len(e.api.pls["pl1"].items) != 150 || res.Created != 1 || res.Updated != 1 {
		t.Errorf("retomada: pls=%d rock=%d %+v", len(e.api.pls), len(e.api.pls["pl1"].items), res)
	}
	if !e.st.LastRun.Completed {
		t.Error("execução deveria estar completa")
	}
}

func TestAmbiguousCreateTellsUserToRerunAndNoDuplicate(t *testing.T) {
	e := newEnv(t)
	e.api.fault = func(op string, n int) (error, bool) {
		if op == "create" && n == 1 {
			return &spotify.APIError{Status: 500}, true // criada, mas resposta perdida
		}
		return nil, true
	}
	g := []grouping.Group{grp("Rock", 3)}
	if _, err := e.apply(t, g, opts); err == nil || !strings.Contains(err.Error(), "rode novamente") {
		t.Fatalf("%v", err)
	}
	e.api.fault = nil
	if _, err := e.apply(t, g, opts); err != nil { // reconhecida pelo marcador, não recriada
		t.Fatal(err)
	}
	if len(e.api.pls) != 1 || len(e.api.pls["pl1"].items) != 3 {
		t.Errorf("duplicou playlist: %d", len(e.api.pls))
	}
}

func TestRefusesUnmanagedAndOrphansUntouched(t *testing.T) {
	e := newEnv(t)
	e.api.pls["x"] = &fakePlaylist{id: "x", name: "Curtidas • Rock", desc: "minha", items: []string{"spotify:track:keep"}}
	e.api.order = []string{"x"}
	e.api.pls["o"] = &fakePlaylist{id: "o", name: "Curtidas • Velha", desc: planner.Marker, items: []string{"spotify:track:old"}}
	e.api.order = append(e.api.order, "o")

	res, err := e.apply(t, []grouping.Group{grp("Rock", 2)}, opts)
	if err != nil {
		t.Fatal(err)
	}
	// "Rock" sem marcador não é tocada: cria outra; a órfã gerenciada fica intacta
	if res.Created != 1 || res.Skipped != 1 || len(e.api.pls["x"].items) != 1 || len(e.api.pls["o"].items) != 1 {
		t.Errorf("%+v", res)
	}

	// defesa em profundidade: plano forjado apontando para playlist não gerenciada
	plan := planner.Plan{Items: []planner.Item{{Action: planner.Update, Name: "Curtidas • Rock", ExistingID: "x", Add: []string{"spotify:track:z"}}}}
	ex := []planner.Existing{{ID: "x", Name: "Curtidas • Rock", Managed: false}}
	if _, err := Run(context.Background(), e.api, plan, ex, e.st, e.store, opts); err == nil || !strings.Contains(err.Error(), "não é uma playlist gerenciada") {
		t.Errorf("deveria recusar: %v", err)
	}
	if len(e.api.pls["x"].items) != 1 {
		t.Error("alterou playlist não gerenciada")
	}
}

func TestValidateGuards(t *testing.T) {
	plan := planner.Plan{Items: []planner.Item{{Action: planner.Create}, {Action: planner.Create}, {Action: planner.Create}}}
	if err := Validate(plan, Options{Mode: ModeSync, MaxCreates: 2}); err == nil || !strings.Contains(err.Error(), "limite de segurança") {
		t.Errorf("max creates: %v", err)
	}
	if Validate(plan, Options{Mode: ModeSync, MaxCreates: 3}) != nil {
		t.Error("3 ≤ 3 deveria passar")
	}
	if Validate(plan, Options{Mode: "apagar"}) == nil || Validate(plan, Options{Mode: ModeSync, BatchSize: 101}) == nil {
		t.Error("modo/lote inválidos")
	}
	// nada é escrito quando a validação falha
	e := newEnv(t)
	if _, err := Run(context.Background(), e.api, plan, nil, e.st, e.store, Options{Mode: ModeSync, MaxCreates: 1}); err == nil {
		t.Fatal("deveria falhar")
	}
	if e.api.calls["create"] != 0 {
		t.Error("escreveu apesar da validação")
	}
}

func TestPublicOptionAndCancel(t *testing.T) {
	e := newEnv(t)
	o := opts
	o.Public = true
	if _, err := e.apply(t, []grouping.Group{grp("Rock", 1)}, o); err != nil {
		t.Fatal(err)
	}
	if !e.api.pls["pl1"].public {
		t.Error("--public ignorado")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := planner.Build(planner.Input{Groups: []grouping.Group{grp("Pop", 1)}})
	_, err := Run(ctx, e.api, plan, nil, e.st, e.store, opts)
	if !errors.Is(err, context.Canceled) || len(e.api.pls) != 1 {
		t.Errorf("cancelado: err=%v pls=%d", err, len(e.api.pls))
	}
}

func TestProgressEvents(t *testing.T) {
	e := newEnv(t)
	var phases []string
	o := opts
	o.Progress = func(ev Event) { phases = append(phases, ev.Phase) }
	if _, err := e.apply(t, []grouping.Group{grp("Rock", 150)}, o); err != nil {
		t.Fatal(err)
	}
	if strings.Join(phases, ",") != "start,batch,batch,done" {
		t.Errorf("%v", phases)
	}
}

func TestBatchSizes(t *testing.T) {
	mk := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("spotify:track:%04d", i)
		}
		return out
	}
	tests := []struct {
		name  string
		n     int
		size  int // 0 = padrão (100)
		want  []int
		added int
	}{
		{"vazio", 0, 0, nil, 0},
		{"um", 1, 0, []int{1}, 1},
		{"99", 99, 0, []int{99}, 99},
		{"100 exato", 100, 0, []int{100}, 100},
		{"101", 101, 0, []int{100, 1}, 101},
		{"250", 250, 0, []int{100, 100, 50}, 250},
		{"lote 30", 100, 30, []int{30, 30, 30, 10}, 100},
		{"lote 1", 3, 1, []int{1, 1, 1}, 3},
	}
	for _, tt := range tests {
		api := newFake()
		api.pls["p"] = &fakePlaylist{id: "p", name: "P"}
		got, err := addAll(context.Background(), api, "p", "P", mk(tt.n), Options{BatchSize: tt.size}, Event{})
		if err != nil || got != tt.added || !slices.Equal(api.batches, tt.want) || len(api.pls["p"].items) != tt.n {
			t.Errorf("%s: added=%d lotes=%v err=%v", tt.name, got, api.batches, err)
		}
	}
	// tamanho inválido cai no máximo permitido (100), nunca acima
	api := newFake()
	api.pls["p"] = &fakePlaylist{id: "p"}
	if _, err := addAll(context.Background(), api, "p", "P", mk(150), Options{BatchSize: 500}, Event{}); err != nil || !slices.Equal(api.batches, []int{100, 50}) {
		t.Errorf("lote acima do máximo: %v %v", api.batches, err)
	}
}
