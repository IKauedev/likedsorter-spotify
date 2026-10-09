package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
)

// EnvFilePath devolve o .env do diretório de config (onde `setup` grava as credenciais).
func EnvFilePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".env"), nil
}

var clientIDRe = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// LooksLikeClientID informa se s tem o formato de um Client ID do Spotify (32 hex).
func LooksLikeClientID(s string) bool { return clientIDRe.MatchString(s) }

// ValidateRedirectURI aceita só loopback por IP (o Spotify proíbe "localhost").
func ValidateRedirectURI(s string) error {
	if !strings.HasPrefix(s, "http://127.0.0.1:") && !strings.HasPrefix(s, "http://[::1]:") {
		return errors.New("use http://127.0.0.1:PORTA/caminho (o Spotify não aceita \"localhost\")")
	}
	return nil
}

// ReadEnvFile lê KEY=VALUE de path sem tocar no ambiente do processo.
// Arquivo ausente devolve mapa vazio.
func ReadEnvFile(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := parseEnvLine(strings.TrimRight(line, "\r")); ok {
			m[k] = v
		}
	}
	return m, nil
}

const envHeader = `# likedsorter: credenciais (gerado por "likedsorter setup").
# Este arquivo contém dados pessoais: NUNCA o compartilhe nem o versione.
# Variáveis de ambiente reais têm precedência sobre ele.
`

// UpdateEnvFile grava (com modo 0600) as chaves de values em path, preservando
// as demais linhas e comentários. Valores vazios removem a linha da chave.
func UpdateEnvFile(path string, values map[string]string) error {
	var lines []string
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		lines = strings.Split(strings.TrimRight(envHeader, "\n"), "\n")
	case err != nil:
		return err
	default:
		lines = strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	}

	done := map[string]bool{}
	var out []string
	for _, line := range lines {
		k, _, ok := parseEnvLine(line)
		v, mine := values[k]
		if !ok || !mine {
			out = append(out, line)
			continue
		}
		done[k] = true
		if v != "" {
			out = append(out, k+"="+v)
		}
	}
	for _, k := range []string{EnvClientID, EnvRedirectURI, EnvLastFMKey, EnvDiscogs, EnvAIProvider, EnvAIKey, EnvAIModel, EnvAIBaseURL, EnvContact} {
		if v, mine := values[k]; mine && v != "" && !done[k] {
			out = append(out, k+"="+v)
		}
	}
	return fsutil.WriteFileAtomic(path, []byte(strings.Join(out, "\n")+"\n"))
}
