package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type helpTopic struct {
	names []string // o primeiro é o nome canônico
	title string
	body  string
}

var helpTopics = []helpTopic{
	{[]string{"instalar", "install", "instalacao"}, "Instalar no PC", helpInstall},
	{[]string{"segredos", "credenciais", "setup", "spotify"}, "Credenciais (segredos) do Spotify", helpSecrets},
	{[]string{"comecar", "inicio", "start"}, "Primeiros passos (do zero à primeira playlist)", helpStart},
	{[]string{"comandos", "commands"}, "Todos os comandos e flags", helpCommands},
	{[]string{"playlists", "playlist", "adicionar"}, "Adicionar músicas às suas playlists", helpPlaylists},
	{[]string{"estrategias", "agrupar", "grouping"}, "Estratégias de agrupamento", helpStrategies},
	{[]string{"ia", "ai", "inteligencia"}, "Inteligência artificial (Claude, OpenAI, Ollama)", helpAI},
	{[]string{"frequencia", "ouvidas", "listening", "historico"}, "Playlists por frequência de reprodução", helpListening},
	{[]string{"regras", "rules", "generos"}, "Regras próprias e gêneros personalizados", helpRules},
	{[]string{"automatico", "auto", "agendar", "schedule"}, "Modo automático e agendamento", helpAuto},
	{[]string{"config", "configuracao"}, "Configuração (config.yaml e variáveis)", helpConfig},
	{[]string{"arquivos", "files"}, "Onde ficam os arquivos", helpFiles},
	{[]string{"seguranca", "safety"}, "Garantias de segurança e recuperação", helpSafety},
	{[]string{"problemas", "erros", "troubleshooting"}, "Solução de problemas", helpTroubleshooting},
}

func runHelp(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return nil
	}
	want := strings.ToLower(args[0])
	if want == "topicos" || want == "tópicos" || want == "topics" {
		fmt.Fprint(out, helpIndex())
		return nil
	}
	if want == "tudo" || want == "all" {
		for _, t := range helpTopics {
			fmt.Fprintf(out, "━━ %s ━━\n%s\n", t.title, t.body)
		}
		return nil
	}
	for _, t := range helpTopics {
		for _, n := range t.names {
			if n == want {
				fmt.Fprint(out, t.body)
				return nil
			}
		}
	}
	return fmt.Errorf("tópico %q desconhecido\n\n%s", args[0], helpIndex())
}

func helpIndex() string {
	var b strings.Builder
	b.WriteString("Tópicos de ajuda (likedsorter help <tópico>):\n")
	rows := make([]string, 0, len(helpTopics))
	for _, t := range helpTopics {
		rows = append(rows, fmt.Sprintf("  %-12s %s", t.names[0], t.title))
	}
	sort.Strings(rows)
	b.WriteString(strings.Join(rows, "\n"))
	b.WriteString("\n  tudo         imprime todos os tópicos\n")
	return b.String()
}

const helpStart = `Do zero à primeira playlist (o jeito mais fácil):

  likedsorter start               ASSISTENTE: instala, guia a criação do app no Spotify,
                                  grava o Client ID, faz o login e a 1ª sincronização.
                                  (Rodar só "likedsorter" na primeira vez já abre o assistente.)

Ou, passo a passo manual:

  1. likedsorter install          copia o programa para o PC e coloca no PATH
  2. (feche e abra o terminal)
  3. likedsorter help segredos    como criar o app no Spotify e obter o Client ID
  4. likedsorter setup            grava o Client ID (pergunta no terminal)
  5. likedsorter auth login       autoriza sua conta no navegador
  6. likedsorter sync             lê as curtidas e os gêneros (1ª vez demora)
  7. likedsorter plan             SIMULA: mostra o que seria criado
  8. likedsorter apply            cria/atualiza as playlists (pede confirmação)

Algo errado? "likedsorter doctor" diagnostica instalação, credenciais, login, porta e rede.
Depois da configuração, rode só "likedsorter" para abrir o menu interativo.
`

