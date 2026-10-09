// Package plays guarda localmente o histórico de reproduções do usuário. A API do Spotify
// não expõe contagem de plays; este pacote acumula as reproduções recentes a cada
// sincronização e aceita o histórico completo exportado pelo Spotify (privacidade da conta).
package plays

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
)

// MinPlayMs é o tempo mínimo para contar como reprodução (o Spotify conta a partir de 30 s).
const MinPlayMs = 30_000

// Event é uma reprodução: instante (segundos Unix, UTC) e ID da faixa.
type Event struct {
	T  int64  `json:"t"`
	ID string `json:"id"`
}

// Meta guarda o suficiente para mostrar a faixa sem consultar a API.
type Meta struct {
	Name    string   `json:"name"`
	Artists []string `json:"artists,omitempty"`
}

// Data é o conteúdo persistido.
type Data struct {
	Version  int             `json:"version"`
	Events   []Event         `json:"events"`
	Meta     map[string]Meta `json:"meta"`
	CursorMs int64           `json:"cursor_ms"` // played_at mais recente já lido de recently-played

	seen map[Event]bool
}

// Load lê o arquivo; ausente devolve Data vazio.
func Load(path string) (*Data, error) {
	d := &Data{Version: 1, Meta: map[string]Meta{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, d); err != nil {
		return nil, fmt.Errorf("%s corrompido: %w", path, err)
	}
	if d.Meta == nil {
		d.Meta = map[string]Meta{}
	}
	return d, nil
}

// Save grava de forma atômica (modo 0600), com eventos em ordem cronológica.
func (d *Data) Save(path string) error {
	sort.SliceStable(d.Events, func(i, j int) bool { return d.Events[i].T < d.Events[j].T })
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, b)
}

func (d *Data) index() {
	if d.seen != nil {
		return
	}
	d.seen = make(map[Event]bool, len(d.Events))
	for _, e := range d.Events {
		d.seen[e] = true
	}
}

// Add registra uma reprodução; devolve false se já existia (mesma faixa no mesmo segundo).
func (d *Data) Add(id string, at time.Time, m Meta) bool {
	if id == "" {
		return false
	}
	d.index()
	e := Event{T: at.UTC().Unix(), ID: id}
	if d.seen[e] {
		return false
	}
	d.seen[e] = true
	d.Events = append(d.Events, e)
	if m.Name != "" {
		d.Meta[id] = m
	}
	return true
}

// AdvanceCursor avança o marcador de "recently-played" (só para frente).
func (d *Data) AdvanceCursor(at time.Time) {
	if ms := at.UnixMilli(); ms > d.CursorMs {
		d.CursorMs = ms
	}
}

// ImportExtended lê um arquivo "Streaming_History_Audio_*.json" (exportação estendida do
// Spotify) e registra as reproduções de faixas com pelo menos 30 s. Devolve quantas
// entraram e quantas foram ignoradas (curtas, podcasts, repetidas ou sem ID).
func (d *Data) ImportExtended(r io.Reader) (added, skipped int, err error) {
	var rows []struct {
		TS       string `json:"ts"`
		URI      string `json:"spotify_track_uri"`
		MsPlayed int64  `json:"ms_played"`
		Name     string `json:"master_metadata_track_name"`
		Artist   string `json:"master_metadata_album_artist_name"`
	}
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return 0, 0, fmt.Errorf("não parece um arquivo de histórico estendido do Spotify: %w", err)
	}
	for _, row := range rows {
		id, ok := strings.CutPrefix(row.URI, "spotify:track:")
		if !ok || row.MsPlayed < MinPlayMs {
			skipped++
			continue
		}
		at, perr := time.Parse(time.RFC3339, row.TS)
		if perr != nil {
			skipped++
			continue
		}
		m := Meta{Name: row.Name}
		if row.Artist != "" {
			m.Artists = []string{row.Artist}
		}
		if d.Add(id, at, m) {
			added++
		} else {
			skipped++
		}
	}
	return added, skipped, nil
}

// Stats é um índice de consulta sobre os eventos.
type Stats struct {
	times map[string][]int64 // por faixa, em ordem crescente
	total int
	first int64
	last  int64
}

// Stats monta o índice.
func (d *Data) Stats() *Stats {
	s := &Stats{times: map[string][]int64{}, total: len(d.Events)}
	for _, e := range d.Events {
		s.times[e.ID] = append(s.times[e.ID], e.T)
		if s.first == 0 || e.T < s.first {
			s.first = e.T
		}
		if e.T > s.last {
			s.last = e.T
		}
	}
	for _, ts := range s.times {
		sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	}
	return s
}

// HasHistory informa se há ao menos uma reprodução registrada.
func (s *Stats) HasHistory() bool { return s != nil && s.total > 0 }

// Span devolve o primeiro e o último instante registrados.
func (s *Stats) Span() (first, last time.Time) {
	return time.Unix(s.first, 0).UTC(), time.Unix(s.last, 0).UTC()
}

// Count conta as reproduções da faixa desde since (zero = desde sempre).
func (s *Stats) Count(id string, since time.Time) int {
	ts := s.times[id]
	if since.IsZero() {
		return len(ts)
	}
	i := sort.Search(len(ts), func(i int) bool { return ts[i] >= since.Unix() })
	return len(ts) - i
}

// Last devolve a última reprodução da faixa (zero se nunca).
func (s *Stats) Last(id string) time.Time {
	ts := s.times[id]
	if len(ts) == 0 {
		return time.Time{}
	}
	return time.Unix(ts[len(ts)-1], 0).UTC()
}

// Ranked é uma faixa com sua contagem.
type Ranked struct {
	ID    string
	Count int
	Last  time.Time
}

// Top devolve as n faixas mais tocadas desde since, desempatando pela reprodução mais recente.
func (s *Stats) Top(since time.Time, n int) []Ranked {
	var out []Ranked
	for id := range s.times {
		if c := s.Count(id, since); c > 0 {
			out = append(out, Ranked{id, c, s.Last(id)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if !out[i].Last.Equal(out[j].Last) {
			return out[i].Last.After(out[j].Last)
		}
		return out[i].ID < out[j].ID
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// FirstPlay devolve a reprodução mais antiga registrada (zero se vazio).
func (s *Stats) FirstPlay() time.Time {
	if s == nil || s.first == 0 {
		return time.Time{}
	}
	return time.Unix(s.first, 0).UTC()
}

// TopIDs devolve só os IDs de Top, para a estratégia de agrupamento.
func (s *Stats) TopIDs(since time.Time, n int) []string {
	top := s.Top(since, n)
	ids := make([]string, len(top))
	for i, r := range top {
		ids[i] = r.ID
	}
	return ids
}
