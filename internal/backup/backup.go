// Package backup salva e lê snapshots JSON do conteúdo das playlists, feitos
// antes de qualquer alteração e usados por `export` e `restore`.
package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
	"github.com/ikauedeveloper/likedsorter/internal/planner"
)

const version = 1

// Item é uma faixa dentro de uma playlist, na ordem original.
type Item struct {
	URI  string `json:"uri"`
	Type string `json:"type,omitempty"`
}

// Playlist é o retrato de uma playlist.
type Playlist struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Public      bool   `json:"public"`
	LocalItems  int    `json:"local_items,omitempty"` // arquivos locais: não podem ser restaurados pela API
	Items       []Item `json:"items"`
}

// Snapshot é um conjunto de playlists num instante.
type Snapshot struct {
	Version   int        `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	Playlists []Playlist `json:"playlists"`
}

// FromExisting monta o snapshot das playlists já carregadas (Loaded).
// Playlists sem itens carregados são rejeitadas: um backup incompleto seria enganoso.
func FromExisting(es []planner.Existing, now time.Time) (Snapshot, error) {
	snap := Snapshot{Version: version, CreatedAt: now.UTC()}
	for _, e := range es {
		if !e.Loaded {
			return Snapshot{}, fmt.Errorf("playlist %q sem itens carregados; backup incompleto", e.Name)
		}
		p := Playlist{ID: e.ID, Name: e.Name, Description: e.Description, Public: e.Public, LocalItems: e.LocalItems, Items: []Item{}}
		for _, m := range e.Items {
			p.Items = append(p.Items, Item{URI: m.URI, Type: m.Type})
		}
		snap.Playlists = append(snap.Playlists, p)
	}
	return snap, nil
}

// URIs devolve as URIs de p na ordem original.
func (p Playlist) URIs() []string {
	out := make([]string, len(p.Items))
	for i, it := range p.Items {
		out[i] = it.URI
	}
	return out
}

// Save grava o snapshot (0600: contém sua biblioteca) de forma atômica.
func Save(path string, snap Snapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, b)
}

// Load lê e valida um snapshot.
func Load(path string) (Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("ler backup: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("backup inválido (%s): %w", path, err)
	}
	if snap.Version != version {
		return Snapshot{}, fmt.Errorf("versão de backup %d não suportada (esperada %d)", snap.Version, version)
	}
	return snap, nil
}

// NewPath devolve <dir>/backups/<timestamp UTC>/snapshot.json (sem criar nada).
func NewPath(dir string, now time.Time) string {
	return filepath.Join(dir, "backups", now.UTC().Format("20060102-150405"), "snapshot.json")
}
