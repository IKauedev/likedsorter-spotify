package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheClearOnlyRemovesKnownFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIKEDSORTER_CACHE_DIR", dir)
	for _, n := range []string{"tracks.json", "artists.json", "importante.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := runCache([]string{"info"}, &out); err != nil || !strings.Contains(out.String(), "tracks.json") {
		t.Fatalf("info: %v %q", err, out.String())
	}
	if err := runCache([]string{"clear", "--yes"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tracks.json")); err == nil {
		t.Error("tracks.json deveria ter sido apagado")
	}
	if _, err := os.Stat(filepath.Join(dir, "importante.txt")); err != nil {
		t.Error("arquivos desconhecidos nunca devem ser apagados")
	}
}

func TestUnsetCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIKEDSORTER_CONFIG_DIR", dir)
	var out bytes.Buffer
	if err := runSetup([]string{"--client-id", "0123456789abcdef0123456789abcdef", "--lastfm-key", "K", "--contact", "a@b.c"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := runSetup([]string{"--unset", "lastfm-key,contact"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if strings.Contains(string(b), "LASTFM") || strings.Contains(string(b), "CONTACT") || !strings.Contains(string(b), "SPOTIFY_CLIENT_ID=0123") {
		t.Errorf(".env após unset:\n%s", b)
	}
	if err := runSetup([]string{"--unset", "client-id"}, nil, &out); err == nil {
		t.Error("o Client ID não pode ser removido")
	}
}
