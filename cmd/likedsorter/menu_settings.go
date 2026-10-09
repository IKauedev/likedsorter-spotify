package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ikauedeveloper/likedsorter/internal/config"
)

func maskSecret(v string) string {
	switch {
	case v == "":
		return "(não definido)"
	case len(v) <= 6:
		return "***"
	}
	return v[:4] + "…" + v[len(v)-2:]
}

type envField struct {
	env      string
	label    string
	required bool   // não pode ser removido
	plain    bool   // mostrar o valor inteiro (não é segredo)
	hint     string // dica ao perguntar
}

var (
	credFields = []envField{
		{config.EnvClientID, "Spotify Client ID", true, false, "32 caracteres do Dashboard"},
		{config.EnvRedirectURI, "Redirect URI", true, true, "igual ao cadastrado no Dashboard; padrão " + config.DefaultRedirectURI},
	}
	sourceFields = []envField{
		{config.EnvLastFMKey, "Last.fm API key", false, false, "https://www.last.fm/api/account/create"},
		{config.EnvDiscogs, "Discogs token", false, false, "https://www.discogs.com/settings/developers"},
		{config.EnvContact, "Contato (MusicBrainz)", false, true, "seu e-mail ou URL, enviado no User-Agent"},
	}
	aiFields = []envField{
		{config.EnvAIProvider, "Provedor de IA", false, true, "anthropic, openai ou ollama (local, sem enviar nada para fora)"},
		{config.EnvAIKey, "Chave de API da IA", false, false, "não é necessária no ollama"},
		{config.EnvAIModel, "Modelo de IA", false, true, "vazio = padrão do provedor"},
		{config.EnvAIBaseURL, "URL base da IA", false, true, "vazio = padrão; ollama: http://localhost:11434/v1"},
	}
)

// settings é o submenu "Configurações e ferramentas".
func (m *menu) settings() error {
	for {
		fmt.Fprintln(m.out)
		k, err := m.choose(m.paint("1", "Configurações e ferramentas"), []option{
			{"cred", "Credenciais", "trocar Client ID e Redirect URI"},
			{"sources", "Fontes de gênero", "Last.fm, Discogs, contato do MusicBrainz"},
			{"ai", "Inteligência artificial", "provedor, chave e modelo (ai classify / ai rules)"},
			{"prefs", "Preferências", "padrões: estratégia, tamanho mínimo, divisão, visibilidade..."},
			{"show", "Ver tudo", "configuração efetiva e credenciais salvas"},
			{"genres", "Gêneros personalizados", "definir o gênero de artistas"},
			{"rules", "Regras próprias", "modelo e validação do rules.yaml"},
			{"schedule", "Agendamento", "atualizar as playlists automaticamente"},
			{"cache", "Cache", "ver tamanho / limpar"},
			{"account", "Conta", "status, trocar de conta, sair"},
			{"back", "Voltar", ""},
		}, "back")
		if err != nil {
			return err
		}
		switch k {
		case "back":
			return nil
		case "cred":
			err = m.editEnv("Credenciais do Spotify", credFields)
		case "sources":
			err = m.editEnv("Fontes de gênero", sourceFields)
		case "ai":
			err = m.editEnv("Inteligência artificial", aiFields)
		case "prefs":
			err = m.editPrefs()
		case "show":
			m.exec("setup", "--show")
			m.exec("config", "show")
			m.pause()
		case "genres":
			err = m.genresMenu()
		case "rules":
			err = m.rulesMenu()
		case "schedule":
			err = m.scheduleMenu()
		case "cache":
			err = m.cacheMenu()
		case "account":
			err = m.accountMenu()
		}
		if err != nil {
			return err
		}
	}
}

