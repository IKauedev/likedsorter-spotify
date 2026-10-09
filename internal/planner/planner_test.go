package planner

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

func group(name string, ids ...string) grouping.Group {
	g := grouping.Group{Key: name, Name: name, Part: 1, Parts: 1}
	for _, id := range ids {
		g.Tracks = append(g.Tracks, spotify.SavedTrack{ID: id})
	}
	return g
}

func uris(ids ...string) []Member {
	var m []Member
	for _, id := range ids {
		m = append(m, Member{URI: "spotify:track:" + id, Type: "track"})
	}
	return m
}

func u(ids ...string) string {
	var s []string
	for _, id := range ids {
		s = append(s, "spotify:track:"+id)
	}
	return strings.Join(s, ",")
}

func find(p Plan, name string) Item {
	for _, it := range p.Items {
		if it.Name == name {
			return it
		}
	}
	return Item{}
}

func TestBuildCreateUpdateKeepOrphan(t *testing.T) {
	in := Input{
		Groups: []grouping.Group{group("Rock", "a", "b", "c"), group("Jazz", "x", "y"), group("Pop", "p1")},
		Existing: []Existing{
			{ID: "pl-jazz", Name: "Curtidas • Jazz", Managed: true, Loaded: true, Total: 3, Items: uris("x", "z")}, // update: +y -z
			{ID: "pl-pop", Name: "Curtidas • Pop", Managed: true, Loaded: true, Total: 1, Items: uris("p1")},       // keep
			{ID: "pl-old", Name: "Curtidas • Forró", Managed: true, Loaded: false, Total: 40},                      // orphan
			{ID: "pl-mine", Name: "Minha playlist", Managed: false, Total: 10},                                     // ignorada
		},
	}
	p := Build(in)

	if it := find(p, "Curtidas • Rock"); it.Action != Create || u() != "" || strings.Join(it.Add, ",") != u("a", "b", "c") || it.ExistingID != "" {
		t.Errorf("Rock: %+v", it)
	}
	jz := find(p, "Curtidas • Jazz")
	if jz.Action != Update || strings.Join(jz.Add, ",") != u("y") || strings.Join(jz.Remove, ",") != u("z") || jz.Keep != 1 || jz.ExistingID != "pl-jazz" {
		t.Errorf("Jazz: %+v", jz)
	}
	if pp := find(p, "Curtidas • Pop"); pp.Action != Keep || len(pp.Add)+len(pp.Remove) != 0 || pp.Keep != 1 {
		t.Errorf("Pop: %+v", pp)
	}
	if or := find(p, "Curtidas • Forró"); or.Action != Orphan || or.ExistingID != "pl-old" || or.Keep != 40 {
		t.Errorf("órfã: %+v", or)
	}
	for _, it := range p.Items {
		if it.Name == "Minha playlist" {
			t.Error("playlist não gerenciada nunca entra no plano")
		}
	}

	tot := p.Totals()
	if tot.Create != 1 || tot.Update != 1 || tot.Keep != 1 || tot.Orphan != 1 || tot.Add != 4 || tot.Remove != 1 {
		t.Errorf("totais: %+v", tot)
	}
}

func TestBuildIdempotentWhenEqual(t *testing.T) {
	// segunda execução: tudo que foi criado já está lá → só "keep"
	g := []grouping.Group{group("A", "1", "2"), group("B", "3")}
	ex := []Existing{
		{ID: "1", Name: "Curtidas • A", Managed: true, Loaded: true, Items: uris("2", "1")}, // ordem diferente não importa
		{ID: "2", Name: "Curtidas • B", Managed: true, Loaded: true, Items: uris("3")},
	}
	tot := Build(Input{Groups: g, Existing: ex}).Totals()
	if tot.Keep != 2 || tot.Create+tot.Update != 0 || tot.Add != 0 {
		t.Errorf("deveria ser no-op: %+v", tot)
	}
}

func TestBuildDuplicatesAndWarnings(t *testing.T) {
	p := Build(Input{
		Groups: []grouping.Group{group("Rock", "a"), group("Jazz", "x"), group("Pop", "p")},
		Existing: []Existing{
			{ID: "r1", Name: "Curtidas • Rock", Managed: true, Loaded: true, Items: uris("a", "a", "a")},
			{ID: "j1", Name: "Curtidas • Jazz", Managed: true, Loaded: true, Items: uris("x")},
			{ID: "j2", Name: "Curtidas • Jazz", Managed: true, Loaded: true, Items: uris("x")},
			{ID: "p0", Name: "Curtidas • Pop", Managed: false}, // mesmo nome, sem marcador
		},
	})
	if rock := find(p, "Curtidas • Rock"); rock.Duplicates != 2 || rock.Action != Keep {
		t.Errorf("duplicatas: %+v", rock)
	}
	if pop := find(p, "Curtidas • Pop"); pop.Action != Create {
		t.Errorf("sem marcador não é reaproveitada: %+v", pop)
	}
	joined := strings.Join(p.Warnings, "|")
	if !strings.Contains(joined, "2 playlists gerenciadas") || !strings.Contains(joined, "sem o marcador") {
		t.Errorf("avisos: %v", p.Warnings)
	}
	// j2 (a segunda com o mesmo nome) vira órfã, nunca é tocada
	var orphans int
	for _, it := range p.Items {
		if it.Action == Orphan && it.ExistingID == "j2" {
			orphans++
		}
	}
	if orphans != 1 {
		t.Errorf("a repetida deveria aparecer como órfã: %+v", p.Items)
	}
}

