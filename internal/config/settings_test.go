package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(testutil.TempDir(t), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func entry(es []Entry, key string) Entry {
	for _, e := range es {
		if e.Key == key {
			return e
		}
	}
	return Entry{}
}

// limpa variáveis LIKEDSORTER_* herdadas do ambiente de quem roda os testes
func cleanEnv(t *testing.T) {
	t.Helper()
	for _, r := range registry {
		t.Setenv(EnvName(r.key), "")
		os.Unsetenv(EnvName(r.key))
	}
}

func TestDefaultsWhenNoFile(t *testing.T) {
	cleanEnv(t)
	t.Setenv(EnvConfigDir, testutil.TempDir(t))
	s, entries, err := LoadSettings("")
	if err != nil {
		t.Fatal(err)
	}
	if s != DefaultSettings() {
		t.Errorf("sem arquivo deveria ser o padrão: %+v", s)
	}
	for _, e := range entries {
		if e.Source != SourceDefault {
			t.Errorf("%s: origem %s", e.Key, e.Source)
		}
	}
}

func TestLayering_DefaultsFileEnv(t *testing.T) {
	cleanEnv(t)
	p := write(t, `
log_level: info
sync:
  tracks_ttl: 72h
  genre_source: [spotify, lastfm]
  include_featured: true
group:
  by: artist
  min_size: 5
plan:
  name_template: "Meu • {group}"
apply:
  public: true
`)
	t.Setenv("LIKEDSORTER_GROUP_BY", "decade") // ambiente vence o arquivo
	s, entries, err := LoadSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.LogLevel != "info" || s.TracksTTL != 72*time.Hour || s.GenreSource != "spotify,lastfm" || !s.IncludeFeatured ||
		s.MinSize != 5 || s.NameTemplate != "Meu • {group}" || !s.Public {
		t.Errorf("arquivo não aplicado: %+v", s)
	}
	if s.By != "decade" {
		t.Errorf("ambiente deveria vencer o arquivo: %q", s.By)
	}
	if s.Market != "from_token" || s.Mode != "sync" { // intocados: padrão
		t.Errorf("padrões perdidos: %+v", s)
	}
	for key, want := range map[string]string{
		"group.by": SourceEnv, "group.min_size": SourceFile, "sync.market": SourceDefault, "log_level": SourceFile,
	} {
		if got := entry(entries, key).Source; got != want {
			t.Errorf("origem de %s = %s, want %s", key, got, want)
		}
	}
	if e := entry(entries, "group.by"); e.Env != "LIKEDSORTER_GROUP_BY" || e.Value != "decade" {
		t.Errorf("%+v", e)
	}
}

func TestEmptyEnvDoesNotOverride(t *testing.T) {
	cleanEnv(t)
	t.Setenv("LIKEDSORTER_GROUP_BY", "  ")
	s, _, err := LoadSettings(write(t, "group:\n  by: year\n"))
	if err != nil || s.By != "year" {
		t.Errorf("by=%q err=%v", s.By, err)
	}
}

func TestInvalidFileAndEnvAreRejectedWithContext(t *testing.T) {
	cleanEnv(t)
	tests := []struct {
		name, yaml, want string
	}{
		{"chave desconhecida", "group:\n  bye: x\n", `chave desconhecida "group.bye"`},
		{"typo no topo", "logLevel: info\n", "válidas:"},
		{"valor inválido", "group:\n  small_groups: drop\n", "other, skip"},
		{"inteiro negativo", "group:\n  min_size: -3\n", "inteiro >= 0"},
		{"duração ruim", "sync:\n  tracks_ttl: amanhã\n", "duração"},
		{"bool ruim", "group:\n  multi_genre: talvez\n", "true ou false"},
		{"template sem grupo", "plan:\n  name_template: fixo\n", "{group}"},
		{"yaml quebrado", "group: [", "YAML inválido"},
		{"allow_remove proibido", "apply:\n  allow_remove: true\n", "a cada execução"},
		{"yes proibido", "yes: true\n", "não pode ser desligada"},
	}
	for _, tt := range tests {
		_, _, err := LoadSettings(write(t, tt.yaml))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err=%v, want conter %q", tt.name, err, tt.want)
		}
	}

	t.Setenv("LIKEDSORTER_APPLY_MODE", "apagar")
	if _, _, err := LoadSettings(write(t, "")); err == nil || !strings.Contains(err.Error(), "LIKEDSORTER_APPLY_MODE") {
		t.Errorf("env inválida deveria citar a variável: %v", err)
	}
}

func TestExplicitMissingFileIsErrorButDefaultPathIsNot(t *testing.T) {
	cleanEnv(t)
	if _, _, err := LoadSettings(filepath.Join(testutil.TempDir(t), "nao-existe.yaml")); err == nil {
		t.Error("--config apontando para arquivo inexistente deveria falhar")
	}
	t.Setenv(EnvConfigDir, testutil.TempDir(t))
	if _, _, err := LoadSettings(""); err != nil {
		t.Errorf("arquivo padrão ausente não é erro: %v", err)
	}
}

func TestSampleFileRoundTripsToDefaults(t *testing.T) {
	cleanEnv(t)
	sample := SampleFile()
	for _, r := range registry {
		if !strings.Contains(sample, r.help) {
			t.Errorf("modelo sem a ajuda de %s", r.key)
		}
	}
	if strings.Contains(sample, "allow_remove:") {
		t.Error("o modelo não pode oferecer allow_remove")
	}
	s, entries, err := LoadSettings(write(t, sample))
	if err != nil {
		t.Fatalf("o modelo gerado precisa ser um arquivo válido: %v\n%s", err, sample)
	}
	if s != DefaultSettings() {
		t.Errorf("modelo ≠ padrões:\n got %+v\nwant %+v", s, DefaultSettings())
	}
	if entry(entries, "plan.name_template").Source != SourceFile {
		t.Error("valores do modelo vêm do arquivo")
	}
}

func TestEnvNameAndDurationFormat(t *testing.T) {
	if EnvName("sync.tracks_ttl") != "LIKEDSORTER_SYNC_TRACKS_TTL" || EnvName("log_level") != "LIKEDSORTER_LOG_LEVEL" {
		t.Error("EnvName")
	}
	for in, want := range map[time.Duration]string{168 * time.Hour: "168h", 0: "0s", 90 * time.Minute: "1h30m0s"} {
		if got := formatDuration(in); got != want {
			t.Errorf("%v → %s, want %s", in, got, want)
		}
	}
}
