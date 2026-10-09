package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ikauedeveloper/likedsorter/internal/config"
)

type askFunc func(label, def string) (string, error)

// runSetup grava as credenciais no .env do diretório de config.
// Sem flags e num terminal, pergunta cada valor; com flags, é não interativo.
func runSetup(args []string, in io.Reader, out io.Writer) error {
	fl := flag.NewFlagSet("setup", flag.ContinueOnError)
	clientID := fl.String("client-id", "", "Client ID do app no Spotify Dashboard")
	redirect := fl.String("redirect-uri", "", "Redirect URI (padrão: "+config.DefaultRedirectURI+")")
	lastfm := fl.String("lastfm-key", "", "chave da API do Last.fm (opcional)")
	aiProvider := fl.String("ai-provider", "", "provedor de IA: anthropic | openai | ollama")
	aiKey := fl.String("ai-key", "", "chave de API do provedor de IA")
	aiModel := fl.String("ai-model", "", "modelo de IA (padrão depende do provedor)")
	aiBase := fl.String("ai-base-url", "", "URL base da API de IA (ex.: http://localhost:11434/v1 para Ollama)")
	discogs := fl.String("discogs-token", "", "token pessoal do Discogs (opcional)")
	contact := fl.String("contact", "", "e-mail/URL de contato para o MusicBrainz (opcional)")
	show := fl.Bool("show", false, "só mostra o que está configurado (sem revelar chaves)")
	asJSON := fl.Bool("json", false, "com --show: saída em JSON")
	unset := fl.String("unset", "", "remove valores salvos (separe por vírgula): lastfm-key, discogs-token, contact, redirect-uri, ai-provider, ai-key, ai-model, ai-base-url")
	if err := fl.Parse(args); err != nil {
		return err
	}
	path, err := config.EnvFilePath()
	if err != nil {
		return err
	}
	if *show {
		return setupShow(path, out, *asJSON)
	}

	if *unset != "" {
		if err := unsetCredentials(path, *unset, out); err != nil {
			return err
		}
	}
	given := map[string]string{
		config.EnvClientID: *clientID, config.EnvRedirectURI: *redirect,
		config.EnvLastFMKey: *lastfm, config.EnvContact: *contact, config.EnvDiscogs: *discogs,
		config.EnvAIProvider: *aiProvider, config.EnvAIKey: *aiKey, config.EnvAIModel: *aiModel, config.EnvAIBaseURL: *aiBase,
	}
	anyFlag := false
	for k, v := range given {
		if v == "" {
			delete(given, k) // flag não informada: não mexe no valor atual
		} else {
			anyFlag = true
		}
	}
	if anyFlag {
		return saveSetup(path, given, out)
	}
	if *unset != "" {
		return nil
	}
	if !stdinIsTerminal() {
		return fmt.Errorf("sem terminal: informe --client-id (veja `likedsorter help segredos`)")
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
	return interactiveSetup(path, ask, out)
}

func setupShow(path string, out io.Writer, asJSON bool) error {
	cur, err := config.ReadEnvFile(path)
	if err != nil {
		return err
	}
	if asJSON {
		m := func(k string) string {
			v := cur[k]
			switch {
			case v == "":
				return ""
			case len(v) <= 6:
				return "***"
			}
			return v[:4] + "…" + v[len(v)-2:]
		}
		_, statErr := os.Stat(path)
		return writeJSON(out, map[string]any{"file": path, "exists": statErr == nil, "values": map[string]string{
			config.EnvClientID: m(config.EnvClientID), config.EnvRedirectURI: cur[config.EnvRedirectURI],
			config.EnvLastFMKey: m(config.EnvLastFMKey), config.EnvDiscogs: m(config.EnvDiscogs), config.EnvContact: m(config.EnvContact)}})
	}
	fmt.Fprintf(out, "Arquivo: %s\n", path)
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(out, "(ainda não existe; rode `likedsorter setup`)")
		return nil //nolint:nilerr // sem arquivo: avisa e segue
	}
	mask := func(v string) string {
		switch {
		case v == "":
			return "(não definido)"
		case len(v) <= 6:
			return "***"
		}
		return v[:4] + "…" + v[len(v)-2:]
	}
	fmt.Fprintf(out, "SPOTIFY_CLIENT_ID    %s\n", mask(cur[config.EnvClientID]))
	redir := cur[config.EnvRedirectURI]
	if redir == "" {
		redir = config.DefaultRedirectURI + " (padrão)"
	}
	fmt.Fprintf(out, "SPOTIFY_REDIRECT_URI %s\n", redir)
	fmt.Fprintf(out, "LASTFM_API_KEY       %s\n", mask(cur[config.EnvLastFMKey]))
	fmt.Fprintf(out, "DISCOGS_TOKEN        %s\n", mask(cur[config.EnvDiscogs]))
	fmt.Fprintf(out, "AI_PROVIDER          %s\n", orUnset(cur[config.EnvAIProvider]))
	fmt.Fprintf(out, "AI_API_KEY           %s\n", mask(cur[config.EnvAIKey]))
	fmt.Fprintf(out, "AI_MODEL             %s\n", orUnset(cur[config.EnvAIModel]))
	fmt.Fprintf(out, "AI_BASE_URL          %s\n", orUnset(cur[config.EnvAIBaseURL]))
	fmt.Fprintf(out, "LIKEDSORTER_CONTACT  %s\n", mask(cur[config.EnvContact]))
	return nil
}

