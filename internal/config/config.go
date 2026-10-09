// Package config carrega credenciais e caminhos. Precedência (Fase 1):
// variáveis de ambiente > .env no diretório atual > .env no diretório de config.
// Flags e YAML entram na Fase 8.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvClientID    = "SPOTIFY_CLIENT_ID"
	EnvRedirectURI = "SPOTIFY_REDIRECT_URI"
	EnvConfigDir   = "LIKEDSORTER_CONFIG_DIR"
	EnvCacheDir    = "LIKEDSORTER_CACHE_DIR"
	EnvLastFMKey   = "LASTFM_API_KEY"
	EnvContact     = "LIKEDSORTER_CONTACT"
	EnvDiscogs     = "DISCOGS_TOKEN"
	EnvAIProvider  = "AI_PROVIDER"
	EnvAIKey       = "AI_API_KEY"
	EnvAIModel     = "AI_MODEL"
	EnvAIBaseURL   = "AI_BASE_URL"

	DefaultContact = "https://github.com/IKauedev/likedsorter-spotify"

	DefaultRedirectURI = "http://127.0.0.1:8888/callback"
	placeholderID      = "COLE_SEU_CLIENT_ID_AQUI"
)

// ErrMissingClientID indica que o Client ID não foi configurado.
var ErrMissingClientID = errors.New("SPOTIFY_CLIENT_ID não configurado")

// Config reúne as credenciais necessárias para o OAuth.
type Config struct {
	ClientID    string
	RedirectURI string
	LastFMKey   string // opcional (fonte de gênero lastfm)
	DiscogsKey  string // opcional (fonte de gênero discogs)
	AIProvider  string // opcional: anthropic | openai | ollama
	AIKey       string
	AIModel     string
	AIBaseURL   string
	Contact     string // contato para o User-Agent do MusicBrainz (URL ou e-mail)
}

// Dir devolve o diretório de configuração do likedsorter.
func Dir() (string, error) {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("diretório de config do usuário: %w", err)
	}
	return filepath.Join(base, "likedsorter"), nil
}

// CacheDir devolve o diretório do cache em disco (faixas e artistas).
func CacheDir() (string, error) {
	if d := os.Getenv(EnvCacheDir); d != "" {
		return d, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("diretório de cache do usuário: %w", err)
	}
	return filepath.Join(base, "likedsorter"), nil
}

// Load lê os arquivos .env (sem sobrescrever variáveis já definidas) e
// monta a Config. Retorna ErrMissingClientID (com instruções) se faltar o ID.
func Load() (Config, error) {
	loadEnvFiles()
	c := Config{
		ClientID:    strings.TrimSpace(os.Getenv(EnvClientID)),
		RedirectURI: strings.TrimSpace(os.Getenv(EnvRedirectURI)),
		LastFMKey:   strings.TrimSpace(os.Getenv(EnvLastFMKey)),
		Contact:     strings.TrimSpace(os.Getenv(EnvContact)),
		DiscogsKey:  strings.TrimSpace(os.Getenv(EnvDiscogs)),
		AIProvider:  strings.ToLower(strings.TrimSpace(os.Getenv(EnvAIProvider))),
		AIKey:       strings.TrimSpace(os.Getenv(EnvAIKey)),
		AIModel:     strings.TrimSpace(os.Getenv(EnvAIModel)),
		AIBaseURL:   strings.TrimSpace(os.Getenv(EnvAIBaseURL)),
	}
	if c.Contact == "" {
		c.Contact = DefaultContact
	}
	if c.RedirectURI == "" {
		c.RedirectURI = DefaultRedirectURI
	}
	if c.ClientID == "" || c.ClientID == placeholderID {
		dir, _ := Dir()
		return c, fmt.Errorf("%w: rode `likedsorter start` (assistente) ou `likedsorter setup`; também vale copiar .env.example para .env (aqui ou em %s) e preencher o Client ID", ErrMissingClientID, dir)
	}
	return c, nil
}

func loadEnvFiles() {
	paths := []string{".env"}
	if dir, err := Dir(); err == nil {
		paths = append(paths, filepath.Join(dir, ".env"))
	}
	for _, p := range paths {
		_ = LoadDotEnv(p) // arquivo ausente não é erro
	}
}

// LoadDotEnv lê KEY=VALUE de path e define apenas as variáveis ainda não definidas.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := parseEnvLine(sc.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(k); !set {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}

func parseEnvLine(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	k, v = strings.TrimSpace(k), strings.TrimSpace(v)
	if k == "" {
		return "", "", false
	}
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	} else if i := strings.Index(v, " #"); i >= 0 { // comentário inline
		v = strings.TrimSpace(v[:i])
	}
	return k, v, true
}
