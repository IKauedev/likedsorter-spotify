package enrich

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

	"github.com/ikauedeveloper/likedsorter/internal/cache"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

// ------------------------------------------------------------------ fakes

type fakeFetcher struct {
	genres map[string][]string // id → gêneros do Spotify
	errs   map[string]error
	calls  atomic.Int32
}

func (f *fakeFetcher) Artist(_ context.Context, id string) (*spotify.ArtistDetail, error) {
	f.calls.Add(1)
	if err := f.errs[id]; err != nil {
		return nil, err
	}
	return &spotify.ArtistDetail{ID: id, Name: "N-" + id, Genres: f.genres[id], ImageURL: "img-" + id}, nil
}

type fakeProvider struct {
	name  string
	data  map[string][]string // nome do artista → gêneros
	err   error
	calls atomic.Int32
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) Genres(_ context.Context, artist string) ([]string, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	return p.data[artist], nil
}

func newEnricher(t *testing.T, sources []string, provs map[string]GenreProvider, store *cache.Store) *Enricher {
	t.Helper()
	m, err := cache.OpenTTLMap[Info](store, "artists.json", 24*time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(Config{Sources: sources, Providers: provs, Concurrency: 3, BreakAfter: 3}, m)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func arts(ids ...string) []spotify.Artist {
	out := make([]spotify.Artist, len(ids))
	for i, id := range ids {
		out[i] = spotify.Artist{ID: id, Name: "N-" + id}
	}
	return out
}

// ----------------------------------------------------------- Enricher

func TestEnrichSpotifyThenFallbackThenNone(t *testing.T) {
	f := &fakeFetcher{genres: map[string][]string{"a": {"Rock", " ROCK ", "indie"}}}
	mb := &fakeProvider{name: SourceMusicBrainz, data: map[string][]string{"N-b": {"mpb"}}}
	lf := &fakeProvider{name: SourceLastFM, data: map[string][]string{"N-c": {"funk"}}}
	e := newEnricher(t, []string{"spotify", "musicbrainz", "lastfm"},
		map[string]GenreProvider{"musicbrainz": mb, "lastfm": lf}, &cache.Store{Dir: testutil.TempDir(t)})

	var lastDone int
	res, st, err := e.Enrich(context.Background(), f, arts("a", "b", "c", "d"), func(d, _ int) { lastDone = d })
	if err != nil {
		t.Fatal(err)
	}
	if got := res["a"]; got.Source != "spotify" || strings.Join(got.Genres, ",") != "rock,indie" || got.ImageURL != "img-a" {
		t.Errorf("a: %+v", got)
	}
	if res["b"].Source != "musicbrainz" || res["c"].Source != "lastfm" || res["d"].Source != SourceNone {
		t.Errorf("fallbacks: b=%+v c=%+v d=%+v", res["b"], res["c"], res["d"])
	}
	if mb.calls.Load() != 3 || lf.calls.Load() != 2 { // a não precisa de fallback
		t.Errorf("chamadas mb=%d lf=%d", mb.calls.Load(), lf.calls.Load())
	}
	want := map[string]int{"spotify": 1, "musicbrainz": 1, "lastfm": 1, "none": 1}
	for k, v := range want {
		if st.BySource[k] != v {
			t.Errorf("BySource[%s]=%d, want %d", k, st.BySource[k], v)
		}
	}
	if lastDone != 4 {
		t.Errorf("progresso final %d", lastDone)
	}
}

func TestEnrichRespectsSourceOrderAndSubset(t *testing.T) {
	f := &fakeFetcher{}
	mb := &fakeProvider{name: SourceMusicBrainz, data: map[string][]string{"N-a": {"jazz"}}}
	// sem "spotify" na lista: nenhuma chamada ao Spotify
	e := newEnricher(t, []string{"musicbrainz"}, map[string]GenreProvider{"musicbrainz": mb}, &cache.Store{Dir: testutil.TempDir(t)})
	res, _, err := e.Enrich(context.Background(), f, arts("a"), nil)
	if err != nil || f.calls.Load() != 0 || res["a"].Source != "musicbrainz" {
		t.Fatalf("calls=%d res=%+v err=%v", f.calls.Load(), res["a"], err)
	}
}

func TestEnrichUsesCacheAndPersistsAcrossRuns(t *testing.T) {
	dir := testutil.TempDir(t)
	f := &fakeFetcher{genres: map[string][]string{"a": {"rock"}}}
	e1 := newEnricher(t, []string{"spotify"}, nil, &cache.Store{Dir: dir})
	if _, _, err := e1.Enrich(context.Background(), f, arts("a", "b"), nil); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 2 {
		t.Fatalf("1ª execução: %d chamadas", f.calls.Load())
	}

	e2 := newEnricher(t, []string{"spotify"}, nil, &cache.Store{Dir: dir})
	_, st, _ := e2.Enrich(context.Background(), f, arts("a", "b"), nil)
	if f.calls.Load() != 2 || st.Cached != 2 {
		t.Errorf("2ª execução deveria vir do cache: calls=%d cached=%d", f.calls.Load(), st.Cached)
	}

	// --refresh: ignora o disco
	e3 := newEnricher(t, []string{"spotify"}, nil, &cache.Store{Dir: dir, NoRead: true})
	_, st, _ = e3.Enrich(context.Background(), f, arts("a", "b"), nil)
	if f.calls.Load() != 4 || st.Cached != 0 {
		t.Errorf("--refresh: calls=%d cached=%d", f.calls.Load(), st.Cached)
	}
}

func TestEnrichRetriesNoGenreWhenNewSourceEnabled(t *testing.T) {
	dir := testutil.TempDir(t)
	f := &fakeFetcher{}
	e1 := newEnricher(t, []string{"spotify"}, nil, &cache.Store{Dir: dir})
	res, _, _ := e1.Enrich(context.Background(), f, arts("a"), nil)
	if res["a"].Source != SourceNone {
		t.Fatal("esperava none")
	}

	lf := &fakeProvider{name: SourceLastFM, data: map[string][]string{"N-a": {"funk"}}}
	e2 := newEnricher(t, []string{"spotify", "lastfm"}, map[string]GenreProvider{"lastfm": lf}, &cache.Store{Dir: dir})
	res, st, _ := e2.Enrich(context.Background(), f, arts("a"), nil)
	if res["a"].Source != "lastfm" || st.Cached != 0 {
		t.Errorf("deveria reprocessar com a nova fonte: %+v cached=%d", res["a"], st.Cached)
	}
}

func TestEnrichSpotify404IsNotFatal(t *testing.T) {
	f := &fakeFetcher{errs: map[string]error{"gone": &spotify.APIError{Status: 404, Message: "not found"}}}
	mb := &fakeProvider{name: SourceMusicBrainz, data: map[string][]string{"N-gone": {"samba"}}}
	e := newEnricher(t, []string{"spotify", "musicbrainz"}, map[string]GenreProvider{"musicbrainz": mb}, &cache.Store{Dir: testutil.TempDir(t)})
	res, _, err := e.Enrich(context.Background(), f, arts("gone"), nil)
	if err != nil || res["gone"].Source != "musicbrainz" {
		t.Fatalf("res=%+v err=%v", res["gone"], err)
	}
}

func TestEnrichSpotifyFatalErrorAbortsButKeepsCache(t *testing.T) {
	dir := testutil.TempDir(t)
	f := &fakeFetcher{
		genres: map[string][]string{"ok": {"rock"}},
		errs:   map[string]error{"bad": &spotify.APIError{Status: 403, Message: "forbidden"}},
	}
	e := newEnricher(t, []string{"spotify"}, nil, &cache.Store{Dir: dir})
	e.cfg.Concurrency = 1 // ordem determinística
	_, _, err := e.Enrich(context.Background(), f, arts("ok", "bad", "never"), nil)
	var ae *spotify.APIError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("esperava 403, veio %v", err)
	}
	// o que foi resolvido antes do erro ficou salvo
	e2 := newEnricher(t, []string{"spotify"}, nil, &cache.Store{Dir: dir})
	if _, ok := e2.cache.Get("ok"); !ok {
		t.Error("cache parcial não foi persistido")
	}
}

func TestEnrichProviderFailureNonFatalAndBreaker(t *testing.T) {
	f := &fakeFetcher{}
	mb := &fakeProvider{name: SourceMusicBrainz, err: errors.New("503")}
	e := newEnricher(t, []string{"spotify", "musicbrainz"}, map[string]GenreProvider{"musicbrainz": mb}, &cache.Store{Dir: testutil.TempDir(t)})
	e.cfg.Concurrency = 1
	res, st, err := e.Enrich(context.Background(), f, arts("a", "b", "c", "d", "e"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if mb.calls.Load() != 3 { // BreakAfter=3 desliga o provedor
		t.Errorf("chamadas ao provedor: %d", mb.calls.Load())
	}
	if len(st.Disabled) != 1 || st.Disabled[0] != "musicbrainz" || st.Failures != 3 {
		t.Errorf("stats: %+v", st)
	}
	if res["e"].Source != SourceNone || len(res["e"].Tried) != 1 { // só "spotify" tentado com sucesso
		t.Errorf("e: %+v", res["e"])
	}
}

func TestEnrichContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := newEnricher(t, []string{"spotify"}, nil, &cache.Store{Dir: testutil.TempDir(t)})
	if _, _, err := e.Enrich(ctx, &fakeFetcher{}, arts("a", "b"), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("veio %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	m, _ := cache.OpenTTLMap[Info](&cache.Store{Disabled: true}, "x", 0, nil)
	if _, err := New(Config{}, m); err == nil {
		t.Error("sem fontes deveria falhar")
	}
	if _, err := New(Config{Sources: []string{"lastfm"}}, m); err == nil {
		t.Error("fonte sem provider deveria falhar")
	}
}

func TestArtistsPrimaryVsFeatured(t *testing.T) {
	tracks := []spotify.SavedTrack{
		{Artists: []spotify.Artist{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}},
		{Artists: []spotify.Artist{{ID: "a", Name: "A"}}},
		{Artists: []spotify.Artist{{ID: "", Name: "Local"}}},
		{Artists: []spotify.Artist{{ID: "c", Name: "C"}, {ID: "b", Name: "B"}}},
	}
	if got := Artists(tracks, false); len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("principais: %+v", got)
	}
	if got := Artists(tracks, true); len(got) != 3 {
		t.Errorf("com participações: %+v", got)
	}
}

func TestParseSourcesAndNormalize(t *testing.T) {
	got, err := ParseSources(" Spotify, musicbrainz ,spotify,LASTFM")
	if err != nil || strings.Join(got, ",") != "spotify,musicbrainz,lastfm" {
		t.Errorf("got %v err %v", got, err)
	}
	for _, bad := range []string{"", "spotfy", "spotify,bandcamp"} {
		if _, err := ParseSources(bad); err == nil {
			t.Errorf("%q deveria falhar", bad)
		}
	}
	if g := NormalizeGenres([]string{"Rock", "seen live", "rock", " Hip Hop ", "pop", "jazz"}, 3); strings.Join(g, ",") != "rock,hip hop,pop" {
		t.Errorf("normalize: %v", g)
	}
}

// -------------------------------------------------------------- providers

func fastHTTP(sleeps *[]time.Duration) (*rate.Limiter, func(context.Context, time.Duration) error) {
	return rate.NewLimiter(rate.Inf, 1), func(_ context.Context, d time.Duration) error {
		if sleeps != nil {
			*sleeps = append(*sleeps, d)
		}
		return nil
	}
}

func TestMusicBrainzRequestAndSelection(t *testing.T) {
	var gotUA, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotQuery = r.Header.Get("User-Agent"), r.URL.Query().Get("query")
		fmt.Fprint(w, `{"artists":[
		 {"name":"Outro Nome","score":100,"tags":[{"name":"metal","count":9}]},
		 {"name":"Racionais MC's","score":70,"tags":[{"name":"x","count":1}]},
		 {"name":"racionais mc's","score":98,"tags":[{"name":"seen live","count":50},{"name":"Hip Hop","count":10},{"name":"rap","count":30},{"name":"zero","count":0}]}]}`)
	}))
	defer srv.Close()

	lim, sl := fastHTTP(nil)
	mb, err := NewMusicBrainz(MusicBrainzOptions{BaseURL: srv.URL, UserAgent: "likedsorter/1 ( https://example.com )", Limiter: lim, Sleep: sl})
	if err != nil {
		t.Fatal(err)
	}
	g, err := mb.Genres(context.Background(), `Racionais MC's`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(g, ",") != "rap,hip hop" { // ordenado por count, sem ruído nem count 0
		t.Errorf("gêneros: %v", g)
	}
	if gotUA != "likedsorter/1 ( https://example.com )" || gotQuery != `artist:"Racionais MC's"` {
		t.Errorf("ua=%q query=%q", gotUA, gotQuery)
	}
}

func TestMusicBrainzNoStrongMatchAndValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"artists":[{"name":"Something Else","score":100,"tags":[{"name":"rock","count":3}]}]}`)
	}))
	defer srv.Close()
	lim, sl := fastHTTP(nil)
	mb, _ := NewMusicBrainz(MusicBrainzOptions{BaseURL: srv.URL, UserAgent: "a/1 ( x )", Limiter: lim, Sleep: sl})
	if g, err := mb.Genres(context.Background(), "Wanted"); g != nil || err != nil {
		t.Errorf("sem match forte deveria ser (nil,nil): %v %v", g, err)
	}
	if _, err := NewMusicBrainz(MusicBrainzOptions{UserAgent: "sem-contato"}); err == nil {
		t.Error("UA sem contato deveria falhar")
	}
}

func TestMusicBrainzRetries503(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"artists":[{"name":"X","score":100,"tags":[{"name":"rock","count":3}]}]}`)
	}))
	defer srv.Close()
	var sleeps []time.Duration
	lim, sl := fastHTTP(&sleeps)
	mb, _ := NewMusicBrainz(MusicBrainzOptions{BaseURL: srv.URL, UserAgent: "a/1 ( x )", Limiter: lim, Sleep: sl})
	g, err := mb.Genres(context.Background(), "X")
	if err != nil || len(g) != 1 || calls.Load() != 3 {
		t.Fatalf("g=%v err=%v calls=%d", g, err, calls.Load())
	}
	if len(sleeps) != 2 || sleeps[0] != 2*time.Second || sleeps[1] != 4*time.Second {
		t.Errorf("backoff: %v", sleeps)
	}
}

