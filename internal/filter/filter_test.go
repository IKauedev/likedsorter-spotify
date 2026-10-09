package filter

import (
	"strings"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

func track(id string, added string, artists ...string) spotify.SavedTrack {
	at, _ := time.Parse("2006-01-02", added)
	t := spotify.SavedTrack{ID: id, AddedAt: at}
	for _, a := range artists {
		t.Artists = append(t.Artists, spotify.Artist{ID: "ar-" + a, Name: a})
	}
	return t
}

var tracks = []spotify.SavedTrack{
	track("1", "2022-12-31", "Legião Urbana"),
	track("2", "2023-01-01", "Racionais MC's", "Edi Rock"),
	track("3", "2024-06-10", "Djavan"),
	track("4", "2024-07-01", "Marisa Monte", "Djavan"),
}

var infos = map[string]enrich.Info{
	"ar-Legião Urbana":  {Genres: []string{"rock brasileiro"}},
	"ar-Racionais MC's": {Genres: []string{"hip hop", "rap"}},
	"ar-Djavan":         {Genres: []string{"mpb"}},
	"ar-Marisa Monte":   {Genres: []string{"mpb", "pop"}},
}

func ids(ts []spotify.SavedTrack) string {
	var s []string
	for _, t := range ts {
		s = append(s, t.ID)
	}
	return strings.Join(s, "")
}

func TestApply(t *testing.T) {
	tests := []struct {
		name string
		f    Filter
		want string
	}{
		{"sem filtro", Filter{}, "1234"},
		{"artista (trecho, sem caixa)", Filter{Artists: SplitList("DJAV")}, "34"},
		{"artista de participação conta", Filter{Artists: SplitList("edi rock")}, "2"},
		{"vários artistas = OU", Filter{Artists: SplitList("legião, marisa")}, "14"},
		{"gênero do artista principal", Filter{Genres: SplitList("mpb")}, "34"},
		{"gênero por trecho", Filter{Genres: SplitList("hop")}, "2"},
		{"since inclusivo", Filter{Since: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)}, "234"},
		{"combinação = E", Filter{Genres: SplitList("mpb"), Since: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)}, "4"},
		{"nada casa", Filter{Artists: SplitList("beatles")}, ""},
	}
	for _, tt := range tests {
		if got := ids(tt.f.Apply(tracks, infos)); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGenreFilterNeedsPrimaryArtistInfo(t *testing.T) {
	f := Filter{Genres: SplitList("pop")}
	// Marisa Monte é a principal da faixa 4 e tem "pop"; Djavan (participação) não conta como principal
	if got := ids(f.Apply(tracks, infos)); got != "4" {
		t.Errorf("got %q", got)
	}
	if got := f.Apply(tracks, nil); len(got) != 0 { // sem enriquecimento, nada casa
		t.Errorf("sem infos: %v", got)
	}
	if !f.NeedsGenres() || (Filter{Artists: []string{"x"}}).NeedsGenres() {
		t.Error("NeedsGenres")
	}
}

func TestActiveDescribeSplitParse(t *testing.T) {
	if (Filter{}).Active() || (Filter{}).Describe() != "" {
		t.Error("filtro vazio")
	}
	f := Filter{Artists: []string{"a"}, Genres: []string{"rock", "pop"}, Since: time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)}
	if !f.Active() || f.Describe() != "artista~a, gênero~rock|pop, desde 2023-01-02" {
		t.Errorf("%q", f.Describe())
	}
	if got := SplitList(" A, ,b ,, C"); strings.Join(got, "|") != "a|b|c" {
		t.Errorf("%v", got)
	}

	for _, ok := range []string{"2023-05-17", "2023-05-17T10:00:00Z", ""} {
		if _, err := ParseSince(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"ontem", "17/05/2023", "2023-13-01"} {
		if _, err := ParseSince(bad); err == nil {
			t.Errorf("%q deveria falhar", bad)
		}
	}
	if d, _ := ParseSince(""); !d.IsZero() {
		t.Error("vazio = sem limite")
	}
}

func TestApplyWithoutFilterReturnsSameSlice(t *testing.T) {
	got := Filter{}.Apply(tracks, infos)
	if len(got) != len(tracks) || &got[0] != &tracks[0] {
		t.Error("sem filtro não deveria copiar")
	}
}
