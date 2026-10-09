package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
	"github.com/ikauedeveloper/likedsorter/internal/grouping"
)

const sampleRules = `# Suas regras de organização (use: likedsorter plan --by=rules)
# Dentro de uma regra, TODAS as condições informadas precisam casar (E).
# Em cada lista, basta UMA casar (OU). Textos casam por trecho, sem diferenciar maiúsculas.
# Faixas que não casam com nenhuma regra vão para "Outros".

# true (padrão): a faixa entra só na primeira regra que casar.
# false: a faixa entra em todas as regras que casarem.
primeira_regra: true

regras:
  - nome: MPB raiz
    artista: [djavan, marisa monte, caetano]
    genero: [mpb]

  - nome: Clássicos 70-90
    lancamento: "1970-1999"

  - nome: Descobertas recentes
    curtida_desde: 2024-01-01

  - nome: Acústicos
    titulo: [acústico, acoustic, unplugged]
`

// runRules: rules init | path | check.
func runRules(args []string, out io.Writer) error {
	const uso = "uso: likedsorter rules init [--force] | path | check [arquivo]"
	args, asJSON := stripJSON(args)
	if len(args) == 0 {
		return errors.New(uso)
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "rules.yaml")
	switch args[0] {
	case "path":
		fmt.Fprintln(out, path)
		return nil
	case "init":
		force := len(args) > 1 && args[1] == "--force"
		if _, err := os.Stat(path); err == nil && !force {
			return fmt.Errorf("%s já existe (use --force para sobrescrever)", path)
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := fsutil.WriteFileAtomic(path, []byte(sampleRules)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Modelo criado em %s\nEdite e rode: likedsorter plan --by=rules\n", path)
		return nil
	case "check":
		if len(args) > 1 {
			path = args[1]
		}
		rs, err := grouping.LoadRules(path)
		if err != nil {
			return err
		}
		if asJSON {
			names := []string{}
			for _, r := range rs.Rules {
				names = append(names, r.Name)
			}
			return writeJSON(out, map[string]any{"path": path, "valid": true, "first_match": rs.FirstMatch, "rules": names})
		}
		fmt.Fprintf(out, "%s: %d regra(s) válidas (%s)\n", path, len(rs.Rules), map[bool]string{true: "primeira que casar", false: "todas que casarem"}[rs.FirstMatch])
		for i, r := range rs.Rules {
			fmt.Fprintf(out, "  %d. %s\n", i+1, r.Name)
		}
		return nil
	}
	return fmt.Errorf("subcomando rules desconhecido %q\n%s", args[0], uso)
}
