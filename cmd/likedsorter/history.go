package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/auth"
	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/plays"
)

// errNoListening indica que o login atual não inclui o histórico de reprodução.
var errNoListening = errors.New("seu login não inclui o histórico de reprodução")

func playsPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plays.json"), nil
}

// requireListeningScopes falha com instruções claras se o login não incluiu o histórico de reprodução.
func requireListeningScopes() error {
	store, err := tokenStore()
	if err != nil {
		return err
	}
	sv, err := store.Load()
	if err != nil {
		return err
	}
	if miss := auth.MissingListening(sv.Scope); len(miss) > 0 {
		return fmt.Errorf("%w (faltam: %s): rode `likedsorter auth login` de novo", errNoListening, strings.Join(miss, ", "))
	}
	return nil
}

// syncRecentPlays lê as reproduções recentes e as acrescenta ao histórico local. Devolve quantas são novas.
func syncRecentPlays(ctx context.Context) (added int, data *plays.Data, err error) {
	if err := requireListeningScopes(); err != nil {
		return 0, nil, err
	}
	path, err := playsPath()
	if err != nil {
		return 0, nil, err
	}
	data, err = plays.Load(path)
	if err != nil {
		return 0, nil, err
	}
	sess, err := newSession(ctx, cur.Market)
	if err != nil {
		return 0, nil, err
	}
	events, err := sess.Client.RecentlyPlayed(ctx, data.CursorMs)
	if err != nil {
		return 0, nil, err
	}
	for _, ev := range events {
		if data.Add(ev.Track.ID, ev.PlayedAt, plays.Meta{Name: ev.Track.Name, Artists: ev.Track.Artists}) {
			added++
		}
		data.AdvanceCursor(ev.PlayedAt)
	}
	if err := data.Save(path); err != nil {
		return 0, nil, err
	}
	return added, data, nil
}

// runHistory: history sync | stats | import | path | clear.
func runHistory(ctx context.Context, args []string, out io.Writer) error {
	const uso = `uso:
  likedsorter history sync                    acumula as músicas tocadas recentemente (últimas 50 por chamada)
  likedsorter history stats [--days 30] [--top 20] [--json]
  likedsorter history import ARQUIVO.json...  importa o histórico estendido exportado pelo Spotify
  likedsorter history path | clear [--yes]`
	if len(args) == 0 {
		return errors.New(uso)
	}
	path, err := playsPath()
	if err != nil {
		return err
	}
	switch args[0] {
	case "path":
		fmt.Fprintln(out, path)
		return nil

	case "sync":
		added, data, err := syncRecentPlays(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%d reprodução(ões) nova(s); histórico local: %d no total (%s)\n", added, len(data.Events), path)
		if added == 0 && len(data.Events) == 0 {
			fmt.Fprintln(out, "O Spotify só informa as últimas 50 reproduções: toque algumas músicas e rode de novo, ou agende com `likedsorter schedule install` para acumular sozinho.")
		}
		return nil

	case "import":
		if len(args) < 2 {
			return errors.New(uso)
		}
		data, err := plays.Load(path)
		if err != nil {
			return err
		}
		totalAdded := 0
		for _, f := range args[1:] {
			fh, err := os.Open(f)
			if err != nil {
				return err
			}
			added, skipped, err := data.ImportExtended(fh)
			fh.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
			fmt.Fprintf(out, "%s: +%d reproduções (%d ignoradas: curtas <30 s, podcasts ou repetidas)\n", f, added, skipped)
			totalAdded += added
		}
		if err := data.Save(path); err != nil {
			return err
		}
		fmt.Fprintf(out, "Histórico local: %d reproduções (+%d agora).\n", len(data.Events), totalAdded)
		return nil

	case "clear":
		fl := flag.NewFlagSet("history clear", flag.ContinueOnError)
		yes := fl.Bool("yes", false, "não pedir confirmação")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			fmt.Fprintln(out, "Não há histórico local.")
			return nil //nolint:nilerr // sem arquivo: avisa e segue
		}
		if ok, err := confirmYes(out, "Apagar o histórico local de reproduções ("+path+")?", *yes); err != nil {
			return err
		} else if !ok {
			fmt.Fprintln(out, "Nada foi alterado.")
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Fprintln(out, "Histórico apagado.")
		return nil

	case "stats":
		fl := flag.NewFlagSet("history stats", flag.ContinueOnError)
		days := fl.Int("days", 30, "janela em dias (0 = desde sempre)")
		top := fl.Int("top", 20, "quantas faixas mostrar")
		asJSON := fl.Bool("json", false, "saída em JSON")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		data, err := plays.Load(path)
		if err != nil {
			return err
		}
		st := data.Stats()
		if !st.HasHistory() {
			return errors.New("histórico local vazio: rode `likedsorter history sync` ou `likedsorter history import ARQUIVO.json`")
		}
		var since time.Time
		if *days > 0 {
			since = time.Now().AddDate(0, 0, -*days)
		}
		ranked := st.Top(since, *top)
		first, last := st.Span()
		type row struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Artists []string `json:"artists"`
			Plays   int      `json:"plays"`
			Last    string   `json:"last_played"`
		}
		rows := make([]row, len(ranked))
		for i, r := range ranked {
			m := data.Meta[r.ID]
			rows[i] = row{r.ID, m.Name, m.Artists, r.Count, r.Last.Local().Format("2006-01-02")}
			if m.Name == "" {
				rows[i].Name = r.ID
			}
		}
		if *asJSON {
			return writeJSON(out, map[string]any{"total_plays": len(data.Events), "first": first, "last": last, "days": *days, "top": rows})
		}
		fmt.Fprintf(out, "Histórico: %d reproduções de %s a %s\n\n", len(data.Events), first.Local().Format("2006-01-02"), last.Local().Format("2006-01-02"))
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PLAYS\tÚLTIMA\tFAIXA")
		for _, r := range rows {
			fmt.Fprintf(tw, "%d\t%s\t%s\n", r.Plays, r.Last, strings.TrimSpace(strings.Join(r.Artists, ", ")+": "+r.Name))
		}
		return tw.Flush()
	}
	return fmt.Errorf("subcomando history desconhecido %q\n%s", args[0], uso)
}
