package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/time/rate"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

const (
	idA = "AAAAAAAAAAAAAAAAAAAAAA"
	idB = "BBBBBBBBBBBBBBBBBBBBBB"
	idC = "CCCCCCCCCCCCCCCCCCCCCC"
)

func TestParseTrackRef(t *testing.T) {
	cases := []struct {
		in       string
		id       string
		ok, nonT bool
	}{
		{"spotify:track:" + idA, idA, true, false},
		{"https://open.spotify.com/track/" + idA + "?si=abc", idA, true, false},
		{"https://open.spotify.com/intl-pt/track/" + idA, idA, true, false},
		{"2kgT6sMwXd3mdeXhBbLMQe", "2kgT6sMwXd3mdeXhBbLMQe", true, false},
		{"https://open.spotify.com/album/" + idA, "", false, true},
		{"spotify:playlist:" + idA, "", false, true},
		{"djavan sina", "", false, false},
		{"abcdefghijklmnopqrstuv", "", false, false}, // 22 letras sem dígitos: é texto, não ID
	}
	for _, c := range cases {
		id, ok, nonT := parseTrackRef(c.in)
		if id != c.id || ok != c.ok || nonT != c.nonT {
			t.Errorf("parseTrackRef(%q) = (%q,%v,%v), quero (%q,%v,%v)", c.in, id, ok, nonT, c.id, c.ok, c.nonT)
		}
	}
}

func TestFindPlaylist(t *testing.T) {
	pls := []spotify.Playlist{{ID: idA, Name: "Treino"}, {ID: idB, Name: "Estudo"}, {ID: idC, Name: "treino"}}
	if p, err := findPlaylist(pls, "estudo"); err != nil || p.ID != idB {
		t.Errorf("por nome: %v %v", p, err)
	}
	if p, err := findPlaylist(pls, "https://open.spotify.com/playlist/"+idC+"?si=x"); err != nil || p.ID != idC {
		t.Errorf("por link: %v %v", p, err)
	}
	if _, err := findPlaylist(pls, "Treino"); err == nil || !strings.Contains(err.Error(), "2 playlists") {
		t.Errorf("ambígua deveria falhar, err=%v", err)
	}
	if _, err := findPlaylist(pls, "nada"); err != errNoPlaylist {
		t.Errorf("inexistente: %v", err)
	}
}

type fakeSpotify struct {
	mu      sync.Mutex
	added   []string
	created []string
	srv     *httptest.Server
}

func newFake(t *testing.T) (*fakeSpotify, *spotify.Client) {
	f := &fakeSpotify{}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) { write(w, map[string]any{"id": "me1"}) })
	mux.HandleFunc("/me/playlists", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			f.mu.Lock()
			f.created = append(f.created, "novo")
			f.mu.Unlock()
			write(w, map[string]any{"id": idC, "name": "Nova", "owner": map[string]any{"id": "me1"}})
			return
		}
		write(w, map[string]any{"total": 2, "next": nil, "items": []map[string]any{
			{"id": idA, "name": "Treino", "owner": map[string]any{"id": "me1"}, "items": map[string]any{"total": 1}},
			{"id": idB, "name": "Alheia", "owner": map[string]any{"id": "outra"}, "items": map[string]any{"total": 0}},
		}})
	})
	mux.HandleFunc("/playlists/"+idA+"/items", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var b struct{ URIs []string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			f.mu.Lock()
			f.added = append(f.added, b.URIs...)
			f.mu.Unlock()
			write(w, map[string]any{"snapshot_id": "s"})
			return
		}
		write(w, map[string]any{"total": 1, "next": nil, "items": []map[string]any{
			{"item": map[string]any{"id": idA, "uri": "spotify:track:" + idA, "type": "track"}},
		}})
	})
	mux.HandleFunc("/playlists/"+idC+"/items", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ URIs []string }
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.added = append(f.added, b.URIs...)
		f.mu.Unlock()
		write(w, map[string]any{})
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"tracks": map[string]any{"items": []map[string]any{
			{"id": idB, "uri": "spotify:track:" + idB, "name": "Sina", "artists": []map[string]any{{"name": "Djavan"}}, "album": map[string]any{"name": "Luz", "release_date": "1982-01-01"}},
		}}})
	})
	mux.HandleFunc("/tracks/"+idA, func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"id": idA, "uri": "spotify:track:" + idA, "name": "Repetida", "artists": []map[string]any{{"name": "X"}}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	c := spotify.New(f.srv.Client(), spotify.Options{BaseURL: f.srv.URL, Limiter: rate.NewLimiter(rate.Inf, 1)})
	return f, c
}

func yesUI(answer bool) playlistUI {
	return playlistUI{
		confirm: func(string) (bool, error) { return answer, nil },
		choose:  func(string, []string) (int, error) { return 0, nil },
	}
}

func TestAddToPlaylistSkipsDuplicatesAndAdds(t *testing.T) {
	f, c := newFake(t)
	var out bytes.Buffer
	// idA já está na playlist; "djavan sina" resolve para idB (busca).
	err := addToPlaylist(context.Background(), c,
		addOptions{To: "treino", Items: []string{"spotify:track:" + idA, "djavan sina", "djavan sina"}}, yesUI(true), &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.added) != 1 || f.added[0] != "spotify:track:"+idB {
		t.Fatalf("adicionou %v, quero só %s\n%s", f.added, idB, out.String())
	}
	if !strings.Contains(out.String(), "puladas") {
		t.Errorf("deveria avisar das repetidas:\n%s", out.String())
	}
}

func TestAddToPlaylistDeclinedWritesNothing(t *testing.T) {
	f, c := newFake(t)
	err := addToPlaylist(context.Background(), c, addOptions{To: "Treino", Items: []string{"djavan sina"}}, yesUI(false), &bytes.Buffer{})
	if err != nil || len(f.added) != 0 {
		t.Fatalf("err=%v added=%v", err, f.added)
	}
}

func TestAddToPlaylistRefusesOthersPlaylist(t *testing.T) {
	f, c := newFake(t)
	err := addToPlaylist(context.Background(), c, addOptions{To: "Alheia", Items: []string{"djavan sina"}}, yesUI(true), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "outra pessoa") || len(f.added) != 0 {
		t.Fatalf("err=%v added=%v", err, f.added)
	}
}

func TestAddToPlaylistCreate(t *testing.T) {
	f, c := newFake(t)
	var out bytes.Buffer
	if err := addToPlaylist(context.Background(), c, addOptions{To: "Nova", Items: []string{"djavan sina"}}, yesUI(true), &out); err == nil {
		t.Fatal("sem --create deveria falhar")
	}
	if err := addToPlaylist(context.Background(), c, addOptions{To: "Nova", Create: true, Items: []string{"djavan sina"}}, yesUI(true), &out); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 1 || len(f.added) != 1 {
		t.Fatalf("created=%v added=%v", f.created, f.added)
	}
}
