package plays

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func TestAddDedupeAndStats(t *testing.T) {
	d := &Data{Meta: map[string]Meta{}}
	if !d.Add("a", t0, Meta{Name: "A"}) || d.Add("a", t0, Meta{}) {
		t.Error("mesma faixa no mesmo instante deve ser deduplicada")
	}
	d.Add("a", t0.Add(24*time.Hour), Meta{})
	d.Add("a", t0.Add(48*time.Hour), Meta{})
	d.Add("b", t0.Add(72*time.Hour), Meta{Name: "B"})
	if d.Add("", t0, Meta{}) {
		t.Error("ID vazio não conta")
	}
	st := d.Stats()
	if st.Count("a", time.Time{}) != 3 || st.Count("a", t0.Add(25*time.Hour)) != 1 || st.Count("zzz", time.Time{}) != 0 {
		t.Errorf("contagens: %d %d", st.Count("a", time.Time{}), st.Count("a", t0.Add(25*time.Hour)))
	}
	if !st.Last("b").Equal(t0.Add(72 * time.Hour)) {
		t.Errorf("Last(b) = %v", st.Last("b"))
	}
	top := st.Top(time.Time{}, 1)
	if len(top) != 1 || top[0].ID != "a" || top[0].Count != 3 {
		t.Errorf("Top = %+v", top)
	}
	if ids := st.TopIDs(t0.Add(60*time.Hour), 5); len(ids) != 1 || ids[0] != "b" {
		t.Errorf("TopIDs com janela = %v", ids)
	}
	if first, _ := st.Span(); !first.Equal(t0) || !st.FirstPlay().Equal(t0) {
		t.Errorf("Span/FirstPlay = %v", first)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plays.json")
	d, err := Load(p) // ausente
	if err != nil || len(d.Events) != 0 {
		t.Fatal(err)
	}
	d.Add("a", t0, Meta{Name: "A", Artists: []string{"X"}})
	d.AdvanceCursor(t0)
	d.AdvanceCursor(t0.Add(-time.Hour)) // não recua
	if err := d.Save(p); err != nil {
		t.Fatal(err)
	}
	d2, err := Load(p)
	if err != nil || len(d2.Events) != 1 || d2.Meta["a"].Name != "A" || d2.CursorMs != t0.UnixMilli() {
		t.Fatalf("round trip: %+v err=%v", d2, err)
	}
	if d2.Add("a", t0, Meta{}) {
		t.Error("evento carregado do disco também deve deduplicar")
	}
}

func TestImportExtended(t *testing.T) {
	js := `[
	 {"ts":"2024-01-01T10:00:00Z","spotify_track_uri":"spotify:track:AAA","ms_played":200000,"master_metadata_track_name":"Música","master_metadata_album_artist_name":"Artista"},
	 {"ts":"2024-01-01T10:10:00Z","spotify_track_uri":"spotify:track:AAA","ms_played":10000},
	 {"ts":"2024-01-02T10:00:00Z","spotify_track_uri":null,"ms_played":900000},
	 {"ts":"2024-01-02T11:00:00Z","spotify_track_uri":"spotify:episode:EEE","ms_played":900000},
	 {"ts":"2024-01-03T10:00:00Z","spotify_track_uri":"spotify:track:BBB","ms_played":60000},
	 {"ts":"2024-01-03T10:00:00Z","spotify_track_uri":"spotify:track:BBB","ms_played":60000}]`
	d := &Data{Meta: map[string]Meta{}}
	added, skipped, err := d.ImportExtended(strings.NewReader(js))
	if err != nil || added != 2 || skipped != 4 {
		t.Fatalf("added=%d skipped=%d err=%v", added, skipped, err)
	}
	if d.Meta["AAA"].Name != "Música" || d.Meta["AAA"].Artists[0] != "Artista" {
		t.Errorf("meta = %+v", d.Meta["AAA"])
	}
	if _, _, err := d.ImportExtended(strings.NewReader(`{"não":"é lista"}`)); err == nil {
		t.Error("formato errado deveria dar erro")
	}
}