const helpInstall = `Instalar o likedsorter no PC

O próprio executável se instala: não precisa de instalador, administrador nem Go.

  likedsorter install

O que faz:
  - copia o executável para a pasta do usuário
      Windows:        %LOCALAPPDATA%\Programs\likedsorter\likedsorter.exe
      Linux / macOS:  ~/.local/bin/likedsorter
  - no Windows, adiciona essa pasta ao PATH do usuário (sem administrador)
  - nos demais sistemas, mostra a linha "export PATH=..." para o seu shell

Depois, abra um terminal NOVO e confirme com:  likedsorter version
  - Terminais que já estavam abertos (e os abertos de dentro de outro programa, como o terminal
    do VS Code) mantêm o PATH antigo. O install avisa o Windows da mudança, mas se o comando
    ainda não for reconhecido:
      PowerShell (neste terminal):  $env:Path += ";$env:LOCALAPPDATA\Programs\likedsorter"
      cmd (neste terminal):         set PATH=%PATH%;%LOCALAPPDATA%\Programs\likedsorter
    ou abra o terminal pelo menu Iniciar, reinicie o VS Code, ou faça logoff/login.

Opções:
  --dir PASTA       instalar em outra pasta
  --no-path         não alterar o PATH
  --uninstall       remove o executável instalado e a entrada do PATH

Sem instalar: o programa roda de qualquer pasta (.\likedsorter.exe ...). Instalar só
serve para chamá-lo de qualquer lugar digitando "likedsorter".

Atualizar: baixe/compile a versão nova e rode "likedsorter install" com ela; o
executável antigo é substituído e suas configurações são mantidas. (Se der erro de
arquivo em uso, feche outros terminais que estejam usando o likedsorter.)

Desinstalar completamente:
  likedsorter auth logout            apaga o token local
  likedsorter install --uninstall    remove programa e PATH
  Windows (PowerShell), para apagar também configuração e cache:
    Remove-Item -Recurse "$env:APPDATA\likedsorter", "$env:LOCALAPPDATA\likedsorter"
  Para revogar o acesso no Spotify: https://www.spotify.com/account/apps/

Compilar a partir do código-fonte (exige Go 1.22+):
  go build -trimpath -ldflags "-s -w" -o likedsorter.exe ./cmd/likedsorter
`

//nolint:gosec // G101: texto de ajuda, não é credencial
const helpSecrets = `Credenciais (segredos) do Spotify

O programa precisa do "Client ID" de um app SEU no Spotify. Usa OAuth com PKCE:
não existe Client Secret, e o token de acesso fica só no seu computador.

1. Acesse https://developer.spotify.com/dashboard e clique em "Create app".
2. Em "Redirect URIs" cadastre EXATAMENTE:
       http://127.0.0.1:8888/callback
   ("localhost" não é aceito pelo Spotify; use 127.0.0.1.)
3. Marque "Web API" e salve.
4. Em Settings → User Management, adicione o e-mail da SUA conta Spotify
   (Development Mode permite até 5 usuários; a conta precisa ser Premium).
5. Em Settings, copie o "Client ID" (32 caracteres).
6. Grave no programa:

       likedsorter setup

   Ele pergunta Client ID, Redirect URI e, opcionalmente, chave do Last.fm e contato
   para o MusicBrainz, e salva tudo no arquivo .env do diretório de config
   (permissão restrita ao seu usuário). Sem terminal/para scripts:

       likedsorter setup --client-id SEU_ID
       likedsorter setup --client-id SEU_ID --redirect-uri http://127.0.0.1:9000/cb
       likedsorter setup --lastfm-key CHAVE --discogs-token TOKEN --contact voce@exemplo.com
       likedsorter setup --show          mostra o que está salvo (chaves mascaradas)

7. Autorize a conta:  likedsorter auth login

Onde o .env é procurado (a primeira definição de cada variável vence):
  1) variáveis de ambiente reais   2) .env na pasta atual   3) .env no diretório de config
  Diretório de config: Windows %APPDATA%\likedsorter · Linux ~/.config/likedsorter
                       macOS ~/Library/Application Support/likedsorter
  (mude com LIKEDSORTER_CONFIG_DIR)

Variáveis:
  SPOTIFY_CLIENT_ID      obrigatória
  SPOTIFY_REDIRECT_URI   padrão http://127.0.0.1:8888/callback (idêntica à do Dashboard)
  LASTFM_API_KEY         opcional; fonte extra de gêneros (uso não comercial)
  AI_PROVIDER / AI_API_KEY / AI_MODEL / AI_BASE_URL   opcionais; IA (help ia). Gere com: likedsorter ai setup
  DISCOGS_TOKEN          opcional; fonte de gêneros Discogs (token pessoal, discogs.com/settings/developers)
                         (a fonte "deezer" não precisa de chave)
  LIKEDSORTER_CONTACT    opcional; e-mail/URL enviado ao MusicBrainz no User-Agent

Cuidados: nunca compartilhe nem versione o .env nem o token.json. O token expira e é
renovado sozinho; "auth logout" apaga o token local.
`