// interactiveSetup faz as perguntas; compartilhada entre `setup` e o menu.
func interactiveSetup(path string, ask askFunc, out io.Writer) error {
	cur, err := config.ReadEnvFile(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Configuração das credenciais (Enter mantém o valor entre colchetes).")
	fmt.Fprintln(out, "Precisa de um app em https://developer.spotify.com/dashboard (veja `likedsorter help segredos`).")
	fmt.Fprintln(out)

	defID := cur[config.EnvClientID]
	if defID == "COLE_SEU_CLIENT_ID_AQUI" {
		defID = ""
	}
	id, err := ask("Spotify Client ID", defID)
	if err != nil {
		return err
	}
	defRedir := cur[config.EnvRedirectURI]
	if defRedir == "" {
		defRedir = config.DefaultRedirectURI
	}
	redir, err := ask("Redirect URI (igual ao cadastrado no Dashboard)", defRedir)
	if err != nil {
		return err
	}
	lf, err := ask("Last.fm API key, opcional (Enter para pular)", cur[config.EnvLastFMKey])
	if err != nil {
		return err
	}
	dg, err := ask("Discogs token, opcional (Enter para pular)", cur[config.EnvDiscogs])
	if err != nil {
		return err
	}
	ct, err := ask("Contato para o MusicBrainz (e-mail ou URL), opcional", cur[config.EnvContact])
	if err != nil {
		return err
	}
	return saveSetup(path, map[string]string{
		config.EnvClientID: id, config.EnvRedirectURI: redir, config.EnvLastFMKey: lf, config.EnvDiscogs: dg, config.EnvContact: ct,
	}, out)
}

func saveSetup(path string, v map[string]string, out io.Writer) error {
	for k := range v {
		v[k] = strings.TrimSpace(v[k])
	}
	if id, ok := v[config.EnvClientID]; ok && id != "" {
		if !config.LooksLikeClientID(id) {
			fmt.Fprintln(out, "Aviso: o Client ID costuma ter 32 caracteres hexadecimais; confira se copiou certo.")
		}
	} else if _, asked := v[config.EnvClientID]; !asked {
		// só opcionais (ex.: --ai-provider, --lastfm-key): o Client ID não está em jogo
	} else if cur, _ := config.ReadEnvFile(path); cur[config.EnvClientID] == "" || cur[config.EnvClientID] == "COLE_SEU_CLIENT_ID_AQUI" {
		return errors.New("o Client ID é obrigatório (veja `likedsorter help segredos`)")
	} else {
		delete(v, config.EnvClientID) // mantém o existente
	}
	if r := v[config.EnvRedirectURI]; r != "" {
		if err := config.ValidateRedirectURI(r); err != nil {
			return fmt.Errorf("o Redirect URI é inválido: %w", err)
		}
	}
	// Só grava o que foi informado; vazio em chave opcional remove a linha apenas se veio da pergunta.
	for k, val := range v {
		if val == "" && (k == config.EnvClientID || k == config.EnvRedirectURI) {
			delete(v, k)
		}
	}
	if err := config.UpdateEnvFile(path, v); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nCredenciais salvas em %s\n", path)
	fmt.Fprintln(out, "Próximo passo: likedsorter auth login")
	return nil
}

// unsetCredentials remove valores opcionais do .env. O Client ID não pode ser removido (só trocado).
func unsetCredentials(path, list string, out io.Writer) error {
	names := map[string]string{
		"lastfm-key": config.EnvLastFMKey, "discogs-token": config.EnvDiscogs,
		"contact": config.EnvContact, "redirect-uri": config.EnvRedirectURI,
		"ai-provider": config.EnvAIProvider, "ai-key": config.EnvAIKey, "ai-model": config.EnvAIModel, "ai-base-url": config.EnvAIBaseURL,
	}
	vals := map[string]string{}
	for _, n := range strings.Split(list, ",") {
		n = strings.TrimSpace(n)
		env, ok := names[n]
		if !ok {
			return fmt.Errorf("--unset: %q não pode ser removido (use: lastfm-key, discogs-token, contact, redirect-uri, ai-provider, ai-key, ai-model, ai-base-url; o Client ID só pode ser trocado)", n)
		}
		vals[env] = ""
	}
	if err := config.UpdateEnvFile(path, vals); err != nil {
		return err
	}
	for env := range vals {
		_ = os.Unsetenv(env)
	}
	fmt.Fprintf(out, "Removido de %s: %s\n", path, list)
	return nil
}

func orUnset(v string) string {
	if v == "" {
		return "(não definido)"
	}
	return v
}
