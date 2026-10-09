package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/ikauedeveloper/likedsorter/internal/fsutil"

	"golang.org/x/oauth2"
)

// Saved é o conteúdo persistido em disco.
type Saved struct {
	Token *oauth2.Token `json:"token"`
	Scope string        `json:"scope"`
}

// Store persiste o token em um arquivo JSON com permissão 0600.
// No Windows, os bits POSIX são ignorados: a proteção vem do perfil do usuário.
type Store struct{ Path string }

// NewStore cria um Store para o arquivo path.
func NewStore(path string) *Store { return &Store{Path: path} }

// Load lê o token salvo. Devolve ErrNotLoggedIn se não existir.
func (s *Store) Load() (*Saved, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, fmt.Errorf("ler token: %w", err)
	}
	var sv Saved
	if err := json.Unmarshal(b, &sv); err != nil || sv.Token == nil {
		return nil, fmt.Errorf("arquivo de token corrompido (%s): rode `auth logout` e `auth login`", s.Path)
	}
	return &sv, nil
}

// Save grava de forma atômica com modo 0600.
func (s *Store) Save(sv *Saved) error {
	b, err := json.MarshalIndent(sv, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.Path, b)
}

// Delete remove o token salvo. Não falha se ele não existir.
func (s *Store) Delete() error {
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
