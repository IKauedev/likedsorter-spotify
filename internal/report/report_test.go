package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/library"
	"github.com/ikauedeveloper/likedsorter/internal/planner"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

var update = flag.Bool("update", false, "regrava os arquivos golden")

func sampleData() Data {
	return Data{
		Strategy:    "macro-genre",
		GeneratedAt: time.Date(2026, 10, 8, 21, 30, 0, 0, time.UTC),
		Stats: Stats{
			Liked: 2105, Usable: 2090, Ignored: library.Stats{Local: 3, Unavailable: 2, Duplicates: 10},
			Groups: 4, Assigned: 2090, NoGenre: 150, GenresLoaded: true,
			Sizes:      []Bucket{{"1-4", 0}, {"5-9", 1}, {"10-24", 0}, {"25-49", 0}, {"50-99", 0}, {"100-499", 2}, {"500-999", 0}, {"1000+", 1}},
			TopArtists: []Count{{"Racionais MC's", 40}, {"Djavan", 31}},
			TopGenres:  []Count{{"sertanejo", 400}, {"pop", 300}},
		},
		Plan: planner.Plan{
			Items: []planner.Item{
				{Action: planner.Create, Key: "Rock", Name: "Curtidas • Rock", Desired: 3, Add: []string{"spotify:track:a", "spotify:track:b", "spotify:track:c"}},
				{Action: planner.Update, Key: "Pop", Name: "Curtidas • Pop | Dance", Desired: 5, Keep: 4, ExistingID: "pl1", Add: []string{"spotify:track:d"}, Remove: []string{"spotify:track:z"}},
				{Action: planner.Keep, Key: "Jazz", Name: "Curtidas • Jazz", Desired: 2, Keep: 2, ExistingID: "pl2"},
				{Action: planner.Orphan, Key: "Curtidas • Forró", Name: "Curtidas • Forró", ExistingID: "pl3", Keep: 40},
			},
			Warnings: []string{`já existe uma playlist sua chamada "Curtidas • Rock" sem o marcador do likedsorter`},
		},
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden ausente (rode go test -update): %v", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("%s difere do golden.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func render(t *testing.T, format string, d Data) string {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, format, d); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestGoldenFormats(t *testing.T) {
	for _, tt := range []struct{ format, file string }{
		{"table", "plan.table.golden"}, {"md", "plan.md.golden"}, {"csv", "plan.csv.golden"}, {"json", "plan.json.golden"},
	} {
		t.Run(tt.format, func(t *testing.T) { golden(t, tt.file, render(t, tt.format, sampleData())) })
	}
}

func TestGoldenNoRemoteAndNoGenres(t *testing.T) {
	d := sampleData()
	d.NoRemote = true
	d.Stats.GenresLoaded = false
	d.Plan = planner.Plan{Items: d.Plan.Items[:1]}
	golden(t, "plan.noremote.table.golden", render(t, "table", d))
}

func TestJSONValidAndDetail(t *testing.T) {
	var out struct {
		DryRun    bool `json:"dry_run"`
		Totals    struct{ Create, Update, Keep, Orphan, Add, Remove int }
		Playlists []struct {
			Name        string   `json:"name"`
			AddCount    int      `json:"add_count"`
			RemoveCount int      `json:"remove_count"`
			Add         []string `json:"add"`
		}
	}
	if err := json.Unmarshal([]byte(render(t, "json", sampleData())), &out); err != nil {
		t.Fatal(err)
	}
	if !out.DryRun || out.Totals.Create != 1 || out.Totals.Add != 4 || out.Totals.Remove != 1 || len(out.Playlists) != 4 {
		t.Errorf("%+v", out)
	}
	if out.Playlists[0].AddCount != 3 || out.Playlists[0].Add != nil {
		t.Errorf("sem --detail não lista URIs: %+v", out.Playlists[0])
	}
	d := sampleData()
	d.Detail = true
	_ = json.Unmarshal([]byte(render(t, "json", d)), &out)
	if len(out.Playlists[0].Add) != 3 {
		t.Errorf("com --detail lista URIs: %+v", out.Playlists[0])
	}
}

func TestCSVParsesAndNeutralizesFormulas(t *testing.T) {
	d := sampleData()
	d.Plan.Items[0].Name = `=HYPERLINK("http://x")`
	d.Plan.Items[1].Name = `Com, vírgula e "aspas"`
	rows, err := csv.NewReader(strings.NewReader(render(t, "csv", d))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || len(rows[0]) != 11 {
		t.Fatalf("linhas=%d", len(rows))
	}
	if !strings.HasPrefix(rows[1][1], "'=") {
		t.Errorf("fórmula não neutralizada: %q", rows[1][1])
	}
	if rows[2][1] != `Com, vírgula e "aspas"` {
		t.Errorf("escape do CSV: %q", rows[2][1])
	}
}

func TestMarkdownEscapesPipes(t *testing.T) {
	out := render(t, "md", sampleData())
	if !strings.Contains(out, `Curtidas • Pop \| Dance`) {
		t.Errorf("pipe no nome quebraria a tabela:\n%s", out)
	}
}

func TestUnknownFormat(t *testing.T) {
	if err := Render(&bytes.Buffer{}, "xml", sampleData()); err == nil {
		t.Error("formato desconhecido deveria falhar")
	}
	if render(t, "", sampleData()) != render(t, "table", sampleData()) {
		t.Error("padrão = table")
	}
}

func TestComputeStats(t *testing.T) {
	mk := func(id, artist string) spotify.SavedTrack {
		return spotify.SavedTrack{ID: id, Artists: []spotify.Artist{{ID: "ar-" + artist, Name: artist}}}
	}
	tracks := []spotify.SavedTrack{mk("1", "A"), mk("2", "A"), mk("3", "B"), mk("4", "C"), {ID: "5"}}
	infos := map[string]enrich.Info{
		"ar-A": {Genres: []string{"rock", "indie"}},
		"ar-B": {Genres: []string{"pop"}},
	}
	// 3 playlists: 1, 5 e 120 faixas → buckets 1-4, 5-9, 100-499
	res := &grouping.Result{Assigned: 4, Skipped: 1, Groups: []grouping.Group{
		{Tracks: make([]spotify.SavedTrack, 1)}, {Tracks: make([]spotify.SavedTrack, 5)}, {Tracks: make([]spotify.SavedTrack, 120)},
	}}
	s := Compute(StatsInput{Liked: 7, Tracks: tracks, Infos: infos, Result: res, GenresLoaded: true})

	if s.Liked != 7 || s.Usable != 5 || s.Groups != 3 || s.Assigned != 4 || s.SkippedSmall != 1 {
		t.Errorf("%+v", s)
	}
	if s.NoGenre != 2 { // artista C sem gênero + faixa sem artista
		t.Errorf("NoGenre=%d", s.NoGenre)
	}
	sizes := map[string]int{}
	for _, b := range s.Sizes {
		sizes[b.Label] = b.Playlists
	}
	if sizes["1-4"] != 1 || sizes["5-9"] != 1 || sizes["100-499"] != 1 || sizes["10-24"] != 0 {
		t.Errorf("buckets: %v", sizes)
	}
	if s.TopArtists[0] != (Count{"A", 2}) || s.TopArtists[1].Name != "B" { // empate B/C → ordem alfabética
		t.Errorf("artistas: %+v", s.TopArtists)
	}
	if len(s.TopGenres) != 2 || s.TopGenres[0].Name != "pop" && s.TopGenres[0].Name != "rock" {
		t.Errorf("gêneros: %+v", s.TopGenres)
	}
}

func TestTopLimitedToTen(t *testing.T) {
	m := map[string]int{}
	for i := 0; i < 25; i++ {
		m[fmt.Sprintf("a%02d", i)] = i + 1
	}
	got := top(m)
	if len(got) != 10 || got[0].Name != "a24" || got[9].Name != "a15" {
		t.Errorf("%+v", got)
	}
}
