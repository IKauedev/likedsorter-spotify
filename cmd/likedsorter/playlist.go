package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

var (
	trackURIRe = regexp.MustCompile(`^spotify:track:([0-9A-Za-z]{22})$`)
	trackURLRe = regexp.MustCompile(`^https?://open\.spotify\.com/(?:intl-[a-z-]+/)?track/([0-9A-Za-z]{22})(?:[/?#].*)?$`)
	bareIDRe   = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)
	plRefRe    = regexp.MustCompile(`^(?:spotify:playlist:|https?://open\.spotify\.com/(?:intl-[a-z-]+/)?playlist/)([0-9A-Za-z]{22})(?:[/?#].*)?$`)
)

// parseTrackRef reconhece URI, link ou ID de faixa do Spotify. Qualquer outro texto
// vira busca (ok=false). notTrack=true quando é um link de álbum/playlist/artista.
func parseTrackRef(s string) (id string, ok, notTrack bool) {
	s = strings.TrimSpace(s)
	for _, re := range []*regexp.Regexp{trackURIRe, trackURLRe} {
		if m := re.FindStringSubmatch(s); m != nil {
			return m[1], true, false
		}
	}
	if strings.HasPrefix(s, "spotify:") || strings.HasPrefix(s, "https://open.spotify.com/") {
		return "", false, true
	}
	// ID "puro" de 22 caracteres: só aceita se não tiver cara de texto de busca.
	if bareIDRe.MatchString(s) && strings.ContainsAny(s, "0123456789") {
		return s, true, false
	}
	return "", false, false
}

// parsePlaylistRef extrai o ID de um link/URI de playlist ("" se não for).
func parsePlaylistRef(s string) string {
	if m := plRefRe.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		return m[1]
	}
	return ""
}

// playlistUI abstrai as perguntas (terminal comum ou menu interativo).
type playlistUI struct {
	// choose mostra opções 1..n e devolve o índice 0-based, ou -1 para pular.
	choose  func(prompt string, labels []string) (int, error)
	confirm func(question string) (bool, error)
}

type addOptions struct {
	To     string // nome, ID, link ou URI da playlist
	Create bool
	Public bool
	Pick   bool // escolher entre os resultados de cada busca
	Items  []string

	// Resolved dispensa a resolução de Items (ex.: faixas que já temos do cache).
	Resolved []spotify.TrackInfo
}

func allMyPlaylists(ctx context.Context, c *spotify.Client) ([]spotify.Playlist, error) {
	var all []spotify.Playlist
	for off := 0; ; off += 50 {
		pg, err := c.MyPlaylists(ctx, off, 50)
		if err != nil {
			return nil, err
		}
		all = append(all, pg.Items...)
		if !pg.HasNext || len(pg.Items) == 0 {
			return all, nil
		}
	}
}

// findPlaylist resolve por ID/link ou por nome (sem diferenciar maiúsculas).
func findPlaylist(pls []spotify.Playlist, ref string) (*spotify.Playlist, error) {
	if id := parsePlaylistRef(ref); id != "" {
		for i := range pls {
			if pls[i].ID == id {
				return &pls[i], nil
			}
		}
		return nil, fmt.Errorf("playlist %s não está entre as suas", id)
	}
	var hit []int
	for i := range pls {
		if strings.EqualFold(pls[i].Name, strings.TrimSpace(ref)) || pls[i].ID == ref {
			hit = append(hit, i)
		}
	}
	switch len(hit) {
	case 0:
		return nil, errNoPlaylist
	case 1:
		return &pls[hit[0]], nil
	}
	return nil, fmt.Errorf("%d playlists se chamam %q; use o link/ID da que você quer (veja `likedsorter playlist list`)", len(hit), ref)
}

var errNoPlaylist = errors.New("playlist não encontrada")

func existingURIs(ctx context.Context, c *spotify.Client, id string) (map[string]bool, error) {
	have := map[string]bool{}
	for off := 0; ; off += 50 {
		pg, err := c.PlaylistItems(ctx, id, off, 50)
		if err != nil {
			return nil, err
		}
		for _, it := range pg.Items {
			if it.URI != "" {
				have[it.URI] = true
			}
		}
		if !pg.HasNext || len(pg.Items) == 0 {
			return have, nil
		}
	}
}

// resolveItems transforma links/IDs/buscas em faixas. Devolve também o que foi ignorado.
func resolveItems(ctx context.Context, c *spotify.Client, items []string, pick bool, ui playlistUI, out io.Writer) ([]spotify.TrackInfo, error) {
	var tracks []spotify.TrackInfo
	for _, raw := range items {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, ok, notTrack := parseTrackRef(raw)
		switch {
		case notTrack:
			fmt.Fprintf(out, "  ignorado (só faixas são aceitas): %s\n", raw)
		case ok:
			t, err := c.Track(ctx, id)
			if err != nil {
				fmt.Fprintf(out, "  não encontrei a faixa %s: %v\n", id, err)
				continue
			}
			tracks = append(tracks, *t)
		default:
			res, err := c.SearchTracks(ctx, raw, 5)
			if err != nil {
				return nil, err
			}
			if len(res) == 0 {
				fmt.Fprintf(out, "  sem resultados para %q\n", raw)
				continue
			}
			chosen := 0
			if pick && len(res) > 1 {
				labels := make([]string, len(res))
				for i, r := range res {
					labels[i] = r.Label()
				}
				if chosen, err = ui.choose(fmt.Sprintf("Resultados para %q", raw), labels); err != nil {
					return nil, err
				}
				if chosen < 0 {
					continue
				}
			}
			tracks = append(tracks, res[chosen])
		}
	}
	return tracks, nil
}