// editEnv deixa trocar (ou remover) valores do .env, um por vez.
func (m *menu) editEnv(title string, fields []envField) error {
	path, err := config.EnvFilePath()
	if err != nil {
		fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
		return nil //nolint:nilerr // erro já mostrado ao usuário; segue normalmente
	}
	for {
		cur, _ := config.ReadEnvFile(path)
		opts := make([]option, 0, len(fields)+1)
		for _, f := range fields {
			v := cur[f.env]
			shown := maskSecret(v)
			if f.plain && v != "" {
				shown = v
			}
			opts = append(opts, option{f.env, f.label, shown})
		}
		opts = append(opts, option{"back", "Voltar", ""})
		fmt.Fprintln(m.out)
		key, err := m.choose(title+": qual alterar?", opts, "back")
		if err != nil {
			return err
		}
		if key == "back" {
			return nil
		}
		var f envField
		for _, x := range fields {
			if x.env == key {
				f = x
			}
		}
		prompt := fmt.Sprintf("Novo valor de %s (%s)", f.label, f.hint)
		if !f.required {
			prompt += "; '-' remove"
		}
		val, err := m.ask(prompt+" — Enter cancela", "")
		if err != nil {
			return err
		}
		if val == "" {
			fmt.Fprintln(m.out, "Nada foi alterado.")
			continue
		}
		if val == "-" {
			if f.required {
				fmt.Fprintln(m.out, m.paint("31", "Este valor é obrigatório e só pode ser trocado."))
				continue
			}
			val = ""
		}
		before := cur[f.env]
		if err := saveSetup(path, map[string]string{f.env: val}, m.out); err != nil {
			fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
			continue
		}
		if val == "" {
			_ = os.Unsetenv(f.env)
		} else {
			_ = os.Setenv(f.env, val)
		}
		if f.env == config.EnvClientID && before != "" && before != val {
			if err := m.relogin(); err != nil {
				return err
			}
		}
	}
}

// relogin: o token pertence ao Client ID antigo; precisa autorizar de novo.
func (m *menu) relogin() error {
	fmt.Fprintln(m.out, "O token salvo pertence ao Client ID anterior; é preciso autorizar de novo.")
	ok, err := m.confirm("Fazer logout e login agora?", true)
	if err != nil || !ok {
		return err
	}
	m.exec("auth", "logout")
	m.exec("auth", "login")
	return nil
}

// editPrefs edita os padrões do config.yaml (config.SetSetting valida cada valor).
func (m *menu) editPrefs() error {
	for {
		path := m.g.configPath
		if path == "" {
			p, err := config.SettingsPath()
			if err != nil {
				fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
				return nil //nolint:nilerr // erro já mostrado ao usuário; segue normalmente
			}
			path = p
		}
		_, entries, err := config.LoadSettings(m.g.configPath)
		if err != nil {
			fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
			return nil //nolint:nilerr // erro já mostrado ao usuário; segue normalmente
		}
		fmt.Fprintln(m.out)
		tw := tabwriter.NewWriter(m.out, 0, 0, 2, ' ', 0)
		for i, e := range entries {
			v := e.Value
			if v == "" {
				v = `""`
			}
			fmt.Fprintf(tw, "  %2d)\t%s\t%s\t%s\n", i+1, e.Key, v, m.paint("2", "["+e.Source+"] "+e.Help))
		}
		_ = tw.Flush()
		s, err := m.ask("Número da preferência a alterar (Enter volta)", "")
		if err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		n, convErr := strconv.Atoi(s)
		if convErr != nil || n < 1 || n > len(entries) {
			fmt.Fprintln(m.out, "Opção inválida.")
			continue
		}
		e := entries[n-1]
		val, err := m.ask(fmt.Sprintf("Novo valor de %s (atual %s; '-' volta ao padrão %s)", e.Key, e.Value, config.DefaultValue(e.Key)), "")
		if err != nil {
			return err
		}
		switch strings.TrimSpace(val) {
		case "":
			fmt.Fprintln(m.out, "Nada foi alterado.")
		case "-":
			was, err := config.UnsetSetting(path, e.Key)
			if err != nil {
				fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
			} else if was {
				fmt.Fprintf(m.out, "%s voltou ao padrão.\n", e.Key)
			} else {
				fmt.Fprintln(m.out, "Já estava no padrão.")
			}
		default:
			if err := config.SetSetting(path, e.Key, val); err != nil {
				fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
			} else {
				fmt.Fprintf(m.out, "%s = %s gravado.\n", e.Key, strings.TrimSpace(val))
			}
		}
	}
}

