// Package testutil reúne ajudas só para testes.
package testutil

import (
	"os"
	"testing"
	"time"
)

// TempDir cria um diretório temporário como t.TempDir, mas com limpeza
// tolerante: no Windows, antivírus/indexador às vezes seguram um arquivo
// recém-renomeado por instantes e o RemoveAll falha com "directory is not
// empty", reprovando testes que estão corretos. Aqui tentamos por até ~3 s e,
// se ainda assim falhar, apenas registramos (o SO limpa o %TEMP% depois).
func TempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "likedsorter-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var last error
		for i := 0; i < 60; i++ {
			if last = os.RemoveAll(dir); last == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Logf("aviso: não foi possível remover %s: %v", dir, last)
	})
	return dir
}
