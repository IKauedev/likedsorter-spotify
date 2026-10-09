package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
)

type fakeProvider struct {
	reply string
	got   string
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Model() string { return "fake-1" }
func (f *fakeProvider) Complete(_ context.Context, system, user string) (string, error) {
	f.got = system + "\n" + user
	return f.reply, nil
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		c  Config
		ok bool
	}{
		{Config{}, false},
		{Config{Provider: "anthropic"}, false}, // sem chave
		{Config{Provider: "anthropic", APIKey: "k"}, true},
		{Config{Provider: "ollama"}, true}, // local não precisa de chave
		{Config{Provider: "openai", APIKey: "k", BaseURL: "http://exemplo.com/v1"}, false}, // http fora do localhost
		{Config{Provider: "openai", APIKey: "k", BaseURL: "http://localhost:8080/v1"}, true},
		{Config{Provider: "openai", APIKey: "k", BaseURL: "https://exemplo.com/v1"}, true},
		{Config{Provider: "gemini", APIKey: "k"}, false},
	}
	for i, c := range cases {
		if err := c.c.Validate(); (err == nil) != c.ok {
			t.Errorf("caso %d (%+v): err=%v", i, c.c, err)
		}
	}
	if !(Config{Provider: "ollama"}).IsLocal() || (Config{Provider: "anthropic", APIKey: "k"}).IsLocal() {
		t.Error("IsLocal incorreto")
	}
}

func TestAnthropicRequestAndResponse(t *testing.T) {
	var gotKey, gotVersion string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotVersion = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"olá"},{"type":"text","text":" mundo"}]}`))
	}))
	defer srv.Close()
	p, err := New(Config{Provider: "anthropic", APIKey: "SEGREDO", BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	// BaseURL http://127.0.0.1 é local, então é aceita
	out, err := p.Complete(context.Background(), "sys", "user")
	if err != nil || out != "olá mundo" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if gotKey != "SEGREDO" || gotVersion == "" || body["system"] != "sys" || body["model"] == "" {
		t.Errorf("requisição: key=%q ver=%q body=%v", gotKey, gotVersion, body)
	}
}

func TestOpenAICompatAndErrorsHideKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer TOKEN123" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"chave inválida TOKEN123"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	p, _ := New(Config{Provider: "openai", APIKey: "TOKEN123", BaseURL: srv.URL}, srv.Client())
	if out, err := p.Complete(context.Background(), "s", "u"); err != nil || out != "ok" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	bad, _ := New(Config{Provider: "openai", APIKey: "errada", BaseURL: srv.URL}, srv.Client())
	if _, err := bad.Complete(context.Background(), "s", "u"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("deveria falhar com 401: %v", err)
	}
}

func TestClassifyArtistsSanitizes(t *testing.T) {
	f := &fakeProvider{reply: "```json\n" + `{"artists":[
	 {"name":"Djavan","genres":["MPB"," mpb ","Bossa Nova","x","jazz","pop"]},
	 {"name":"Inventado","genres":["rock"]},
	 {"name":"emicida","genres":["hip hop","<script>alert(1)</script>"]},
	 {"name":"Desconhecido","genres":[]}]}` + "\n```"}
	got, err := ClassifyArtists(context.Background(), f, []ArtistSample{{Name: "Djavan"}, {Name: "Emicida"}, {Name: "Desconhecido"}})
	if err != nil {
		t.Fatal(err)
	}
	if g := got["Djavan"]; strings.Join(g, ",") != "mpb,bossa nova,jazz" {
		t.Errorf("Djavan = %v (dedupe, tamanho mínimo e no máximo 3)", g)
	}
	if _, ok := got["Inventado"]; ok {
		t.Error("artista que não foi pedido deve ser descartado")
	}
	if g := got["Emicida"]; strings.Join(g, ",") != "hip hop" {
		t.Errorf("Emicida = %v (gênero malformado deve ser descartado)", g)
	}
	if _, ok := got["Desconhecido"]; ok {
		t.Error("sem gêneros não entra")
	}
	if !strings.Contains(f.got, "<data>") || !strings.Contains(f.got, "não confiável") {
		t.Error("o prompt deve isolar os dados e avisar que não são confiáveis")
	}
	if _, err := ClassifyArtists(context.Background(), &fakeProvider{reply: "não é json"}, []ArtistSample{{Name: "A"}}); err == nil {
		t.Error("resposta fora do formato deveria dar erro")
	}
}

func TestGenerateRulesPassesFeedbackAndStripsFence(t *testing.T) {
	f := &fakeProvider{reply: "Aqui está:\n```yaml\nregras:\n  - nome: A\n    artista: [x]\n```\n"}
	y, err := GenerateRules(context.Background(), f, LibrarySummary{TotalTracks: 1}, "estudo", "nome repetido")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(y, "```") || !strings.HasPrefix(y, "regras:") {
		t.Errorf("yaml = %q", y)
	}
	if !strings.Contains(f.got, "nome repetido") || !strings.Contains(f.got, "estudo") {
		t.Error("instrução e feedback precisam chegar ao modelo")
	}
	if _, err := GenerateRules(context.Background(), f, LibrarySummary{}, "  ", ""); err == nil {
		t.Error("instrução vazia deveria falhar")
	}
}

func TestGenreStoreApplyOnlyFillsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ai.json")
	st, _ := LoadStore(p)
	st.Put("Djavan", []string{"mpb"}, "m")
	st.Put("Outro", []string{"rock"}, "m")
	if err := st.Save(p); err != nil {
		t.Fatal(err)
	}
	st2, err := LoadStore(p)
	if err != nil {
		t.Fatal(err)
	}
	infos := map[string]enrich.Info{
		"1": {Name: "DJAVAN"},
		"2": {Name: "Outro", Genres: []string{"punk"}},
		"3": {Name: "Ninguém"},
	}
	if n := st2.Apply(infos); n != 1 {
		t.Fatalf("Apply = %d", n)
	}
	if infos["1"].Genres[0] != "mpb" || infos["1"].Source != "ai" || infos["2"].Genres[0] != "punk" || len(infos["3"].Genres) != 0 {
		t.Errorf("infos = %+v", infos)
	}
	_ = os.Remove(p)
}
