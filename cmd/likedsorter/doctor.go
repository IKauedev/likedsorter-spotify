package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/auth"
	"github.com/ikauedeveloper/likedsorter/internal/config"
)

type checkStatus string

const (
	stOK   checkStatus = "ok"
	stWarn checkStatus = "aviso"
	stFail checkStatus = "falha"
)

type check struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
	Fix    string      `json:"fix,omitempty"`
}

// runDoctor diagnostica instalação, credenciais, token, porta de login e rede.
func runDoctor(ctx context.Context, args []string, out io.Writer) error {
	fl := flag.NewFlagSet("doctor", flag.ContinueOnError)
	offline := fl.Bool("offline", false, "não testar a rede nem a API do Spotify")
	asJSON := fl.Bool("json", false, "saída em JSON")
	if err := fl.Parse(args); err != nil {
		return err
	}
	checks := runChecks(ctx, *offline)

	failed := 0
	for _, c := range checks {
		if c.Status == stFail {
			failed++
		}
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(checks); err != nil {
			return err
		}
	} else {
		icon := map[checkStatus]string{stOK: paint("32", "[ok]    "), stWarn: paint("33", "[aviso] "), stFail: paint("1;31", "[FALHA] ")}
		for _, c := range checks {
			fmt.Fprintf(out, "%s%s: %s\n", icon[c.Status], c.Name, c.Detail)
			if c.Fix != "" && c.Status != stOK {
				fmt.Fprintf(out, "         → %s\n", c.Fix)
			}
		}
		fmt.Fprintln(out)
		if failed == 0 {
			fmt.Fprintln(out, "Tudo certo para usar.")
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d verificação(ões) falharam", failed)
	}
	return nil
}

func runChecks(ctx context.Context, offline bool) []check {
	var cs []check
	add := func(name string, st checkStatus, detail, fix string) {
		cs = append(cs, check{name, st, detail, fix})
	}

	// Executável e PATH
	exe, _ := os.Executable()
	add("Versão", stOK, fmt.Sprintf("%s (%s)", version, exe), "")
	if p, err := exec.LookPath("likedsorter"); err != nil {
		add("PATH", stWarn, "\"likedsorter\" não está no PATH deste terminal", "rode `likedsorter install`; neste terminal use $env:Path += \";<pasta>\" (PowerShell) ou abra um terminal novo pelo menu Iniciar")
	} else if p2, _ := filepath.EvalSymlinks(p); !sameFile(p2, exe) {
		add("PATH", stWarn, "o \"likedsorter\" do PATH é outro arquivo: "+p, "rode `likedsorter install` para atualizá-lo")
	} else {
		add("PATH", stOK, p, "")
	}

	// Diretórios
	dir, err := config.Dir()
	if err != nil {
		add("Diretório de config", stFail, err.Error(), "defina LIKEDSORTER_CONFIG_DIR")
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		add("Diretório de config", stFail, fmt.Sprintf("%s não é gravável: %v", dir, err), "")
	} else {
		add("Diretório de config", stOK, dir, "")
	}
	if _, _, err := config.LoadSettings(""); err != nil {
		add("config.yaml", stFail, err.Error(), "corrija o arquivo ou rode `likedsorter config show`")
	} else {
		add("config.yaml", stOK, "válido (ou ausente: usando padrões)", "")
	}

	// Credenciais
	cfg, cerr := config.Load()
	switch {
	case errors.Is(cerr, config.ErrMissingClientID):
		add("Client ID", stFail, "não configurado", "rode `likedsorter start` (assistente) ou `likedsorter setup`")
	case cerr != nil:
		add("Client ID", stFail, cerr.Error(), "")
	case !config.LooksLikeClientID(cfg.ClientID):
		add("Client ID", stWarn, "configurado, mas não tem 32 caracteres hexadecimais", "confira se copiou o Client ID do Dashboard")
	default:
		add("Client ID", stOK, "configurado ("+cfg.ClientID[:4]+"…)", "")
	}

	// Redirect URI e porta
	addr, _, rerr := auth.ParseRedirect(cfg.RedirectURI)
	if rerr != nil {
		add("Redirect URI", stFail, rerr.Error(), "rode `likedsorter setup` e use http://127.0.0.1:8888/callback")
	} else {
		ln, lerr := net.Listen("tcp", addr)
		if lerr != nil {
			add("Redirect URI", stWarn, cfg.RedirectURI+": porta ocupada agora ("+lerr.Error()+")", "feche o programa que usa a porta antes do `auth login`, ou troque a porta no setup E no Dashboard")
		} else {
			ln.Close()
			add("Redirect URI", stOK, cfg.RedirectURI+" (porta livre; precisa ser idêntico ao do Dashboard)", "")
		}
	}

	// Token
	store, serr := tokenStore()
	var haveToken bool
	if serr == nil {
		if sv, err := store.Load(); err != nil {
			add("Login", stFail, "sem token: "+err.Error(), "rode `likedsorter auth login`")
		} else {
			haveToken = true
			switch {
			case sv.Token.RefreshToken == "":
				add("Login", stWarn, "token sem refresh token; vai expirar", "rode `likedsorter auth login`")
			case len(auth.MissingScopes(sv.Scope)) > 0:
				add("Login", stWarn, "faltam escopos: "+strings.Join(auth.MissingScopes(sv.Scope), ", "), "rode `likedsorter auth login`")
			default:
				add("Login", stOK, "token válido e renovável, escopos completos", "")
			}
		}
	}

	if offline {
		return cs
	}

	// Rede
	hc := &http.Client{Timeout: 8 * time.Second}
	for _, h := range []struct{ name, url string }{
		{"Rede (accounts.spotify.com)", "https://accounts.spotify.com/"},
		{"Rede (api.spotify.com)", "https://api.spotify.com/v1/"},
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
		resp, err := hc.Do(req)
		if err != nil {
			add(h.name, stFail, err.Error(), "verifique internet, proxy ou firewall")
			continue
		}
		resp.Body.Close()
		add(h.name, stOK, fmt.Sprintf("HTTP %d", resp.StatusCode), "")
	}

	// API com o token
	if haveToken && cerr == nil {
		sess, err := newSession(ctx, "from_token")
		if err == nil {
			var id string
			if id, err = sess.Client.CurrentUserID(ctx); err == nil {
				add("API do Spotify", stOK, "autenticado como "+id, "")
				return cs
			}
		}
		add("API do Spotify", stFail, err.Error(), "se for 403: adicione seu e-mail em User Management do app e use conta Premium")
	}
	return cs
}
