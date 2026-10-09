package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateEnvFilePreservesOtherLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	orig := "# meu comentário\nSPOTIFY_CLIENT_ID=antigo\nOUTRA=1\n"
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	err := UpdateEnvFile(p, map[string]string{EnvClientID: "novo", EnvLastFMKey: "k", EnvContact: ""})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	for _, want := range []string{"# meu comentário", "SPOTIFY_CLIENT_ID=novo", "OUTRA=1", "LASTFM_API_KEY=k"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("faltou %q em:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "antigo") {
		t.Errorf("valor antigo permaneceu:\n%s", got)
	}
}

func TestUpdateEnvFileEmptyRemovesKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	_ = UpdateEnvFile(p, map[string]string{EnvClientID: "x", EnvContact: "a@b.c"})
	_ = UpdateEnvFile(p, map[string]string{EnvContact: ""})
	m, err := ReadEnvFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m[EnvContact]; ok || m[EnvClientID] != "x" {
		t.Errorf("mapa inesperado: %v", m)
	}
}

func TestValidateRedirectURI(t *testing.T) {
	for uri, ok := range map[string]bool{
		"http://127.0.0.1:8888/callback": true,
		"http://[::1]:8888/cb":           true,
		"http://localhost:8888/cb":       false,
		"https://exemplo.com/cb":         false,
	} {
		if (ValidateRedirectURI(uri) == nil) != ok {
			t.Errorf("%s: esperado ok=%v", uri, ok)
		}
	}
}

func TestLooksLikeClientID(t *testing.T) {
	if !LooksLikeClientID("0123456789abcdef0123456789ABCDEF") || LooksLikeClientID("curto") {
		t.Error("validação de Client ID incorreta")
	}
}
