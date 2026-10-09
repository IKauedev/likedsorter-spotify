package main

import (
	"bytes"
	"context"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

func TestSplitGlobals(t *testing.T) {
	tests := []struct {
		in       []string
		wantRest string
		want     globals
	}{
		{[]string{"sync", "--no-enrich"}, "sync --no-enrich", globals{}},
		{[]string{"--verbose", "plan", "--by", "year"}, "plan --by year", globals{verbose: true}},
		{[]string{"plan", "-v", "--log-level=info"}, "plan", globals{verbose: true, logLevel: "info"}},
		{[]string{"--log-level", "debug", "sync", "--config", "x.yaml"}, "sync", globals{logLevel: "debug", configPath: "x.yaml"}},
		{[]string{"sync", "--config=/tmp/c.yaml"}, "sync", globals{configPath: "/tmp/c.yaml"}},
	}
	for _, tt := range tests {
		rest, g, err := splitGlobals(tt.in)
		if err != nil || strings.Join(rest, " ") != tt.wantRest || g != tt.want {
			t.Errorf("%v → rest=%q g=%+v err=%v", tt.in, rest, g, err)
		}
	}
	if _, _, err := splitGlobals([]string{"sync", "--config"}); err == nil {
		t.Error("--config sem valor deveria falhar")
	}
}

func TestParseLevelAndSetupLogging(t *testing.T) {
	for in, want := range map[string]slog.Level{"debug": slog.LevelDebug, "INFO": slog.LevelInfo, "warn": slog.LevelWarn, "": slog.LevelWarn, "error": slog.LevelError} {
		if got, err := parseLevel(in); err != nil || got != want {
			t.Errorf("%q → %v %v", in, got, err)
		}
	}
	if _, err := parseLevel("barulhento"); err == nil {
		t.Error("nível inválido")
	}

	var buf bytes.Buffer
	old := cur
	defer func() { cur = old }()

	cur.LogLevel = "error"
	if err := setupLogging(globals{}, &buf); err != nil {
		t.Fatal(err)
	}
	appLogger.Warn("w")
	if buf.Len() != 0 {
		t.Error("nível error não deveria mostrar warn")
	}
	if err := setupLogging(globals{logLevel: "info"}, &buf); err != nil { // flag vence a config
		t.Fatal(err)
	}
	appLogger.Info("visivel")
	if !strings.Contains(buf.String(), "visivel") {
		t.Error("--log-level info deveria mostrar info")
	}
	buf.Reset()
	if err := setupLogging(globals{logLevel: "error", verbose: true}, &buf); err != nil { // verbose vence tudo
		t.Fatal(err)
	}
	appLogger.Debug("detalhe")
	if !strings.Contains(buf.String(), "detalhe") {
		t.Error("--verbose deveria mostrar debug")
	}
	if setupLogging(globals{logLevel: "xx"}, &buf) == nil {
		t.Error("nível inválido")
	}
}

func TestFilterFlagsBuild(t *testing.T) {
	f := filterFlags{artist: "Djavan, Marisa", genre: "MPB", since: "2023-01-01"}
	flt, err := f.build()
	if err != nil || !flt.Active() || len(flt.Artists) != 2 || flt.Artists[0] != "djavan" || flt.Since.Year() != 2023 {
		t.Errorf("%+v %v", flt, err)
	}
	if _, err := (&filterFlags{since: "ontem"}).build(); err == nil {
		t.Error("data inválida")
	}
	if flt, _ := (&filterFlags{}).build(); flt.Active() {
		t.Error("vazio = inativo")
	}
}

// runCLI executa a CLI em processo, isolando config/cache e o estado global.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dir := testutil.TempDir(t)
	t.Setenv(config.EnvConfigDir, dir)
	t.Setenv(config.EnvCacheDir, filepath.Join(dir, "cache"))
	old := cur
	t.Cleanup(func() { cur = old })
	var out bytes.Buffer
	err := run(context.Background(), args, &out)
	return out.String(), err
}

