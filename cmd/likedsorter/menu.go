package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ikauedeveloper/likedsorter/internal/config"
)

// menu é o terminal interativo: coleta as opções e delega aos mesmos comandos
// diretos (run), mostrando o comando equivalente para quem quiser repetir depois.
type menu struct {
	ctx   context.Context
	in    *bufio.Reader
	out   io.Writer
	g     globals
	color bool
}

// stdinIsTerminal informa se a entrada padrão é um terminal (não pipe/arquivo).
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func newMenu(ctx context.Context, in io.Reader, out io.Writer, g globals) *menu {
	_, noColor := os.LookupEnv("NO_COLOR")
	// O Windows Terminal e terminais Unix entendem ANSI; o conhost antigo não.
	_, wt := os.LookupEnv("WT_SESSION")
	term := os.Getenv("TERM") != ""
	return &menu{ctx: ctx, in: bufio.NewReader(in), out: out, g: g, color: !noColor && (wt || term)}
}

func (m *menu) paint(code, s string) string {
	if !m.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// readLine lê uma linha; devolve io.EOF quando a entrada acabar.
func (m *menu) readLine() (string, error) {
	s, err := m.in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// ask pergunta um texto com valor padrão (Enter aceita o padrão).
func (m *menu) ask(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(m.out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(m.out, "%s: ", label)
	}
	s, err := m.readLine()
	if err != nil {
		return "", err
	}
	if s == "" {
		return def, nil
	}
	return s, nil
}

// confirm pergunta sim/não; o padrão vale ao apertar Enter.
func (m *menu) confirm(label string, def bool) (bool, error) {
	hint := "s/N"
	if def {
		hint = "S/n"
	}
	for {
		fmt.Fprintf(m.out, "%s (%s): ", label, hint)
		s, err := m.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "":
			return def, nil
		case "s", "sim", "y", "yes":
			return true, nil
		case "n", "não", "nao", "no":
			return false, nil
		}
		fmt.Fprintln(m.out, "Responda s ou n.")
	}
}

type option struct{ key, label, desc string }

// choose mostra uma lista numerada e devolve a chave escolhida.
func (m *menu) choose(title string, opts []option, def string) (string, error) {
	fmt.Fprintln(m.out, title)
	for i, o := range opts {
		fmt.Fprintf(m.out, "  %d) %-14s %s\n", i+1, o.label, m.paint("2", o.desc))
	}
	for {
		s, err := m.ask("Escolha", def)
		if err != nil {
			return "", err
		}
		if n, e := strconv.Atoi(s); e == nil && n >= 1 && n <= len(opts) {
			return opts[n-1].key, nil
		}
		for _, o := range opts {
			if strings.EqualFold(s, o.key) {
				return o.key, nil
			}
		}
		fmt.Fprintln(m.out, "Opção inválida.")
	}
}

var strategyOptions = []option{
	{"macro-genre", "Macro-gênero", "Rock, Hip Hop, Pop, Funk... (recomendado)"},
	{"genre", "Gênero", "gênero exato do artista"},
	{"artist", "Artista", "uma playlist por artista (use tamanho mínimo)"},
	{"decade", "Década", "Anos 90, Anos 2000..."},
	{"year", "Ano", "ano de lançamento"},
	{"added-period", "Mês curtido", "quando você curtiu"},
	{"macro-genre+decade", "Gênero + década", "ex.: Rock · Anos 90"},
}

// groupingArgs pergunta estratégia, tamanho mínimo e filtros comuns a groups/plan/apply.
func (m *menu) groupingArgs() ([]string, error) {
	by, err := m.choose("Como agrupar as curtidas?", strategyOptions, "1")
	if err != nil {
		return nil, err
	}
	args := []string{"--by=" + by}
	min, err := m.ask("Tamanho mínimo da playlist (menores vão para 'Outros')", "10")
	if err != nil {
		return nil, err
	}
	if n, e := strconv.Atoi(min); e != nil || n < 0 {
		fmt.Fprintln(m.out, "Valor inválido; usando 10.")
		min = "10"
	}
	args = append(args, "--min-size="+min)
	if ok, err := m.confirm("Aplicar filtros (artista, gênero, data)?", false); err != nil {
		return nil, err
	} else if ok {
		for _, f := range [][2]string{
			{"filter-artist", "Artistas (trechos separados por vírgula)"},
			{"filter-genre", "Gêneros (trechos separados por vírgula)"},
			{"since", "Curtidas desde (AAAA-MM-DD)"},
		} {
			v, err := m.ask(f[1]+" — Enter para ignorar", "")
			if err != nil {
				return nil, err
			}
			if v != "" {
				args = append(args, "--"+f[0]+"="+v)
			}
		}
	}
	return args, nil
}

// exec roda um comando direto, mostrando o equivalente. Erros não encerram o menu.
func (m *menu) exec(args ...string) bool {
	line := "likedsorter " + joinArgs(args)
	fmt.Fprintf(m.out, "\n%s %s\n\n", m.paint("36", "▶"), m.paint("2", line))
	full := args
	if m.g.configPath != "" {
		full = append([]string{"--config", m.g.configPath}, full...)
	}
	if m.g.verbose {
		full = append(full, "-v")
	}
	err := run(m.ctx, full, m.out)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(m.out, "\ncancelado")
		} else {
			fmt.Fprintln(m.out, m.paint("31", "\nerro: "+err.Error()))
		}
		return false
	}
	return true
}

