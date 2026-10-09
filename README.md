# likedsorter

CLI em Go que lê suas músicas curtidas (**Liked Songs**) no Spotify, agrupa por artista, gênero, década etc. e cria/atualiza playlists. **Dry-run por padrão**: nada é alterado sem confirmação explícita.

- [O que o app faz](#o-que-o-app-faz)
- [Instalação](#instalação)
- [Credenciais (segredos)](#credenciais-segredos)
- [Primeiros passos (assistente)](#primeiros-passos)
- [Adicionar músicas a uma playlist](#adicionar-músicas-a-uma-playlist)
- [Regras próprias e gêneros personalizados](#regras-próprias-e-gêneros-personalizados)
- [Modo automático e agendamento](#modo-automático-e-agendamento)
- [Inteligência artificial (opcional)](#inteligência-artificial-opcional)
- [Playlists por frequência de reprodução](#playlists-por-frequência-de-reprodução)
- [Gêneros: fontes e sugestões](#gêneros-fontes-e-sugestões)
- [Saída JSON, cores e simulação](#saída-json-cores-e-simulação)
- [Diagnóstico, atualização e autocompletar](#diagnóstico-atualização-e-autocompletar)
- [Referência de comandos](#referência-de-comandos)
- [Estratégias de agrupamento](#estratégias-de-agrupamento)
- [Configuração](#configuração)
- [Trocar credenciais e ajustar opções](#trocar-credenciais-e-ajustar-opções)
- [Onde ficam os arquivos](#onde-ficam-os-arquivos)
- [Segurança e recuperação](#segurança-e-recuperação)
- [Solução de problemas](#solução-de-problemas)
- [Spotify Web API: situação atual](#spotify-web-api-situação-atual)
- [Desenvolvimento](#desenvolvimento)

> Toda esta documentação também está **dentro do executável**: `likedsorter help` e `likedsorter help <tópico>` (`instalar`, `segredos`, `comecar`, `comandos`, `playlists`, `estrategias`, `regras`, `automatico`, `config`, `arquivos`, `seguranca`, `problemas`, `tudo`).

## O que o app faz

1. **Autentica** na sua conta Spotify (OAuth 2.0 + PKCE, sem Client Secret).
2. **Sincroniza** suas curtidas (incremental, com cache em disco) e descobre o **gênero** de cada artista (Spotify → MusicBrainz → Last.fm).
3. **Agrupa** as faixas por macro-gênero, gênero, artista, década, ano, período em que curtiu, idioma (experimental) ou combinações.
4. **Planeja** (`plan`, somente leitura): mostra quais playlists seriam criadas/atualizadas, em tabela, JSON, CSV ou Markdown.
5. **Aplica** (`apply`): cria as playlists e adiciona as faixas, com confirmação, backup automático e sem nunca apagar playlists.
6. **Adiciona músicas** (por link, ID ou busca) às suas playlists, sem duplicar.
7. **Regras próprias** (`--by=rules`) e **gêneros personalizados** por artista.
8. **Modo automático** (`auto`) e **agendamento** no Windows/cron para manter tudo em dia.
9. Extras: **assistente de primeiro uso** (`start`), **diagnóstico** (`doctor`), **autoatualização** (`update`), **autocompletar**, estatísticas, duplicadas por ISRC, export/restore de snapshots, configuração em camadas, menu interativo.

Dois modos de uso: **menu interativo** (`likedsorter` sem argumentos num terminal) ou **comandos diretos** (ideais para scripts).

## Instalação

Requisitos: Windows 10/11, Linux ou macOS; conta Spotify **Premium** (exigência do Development Mode da API). Go 1.22+ só é necessário para compilar.

O jeito mais fácil: rode o **assistente**, que instala, guia a criação do app no Spotify, grava o Client ID, faz o login e a primeira sincronização:

```powershell
.\likedsorter.exe start        # ou apenas .\likedsorter.exe na primeira vez
```

Se preferir fazer cada passo, o próprio executável se instala, sem administrador:

```powershell
.\likedsorter.exe install
```

- Windows: copia para `%LOCALAPPDATA%\Programs\likedsorter\` e adiciona ao PATH do usuário.
- Linux/macOS: copia para `~/.local/bin/` e mostra a linha `export PATH=...`.
- Opções: `--dir PASTA`, `--no-path`, `--uninstall`.

**Feche e abra o terminal** e confirme com `likedsorter version`. Também dá para usar o executável sem instalar (`.\likedsorter.exe ...`).

Compilar do código-fonte:

```powershell
go build -trimpath -ldflags "-s -w" -o likedsorter.exe ./cmd/likedsorter    # ou: make build
```

Alternativa por script (compila e instala): `powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Build`. Guia detalhado em [docs/INSTALACAO.md](docs/INSTALACAO.md).

**Atualizar:** `likedsorter update` (baixa a última release do GitHub e confere o SHA-256) ou rode `install` com um executável novo; configuração e token são mantidos.
**Desinstalar:** `likedsorter auth logout` e `likedsorter install --uninstall`. Para apagar também configuração e cache, veja [Onde ficam os arquivos](#onde-ficam-os-arquivos). Para revogar o acesso: <https://www.spotify.com/account/apps/>.

## Credenciais (segredos)

O app precisa do **Client ID** de um app seu no Spotify. Não existe Client Secret (PKCE).

1. Em <https://developer.spotify.com/dashboard>, **Create app**.
2. Em **Redirect URIs** cadastre **exatamente** `http://127.0.0.1:8888/callback` (`localhost` não é aceito).
3. Marque **Web API** e salve.
4. Em **Settings → User Management**, adicione o e-mail da sua conta Spotify (Development Mode: até 5 usuários).
5. Copie o **Client ID** (Settings).
6. Grave no programa:

```powershell
likedsorter setup                                   # pergunta no terminal
likedsorter setup --client-id SEU_ID                # sem terminal / scripts
likedsorter setup --lastfm-key CHAVE --contact voce@exemplo.com
likedsorter setup --show                            # confere o que está salvo (chaves mascaradas)
```

O `setup` grava o `.env` no diretório de config com permissão restrita ao seu usuário e valida o formato (Client ID de 32 hex; Redirect URI em `127.0.0.1`). Também funciona pelo menu (opção `s`). Dá para editar o arquivo à mão (modelo: `.env.example`).

| Variável | Obrigatória | Função |
|---|---|---|
| `SPOTIFY_CLIENT_ID` | sim | Client ID do app |
| `SPOTIFY_REDIRECT_URI` | não | padrão `http://127.0.0.1:8888/callback`; idêntico ao do Dashboard |
| `LASTFM_API_KEY` | não | fonte extra de gêneros (uso **não comercial**; atribua dados ao [Last.fm](https://www.last.fm)) |
| `AI_PROVIDER`, `AI_API_KEY`, `AI_MODEL`, `AI_BASE_URL` | não | IA opcional (`likedsorter ai setup`; veja a seção de IA) |
| `DISCOGS_TOKEN` | não | fonte de gêneros Discogs ([token pessoal](https://www.discogs.com/settings/developers)); o Deezer não precisa de chave |
| `LIKEDSORTER_CONTACT` | não | e-mail/URL enviado ao MusicBrainz no `User-Agent` (exigência deles) |

Precedência: variáveis de ambiente > `./.env` > `.env` do diretório de config. **Nunca versione** `.env` nem `token.json` (o `.gitignore` já os cobre).

## Primeiros passos

**Atalho:** `likedsorter start` (ou só `likedsorter` na primeira vez) executa os 4 passos abaixo com perguntas e explicações. Cada etapa já concluída é pulada, então pode rodar de novo à vontade.

```powershell
likedsorter install            # 1. instala no PC (feche e abra o terminal)
likedsorter setup              # 2. grava o Client ID
likedsorter auth login         # 3. autoriza no navegador (--no-browser só imprime o link)
likedsorter sync               # 4. lê curtidas e gêneros (1ª vez demora; depois é incremental)
likedsorter plan               # 5. SIMULA o que seria criado
likedsorter apply              # 6. cria/atualiza (mostra o plano e pede confirmação)
```

Ou rode `likedsorter` e siga o menu: `1` conta → `3` sincronizar → `5` plano → `6` aplicar. O `apply` pelo menu mostra o plano e só grava depois de um "s" explícito; remoção de faixas e playlists públicas são perguntas separadas (padrão: não). A opção `c` aceita qualquer comando direto.

## Referência de comandos

Flags globais (em qualquer posição): `--config ARQUIVO`, `--log-level debug|info|warn|error` (padrão `warn`), `-v/--verbose` (= debug; mostra cada requisição HTTP, nunca tokens). `likedsorter <comando> -h` lista as flags de um comando.

| Comando | O que faz | Altera o Spotify? |
|---|---|---|
| `menu` | menu interativo (padrão ao rodar sem argumentos num terminal) | só via `apply` |
| `start` | assistente guiado: instalar, credenciais, login, 1ª sincronização | só local |
| `install [--dir D] [--no-path] [--uninstall]` | instala/desinstala o executável e ajusta o PATH | não |
| `setup [--client-id --redirect-uri --lastfm-key --contact --show]` | grava credenciais no `.env` | não |
| `auth login [--no-browser] [--timeout 5m]` | autoriza a conta (abre o navegador) | não |
| `auth status [--offline]` | estado do token, escopos e usuário | não |
| `auth logout` | apaga o token local | não |
| `sync` | lê curtidas e gêneros; atualiza o cache | não |
| `stats [--format table\|json]` | total, horas, explícitas, por década, por ano, top artistas/gêneros | não |
| `groups` | mostra como as curtidas ficariam agrupadas | não |
| `plan` | dry-run do que seria criado/atualizado | não |
| `apply` | cria/atualiza playlists | **sim** (com confirmação) |
| `dedupe [--format --out --to-playlist NOME]` | lista a mesma gravação (ISRC) curtida em versões diferentes; `--to-playlist` copia as cópias antigas para uma playlist de revisão | só com `--to-playlist` |
| `playlist list [--json]` | suas playlists | não |
| `playlist search "texto"` | busca faixas (até 10) | não |
| `playlist add --to NOME MÚSICA...` | adiciona músicas à **sua** playlist | **sim** (com confirmação) |
| `genre list\|set\|unset\|missing` | gêneros personalizados por artista | não |
| `ai setup\|test\|classify\|rules\|genres` | IA propõe gêneros e regras (você revisa) | só `classify`/`rules` enviam metadados à IA escolhida |
| `top tracks\|artists [--to-playlist]` | o que você mais ouve (ranking do Spotify) | só com `--to-playlist` |
| `history sync\|stats\|import\|clear` | histórico local de reproduções | não |
| `suggest [--emit-rules]` | sugere novos grupos a partir do que sobrou em "Outros" | não |
| `rules init\|path\|check` | suas regras para `--by=rules` | não |
| `auto [flags do apply]` | sync + apply sem perguntar, **sem remover nada** | **sim** (só adiciona) |
| `schedule install\|status\|remove` | agenda o `auto` (Agendador de Tarefas / cron) | não |
| `doctor [--offline] [--json]` | diagnostica instalação, credenciais, login, porta e rede | não |
| `update [--check] [--yes]` | atualiza para a última release | não |
| `completion powershell\|bash\|zsh` | script de autocompletar | não |
| `export [--liked] [--out]` | snapshot JSON das playlists gerenciadas (ou das curtidas) | não |
| `restore --file snapshot.json` | restaura playlists gerenciadas de um snapshot | **sim** (com confirmação) |
| `config show\|get\|set\|unset\|path\|init` | ver e editar o `config.yaml` | não |
| `cache path\|info\|clear` | ver ou limpar o cache de curtidas e artistas | não |
| `version` | versão | não |
| `help [tópico]` | ajuda | não |

### Flags de dados (`sync`, `stats`, `groups`, `plan`, `apply`, `dedupe`)

| Flag | Efeito |
|---|---|
| `--full-sync` | relê todas as curtidas |
| `--refresh` | ignora o cache existente e o reconstrói |
| `--no-cache` | não lê nem grava cache |
| `--tracks-ttl 168h` / `--artists-ttl 720h` | validade dos caches (`0` = nunca) |
| `--no-enrich` | só curtidas, sem gêneros |
| `--genre-source spotify,musicbrainz,lastfm,deezer,discogs` | fontes de gênero, em ordem de prioridade |
| `--include-featured` | considera artistas de participação |
| `--market from_token` | market enviado ao Spotify (ou código ISO, ex.: `BR`) |
| `--concurrency 4` | chamadas simultâneas ao Spotify |

### Flags de agrupamento (`groups`, `plan`, `apply`)

| Flag | Efeito |
|---|---|
| `--by ESTRATÉGIA` | veja a próxima seção (inclui `rules`) |
| `--rules arq.yaml` | suas regras para `--by=rules` (padrão `<config>/rules.yaml`) |
| `--min-size N` | grupos menores vão para "Outros" (ou são ignorados com `--small-groups=skip`) |
| `--max-size N` | divide em "X (Parte 1)", "X (Parte 2)"… (teto do Spotify: 10.000) |
| `--split-over N` · `--split-by decade\|year` | grupos com mais de N faixas viram "Rock · Anos 90", "Rock · Anos 2000"… (subgrupos com menos de 10 faixas vão para "Rock · Outros") |
| `--multi-genre` | a faixa pode entrar em até 3 grupos de gênero |
| `--sort added\|release\|title` | ordem dentro do grupo |
| `--macro-map arq.yaml` | mapa de macro-gêneros próprio |
| `--name-template "Curtidas • {group}"` | nome das playlists |
| `--examples N` (`groups`) · `--dump-macro-map` (`groups`) | exemplos por grupo · imprime o mapa padrão para editar |

Filtros (`groups`, `plan`, `apply`, `stats`): `--filter-artist a,b`, `--filter-genre rock,pop`, `--since AAAA-MM-DD`. Casam por trecho, sem diferenciar maiúsculas (vários valores = OU; tipos diferentes = E). Com filtro ativo o plano **não calcula remoções** e o `apply` recusa `--allow-remove`/`--mode=recreate`.

### `plan`

`--format table|json|csv|md`, `--out ARQUIVO`, `--detail`, `--skip-existing` (não consulta playlists existentes). O relatório traz estatísticas, a tabela do plano (ação, desejadas, manter, adicionar, remover) e avisos. O CSV neutraliza nomes que começam com `= + - @` para não virarem fórmulas.

### `apply`

| Flag | Efeito |
|---|---|
| `--mode=sync` (padrão) | cria o que falta e **adiciona** faixas novas às playlists gerenciadas |
| `--mode=create-only` | só cria playlists novas |
| `--mode=recreate` | reescreve as gerenciadas na ordem do grupo (exige `--allow-remove`) |
| `--allow-remove` | permite remover faixas que saíram do grupo (sem isso, **nada é removido**) |
| `--private` (padrão) / `--public` | visibilidade das playlists **criadas** |
| `--max-playlists 100` | trava: aborta se o plano criaria mais playlists que isso |
| `--yes` | não pede confirmação (sem terminal é obrigatório) |
| `--dry-run` | mostra o plano e sai, sem confirmar nem escrever |

Rodar de novo é seguro: compara com o que já existe e só faz o que falta.

### Receitas

```powershell
likedsorter plan --by=macro-genre --min-size=10
likedsorter plan --by=artist --min-size=5 --format=md --out plano.md
likedsorter plan --by=decade --format=json --detail --out plano.json
likedsorter plan --filter-artist "djavan,marisa" --by=year
likedsorter plan --filter-genre rock --since 2023-01-01
likedsorter apply --by=macro-genre+decade --min-size=10 --public
likedsorter sync --genre-source=musicbrainz,lastfm --concurrency=2   # evita rate limit do Spotify
likedsorter export --out playlists.json
likedsorter restore --file "$env:APPDATA\likedsorter\backups\20261008-210000\snapshot.json"
```

## Estratégias de agrupamento

| `--by` | agrupa por |
|---|---|
| `artist` | artista principal |
| `genre` | gênero do artista principal |
| `macro-genre` | família: Rock, Hip Hop, Sertanejo, MPB, Pagode/Samba, Funk, Eletrônica, Pop, Gospel, Clássica, Jazz, Outros (**recomendado**) |
| `decade` / `year` | data de lançamento do álbum |
| `added-period` | mês/ano da curtida (UTC) |
| `listening` | **frequência de reprodução**: Mais ouvidas, Esquecidas, Redescobertas (veja a seção própria) |
| `rules` | **suas regras** em YAML (veja [Regras próprias](#regras-próprias-e-gêneros-personalizados)) |
| `language` | **experimental**: muitos ficam "Indefinido" |
| `a+b` | composta, ex.: `macro-genre+decade` → "Rock · Anos 90" |

**Macro-gêneros editáveis:** `likedsorter groups --dump-macro-map > macro.yaml`, edite (substrings, regex e prioridade; vence a maior) e use `--macro-map macro.yaml` ou salve como `macro_genres.yaml` no diretório de config. Gênero não reconhecido cai em "Outros"; artista sem gênero, em "Sem gênero".

**Gêneros dos artistas.** O `GET /artists?ids=` foi removido pelo Spotify, então é uma chamada por artista (concorrência limitada + 8 req/s); o resultado fica em cache (30 dias) e Ctrl+C retoma de onde parou. Cascata: o primeiro provedor que devolver gênero vence. **MusicBrainz** (sem chave, ~1 req/s, só correspondência forte) e **Last.fm** (`LASTFM_API_KEY`) são os fallbacks. Falhas de provedores externos não abortam a execução; erros do Spotify (403, rate limit prolongado) abortam preservando o cache.

**Cache incremental.** O `sync` lê só até a primeira faixa já conhecida; descurtidas são detectadas conferindo o total e, no pior caso, pelo TTL (7 dias). São descartadas e contadas: arquivos locais, indisponíveis no seu mercado, sem ID e duplicadas.

## Adicionar músicas a uma playlist

```powershell
likedsorter playlist list                                   # suas playlists (nome, dona, ID)
likedsorter playlist search "djavan sina"                   # até 10 resultados, com o URI de cada um
likedsorter playlist add --to "Treino" "racionais capítulo 4" "emicida levanta e anda"
likedsorter playlist add --to "Estudo" --create --pick "lofi hip hop"
likedsorter playlist add --to "Treino" spotify:track:2kgT6sMwXd3mdeXhBbLMQe https://open.spotify.com/track/0zXJ9O5PtvOEey11TcQ55y
likedsorter playlist add --to "Treino" --from-file musicas.txt --yes
likedsorter dedupe --to-playlist "Duplicadas para revisar"  # copia as cópias antigas para revisão
```

Cada **MÚSICA** pode ser um link, um URI, um ID ou um **texto de busca** (usa o 1º resultado; `--pick` deixa você escolher entre 5). Pelo menu, use a opção `a`.

- O destino é achado pelo nome (sem diferenciar maiúsculas), link ou ID **entre as suas playlists**. Playlists de outras pessoas (não colaborativas) são recusadas.
- Só **acrescenta** ao final: nunca remove nem reordena. Músicas que já estão na playlist (ou repetidas na sua lista) são puladas, então repetir o comando é seguro.
- Mostra o que vai entrar e pede confirmação (`--yes` pula). `--create` cria a playlist se o nome não existir (privada; `--public` para pública).
- Diferente do `apply`, funciona em **qualquer** playlist sua, não só nas gerenciadas, porque é você quem pede cada música.

## Regras próprias e gêneros personalizados

**Regras** (`--by=rules`): você descreve as playlists em um YAML.

```powershell
likedsorter rules init         # cria <config>/rules.yaml de exemplo
likedsorter rules check        # valida e lista
likedsorter plan --by=rules    # ou --rules outro.yaml; combina: --by=rules+decade
```

```yaml
primeira_regra: true            # true (padrão): só a 1ª regra que casar; false: todas
regras:
  - nome: MPB raiz
    artista: [djavan, marisa monte]   # trechos do nome (OU)
    genero: [mpb]                     # trechos do gênero do artista principal (OU)
  - nome: Clássicos 70-90
    lancamento: "1970-1999"           # ano ou intervalo do álbum
  - nome: Recentes
    curtida_desde: 2024-01-01         # quando você curtiu
  - nome: Acústicos
    titulo: [acústico, unplugged]     # trechos do título (OU)
```

Dentro de uma regra, todas as condições informadas precisam casar (E); em cada lista basta uma (OU). O que não casar com nenhuma regra vai para "Outros".

**Gêneros personalizados**: o gênero que você define vence Spotify, MusicBrainz e Last.fm (útil porque o Spotify deprecou o campo `genres`).

```powershell
likedsorter genre missing                 # artistas sem gênero, mais faixas primeiro
likedsorter genre set "Djavan" mpb        # aceita vários: rock,pop
likedsorter genre unset "Djavan"
likedsorter genre list
```

Ficam em `<config>/genre_overrides.yaml` (pode editar à mão).

## Modo automático e agendamento

```powershell
likedsorter auto --by=macro-genre --min-size=10      # sync incremental + apply --yes
likedsorter schedule install                         # todo dia às 03:00
likedsorter schedule install --every weekly --at 21:30 -- --by=macro-genre --min-size=10
likedsorter schedule status
likedsorter schedule remove
```

O `auto` é pensado para rodar sem você: **nunca remove faixas** (`--allow-remove` e `--mode=recreate` são recusados; a única exceção é `--rotate` com `--by=listening`, veja a seção de frequência), respeita todas as travas do `apply` e grava o resultado em `<config>/auto.log` (`--log-file` muda). No Windows, `schedule` cria a tarefa **likedsorter** no Agendador de Tarefas (roda com você logado); em Linux/macOS mostra a linha para o `crontab`.

## Inteligência artificial (opcional)

A IA ajuda onde o Spotify falha (artistas "Sem gênero") e a montar regras a partir de um pedido em português. Ela **só propõe**: o resultado é validado e revisado por você antes de ser salvo ou aplicado, e o `apply` mantém todas as travas.

```powershell
likedsorter ai setup                 # provedor: anthropic (Claude), openai ou ollama (local), chave e modelo
likedsorter ai test
likedsorter ai classify --dry-run    # propõe gêneros para quem está sem gênero (nada é salvo)
likedsorter ai classify              # idem, pede confirmação e salva em ai_genres.json
likedsorter ai rules "playlists para estudar, treinar e festa"   # gera <config>/rules.ai.yaml
likedsorter plan --by=rules --rules "$env:APPDATA\likedsorter\rules.ai.yaml"
```

| Provedor | Configuração | Modelo padrão | Para onde vão os dados |
|---|---|---|---|
| `anthropic` | `AI_API_KEY` (ou `ANTHROPIC_API_KEY`) | `claude-haiku-5-5` | api.anthropic.com |
| `openai` | `AI_API_KEY` (ou `OPENAI_API_KEY`) | `gpt-4o-mini` | api.openai.com (ou `AI_BASE_URL` compatível) |
| `ollama` | sem chave; `AI_BASE_URL` padrão `http://localhost:11434/v1` | `llama3.1` | **só o seu computador** |

- **Privacidade:** com provedor em nuvem vão para a internet apenas nomes de artistas, até 3 títulos de cada (`classify`) ou os ~150 artistas mais frequentes com gêneros e contagem por década (`rules`). Nunca tokens do Spotify, e-mail ou perfil. O programa mostra o destino e pede confirmação (`--yes` pula); com Ollama nada sai da máquina.
- **Segurança:** nomes e títulos vêm da sua biblioteca e são tratados como dados não confiáveis. O prompt isola esse conteúdo, e a resposta só é aceita se tiver o formato esperado (artistas que foram pedidos, gêneros bem formados, regras válidas).
- **Precedência de gêneros:** `genre set` (você) > IA (`ai_genres.json`, só para quem está sem gênero) > fontes externas. `likedsorter ai genres list|clear` gerencia o que a IA salvou.
- **Custo:** 200 artistas custam centavos em modelos pequenos (`--limit` controla).

## Playlists por frequência de reprodução

A API do Spotify **não informa quantas vezes você ouviu** cada música. O app combina três fontes:

| Fonte | Como | Observação |
|---|---|---|
| `/me/top` | `likedsorter top tracks\|artists --range short\|medium\|long` | ranking do Spotify (até 50) |
| Tocadas recentemente | `likedsorter history sync` | o Spotify guarda só as últimas 50; o app **acumula** localmente a cada execução (o `auto` faz isso sozinho) |
| Exportação estendida | `likedsorter history import Streaming_History_Audio_*.json` | contagem exata de todo o passado (Conta Spotify → Privacidade → Dados estendidos; leva dias) |

Essas leituras exigem permissão extra: rode `likedsorter auth login` **uma vez** (`--no-listening` pula). Quem não usa o recurso não precisa refazer o login.

```powershell
likedsorter top tracks --range short --to-playlist "Minhas do mês"
likedsorter history sync
likedsorter history stats --days 30
likedsorter plan --by=listening                      # Mais ouvidas, Esquecidas, Redescobertas
likedsorter apply --by=listening --allow-remove      # as "Mais ouvidas" trocam de faixas com o tempo
likedsorter schedule install -- --by=listening --rotate   # tudo isso, sozinho
```

| Grupo (`--by=listening`) | Regra |
|---|---|
| Mais ouvidas · 30 dias | as N mais tocadas na janela (`--listen-top 50`, `--listen-days 30`) |
| Mais ouvidas de sempre | as N mais tocadas em todo o histórico |
| Esquecidas | curtidas há mais de 180 dias e **sem tocar** nesse período (`--forgotten-days`); só aparece se o histórico já cobre o período inteiro |
| Redescobertas | curtidas antigas que voltaram a tocar recentemente |

O `auto --rotate` só é aceito com `--by=listening` e é a única exceção à regra "o `auto` nunca remove": apenas as playlists desse modo perdem faixas, sempre com backup.

## Gêneros: fontes e sugestões

O Spotify deprecou o campo `genres`, então o app combina fontes em cascata (`--genre-source`, a primeira que devolver gênero vence):

| Fonte | Chave | Observações |
|---|---|---|
| `spotify` | — | pode vir vazio |
| `musicbrainz` | — | ~1 req/s; só correspondência forte |
| `lastfm` | `LASTFM_API_KEY` | uso não comercial |
| `deezer` | — | API pública; gênero vem do álbum das faixas mais tocadas do artista (3 chamadas por artista) |
| `discogs` | `DISCOGS_TOKEN` | usa *styles* (ex.: MPB, Bossa Nova) antes dos gêneros amplos; só conta releases do artista exato; 60 req/min |

```powershell
likedsorter sync --genre-source=spotify,deezer,musicbrainz
likedsorter setup --discogs-token SEU_TOKEN
likedsorter genre missing                 # quem ficou sem gênero (para definir à mão)
```

O que sobrou em "Outros"/"Sem gênero" pode virar playlist:

```powershell
likedsorter suggest --by=macro-genre --min-size=10          # artistas, gêneros e décadas frequentes entre as sobras
likedsorter suggest --emit-rules >> "$(likedsorter rules path)"   # gera regras YAML prontas
```

Para playlists grandes demais, `--split-over 300` divide por década (ou `--split-by year`) em vez de "Parte 1/2".

## Saída JSON, cores e simulação

- `--json` está disponível em: `version`, `auth status`, `sync`, `groups`, `plan`, `stats`, `apply`, `restore`, `dedupe`, `doctor`, `config show`, `setup --show`, `install`, `update`, `genre list/missing`, `rules check`, `suggest`, `playlist list/search/add` e `schedule status`. O texto de progresso vai para **stderr** e só o JSON sai em stdout, então `likedsorter apply --yes --json | ConvertFrom-Json` funciona.
- Cores: automáticas em terminais compatíveis (`doctor`, menu); `NO_COLOR=1` desliga.
- Progresso: `sync` mostra barra `[####----] 42% (840/2000)` quando stderr é um terminal.
- Simulação: `apply --dry-run` mostra o plano e sai sem confirmar nem escrever (o `plan` continua sendo o dry-run completo, com mais formatos).

## Diagnóstico, atualização e autocompletar

```powershell
likedsorter doctor                  # PATH, config, Client ID, porta, login, escopos, rede e API (--offline, --json)
likedsorter update --check          # há versão nova?
likedsorter update                  # baixa a release, confere o SHA-256 e troca o executável
likedsorter completion powershell | Out-String | Invoke-Expression   # Tab completa comandos e --by
```

`update` só instala binários listados em `checksums.txt` da release e recusa se o hash não conferir.

**Scoop (Windows):** cada release inclui o manifest `likedsorter.json` (gerado pelo workflow, com hashes). Instale com `scoop install https://github.com/ikauedeveloper/likedsorter/releases/latest/download/likedsorter.json` e atualize com `scoop update likedsorter`. Para publicar uma release basta enviar uma tag: `git tag v1.0.0 && git push origin v1.0.0` (workflow `.github/workflows/release.yml`).

## Configuração

Precedência: **flags > variáveis de ambiente > `config.yaml` > padrões**.

```powershell
likedsorter config init     # cria o modelo em <config>/config.yaml (--force sobrescreve)
likedsorter config show     # valor efetivo de cada chave e de onde veio
likedsorter config path
```

| Chave | Padrão | Variável |
|---|---|---|
| `log_level` | `warn` | `LIKEDSORTER_LOG_LEVEL` |
| `sync.market` | `from_token` | `LIKEDSORTER_SYNC_MARKET` |
| `sync.tracks_ttl` / `sync.artists_ttl` | `168h` / `720h` | `LIKEDSORTER_SYNC_TRACKS_TTL` / `…_ARTISTS_TTL` |
| `sync.genre_source` | `spotify,musicbrainz` | `LIKEDSORTER_SYNC_GENRE_SOURCE` |
| `sync.concurrency` | `4` | `LIKEDSORTER_SYNC_CONCURRENCY` |
| `sync.include_featured` | `false` | `LIKEDSORTER_SYNC_INCLUDE_FEATURED` |
| `group.by` | `macro-genre` | `LIKEDSORTER_GROUP_BY` |
| `group.min_size` / `group.max_size` | `0` / `0` | `LIKEDSORTER_GROUP_MIN_SIZE` / `…_MAX_SIZE` |
| `group.small_groups` | `other` | `LIKEDSORTER_GROUP_SMALL_GROUPS` |
| `group.multi_genre` | `false` | `LIKEDSORTER_GROUP_MULTI_GENRE` |
| `group.sort` | `added` | `LIKEDSORTER_GROUP_SORT` |
| `group.macro_map` | vazio | `LIKEDSORTER_GROUP_MACRO_MAP` |
| `group.split_over` | `0` (desligado) | `LIKEDSORTER_GROUP_SPLIT_OVER` |
| `group.split_by` | `decade` | `LIKEDSORTER_GROUP_SPLIT_BY` |
| `plan.name_template` | `Curtidas • {group}` | `LIKEDSORTER_PLAN_NAME_TEMPLATE` |
| `apply.mode` | `sync` | `LIKEDSORTER_APPLY_MODE` |
| `apply.public` | `false` | `LIKEDSORTER_APPLY_PUBLIC` |
| `apply.max_playlists` | `100` | `LIKEDSORTER_APPLY_MAX_PLAYLISTS` |

Mudar pelo terminal, sem editar arquivo (cada valor é validado antes de gravar):

```powershell
likedsorter config set group.min_size 10
likedsorter config set apply.public true
likedsorter config get group.min_size
likedsorter config unset group.min_size      # volta ao padrão
```

Chave desconhecida ou valor inválido dá erro com a lista de chaves válidas. **Por segurança, `--allow-remove` e `--yes` não existem na configuração**: precisam ser passados a cada execução.

Outras variáveis: `LIKEDSORTER_CONFIG_DIR` e `LIKEDSORTER_CACHE_DIR` (mudam os diretórios).

## Trocar credenciais e ajustar opções depois

Pelo menu (`likedsorter` -> **9) Configurações e ferramentas**) ou por comandos:

| Quero | Menu 9 -> | Comando |
|---|---|---|
| Trocar o Client ID / Redirect URI | Credenciais | `likedsorter setup --client-id NOVO_ID` (depois `auth logout` e `auth login`; o menu faz isso por você) |
| Trocar a chave do Last.fm / Discogs / contato | Fontes de gênero | `likedsorter setup --lastfm-key CHAVE --discogs-token TOKEN --contact voce@exemplo.com` |
| Remover uma chave opcional | Fontes de gênero (digite `-`) | `likedsorter setup --unset lastfm-key,discogs-token,contact` |
| Mudar padrões (estratégia, tamanho mínimo, divisão, playlists públicas...) | Preferências | `likedsorter config set CHAVE VALOR` |
| Ver o que está salvo | Ver tudo | `likedsorter setup --show` e `likedsorter config show` |
| Definir gênero de um artista | Gêneros personalizados | `likedsorter genre set "Artista" rock` |
| Agendar a atualização automática | Agendamento | `likedsorter schedule install` |
| Limpar o cache | Cache | `likedsorter cache clear` |
| Trocar de conta Spotify | Conta | `likedsorter auth logout` + `likedsorter auth login` |

O `setup` sem argumentos pergunta cada valor com o atual entre colchetes (Enter mantém). Trocar o Client ID invalida o token salvo, por isso é preciso autorizar de novo.

## Onde ficam os arquivos

| O quê | Windows | Linux / macOS |
|---|---|---|
| Executável (após `install`) | `%LOCALAPPDATA%\Programs\likedsorter\` | `~/.local/bin/` |
| Config: `.env`, `config.yaml`, `token.json` (0600), `state.json`, `macro_genres.yaml`, `rules.yaml`, `rules.ai.yaml`, `genre_overrides.yaml`, `ai_genres.json`, `plays.json`, `auto.log`, `backups\` | `%APPDATA%\likedsorter\` | `~/.config/likedsorter/` · `~/Library/Application Support/likedsorter/` |
| Cache: `tracks.json`, `artists.json` | `%LOCALAPPDATA%\likedsorter\` | `~/.cache/likedsorter/` |

Apagar tudo (Windows): `Remove-Item -Recurse "$env:APPDATA\likedsorter", "$env:LOCALAPPDATA\likedsorter"`.

## Segurança e recuperação

- `plan`, `groups`, `stats`, `dedupe` (sem `--to-playlist`), `export`, `doctor` e `playlist list/search` são somente leitura. Escrevem na conta: `apply`, `restore`, `playlist add`, `dedupe --to-playlist` (todos pedem confirmação) e `auto` (só adiciona, nunca remove).
- **Nunca apaga playlists** (o cliente nem tem esse método). No máximo esvazia e repopula as gerenciadas, só com `--mode=recreate --allow-remove`.
- Só altera playlists **suas** com `[managed:likedsorter]` na descrição (ou criadas pela ferramenta e registradas em `state.json`). O casamento grupo → playlist é pelo **nome**: renomear uma gerenciada a torna "órfã" (nunca é alterada) e uma nova seria criada. Playlists com o mesmo nome sem marcador não são tocadas (aviso no plano).
- **Backup antes de alterar:** as playlists existentes que serão modificadas são salvas em `<config>/backups/<data-hora>/snapshot.json`. Se o backup falhar, nada é alterado.
- Escritas em lotes de 100. Em falha ambígua (5xx/rede) a playlist é relida antes de continuar, então uma escrita pela metade não duplica faixas. Erros definitivos (ex.: 403) interrompem; rode o mesmo comando para retomar.
- `restore` só reescreve gerenciadas que ainda existem, salva o estado atual antes e pede confirmação. Arquivos locais não podem ser restaurados pela API.
- Logs nunca mostram tokens ou chaves.

## Solução de problemas

| Sintoma | Solução |
|---|---|
| `likedsorter` não é reconhecido | Terminal aberto antes do `install`; feche e abra outro |
| `SPOTIFY_CLIENT_ID não configurado` | Rode `likedsorter setup` |
| `INVALID_CLIENT: Invalid redirect URI` | O Redirect URI do Dashboard deve ser idêntico ao do `.env` |
| 403 do Spotify | E-mail fora de *User Management*, ou conta não Premium |
| Login não conclui | Porta 8888 ocupada: troque a porta no `setup` **e** no Dashboard |
| Navegador não abre | `auth login --no-browser` |
| `sync` lento na 1ª vez | Normal; o cache retoma após Ctrl+C |
| Muitos "Sem gênero" | `--genre-source=spotify,musicbrainz,lastfm` |
| Erro ao reinstalar (arquivo em uso) | Feche outros terminais usando o `likedsorter` |
| SmartScreen/antivírus alerta | O binário não é assinado; compile você mesmo |
| Investigar uma execução | Adicione `-v` (ex.: `likedsorter sync -v`) |
| Não sei o que está errado | `likedsorter doctor` |
| `playlist add`/busca dá 403 de escopo | `likedsorter auth login` de novo |
| Busca/faixa com rate limit | O Spotify limita por endpoint; o `playlist add` segue só pelo ID quando não consegue ler os detalhes |

## Spotify Web API: situação atual

Verificada em out/2026 (changelog e guia de migração de fev/2026 em developer.spotify.com).

| Endpoint | Uso | Escopo |
|---|---|---|
| `GET /me/tracks` (limit ≤ 50) | curtidas + `added_at` | `user-library-read` |
| `GET /artists/{id}` | gêneros (um a um) | nenhum |
| `GET /me` | identificar o usuário | nenhum |
| `POST /me/playlists` | criar playlist | `playlist-modify-private` / `-public` |
| `GET /playlists/{id}/items` | ler itens (só playlists próprias/colaborativas) | `playlist-read-private` |
| `POST/PUT/DELETE /playlists/{id}/items` | adicionar/substituir/remover (100 por chamada) | `playlist-modify-*` |
| `GET /me/playlists` | achar playlists gerenciadas | `playlist-read-private` |

Escopos: `user-library-read`, `playlist-read-private`, `playlist-modify-private`, `playlist-modify-public`.

Removido/restrito e como o app se adapta:

| Mudança | Adaptação |
|---|---|
| `GET /artists?ids=` removido | uma chamada por artista, com concorrência limitada, rate limiter e cache |
| `genres` de artista deprecated (pode vir vazio) | fallbacks MusicBrainz → Last.fm → "Sem gênero" |
| `popularity`/`followers` removidos | ordenação por popularidade não existe |
| `/tracks` → `/items`; `track` → `item` | só `/items` |
| `POST /users/{id}/playlists` removido | `POST /me/playlists` |
| Audio features, recommendations, related artists | não usados |
| Redirect URI: `localhost` proibido | callback em `http://127.0.0.1:PORT` |
| Development Mode: dono Premium, até 5 usuários, rate limit menor | 403 se o usuário não estiver em *User Management* |

Rate limit: janela deslizante de 30 s; 429 traz `Retry-After`, respeitado pelo cliente (com backoff e jitter para 5xx).

**Por que cliente HTTP próprio** (e não `zmb3/spotify`): a API mudou muito em 2026, a superfície usada é pequena (~7 endpoints), é preciso controle fino de retry/rate limit/`context`, e uma interface facilita mockar com `httptest`. Usa `golang.org/x/oauth2` (PKCE e refresh), `x/time/rate` e `x/sync/errgroup`.

## Desenvolvimento

Pré-requisitos: Go 1.22+; `make` e `golangci-lint` opcionais.

```
make build            # bin/likedsorter com versão embutida
make test             # go test -race (exige gcc/clang; no Windows sem MinGW: make test-norace)
make lint             # go vet + golangci-lint
make cover-check      # falha se grouping, planner ou cache tiverem < 70% de cobertura
make ci               # fmt-check + vet + lint + test + cover-check
```

Sem `make` (Windows): `go vet ./...`, `go test ./...`, `golangci-lint run ./...`, `bash scripts/check-coverage.sh 70`.

**Testes:** tabela para estratégias, planner/diff, executor e lotes; `httptest` para o cliente Spotify (paginação, `429`, `5xx`, 4xx, falha ambígua sem duplicar); *golden files* em `internal/report/testdata` (regravar com `go test ./internal/report -update` e revisar o diff); executor contra um Spotify falso com injeção de falhas.

**CI** (`.github/workflows/ci.yml`): `gofmt`, `go mod tidy`, `go vet`, `golangci-lint`; testes com `-race` em Linux, macOS e Windows; cobertura mínima de 70%; build multiplataforma (linux/mac amd64+arm64, windows amd64) publicado como artefato. Dependabot atualiza módulos e Actions mensalmente.

Estrutura: `cmd/likedsorter` (CLI, menu, assistente, help, install, setup, doctor, playlist, auto/schedule, update, completion) · `internal/{auth,spotify,library,enrich,grouping,filter,planner,executor,report,backup,state,cache,config,fsutil,update}`.

Não implementado (por decisão ou limitação da API do Spotify):

- TUI com bubbletea (o fluxo `plan` → ajustar flags/regras → `apply` cobre a revisão dos grupos).
- Remover duplicadas das curtidas (só há a cópia para uma playlist de revisão): exige escopo e endpoint de escrita na biblioteca que a ferramenta não usa.
- Humor/BPM/energia: o Spotify descontinuou as audio features.
- Outros serviços de música; pacote `.msi`, manifest winget (exige PR ao repositório da Microsoft) e assinatura de código.

## Licença

[MIT](LICENSE) © 2026 ikauedeveloper. Os dados de gênero obtidos do Last.fm, MusicBrainz, Deezer e Discogs seguem os termos de cada serviço; veja as notas nas seções de credenciais e gêneros.
