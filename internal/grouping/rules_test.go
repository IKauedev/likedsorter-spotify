package grouping

import (
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

const rulesYAML = `
regras:
  - nome: MPB
    artista: [djavan]
    genero: [mpb]
  - nome: Antigas
    lancamento: "1970-1999"
  - nome: Recentes
    curtida_desde: 2024-01-01
`

func track(id, artist, date string, added time.Time) spotify.SavedTrack {
	return spotify.SavedTrack{ID: id, Name: "t" + id, Artists: []spotify.Artist{{ID: artist, Name: artist}},
		Album: spotify.Album{ReleaseDate: date}, AddedAt: added}
}

func TestRulesFirstMatchAndOther(t *testing.T) {
	rs, err := ParseRules([]byte(rulesYAML), "teste")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	new := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tracks := []spotify.SavedTrack{
		track("1", "Djavan", "1982", old), // MPB (artista+gênero) e Antigas: vence a 1ª
		track("2", "Outro", "1990", old),  // Antigas
		track("3", "Outro", "2020", new),  // Recentes
		track("4", "Outro", "2020", old),  // nenhuma → Outros
	}
	infos := map[string]enrich.Info{"Djavan": {ID: "Djavan", Genres: []string{"mpb"}}}
	st, err := New("rules", Options{Rules: rs})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(tracks, infos, st, Options{Rules: rs})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, g := range res.Groups {
		got[g.Key] = len(g.Tracks)
	}
	want := map[string]int{"MPB": 1, "Antigas": 1, "Recentes": 1, OtherKey: 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("grupo %q = %d, quero %d (todos: %v)", k, got[k], v, got)
		}
	}
}

func TestRulesAllMatches(t *testing.T) {
	rs, _ := ParseRules([]byte("primeira_regra: false\n"+rulesYAML), "teste")
	st, _ := New("rules", Options{Rules: rs})
	keys := st.Keys(track("1", "Djavan", "1982", time.Now()), &Context{Info: map[string]enrich.Info{"Djavan": {Genres: []string{"mpb"}}}})
	if len(keys) < 2 {
		t.Errorf("com primeira_regra=false deveria casar várias, veio %v", keys)
	}
}

func TestRulesValidation(t *testing.T) {
	for name, y := range map[string]string{
		"vazio":         "regras: []",
		"sem nome":      "regras:\n  - artista: [x]",
		"sem condição":  "regras:\n  - nome: A",
		"ano inválido":  "regras:\n  - nome: A\n    lancamento: abc",
		"data inválida": "regras:\n  - nome: A\n    curtida_desde: ontem",
		"nome repetido": "regras:\n  - nome: A\n    artista: [x]\n  - nome: a\n    artista: [y]",
	} {
		if _, err := ParseRules([]byte(y), "t"); err == nil {
			t.Errorf("%s: deveria dar erro", name)
		}
	}
	if _, err := New("rules", Options{}); err == nil {
		t.Error("--by=rules sem arquivo deveria falhar")
	}
}
