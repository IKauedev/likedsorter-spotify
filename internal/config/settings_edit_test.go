package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetAndUnsetSetting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := SetSetting(p, "group.min_size", "15"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(p, "apply.public", "true"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(p, "sync.tracks_ttl", "72h"); err != nil {
		t.Fatal(err)
	}
	s, entries, err := LoadSettings(p)
	if err != nil {
		t.Fatalf("o arquivo gravado precisa ser lido de volta: %v", err)
	}
	if s.MinSize != 15 || !s.Public || s.TracksTTL.Hours() != 72 {
		t.Errorf("valores: %+v", s)
	}
	src := map[string]string{}
	for _, e := range entries {
		src[e.Key] = e.Source
	}
	if src["group.min_size"] != SourceFile || src["group.sort"] != SourceDefault {
		t.Errorf("origens: %v", src)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "# market enviado ao Spotify") || !strings.Contains(string(b), "# market: from_token") {
		t.Errorf("comentários de ajuda e padrões comentados deveriam permanecer:\n%s", b)
	}

	was, err := UnsetSetting(p, "group.min_size")
	if err != nil || !was {
		t.Fatalf("unset: %v %v", was, err)
	}
	if s, _, _ := LoadSettings(p); s.MinSize != 0 {
		t.Errorf("min_size deveria voltar ao padrão, = %d", s.MinSize)
	}
	if was, _ := UnsetSetting(p, "group.min_size"); was {
		t.Error("segundo unset não deveria acusar mudança")
	}
}

func TestSetSettingRejectsInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	for key, val := range map[string]string{
		"group.min_size":     "abc",
		"group.sort":         "popularity",
		"group.by":           "12",
		"nao.existe":         "1",
		"apply.yes":          "true",
		"plan.name_template": "sem placeholder",
	} {
		if err := SetSetting(p, key, val); err == nil {
			t.Errorf("%s=%s deveria ser recusado", key, val)
		}
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("nenhum arquivo deveria ser criado por valores inválidos")
	}
}

func TestRenderFileRoundTripsLikeSample(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(RenderFile(nil)), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _, err := LoadSettings(p)
	if err != nil || s != DefaultSettings() {
		t.Errorf("arquivo sem chaves ativas deve equivaler aos padrões: err=%v", err)
	}
}