func TestBuildPartsAndTemplate(t *testing.T) {
	g := group("Rock", "a")
	g.Name, g.Part, g.Parts = "Rock (Parte 2)", 2, 3
	p := Build(Input{Groups: []grouping.Group{g}, NameTemplate: "🎧 {group} — auto"})
	if it := p.Items[0]; it.Name != "🎧 Rock (Parte 2) — auto" || it.Key != "Rock" || it.Part != 2 {
		t.Errorf("%+v", it)
	}
}

func TestNameAndDescriptionLimits(t *testing.T) {
	long := Name("{group}", strings.Repeat("é", 300))
	if r := []rune(long); len(r) != 100 || r[99] != '…' {
		t.Errorf("nome: %d runas", len(r))
	}
	if Name("", "X") != "Curtidas • X" {
		t.Error("template padrão")
	}
	d := Description(strings.Repeat("x", 1000))
	if len([]rune(d)) > 300 || !strings.HasSuffix(d, Marker) {
		t.Errorf("descrição: %d runas, sufixo ok=%v", len([]rune(d)), strings.HasSuffix(d, Marker))
	}
	if !strings.HasSuffix(Description("artist"), Marker) {
		t.Error("marcador no fim")
	}
	if ValidateTemplate("sem placeholder") == nil || ValidateTemplate("{group}") != nil {
		t.Error("validação do template")
	}
}

func TestIsManaged(t *testing.T) {
	known := map[string]bool{"k": true}
	tests := []struct {
		desc, owner, id string
		want            bool
	}{
		{"bla " + Marker, "me", "a", true},
		{"", "me", "k", true},         // registrada no estado, descrição nula
		{"", "me", "a", false},        // sem marcador e desconhecida
		{Marker, "outro", "a", false}, // marcador, mas de outro dono
		{"texto qualquer", "me", "a", false},
	}
	for _, tt := range tests {
		if got := IsManaged(tt.desc, tt.owner, "me", tt.id, known); got != tt.want {
			t.Errorf("%+v → %v", tt, got)
		}
	}
}

// ------------------------------------------------------------ FetchExisting

type fakeSource struct {
	playlists []spotify.Playlist
	items     map[string][]spotify.PlaylistItem
	itemCalls map[string]int
}

func (f *fakeSource) CurrentUserID(context.Context) (string, error) { return "me", nil }
func (f *fakeSource) MyPlaylists(_ context.Context, off, lim int) (*spotify.PlaylistPage, error) {
	end := min(off+lim, len(f.playlists))
	return &spotify.PlaylistPage{Items: f.playlists[off:end], Total: len(f.playlists), HasNext: end < len(f.playlists)}, nil
}
func (f *fakeSource) PlaylistItems(_ context.Context, id string, off, lim int) (*spotify.PlaylistItemsPage, error) {
	f.itemCalls[id]++
	all := f.items[id]
	end := min(off+lim, len(all))
	return &spotify.PlaylistItemsPage{Items: all[off:end], Total: len(all), HasNext: end < len(all)}, nil
}

func TestFetchExisting(t *testing.T) {
	src := &fakeSource{items: map[string][]spotify.PlaylistItem{}, itemCalls: map[string]int{}}
	// 120 playlists do usuário (3 páginas) + 1 de outro dono com marcador
	for i := 0; i < 120; i++ {
		src.playlists = append(src.playlists, spotify.Playlist{ID: fmt.Sprint("p", i), Name: fmt.Sprint("Outra ", i), OwnerID: "me"})
	}
	src.playlists = append(src.playlists,
		spotify.Playlist{ID: "rock", Name: "Curtidas • Rock", OwnerID: "me", Description: "x " + Marker, Total: 130},
		spotify.Playlist{ID: "forro", Name: "Curtidas • Forró", OwnerID: "me", Description: Marker, Total: 9},
		spotify.Playlist{ID: "alheia", Name: "Curtidas • Rock", OwnerID: "outro", Description: Marker},
	)
	var items []spotify.PlaylistItem
	for i := 0; i < 130; i++ { // 3 páginas de itens
		items = append(items, spotify.PlaylistItem{URI: fmt.Sprintf("spotify:track:t%d", i), ID: fmt.Sprint("t", i), Type: "track"})
	}
	items = append(items, spotify.PlaylistItem{IsLocal: true, URI: "spotify:local:x"}, spotify.PlaylistItem{}) // local e removido
	src.items["rock"] = items

	ex, err := FetchExisting(context.Background(), src, []grouping.Group{group("Rock", "t1")}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex) != 123 {
		t.Fatalf("playlists: %d", len(ex))
	}
	var rock, forro, alheia *Existing
	for i := range ex {
		switch ex[i].ID {
		case "rock":
			rock = &ex[i]
		case "forro":
			forro = &ex[i]
		case "alheia":
			alheia = &ex[i]
		}
	}
	if !rock.Managed || !rock.Loaded || len(rock.Items) != 130 || rock.LocalItems != 1 {
		t.Errorf("rock: managed=%v loaded=%v items=%d locais=%d", rock.Managed, rock.Loaded, len(rock.Items), rock.LocalItems)
	}
	if src.itemCalls["rock"] != 3 {
		t.Errorf("paginação de itens: %d chamadas", src.itemCalls["rock"])
	}
	if !forro.Managed || forro.Loaded || src.itemCalls["forro"] != 0 {
		t.Error("órfã não precisa de itens")
	}
	if alheia.Managed {
		t.Error("playlist de outro dono nunca é gerenciada")
	}
	if len(src.itemCalls) != 1 {
		t.Errorf("só as necessárias deveriam ser lidas: %v", src.itemCalls)
	}
}
