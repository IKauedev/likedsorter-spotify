package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/testutil"
)

func TestNoLeftoverTempFiles(t *testing.T) {
	dir := testutil.TempDir(t)
	p := filepath.Join(dir, "x.json")
	for i := 0; i < 300; i++ {
		if err := WriteFileAtomic(p, []byte("conteudo")); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		for _, e := range ents {
			t.Log("sobrou:", e.Name())
		}
		t.Fatalf("esperava 1 arquivo, há %d", len(ents))
	}
}
