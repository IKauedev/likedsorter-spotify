// Package update consulta a última release no GitHub, baixa o binário da
// plataforma, confere o SHA-256 e substitui o executável em uso.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// DefaultRepo é o repositório das releases.
const DefaultRepo = "IKauedev/likedsorter-spotify"

const maxBinary = 200 << 20 // 200 MiB

// Release é o recorte da API do GitHub que usamos.
type Release struct {
	Tag    string  `json:"tag_name"`
	URL    string  `json:"html_url"`
	Assets []Asset `json:"assets"`
}

// Asset é um arquivo anexado à release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Updater fala com o GitHub. BaseURL e HTTP existem para testes.
type Updater struct {
	Repo    string
	BaseURL string // padrão https://api.github.com
	HTTP    *http.Client
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{} // sem timeout global: o downloads usam o contexto
}

func (u *Updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "likedsorter-updater")
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("release não encontrada (o repositório ainda não publicou versões?)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d em %s", resp.StatusCode, url)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("resposta maior que o limite de %d bytes", limit)
	}
	return b, nil
}

// Latest devolve a última release publicada.
func (u *Updater) Latest(ctx context.Context) (*Release, error) {
	base, repo := u.BaseURL, u.Repo
	if base == "" {
		base = "https://api.github.com"
	}
	if repo == "" {
		repo = DefaultRepo
	}
	b, err := u.get(ctx, base+"/repos/"+repo+"/releases/latest", 4<<20)
	if err != nil {
		return nil, err
	}
	var r Release
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("resposta inválida do GitHub: %w", err)
	}
	if r.Tag == "" {
		return nil, errors.New("release sem tag")
	}
	return &r, nil
}

// AssetName é o nome do binário da plataforma, ex.: likedsorter_windows_amd64.exe.
func AssetName(goos, goarch string) string {
	n := "likedsorter_" + goos + "_" + goarch
	if goos == "windows" {
		n += ".exe"
	}
	return n
}

// ParseVersion lê "v1.2.3" / "1.2.3" (ignora sufixos como -rc1 ou -dirty). ok=false se não for semver.
func ParseVersion(s string) (v [3]int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// Newer informa se latest é uma versão maior que current.
func Newer(current, latest string) bool {
	c, ok1 := ParseVersion(current)
	l, ok2 := ParseVersion(latest)
	if !ok2 {
		return false
	}
	if !ok1 {
		return true // "dev" ou versão desconhecida: oferece atualizar
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func find(r *Release, name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// checksumFor extrai o hash de name de um arquivo no formato `sha256sum`.
func checksumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt não lista %s", name)
}

// Download baixa o binário da plataforma para um arquivo temporário ao lado de dest
// e confere o SHA-256 contra checksums.txt. Devolve o caminho do arquivo verificado.
func (u *Updater) Download(ctx context.Context, r *Release, goos, goarch, dest string) (string, error) {
	name := AssetName(goos, goarch)
	bin, ok := find(r, name)
	if !ok {
		return "", fmt.Errorf("a release %s não tem o arquivo %s para esta plataforma", r.Tag, name)
	}
	sumAsset, ok := find(r, "checksums.txt")
	if !ok {
		return "", fmt.Errorf("a release %s não tem checksums.txt; recuso atualizar sem verificação", r.Tag)
	}
	for _, a := range []Asset{bin, sumAsset} {
		if !strings.HasPrefix(a.URL, "https://") {
			return "", fmt.Errorf("URL de download insegura: %s", a.URL)
		}
	}
	sums, err := u.get(ctx, sumAsset.URL, 1<<20)
	if err != nil {
		return "", fmt.Errorf("baixar checksums.txt: %w", err)
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return "", err
	}
	data, err := u.get(ctx, bin.URL, maxBinary)
	if err != nil {
		return "", fmt.Errorf("baixar %s: %w", name, err)
	}
	h := sha256.Sum256(data)
	if got := hex.EncodeToString(h[:]); got != want {
		return "", fmt.Errorf("SHA-256 não confere (esperado %s, obtido %s); download descartado", want, got)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".likedsorter-new-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil { //nolint:gosec // G302: executável precisa ser executável
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// Replace troca o executável dest por newPath. No Windows um executável em uso não
// pode ser sobrescrito, mas pode ser renomeado: o antigo vira dest+".old".
func Replace(newPath, dest string) error {
	if runtime.GOOS == "windows" {
		old := dest + ".old"
		_ = os.Remove(old)
		if err := os.Rename(dest, old); err != nil {
			return fmt.Errorf("mover o executável atual: %w", err)
		}
		if err := os.Rename(newPath, dest); err != nil {
			_ = os.Rename(old, dest) // desfaz
			return fmt.Errorf("instalar o novo executável: %w", err)
		}
		return nil
	}
	return os.Rename(newPath, dest)
}

// CleanupOld apaga o dest+".old" deixado por uma atualização anterior (melhor esforço).
func CleanupOld(dest string) { _ = os.Remove(dest + ".old") }
