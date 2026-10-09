package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ikauedeveloper/likedsorter/internal/config"
)

// cacheFiles são os únicos arquivos que o likedsorter cria no diretório de cache.
var cacheFiles = []string{"tracks.json", "artists.json"}

// runCache: cache path | info | clear [--yes]. Só apaga os arquivos conhecidos, nunca a pasta inteira.
func runCache(args []string, out io.Writer) error {
	const uso = "uso: likedsorter cache path | info [--json] | clear [--yes]"
	args, asJSON := stripJSON(args)
	if len(args) == 0 {
		return errors.New(uso)
	}
	dir, err := config.CacheDir()
	if err != nil {
		return err
	}
	type fileInfo struct {
		Name  string `json:"name"`
		Bytes int64  `json:"bytes"`
	}
	var found []fileInfo
	var total int64
	for _, n := range cacheFiles {
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil {
			found = append(found, fileInfo{n, st.Size()})
			total += st.Size()
		}
	}

	switch args[0] {
	case "path":
		fmt.Fprintln(out, dir)
		return nil
	case "info":
		if asJSON {
			return writeJSON(out, map[string]any{"dir": dir, "files": found, "total_bytes": total})
		}
		fmt.Fprintf(out, "Cache: %s\n", dir)
		if len(found) == 0 {
			fmt.Fprintln(out, "(vazio)")
		}
		for _, f := range found {
			fmt.Fprintf(out, "  %-14s %8.1f KB\n", f.Name, float64(f.Bytes)/1024)
		}
		fmt.Fprintln(out, "Limpar: likedsorter cache clear (a próxima sincronização relê tudo e os gêneros dos artistas são buscados de novo)")
		return nil
	case "clear":
		fl := flag.NewFlagSet("cache clear", flag.ContinueOnError)
		yes := fl.Bool("yes", false, "não pedir confirmação")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		if len(found) == 0 {
			fmt.Fprintln(out, "O cache já está vazio.")
			return nil
		}
		if ok, err := confirmYes(out, fmt.Sprintf("Apagar %d arquivo(s) de cache (%.1f KB) em %s?", len(found), float64(total)/1024, dir), *yes); err != nil {
			return err
		} else if !ok {
			fmt.Fprintln(out, "Nada foi alterado.")
			return nil
		}
		for _, f := range found {
			if err := os.Remove(filepath.Join(dir, f.Name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		fmt.Fprintln(out, "Cache apagado. Suas playlists e configurações não foram tocadas.")
		return nil
	}
	return fmt.Errorf("subcomando cache desconhecido %q\n%s", args[0], uso)
}
