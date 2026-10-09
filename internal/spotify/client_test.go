package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// newTestClient devolve um Client sem espera real; sleeps guarda as esperas pedidas.
func newTestClient(t *testing.T, h http.Handler, sleeps *[]time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.Client(), Options{
		BaseURL: srv.URL,
		Limiter: rate.NewLimiter(rate.Inf, 1),
		Sleep: func(_ context.Context, d time.Duration) error {
			if sleeps != nil {
				*sleeps = append(*sleeps, d)
			}
			return nil
		},
		Jitter: func(max time.Duration) time.Duration { return max }, // determinístico: backoff = d
	})
}

const onePage = `{"total":1,"next":null,"items":[{"added_at":"2024-05-01T10:00:00Z","track":{
 "id":"t1","uri":"spotify:track:t1","name":"Song","duration_ms":1000,"is_local":false,"is_playable":true,
 "external_ids":{"isrc":"BR123"},
 "artists":[{"id":"a1","name":"Artist"}],
 "album":{"id":"al1","name":"Album","release_date":"2020-05","release_date_precision":"month"}}}]}`

func TestSavedTracksPageParsesAndSendsParams(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/me/tracks" || q.Get("limit") != "50" || q.Get("offset") != "100" || q.Get("market") != "from_token" {
			t.Errorf("requisição inesperada: %s", r.URL)
		}
		fmt.Fprint(w, onePage)
	}), nil)

	p, err := c.SavedTracksPage(context.Background(), 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || p.HasNext || len(p.Items) != 1 {
		t.Fatalf("página: %+v", p)
	}
	it := p.Items[0]
	if it.ID != "t1" || it.ISRC != "BR123" || it.Artists[0].ID != "a1" || it.Album.ReleaseDate != "2020-05" ||
		it.AddedAt.Year() != 2024 || it.Unavailable || it.IsLocal {
		t.Errorf("faixa mal mapeada: %+v", it)
	}
}

func TestSavedTracksNullFields(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"total":3,"next":"x","items":[
		 {"added_at":"2024-05-01T10:00:00Z","track":null},
		 {"added_at":"2024-05-01T10:00:00Z","track":{"id":null,"uri":"spotify:local:a","name":"L","is_local":true,"artists":[{"id":null,"name":"X"}],"album":{"id":null}}},
		 {"added_at":"2024-05-01T10:00:00Z","track":{"id":"u","name":"U","is_playable":false,"artists":[],"album":{}}}]}`)
	}), nil)
	p, err := c.SavedTracksPage(context.Background(), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasNext || p.Items[0].ID != "" || !p.Items[1].IsLocal || !p.Items[2].Unavailable {
		t.Errorf("campos nulos mal tratados: %+v", p.Items)
	}
}

func TestRetry429HonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	var sleeps []time.Duration
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, onePage)
	}), &sleeps)

	if _, err := c.SavedTracksPage(context.Background(), 0, 50); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(sleeps) != 1 || sleeps[0] != 7*time.Second {
		t.Errorf("calls=%d sleeps=%v", calls.Load(), sleeps)
	}
}

func TestRetry429TooLongFailsFast(t *testing.T) {
	var sleeps []time.Duration
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "86400")
		w.WriteHeader(http.StatusTooManyRequests)
	}), &sleeps)

	_, err := c.SavedTracksPage(context.Background(), 0, 50)
	if !IsRateLimited(err) || len(sleeps) != 0 {
		t.Fatalf("err=%v sleeps=%v", err, sleeps)
	}
}

func TestRetry5xxBackoffThenSuccess(t *testing.T) {
	var calls atomic.Int32
	var sleeps []time.Duration
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, onePage)
	}), &sleeps)

	if _, err := c.SavedTracksPage(context.Background(), 0, 50); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second} // dobra a cada tentativa
	if len(sleeps) != 3 {
		t.Fatalf("sleeps=%v", sleeps)
	}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Errorf("espera %d = %v, want %v", i, sleeps[i], want[i])
		}
	}
}

func TestBackoffJitterBounds(t *testing.T) {
	c := New(http.DefaultClient, Options{Jitter: func(max time.Duration) time.Duration { return 0 }})
	if d := c.backoff(0); d != 250*time.Millisecond { // piso: metade
		t.Errorf("piso=%v", d)
	}
	if d := c.backoff(20); d != backoffMax/2 { // teto respeitado
		t.Errorf("teto=%v", d)
	}
	c = New(http.DefaultClient, Options{}) // jitter real
	for i := 0; i < 100; i++ {
		if d := c.backoff(3); d < 2*time.Second || d > 4*time.Second {
			t.Fatalf("fora dos limites: %v", d)
		}
	}
}

func TestRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}), nil)
	_, err := c.SavedTracksPage(context.Background(), 0, 50)
	if err == nil || calls.Load() != 6 { // 1 + 5 retries
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 500 {
		t.Errorf("deveria embrulhar APIError 500: %v", err)
	}
}

func TestClientErrorsDoNotRetry(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"status":403,"message":"User not registered in the Developer Dashboard"}}`)
	}), nil)
	_, err := c.SavedTracksPage(context.Background(), 0, 50)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 403 || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	if !strings.Contains(err.Error(), "not registered") || !strings.Contains(err.Error(), "User Management") {
		t.Errorf("mensagem sem detalhes: %v", err)
	}
}

func TestContextCancelStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := New(srv.Client(), Options{BaseURL: srv.URL, Limiter: rate.NewLimiter(rate.Inf, 1),
		Sleep: func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }})

	if _, err := c.SavedTracksPage(ctx, 0, 50); !errors.Is(err, context.Canceled) {
		t.Fatalf("esperava context.Canceled, veio %v", err)
	}
}

func TestArtistParsesGenresAndPicksImage(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artists/abc" {
			t.Errorf("path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":"abc","name":"Fulano","genres":["mpb","samba"],"images":[
		 {"url":"big","width":640},{"url":"mid","width":300},{"url":"small","width":64}]}`)
	}), nil)
	a, err := c.Artist(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Fulano" || len(a.Genres) != 2 || a.ImageURL != "mid" {
		t.Errorf("%+v", a)
	}
}

func TestArtistNotFoundAndEmptyGenres(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gone") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"status":404,"message":"non existing id"}}`)
			return
		}
		fmt.Fprint(w, `{"id":"x","name":"X","genres":[],"images":[]}`)
	}), nil)
	if _, err := c.Artist(context.Background(), "gone"); !IsNotFound(err) {
		t.Errorf("esperava 404: %v", err)
	}
	a, err := c.Artist(context.Background(), "x")
	if err != nil || len(a.Genres) != 0 || a.ImageURL != "" {
		t.Errorf("%+v %v", a, err)
	}
}

func TestMyPlaylistsParsing(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/playlists" || r.URL.Query().Get("limit") != "50" {
			t.Errorf("req %s", r.URL)
		}
		fmt.Fprint(w, `{"total":3,"next":"x","items":[
		 {"id":"a","name":"Nova","description":"d [managed:likedsorter]","public":false,"owner":{"id":"me"},"snapshot_id":"s","items":{"total":12}},
		 {"id":"b","name":"Antiga","description":null,"public":null,"owner":{"id":"me"},"tracks":{"total":7}},
		 {"id":"c","name":"Sem contagem","owner":{"id":"x"}}]}`)
	}), nil)
	p, err := c.MyPlaylists(context.Background(), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasNext || len(p.Items) != 3 {
		t.Fatalf("%+v", p)
	}
	if a := p.Items[0]; a.Total != 12 || !strings.Contains(a.Description, "managed") || a.OwnerID != "me" || a.Public {
		t.Errorf("a: %+v", a)
	}
	if b := p.Items[1]; b.Total != 7 || b.Description != "" { // tracks.total como reserva; description null
		t.Errorf("b: %+v", b)
	}
}

func TestPlaylistItemsParsing(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playlists/abc/items" {
			t.Errorf("path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"total":4,"next":null,"items":[
		 {"added_at":"2024-01-02T03:04:05Z","is_local":false,"item":{"id":"t1","uri":"spotify:track:t1","type":"track"}},
		 {"added_at":null,"is_local":false,"track":{"id":"t2","uri":"spotify:track:t2","type":"track"}},
		 {"is_local":true,"item":{"id":null,"uri":"spotify:local:a:b:c:1","type":"track"}},
		 {"is_local":false,"item":null}]}`)
	}), nil)
	p, err := c.PlaylistItems(context.Background(), "abc", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if p.HasNext || len(p.Items) != 4 {
		t.Fatalf("%+v", p)
	}
	if p.Items[0].ID != "t1" || p.Items[0].AddedAt.Year() != 2024 || p.Items[1].ID != "t2" { // item e track antigo
		t.Errorf("%+v", p.Items[:2])
	}
	if !p.Items[2].IsLocal || p.Items[2].ID != "" || p.Items[3].URI != "" {
		t.Errorf("%+v", p.Items[2:])
	}
}

func TestCurrentUserID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"id":"kaue"}`) }), nil)
	if id, err := c.CurrentUserID(context.Background()); err != nil || id != "kaue" {
		t.Errorf("%q %v", id, err)
	}
}