// addToPlaylist adiciona faixas a uma playlist SUA (ou colaborativa). Só acrescenta;
// nada é removido nem reordenado, e faixas que já estão na playlist são puladas.
// addResult resume o que addToPlaylist fez (para --json).
type addResult struct {
	Playlist string   `json:"playlist"`
	Created  bool     `json:"created"`
	Added    []string `json:"added"`
	Skipped  int      `json:"skipped"`
}

func addToPlaylist(ctx context.Context, c *spotify.Client, o addOptions, ui playlistUI, out io.Writer) error {
	_, err := addToPlaylistRes(ctx, c, o, ui, out)
	return err
}

func addToPlaylistRes(ctx context.Context, c *spotify.Client, o addOptions, ui playlistUI, out io.Writer) (*addResult, error) {
	res := &addResult{Added: []string{}}
	if strings.TrimSpace(o.To) == "" {
		return nil, errors.New("informe a playlist de destino (--to \"Nome\")")
	}
	if len(o.Items) == 0 && len(o.Resolved) == 0 {
		return nil, errors.New("informe ao menos uma música (link, URI, ID ou texto de busca)")
	}
	me, err := c.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	pls, err := allMyPlaylists(ctx, c)
	if err != nil {
		return nil, err
	}
	target, err := findPlaylist(pls, o.To)
	createNew := false
	switch {
	case errors.Is(err, errNoPlaylist):
		if !o.Create {
			return nil, fmt.Errorf("%w: %q (use --create para criá-la)", errNoPlaylist, o.To)
		}
		createNew = true
	case err != nil:
		return nil, err
	case target.OwnerID != me && !target.Collaborative:
		return nil, fmt.Errorf("a playlist %q pertence a outra pessoa; só dá para adicionar nas suas ou colaborativas", target.Name)
	}

	tracks := o.Resolved
	if len(tracks) == 0 {
		if tracks, err = resolveItems(ctx, c, o.Items, o.Pick, ui, out); err != nil {
			return nil, err
		}
	}
	if len(tracks) == 0 {
		return nil, errors.New("nenhuma música válida para adicionar")
	}

	have := map[string]bool{}
	if !createNew {
		if have, err = existingURIs(ctx, c, target.ID); err != nil {
			return nil, fmt.Errorf("ler %q: %w", target.Name, err)
		}
	}
	var uris []string
	var add []spotify.TrackInfo
	skipped := 0
	for _, t := range tracks {
		if have[t.URI] {
			skipped++
			continue
		}
		have[t.URI] = true
		uris, add = append(uris, t.URI), append(add, t)
	}

	name := o.To
	if !createNew {
		name = target.Name
	}
	fmt.Fprintf(out, "\nDestino: %s%s\n", name, map[bool]string{true: " (será criada)", false: ""}[createNew])
	for _, t := range add {
		fmt.Fprintf(out, "  + %s\n", t.Label())
	}
	if skipped > 0 {
		fmt.Fprintf(out, "  (%d já estavam na playlist ou repetidas; puladas)\n", skipped)
	}
	if len(uris) == 0 {
		fmt.Fprintln(out, "Nada a adicionar.")
		return res, nil
	}
	if ok, err := ui.confirm(fmt.Sprintf("Adicionar %d música(s) a %q?", len(uris), name)); err != nil {
		return nil, err
	} else if !ok {
		fmt.Fprintln(out, "Nada foi alterado.")
		return res, nil
	}

	if createNew {
		p, err := c.CreatePlaylist(ctx, name, "Criada pelo likedsorter", o.Public)
		if err != nil {
			return nil, err
		}
		target = p
	}
	res.Playlist, res.Created, res.Skipped = name, createNew, skipped
	for i := 0; i < len(uris); i += spotify.MaxItemsPerRequest {
		end := min(i+spotify.MaxItemsPerRequest, len(uris))
		if err := c.AddItems(ctx, target.ID, uris[i:end]); err != nil {
			return nil, fmt.Errorf("%w\n(rode o mesmo comando de novo: o que já entrou é pulado)", err)
		}
	}
	res.Added = uris
	fmt.Fprintf(out, "Adicionadas %d música(s) a %q.\n", len(uris), name)
	return res, nil
}

