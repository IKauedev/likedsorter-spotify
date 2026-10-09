package library

import (
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

func TestFindDuplicates(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2024, 1, day, 0, 0, 0, 0, time.UTC) }
	tracks := []spotify.SavedTrack{
		{ID: "a1", ISRC: "ISRC-A", AddedAt: d(1)},
		{ID: "b1", ISRC: "ISRC-B", AddedAt: d(2)},
		{ID: "a2", ISRC: "ISRC-A", AddedAt: d(5)}, // versão de outro álbum, curtida depois
		{ID: "c1", ISRC: ""},                      // sem ISRC: não comparável
		{ID: "a3", ISRC: "ISRC-A", AddedAt: d(3)},
		{ID: "b2", ISRC: "ISRC-B", AddedAt: d(4)},
		{ID: "u1", ISRC: "ISRC-UNICO"},
	}
	r := FindDuplicates(tracks)
	if r.NoISRC != 1 || len(r.Groups) != 2 {
		t.Fatalf("%+v", r)
	}
	a := r.Groups[0] // maior grupo primeiro
	if a.ISRC != "ISRC-A" || len(a.Tracks) != 3 || a.Tracks[0].ID != "a2" || a.Tracks[2].ID != "a1" {
		t.Errorf("A: %+v", a) // mais recentemente curtida primeiro
	}
	if r.Groups[1].ISRC != "ISRC-B" || len(r.Groups[1].Tracks) != 2 {
		t.Errorf("B: %+v", r.Groups[1])
	}
	if got := FindDuplicates(nil); len(got.Groups) != 0 || got.NoISRC != 0 {
		t.Errorf("vazio: %+v", got)
	}
}
