package library

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

type fakeSource struct {
	items  []spotify.SavedTrack
	calls  []int // offsets pedidos
	failAt int   // offset que falha (-1 = nunca)
}

func (f *fakeSource) SavedTracksPage(ctx context.Context, offset, limit int) (*spotify.SavedPage, error) {
	f.calls = append(f.calls, offset)
	if offset == f.failAt {
		return nil, errors.New("boom")
	}
	end := min(offset+limit, len(f.items))
	return &spotify.SavedPage{Items: f.items[offset:end], Total: len(f.items), HasNext: end < len(f.items)}, nil
}

func tracks(n int) []spotify.SavedTrack {
	out := make([]spotify.SavedTrack, n)
	for i := range out {
		out[i] = spotify.SavedTrack{ID: fmt.Sprintf("id%d", i), Name: "t"}
	}
	return out
}

func TestCollectPaginatesAll(t *testing.T) {
	src := &fakeSource{items: tracks(2105), failAt: -1}
	var lastDone, lastTotal int
	res, err := Collect(context.Background(), src, Options{Progress: func(d, tot int) { lastDone, lastTotal = d, tot }})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 2105 || res.Total != 2105 || len(src.calls) != 43 {
		t.Errorf("tracks=%d total=%d chamadas=%d", len(res.Tracks), res.Total, len(src.calls))
	}
	if src.calls[1] != 50 || src.calls[42] != 2100 {
		t.Errorf("offsets: %v", src.calls)
	}
	if lastDone != 2105 || lastTotal != 2105 {
		t.Errorf("progresso final %d/%d", lastDone, lastTotal)
	}
}

func TestCollectFiltersAndDedupes(t *testing.T) {
	items := []spotify.SavedTrack{
		{ID: "a", Name: "newest copy"},
		{ID: "", IsLocal: true, Name: "local"},
		{ID: "", Name: "removida"},
		{ID: "u", Unavailable: true},
		{ID: "a", Name: "older copy"},
		{ID: "b"},
	}
	res, err := Collect(context.Background(), &fakeSource{items: items, failAt: -1}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{Fetched: 6, Local: 1, NoID: 1, Unavailable: 1, Duplicates: 1}
	if res.Stats != want {
		t.Errorf("stats=%+v, want %+v", res.Stats, want)
	}
	if len(res.Tracks) != 2 || res.Tracks[0].Name != "newest copy" {
		t.Errorf("deveria manter a primeira ocorrência (mais recente): %+v", res.Tracks)
	}
}

func TestCollectEmptyLibrary(t *testing.T) {
	res, err := Collect(context.Background(), &fakeSource{failAt: -1}, Options{})
	if err != nil || len(res.Tracks) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

type stuckSource struct{}

func (stuckSource) SavedTracksPage(context.Context, int, int) (*spotify.SavedPage, error) {
	return &spotify.SavedPage{HasNext: true}, nil // página vazia dizendo que há mais
}

func TestCollectNoInfiniteLoopOnEmptyPage(t *testing.T) {
	if _, err := Collect(context.Background(), stuckSource{}, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestCollectErrorAndCancel(t *testing.T) {
	_, err := Collect(context.Background(), &fakeSource{items: tracks(120), failAt: 50}, Options{})
	if err == nil {
		t.Fatal("esperava erro")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, &fakeSource{items: tracks(10), failAt: -1}, Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("esperava Canceled, veio %v", err)
	}
}