func (m *menu) pause() {
	fmt.Fprint(m.out, "\nEnter para voltar ao menu...")
	_, _ = m.readLine()
}

func (m *menu) header() {
	fmt.Fprintln(m.out)
	fmt.Fprintln(m.out, m.paint("1;38;5;75", "━━ menu ━━"), m.paint("2", "Fluxo: 1 Login → 3 Sincronizar → 5 Plano → 6 Aplicar"))
	fmt.Fprintln(m.out)
	items := []string{
		"1) Conta: status / login / logout",
		"2) Estatísticas da biblioteca",
		"3) Sincronizar curtidas e gêneros",
		"4) Ver grupos (como ficaria)",
		"5) Plano (dry-run, não altera nada)",
		"6) Aplicar: criar/atualizar playlists",
		"7) Duplicadas (mesma gravação)",
		"8) Backup / restaurar",
		"9) Configurações e ferramentas (credenciais, preferências, cache, agendamento...)",
		"a) Adicionar músicas a uma playlist",
		"s) Credenciais do Spotify (setup)",
		"w) Assistente de configuração",
		"d) Diagnóstico (doctor)",
		"u) Atualizar o programa",
		"i) Instalar este programa no PC",
		"c) Digitar um comando direto",
		"h) Ajuda    0) Sair",
	}
	for _, it := range items {
		fmt.Fprintln(m.out, "  "+it)
	}
}

// loop é o laço principal; termina com 0/sair ou fim da entrada (Ctrl+D/Ctrl+Z).
func (m *menu) loop() error {
	fmt.Fprint(m.out, banner(m.color))
	for {
		if m.ctx.Err() != nil {
			return nil //nolint:nilerr // erro já mostrado ao usuário; segue normalmente
		}
		m.header()
		fmt.Fprint(m.out, "\n> ")
		s, err := m.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(m.out)
				return nil
			}
			return err
		}
		done, err := m.dispatch(strings.ToLower(s))
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if done {
			fmt.Fprintln(m.out, "Até logo!")
			return nil
		}
	}
}

func (m *menu) dispatch(choice string) (bool, error) {
	var err error
	switch choice {
	case "0", "q", "sair", "exit":
		return true, nil
	case "":
		return false, nil
	case "1":
		err = m.account()
	case "2":
		m.exec("stats")
		m.pause()
	case "3":
		err = m.sync()
	case "4":
		var a []string
		if a, err = m.groupingArgs(); err == nil {
			m.exec(append([]string{"groups"}, a...)...)
			m.pause()
		}
	case "5":
		err = m.plan()
	case "6":
		err = m.apply()
	case "7":
		m.exec("dedupe")
		m.pause()
	case "8":
		err = m.backup()
	case "9":
		err = m.settings()
	case "a":
		err = m.addSongs()
	case "w":
		err = m.wizard()
		m.pause()
	case "d":
		m.exec("doctor")
		m.pause()
	case "u":
		m.exec("update")
		m.pause()
	case "s":
		err = m.setup()
	case "i":
		m.exec("install")
		m.pause()
	case "c":
		err = m.free()
	case "h", "?", "ajuda":
		fmt.Fprint(m.out, "\n"+usage+"\n"+helpIndex())
		m.pause()
	default:
		fmt.Fprintln(m.out, "Opção inválida.")
	}
	return false, err
}

func (m *menu) account() error {
	k, err := m.choose("Conta", []option{
		{"status", "Status", "mostra token e usuário"},
		{"login", "Login", "abre o navegador para autorizar"},
		{"logout", "Logout", "apaga o token local"},
	}, "1")
	if err != nil {
		return err
	}
	m.exec("auth", k)
	m.pause()
	return nil
}

