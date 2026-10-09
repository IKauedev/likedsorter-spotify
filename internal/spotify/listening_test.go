package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func TestListeningEndpoints(t *testing.T) {
	var gotAfter, gotRange string
	mux := http.NewServeMux()
	track := `{"id":"T1","uri":"spotify:track:T1","name":"Sina","artists":[{"name":"Djavan"}],"album":{"name":"Luz","release_date":"1982"}}`
	mux.HandleFunc("/me/top/tracks", func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.URL.Query().Get("time_range")
		_, _ = w.Write([]byte(`{"items":[` + track + `,{"id":null,"is_local":true,"name":"local"}]}`))
	})
	mux.HandleFunc("/me/top/artists", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"A1","name":"Djavan"}]}`))
	})
	mux.HandleFunc("/me/player/recently-played", func(w http.ResponseWriter, r *http.Request) {
		gotAfter = r.URL.Query().Get("after")
		_, _ = w.Write([]byte(`{"items":[{"played_at":"2026-06-01T12:00:00.123Z","track":` + track + `},{"played_at":"2026-06-01T11:00:00Z","track":null}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.Client(), Options{BaseURL: srv.URL, Limiter: rate.NewLimiter(rate.Inf, 1)})

	tr, ok := ParseTimeRange("short")
	if !ok || tr != RangeShort {
		t.Fatal("ParseTimeRange")
	}
	tracks, err := c.TopTracks(context.Background(), tr, 0, 10)
	if err != nil || len(tracks) != 1 || tracks[0].ID != "T1" || gotRange != "short_term" {
		t.Fatalf("TopTracks: %v %v range=%s", tracks, err, gotRange)
	}
	arts, err := c.TopArtists(context.Background(), RangeLong, 0, 5)
	if err != nil || len(arts) != 1 || arts[0].Name != "Djavan" {
		t.Fatalf("TopArtists: %v %v", arts, err)
	}
	ev, err := c.RecentlyPlayed(context.Background(), 12345)
	if err != nil || len(ev) != 1 || ev[0].Track.Name != "Sina" || gotAfter != "12345" {
		t.Fatalf("RecentlyPlayed: %v %v after=%s", ev, err, gotAfter)
	}
	if _, ok := ParseTimeRange("tudo"); ok {
		t.Error("range inválido")
	}
}
