package cache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

type payload struct {
	N    int    `json:"n"`
	Name string `json:"name"`
}

func TestStoreRoundTrip(t *testing.T) {
	s := &Store{Dir: filepath.Join(testutil.TempDir(t), "c")}
	var got payload
	if found, err := s.ReadJSON("x.json", &got); found || err != nil {
		t.Fatalf("ausente: found=%v err=%v", found, err)
	}
	if err := s.WriteJSON("x.json", payload{N: 3, Name: "ç"}); err != nil {
		t.Fatal(err)
	}
	if found, err := s.ReadJSON("x.json", &got); !found || err != nil || got.N != 3 || got.Name != "ç" {
		t.Fatalf("found=%v err=%v got=%+v", found, err, got)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(s.Dir, "x.json"))
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("permissão %v", fi.Mode().Perm())
		}
	}
	if err := s.Remove("x.json"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("x.json"); err != nil {
		t.Fatal("Remove deve ser idempotente:", err)
	}
}

func TestStoreFlags(t *testing.T) {
	dir := testutil.TempDir(t)
	(&Store{Dir: dir}).WriteJSON("x.json", payload{N: 1})

	var got payload
	if found, _ := (&Store{Dir: dir, NoRead: true}).ReadJSON("x.json", &got); found {
		t.Error("NoRead não deveria ler")
	}
	if found, _ := (&Store{Dir: dir, Disabled: true}).ReadJSON("x.json", &got); found {
		t.Error("Disabled não deveria ler")
	}
	// Disabled não grava; NoRead grava
	off := &Store{Dir: dir, Disabled: true}
	_ = off.WriteJSON("y.json", payload{})
	if _, err := os.Stat(filepath.Join(dir, "y.json")); err == nil {
		t.Error("Disabled gravou arquivo")
	}
	_ = (&Store{Dir: dir, NoRead: true}).WriteJSON("z.json", payload{})
	if _, err := os.Stat(filepath.Join(dir, "z.json")); err != nil {
		t.Error("NoRead deveria gravar")
	}
}

func TestStoreCorruptIsMiss(t *testing.T) {
	dir := testutil.TempDir(t)
	_ = os.WriteFile(filepath.Join(dir, "x.json"), []byte("{quebrado"), 0o600)
	var got payload
	if found, err := (&Store{Dir: dir}).ReadJSON("x.json", &got); found || err != nil {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		at   time.Time
		ttl  time.Duration
		want bool
	}{
		{now.Add(-time.Hour), 2 * time.Hour, false},
		{now.Add(-3 * time.Hour), 2 * time.Hour, true},
		{now.Add(-1000 * time.Hour), 0, false}, // ttl 0 = nunca expira
	}
	for _, tt := range tests {
		if got := Expired(tt.at, tt.ttl, now); got != tt.want {
			t.Errorf("Expired(%v,%v)=%v", tt.at, tt.ttl, got)
		}
	}
}

func TestTTLMap(t *testing.T) {
	s := &Store{Dir: testutil.TempDir(t)}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }

	m, err := OpenTTLMap[payload](s, "a.json", 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	m.Put("k1", payload{N: 1})
	clock = clock.Add(20 * time.Hour)
	m.Put("k2", payload{N: 2})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	// reabre: ambos válidos
	clock = clock.Add(2 * time.Hour) // k1 com 22h, k2 com 2h
	m2, _ := OpenTTLMap[payload](s, "a.json", 24*time.Hour, now)
	if v, ok := m2.Get("k1"); !ok || v.N != 1 {
		t.Errorf("k1: %+v %v", v, ok)
	}
	if m2.Len() != 2 {
		t.Errorf("Len=%d", m2.Len())
	}

	// k1 vence, k2 não
	clock = clock.Add(3 * time.Hour)
	if _, ok := m2.Get("k1"); ok {
		t.Error("k1 deveria ter vencido")
	}
	if _, ok := m2.Get("k2"); !ok {
		t.Error("k2 deveria valer")
	}
	if m2.Len() != 1 {
		t.Errorf("Len=%d", m2.Len())
	}
	_ = m2.Save()
	m3, _ := OpenTTLMap[payload](s, "a.json", 0, now) // ttl 0 mostra o que ficou no disco
	if m3.Len() != 1 {
		t.Errorf("vencidas deveriam ser descartadas ao salvar: %d", m3.Len())
	}

	// --refresh ignora o disco
	m4, _ := OpenTTLMap[payload](&Store{Dir: s.Dir, NoRead: true}, "a.json", 0, now)
	if m4.Len() != 0 {
		t.Error("NoRead deveria começar vazio")
	}
}

func TestTTLMapConcurrent(t *testing.T) {
	m, _ := OpenTTLMap[int](&Store{Dir: testutil.TempDir(t)}, "c.json", 0, nil)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(i int) {
			for j := 0; j < 200; j++ {
				m.Put("k", i)
				m.Get("k")
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
