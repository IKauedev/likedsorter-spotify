package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// runTop mostra o que você mais ouve (ranking do Spotify) e pode jogar as faixas numa playlist.
func runTop(ctx context.Context, args []string, out io.Writer) error {
	const uso = "uso: likedsorter top tracks|artists [--range short|medium|long] [--limit 50] [--json] [--to-playlist NOME [--yes]]"
	if len(args) == 0 || (args[0] != "tracks" && args[0] != "artists") {
		return errors.New(uso)
	}
	kind := args[0]
	fl := flag.NewFlagSet("top "+kind, flag.ContinueOnError)
	rng := fl.String("range", "medium", "short (~4 semanas) | medium (~6 meses) | long (~1 ano ou mais)")
	limit := fl.Int("limit", 50, "quantos itens (máx. 50)")
	asJSON := fl.Bool("json", false, "saída em JSON")
	to := fl.String("to-playlist", "", "(tracks) adicionar a esta playlist, criada se não existir")
	yes := fl.Bool("yes", false, "com --to-playlist: não pedir confirmação")
	if err := fl.Parse(args[1:]); err != nil {
		return err
	}
	tr, ok := spotify.ParseTimeRange(*rng)
	if !ok {
		return fmt.Errorf("--range deve ser short, medium ou long, recebi %q", *rng)
	}
	if *limit < 1 || *limit > 50 {
		return errors.New("--limit deve estar entre 1 e 50")
	}
	if *to != "" && kind != "tracks" {
		return errors.New("--to-playlist só vale para `top tracks`")
	}
	if err := requireListeningScopes(); err != nil {
		return err
	}
	sess, err := newSession(ctx, cur.Market)
	if err != nil {
		return err
	}

	if kind == "artists" {
		artists, err := sess.Client.TopArtists(ctx, tr, 0, *limit)
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(out, artists)
		}
		for i, a := range artists {
			fmt.Fprintf(out, "%2d. %s\n", i+1, a.Name)
		}
		return nil
	}

	tracks, err := sess.Client.TopTracks(ctx, tr, 0, *limit)
	if err != nil {
		return err
	}
	if *to != "" {
		text := out
		if *asJSON {
			text = errWriter()
		}
		for i, t := range tracks {
			fmt.Fprintf(text, "%2d. %s\n", i+1, t.Label())
		}
		res, err := addToPlaylistRes(ctx, sess.Client, addOptions{To: *to, Create: true, Resolved: tracks}, terminalUI(text, *yes), text)
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(out, res)
		}
		return nil
	}
	if *asJSON {
		return writeJSON(out, tracks)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for i, t := range tracks {
		fmt.Fprintf(tw, "%d.\t%s\n", i+1, t.Label())
	}
	return tw.Flush()
}
