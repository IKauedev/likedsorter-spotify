package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// defaultInstallDir: Windows → %LOCALAPPDATA%\Programs\likedsorter; demais → ~/.local/bin.
func defaultInstallDir() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA não definido; use --dir")
		}
		return filepath.Join(base, "Programs", "likedsorter"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "likedsorter.exe"
	}
	return "likedsorter"
}

// runInstall copia o próprio executável para uma pasta do usuário e a coloca no PATH.
func runInstall(args []string, out io.Writer) error {
	return withJSON(args, out, func(a []string, text io.Writer) (any, error) { return installRun(a, text) })
}

func installRun(args []string, out io.Writer) (any, error) {
	fl := flag.NewFlagSet("install", flag.ContinueOnError)
	dir := fl.String("dir", "", "pasta de destino (padrão depende do sistema)")
	noPath := fl.Bool("no-path", false, "não alterar o PATH do usuário")
	uninstall := fl.Bool("uninstall", false, "remove o executável instalado e a entrada do PATH")
	if err := fl.Parse(args); err != nil {
		return nil, err
	}
	target := filepath.Clean(*dir)
	if *dir == "" {
		d, err := defaultInstallDir()
		if err != nil {
			return nil, err
		}
		target = d
	}
	dst := filepath.Join(target, exeName())

	if *uninstall {
		return map[string]any{"uninstalled": dst}, uninstallSelf(target, dst, *noPath, out)
	}

	src, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if src, err = filepath.EvalSymlinks(src); err != nil {
		return nil, err
	}
	if sameFile(src, dst) {
		fmt.Fprintf(out, "Já está instalado em %s\n", dst)
	} else {
		if err := copyExecutable(src, dst); err != nil {
			return nil, fmt.Errorf("copiar para %s: %w (feche outros terminais usando o likedsorter e tente de novo)", dst, err)
		}
		fmt.Fprintf(out, "Copiado para %s\n", dst)
	}

	if *noPath {
		fmt.Fprintf(out, "PATH não alterado. Rode pelo caminho completo ou adicione %s ao PATH.\n", target)
		return map[string]any{"installed": dst, "path_changed": false}, nil
	}
	changed, note, err := addToUserPath(target)
	if err != nil {
		return nil, fmt.Errorf("atualizar o PATH: %w", err)
	}
	if changed {
		fmt.Fprintf(out, "Adicionado ao PATH do usuário: %s\n", target)
	}
	if note != "" {
		fmt.Fprintln(out, note)
	}
	printPathAdvice(out, target, dst)
	fmt.Fprintln(out, "\nDepois rode:")
	fmt.Fprintln(out, "  likedsorter start        (assistente: credenciais, login e 1ª sincronização)")
	fmt.Fprintln(out, "  likedsorter doctor       (se algo não funcionar)")
	return map[string]any{"installed": dst, "path_changed": changed}, nil
}

func uninstallSelf(dir, dst string, noPath bool, out io.Writer) error {
	if !noPath {
		if changed, _, err := removeFromUserPath(dir); err != nil {
			return fmt.Errorf("atualizar o PATH: %w", err)
		} else if changed {
			fmt.Fprintf(out, "Removido do PATH do usuário: %s\n", dir)
		}
	}
	if src, err := os.Executable(); err == nil && sameFile(src, dst) {
		fmt.Fprintf(out, "O executável em uso (%s) não pode se apagar sozinho; apague-o depois de fechar o programa.\n", dst)
	} else if err := os.Remove(dst); err == nil {
		fmt.Fprintf(out, "Removido: %s\n", dst)
		_ = os.Remove(dir) // só remove se estiver vazia
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		fmt.Fprintf(out, "Nada instalado em %s\n", dst)
	}
	fmt.Fprintln(out, "Credenciais, token e cache foram preservados (veja `likedsorter help instalar` para apagá-los).")
	return nil
}

func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// copyExecutable copia src para dst via arquivo temporário + rename.
func copyExecutable(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".likedsorter-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
