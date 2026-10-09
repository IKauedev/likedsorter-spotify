package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/planner"
	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

func TestFromExistingSaveLoadRoundTrip(t *testing.T) {
	now := time.Date(2026, 10, 8, 21, 0, 0, 0, time.UTC)
	ex := []planner.Existing{{
		ID: "p1", Name: "Curtidas • Rock", Description: "d " + planner.Marker, Public: true, Loaded: true, LocalItems: 2,
		Items: []planner.Member{{URI: "spotify:track:b", Type: "track"}, {URI: "spotify:track:a", Type: "track"}, {URI: "spotify:track:b", Type: "track"}},
	}, {ID: "p2", Name: "Vazia", Loaded: true}}

	snap, err := FromExisting(ex, now)
	if err != nil {
		t.Fatal(err)
	}
	path := NewPath(testutil.TempDir(t), now)
	if !strings.Contains(filepath.ToSlash(path), "backups/20261008-210000/snapshot.json") {
		t.Errorf("caminho: %s", path)
	}
	if err := Save(path, snap); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := got.Playlists[0]
	if !slices.Equal(p.URIs(), []string{"spotify:track:b", "spotify:track:a", "spotify:track:b"}) { // ordem e repetições preservadas
		t.Errorf("itens: %v", p.URIs())
	}
	if p.ID != "p1" || !p.Public || p.LocalItems != 2 || !strings.HasSuffix(p.Description, planner.Marker) {
		t.Errorf("%+v", p)
	}
	if got.Playlists[1].Items == nil || len(got.Playlists[1].URIs()) != 0 { // vazia vira [] e não null
		t.Errorf("vazia: %+v", got.Playlists[1])
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("backup contém sua biblioteca; permissão %v", fi.Mode().Perm())
		}
	}
}

func TestFromExistingRejectsIncomplete(t *testing.T) {
	_, err := FromExisting([]planner.Existing{{ID: "p", Name: "N", Loaded: false}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "incompleto") {
		t.Errorf("backup sem itens carregados seria enganoso: %v", err)
	}
}

func TestLoadValidation(t *testing.T) {
	dir := testutil.TempDir(t)
	if _, err := Load(filepath.Join(dir, "nao-existe.json")); err == nil {
		t.Error("ausente")
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{x"), 0o600)
	if _, err := Load(bad); err == nil {
		t.Error("JSON inválido")
	}
	old := filepath.Join(dir, "old.json")
	_ = os.WriteFile(old, []byte(`{"version":99,"playlists":[]}`), 0o600)
	if _, err := Load(old); err == nil || !strings.Contains(err.Error(), "versão") {
		t.Errorf("versão: %v", err)
	}
}
