package enrich

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func fastLimiter() *rate.Limiter { return rate.NewLimiter(rate.Inf, 1) }

func TestDeezerGenres(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":1,"name":"Outro Djavan"},{"id":7,"name":"Djavan"}]}`))
	})
	mux.HandleFunc("/artist/7/top", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"album":{"id":100}},{"album":{"id":100}},{"album":{"id":200}}]}`))
	})
	mux.HandleFunc("/album/100", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"genres":{"data":[{"name":"MPB"},{"name":"All"}]}}`))
	})
	mux.HandleFunc("/album/200", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"genres":{"data":[{"name":"MPB"},{"name":"Samba"}]}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	d := NewDeezer(DeezerOptions{BaseURL: srv.URL, UserAgent: "t", Limiter: fastLimiter()})

	got, err := d.Genres(context.Background(), "djavan")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "mpb,samba" { // "all" é descartado; mpb aparece 2x
		t.Errorf("gêneros = %v", got)
	}
	if g, _ := d.Genres(context.Background(), "Ninguém Assim"); g != nil {
		t.Errorf("artista sem correspondência exata deveria dar nil, deu %v", g)
	}
}

func TestDeezerAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
	}))
	defer srv.Close()
	d := NewDeezer(DeezerOptions{BaseURL: srv.URL, UserAgent: "t", Limiter: fastLimiter()})
	if _, err := d.Genres(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "Quota") {
		t.Errorf("erro da API deveria subir, err=%v", err)
	}
}

func TestDiscogsGenresUsesStylesAndAuth(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"results":[
		 {"title":"Djavan - Luz","genre":["Latin"],"style":["MPB"]},
		 {"title":"Djavan - Seduzir","genre":["Latin","Pop"],"style":["MPB","Bossa Nova"]},
		 {"title":"Outro - Coisa","genre":["Rock"],"style":["Punk"]}]}`))
	}))
	defer srv.Close()
	if _, err := NewDiscogs(DiscogsOptions{}); err == nil {
		t.Fatal("sem token deveria falhar")
	}
	d, err := NewDiscogs(DiscogsOptions{BaseURL: srv.URL, Token: "SEGREDO", UserAgent: "t", Limiter: fastLimiter()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Genres(context.Background(), "Djavan")
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Discogs token=SEGREDO" {
		t.Errorf("Authorization = %q", auth)
	}
	if strings.Join(got, ",") != "mpb,bossa nova,latin" || strings.Contains(strings.Join(got, ","), "punk") {
		t.Errorf("gêneros = %v (estilos primeiro, sem releases de outro artista)", got)
	}
}

func TestParseSourcesNew(t *testing.T) {
	got, err := ParseSources("spotify, Deezer ,discogs")
	if err != nil || strings.Join(got, ",") != "spotify,deezer,discogs" {
		t.Errorf("ParseSources = %v %v", got, err)
	}
	_ = time.Second
}
