package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

func TestParseEnvLine(t *testing.T) {
	tests := []struct {
		in, k, v string
		ok       bool
	}{
		{"A=b", "A", "b", true},
		{"export A = b ", "A", "b", true},
		{`A="x y"`, "A", "x y", true},
		{"A='x'", "A", "x", true},
		{"A=b # comentário", "A", "b", true},
		{"A=http://h/#frag", "A", "http://h/#frag", true},
		{"# nada", "", "", false},
		{"", "", "", false},
		{"semigual", "", "", false},
	}
	for _, tt := range tests {
		k, v, ok := parseEnvLine(tt.in)
		if k != tt.k || v != tt.v || ok != tt.ok {
			t.Errorf("%q: got (%q,%q,%v)", tt.in, k, v, ok)
		}
	}
}

func TestLoadPrecedenceAndDefaults(t *testing.T) {
	dir := testutil.TempDir(t)
	t.Setenv(EnvConfigDir, dir)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(testutil.TempDir(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	t.Setenv(EnvClientID, "")
	os.Unsetenv(EnvClientID)
	os.Unsetenv(EnvRedirectURI)

	if _, err := Load(); !errors.Is(err, ErrMissingClientID) {
		t.Fatalf("esperava ErrMissingClientID, veio %v", err)
	}

	// placeholder do .env.example também conta como não configurado
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte("SPOTIFY_CLIENT_ID=COLE_SEU_CLIENT_ID_AQUI\n"), 0o600)
	os.Unsetenv(EnvClientID)
	if _, err := Load(); !errors.Is(err, ErrMissingClientID) {
		t.Fatalf("placeholder deveria falhar, veio %v", err)
	}

	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte("SPOTIFY_CLIENT_ID=do-config-dir\n"), 0o600)
	os.Unsetenv(EnvClientID)
	c, err := Load()
	if err != nil || c.ClientID != "do-config-dir" || c.RedirectURI != DefaultRedirectURI {
		t.Fatalf("got %+v, %v", c, err)
	}

	// .env local vence o do config dir; variável de ambiente vence ambos
	_ = os.WriteFile(".env", []byte("SPOTIFY_CLIENT_ID=local\n"), 0o600)
	os.Unsetenv(EnvClientID)
	if c, _ := Load(); c.ClientID != "local" {
		t.Errorf("local deveria vencer: %+v", c)
	}
	t.Setenv(EnvClientID, "do-env")
	if c, _ := Load(); c.ClientID != "do-env" {
		t.Errorf("env deveria vencer: %+v", c)
	}
}