func (m *menu) genresMenu() error {
	k, err := m.choose("Gêneros personalizados", []option{
		{"missing", "Sem gênero", "artistas que ficaram sem gênero (mais faixas primeiro)"},
		{"set", "Definir", "gênero de um artista (vence as fontes externas)"},
		{"list", "Listar", "os que você já definiu"},
		{"unset", "Remover", "voltar um artista ao automático"},
	}, "1")
	if err != nil {
		return err
	}
	switch k {
	case "missing", "list":
		m.exec("genre", k)
	case "set", "unset":
		artist, err := m.ask("Nome do artista", "")
		if err != nil || artist == "" {
			return err
		}
		if k == "unset" {
			m.exec("genre", "unset", artist)
			break
		}
		g, err := m.ask("Gênero(s), separados por vírgula (ex.: rock,pop)", "")
		if err != nil || g == "" {
			return err
		}
		m.exec("genre", "set", artist, g)
	}
	m.pause()
	return nil
}

func (m *menu) rulesMenu() error {
	k, err := m.choose("Regras próprias (--by=rules)", []option{
		{"init", "Criar modelo", "gera rules.yaml de exemplo"},
		{"check", "Validar", "confere o rules.yaml"},
		{"path", "Caminho", "onde fica o arquivo"},
	}, "1")
	if err != nil {
		return err
	}
	m.exec("rules", k)
	m.pause()
	return nil
}

func (m *menu) scheduleMenu() error {
	k, err := m.choose("Agendamento (auto: sincroniza e adiciona às playlists, nunca remove)", []option{
		{"status", "Status", "há tarefa agendada?"},
		{"install", "Agendar", "diário, semanal ou de hora em hora"},
		{"remove", "Remover", "apaga a tarefa agendada"},
	}, "1")
	if err != nil {
		return err
	}
	if k != "install" {
		m.exec("schedule", k)
		m.pause()
		return nil
	}
	every, err := m.choose("Frequência", []option{
		{"daily", "Diária", ""}, {"weekly", "Semanal", "domingo"}, {"hourly", "De hora em hora", ""},
	}, "1")
	if err != nil {
		return err
	}
	args := []string{"schedule", "install", "--every", every}
	if every != "hourly" {
		at, err := m.ask("Horário (HH:MM)", "03:00")
		if err != nil {
			return err
		}
		args = append(args, "--at", at)
	}
	m.exec(args...)
	m.pause()
	return nil
}

func (m *menu) cacheMenu() error {
	k, err := m.choose("Cache de curtidas e artistas", []option{
		{"info", "Ver", "arquivos e tamanho"},
		{"clear", "Limpar", "a próxima sincronização relê tudo"},
		{"path", "Caminho", ""},
	}, "1")
	if err != nil {
		return err
	}
	if k == "clear" {
		ok, err := m.confirm("Apagar o cache? (playlists e configurações não são tocadas)", false)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(m.out, "Nada foi alterado.")
			m.pause()
			return nil
		}
		m.exec("cache", "clear", "--yes")
	} else {
		m.exec("cache", k)
	}
	m.pause()
	return nil
}

func (m *menu) accountMenu() error {
	k, err := m.choose("Conta", []option{
		{"status", "Status", "token e usuário logado"},
		{"switch", "Trocar de conta", "logout e novo login"},
		{"logout", "Sair", "apaga o token local"},
		{"login", "Entrar", "autorizar no navegador"},
	}, "1")
	if err != nil {
		return err
	}
	switch k {
	case "switch":
		m.exec("auth", "logout")
		m.exec("auth", "login")
	default:
		m.exec("auth", k)
	}
	m.pause()
	return nil
}