const helpCommands = `Comandos

  menu                        menu interativo (é o padrão ao rodar sem argumentos num terminal)
  start                       assistente guiado: instalar, credenciais, login, 1ª sincronização
  install                     copia o programa para o PC e ajusta o PATH      (help instalar)
  setup                       grava as credenciais no .env                    (help segredos)
  doctor  [--offline] [--json]        diagnóstico completo (PATH, Client ID, porta, token, rede, API)
  update  [--check] [--yes]           atualiza para a última release (confere SHA-256)
  auth login|status|logout    autenticação OAuth + PKCE
      login   [--no-browser] [--timeout 5m]
      status  [--offline]     estado do token e usuário logado
      logout                  apaga o token local
  sync                        lê as curtidas (incremental) e os gêneros dos artistas
  stats   [--format table|json]       estatísticas da biblioteca (somente leitura)
  groups                      mostra como as curtidas ficariam agrupadas (somente leitura)
      --dump-macro-map        imprime o mapa de macro-gêneros para editar
      --examples N            exemplos de faixas por grupo
  plan                        dry-run: o que seria criado/atualizado (NÃO altera nada)
      --format table|json|csv|md  --out ARQUIVO  --detail  --skip-existing
  apply                       CRIA/ATUALIZA playlists (pede confirmação; nunca apaga playlists)
      --mode create-only|sync|recreate   --public|--private   --allow-remove
      --yes   --max-playlists 100
  dedupe  [--format table|json] [--out ARQUIVO] [--to-playlist NOME]
                              curtidas duplicadas por ISRC; --to-playlist copia as cópias mais
                              antigas para uma playlist para você revisar (nada é removido)
  playlist list [--json]      suas playlists
  playlist search "texto"     busca faixas
  playlist add --to NOME [--create] [--public] [--pick] [--yes] [--from-file F] MÚSICA...
                              adiciona músicas à SUA playlist             (help playlists)
  ai setup|test|classify|rules        IA propõe gêneros e regras; você revisa (help ia)
  top tracks|artists [--range R] [--to-playlist NOME]   o que você mais ouve (help frequencia)
  history sync|stats|import           histórico local de reproduções       (help frequencia)
  genre list|set|unset|missing        gêneros personalizados por artista  (help regras)
  rules init|path|check       suas regras para --by=rules                 (help regras)
  suggest [--emit-rules] [--top N] [--min-count N]
                              sugere novos grupos a partir do que sobrou em "Outros"
  auto    [flags do apply]    sync + apply sem perguntar, SEM remover nada (help automatico)
  schedule install|status|remove      agenda o auto no Windows/cron       (help automatico)
  completion powershell|bash|zsh      autocompletar com Tab
  export  [--liked] [--out ARQUIVO]   snapshot JSON das playlists gerenciadas (ou das curtidas)
  restore --file snapshot.json        restaura playlists gerenciadas de um snapshot
  config show|get|set|unset|path|init configuração (config.yaml)           (help config)
  cache path|info|clear [--yes]       ver ou limpar o cache de curtidas/artistas
  version                     versão
  help [tópico]               esta ajuda (tópicos: ver "likedsorter help tópicos")

Flags globais (valem em qualquer posição):
  --config ARQUIVO       usa outro config.yaml
  --log-level LEVEL      debug | info | warn | error (padrão warn)
  -v, --verbose          = --log-level debug (mostra cada requisição HTTP, sem tokens)

Flags de dados (sync, stats, groups, plan, apply, dedupe):
  --full-sync            relê todas as curtidas
  --refresh              ignora e reconstrói o cache
  --no-cache             não lê nem grava cache
  --tracks-ttl 168h      validade do cache de faixas
  --artists-ttl 720h     validade do cache de artistas
  --no-enrich            não busca gêneros
  --genre-source LISTA   spotify,musicbrainz,lastfm,deezer,discogs (ordem de prioridade)
  --include-featured     considera artistas de participação
  --market from_token    market enviado ao Spotify
  --concurrency 4        chamadas simultâneas ao Spotify

Flags de agrupamento (groups, plan, apply):
  --by ESTRATÉGIA        veja "help estrategias" (inclui rules: "help regras")
  --rules ARQ.yaml       suas regras (padrão: <config>/rules.yaml)
  --min-size N           grupos menores vão para "Outros" (ou são ignorados)
  --small-groups other|skip
  --max-size N           divide grupos grandes em "X (Parte 1)", "X (Parte 2)"...
  --multi-genre          a faixa pode entrar em até 3 grupos de gênero
  --sort added|release|title
  --split-over N         divide grupos com mais de N faixas por década (ou ano, --split-by year)
  --split-by decade|year
  --listen-top N --listen-days N --forgotten-days N   ajustes do --by=listening (help frequencia)
  --macro-map ARQ.yaml   mapa de macro-gêneros próprio
  --name-template "Curtidas • {group}"

Filtros (groups, plan, apply, stats):
  --filter-artist a,b    --filter-genre rock,pop    --since AAAA-MM-DD

Saída para scripts: --json (em version, auth status, sync, groups, plan, stats, apply, restore,
dedupe, doctor, config show, setup --show, install, update, genre, rules check, suggest,
playlist list/search/add, schedule status). Com --json o texto de progresso vai para stderr e
só o JSON sai em stdout. Cores: automáticas em terminal; NO_COLOR desliga.
Simulação: "apply --dry-run" mostra o plano e sai sem pedir confirmação nem escrever.

Dica: "likedsorter <comando> -h" lista todas as flags do comando.
`

