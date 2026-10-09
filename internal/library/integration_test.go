package library_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// fakeLikedAPI simula GET /me/tracks de verdade (paginação por offset/limit,
// campo "next") e permite injetar falhas por número de requisição.
type fakeLikedAPI struct {
	total   int
	reqs    atomic.Int32
	inject  func(n int32, w http.ResponseWriter) bool // true = já respondeu (falha injetada)
	offsets []int
}

func (f *fakeLikedAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := f.reqs.Add(1)
	if f.inject != nil && f.inject(n, w) {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	f.offsets = append(f.offsets, offset)
	end := min(offset+limit, f.total)

	var items []string
	for i := offset; i < end; i++ {
		items = append(items, fmt.Sprintf(`{"added_at":"2024-01-01T00:00:00Z","track":{"id":"t%04d","uri":"spotify:track:t%04d","name":"n%d",
			"artists":[{"id":"a","name":"A"}],"album":{"id":"al","name":"Al","release_date":"2020"}}}`, i, i, i))
	}
	next := "null"
	if end < f.total {
		next = `"http://next"`
	}
	fmt.Fprintf(w, `{"total":%d,"next":%s,"items":[%s]}`, f.total, next, strings.Join(items, ","))
}

func newClient(t *testing.T, h http.Handler, sleeps *[]time.Duration) *spotify.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return spotify.New(srv.Client(), spotify.Options{
		BaseURL: srv.URL,
		Limiter: rate.NewLimiter(rate.Inf, 1),
		Sleep:   func(_ context.Context, d time.Duration) error { *sleeps = append(*sleeps, d); return nil },
		Jitter:  func(max time.Duration) time.Duration { return max },
	})
}

func assertComplete(t *testing.T, res *library.Result, total int) {
	t.Helper()
	if len(res.Tracks) != total || res.Total != total || res.Stats.Fetched != total {
		t.Fatalf("tracks=%d total=%d fetched=%d, want %d", len(res.Tracks), res.Total, res.Stats.Fetched, total)
	}
	seen := map[string]bool{}
	for i, tr := range res.Tracks {
		if seen[tr.ID] {
			t.Fatalf("faixa repetida: %s", tr.ID)
		}
		seen[tr.ID] = true
		if want := fmt.Sprintf("t%04d", i); tr.ID != want { // ordem preservada, sem pular nenhuma
			t.Fatalf("posição %d: %s, want %s", i, tr.ID, want)
		}
	}
}

func TestCollectOverHTTPPaginatesAllPages(t *testing.T) {
	var sleeps []time.Duration
	api := &fakeLikedAPI{total: 2105} // > 2.000 faixas: 43 páginas
	c := newClient(t, api, &sleeps)

	res, err := library.Collect(context.Background(), c, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertComplete(t, res, 2105)
	if api.reqs.Load() != 43 || api.offsets[1] != 50 || api.offsets[42] != 2100 {
		t.Errorf("requisições=%d offsets=%v...", api.reqs.Load(), api.offsets[:3])
	}
	if len(sleeps) != 0 {
		t.Errorf("sem falhas não deveria esperar: %v", sleeps)
	}
}

func TestCollectOverHTTPSurvives429And5xxMidPagination(t *testing.T) {
	var sleeps []time.Duration
	api := &fakeLikedAPI{total: 320}
	api.inject = func(n int32, w http.ResponseWriter) bool {
		switch n {
		case 2: // 2ª página: rate limit com Retry-After
			w.Header().Set("Retry-After", "4")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		case 4, 5: // 3ª página: dois 503 seguidos
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		return false
	}
	c := newClient(t, api, &sleeps)

	res, err := library.Collect(context.Background(), c, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertComplete(t, res, 320)

	// esperas: Retry-After exato (4s) e depois backoff exponencial (0,5s, 1s)
	want := []time.Duration{4 * time.Second, 500 * time.Millisecond, time.Second}
	if len(sleeps) != len(want) {
		t.Fatalf("esperas: %v", sleeps)
	}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Errorf("espera %d = %v, want %v", i, sleeps[i], want[i])
		}
	}
}

func TestCollectOverHTTPFailsCleanlyOnPermanentError(t *testing.T) {
	var sleeps []time.Duration
	api := &fakeLikedAPI{total: 200}
	api.inject = func(n int32, w http.ResponseWriter) bool {
		if n == 3 {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"status":403,"message":"User not registered in the Developer Dashboard"}}`)
			return true
		}
		return false
	}
	_, err := library.Collect(context.Background(), newClient(t, api, &sleeps), library.Options{})
	if err == nil || !strings.Contains(err.Error(), "User not registered") || !strings.Contains(err.Error(), "offset 100") {
		t.Fatalf("erro deveria indicar a causa e o offset: %v", err)
	}
	if api.reqs.Load() != 3 || len(sleeps) != 0 { // 4xx não é repetido
		t.Errorf("requisições=%d esperas=%v", api.reqs.Load(), sleeps)
	}
}
