package ai

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
)

// Entry é um gênero proposto pela IA e aceito pelo usuário.
type Entry struct {
	Genres []string  `json:"genres"`
	Model  string    `json:"model"`
	At     time.Time `json:"at"`
}

// GenreStore guarda as classificações aceitas (por nome de artista, em minúsculas).
// Só preenchem artistas SEM gênero; os gêneros que o próprio usuário define (genre set) sempre vencem.
type GenreStore struct {
	Artists map[string]Entry `json:"artists"`
}

// LoadStore lê o arquivo; ausente devolve vazio.
func LoadStore(path string) (*GenreStore, error) {
	s := &GenreStore{Artists: map[string]Entry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	if s.Artists == nil {
		s.Artists = map[string]Entry{}
	}
	return s, nil
}

// Save grava de forma atômica (0600).
func (s *GenreStore) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, b)
}

// Put registra a classificação de um artista.
func (s *GenreStore) Put(name string, genres []string, model string) {
	s.Artists[strings.ToLower(strings.TrimSpace(name))] = Entry{Genres: genres, Model: model, At: time.Now().UTC()}
}

// Names devolve os artistas guardados, em ordem.
func (s *GenreStore) Names() []string {
	out := make([]string, 0, len(s.Artists))
	for k := range s.Artists {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Apply preenche os gêneros de quem está SEM gênero em infos (por nome do artista). Devolve quantos.
func (s *GenreStore) Apply(infos map[string]enrich.Info) int {
	n := 0
	for id, inf := range infos {
		if len(inf.Genres) > 0 {
			continue
		}
		if e, ok := s.Artists[strings.ToLower(strings.TrimSpace(inf.Name))]; ok {
			inf.Genres, inf.Source = e.Genres, "ai"
			infos[id] = inf
			n++
		}
	}
	return n
}