// setup reaproveita o leitor do menu para não disputar o stdin.
func (m *menu) setup() error {
	path, err := config.EnvFilePath()
	if err != nil {
		fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
		return nil //nolint:nilerr // erro já mostrado ao usuário; segue normalmente
	}
	fmt.Fprintln(m.out)
	if err := interactiveSetup(path, m.ask, m.out); err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}
		fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
	}
	m.pause()
	return nil
}

func (m *menu) sync() error {
	k, err := m.choose("Sincronizar", []option{
		{"normal", "Normal", "incremental, usa o cache"},
		{"full", "Completo", "relê todas as curtidas (--full-sync)"},
		{"noenrich", "Só curtidas", "sem buscar gêneros (--no-enrich)"},
		{"external", "Sem Spotify", "gêneros só via MusicBrainz + Last.fm (evita rate limit)"},
	}, "1")
	if err != nil {
		return err
	}
	args := []string{"sync"}
	switch k {
	case "full":
		args = append(args, "--full-sync")
	case "noenrich":
		args = append(args, "--no-enrich")
	case "external":
		args = append(args, "--genre-source=musicbrainz,lastfm", "--concurrency=2")
	}
	m.exec(args...)
	m.pause()
	return nil
}

func (m *menu) plan() error {
	a, err := m.groupingArgs()
	if err != nil {
		return err
	}
	m.exec(append([]string{"plan"}, a...)...)
	m.pause()
	return nil
}

func (m *menu) apply() error {
	a, err := m.groupingArgs()
	if err != nil {
		return err
	}
	mode, err := m.choose("Modo", []option{
		{"sync", "sync", "cria o que falta e adiciona faixas novas (padrão)"},
		{"create-only", "create-only", "só cria playlists novas"},
	}, "1")
	if err != nil {
		return err
	}
	remove, err := m.confirm("Remover das playlists gerenciadas as faixas que saíram do grupo? (faz backup antes)", false)
	if err != nil {
		return err
	}
	public, err := m.confirm("Criar playlists públicas? (não = privadas)", false)
	if err != nil {
		return err
	}
	args := append(a, "--mode="+mode)
	if remove {
		args = append(args, "--allow-remove")
	}
	if public {
		args = append(args, "--public")
	}
	// Mostra o plano primeiro; a confirmação é feita aqui para não disputar o stdin.
	if !m.exec(append([]string{"plan"}, a...)...) {
		m.pause()
		return nil
	}
	ok, err := m.confirm("\nAplicar isto na sua conta do Spotify?", false)
	if err != nil {
		return err
	}
	if ok {
		m.exec(append([]string{"apply"}, append(args, "--yes")...)...)
	} else {
		fmt.Fprintln(m.out, "Nada foi alterado.")
	}
	m.pause()
	return nil
}

func (m *menu) backup() error {
	k, err := m.choose("Backup", []option{
		{"export", "Exportar", "snapshot das playlists gerenciadas"},
		{"liked", "Exportar curtidas", "snapshot das suas curtidas"},
		{"restore", "Restaurar", "volta uma playlist de um snapshot"},
	}, "1")
	if err != nil {
		return err
	}
	switch k {
	case "export", "liked":
		def := "playlists.json"
		args := []string{"export"}
		if k == "liked" {
			def = "curtidas.json"
			args = append(args, "--liked")
		}
		f, err := m.ask("Arquivo de saída", def)
		if err != nil {
			return err
		}
		m.exec(append(args, "--out", f)...)
	case "restore":
		f, err := m.ask("Caminho do snapshot.json", "")
		if err != nil {
			return err
		}
		if f == "" {
			fmt.Fprintln(m.out, "Nenhum arquivo informado.")
		} else {
			m.exec("restore", "--file", f)
		}
	}
	m.pause()
	return nil
}

func (m *menu) free() error {
	s, err := m.ask("Comando (sem 'likedsorter'), ex.: plan --by=year", "")
	if err != nil {
		return err
	}
	args, err := splitLine(s)
	if err != nil {
		fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
		return nil //nolint:nilerr // erro já mostrado ao usuário; segue normalmente
	}
	if len(args) == 0 {
		return nil
	}
	if args[0] == "menu" {
		fmt.Fprintln(m.out, "Você já está no menu.")
		return nil
	}
	m.exec(args...)
	m.pause()
	return nil
}

// splitLine divide uma linha em argumentos, respeitando aspas simples e duplas.
func splitLine(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	has := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, has = r, true
		case r == ' ' || r == '\t':
			if has || cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, errors.New("aspas não fechadas")
	}
	if has || cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args, nil
}

// joinArgs junta argumentos para exibição, com aspas nos que têm espaço.
func joinArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = strconv.Quote(a)
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}

func runMenu(ctx context.Context, in io.Reader, out io.Writer, g globals) error {
	return newMenu(ctx, in, out, g).loop()
}
