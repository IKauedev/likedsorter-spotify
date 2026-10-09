package report

import (
	"bytes"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

func sampleLibrary() LibraryStats {
	return LibraryStats{
		Liked: 2105, Usable: 1800, Filter: "gênero~rock, desde 2023-01-01",
		Ignored: library.Stats{Local: 3, Unavailable: 2, NoID: 1, Duplicates: 10},
		Hours:   123.4, Explicit: 210, GenresLoaded: true, NoGenre: 150,
		ByDecade:    []Count{{"1980s", 100}, {"1990s", 400}, {"2010s", 900}},
		AddedByYear: []Count{{"2022", 700}, {"2023", 1100}},
		TopArtists:  []Count{{"Legião Urbana", 55}, {"Djavan", 40}},
		TopGenres:   []Count{{"rock brasileiro", 300}, {"mpb", 250}},
	}
}

func TestGoldenLibraryStats(t *testing.T) {
	golden(t, "library.table.golden", render2(t, func(b *bufWriter) error { return RenderLibrary(b, "table", sampleLibrary()) }))
	golden(t, "library.json.golden", render2(t, func(b *bufWriter) error { return RenderLibrary(b, "json", sampleLibrary()) }))
}

func TestGoldenDuplicates(t *testing.T) {
	r := library.DedupeResult{NoISRC: 4, Groups: []library.DupGroup{{ISRC: "BRABC2000001", Tracks: []spotify.SavedTrack{
		{ID: "t2", Name: "Eduardo e Mônica", Artists: []spotify.Artist{{Name: "Legião Urbana"}}, Album: spotify.Album{Name: "Dois", ReleaseDate: "1986"}, AddedAt: time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)},
		{ID: "t1", Name: "Eduardo e Mônica", Artists: []spotify.Artist{{Name: "Legião Urbana"}}, Album: spotify.Album{Name: "Mais do Mesmo", ReleaseDate: "1998"}, AddedAt: time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)},
	}}}}
	golden(t, "dedupe.table.golden", render2(t, func(b *bufWriter) error { return RenderDuplicates(b, "table", r, 1800) }))
	golden(t, "dedupe.json.golden", render2(t, func(b *bufWriter) error { return RenderDuplicates(b, "json", r, 1800) }))
}

// bufWriter evita importar bytes só para isso nos helpers acima.
type bufWriter = bytes.Buffer

func render2(t *testing.T, fn func(*bufWriter) error) string {
	t.Helper()
	var b bufWriter
	if err := fn(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
