package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
)

// LoginOptions controla o fluxo interativo.
type LoginOptions struct {
	Timeout time.Duration          // padrão: 5 min
	Out     io.Writer              // mensagens ao usuário
	Open    func(url string) error // abre o navegador; nil = só imprime o link
}

type callbackResult struct {
	code string
	err  error
}

// Login executa o Authorization Code + PKCE: sobe um servidor em 127.0.0.1,
// abre o navegador, valida o state, troca o code pelo token e o persiste.
func Login(ctx context.Context, cfg *oauth2.Config, store *Store, opt LoginOptions) (*oauth2.Token, error) {
	addr, path, err := ParseRedirect(cfg.RedirectURL)
	if err != nil {
		return nil, err
	}
	if opt.Timeout == 0 {
		opt.Timeout = 5 * time.Minute
	}
	if opt.Out == nil {
		opt.Out = io.Discard
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("não foi possível escutar em %s (porta ocupada?): %w", addr, err)
	}

	state, err := randomState()
	if err != nil {
		ln.Close()
		return nil, err
	}
	verifier := oauth2.GenerateVerifier() // 32 bytes de crypto/rand, base64url
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	results, shutdown := serveCallback(ln, path, state)
	defer shutdown()

	fmt.Fprintf(opt.Out, "Abra este link no navegador para autorizar:\n\n  %s\n\n", authURL)
	if opt.Open != nil {
		if err := opt.Open(authURL); err != nil {
			fmt.Fprintf(opt.Out, "(não consegui abrir o navegador automaticamente: %v)\n", err)
		}
	}
	fmt.Fprintf(opt.Out, "Aguardando autorização em %s ... (Ctrl+C cancela)\n", cfg.RedirectURL)

	var res callbackResult
	select {
	case res = <-results:
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("tempo esgotado (%s) aguardando a autorização", opt.Timeout)
		}
		return nil, ctx.Err()
	}
	if res.err != nil {
		return nil, res.err
	}

	tok, err := cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("trocar code por token: %w", err)
	}
	scope, _ := tok.Extra("scope").(string)
	if err := store.Save(&Saved{Token: tok, Scope: scope}); err != nil {
		return nil, err
	}
	return tok, nil
}

// serveCallback atende o redirect do Spotify. Requisições com state inválido
// recebem 400 e NÃO encerram o fluxo (evita que requisições alheias o derrubem).
func serveCallback(ln net.Listener, path, state string) (<-chan callbackResult, func()) {
	results := make(chan callbackResult, 1)
	deliver := func(r callbackResult) {
		select {
		case results <- r:
		default: // já entregue
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "state inválido", http.StatusBadRequest)
			return
		}
		if e := q.Get("error"); e != "" {
			deliver(callbackResult{err: fmt.Errorf("autorização negada pelo Spotify: %s", e)})
			writePage(w, http.StatusOK, "Autorização negada", html.EscapeString(e)+". Você pode fechar esta aba.")
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "code ausente", http.StatusBadRequest)
			return
		}
		deliver(callbackResult{code: code})
		writePage(w, http.StatusOK, "Tudo certo!", "Autenticado. Você pode fechar esta aba e voltar ao terminal.")
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return results, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func writePage(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>likedsorter</title>"+
		"<body style=\"font-family:sans-serif;max-width:32rem;margin:4rem auto\"><h1>%s</h1><p>%s</p>", title, msg)
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gerar state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// OpenBrowser abre link no navegador padrão do sistema. Só aceita http(s):
// o valor vira argumento de um processo, então nada além de uma URL web passa.
func OpenBrowser(link string) error {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("recusando abrir %q: não é uma URL http(s)", link)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link) //nolint:gosec,noctx // G204: link validado acima; o navegador deve sobreviver ao contexto
	case "darwin":
		cmd = exec.Command("open", link) //nolint:gosec,noctx // G204: link validado acima; o navegador deve sobreviver ao contexto
	default:
		cmd = exec.Command("xdg-open", link) //nolint:gosec,noctx // G204: link validado acima; o navegador deve sobreviver ao contexto
	}
	return cmd.Start()
}
