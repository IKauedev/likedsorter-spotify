package grouping

import (
	"sort"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// fakeStats: contagens por faixa em instantes dados.
type fakeStats struct {
	plays map[string][]time.Time
	first time.Time
}

func (f fakeStats) HasHistory() bool     { return len(f.plays) > 0 }
func (f fakeStats) FirstPlay() time.Time { return f.first }
func (f fakeStats) Count(id string, since time.Time) int {
	n := 0
	for _, p := range f.plays[id] {
		if since.IsZero() || !p.Before(since) {
			n++
		}
	}
	return n
}
func (f fakeStats) TopIDs(since time.Time, n int) []string {
	type kv struct {
		id string
		c  int
	}
	var all []kv
	for id := range f.plays {
		if c := f.Count(id, since); c > 0 {
			all = append(all, kv{id, c})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].c > all[j].c || all[i].c == all[j].c && all[i].id < all[j].id })
	var ids []string
	for i, e := range all {
		if i >= n {
			break
		}
		ids = append(ids, e.id)
	}
	return ids
}

func days(now time.Time, d int) time.Time { return now.AddDate(0, 0, -d) }

func TestListeningGroups(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	stats := fakeStats{first: days(now, 400), plays: map[string][]time.Time{
		"hot":     {days(now, 1), days(now, 2), days(now, 3), days(now, 4)}, // top recente e de sempre
		"back":    {days(now, 2), days(now, 3), days(now, 400)},             // redescoberta: tocou há 400d e voltou
		"old":     {days(now, 390)},                                         // última há 390d → esquecida
		"never":   nil,                                                      // curtida antiga, zero plays → esquecida
		"newlike": nil,                                                      // curtida há 10 dias → ainda não é esquecida
	}}
	delete(stats.plays, "never")
	delete(stats.plays, "newlike")
	mk := func(id string, addedDaysAgo int) spotify.SavedTrack {
		return spotify.SavedTrack{ID: id, Name: id, AddedAt: days(now, addedDaysAgo)}
	}
	tracks := []spotify.SavedTrack{mk("hot", 500), mk("back", 500), mk("old", 500), mk("never", 500), mk("newlike", 10)}
	opt := Options{Plays: stats, Now: now, ListenTop: 1, ListenDays: 30, ForgottenDays: 180}
	st, err := New("listening", opt)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(tracks, nil, st, opt)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, g := range res.Groups {
		for _, tr := range g.Tracks {
			got[g.Key] = append(got[g.Key], tr.ID)
		}
	}
	has := func(group, id string) bool {
		for _, x := range got[group] {
			if x == id {
				return true
			}
		}
		return false
	}
	for _, c := range []struct{ g, id string }{
		{"Mais ouvidas · 30 dias", "hot"}, {GroupTopAllTime, "hot"},
		{GroupRediscovered, "back"}, {GroupForgotten, "old"}, {GroupForgotten, "never"},
	} {
		if !has(c.g, c.id) {
			t.Errorf("%s deveria conter %s (grupos: %v)", c.g, c.id, got)
		}
	}
	if has(GroupForgotten, "newlike") || has(GroupForgotten, "hot") {
		t.Errorf("curtida recente e faixa quente não são esquecidas: %v", got)
	}
	if _, ok := got[OtherKey]; ok {
		t.Errorf("faixas sem grupo não devem cair em Outros: %v", got)
	}
	if res.Assigned != 4 { // newlike fica de fora de tudo
		t.Errorf("Assigned = %d, quero 4", res.Assigned)
	}
}

func TestListeningRequiresHistoryAndCoherentWindows(t *testing.T) {
	if _, err := New("listening", Options{}); err == nil {
		t.Error("sem histórico deveria falhar")
	}
	now := time.Now()
	stats := fakeStats{first: days(now, 10), plays: map[string][]time.Time{"a": {days(now, 1)}}}
	if _, err := New("listening", Options{Plays: stats, ListenDays: 90, ForgottenDays: 30}); err == nil {
		t.Error("forgotten-days <= listen-days deveria falhar")
	}
	// histórico curto (10 dias) não pode declarar nada "esquecido"
	opt := Options{Plays: stats, Now: now}
	st, _ := New("listening", opt)
	old := spotify.SavedTrack{ID: "z", AddedAt: days(now, 800)}
	for _, k := range st.Keys(old, nil) {
		if k == GroupForgotten {
			t.Error("com histórico curto ninguém é 'esquecida'")
		}
	}
}
