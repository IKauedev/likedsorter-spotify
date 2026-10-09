package library

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/cache"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// lib monta n curtidas; a de índice 0 é a mais recente (added_at decrescente).
func lib(n int) []spotify.SavedTrack {
	out := make([]spotify.SavedTrack, n)
	for i := range out {
		out[i] = spotify.SavedTrack{ID: fmt.Sprintf("id%d", i), Name: fmt.Sprintf("t%d", i), AddedAt: t0.Add(-time.Duration(i) * time.Hour)}
	}
	return out
}

func newStore(t *testing.T) *cache.Store { return &cache.Store{Dir: testutil.TempDir(t)} }

func syncOpts(now time.Time) SyncOptions {
	return SyncOptions{TTL: 24 * time.Hour, Now: func() time.Time { return now }}
}

func TestSyncFirstRunIsFullThenIncrementalAddsOnlyNew(t *testing.T) {
	store := newStore(t)
	src := &fakeSource{items: lib(120), failAt: -1}

	r1, err := Sync(context.Background(), src, store, syncOpts(t0))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Mode != "full" || r1.Reason != "sem cache" || len(r1.Tracks) != 120 {
		t.Fatalf("1ª execução: %+v", r1)
	}

	// 3 curtidas novas no topo
	newer := []spotify.SavedTrack{
		{ID: "n2", AddedAt: t0.Add(3 * time.Hour)}, {ID: "n1", AddedAt: t0.Add(2 * time.Hour)}, {ID: "n0", AddedAt: t0.Add(time.Hour)},
	}
	src2 := &fakeSource{items: append(append([]spotify.SavedTrack{}, newer...), lib(120)...), failAt: -1}
	r2, err := Sync(context.Background(), src2, store, syncOpts(t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Mode != "incremental" || r2.New != 3 || len(r2.Tracks) != 123 || r2.Tracks[0].ID != "n2" || r2.Tracks[3].ID != "id0" {
		t.Fatalf("incremental: mode=%s new=%d len=%d", r2.Mode, r2.New, len(r2.Tracks))
	}
	if len(src2.calls) != 1 { // só a 1ª página
		t.Errorf("incremental deveria ler 1 página, leu %d: %v", len(src2.calls), src2.calls)
	}

	// o resultado foi persistido
	r3, _ := Sync(context.Background(), &fakeSource{items: src2.items, failAt: -1}, store, syncOpts(t0.Add(2*time.Hour)))
	if r3.Mode != "incremental" || r3.New != 0 || len(r3.Tracks) != 123 {
		t.Errorf("sem novidades: %+v", r3)
	}
}

func TestSyncFullSyncFlagAndTTL(t *testing.T) {
	store := newStore(t)
	src := &fakeSource{items: lib(60), failAt: -1}
	_, _ = Sync(context.Background(), src, store, syncOpts(t0))

	o := syncOpts(t0.Add(time.Hour))
	o.Full = true
	if r, _ := Sync(context.Background(), &fakeSource{items: lib(60), failAt: -1}, store, o); r.Mode != "full" || r.Reason != "--full-sync" {
		t.Errorf("--full-sync: %+v", r)
	}

	// TTL de 24h vencido
	r, _ := Sync(context.Background(), &fakeSource{items: lib(60), failAt: -1}, store, syncOpts(t0.Add(48*time.Hour)))
	if r.Mode != "full" || !strings.Contains(r.Reason, "TTL") {
		t.Errorf("TTL: %+v", r)
	}
}

func TestSyncDetectsUnlikesViaTotalMismatch(t *testing.T) {
	store := newStore(t)
	items := lib(100)
	_, _ = Sync(context.Background(), &fakeSource{items: items, failAt: -1}, store, syncOpts(t0))

	// descurtiu 2 músicas do meio; nada novo no topo
	after := append(append([]spotify.SavedTrack{}, items[:40]...), items[42:]...)
	r, err := Sync(context.Background(), &fakeSource{items: after, failAt: -1}, store, syncOpts(t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != "full" || !strings.Contains(r.Reason, "divergente") || len(r.Tracks) != 98 {
		t.Fatalf("esperava full por divergência: mode=%s reason=%q len=%d", r.Mode, r.Reason, len(r.Tracks))
	}
}

func TestSyncRelikedTrackReplacesOld(t *testing.T) {
	store := newStore(t)
	items := lib(10)
	_, _ = Sync(context.Background(), &fakeSource{items: items, failAt: -1}, store, syncOpts(t0))

	// id5 foi descurtida e curtida de novo: sobe ao topo com added_at novo (total igual → divergente → full)
	relike := items[5]
	relike.AddedAt = t0.Add(time.Hour)
	after := []spotify.SavedTrack{relike}
	for i, it := range items {
		if i != 5 {
			after = append(after, it)
		}
	}
	r, err := Sync(context.Background(), &fakeSource{items: after, failAt: -1}, store, syncOpts(t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tracks) != 10 || r.Tracks[0].ID != "id5" || !r.Tracks[0].AddedAt.Equal(relike.AddedAt) {
		t.Fatalf("re-curtida mal tratada: %+v", r.Tracks[:2])
	}
	seen := map[string]int{}
	for _, tr := range r.Tracks {
		seen[tr.ID]++
	}
	if seen["id5"] != 1 {
		t.Errorf("id5 duplicada: %d", seen["id5"])
	}
}

func TestSyncCacheFlags(t *testing.T) {
	dir := testutil.TempDir(t)
	src := func() *fakeSource { return &fakeSource{items: lib(30), failAt: -1} }
	_, _ = Sync(context.Background(), src(), &cache.Store{Dir: dir}, syncOpts(t0))

	// --refresh ignora o cache e regrava
	r, _ := Sync(context.Background(), src(), &cache.Store{Dir: dir, NoRead: true}, syncOpts(t0))
	if r.Mode != "full" || r.Reason != "sem cache" {
		t.Errorf("--refresh: %+v", r)
	}
	// --no-cache: sempre completo e não grava
	off := testutil.TempDir(t)
	_, _ = Sync(context.Background(), src(), &cache.Store{Dir: off, Disabled: true}, syncOpts(t0))
	r, _ = Sync(context.Background(), src(), &cache.Store{Dir: off, Disabled: true}, syncOpts(t0))
	if r.Mode != "full" {
		t.Errorf("--no-cache: %+v", r)
	}
}

func TestSyncKeepsIgnoredCountsAcrossRuns(t *testing.T) {
	store := newStore(t)
	items := lib(5)
	items[2].IsLocal, items[2].ID = true, ""
	_, _ = Sync(context.Background(), &fakeSource{items: items, failAt: -1}, store, syncOpts(t0))

	r, err := Sync(context.Background(), &fakeSource{items: items, failAt: -1}, store, syncOpts(t0.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	// sem divergência apesar da faixa local (que não está no cache)
	if r.Mode != "incremental" || r.Stats.Local != 1 || len(r.Tracks) != 4 {
		t.Errorf("mode=%s stats=%+v len=%d", r.Mode, r.Stats, len(r.Tracks))
	}
}

func TestCollectStopAt(t *testing.T) {
	src := &fakeSource{items: lib(200), failAt: -1}
	res, err := Collect(context.Background(), src, Options{StopAt: func(tr spotify.SavedTrack) bool { return tr.ID == "id60" }})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stopped || len(res.Tracks) != 60 || res.Stats.Fetched != 60 || len(src.calls) != 2 {
		t.Errorf("stopped=%v tracks=%d fetched=%d calls=%v", res.Stopped, len(res.Tracks), res.Stats.Fetched, src.calls)
	}
}