func TestLastFM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("method") != "artist.gettoptags" || q.Get("api_key") != "KEY" || q.Get("format") != "json" {
			t.Errorf("query: %v", q)
		}
		switch q.Get("artist") {
		case "Known":
			fmt.Fprint(w, `{"toptags":{"tag":[{"count":100,"name":"Rock"},{"count":80,"name":"seen live"},{"count":50,"name":"Alternative"},{"count":5,"name":"marginal"}]}}`)
		case "Missing":
			fmt.Fprint(w, `{"error":6,"message":"The artist you supplied could not be found"}`)
		default:
			fmt.Fprint(w, `{"error":10,"message":"Invalid API key"}`)
		}
	}))
	defer srv.Close()
	lim, sl := fastHTTP(nil)
	lf, err := NewLastFM(LastFMOptions{BaseURL: srv.URL, APIKey: "KEY", UserAgent: "a/1", Limiter: lim, Sleep: sl})
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := lf.Genres(context.Background(), "Known"); strings.Join(g, ",") != "rock,alternative" {
		t.Errorf("Known: %v", g)
	}
	if g, err := lf.Genres(context.Background(), "Missing"); g != nil || err != nil {
		t.Errorf("Missing: %v %v", g, err)
	}
	if _, err := lf.Genres(context.Background(), "Other"); err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("erro da API: %v", err)
	}
	if _, err := NewLastFM(LastFMOptions{}); err == nil {
		t.Error("sem key deveria falhar")
	}
}

func TestLastFMErrorsDoNotLeakKey(t *testing.T) {
	lim, sl := fastHTTP(nil)
	lf, _ := NewLastFM(LastFMOptions{BaseURL: "http://127.0.0.1:1/", APIKey: "SECRETKEY", Limiter: lim, Sleep: sl})
	_, err := lf.Genres(context.Background(), "x")
	if err == nil || strings.Contains(err.Error(), "SECRETKEY") {
		t.Fatalf("erro vazou a chave ou não ocorreu: %v", err)
	}
}
