package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingAndRoundTrip(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	st, err := store.Load()
	if err != nil || len(st.Playlists) != 0 || st.Interrupted() {
		t.Fatalf("estado inicial: %+v %v", st, err)
	}
	st.Playlists["pl1"] = PlaylistRecord{Name: "Rock", Key: "Rock", CreatedAt: time.Unix(0, 0).UTC()}
	st.LastRun = &Run{StartedAt: time.Unix(0, 0).UTC(), Items: []RunItem{{Name: "Rock", Status: StatusFailed}}}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.KnownIDs()["pl1"] || !got.Interrupted() || got.LastRun.Items[0].Status != StatusFailed {
		t.Errorf("round trip: %+v", got)
	}
}