// terminalUI lê respostas do stdin. yes=true aprova sem perguntar.
func terminalUI(out io.Writer, yes bool) playlistUI {
	r := bufio.NewReader(os.Stdin)
	line := func() (string, error) {
		s, err := r.ReadString('\n')
		if err != nil && (err != io.EOF || s == "") {
			return "", err
		}
		return strings.TrimSpace(s), nil
	}
	return playlistUI{
		choose: func(prompt string, labels []string) (int, error) {
			fmt.Fprintln(out, prompt)
			for i, l := range labels {
				fmt.Fprintf(out, "  %d) %s\n", i+1, l)
			}
			for {
				fmt.Fprint(out, "Escolha (Enter = 1, 0 = pular): ")
				s, err := line()
				if err != nil {
					return -1, err
				}
				if s == "" {
					return 0, nil
				}
				if n, e := strconv.Atoi(s); e == nil && n >= 0 && n <= len(labels) {
					return n - 1, nil
				}
				fmt.Fprintln(out, "Opção inválida.")
			}
		},
		confirm: func(q string) (bool, error) {
			if yes {
				return true, nil
			}
			if !isTerminal(os.Stdin) {
				return false, errors.New("confirmação necessária: rode em um terminal ou passe --yes")
			}
			fmt.Fprintf(out, "%s (s/N): ", q)
			s, err := line()
			if err != nil {
				return false, err
			}
			switch strings.ToLower(s) {
			case "s", "sim", "y", "yes":
				return true, nil
			}
			return false, nil
		},
	}
}

func readItemsFile(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			items = append(items, l)
		}
	}
	return items, nil
}

// runPlaylist: playlist list | add | search.
func runPlaylist(ctx context.Context, args []string, out io.Writer) error {
	const uso = `uso:
  likedsorter playlist list [--json]
  likedsorter playlist search "texto"
  likedsorter playlist add --to "Nome" [--create] [--public] [--pick] [--yes] [--from-file arq.txt] MÚSICA...
    MÚSICA = link, URI ou ID da faixa, ou texto de busca (ex.: "djavan sina")`
	if len(args) == 0 {
		return errors.New(uso)
	}
	sess, err := newSession(ctx, cur.Market)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		fl := flag.NewFlagSet("playlist list", flag.ContinueOnError)
		asJSON := fl.Bool("json", false, "saída em JSON")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		me, err := sess.Client.CurrentUserID(ctx)
		if err != nil {
			return err
		}
		pls, err := allMyPlaylists(ctx, sess.Client)
		if err != nil {
			return err
		}
		if *asJSON {
			type row struct {
				ID, Name string
				Tracks   int
				Owned    bool
				Public   bool
				Collab   bool
			}
			rows := make([]row, len(pls))
			for i, p := range pls {
				rows[i] = row{p.ID, p.Name, p.Total, p.OwnerID == me, p.Public, p.Collaborative}
			}
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "FAIXAS\tDONA\tNOME\tID")
		for _, p := range pls {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", p.Total, map[bool]string{true: "você", false: "outra pessoa"}[p.OwnerID == me], p.Name, p.ID)
		}
		return tw.Flush()

	case "search":
		if len(args) < 2 {
			return errors.New(uso)
		}
		sargs, asJSON := stripJSON(args[1:])
		res, err := sess.Client.SearchTracks(ctx, strings.Join(sargs, " "), spotify.MaxSearchLimit)
		if err != nil {
			return err
		}
		if asJSON {
			type tj struct {
				URI    string   `json:"uri"`
				Name   string   `json:"name"`
				Artist []string `json:"artists"`
				Album  string   `json:"album"`
				Year   string   `json:"year"`
			}
			js := make([]tj, len(res))
			for i, t := range res {
				js[i] = tj{t.URI, t.Name, t.Artists, t.Album, t.Year}
			}
			return writeJSON(out, js)
		}
		if len(res) == 0 {
			fmt.Fprintln(out, "Sem resultados.")
		}
		for _, t := range res {
			fmt.Fprintf(out, "%s\n    %s\n", t.Label(), t.URI)
		}
		return nil

	case "add":
		fl := flag.NewFlagSet("playlist add", flag.ContinueOnError)
		to := fl.String("to", "", "playlist de destino: nome, link, URI ou ID")
		create := fl.Bool("create", false, "criar a playlist se não existir")
		public := fl.Bool("public", false, "ao criar, deixar pública (padrão: privada)")
		pick := fl.Bool("pick", false, "escolher entre os resultados de cada busca (padrão: o primeiro)")
		yes := fl.Bool("yes", false, "não pedir confirmação")
		asJSON := fl.Bool("json", false, "saída em JSON (o texto vai para stderr)")
		file := fl.String("from-file", "", "arquivo com uma música por linha (# comenta)")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		items := fl.Args()
		if *file != "" {
			more, err := readItemsFile(*file)
			if err != nil {
				return err
			}
			items = append(items, more...)
		}
		text := out
		if *asJSON {
			text = os.Stderr
		}
		res, err := addToPlaylistRes(ctx, sess.Client, addOptions{To: *to, Create: *create, Public: *public, Pick: *pick, Items: items},
			terminalUI(text, *yes), text)
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(out, res)
		}
		return nil
	}
	return fmt.Errorf("subcomando playlist desconhecido %q\n%s", args[0], uso)
}
