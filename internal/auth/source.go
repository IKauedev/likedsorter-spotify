package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
)

// TokenSource devolve um oauth2.TokenSource que renova o access token com o
// refresh token e grava no disco sempre que o token mudar.
func TokenSource(ctx context.Context, cfg *oauth2.Config, store *Store) (oauth2.TokenSource, error) {
	sv, err := store.Load()
	if err != nil {
		return nil, err
	}
	return &persistingSource{
		src:   cfg.TokenSource(ctx, sv.Token), // renova sozinho quando expira
		store: store,
		scope: sv.Scope,
		last:  sv.Token.AccessToken,
	}, nil
}

type persistingSource struct {
	src   oauth2.TokenSource
	store *Store
	scope string

	mu   sync.Mutex
	last string
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tok, err := p.src.Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
			return nil, fmt.Errorf("sessão expirada ou revogada: %w", ErrNotLoggedIn)
		}
		return nil, err
	}
	if tok.AccessToken != p.last {
		if s, ok := tok.Extra("scope").(string); ok && s != "" {
			p.scope = s
		}
		if err := p.store.Save(&Saved{Token: tok, Scope: p.scope}); err != nil {
			return nil, err
		}
		p.last = tok.AccessToken
	}
	return tok, nil
}

// Profile é o subconjunto de GET /me usado no `auth status`.
type Profile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Whoami consulta GET /me. Serve também para validar o refresh do token.
func Whoami(ctx context.Context, ts oauth2.TokenSource, baseURL string) (*Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/me", nil)
	if err != nil {
		return nil, err
	}
	resp, err := oauth2.NewClient(ctx, ts).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /me: HTTP %d (403 costuma indicar usuário fora da lista do Development Mode)", resp.StatusCode)
	}
	var p Profile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}
