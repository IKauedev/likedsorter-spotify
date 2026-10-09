package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// httpJSON faz GETs JSON educados: rate limit próprio, User-Agent e retry
// em 429/5xx/erros de rede com backoff exponencial.
type httpJSON struct {
	hc         *http.Client
	userAgent  string
	limiter    *rate.Limiter
	maxRetries int
	header     http.Header // cabeçalhos extras (ex.: Authorization do Discogs)
	sleep      func(ctx context.Context, d time.Duration) error
}

// statusError é uma resposta HTTP não-2xx definitiva.
type statusError struct {
	Status int
	Body   string
}

func (e *statusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

func (h *httpJSON) get(ctx context.Context, u string, out any) error {
	var last error
	for attempt := 0; ; attempt++ {
		if err := h.limiter.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", h.userAgent)
		req.Header.Set("Accept", "application/json")
		for k, v := range h.header {
			req.Header[k] = v
		}

		wait := time.Duration(2<<attempt) * time.Second // 2s, 4s, 8s...
		resp, err := h.hc.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			last = err
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			switch {
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				if err := json.Unmarshal(body, out); err != nil {
					return fmt.Errorf("decodificar resposta: %w", err)
				}
				return nil
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				last = &statusError{resp.StatusCode, truncate(string(body), 120)}
				if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 && s <= 60 {
					wait = time.Duration(s) * time.Second
				}
			default:
				return &statusError{resp.StatusCode, truncate(string(body), 120)}
			}
		}
		if attempt >= h.maxRetries {
			return fmt.Errorf("desistindo após %d tentativas: %w", attempt+1, last)
		}
		if err := h.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
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
