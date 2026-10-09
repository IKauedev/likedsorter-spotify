package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/planner"
)

// Formatos aceitos em --format.
const (
	FormatTable = "table"
	FormatJSON  = "json"
	FormatCSV   = "csv"
	FormatMD    = "md"
)

// Data é tudo que um relatório mostra.
type Data struct {
	Strategy    string
	GeneratedAt time.Time
	Stats       Stats
	Plan        planner.Plan
	Detail      bool   // JSON: incluir as URIs a adicionar/remover
	NoRemote    bool   // playlists existentes não foram consultadas
	Filter      string // descrição do filtro ativo ("" = nenhum)
}

// Render escreve o relatório no formato pedido.
func Render(w io.Writer, format string, d Data) error {
	switch strings.ToLower(format) {
	case FormatTable, "":
		return renderTable(w, d)
	case FormatJSON:
		return renderJSON(w, d)
	case FormatCSV:
		return renderCSV(w, d)
	case FormatMD, "markdown":
		return renderMD(w, d)
	}
	return fmt.Errorf("formato desconhecido %q (use table, json, csv ou md)", format)
}

var actionLabel = map[planner.Action]string{
	planner.Create: "criar", planner.Update: "atualizar", planner.Keep: "manter", planner.Orphan: "órfã (intocada)",
}

func ignoredLine(s Stats) string {
	return fmt.Sprintf("%d locais, %d indisponíveis, %d sem ID, %d duplicadas",
		s.Ignored.Local, s.Ignored.Unavailable, s.Ignored.NoID, s.Ignored.Duplicates)
}

func sizesLine(s Stats) string {
	var parts []string
	for _, b := range s.Sizes {
		if b.Playlists > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", b.Label, b.Playlists))
		}
	}
	if len(parts) == 0 {
		return "(nenhuma)"
	}
	return strings.Join(parts, " | ")
}

func totalsLine(t planner.Totals) string {
	return fmt.Sprintf("%d a criar, %d a atualizar, %d a manter, %d órfãs | +%d faixas, -%d faixas (remoção só com --allow-remove)",
		t.Create, t.Update, t.Keep, t.Orphan, t.Add, t.Remove)
}

func stamp(t time.Time) string { return t.Format("2006-01-02 15:04") }

// ---------------------------------------------------------------- tabela

func renderTable(w io.Writer, d Data) error {
	s := d.Stats
	fmt.Fprintf(w, "likedsorter | plano (dry-run, nada foi alterado) | estratégia: %s | %s\n", d.Strategy, stamp(d.GeneratedAt))
	if d.NoRemote {
		fmt.Fprintln(w, "AVISO: playlists existentes não foram consultadas (--skip-existing); tudo aparece como \"criar\".")
	}
	if d.Filter != "" {
		fmt.Fprintf(w, "FILTRO ATIVO: %s (remoções não são calculadas)\n", d.Filter)
	}
	fmt.Fprintln(w, "\nESTATÍSTICAS")
	fmt.Fprintf(w, "  Curtidas no Spotify: %d | úteis: %d (ignoradas: %s)\n", s.Liked, s.Usable, ignoredLine(s))
	fmt.Fprintf(w, "  Playlists: %d | faixas atribuídas: %d | ignoradas por grupo pequeno: %d\n", s.Groups, s.Assigned, s.SkippedSmall)
	if s.GenresLoaded {
		fmt.Fprintf(w, "  Faixas sem gênero: %d\n", s.NoGenre)
	} else {
		fmt.Fprintln(w, "  Faixas sem gênero: n/d (gêneros não carregados nesta estratégia)")
	}
	fmt.Fprintf(w, "  Tamanho das playlists: %s\n", sizesLine(s))
	fmt.Fprintln(w, "  Top artistas:", countsInline(s.TopArtists))
	if s.GenresLoaded {
		fmt.Fprintln(w, "  Top gêneros:", countsInline(s.TopGenres))
	}

	fmt.Fprintln(w, "\nPLANO")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AÇÃO\tPLAYLIST\tDESEJADAS\tMANTER\tADICIONAR\tREMOVER")
	for _, it := range d.Plan.Items {
		if it.Action == planner.Orphan {
			fmt.Fprintf(tw, "%s\t%s\t-\t%d\t-\t-\n", actionLabel[it.Action], it.Name, it.Keep)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\n", actionLabel[it.Action], it.Name, it.Desired, it.Keep, len(it.Add), len(it.Remove))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nResumo: %s\n", totalsLine(d.Plan.Totals()))
	if len(d.Plan.Warnings) > 0 {
		fmt.Fprintln(w, "\nAVISOS")
		for _, m := range d.Plan.Warnings {
			fmt.Fprintln(w, "  -", m)
		}
	}
	return nil
}

func countsInline(cs []Count) string {
	if len(cs) == 0 {
		return "(nenhum)"
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = fmt.Sprintf("%s (%d)", c.Name, c.Tracks)
	}
	return strings.Join(parts, ", ")
}

// -------------------------------------------------------------- markdown

func mdEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

func renderMD(w io.Writer, d Data) error {
	s := d.Stats
	fmt.Fprintf(w, "# Plano do likedsorter\n\n_Dry-run: nada foi alterado._ Estratégia: `%s` · %s\n\n", d.Strategy, stamp(d.GeneratedAt))
	if d.NoRemote {
		fmt.Fprint(w, "> **Aviso:** playlists existentes não foram consultadas (`--skip-existing`); tudo aparece como \"criar\".\n\n")
	}
	if d.Filter != "" {
		fmt.Fprintf(w, "> **Filtro ativo:** %s (remoções não são calculadas)\n\n", mdEscape(d.Filter))
	}
	fmt.Fprint(w, "## Estatísticas\n\n")
	fmt.Fprintf(w, "- Curtidas no Spotify: **%d** · úteis: **%d** (ignoradas: %s)\n", s.Liked, s.Usable, ignoredLine(s))
	fmt.Fprintf(w, "- Playlists: **%d** · faixas atribuídas: **%d** · ignoradas por grupo pequeno: %d\n", s.Groups, s.Assigned, s.SkippedSmall)
	if s.GenresLoaded {
		fmt.Fprintf(w, "- Faixas sem gênero: **%d**\n", s.NoGenre)
	}
	fmt.Fprintf(w, "- Tamanho das playlists: %s\n\n", sizesLine(s))
	mdTop(w, "Top artistas", s.TopArtists)
	if s.GenresLoaded {
		mdTop(w, "Top gêneros", s.TopGenres)
	}

	fmt.Fprint(w, "## Plano\n\n")
	fmt.Fprintln(w, "| Ação | Playlist | Desejadas | Manter | Adicionar | Remover |")
	fmt.Fprintln(w, "|---|---|--:|--:|--:|--:|")
	for _, it := range d.Plan.Items {
		if it.Action == planner.Orphan {
			fmt.Fprintf(w, "| %s | %s | - | %d | - | - |\n", actionLabel[it.Action], mdEscape(it.Name), it.Keep)
			continue
		}
		fmt.Fprintf(w, "| %s | %s | %d | %d | %d | %d |\n", actionLabel[it.Action], mdEscape(it.Name), it.Desired, it.Keep, len(it.Add), len(it.Remove))
	}
	fmt.Fprintf(w, "\n**Resumo:** %s\n", totalsLine(d.Plan.Totals()))
	if len(d.Plan.Warnings) > 0 {
		fmt.Fprint(w, "\n## Avisos\n\n")
		for _, m := range d.Plan.Warnings {
			fmt.Fprintf(w, "- %s\n", m)
		}
	}
	return nil
}

func mdTop(w io.Writer, title string, cs []Count) {
	fmt.Fprintf(w, "**%s**\n\n", title)
	if len(cs) == 0 {
		fmt.Fprint(w, "_(nenhum)_\n\n")
		return
	}
	for i, c := range cs {
		fmt.Fprintf(w, "%d. %s (%d)\n", i+1, mdEscape(c.Name), c.Tracks)
	}
	fmt.Fprintln(w)
}

// ------------------------------------------------------------------- csv

func renderCSV(w io.Writer, d Data) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"action", "name", "key", "part", "parts", "existing_id", "desired", "keep", "add", "remove", "duplicates"})
	for _, it := range d.Plan.Items {
		_ = cw.Write([]string{
			string(it.Action), csvSafe(it.Name), csvSafe(it.Key), strconv.Itoa(it.Part), strconv.Itoa(it.Parts), it.ExistingID,
			strconv.Itoa(it.Desired), strconv.Itoa(it.Keep), strconv.Itoa(len(it.Add)), strconv.Itoa(len(it.Remove)), strconv.Itoa(it.Duplicates),
		})
	}
	cw.Flush()
	return cw.Error()
}