const helpStrategies = `Estratégias de agrupamento (--by)

  artist         artista principal (--include-featured: também participações)
  genre          gênero exato do artista principal (--multi-genre: até 3)
  macro-genre    família: Rock, Hip Hop, Sertanejo, MPB, Pagode/Samba, Funk, Eletrônica,
                 Pop, Gospel, Clássica, Jazz, Outros  (recomendado)
  decade         década de lançamento do álbum (Anos 90, Anos 2000...)
  year           ano de lançamento
  added-period   mês/ano em que você curtiu (UTC)
  language       experimental (muitos ficam "Indefinido")
  listening      frequência de reprodução: Mais ouvidas, Esquecidas, Redescobertas (help frequencia)
  a+b            combinação, ex.: macro-genre+decade → "Rock · Anos 90"

Exemplos:
  likedsorter plan --by=macro-genre --min-size=10
  likedsorter plan --by=artist --min-size=5
  likedsorter plan --by=genre --multi-genre --small-groups=skip --min-size=10
  likedsorter plan --by=macro-genre --split-over=300     divide "Rock" (se >300) em Rock · Anos 90, Anos 2000...
  likedsorter plan --by=macro-genre --split-over=300 --split-by=year

Macro-gêneros editáveis:
  likedsorter groups --dump-macro-map > macro.yaml     (edite)
  likedsorter plan --macro-map macro.yaml
  ou salve como macro_genres.yaml no diretório de config.
`

const helpConfig = `Configuração

Precedência: flags > variáveis de ambiente > config.yaml > padrões embutidos.

  likedsorter config init [--force]   cria o modelo do config.yaml
  likedsorter config show             valor efetivo de cada chave e de onde veio
  likedsorter config get CHAVE        valor efetivo e origem de uma chave
  likedsorter config set CHAVE VALOR  grava no config.yaml (valida antes; ex.: config set group.min_size 10)
  likedsorter config unset CHAVE      volta a chave ao padrão
  likedsorter config path             caminho do arquivo

Credenciais (Client ID, Last.fm, Discogs, contato) ficam no .env e mudam com "setup":
  likedsorter setup                          pergunta tudo (Enter mantém o valor atual)
  likedsorter setup --client-id NOVO_ID      troca o Client ID (depois: auth logout && auth login)
  likedsorter setup --lastfm-key CHAVE       troca/define a chave do Last.fm
  likedsorter setup --unset lastfm-key,discogs-token,contact,redirect-uri   remove valores
  likedsorter setup --show                   o que está salvo (mascarado)
No menu: opção 9 (Configurações e ferramentas) -> Credenciais / Fontes de gênero / Preferências.

Cada chave "seção.nome" tem a variável LIKEDSORTER_SEÇÃO_NOME
(ex.: group.min_size → LIKEDSORTER_GROUP_MIN_SIZE).

Chaves: log_level · sync.market, sync.tracks_ttl, sync.artists_ttl, sync.genre_source,
sync.concurrency, sync.include_featured · group.by, group.min_size, group.small_groups,
group.max_size, group.multi_genre, group.sort, group.macro_map · plan.name_template ·
apply.mode, apply.public, apply.max_playlists.

--allow-remove e --yes NÃO existem na configuração: precisam ser passados a cada execução.
`

