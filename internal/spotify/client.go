// Package spotify é um cliente HTTP fino da Web API: retry, rate limit e
// paginação. Só implementa os endpoints que a ferramenta usa.
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const DefaultBaseURL = "https://api.spotify.com/v1"

// Options configura o Client. Zero values têm padrões sensatos.
type Options struct {
	BaseURL       string
	Market        string        // padrão "from_token"
	MaxRetries    int           // tentativas extras após a primeira (padrão 5)
	MaxRetryAfter time.Duration // acima disso, 429 vira erro em vez de espera (padrão 2 min)
	UserAgent     string
	Limiter       *rate.Limiter // padrão 8 req/s, burst 8
	Logger        *slog.Logger

	// Ganchos para teste.
	Sleep  func(ctx context.Context, d time.Duration) error
	Jitter func(max time.Duration) time.Duration // devolve valor em [0, max]
}

// Client fala com a Web API usando um *http.Client que já injeta o token
// (ex.: oauth2.NewClient).
type Client struct {
	http *http.Client
	opt  Options
}

// New cria um Client.
func New(hc *http.Client, opt Options) *Client {
	if opt.BaseURL == "" {
		opt.BaseURL = DefaultBaseURL
	}
	if opt.Market == "" {
		opt.Market = "from_token"
	}
	if opt.MaxRetries == 0 {
		opt.MaxRetries = 5
	}
	if opt.MaxRetryAfter == 0 {
		opt.MaxRetryAfter = 2 * time.Minute
	}
	if opt.Limiter == nil {
		opt.Limiter = rate.NewLimiter(8, 8)
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Sleep == nil {
		opt.Sleep = sleepCtx
	}
	if opt.Jitter == nil {
		opt.Jitter = func(max time.Duration) time.Duration {
			if max <= 0 {
				return 0
			}
			return time.Duration(rand.Int64N(int64(max) + 1)) //nolint:gosec // G404: jitter de backoff não precisa de aleatoriedade criptográfica
		}
	}
	return &Client{http: hc, opt: opt}
}

// APIError é uma resposta de erro não repetível da API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("spotify: HTTP %d: %s", e.Status, e.Message)
	if e.Status == http.StatusForbidden {
		msg += " (403 em Development Mode costuma significar: usuário fora de \"User Management\" do app ou dono sem Premium)"
	}
	return msg
}

// RateLimitedError indica 429 com Retry-After acima do limite aceitável.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("spotify: rate limit excedido; o servidor pediu para aguardar %s", e.RetryAfter.Round(time.Second))
}

const (
	backoffBase = 500 * time.Millisecond
	backoffMax  = 30 * time.Second
)

// get faz GET com retry: 429 respeita Retry-After; 5xx e erros de rede usam
// backoff exponencial com jitter. Demais 4xx retornam *APIError.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, q, nil, out, true)
}

// do executa a requisição com retry. Para métodos que escrevem (idempotent=false)
// só o 429 é repetido (o servidor recusou antes de processar); falhas 5xx e de
// rede são devolvidas ao chamador, pois a escrita pode ter sido aplicada e
// repetir às cegas duplicaria itens. Veja IsTransient.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any, idempotent bool) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	u := c.opt.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := c.opt.Limiter.Wait(ctx); err != nil {
			return err
		}
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rdr)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.opt.UserAgent != "" {
			req.Header.Set("User-Agent", c.opt.UserAgent)
		}

		var wait time.Duration
		start := time.Now()
		resp, err := c.http.Do(req)
		if c.opt.Logger.Enabled(ctx, slog.LevelDebug) {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			c.opt.Logger.Debug("http", "method", method, "path", path, "status", status, "dur", time.Since(start).Round(time.Millisecond), "tentativa", attempt+1, "err", err)
		}
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !idempotent {
				return fmt.Errorf("spotify: %s %s: %w", method, path, err)
			}
			lastErr, wait = err, c.backoff(attempt)

		case resp.StatusCode == http.StatusTooManyRequests:
			ra, ok := retryAfter(resp)
			closeBody(resp)
			if ok && ra > c.opt.MaxRetryAfter {
				return &RateLimitedError{RetryAfter: ra}
			}
			if !ok {
				ra = c.backoff(attempt)
			}
			lastErr, wait = &APIError{Status: 429, Message: "too many requests"}, ra

		case resp.StatusCode >= 500:
			lastErr = &APIError{Status: resp.StatusCode, Message: readErrMessage(resp)}
			closeBody(resp)
			if !idempotent {
				return lastErr
			}
			wait = c.backoff(attempt)

		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			defer closeBody(resp)
			if out == nil {
				return nil
			}
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("spotify: decodificar resposta de %s: %w", path, err)
			}
			return nil

		default:
			defer closeBody(resp)
			return &APIError{Status: resp.StatusCode, Message: readErrMessage(resp)}
		}

		if attempt >= c.opt.MaxRetries {
			return fmt.Errorf("spotify: desistindo após %d tentativas: %w", attempt+1, lastErr)
		}
		c.opt.Logger.Warn("requisição falhou, tentando novamente",
			"path", path, "tentativa", attempt+1, "espera", wait, "erro", lastErr)
		if err := c.opt.Sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// backoff: exponencial com "equal jitter" (metade fixa + metade aleatória).
func (c *Client) backoff(attempt int) time.Duration {
	d := backoffBase << min(attempt, 10)
	if d > backoffMax {
		d = backoffMax
	}
	return d/2 + c.opt.Jitter(d/2)
}

func retryAfter(resp *http.Response) (time.Duration, bool) {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}

func readErrMessage(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	if len(b) == 0 {
		return http.StatusText(resp.StatusCode)
	}
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}

func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)) // permite reuso da conexão
	_ = resp.Body.Close()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRateLimited informa se err é um RateLimitedError.
func IsRateLimited(err error) bool {
	var r *RateLimitedError
	return errors.As(err, &r)
}

// IsTransient informa se err pode ter deixado uma escrita aplicada pela
// metade: erro 5xx ou de rede (não 4xx, não cancelamento). O chamador deve
// reler o estado antes de repetir.
func IsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status >= 500
	}
	var rl *RateLimitedError
	return !errors.As(err, &rl)
}
