// Comando likedsorter: organiza as músicas curtidas do Spotify em playlists.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/auth"
	"github.com/ikauedeveloper/likedsorter/internal/config"
)

// version é sobrescrita no build via -ldflags "-X main.version=...".
var version = "dev"

const spotifyAPI = "https://api.spotify.com/v1"

const usage = `likedsorter: organiza suas músicas curtidas do Spotify em playlists

Primeira vez:  install  →  setup  →  auth login        (veja "likedsorter help comecar")
Fluxo típico:  auth login  →  sync  →  plan  →  apply

Ajuda detalhada: likedsorter help <tópico>
  instalar · segredos · comecar · comandos · playlists · estrategias · regras · automatico · config · arquivos · seguranca · problemas · tudo

Dois modos: comandos diretos (abaixo) ou o menu interativo (rode "likedsorter"
sem argumentos num terminal, ou "likedsorter menu").

Comandos:
  menu                            terminal interativo com menus e confirmações
  start                           assistente passo a passo: instalar, credenciais, login e 1ª sincronização
  install [--dir D] [--uninstall] copia este executável para o PC e adiciona ao PATH
  setup [--client-id ID]          grava as credenciais do Spotify (.env) no diretório de config
  doctor [--offline] [--json]     diagnostica instalação, credenciais, login, porta e rede
  update [--check]                atualiza para a última release (confere SHA-256)
  auth login|status|logout        autenticação (OAuth + PKCE)
  sync                            lê curtidas (incremental, com cache) e gêneros dos artistas
  stats                           estatísticas da biblioteca
  groups --by=macro-genre         mostra como as curtidas ficariam agrupadas
  plan   --by=macro-genre         dry-run: o que seria criado/atualizado (tabela, json, csv, md)
  apply  --by=macro-genre         CRIA/ATUALIZA playlists (pede confirmação; nada é apagado)
  dedupe [--to-playlist NOME]     lista curtidas duplicadas por ISRC; opcionalmente copia as antigas p/ uma playlist
  playlist list|add|search        adiciona músicas (link, ID ou busca) às SUAS playlists
  genre list|set|unset|missing    gêneros personalizados por artista
  rules init|path|check           suas regras para --by=rules
  cache path|info|clear           ver ou limpar o cache de curtidas/artistas
  ai setup|test|classify|rules    IA (Claude, OpenAI, Ollama) propõe gêneros e regras; você revisa
  top tracks|artists              o que você mais ouve (Spotify); --to-playlist cria a playlist
  history sync|stats|import       histórico local de reproduções (base do --by=listening)
  suggest [--emit-rules]          sugere novos grupos a partir do que sobrou em "Outros"
  auto                            sync + apply sem perguntar e SEM remover nada (para agendar)
  schedule install|status|remove  agenda o auto (Agendador de Tarefas / cron)
  completion powershell|bash|zsh  autocompletar comandos
  export [--liked]                salva snapshot JSON das playlists gerenciadas (ou das curtidas)
  restore --file snapshot.json    restaura playlists gerenciadas a partir de um snapshot
  config show|get|set|unset|path|init   configuração (config.yaml)
  version
  help [tópico]                   esta ajuda ou um tópico (instalar, segredos, comandos...)

Flags globais (em qualquer posição):
  --config ARQUIVO      config.yaml alternativo
  --log-level LEVEL     debug | info | warn | error   (padrão: warn)
  -v, --verbose         equivale a --log-level debug

Estratégias (--by): artist, genre, macro-genre, decade, year, added-period, language (experimental),
ou combinações com +, ex.: macro-genre+decade.

Flags comuns de sync/groups/plan/apply/stats:
  --full-sync --refresh --no-cache --tracks-ttl 168h --no-enrich --genre-source spotify,musicbrainz,lastfm
  --artists-ttl 720h --include-featured --market from_token --concurrency 4

Agrupamento (groups/plan/apply):
  --by --min-size N --small-groups other|skip --max-size N --multi-genre
  --sort added|release|title --macro-map arquivo.yaml
  --filter-artist a,b --filter-genre rock,pop --since 2023-01-01

plan:   --format table|json|csv|md --out ARQUIVO --name-template "Curtidas • {group}" --skip-existing --detail
apply:  --mode create-only|sync|recreate --public|--private --allow-remove --yes --max-playlists 100

Padrões podem ser definidos em config.yaml e variáveis LIKEDSORTER_*; flags sempre vencem
(veja "likedsorter config show"). Detalhes no README.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]
	// Sem argumentos num terminal interativo: abre o menu. Em pipe/script, mostra a ajuda.
	if len(args) == 0 && stdinIsTerminal() {
		args = []string{"menu"}
		if credentialsMissing() { // primeira vez: o assistente guia o caminho
			args = []string{"start"}
		}
	}
	if err := run(ctx, args, os.Stdout); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "cancelado")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

// semConfig lista comandos que funcionam mesmo com um config.yaml inválido
// (para poder diagnosticá-lo com `config show`).
var semConfig = []string{"version", "--version", "help", "-h", "--help", "config", "auth", "cache", "history", "ai", "install", "setup", "start", "doctor", "completion", "update", "schedule", "rules"}

func run(ctx context.Context, args []string, out io.Writer) error {
	args, g, err := splitGlobals(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return nil
	}
	cmd := args[0]

	s, _, serr := config.LoadSettings(g.configPath)
	if serr != nil && !slices.Contains(semConfig, cmd) {
		return serr
	}
	if serr == nil {
		cur = s
	}
	if err := setupLogging(g, os.Stderr); err != nil {
		return err
	}
	appLogger.Debug("iniciando", "comando", cmd, "versão", version)

	switch cmd {
	case "version", "--version":
		if hasJSON(args[1:]) {
			return writeJSON(out, map[string]string{"version": version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH})
		}
		fmt.Fprintln(out, "likedsorter", version)
		return nil
	case "menu", "interactive":
		return runMenu(ctx, os.Stdin, out, g)
	case "start", "wizard":
		return runStart(ctx, os.Stdin, out, g)
	case "doctor":
		return runDoctor(ctx, args[1:], out)
	case "playlist":
		return runPlaylist(ctx, args[1:], out)
	case "ai":
		return runAI(ctx, args[1:], out)
	case "history":
		return runHistory(ctx, args[1:], out)
	case "top":
		return runTop(ctx, args[1:], out)
	case "cache":
		return runCache(args[1:], out)
	case "suggest":
		return runSuggest(ctx, args[1:], out)
	case "genre":
		return runGenre(ctx, args[1:], out)
	case "rules":
		return runRules(args[1:], out)
	case "auto":
		return runAuto(ctx, args[1:], out)
	case "schedule":
		return runSchedule(ctx, args[1:], out)
	case "update":
		return runUpdate(ctx, args[1:], out)
	case "completion":
		return runCompletion(args[1:], out)
	case "install":
		return runInstall(args[1:], out)
	case "setup":
		return runSetup(args[1:], os.Stdin, out)
	case "auth":
		return runAuth(ctx, args[1:], out)
	case "sync":
		return runSync(ctx, args[1:], out)
	case "stats":
		return runStats(ctx, args[1:], out)
	case "groups":
		return runGroups(ctx, args[1:], out)
	case "plan":
		return runPlan(ctx, args[1:], out)
	case "apply":
		return runApply(ctx, args[1:], out)
	case "dedupe":
		return runDedupe(ctx, args[1:], out)
	case "export":
		return runExport(ctx, args[1:], out)
	case "restore":
		return runRestore(ctx, args[1:], out)
	case "config":
		return runConfig(g, args[1:], out)
	case "help", "-h", "--help":
		return runHelp(args[1:], out)
	default:
		return fmt.Errorf("comando desconhecido %q\n\n%s", cmd, usage)
	}
}

func tokenStore() (*auth.Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return auth.NewStore(filepath.Join(dir, "token.json")), nil
}

func runAuth(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: likedsorter auth login|status|logout")
	}
	store, err := tokenStore()
	if err != nil {
		return err
	}

	switch args[0] {
	case "login":
		fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
		noBrowser := fs.Bool("no-browser", false, "não abrir o navegador; apenas imprimir o link")
		timeout := fs.Duration("timeout", 5*time.Minute, "tempo máximo aguardando a autorização")
		noListening := fs.Bool("no-listening", false, "não pedir acesso ao histórico de reprodução (top e tocadas recentemente)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		opt := auth.LoginOptions{Timeout: *timeout, Out: out}
		if !*noBrowser {
			opt.Open = auth.OpenBrowser
		}
		oc := auth.OAuthConfig(cfg.ClientID, cfg.RedirectURI)
		if *noListening {
			oc.Scopes = auth.Scopes
		}
		tok, err := auth.Login(ctx, oc, store, opt)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "\nAutenticado. Token salvo em %s (expira em %s)\n",
			store.Path, time.Until(tok.Expiry).Round(time.Second))
		return nil

	case "status":
		fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
		offline := fs.Bool("offline", false, "não consultar a API (só mostra o estado local)")
		asJSON := fs.Bool("json", false, "saída em JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return authStatus(ctx, store, *offline, *asJSON, out)

	case "logout":
		if err := store.Delete(); err != nil {
			return err
		}
		fmt.Fprintln(out, "Token local removido. (O Spotify não tem endpoint de revogação; para revogar o acesso do app, use https://www.spotify.com/account/apps/)")
		return nil

	default:
		return fmt.Errorf("subcomando auth desconhecido %q", args[0])
	}
}

func authStatus(ctx context.Context, store *auth.Store, offline, asJSON bool, out io.Writer) error {
	sv, err := store.Load()
	if err != nil {
		return err
	}
	info := map[string]any{"listening_access": len(auth.MissingListening(sv.Scope)) == 0, "token_file": store.Path, "refresh_token": sv.Token.RefreshToken != "", "scopes": strings.Fields(sv.Scope)}
	text := &strings.Builder{}
	fmt.Fprintf(text, "Arquivo do token: %s\n", store.Path)
	switch {
	case sv.Token.Expiry.IsZero():
		fmt.Fprintln(text, "Access token: sem data de expiração")
	case time.Now().After(sv.Token.Expiry):
		info["expires_at"], info["expired"] = sv.Token.Expiry.Format(time.RFC3339), true
		fmt.Fprintf(text, "Access token: expirado em %s (será renovado automaticamente)\n", sv.Token.Expiry.Local().Format(time.RFC3339))
	default:
		info["expires_at"], info["expired"] = sv.Token.Expiry.Format(time.RFC3339), false
		fmt.Fprintf(text, "Access token: válido por mais %s\n", time.Until(sv.Token.Expiry).Round(time.Second))
	}
	fmt.Fprintf(text, "Refresh token: %s\n", yesNo(sv.Token.RefreshToken != ""))
	fmt.Fprintf(text, "Escopos: %s\n", sv.Scope)
	fmt.Fprintf(text, "Histórico de reprodução (top/tocadas recentemente): %s\n", yesNo(len(auth.MissingListening(sv.Scope)) == 0))
	miss := auth.MissingScopes(sv.Scope)
	if miss == nil {
		miss = []string{}
	}
	info["missing_scopes"] = miss
	if len(miss) > 0 {
		fmt.Fprintf(text, "ATENÇÃO: faltam escopos (%s). Rode `auth login` novamente.\n", strings.Join(miss, ", "))
	}
	if !offline {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		ts, err := auth.TokenSource(ctx, auth.OAuthConfig(cfg.ClientID, cfg.RedirectURI), store)
		if err != nil {
			return err
		}
		p, err := auth.Whoami(ctx, ts, spotifyAPI)
		if err != nil {
			return err
		}
		name := p.DisplayName
		if name == "" {
			name = p.ID
		}
		info["user_id"], info["display_name"] = p.ID, name
		fmt.Fprintf(text, "Usuário: %s (%s)\n", name, p.ID)
	}
	if asJSON {
		return writeJSON(out, info)
	}
	_, err = io.WriteString(out, text.String())
	return err
}

func yesNo(b bool) string {
	if b {
		return "sim"
	}
	return "não"
}
