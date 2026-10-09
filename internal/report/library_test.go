package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

func TestComputeLibraryAndRender(t *testing.T) {
	mk := func(id, artist, release string, ms int, added string, explicit bool) spotify.SavedTrack {
		at, _ := time.Parse("2006-01-02", added)
		return spotify.SavedTrack{ID: id, DurationMs: ms, Explicit: explicit, AddedAt: at, Album: spotify.Album{ReleaseDate: release},
			Artists: []spotify.Artist{{ID: "ar-" + artist, Name: artist}}}
	}
	tracks := []spotify.SavedTrack{
		mk("1", "A", "1994-05-01", 3600_000, "2022-03-01", false),
		mk("2", "A", "1999", 1800_000, "2023-01-01", true),
		mk("3", "B", "2015-01", 1800_000, "2023-06-01", false),
		mk("4", "C", "", 0, "2023-07-01", false),
	}
	infos := map[string]enrich.Info{"ar-A": {Genres: []string{"rock"}}, "ar-B": {Genres: []string{"pop"}}}
	s := ComputeLibrary(10, library.Stats{Local: 1}, tracks, infos, true, "desde 2022-01-01")

	if s.Liked != 10 || s.Usable != 4 || s.Explicit != 1 || s.Hours != 2 || s.NoGenre != 1 {
		t.Errorf("%+v", s)
	}
	if got := countsInline(s.ByDecade); got != "1990s (2), 2010s (1)" { // cronológico; faixa sem data fica de fora
		t.Errorf("décadas: %s", got)
	}
	if got := countsInline(s.AddedByYear); got != "2022 (1), 2023 (3)" {
		t.Errorf("anos: %s", got)
	}
	if s.TopArtists[0] != (Count{"A", 2}) || s.TopGenres[0] != (Count{"rock", 2}) {
		t.Errorf("tops: %+v %+v", s.TopArtists, s.TopGenres)
	}

	var b bytes.Buffer
	if err := RenderLibrary(&b, "table", s); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Filtro ativo: desde 2022-01-01", "Duração total: 2.0 h", "Faixas sem gênero: 1", "1990s (2)"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("tabela sem %q:\n%s", want, b.String())
		}
	}
	b.Reset()
	if err := RenderLibrary(&b, "json", s); err != nil {
		t.Fatal(err)
	}
	var back LibraryStats
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || back.Usable != 4 {
		t.Errorf("json: %v %+v", err, back)
	}
	if RenderLibrary(&b, "xml", s) == nil {
		t.Error("formato inválido")
	}
}

func TestRenderDuplicates(t *testing.T) {
	r := library.DedupeResult{NoISRC: 2, Groups: []library.DupGroup{{ISRC: "ISRC-A", Tracks: []spotify.SavedTrack{
		{ID: "a2", Name: "Música", Artists: []spotify.Artist{{Name: "Fulano"}}, Album: spotify.Album{Name: "Ao Vivo", ReleaseDate: "2020"}, AddedAt: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
		{ID: "a1", Name: "Música", Artists: []spotify.Artist{{Name: "Fulano"}}, Album: spotify.Album{Name: "Estúdio", ReleaseDate: "2010"}, AddedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	}}}}
	var b bytes.Buffer
	if err := RenderDuplicates(&b, "table", r, 50); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nada é removido", "grupos duplicados: 1", "ISRC ISRC-A (2 versões)", "Ao Vivo (2020)", "curtida em 2024-01-05"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("sem %q:\n%s", want, b.String())
		}
	}
	b.Reset()
	_ = RenderDuplicates(&b, "json", library.DedupeResult{}, 3)
	var out struct {
		Groups []any `json:"groups"`
	}
	if err := json.Unmarshal(b.Bytes(), &out); err != nil || out.Groups == nil { // [] e não null
		t.Errorf("json vazio: %v %s", err, b.String())
	}
	if RenderDuplicates(&b, "xml", r, 1) == nil {
		t.Error("formato inválido")
	}
}
