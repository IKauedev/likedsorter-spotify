// Package state persiste o que o likedsorter sabe entre execuções: as playlists
// que ele criou (para reconhecê-las mesmo sem o marcador) e o andamento do
// último apply (para avisar e retomar após uma interrupção).
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
)

// Status de um item da execução.
const (
	StatusPending = "pending"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// PlaylistRecord registra uma playlist criada pelo likedsorter.
type PlaylistRecord struct {
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
}

// RunItem é o andamento de uma playlist dentro de uma execução.
type RunItem struct {
	Name       string `json:"name"`
	PlaylistID string `json:"playlist_id,omitempty"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	Added      int    `json:"added,omitempty"`
	Removed    int    `json:"removed,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Run descreve uma execução do apply.
type Run struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Strategy   string    `json:"strategy,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Backup     string    `json:"backup,omitempty"`
	Completed  bool      `json:"completed"`
	Items      []RunItem `json:"items"`
}

// State é o conteúdo de state.json.
type State struct {
	Playlists map[string]PlaylistRecord `json:"playlists"` // por ID do Spotify
	LastRun   *Run                      `json:"last_run,omitempty"`
}

// New devolve um estado vazio.
func New() *State { return &State{Playlists: map[string]PlaylistRecord{}} }

// KnownIDs devolve os IDs das playlists criadas pelo likedsorter.
func (s *State) KnownIDs() map[string]bool {
	ids := make(map[string]bool, len(s.Playlists))
	for id := range s.Playlists {
		ids[id] = true
	}
	return ids
}

// Interrupted informa se a última execução começou e não terminou com sucesso.
func (s *State) Interrupted() bool { return s.LastRun != nil && !s.LastRun.Completed }

// Store persiste o estado em um arquivo JSON com permissão 0600.
type Store struct{ Path string }

// Load lê o estado. Um arquivo inexistente devolve um estado vazio.
func (s *Store) Load() (*State, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("ler estado: %w", err)
	}
	st := New()
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("arquivo de estado corrompido (%s): %w", s.Path, err)
	}
	if st.Playlists == nil {
		st.Playlists = map[string]PlaylistRecord{}
	}
	return st, nil
}

// Save grava o estado de forma atômica.
func (s *Store) Save(st *State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.Path, b)
}
