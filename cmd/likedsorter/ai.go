package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/ai"
	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
	"github.com/ikauedeveloper/likedsorter/internal/grouping"
)

func aiStorePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ai_genres.json"), nil
}

// aiConfigFromEnv monta a configuração de IA a partir do .env/ambiente.
// ANTHROPIC_API_KEY e OPENAI_API_KEY servem de reserva para AI_API_KEY.
func aiConfigFromEnv() ai.Config {
	cfg, _ := config.Load() // sem Client ID o Load devolve erro, mas os campos de IA vêm preenchidos
	c := ai.Config{Provider: cfg.AIProvider, APIKey: cfg.AIKey, Model: cfg.AIModel, BaseURL: cfg.AIBaseURL}
	if c.APIKey == "" {
		switch c.Provider {
		case ai.ProviderAnthropic:
			c.APIKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		case ai.ProviderOpenAI:
			c.APIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		}
	}
	return c
}

// aiConsent explica o que será enviado e exige confirmação (exceto com modelo local ou --yes).
func aiConsent(out io.Writer, c ai.Config, what string, yes bool) error {
	if c.IsLocal() {
		fmt.Fprintf(out, "IA local (%s em %s): nada sai do seu computador.\n", c.Provider, c.Destination())
		return nil
	}
	fmt.Fprintf(out, "ATENÇÃO: %s serão enviados a %s (%s).\n", what, c.Destination(), c.Provider)
	fmt.Fprintln(out, "Nada é enviado do Spotify além disso: nem tokens, nem seu perfil. A resposta da IA só é aplicada depois da sua revisão.")
	ok, err := confirmYes(out, "Continuar?", yes)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("cancelado: nada foi enviado")
	}
	return nil
}

// runAI: ai setup | test | classify | rules | genres.
func runAI(ctx context.Context, args []string, out io.Writer) error {
	const uso = `uso:
  likedsorter ai setup                      configura provedor (anthropic, openai, ollama), chave e modelo
  likedsorter ai test                       testa a conexão com o modelo
  likedsorter ai classify [--limit 200] [--dry-run] [--yes]
                                            propõe gêneros para artistas "Sem gênero" (você revisa antes de salvar)
  likedsorter ai rules "instrução" [--out arq.yaml]
                                            gera um rules.yaml a partir do que você pede (ex.: "estudo, treino e festa")
  likedsorter ai genres list | clear        classificações da IA já salvas`
	if len(args) == 0 {
		return errors.New(uso)
	}
	switch args[0] {
	case "setup":
		return aiSetup(os.Stdin, out)
	case "test":
		return aiTest(ctx, out)
	case "classify":
		return aiClassify(ctx, args[1:], out)
	case "rules":
		return aiRules(ctx, args[1:], out)
	case "genres":
		return aiGenres(args[1:], out)
	}
	return fmt.Errorf("subcomando ai desconhecido %q\n%s", args[0], uso)
}