const helpFiles = `Onde ficam os arquivos

Diretório de config (LIKEDSORTER_CONFIG_DIR muda):
  Windows %APPDATA%\likedsorter · Linux ~/.config/likedsorter · macOS ~/Library/Application Support/likedsorter
    .env              credenciais (criado por "setup")
    config.yaml       configuração (criado por "config init")
    token.json        token OAuth (modo 0600)
    state.json        andamento do último apply
    backups/<data>/snapshot.json   backup antes de alterar playlists
    macro_genres.yaml mapa de macro-gêneros (opcional)
    rules.yaml / rules.ai.yaml   suas regras / regras geradas por IA
    genre_overrides.yaml         gêneros que você definiu
    ai_genres.json               classificações da IA que você aceitou
    plays.json                   histórico local de reproduções (history sync/import)

Cache (LIKEDSORTER_CACHE_DIR muda):
  Windows %LOCALAPPDATA%\likedsorter · Linux ~/.cache/likedsorter
    tracks.json, artists.json   apague a pasta para limpar tudo

Executável instalado: veja "likedsorter help instalar".
`

const helpSafety = `Garantias de segurança

  - plan, groups, stats, dedupe e export são somente leitura.
  - Só "apply", "restore", "playlist add", "dedupe --to-playlist" e "auto" escrevem na sua conta.
    Todos pedem confirmação (--yes pula), exceto "auto", que só ADICIONA (nunca remove).
  - "playlist add" só acrescenta músicas a playlists suas ou colaborativas; pula as repetidas.
  - Nunca apaga playlists. Faixas só são removidas com --allow-remove.
  - Só mexe em playlists SUAS marcadas com [managed:likedsorter] na descrição.
  - Backup automático antes de alterar playlists existentes; se falhar, nada é alterado.
  - Falhas no meio do apply: rode o mesmo comando de novo (não duplica faixas).
  - Com filtros ativos, apply recusa --allow-remove e --mode=recreate.
  - Logs nunca mostram tokens nem chaves.

Recuperar:
  likedsorter restore --file <config>/backups/<data-hora>/snapshot.json
`

const helpTroubleshooting = `Solução de problemas

  "likedsorter" não é reconhecido      terminal aberto antes do install; feche e abra outro
  SPOTIFY_CLIENT_ID não configurado    rode "likedsorter setup"
  INVALID_CLIENT / redirect URI        o Redirect URI do Dashboard deve ser idêntico ao do .env
  403 do Spotify                       e-mail não está em User Management, ou conta não é Premium
  login não conclui                    porta 8888 ocupada: troque a porta no setup E no Dashboard
  navegador não abre                   auth login --no-browser (imprime o link)
  sync lento na 1ª vez                 normal (1 chamada por artista); o cache retoma após Ctrl+C
  muitos "Sem gênero"                  --genre-source=spotify,musicbrainz,lastfm
  erro ao atualizar o install          feche outros terminais usando o likedsorter e repita
  detalhes de uma execução             adicione -v (ex.: likedsorter sync -v)
  não sei o que está errado            rode "likedsorter doctor"
  busca/playlist add: 403 de escopo    rode "likedsorter auth login" de novo
`

