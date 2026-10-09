package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/filter"
)

// cur são os padrões efetivos (padrões embutidos ← config.yaml ← ambiente).
// Os comandos os usam como DEFAULT das flags, então passar a flag sempre vence.
var cur = config.DefaultSettings()

// appLogger é o logger estruturado (stderr). Substituído em setupLogging.
var appLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// globals são flags aceitas em qualquer posição da linha de comando.
type globals struct {
	configPath string
	logLevel   string
	verbose    bool
}

// splitGlobals remove --config, --log-level e --verbose/-v de args, onde estiverem.
func splitGlobals(args []string) ([]string, globals, error) {
	var g globals
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(a, "=")
		next := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s exige um valor", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--config":
			v, err := next()
			if err != nil {
				return nil, g, err
			}
			g.configPath = v
		case "--log-level":
			v, err := next()
			if err != nil {
				return nil, g, err
			}
			g.logLevel = v
		case "--verbose", "-v":
			g.verbose = !hasVal || val == "true"
		default:
			rest = append(rest, a)
		}
	}
	return rest, g, nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning", "":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("--log-level %q inválido (use debug, info, warn ou error)", s)
}

// setupLogging cria o logger. Precedência: --verbose > --log-level > config/env > padrão.
func setupLogging(g globals, w io.Writer) error {
	level := cur.LogLevel
	if g.logLevel != "" {
		level = g.logLevel
	}
	lv, err := parseLevel(level)
	if err != nil {
		return err
	}
	if g.verbose {
		lv = slog.LevelDebug
	}
	appLogger = slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lv}))
	slog.SetDefault(appLogger)
	return nil
}

// filterFlags são os filtros para processar só parte da biblioteca.
type filterFlags struct{ artist, genre, since string }

func (f *filterFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.artist, "filter-artist", "", "só faixas cujo(s) artista(s) contenham estes trechos (separados por vírgula)")
	fs.StringVar(&f.genre, "filter-genre", "", "só faixas cujo artista principal tenha gênero contendo estes trechos (separados por vírgula)")
	fs.StringVar(&f.since, "since", "", "só faixas curtidas a partir desta data (AAAA-MM-DD)")
}

func (f *filterFlags) build() (filter.Filter, error) {
	since, err := filter.ParseSince(f.since)
	if err != nil {
		return filter.Filter{}, err
	}
	return filter.Filter{Artists: filter.SplitList(f.artist), Genres: filter.SplitList(f.genre), Since: since}, nil
}

func warnf(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }
