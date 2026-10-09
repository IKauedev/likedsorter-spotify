package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v1.0.0", "v1.0.1", true}, {"1.2.3", "v1.2.3", false}, {"v2.0.0", "v1.9.9", false},
		{"dev", "v0.1.0", true}, {"v1.0.0-3-gabc-dirty", "v1.0.1", true}, {"v1.0.0", "nightly", false},
	} {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q,%q)=%v, quero %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if AssetName("windows", "amd64") != "likedsorter_windows_amd64.exe" || AssetName("linux", "arm64") != "likedsorter_linux_arm64" {
		t.Error("nome de asset inesperado")
	}
}

func fakeGitHub(t *testing.T, body []byte, sum string) *Updater {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","html_url":"https://x","assets":[
		 {"name":"likedsorter_linux_amd64","browser_download_url":"%[1]s/bin"},
		 {"name":"checksums.txt","browser_download_url":"%[1]s/sums"}]}`, srv.URL)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%s  likedsorter_linux_amd64\n", sum) })
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return &Updater{Repo: "o/r", BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	body := []byte("binario-novo")
	h := sha256.Sum256(body)
	u := fakeGitHub(t, body, hex.EncodeToString(h[:]))
	rel, err := u.Latest(context.Background())
	if err != nil || rel.Tag != "v9.9.9" {
		t.Fatal(err, rel)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "likedsorter")
	_ = os.WriteFile(dest, []byte("antigo"), 0o755)

	tmp, err := u.Download(context.Background(), rel, "linux", "amd64", dest)
	if err != nil {
		t.Fatal(err)
	}
	if err := Replace(tmp, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "binario-novo" {
		t.Errorf("conteúdo após Replace = %q", b)
	}
}

func TestDownloadRejectsBadChecksum(t *testing.T) {
	u := fakeGitHub(t, []byte("adulterado"), "0000000000000000000000000000000000000000000000000000000000000000")
	rel, _ := u.Latest(context.Background())
	dir := t.TempDir()
	if _, err := u.Download(context.Background(), rel, "linux", "amd64", filepath.Join(dir, "x")); err == nil {
		t.Fatal("hash divergente deveria ser recusado")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("sobrou arquivo temporário: %v", entries)
	}
}

func TestDownloadMissingAsset(t *testing.T) {
	u := fakeGitHub(t, []byte("x"), "00")
	rel, _ := u.Latest(context.Background())
	if _, err := u.Download(context.Background(), rel, "plan9", "mips", t.TempDir()+"/x"); err == nil {
		t.Fatal("plataforma sem binário deveria falhar")
	}
}