func aiSetup(in io.Reader, out io.Writer) error {
	if !stdinIsTerminal() {
		return errors.New("sem terminal: use `likedsorter setup --ai-provider ... --ai-key ... --ai-model ...`")
	}
	path, err := config.EnvFilePath()
	if err != nil {
		return err
	}
	cur, err := config.ReadEnvFile(path)
	if err != nil {
		return err
	}
	r := bufio.NewReader(in)
	ask := func(label, def string) (string, error) {
		if def != "" {
			fmt.Fprintf(out, "%s [%s]: ", label, def)
		} else {
			fmt.Fprintf(out, "%s: ", label)
		}
		s, err := r.ReadString('\n')
		if err != nil && (err != io.EOF || s == "") {
			return "", err
		}
		if s = strings.TrimSpace(s); s == "" {
			return def, nil
		}
		return s, nil
	}
	fmt.Fprintln(out, "Provedores: anthropic (Claude), openai, ollama (modelo local, sem enviar dados para fora).")
	prov, err := ask("Provedor", orDefault(cur[config.EnvAIProvider], "anthropic"))
	if err != nil {
		return err
	}
	prov = strings.ToLower(prov)
	c := ai.Config{Provider: prov, APIKey: cur[config.EnvAIKey], Model: cur[config.EnvAIModel], BaseURL: cur[config.EnvAIBaseURL]}
	vals := map[string]string{config.EnvAIProvider: prov}
	if prov != ai.ProviderOllama {
		key, err := ask("Chave de API (fica só no seu .env)", c.APIKey)
		if err != nil {
			return err
		}
		c.APIKey, vals[config.EnvAIKey] = key, key
	}
	model, err := ask("Modelo (Enter = padrão do provedor)", c.Model)
	if err != nil {
		return err
	}
	c.Model, vals[config.EnvAIModel] = model, model
	if prov == ai.ProviderOllama || c.BaseURL != "" {
		base, err := ask("URL base (Enter = padrão)", c.BaseURL)
		if err != nil {
			return err
		}
		c.BaseURL, vals[config.EnvAIBaseURL] = base, base
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if err := config.UpdateEnvFile(path, vals); err != nil {
		return err
	}
	for k, v := range vals {
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
	}
	fmt.Fprintf(out, "\nIA configurada em %s. Teste com: likedsorter ai test\n", path)
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func aiProvider() (ai.Provider, ai.Config, error) {
	c := aiConfigFromEnv()
	p, err := ai.New(c, nil)
	return p, c, err
}

func aiTest(ctx context.Context, out io.Writer) error {
	p, c, err := aiProvider()
	if err != nil {
		return err
	}
	start := time.Now()
	reply, err := p.Complete(ctx, "Responda sempre em uma única palavra.", "Responda apenas: ok")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s / %s respondeu %q em %s (destino: %s)\n", p.Name(), p.Model(), strings.TrimSpace(reply), time.Since(start).Round(time.Millisecond), c.Destination())
	return nil
}

func aiClassify(ctx context.Context, args []string, out io.Writer) error {
	fl := flag.NewFlagSet("ai classify", flag.ContinueOnError)
	var d dataFlags
	d.register(fl)
	limit := fl.Int("limit", 200, "máximo de artistas a enviar (os com mais faixas primeiro)")
	batchSize := fl.Int("batch", 40, "artistas por chamada")
	dry := fl.Bool("dry-run", false, "só mostrar as propostas, sem salvar")
	yes := fl.Bool("yes", false, "não pedir confirmação")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if *limit < 1 || *batchSize < 1 || *batchSize > 100 {
		return errors.New("--limit deve ser >= 1 e --batch entre 1 e 100")
	}
	p, c, err := aiProvider()
	if err != nil {
		return err
	}
	l, err := loadData(ctx, &d, true)
	if err != nil {
		return err
	}

	type cand struct {
		name   string
		tracks int
		titles []string
	}
	by := map[string]*cand{}
	for _, t := range l.Sync.Tracks {
		if len(t.Artists) == 0 {
			continue
		}
		a := t.Artists[0]
		if len(l.Infos[a.ID].Genres) > 0 {
			continue
		}
		cd := by[a.Name]
		if cd == nil {
			cd = &cand{name: a.Name}
			by[a.Name] = cd
		}
		cd.tracks++
		if len(cd.titles) < 3 {
			cd.titles = append(cd.titles, t.Name)
		}
	}
	var cands []*cand
	for _, cd := range by {
		cands = append(cands, cd)
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].tracks != cands[j].tracks {
			return cands[i].tracks > cands[j].tracks
		}
		return cands[i].name < cands[j].name
	})
	if len(cands) == 0 {
		fmt.Fprintln(out, "Nenhum artista sem gênero: nada a classificar.")
		return nil
	}
	if len(cands) > *limit {
		cands = cands[:*limit]
	}
	if err := aiConsent(out, c, fmt.Sprintf("os nomes de %d artistas e até 3 títulos de cada", len(cands)), *yes); err != nil {
		return err
	}

	results := map[string][]string{}
	for i := 0; i < len(cands); i += *batchSize {
		end := min(i+*batchSize, len(cands))
		batch := make([]ai.ArtistSample, 0, end-i)
		for _, cd := range cands[i:end] {
			batch = append(batch, ai.ArtistSample{Name: cd.name, Titles: cd.titles})
		}
		fmt.Fprintf(os.Stderr, "\r%s", progressBar("IA", i, len(cands)))
		got, err := ai.ClassifyArtists(ctx, p, batch)
		if err != nil {
			fmt.Fprintln(os.Stderr)
			return fmt.Errorf("lote %d-%d: %w (o que já foi proposto não foi salvo)", i+1, end, err)
		}
		for k, v := range got {
			results[k] = v
		}
	}
	fmt.Fprintf(os.Stderr, "\r%s\n", progressBar("IA", len(cands), len(cands)))

	if len(results) == 0 {
		fmt.Fprintln(out, "A IA não conseguiu classificar nenhum desses artistas com confiança.")
		return nil
	}
	names := make([]string, 0, len(results))
	for k := range results {
		names = append(names, k)
	}
	sort.Strings(names)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ARTISTA\tGÊNEROS PROPOSTOS")
	for _, n := range names {
		fmt.Fprintf(tw, "%s\t%s\n", n, strings.Join(results[n], ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%d de %d artistas classificados (%s).\n", len(results), len(cands), p.Model())
	if *dry {
		fmt.Fprintln(out, "--dry-run: nada foi salvo.")
		return nil
	}
	ok, err := confirmYes(out, fmt.Sprintf("Salvar estas %d classificações? (gêneros que você definir com `genre set` sempre vencem)", len(results)), *yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "Nada foi salvo.")
		return nil
	}
	path, err := aiStorePath()
	if err != nil {
		return err
	}
	st, err := ai.LoadStore(path)
	if err != nil {
		return err
	}
	for n, g := range results {
		st.Put(n, g, p.Model())
	}
	if err := st.Save(path); err != nil {
		return err
	}
	fmt.Fprintf(out, "Salvo em %s. Vale no próximo `plan`/`apply`/`groups`. Remover: likedsorter ai genres clear\n", path)
	return nil
}

func aiRules(ctx context.Context, args []string, out io.Writer) error {
	fl := flag.NewFlagSet("ai rules", flag.ContinueOnError)
	var d dataFlags
	d.register(fl)
	outFile := fl.String("out", "", "arquivo de saída (padrão: <config>/rules.ai.yaml)")
	maxArtists := fl.Int("max-artists", 150, "quantos artistas (mais frequentes) enviar como contexto")
	yes := fl.Bool("yes", false, "não pedir confirmação do envio")
	if err := fl.Parse(args); err != nil {
		return err
	}
	instruction := strings.TrimSpace(strings.Join(fl.Args(), " "))
	if instruction == "" {
		return errors.New("descreva o que você quer, ex.: likedsorter ai rules \"playlists para estudar, treinar e festa\"")
	}
	p, c, err := aiProvider()
	if err != nil {
		return err
	}
	l, err := loadData(ctx, &d, true)
	if err != nil {
		return err
	}

	counts := map[string]*ai.ArtistLine{}
	decades := map[string]int{}
	for _, t := range l.Sync.Tracks {
		decades[grouping.DecadeOf(t)]++
		if len(t.Artists) == 0 {
			continue
		}
		a := t.Artists[0]
		line := counts[a.Name]
		if line == nil {
			line = &ai.ArtistLine{Name: a.Name, Genres: l.Infos[a.ID].Genres}
			counts[a.Name] = line
		}
		line.Tracks++
	}
	lines := make([]ai.ArtistLine, 0, len(counts))
	for _, v := range counts {
		lines = append(lines, *v)
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Tracks != lines[j].Tracks {
			return lines[i].Tracks > lines[j].Tracks
		}
		return lines[i].Name < lines[j].Name
	})
	if len(lines) > *maxArtists {
		lines = lines[:*maxArtists]
	}
	sum := ai.LibrarySummary{TotalTracks: len(l.Sync.Tracks), Decades: decades, Artists: lines}

	if err := aiConsent(out, c, fmt.Sprintf("sua instrução, a contagem por década e os nomes (com gêneros) dos %d artistas mais frequentes", len(lines)), *yes); err != nil {
		return err
	}

	var yamlText, feedback string
	for attempt := 0; attempt < 2; attempt++ {
		yamlText, err = ai.GenerateRules(ctx, p, sum, instruction, feedback)
		if err != nil {
			return err
		}
		if _, verr := grouping.ParseRules([]byte(yamlText), "resposta da IA"); verr != nil {
			feedback = verr.Error()
			continue
		}
		feedback = ""
		break
	}
	if feedback != "" {
		return fmt.Errorf("a IA não devolveu regras válidas (%s); tente reformular o pedido ou outro modelo", feedback)
	}

	path := *outFile
	if path == "" {
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, "rules.ai.yaml")
	}
	if err := fsutil.WriteFileAtomic(path, []byte("# Gerado por IA ("+p.Model()+") para: "+instruction+"\n# REVISE antes de usar.\n"+yamlText)); err != nil {
		return err
	}
	fmt.Fprintln(out, yamlText)
	fmt.Fprintf(out, "Regras salvas em %s (o seu rules.yaml não foi alterado).\nRevise e simule com:\n  likedsorter plan --by=rules --rules %q\nDepois aplique com: likedsorter apply --by=rules --rules %q\n", path, path, path)
	return nil
}

func aiGenres(args []string, out io.Writer) error {
	path, err := aiStorePath()
	if err != nil {
		return err
	}
	st, err := ai.LoadStore(path)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("uso: likedsorter ai genres list | clear")
	}
	switch args[0] {
	case "list":
		if len(st.Artists) == 0 {
			fmt.Fprintln(out, "Nenhuma classificação da IA salva.")
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ARTISTA\tGÊNEROS\tMODELO")
		for _, n := range st.Names() {
			e := st.Artists[n]
			fmt.Fprintf(tw, "%s\t%s\t%s\n", n, strings.Join(e.Genres, ", "), e.Model)
		}
		return tw.Flush()
	case "clear":
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintln(out, "Classificações da IA apagadas.")
		return nil
	}
	return fmt.Errorf("subcomando ai genres desconhecido %q", args[0])
}
