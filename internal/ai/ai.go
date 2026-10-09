// Package ai fala com modelos de linguagem (Claude, OpenAI e compatíveis, Ollama local) usando
// HTTP puro. A IA só PROPÕE: nada do que ela devolve é aplicado sem validação e revisão do usuário.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provedores aceitos em AI_PROVIDER.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderOllama    = "ollama"
)

// Modelos padrão (sobrescreva com AI_MODEL).
var defaultModels = map[string]string{
	ProviderAnthropic: "claude-haiku-5-5",
	ProviderOpenAI:    "gpt-4o-mini",
	ProviderOllama:    "llama3.1",
}

var defaultBaseURLs = map[string]string{
	ProviderAnthropic: "https://api.anthropic.com",
	ProviderOpenAI:    "https://api.openai.com/v1",
	ProviderOllama:    "http://localhost:11434/v1",
}

// Config escolhe e configura o provedor.
type Config struct {
	Provider string
	APIKey   string
	Model    string
	BaseURL  string
}

// Provider devolve o texto da resposta do modelo.
type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, system, user string) (string, error)
}

// Validate confere a configuração sem fazer chamadas.
func (c Config) Validate() error {
	switch c.Provider {
	case "":
		return errors.New("IA não configurada: rode `likedsorter ai setup` (AI_PROVIDER = anthropic, openai ou ollama)")
	case ProviderAnthropic, ProviderOpenAI:
		if strings.TrimSpace(c.APIKey) == "" && !c.IsLocal() {
			return fmt.Errorf("falta a chave de API do provedor %s: rode `likedsorter ai setup`", c.Provider)
		}
	case ProviderOllama:
	default:
		return fmt.Errorf("AI_PROVIDER %q desconhecido (use anthropic, openai ou ollama)", c.Provider)
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("AI_BASE_URL inválida: %q", c.BaseURL)
		}
		if u.Scheme == "http" && !hostIsLocal(u.Hostname()) {
			return errors.New("AI_BASE_URL com http:// só é aceita para localhost (a chave de API trafegaria sem criptografia)")
		}
	}
	return nil
}

// IsLocal informa se as requisições ficam na sua máquina (nada sai para a internet).
func (c Config) IsLocal() bool {
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURLs[c.Provider]
	}
	u, err := url.Parse(base)
	return err == nil && hostIsLocal(u.Hostname())
}

func hostIsLocal(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

// Destination descreve para onde os dados vão (para o aviso de privacidade).
func (c Config) Destination() string {
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURLs[c.Provider]
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host
	}
	return base
}

// New cria o provedor. hc nil usa um cliente com timeout de 2 minutos.
func New(c Config, hc *http.Client) (Provider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	if c.Model == "" {
		c.Model = defaultModels[c.Provider]
	}
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURLs[c.Provider]
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.Provider == ProviderAnthropic {
		return &anthropic{c: c, hc: hc}, nil
	}
	return &openAI{c: c, hc: hc}, nil
}

const maxResponse = 4 << 20

// postJSON faz o POST com até 3 tentativas em 429/5xx. A chave nunca aparece em erros.
func postJSON(ctx context.Context, hc *http.Client, endpoint string, headers map[string]string, body, out any, secret string) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := hc.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			last = redact(err, secret)
		} else {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
			resp.Body.Close()
			switch {
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				if err := json.Unmarshal(b, out); err != nil {
					return fmt.Errorf("resposta inválida do provedor: %w", err)
				}
				return nil
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				last = fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiMessage(b))
			default:
				return fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiMessage(b))
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return fmt.Errorf("desistindo após 3 tentativas: %w", last)
}

func redact(err error, secret string) error {
	if secret == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "***"))
}

func apiMessage(b []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ----------------------------------------------------------------- Anthropic

type anthropic struct {
	c  Config
	hc *http.Client
}

func (a *anthropic) Name() string  { return ProviderAnthropic }
func (a *anthropic) Model() string { return a.c.Model }

func (a *anthropic) Complete(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model": a.c.Model, "max_tokens": 4096, "system": system,
		"messages": []map[string]string{{"role": "user", "content": user}},
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	hdr := map[string]string{"x-api-key": a.c.APIKey, "anthropic-version": "2023-06-01"}
	if err := postJSON(ctx, a.hc, a.c.BaseURL+"/v1/messages", hdr, body, &resp, a.c.APIKey); err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}
	var sb strings.Builder
	for _, c := range resp.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	if sb.Len() == 0 {
		return "", errors.New("anthropic: resposta sem texto")
	}
	return sb.String(), nil
}

// ------------------------------------------------- OpenAI e compatíveis (Ollama)

type openAI struct {
	c  Config
	hc *http.Client
}

func (o *openAI) Name() string  { return o.c.Provider }
func (o *openAI) Model() string { return o.c.Model }

func (o *openAI) Complete(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model": o.c.Model, "temperature": 0,
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	hdr := map[string]string{}
	if o.c.APIKey != "" {
		hdr["Authorization"] = "Bearer " + o.c.APIKey
	}
	if err := postJSON(ctx, o.hc, o.c.BaseURL+"/chat/completions", hdr, body, &resp, o.c.APIKey); err != nil {
		return "", fmt.Errorf("%s: %w", o.c.Provider, err)
	}
	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("%s: resposta sem texto", o.c.Provider)
	}
	return resp.Choices[0].Message.Content, nil
}
