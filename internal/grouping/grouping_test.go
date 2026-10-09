package grouping

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

var base = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

// tr cria uma faixa; added = dias antes de "base".
func tr(id, artist, release string, daysAgo int) spotify.SavedTrack {
	return spotify.SavedTrack{
		ID: id, Name: "Song " + id, AddedAt: base.AddDate(0, 0, -daysAgo),
		Artists: []spotify.Artist{{ID: "ar-" + artist, Name: artist}},
		Album:   spotify.Album{ReleaseDate: release},
	}
}

func infos(m map[string][]string) map[string]enrich.Info {
	out := map[string]enrich.Info{}
	for artist, gs := range m {
		out["ar-"+artist] = enrich.Info{ID: "ar-" + artist, Genres: gs}
	}
	return out
}

func mustBuild(t *testing.T, tracks []spotify.SavedTrack, inf map[string]enrich.Info, by string, opt Options) *Result {
	t.Helper()
	s, err := New(by, opt)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Build(tracks, inf, s, opt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func names(r *Result) string {
	var parts []string
	for _, g := range r.Groups {
		parts = append(parts, fmt.Sprintf("%s=%d", g.Name, len(g.Tracks)))
	}
	return strings.Join(parts, ",")
}

func TestArtistPrimaryAndFeatured(t *testing.T) {
	a := tr("1", "A", "2020", 1)
	a.Artists = append(a.Artists, spotify.Artist{ID: "ar-B", Name: "B"})
	tracks := []spotify.SavedTrack{a, tr("2", "A", "2020", 2), tr("3", "C", "2020", 3)}

	if got := names(mustBuild(t, tracks, nil, "artist", Options{})); got != "A=2,C=1" {
		t.Errorf("principal: %s", got)
	}
	r := mustBuild(t, tracks, nil, "artist", Options{IncludeFeatured: true})
	if got := names(r); got != "A=2,B=1,C=1" || r.Assigned != 3 {
		t.Errorf("com participações: %s assigned=%d", got, r.Assigned)
	}
}

func TestGenreSingleVsMultiAndNoGenre(t *testing.T) {
	inf := infos(map[string][]string{"A": {"hip hop", "r&b", "trap", "drill"}, "B": {"mpb"}})
	tracks := []spotify.SavedTrack{tr("1", "A", "", 1), tr("2", "B", "", 2), tr("3", "Z", "", 3)}

	if got := names(mustBuild(t, tracks, inf, "genre", Options{})); got != "Hip Hop=1,MPB=1,Sem gênero=1" {
		t.Errorf("single: %s", got)
	}
	r := mustBuild(t, tracks, inf, "genre", Options{MultiGenre: true})
	if got := names(r); got != "Hip Hop=1,MPB=1,R&B=1,Trap=1,Sem gênero=1" { // máx. 3 gêneros por faixa
		t.Errorf("multi: %s", got)
	}
	if r.Assigned != 3 {
		t.Errorf("faixa em vários grupos conta uma vez: %d", r.Assigned)
	}
}

func TestMacroGenre(t *testing.T) {
	inf := infos(map[string][]string{
		"rock":  {"classic rock", "hard rock"},
		"pr":    {"pop rock"}, // Pop (55) > Rock (50)
		"sert":  {"sertanejo universitario"},
		"bhh":   {"brazilian hip hop"}, // Hip Hop (75) > nada de MPB
		"th":    {"tropical house"},    // Eletrônica, não MPB
		"funkr": {"funk carioca"},
		"samba": {"samba de raiz", "mpb"},
		"alien": {"zydeco"},
		"cp":    {"chamber pop"}, // Pop, não Clássica
	})
	var tracks []spotify.SavedTrack
	for i, a := range []string{"rock", "pr", "sert", "bhh", "th", "funkr", "samba", "alien", "cp", "nogenre"} {
		tracks = append(tracks, tr(fmt.Sprint(i), a, "", i))
	}
	r := mustBuild(t, tracks, inf, "macro-genre", Options{})
	got := map[string]int{}
	for _, g := range r.Groups {
		got[g.Name] = len(g.Tracks)
	}
	want := map[string]int{"Rock": 1, "Pop": 2, "Sertanejo": 1, "Hip Hop": 1, "Eletrônica": 1, "Funk": 1, "Pagode/Samba": 1, "Outros": 1, "Sem gênero": 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}

	// multi-genre: "samba de raiz"+"mpb" entra em Pagode/Samba e MPB
	multi := mustBuild(t, tracks, inf, "macro-genre", Options{MultiGenre: true})
	var inMPB bool
	for _, g := range multi.Groups {
		if g.Name == "MPB" && len(g.Tracks) == 1 {
			inMPB = true
		}
	}
	if !inMPB {
		t.Errorf("multi deveria incluir MPB: %s", names(multi))
	}
}

func TestMacroMapPriorityTieAndValidation(t *testing.T) {
	m, err := ParseMacroMap([]byte(`
fallback: Diversos
families:
  - {name: A, priority: 10, contains: [xx]}
  - {name: B, priority: 10, contains: [xx]}
  - {name: C, priority: 20, regex: ['^yy']}
`))
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := m.Family("xx yy"); f != "A" { // empate → primeira do arquivo
		t.Errorf("empate: %s", f)
	}
	if f, _ := m.Family("YY"); f != "C" { // regex sem diferenciar maiúsculas
		t.Errorf("regex: %s", f)
	}
	if _, ok := m.Family("zz"); ok || m.Fallback != "Diversos" {
		t.Error("fallback")
	}

	for name, bad := range map[string]string{
		"vazio":      ``,
		"sem regras": "families:\n  - {name: A, priority: 1}",
		"regex ruim": "families:\n  - {name: A, regex: ['(']}",
		"typo":       "families:\n  - {name: A, contain: [x]}",
		"duplicada":  "families:\n  - {name: A, contains: [x]}\n  - {name: a, contains: [y]}",
		"sem nome":   "families:\n  - {contains: [x]}",
	} {
		if _, err := ParseMacroMap([]byte(bad)); err == nil {
			t.Errorf("%s deveria falhar", name)
		}
	}
	if DefaultMacroMap() == nil { // o YAML embutido precisa ser válido
		t.Fatal("default")
	}
}

func TestDecadeYearAddedPeriod(t *testing.T) {
	tracks := []spotify.SavedTrack{
		tr("1", "A", "1987-03-02", 1), tr("2", "A", "1999", 1), tr("3", "A", "2005-05", 1),
		tr("4", "A", "2023-12-25", 1), tr("5", "A", "1931", 1), tr("6", "A", "", 1), tr("7", "A", "abcd", 1),
	}
	// mesmo tamanho → ordem alfabética pela chave; "Data desconhecida" (2) é o maior
	want := "Data desconhecida=2,Anos 2000=1,Anos 2020=1,Anos 80=1,Anos 90=1,Até 1949=1"
	if got := names(mustBuild(t, tracks, nil, "decade", Options{})); got != want {
		t.Errorf("decade: got %s, want %s", got, want)
	}
	want = "Data desconhecida=2,1931=1,1987=1,1999=1,2005=1,2023=1"
	if got := names(mustBuild(t, tracks, nil, "year", Options{})); got != want {
		t.Errorf("year: got %s, want %s", got, want)
	}

	// base = 2024-06-01: 0 dias = junho; 10 e 20 dias antes = maio
	ts := []spotify.SavedTrack{tr("1", "A", "", 0), tr("2", "A", "", 10), tr("3", "A", "", 20)}
	if got := names(mustBuild(t, ts, nil, "added-period", Options{})); got != "2024-05=2,2024-06=1" {
		t.Errorf("added-period: %s", got)
	}
}

func TestComposite(t *testing.T) {
	inf := infos(map[string][]string{"A": {"rock"}, "B": {"sertanejo"}})
	tracks := []spotify.SavedTrack{tr("1", "A", "1995", 1), tr("2", "A", "2015", 2), tr("3", "B", "2015", 3), tr("4", "A", "2016", 4)}
	r := mustBuild(t, tracks, inf, "macro-genre+decade", Options{})
	got := map[string]int{}
	for _, g := range r.Groups {
		got[g.Name] = len(g.Tracks)
	}
	if got["Rock · Anos 90"] != 1 || got["Rock · Anos 2010"] != 2 || got["Sertanejo · Anos 2010"] != 1 || len(got) != 3 {
		t.Errorf("%v", got)
	}
	// multi-genre multiplica as chaves
	inf2 := infos(map[string][]string{"A": {"rock", "samba"}})
	r = mustBuild(t, []spotify.SavedTrack{tr("1", "A", "2015", 1)}, inf2, "macro-genre+year", Options{MultiGenre: true})
	if len(r.Groups) != 2 || r.Assigned != 1 {
		t.Errorf("multi composto: %s", names(r))
	}
	if _, err := New("macro-genre+nada", Options{}); err == nil {
		t.Error("parte desconhecida deveria falhar")
	}
}

func TestMinSizeOtherAndSkip(t *testing.T) {
	var tracks []spotify.SavedTrack
	for i := 0; i < 5; i++ {
		tracks = append(tracks, tr(fmt.Sprint("a", i), "Big", "", i))
	}
	tracks = append(tracks, tr("s1", "S1", "", 1), tr("s2", "S2", "", 2), tr("s3", "S2", "", 3))

	r := mustBuild(t, tracks, nil, "artist", Options{MinSize: 3})
	if got := names(r); got != "Big=5,Outros=3" || r.Skipped != 0 || r.Assigned != 8 {
		t.Errorf("other: %s skipped=%d", got, r.Skipped)
	}
	r = mustBuild(t, tracks, nil, "artist", Options{MinSize: 3, SmallGroups: SmallSkip})
	if got := names(r); got != "Big=5" || r.Skipped != 3 {
		t.Errorf("skip: %s skipped=%d", got, r.Skipped)
	}
	// MinSize 0 desliga
	if got := names(mustBuild(t, tracks, nil, "artist", Options{})); got != "Big=5,S2=2,S1=1" {
		t.Errorf("sem mínimo: %s", got)
	}
	// um "Outros" natural pequeno não some em modo other
	inf := infos(map[string][]string{"x": {"zydeco"}})
	one := []spotify.SavedTrack{tr("1", "x", "", 1)}
	if got := names(mustBuild(t, one, inf, "macro-genre", Options{MinSize: 5})); got != "Outros=1" {
		t.Errorf("Outros pequeno: %s", got)
	}
}

func TestMinSizeMultiGenreDedupesInOther(t *testing.T) {
	inf := infos(map[string][]string{"A": {"x1", "x2"}})
	r := mustBuild(t, []spotify.SavedTrack{tr("1", "A", "", 1)}, inf, "genre", Options{MultiGenre: true, MinSize: 2})
	if len(r.Groups) != 1 || r.Groups[0].Name != "Outros" || len(r.Groups[0].Tracks) != 1 {
		t.Errorf("faixa duplicada em Outros: %s", names(r))
	}
}

func TestMaxSizeSplitsIntoParts(t *testing.T) {
	var tracks []spotify.SavedTrack
	for i := 0; i < 25; i++ {
		tracks = append(tracks, tr(fmt.Sprintf("t%02d", i), "A", "", i))
	}
	r := mustBuild(t, tracks, nil, "artist", Options{MaxSize: 10})
	if got := names(r); got != "A (Parte 1)=10,A (Parte 2)=10,A (Parte 3)=5" {
		t.Errorf("partes: %s", got)
	}
	if r.Groups[0].Key != "A" || r.Groups[2].Part != 3 || r.Groups[2].Parts != 3 {
		t.Errorf("metadados: %+v", r.Groups[2])
	}
	// sem MaxSize, o teto é o do Spotify
	opt := Options{MaxSize: 50000}
	_ = opt.Normalize()
	if opt.MaxSize != MaxPlaylistTracks {
		t.Errorf("teto: %d", opt.MaxSize)
	}
}

func TestSortModes(t *testing.T) {
	a := tr("a", "A", "2001", 5) // curtida há 5 dias
	b := tr("b", "A", "2020", 1)
	c := tr("c", "A", "2010", 3)
	a.Name, b.Name, c.Name = "Zebra", "Maçã", "abacate"
	tracks := []spotify.SavedTrack{a, b, c}
	order := func(sort string) string {
		r := mustBuild(t, tracks, nil, "artist", Options{Sort: sort})
		var ids []string
		for _, tt := range r.Groups[0].Tracks {
			ids = append(ids, tt.ID)
		}
		return strings.Join(ids, "")
	}
	if got := order(SortAdded); got != "bca" {
		t.Errorf("added: %s", got)
	}
	if got := order(SortRelease); got != "bca" {
		t.Errorf("release: %s", got)
	}
	if got := order(SortTitle); got != "cba" { // abacate, maçã, zebra (sem diferenciar caixa)
		t.Errorf("title: %s", got)
	}
}

func TestOptionsValidation(t *testing.T) {
	for _, o := range []Options{{Sort: "popularity"}, {Sort: "xyz"}, {SmallGroups: "drop"}, {MinSize: -1}} {
		if err := o.Normalize(); err == nil {
			t.Errorf("%+v deveria falhar", o)
		}
	}
	err := (&Options{Sort: "popularity"}).Normalize()
	if err == nil || !strings.Contains(err.Error(), "removeu") {
		t.Errorf("mensagem de popularity: %v", err)
	}
}

func TestGroupOrderingPutsSpecialLast(t *testing.T) {
	inf := infos(map[string][]string{"big": {"rock"}, "small": {"jazz"}})
	var tracks []spotify.SavedTrack
	for i := 0; i < 3; i++ {
		tracks = append(tracks, tr(fmt.Sprint("n", i), "none", "", i)) // Sem gênero é o maior grupo...
	}
	tracks = append(tracks, tr("b1", "big", "", 1), tr("b2", "big", "", 1), tr("s1", "small", "", 1))
	r := mustBuild(t, tracks, inf, "macro-genre", Options{})
	if got := names(r); got != "Rock=2,Jazz=1,Sem gênero=3" { // ...mas vai por último
		t.Errorf("ordem: %s", got)
	}
}

func TestLanguageExperimental(t *testing.T) {
	s, _ := New("language", Options{})
	if e, ok := s.(Experimental); !ok || !e.Experimental() {
		t.Error("language deve ser experimental")
	}
	c, _ := New("language+year", Options{})
	if e, ok := c.(Experimental); !ok || !e.Experimental() {
		t.Error("composta com language deve ser experimental")
	}
	lang := func(name string, genres ...string) string {
		return detectLanguage(spotify.SavedTrack{Name: name}, genres)
	}
	tests := []struct{ got, want string }{
		{lang("Não Quero Mais Você"), "Português"},
		{lang("Corazón de la noche con mi amor"), "Espanhol"},
		{lang("Love me like you do"), "Inglês"},
		{lang("사랑해"), "Coreano"},
		{lang("夜に駆ける ヨルシカ"), "Japonês"},
		{lang("Привет"), "Russo"},
		{lang("Xyzzy"), "Indefinido"},
		{lang("Whatever", "sertanejo universitario"), "Português"}, // dica de gênero vence
		{lang("Love", "k-pop"), "Coreano"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestDisplayGenre(t *testing.T) {
	for in, want := range map[string]string{"hip hop": "Hip Hop", "r&b": "R&B", "k-pop": "K-Pop", "mpb": "MPB", "post-punk": "Post-Punk", "pagode": "Pagode"} {
		if got := displayGenre(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestUnknownStrategy(t *testing.T) {
	if _, err := New("nope", Options{}); err == nil || !strings.Contains(err.Error(), "artist") {
		t.Errorf("deveria listar opções: %v", err)
	}
}
