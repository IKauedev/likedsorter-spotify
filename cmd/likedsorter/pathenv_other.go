//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func onPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// Fora do Windows não editamos arquivos de shell; só orientamos.
func addToUserPath(dir string) (bool, string, error) {
	if onPath(dir) {
		return false, "", nil
	}
	return false, "Adicione ao PATH (ex.: no ~/.bashrc ou ~/.zshrc):\n  export PATH=\"" + dir + ":$PATH\"", nil
}

func removeFromUserPath(dir string) (bool, string, error) {
	return false, "Remova " + dir + " do PATH no arquivo de configuração do seu shell, se o adicionou.", nil
}