// csvSafe neutraliza "injeção de fórmula" ao abrir o CSV em planilhas:
// nomes de artistas/grupos vêm de dados externos e podem começar com = + - @.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// ------------------------------------------------------------------ json

type jsonItem struct {
	Action      planner.Action `json:"action"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Part        int            `json:"part,omitempty"`
	Parts       int            `json:"parts,omitempty"`
	ExistingID  string         `json:"existing_id,omitempty"`
	Desired     int            `json:"desired"`
	Keep        int            `json:"keep"`
	AddCount    int            `json:"add_count"`
	RemoveCount int            `json:"remove_count"`
	Duplicates  int            `json:"duplicates"`
	Add         []string       `json:"add,omitempty"`
	Remove      []string       `json:"remove,omitempty"`
}

type jsonOut struct {
	DryRun      bool       `json:"dry_run"`
	Strategy    string     `json:"strategy"`
	GeneratedAt string     `json:"generated_at"`
	NoRemote    bool       `json:"existing_playlists_not_checked,omitempty"`
	Filter      string     `json:"filter,omitempty"`
	Stats       Stats      `json:"stats"`
	Totals      jsonTotals `json:"totals"`
	Playlists   []jsonItem `json:"playlists"`
	Warnings    []string   `json:"warnings,omitempty"`
}

type jsonTotals struct {
	Create int `json:"create"`
	Update int `json:"update"`
	Keep   int `json:"keep"`
	Orphan int `json:"orphan"`
	Add    int `json:"add"`
	Remove int `json:"remove"`
}

func renderJSON(w io.Writer, d Data) error {
	t := d.Plan.Totals()
	out := jsonOut{
		DryRun: true, Strategy: d.Strategy, GeneratedAt: d.GeneratedAt.Format(time.RFC3339), NoRemote: d.NoRemote, Filter: d.Filter,
		Stats: d.Stats, Warnings: d.Plan.Warnings, Playlists: []jsonItem{},
		Totals: jsonTotals{t.Create, t.Update, t.Keep, t.Orphan, t.Add, t.Remove},
	}
	for _, it := range d.Plan.Items {
		ji := jsonItem{
			Action: it.Action, Key: it.Key, Name: it.Name, Part: it.Part, Parts: it.Parts, ExistingID: it.ExistingID,
			Desired: it.Desired, Keep: it.Keep, AddCount: len(it.Add), RemoveCount: len(it.Remove), Duplicates: it.Duplicates,
		}
		if d.Detail {
			ji.Add, ji.Remove = it.Add, it.Remove
		}
		out.Playlists = append(out.Playlists, ji)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
