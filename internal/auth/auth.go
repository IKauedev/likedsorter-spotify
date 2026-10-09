// Package auth implementa OAuth 2.0 Authorization Code com PKCE para o Spotify,
// persistência do token em disco e renovação automática.
package auth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

const (
	AuthURL  = "https://accounts.spotify.com/authorize"
	TokenURL = "https://accounts.spotify.com/api/token" //nolint:gosec // G101: é uma URL pública, não uma credencial
)

// Scopes são os escopos mínimos usados pela ferramenta.
var Scopes = []string{
	"user-library-read",
	"playlist-read-private",
	"playlist-modify-private",
	"playlist-modify-public",
}

// ListeningScopes são opcionais: habilitam /me/top e /me/player/recently-played
// (playlists por frequência de reprodução). Quem já tinha feito login não precisa
// refazê-lo, a menos que use esses recursos.
var ListeningScopes = []string{
	"user-top-read",
	"user-read-recently-played",
}

// ErrNotLoggedIn indica ausência de token salvo ou sessão não renovável.
var ErrNotLoggedIn = errors.New("não autenticado: execute `likedsorter auth login`")

// OAuthConfig monta a configuração OAuth. PKCE dispensa Client Secret, então
// o client_id vai nos parâmetros do corpo (AuthStyleInParams).
func OAuthConfig(clientID, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    clientID,
		RedirectURL: redirectURI,
		Scopes:      append(append([]string{}, Scopes...), ListeningScopes...),
		Endpoint: oauth2.Endpoint{
			AuthURL:   AuthURL,
			TokenURL:  TokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

// ParseRedirect valida o redirect URI e devolve o endereço de escuta e o path.
// Regras do Spotify: HTTP só em loopback literal (127.0.0.1 ou [::1]);
// "localhost" é proibido. Exigimos porta explícita para poder escutar.
func ParseRedirect(raw string) (addr, path string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("redirect URI inválido: %w", err)
	}
	if u.Scheme != "http" {
		return "", "", fmt.Errorf("redirect URI %q: o callback local deve usar http://127.0.0.1:PORTA/caminho", raw)
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "::1" {
		return "", "", fmt.Errorf("redirect URI %q: use 127.0.0.1 ou [::1] (o Spotify não aceita localhost)", raw)
	}
	if u.Port() == "" {
		return "", "", fmt.Errorf("redirect URI %q: informe a porta, ex.: http://127.0.0.1:8888/callback", raw)
	}
	path = u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return u.Host, path, nil
}

// MissingListening devolve os escopos de reprodução que faltam em granted.
func MissingListening(granted string) []string {
	have := map[string]bool{}
	for _, s := range strings.Fields(granted) {
		have[s] = true
	}
	var miss []string
	for _, s := range ListeningScopes {
		if !have[s] {
			miss = append(miss, s)
		}
	}
	return miss
}

// MissingScopes devolve os escopos requeridos que não constam em granted.
func MissingScopes(granted string) []string {
	have := map[string]bool{}
	for _, s := range strings.Fields(granted) {
		have[s] = true
	}
	var miss []string
	for _, s := range Scopes {
		if !have[s] {
			miss = append(miss, s)
		}
	}
	return miss
}
