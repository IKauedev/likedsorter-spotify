// Package cache oferece cache em disco em JSON: um Store de arquivos
// nomeados e um mapa com TTL (usado para metadados de artistas).
//
// Escolha de JSON em vez de SQLite: zero dependências nativas (sem cgo, que
// não existe no Windows padrão) e volumes pequenos (milhares de faixas).
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
)

// Store lê e grava arquivos JSON em Dir.
type Store struct {
	Dir      string
	Disabled bool // --no-cache: não lê nem grava
	NoRead   bool // --refresh: ignora o conteúdo existente, mas regrava
}

// ReadJSON decodifica o arquivo name em v. found=false se o cache estiver
// desligado, o arquivo não existir ou estiver corrompido (um cache ruim é
// tratado como ausente e será sobrescrito na próxima gravação).
func (s *Store) ReadJSON(name string, v any) (found bool, err error) {
	if s.Disabled || s.NoRead {
		return false, nil
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ler cache %s: %w", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, nil //nolint:nilerr // cache corrompido é tratado como ausente e será sobrescrito
	}
	return true, nil
}

// WriteJSON grava v de forma atômica (0600). Não faz nada se Disabled.
func (s *Store) WriteJSON(name string, v any) error {
	if s.Disabled {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(s.Dir, name), b)
}

// Remove apaga o arquivo name (sem erro se não existir).
func (s *Store) Remove(name string) error {
	if err := os.Remove(filepath.Join(s.Dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Expired informa se algo gravado em at já passou do TTL. ttl <= 0 = nunca expira.
func Expired(at time.Time, ttl time.Duration, now time.Time) bool {
	return ttl > 0 && now.Sub(at) > ttl
}
