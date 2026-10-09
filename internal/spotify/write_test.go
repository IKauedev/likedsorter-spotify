package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type recorded struct {
	method, path, ctype string
	body                map[string]any
}

func recorder(t *testing.T, status int, resp string, out *[]recorded) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec := recorded{method: r.Method, path: r.URL.Path, ctype: r.Header.Get("Content-Type")}
		if len(b) > 0 {
			if err := json.Unmarshal(b, &rec.body); err != nil {
				t.Errorf("corpo não é JSON: %s", b)
			}
		}
		*out = append(*out, rec)
		w.WriteHeader(status)
		fmt.Fprint(w, resp)
	})
}

func TestCreatePlaylist(t *testing.T) {
	var reqs []recorded
	c := newTestClient(t, recorder(t, 201, `{"id":"new1","name":"N","snapshot_id":"s","owner":{"id":"me"}}`, &reqs), nil)
	p, err := c.CreatePlaylist(context.Background(), "N", "desc [managed:likedsorter]", false)
	if err != nil {
		t.Fatal(err)
	}
	r := reqs[0]
	if r.method != "POST" || r.path != "/me/playlists" || r.ctype != "application/json" ||
		r.body["name"] != "N" || r.body["public"] != false || r.body["collaborative"] != false ||
		!strings.Contains(r.body["description"].(string), "managed") {
		t.Errorf("requisição: %+v", r)
	}
	if p.ID != "new1" || p.OwnerID != "me" {
		t.Errorf("%+v", p)
	}
}

func TestCreatePlaylistWithoutIDIsError(t *testing.T) {
	var reqs []recorded
	c := newTestClient(t, recorder(t, 201, `{}`, &reqs), nil)
	if _, err := c.CreatePlaylist(context.Background(), "N", "", false); err == nil {
		t.Error("resposta sem id deveria falhar")
	}
}

func TestAddRemoveReplaceRequests(t *testing.T) {
	var reqs []recorded
	c := newTestClient(t, recorder(t, 200, `{"snapshot_id":"x"}`, &reqs), nil)
	ctx := context.Background()
	if err := c.AddItems(ctx, "pl", []string{"spotify:track:a", "spotify:track:b"}); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveItems(ctx, "pl", []string{"spotify:track:a"}); err != nil {
		t.Fatal(err)
	}
	if err := c.ReplaceItems(ctx, "pl", nil); err != nil { // esvaziar
		t.Fatal(err)
	}

	if r := reqs[0]; r.method != "POST" || r.path != "/playlists/pl/items" || len(r.body["uris"].([]any)) != 2 {
		t.Errorf("add: %+v", r)
	}
	r := reqs[1]
	items := r.body["items"].([]any)
	if r.method != "DELETE" || r.path != "/playlists/pl/items" || items[0].(map[string]any)["uri"] != "spotify:track:a" {
		t.Errorf("remove (corpo usa 'items', não 'tracks'): %+v", r)
	}
	if r := reqs[2]; r.method != "PUT" || r.path != "/playlists/pl/items" {
		t.Errorf("replace: %+v", r)
	} else if u, ok := r.body["uris"].([]any); !ok || len(u) != 0 { // "[]", não null
		t.Errorf("esvaziar deve enviar uris: [] — %+v", r.body)
	}
}

func TestBatchLimits(t *testing.T) {
	var reqs []recorded
	c := newTestClient(t, recorder(t, 200, `{}`, &reqs), nil)
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprint("spotify:track:", i)
	}
	ctx := context.Background()
	if c.AddItems(ctx, "pl", many) == nil || c.RemoveItems(ctx, "pl", many) == nil || c.ReplaceItems(ctx, "pl", many) == nil {
		t.Error("lote > 100 deveria falhar")
	}
	if c.AddItems(ctx, "pl", nil) == nil || c.RemoveItems(ctx, "pl", nil) == nil {
		t.Error("lote vazio deveria falhar em add/remove")
	}
	if len(reqs) != 0 {
		t.Errorf("nada deveria ser enviado: %d requisições", len(reqs))
	}
	if c.AddItems(ctx, "pl", many[:100]) != nil {
		t.Error("100 deveria passar")
	}
}

func TestWriteRetries429ButNotAmbiguousFailures(t *testing.T) {
	var calls atomic.Int32
	var sleeps []time.Duration
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(201)
		fmt.Fprint(w, `{}`)
	}), &sleeps)
	if err := c.AddItems(context.Background(), "pl", []string{"spotify:track:a"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(sleeps) != 1 || sleeps[0] != 3*time.Second {
		t.Errorf("429 deveria ser repetido: calls=%d sleeps=%v", calls.Load(), sleeps)
	}

	// 5xx em escrita: UMA tentativa só (pode ter sido aplicada), classificada como transitória
	calls.Store(0)
	c = newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(502)
	}), &sleeps)
	err := c.AddItems(context.Background(), "pl", []string{"spotify:track:a"})
	if err == nil || calls.Load() != 1 || !IsTransient(err) {
		t.Errorf("5xx em escrita: calls=%d err=%v transient=%v", calls.Load(), err, IsTransient(err))
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{&APIError{Status: 500}, true},
		{&APIError{Status: 503}, true},
		{&APIError{Status: 403}, false},
		{&APIError{Status: 400}, false},
		{&RateLimitedError{RetryAfter: time.Hour}, false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
		{errors.New("connection reset by peer"), true},
		{fmt.Errorf("embrulhado: %w", &APIError{Status: 502}), true},
		{fmt.Errorf("embrulhado: %w", &APIError{Status: 404}), false},
	}
	for _, tt := range tests {
		if got := IsTransient(tt.err); got != tt.want {
			t.Errorf("IsTransient(%v) = %v", tt.err, got)
		}
	}
}

func TestWriteForbiddenGivesHelpfulError(t *testing.T) {
	var reqs []recorded
	c := newTestClient(t, recorder(t, 403, `{"error":{"status":403,"message":"Insufficient client scope"}}`, &reqs), nil)
	err := c.AddItems(context.Background(), "pl", []string{"spotify:track:a"})
	if err == nil || !strings.Contains(err.Error(), "Insufficient client scope") || IsTransient(err) {
		t.Errorf("%v", err)
	}
}
