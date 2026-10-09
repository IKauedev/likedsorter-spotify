package cache

import (
	"sync"
	"time"
)

const ttlMapVersion = 1

// Entry é um valor com a data em que foi obtido.
type Entry[V any] struct {
	Value     V         `json:"v"`
	FetchedAt time.Time `json:"at"`
}

type ttlFile[V any] struct {
	Version int                 `json:"version"`
	Entries map[string]Entry[V] `json:"entries"`
}

// TTLMap é um mapa string→V persistido, seguro para uso concorrente. Entradas
// vencidas são tratadas como ausentes em Get (e descartadas ao salvar).
type TTLMap[V any] struct {
	store *Store
	name  string
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]Entry[V]
}

// OpenTTLMap carrega (se existir e o Store permitir) o arquivo name.
// now pode ser nil (usa time.Now).
func OpenTTLMap[V any](s *Store, name string, ttl time.Duration, now func() time.Time) (*TTLMap[V], error) {
	if now == nil {
		now = time.Now
	}
	m := &TTLMap[V]{store: s, name: name, ttl: ttl, now: now, entries: map[string]Entry[V]{}}
	var f ttlFile[V]
	found, err := s.ReadJSON(name, &f)
	if err != nil {
		return nil, err
	}
	if found && f.Version == ttlMapVersion && f.Entries != nil {
		m.entries = f.Entries
	}
	return m, nil
}

// Get devolve o valor se existir e não estiver vencido.
func (m *TTLMap[V]) Get(key string) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok || Expired(e.FetchedAt, m.ttl, m.now()) {
		var zero V
		return zero, false
	}
	return e.Value, true
}

// Put grava (em memória) o valor com a hora atual.
func (m *TTLMap[V]) Put(key string, v V) {
	m.mu.Lock()
	m.entries[key] = Entry[V]{Value: v, FetchedAt: m.now()}
	m.mu.Unlock()
}

// Len é o número de entradas válidas (não vencidas).
func (m *TTLMap[V]) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.entries {
		if !Expired(e.FetchedAt, m.ttl, m.now()) {
			n++
		}
	}
	return n
}

// Save descarta entradas vencidas e persiste. Chamar também no meio de
// trabalhos longos permite retomar após falhas.
func (m *TTLMap[V]) Save() error {
	m.mu.Lock()
	for k, e := range m.entries {
		if Expired(e.FetchedAt, m.ttl, m.now()) {
			delete(m.entries, k)
		}
	}
	f := ttlFile[V]{Version: ttlMapVersion, Entries: m.entries}
	err := m.store.WriteJSON(m.name, f)
	m.mu.Unlock()
	return err
}
