package enrich

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverridesRoundTripAndApply(t *testing.T) {
	p := filepath.Join(t.TempDir(), "o.yaml")
	o, err := LoadOverrides(p) // ausente = vazio
	if err != nil || o.Len() != 0 {
		t.Fatal(err, o.Len())
	}
	o.Set("Djavan", []string{"MPB", " mpb "})
	o.Set("Marisa Monte", []string{"mpb", "pop"})
	if err := o.Save(p, func(path string, b []byte) error { return os.WriteFile(path, b, 0o600) }); err != nil {
		t.Fatal(err)
	}
	o2, err := LoadOverrides(p)
	if err != nil || o2.Len() != 2 {
		t.Fatalf("recarregar: %v len=%d", err, o2.Len())
	}
	infos := map[string]Info{
		"1": {ID: "1", Name: "DJAVAN", Genres: []string{"rock"}, Source: SourceSpotify},
		"2": {ID: "2", Name: "Outro", Genres: []string{"rock"}},
	}
	if n := o2.Apply(infos); n != 1 {
		t.Fatalf("Apply = %d", n)
	}
	if g := infos["1"].Genres; len(g) != 1 || g[0] != "mpb" || infos["1"].Source != "override" {
		t.Errorf("override não aplicado: %+v", infos["1"])
	}
	if infos["2"].Genres[0] != "rock" {
		t.Error("artista sem override foi alterado")
	}
	if !o2.Unset("djavan") || o2.Has("Djavan") {
		t.Error("Unset falhou")
	}
}

func TestOverridesInvalidYAMLValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "o.yaml")
	_ = os.WriteFile(p, []byte("artistas:\n  X: {a: b}\n"), 0o600)
	if _, err := LoadOverrides(p); err == nil {
		t.Error("valor inválido deveria dar erro")
	}
}