const helpPlaylists = `Adicionar músicas às suas playlists

  likedsorter playlist list                    suas playlists (nome, dona, ID)
  likedsorter playlist search "djavan sina"    até 10 resultados com o URI de cada um
  likedsorter playlist add --to "Minha playlist" MÚSICA...

MÚSICA pode ser:
  - link:   https://open.spotify.com/track/2kgT6sMwXd3mdeXhBbLMQe
  - URI:    spotify:track:2kgT6sMwXd3mdeXhBbLMQe
  - ID:     2kgT6sMwXd3mdeXhBbLMQe
  - texto:  "djavan sina"   (usa o 1º resultado; com --pick você escolhe entre 5)

Exemplos:
  likedsorter playlist add --to "Treino" "racionais capítulo 4" "emicida levanta e anda"
  likedsorter playlist add --to "Estudo" --create --pick "lofi hip hop" spotify:track:2kgT6sMwXd3mdeXhBbLMQe
  likedsorter playlist add --to "Treino" --from-file musicas.txt --yes
      (musicas.txt: uma música por linha; linhas começando com # são ignoradas)

Regras de segurança:
  - O destino é achado pelo nome (sem diferenciar maiúsculas), link ou ID, entre as SUAS
    playlists; as de outras pessoas (não colaborativas) são recusadas.
  - Só acrescenta ao final: nunca remove nem reordena. Músicas que já estão na playlist
    (ou repetidas na sua lista) são puladas, então repetir o comando é seguro.
  - Mostra o que vai entrar e pede confirmação (--yes pula).
  - --create cria a playlist (privada; --public para pública) se o nome não existir.
  - Pelo menu: opção "a".
`

const helpRules = `Regras próprias e gêneros personalizados

REGRAS (--by=rules): você define as playlists em um YAML.
  likedsorter rules init        cria o modelo em <config>/rules.yaml
  likedsorter rules check       valida e lista as regras
  likedsorter plan --by=rules   (ou --rules outro.yaml)

  primeira_regra: true          # true (padrão): só a 1ª regra que casar; false: todas
  regras:
    - nome: MPB raiz
      artista: [djavan, marisa monte]   # trechos do nome do artista (OU)
      genero: [mpb]                     # trechos do gênero do artista principal (OU)
    - nome: Clássicos 70-90
      lancamento: "1970-1999"           # ano ou intervalo do álbum
    - nome: Recentes
      curtida_desde: 2024-01-01         # quando você curtiu
    - nome: Acústicos
      titulo: [acústico, unplugged]     # trechos do título (OU)

  Dentro de uma regra, todas as condições informadas precisam casar (E). O que não casar
  com nenhuma regra vai para "Outros". Combine com outras estratégias: --by=rules+decade.

SUGESTÕES: o que sobrou em "Outros"/"Sem gênero" pode virar playlist.
  likedsorter suggest                 mostra artistas, gêneros e décadas frequentes entre as sobras
  likedsorter suggest --emit-rules    gera regras YAML prontas para colar no rules.yaml

GÊNEROS PERSONALIZADOS: o gênero que VOCÊ define vence Spotify/MusicBrainz/Last.fm.
  likedsorter genre missing                  artistas sem gênero (mais faixas primeiro)
  likedsorter genre set "Djavan" mpb         define (aceita vários: rock,pop)
  likedsorter genre unset "Djavan"           remove
  likedsorter genre list                     mostra os definidos
  Arquivo: <config>/genre_overrides.yaml (pode editar à mão)
`

const helpAuto = `Modo automático e agendamento

  likedsorter auto [flags do apply]

Faz "apply --yes" (que já sincroniza as curtidas novas antes) sem perguntar nada. É pensado
para manter as playlists em dia sozinho. Por segurança:
  - NUNCA remove faixas: --allow-remove e --mode=recreate são recusados no auto;
  - respeita as travas de sempre (só playlists gerenciadas, --max-playlists, backup);
  - --log-file ARQUIVO grava o resultado de cada execução.

Agendar:
  likedsorter schedule install                       todo dia às 03:00
  likedsorter schedule install --every hourly
  likedsorter schedule install --every weekly --at 21:30
  likedsorter schedule install -- --by=macro-genre --min-size=10
  likedsorter schedule status
  likedsorter schedule remove

Windows: cria a tarefa "likedsorter" no Agendador de Tarefas (roda com você logado).
Linux/macOS: mostra a linha para colocar no crontab.
O log fica em <config>/auto.log.
`

