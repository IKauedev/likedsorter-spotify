package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

func TestParseRedirect(t *testing.T) {
	tests := []struct {
		in       string
		wantAddr string
		wantPath string
		wantErr  bool
	}{
		{"http://127.0.0.1:8888/callback", "127.0.0.1:8888", "/callback", false},
		{"http://[::1]:9000/cb", "[::1]:9000", "/cb", false},
		{"http://127.0.0.1:8888", "127.0.0.1:8888", "/", false},
		{"http://localhost:8888/callback", "", "", true},
		{"https://127.0.0.1:8888/callback", "", "", true},
		{"http://127.0.0.1/callback", "", "", true}, // sem porta
		{"http://example.com:80/cb", "", "", true},
	}
	for _, tt := range tests {
		addr, path, err := ParseRedirect(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("%q: err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			continue
		}
		if addr != tt.wantAddr || path != tt.wantPath {
			t.Errorf("%q: got (%q,%q), want (%q,%q)", tt.in, addr, path, tt.wantAddr, tt.wantPath)
		}
	}
}

func TestMissingScopes(t *testing.T) {
	if got := MissingScopes(strings.Join(Scopes, " ")); len(got) != 0 {
		t.Errorf("todos concedidos, mas faltam %v", got)
	}
	if got := MissingScopes("user-library-read"); len(got) != len(Scopes)-1 {
		t.Errorf("got %v", got)
	}
}

func TestStoreRoundTripAndPerms(t *testing.T) {
	s := NewStore(filepath.Join(testutil.TempDir(t), "sub", "token.json"))
	if _, err := s.Load(); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("esperava ErrNotLoggedIn, veio %v", err)
	}
	want := &Saved{Token: &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour).Truncate(time.Second)}, Scope: "x y"}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token.AccessToken != "a" || got.Token.RefreshToken != "r" || got.Scope != "x y" {
		t.Errorf("round trip falhou: %+v", got)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(s.Path)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("permissão %v, want 0600", fi.Mode().Perm())
		}
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil { // idempotente
		t.Fatal(err)
	}
}

func TestStoreCorrupted(t *testing.T) {
	p := filepath.Join(testutil.TempDir(t), "token.json")
	_ = os.WriteFile(p, []byte("{nope"), 0o600)
	if _, err := NewStore(p).Load(); err == nil || errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("esperava erro de corrupção, veio %v", err)
	}
}

// fakeSpotify simula o endpoint de token e valida o PKCE (S256).
func fakeSpotify(t *testing.T, challenge *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		jsonErr := func(code string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
		}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != *challenge {
				jsonErr("invalid_grant")
				return
			}
			if r.PostForm.Get("client_id") != "cid" {
				jsonErr("invalid_client")
				return
			}
		case "refresh_token":
			if r.PostForm.Get("refresh_token") != "refresh-1" {
				jsonErr("invalid_grant")
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + r.PostForm.Get("grant_type"), "token_type": "Bearer",
			"expires_in": 3600, "refresh_token": "refresh-1", "scope": strings.Join(Scopes, " "),
		})
	}))
}

func testConfig(tokenURL string) *oauth2.Config {
	cfg := OAuthConfig("cid", "http://127.0.0.1:0/callback")
	cfg.Endpoint.AuthURL = "http://auth.invalid/authorize"
	cfg.Endpoint.TokenURL = tokenURL
	return cfg
}

func freeRedirect(t *testing.T) string {
	t.Helper()
	// usa uma porta efêmera: reserva, fecha e reutiliza
	srv := httptest.NewServer(http.NotFoundHandler())
	u, _ := url.Parse(srv.URL)
	srv.Close()
	return "http://127.0.0.1:" + u.Port() + "/callback"
}