func TestVersionHelpAndUnknown(t *testing.T) {
	if out, err := runCLI(t, "version"); err != nil || !strings.HasPrefix(out, "likedsorter ") {
		t.Errorf("%q %v", out, err)
	}
	out, err := runCLI(t)
	if err != nil || !strings.Contains(out, "Comandos:") {
		t.Errorf("sem args mostra o uso: %q %v", out, err)
	}
	for _, cmd := range []string{"stats", "dedupe", "config", "apply", "restore", "export", "plan", "--filter-artist", "--log-level"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("uso sem %q", cmd)
		}
	}
	if _, err := runCLI(t, "nada"); err == nil || !strings.Contains(err.Error(), "desconhecido") {
		t.Errorf("%v", err)
	}
}

func TestConfigInitShowPath(t *testing.T) {
	dir := testutil.TempDir(t)
	t.Setenv(config.EnvConfigDir, dir)
	t.Setenv("LIKEDSORTER_GROUP_BY", "")
	os.Unsetenv("LIKEDSORTER_GROUP_BY")
	var out bytes.Buffer
	old := cur
	defer func() { cur = old }()
	ctx := context.Background()

	if err := run(ctx, []string{"config", "path"}, &out); err != nil || strings.TrimSpace(out.String()) != filepath.Join(dir, "config.yaml") {
		t.Fatalf("path: %q %v", out.String(), err)
	}
	out.Reset()
	if err := run(ctx, []string{"config", "init"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal("config.yaml não criado")
	}
	if err := run(ctx, []string{"config", "init"}, &out); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("não deve sobrescrever sem --force: %v", err)
	}
	if err := run(ctx, []string{"config", "init", "--force"}, &out); err != nil {
		t.Errorf("--force: %v", err)
	}

	// edita o arquivo e confere a origem em `config show`
	_ = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("group:\n  min_size: 7\n"), 0o600)
	t.Setenv("LIKEDSORTER_GROUP_BY", "year")
	out.Reset()
	if err := run(ctx, []string{"config", "show"}, &out); err != nil {
		t.Fatal(err)
	}
	show := out.String()
	for _, want := range []string{"carregado", "group.min_size", "arquivo", "group.by", "year", "ambiente", "LIKEDSORTER_GROUP_BY"} {
		if !strings.Contains(show, want) {
			t.Errorf("config show sem %q:\n%s", want, show)
		}
	}
}

func TestInvalidConfigBreaksCommandsButNotDiagnosis(t *testing.T) {
	dir := testutil.TempDir(t)
	t.Setenv(config.EnvConfigDir, dir)
	_ = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("group:\n  bye: 1\n"), 0o600)
	old := cur
	defer func() { cur = old }()
	var out bytes.Buffer
	ctx := context.Background()

	if err := run(ctx, []string{"version"}, &out); err != nil {
		t.Errorf("version não depende da config: %v", err)
	}
	if err := run(ctx, []string{"plan"}, &out); err == nil || !strings.Contains(err.Error(), "group.bye") {
		t.Errorf("plan deveria apontar a chave inválida: %v", err)
	}
	if err := run(ctx, []string{"config", "show"}, &out); err == nil || !strings.Contains(err.Error(), "group.bye") {
		t.Errorf("config show deveria diagnosticar: %v", err)
	}
}

func TestSettingsBecomeFlagDefaults(t *testing.T) {
	old := cur
	defer func() { cur = old }()
	cur.By, cur.MinSize = "decade", 9

	var g groupFlags
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	g.register(fs)
	if err := fs.Parse(nil); err != nil || g.by != "decade" || g.minSize != 9 {
		t.Errorf("padrões da config: %+v %v", g, err)
	}
	if err := fs.Parse([]string{"--by", "year"}); err != nil || g.by != "year" || g.minSize != 9 { // flag vence
		t.Errorf("flag deveria vencer: %+v %v", g, err)
	}
}

func TestApplyRejectsFilterWithRemoval(t *testing.T) {
	dir := testutil.TempDir(t)
	t.Setenv(config.EnvConfigDir, dir)
	old := cur
	defer func() { cur = old }()
	var out bytes.Buffer
	for _, args := range [][]string{
		{"apply", "--filter-artist", "x", "--allow-remove"},
		{"apply", "--since", "2024-01-01", "--mode", "recreate", "--allow-remove"},
	} {
		err := run(context.Background(), args, &out)
		if err == nil || !strings.Contains(err.Error(), "não podem ser combinados") {
			t.Errorf("%v → %v", args, err)
		}
	}
}
