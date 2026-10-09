package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/update"
)

// runUpdate verifica e instala a última release (com SHA-256 conferido).
func runUpdate(ctx context.Context, args []string, out io.Writer) error {
	return withJSON(args, out, func(a []string, text io.Writer) (any, error) { return updateRun(ctx, a, text) })
}

func updateRun(ctx context.Context, args []string, out io.Writer) (any, error) {
	fl := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fl.Bool("check", false, "só verifica se há versão nova, sem instalar")
	yes := fl.Bool("yes", false, "não pedir confirmação")
	repo := fl.String("repo", update.DefaultRepo, "repositório GitHub das releases (dono/nome)")
	if err := fl.Parse(args); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}
	update.CleanupOld(exe)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	u := &update.Updater{Repo: *repo}
	rel, err := u.Latest(ctx)
	if err != nil {
		return nil, fmt.Errorf("verificar atualização: %w", err)
	}
	fmt.Fprintf(out, "Versão instalada: %s | última release: %s\n", version, rel.Tag)
	if !update.Newer(version, rel.Tag) {
		fmt.Fprintln(out, "Você já está na versão mais recente.")
		return map[string]any{"current": version, "latest": rel.Tag, "newer": false}, nil
	}
	if *check {
		fmt.Fprintf(out, "Há uma versão nova: %s\nNotas: %s\nAtualize com: likedsorter update\n", rel.Tag, rel.URL)
		return map[string]any{"current": version, "latest": rel.Tag, "newer": true, "url": rel.URL}, nil
	}
	if ok, err := confirmYes(out, fmt.Sprintf("Atualizar %s para %s?", exe, rel.Tag), *yes); err != nil {
		return nil, err
	} else if !ok {
		fmt.Fprintln(out, "Nada foi alterado.")
		return map[string]any{"current": version, "latest": rel.Tag, "newer": true, "updated": false}, nil
	}
	tmp, err := u.Download(ctx, rel, runtime.GOOS, runtime.GOARCH, exe)
	if err != nil {
		return nil, err
	}
	if err := update.Replace(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	fmt.Fprintf(out, "Atualizado para %s. Suas configurações foram mantidas.\n", rel.Tag)
	return map[string]any{"current": version, "latest": rel.Tag, "newer": true, "updated": true}, nil
}

// confirmYes pergunta s/N no terminal; --yes aprova direto.
func confirmYes(out io.Writer, q string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !isTerminal(os.Stdin) {
		return false, errors.New("confirmação necessária: rode em um terminal ou passe --yes")
	}
	return terminalUI(out, false).confirm(q)
}