func TestLoginFullFlow(t *testing.T) {
	var challenge string
	api := fakeSpotify(t, &challenge)
	defer api.Close()

	cfg := testConfig(api.URL)
	cfg.RedirectURL = freeRedirect(t)
	store := NewStore(filepath.Join(testutil.TempDir(t), "token.json"))

	open := func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
			t.Errorf("parâmetros PKCE ausentes: %v", q)
		}
		if len(q.Get("state")) < 32 {
			t.Errorf("state curto: %q", q.Get("state"))
		}
		if q.Get("scope") != strings.Join(append(append([]string{}, Scopes...), ListeningScopes...), " ") {
			t.Errorf("scope=%q", q.Get("scope"))
		}
		challenge = q.Get("code_challenge")

		// requisição alheia com state errado não pode encerrar o fluxo
		resp, err := http.Get(cfg.RedirectURL + "?code=evil&state=wrong")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("state inválido deveria dar 400, deu %d", resp.StatusCode)
		}

		resp, err = http.Get(cfg.RedirectURL + "?code=abc&state=" + url.QueryEscape(q.Get("state")))
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}

	tok, err := Login(context.Background(), cfg, store, LoginOptions{Timeout: 5 * time.Second, Open: open})
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access-authorization_code" {
		t.Errorf("token inesperado: %q", tok.AccessToken)
	}
	sv, err := store.Load()
	if err != nil || sv.Token.RefreshToken != "refresh-1" || len(MissingScopes(sv.Scope)) != 0 {
		t.Errorf("token não persistido corretamente: %+v, %v", sv, err)
	}
}

func TestLoginDenied(t *testing.T) {
	var challenge string
	api := fakeSpotify(t, &challenge)
	defer api.Close()
	cfg := testConfig(api.URL)
	cfg.RedirectURL = freeRedirect(t)

	open := func(authURL string) error {
		u, _ := url.Parse(authURL)
		resp, err := http.Get(cfg.RedirectURL + "?error=access_denied&state=" + url.QueryEscape(u.Query().Get("state")))
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	_, err := Login(context.Background(), cfg, NewStore(filepath.Join(testutil.TempDir(t), "t.json")), LoginOptions{Timeout: 5 * time.Second, Open: open})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("esperava erro access_denied, veio %v", err)
	}
}

func TestLoginTimeout(t *testing.T) {
	cfg := testConfig("http://unused.invalid")
	cfg.RedirectURL = freeRedirect(t)
	_, err := Login(context.Background(), cfg, NewStore(filepath.Join(testutil.TempDir(t), "t.json")), LoginOptions{Timeout: 50 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "tempo esgotado") {
		t.Fatalf("esperava timeout, veio %v", err)
	}
}

func TestTokenSourceRefreshPersists(t *testing.T) {
	var challenge string
	api := fakeSpotify(t, &challenge)
	defer api.Close()
	cfg := testConfig(api.URL)

	store := NewStore(filepath.Join(testutil.TempDir(t), "token.json"))
	_ = store.Save(&Saved{
		Token: &oauth2.Token{AccessToken: "old", RefreshToken: "refresh-1", Expiry: time.Now().Add(-time.Minute)},
		Scope: "user-library-read",
	})
	ts, err := TokenSource(context.Background(), cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access-refresh_token" {
		t.Errorf("não renovou: %q", tok.AccessToken)
	}
	sv, _ := store.Load()
	if sv.Token.AccessToken != "access-refresh_token" {
		t.Errorf("token renovado não foi persistido: %q", sv.Token.AccessToken)
	}
}

func TestTokenSourceInvalidGrant(t *testing.T) {
	var challenge string
	api := fakeSpotify(t, &challenge)
	defer api.Close()
	store := NewStore(filepath.Join(testutil.TempDir(t), "token.json"))
	_ = store.Save(&Saved{Token: &oauth2.Token{AccessToken: "old", RefreshToken: "revogado", Expiry: time.Now().Add(-time.Minute)}})

	ts, _ := TokenSource(context.Background(), testConfig(api.URL), store)
	if _, err := ts.Token(); err == nil || !strings.Contains(err.Error(), "auth login") {
		t.Fatalf("esperava orientação de novo login, veio %v", err)
	}
}