const helpAI = `Inteligência artificial (opcional)

A IA ajuda onde o Spotify falha (artistas "Sem gênero") e a montar regras a partir de um pedido em
português. Ela só PROPÕE: o que volta é validado e você revisa antes de qualquer coisa ser salva ou
aplicada. O apply continua com todas as travas (backup, confirmação, nunca apaga playlists).

Configurar (uma vez):
  likedsorter ai setup
  Provedores: anthropic (Claude), openai, ollama (modelo local, nada sai do seu computador).
  Sem terminal:  likedsorter setup --ai-provider anthropic --ai-key CHAVE [--ai-model MODELO]
                 likedsorter setup --ai-provider ollama --ai-model llama3.1
  Também vale ANTHROPIC_API_KEY / OPENAI_API_KEY no ambiente. Padrões de modelo:
  anthropic = claude-haiku-5-5, openai = gpt-4o-mini, ollama = llama3.1.
  likedsorter ai test             confere a conexão

Classificar artistas sem gênero:
  likedsorter ai classify --dry-run      mostra as propostas sem salvar
  likedsorter ai classify                propõe, pede sua confirmação e salva em ai_genres.json
  likedsorter ai genres list|clear       ver / apagar o que a IA salvou
  Só preenche quem está sem gênero. O que você definir com "genre set" sempre vence.

Gerar regras a partir de um pedido:
  likedsorter ai rules "playlists para estudar, treinar e festa"
  Salva em <config>/rules.ai.yaml (seu rules.yaml NÃO é tocado). Depois:
  likedsorter plan  --by=rules --rules <config>/rules.ai.yaml
  likedsorter apply --by=rules --rules <config>/rules.ai.yaml

Privacidade e custo:
  - Com provedor em nuvem, vão para a internet: os nomes dos artistas, até 3 títulos de cada (classify)
    ou os ~150 artistas mais frequentes com gêneros e contagem por década (rules). NUNCA tokens do
    Spotify, e-mail ou perfil. O programa mostra o destino e pede confirmação (--yes pula).
  - Com ollama em localhost nada sai da sua máquina.
  - 200 artistas custam centavos em modelos pequenos; --limit controla.
  - Nomes e títulos vêm da sua biblioteca e são tratados como dados não confiáveis (o prompt isola esse
    conteúdo e a resposta é validada: só aceita artistas pedidos, gêneros bem formados e regras válidas).
`

const helpListening = `Playlists por frequência de reprodução

A API do Spotify NÃO informa quantas vezes você ouviu cada música. O likedsorter usa:
  1) /me/top: ranking do Spotify (4 semanas, 6 meses, ~1 ano)  ->  likedsorter top
  2) histórico LOCAL que ele mesmo acumula das "tocadas recentemente" (o Spotify só guarda as últimas 50)
  3) a exportação estendida do Spotify (Conta > Privacidade > Dados estendidos de streaming; demora dias)
Esses acessos exigem permissão extra: rode "likedsorter auth login" uma vez (o login passa a pedir
user-top-read e user-read-recently-played; "auth login --no-listening" pula isso).

Ver o que você mais ouve:
  likedsorter top tracks --range short|medium|long [--limit 50]
  likedsorter top artists --range medium
  likedsorter top tracks --range short --to-playlist "Minhas do mês" [--yes]

Acumular histórico:
  likedsorter history sync                 (rode de vez em quando; com "schedule install" acumula sozinho)
  likedsorter history import Streaming_History_Audio_*.json     contagem exata de TODO o passado
  likedsorter history stats --days 30      mais tocadas na janela
  likedsorter history clear                apaga o histórico local

Playlists automáticas (--by=listening), só entre as músicas que você curtiu:
  Mais ouvidas · 30 dias   as N mais tocadas na janela      (--listen-top 50, --listen-days 30)
  Mais ouvidas de sempre   as N mais tocadas no histórico
  Esquecidas               curtidas há mais de 180 dias e sem tocar nesse período (--forgotten-days)
  Redescobertas            curtidas antigas que voltaram a tocar recentemente
  "Esquecidas" só aparece se o histórico já cobre o período inteiro; senão toda faixa pareceria esquecida.
  likedsorter plan  --by=listening
  likedsorter apply --by=listening --allow-remove     (as "Mais ouvidas" trocam de faixas ao longo do tempo)

Manter em dia sem você:
  likedsorter schedule install -- --by=listening --rotate
  O "auto --rotate" só é aceito com --by=listening e é a ÚNICA forma de o auto remover faixas, apenas
  das playlists desse modo (sempre com backup). O auto também atualiza o histórico a cada execução.
`
