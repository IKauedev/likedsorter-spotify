// Package fsutil reúne utilidades de arquivo compartilhadas.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic grava data em path via arquivo temporário + rename, com
// modo 0600 e diretório pai 0700. Leitores nunca veem um arquivo pela metade.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("criar diretório %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*") // CreateTemp já usa 0600
	if err != nil {
		return fmt.Errorf("criar arquivo temporário: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op após rename bem-sucedido
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("gravar %s: %w", path, err)
	}
	return nil
}
