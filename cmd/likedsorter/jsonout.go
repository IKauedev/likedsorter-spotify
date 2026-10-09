package main

import (
	"encoding/json"
	"io"
	"os"
	"slices"
)

// writeJSON imprime v indentado (saída padrão para scripts).
func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// stripJSON remove --json / --json=true de args e informa se estava presente.
func stripJSON(args []string) ([]string, bool) {
	var rest []string
	found := false
	for _, a := range args {
		switch a {
		case "--json", "-json", "--json=true", "-json=true":
			found = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, found
}

// withJSON roda fn; com --json, o texto de progresso vai para stderr e o resultado
// estruturado de fn é impresso em stdout como JSON. Erros continuam indo para stderr.
func withJSON(args []string, out io.Writer, fn func(args []string, text io.Writer) (any, error)) error {
	rest, asJSON := stripJSON(args)
	if !asJSON {
		_, err := fn(rest, out)
		return err
	}
	v, err := fn(rest, os.Stderr)
	if err != nil {
		return err
	}
	if v == nil {
		v = map[string]any{"ok": true}
	}
	return writeJSON(out, v)
}

// hasJSON informa se args pedem JSON (sem removê-lo).
func hasJSON(args []string) bool {
	_, ok := stripJSON(slices.Clone(args))
	return ok
}

func colorEnabled() bool {
	if _, no := os.LookupEnv("NO_COLOR"); no {
		return false
	}
	if !isTerminal(os.Stdout) {
		return false
	}
	_, wt := os.LookupEnv("WT_SESSION")
	return wt || os.Getenv("TERM") != ""
}

// paint pinta s com o código ANSI quando o terminal suporta cor.
func paint(code, s string) string {
	if !colorEnabled() {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func errWriter() io.Writer { return os.Stderr }
