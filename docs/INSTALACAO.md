# Guia de instalação e uso (Windows)

O **likedsorter** é um programa de linha de comando (`likedsorter.exe`) que organiza suas músicas curtidas do Spotify em playlists. Este guia leva você de zero até a primeira playlist criada.

> **Segurança:** por padrão nada é alterado na sua conta. Só o comando `apply` escreve no Spotify, e ele pede confirmação. O programa nunca apaga playlists.

## Índice

1. [Requisitos](#1-requisitos)
2. [Criar o app no Spotify (uma vez)](#2-criar-o-app-no-spotify-uma-vez)
3. [Instalar o executável](#3-instalar-o-executável)
4. [Configurar o Client ID](#4-configurar-o-client-id)
5. [Primeiro uso](#5-primeiro-uso)
6. [Receitas comuns](#6-receitas-comuns)
7. [Onde ficam os arquivos](#7-onde-ficam-os-arquivos)
8. [Atualizar e desinstalar](#8-atualizar-e-desinstalar)
9. [Solução de problemas](#9-solução-de-problemas)

---

## 1. Requisitos

| Item | Detalhe |
|---|---|
| Sistema | Windows 10/11 (64 bits) |
| Conta Spotify | **Premium** (exigência do *Development Mode* da API do Spotify) |
| Navegador | Qualquer um, para autorizar o login |
| Go 1.22+ | Só se for **compilar** do código-fonte ([go.dev/dl](https://go.dev/dl/)) |

## 2. Criar o app no Spotify (uma vez)

O programa precisa de um *Client ID* seu. Não há segredo envolvido (usa PKCE).

1. Acesse <https://developer.spotify.com/dashboard> e clique em **Create app**.
2. Preencha nome e descrição livremente.
3. Em **Redirect URIs** cadastre **exatamente**:
   ```
   http://127.0.0.1:8888/callback
   ```
   (`localhost` não é aceito pelo Spotify; use `127.0.0.1`.)
4. Marque **Web API** e salve.
5. Em **Settings → User Management**, adicione o **e-mail da sua conta Spotify** (Development Mode permite até 5 usuários).
6. Em **Settings**, copie o **Client ID**.

## 3. Instalar o executável

### Opção 00: assistente (mais fácil)

```powershell
.\likedsorter.exe start
```

Instala no PC, explica como criar o app no Spotify (abre o Dashboard), grava o Client ID, faz o login e a primeira sincronização. Se algo falhar depois, `likedsorter doctor` diagnostica. Os demais recursos (adicionar músicas, regras, modo automático, atualização) estão em `likedsorter help` e no README.

### Opção 0: pelo próprio executável

Com o `likedsorter.exe` em mãos (baixado ou compilado):

```powershell
.\likedsorter.exe install          # copia para %LOCALAPPDATA%\Programs\likedsorter e ajusta o PATH
```

Feche e abra o terminal e rode `likedsorter setup` para gravar o Client ID (passo 4 abaixo faz o mesmo à mão). A ajuda embutida cobre tudo: `likedsorter help instalar`, `likedsorter help segredos`, `likedsorter help tudo`. Opções: `--dir`, `--no-path`, `--uninstall`.

### Opção A: script PowerShell (compila e instala)

No PowerShell, na pasta do projeto:

```powershell
# Compila e instala (exige Go)
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Build

# OU instala um .exe já compilado (bin\likedsorter.exe)
powershell -ExecutionPolicy Bypass -File scripts\install.ps1

# OU aponta para um .exe específico
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Exe C:\Downloads\likedsorter.exe
```

O script, sem precisar de administrador:

- copia o programa para `%LOCALAPPDATA%\Programs\likedsorter\likedsorter.exe`;
- adiciona essa pasta ao **PATH do usuário**;
- cria `%APPDATA%\likedsorter\.env` a partir do `.env.example`.

**Feche e abra o terminal** depois, para o PATH valer. Confirme:

```powershell
likedsorter version
```

### Opção B: manual

1. Compile (ou obtenha) o `likedsorter.exe`:
   ```powershell
   go build -trimpath -ldflags "-s -w" -o bin\likedsorter.exe .\cmd\likedsorter
   ```
2. Crie a pasta `C:\Users\<você>\AppData\Local\Programs\likedsorter` e copie o `.exe` para lá.
3. Adicione a pasta ao PATH: *Win + R* → `sysdm.cpl` → **Avançado → Variáveis de Ambiente** → em *Path* do usuário, **Novo** → cole o caminho da pasta.
4. Abra um novo terminal e rode `likedsorter version`.

### Opção C: sem instalar

Rode direto de qualquer pasta: `.\bin\likedsorter.exe` (o `.env` pode ficar ao lado do executável ou no diretório de config).

## 4. Configurar o Client ID

O jeito mais simples é `likedsorter setup` (pergunta e valida os valores). Para fazer à mão, edite `%APPDATA%\likedsorter\.env` (o script `install.ps1` já cria o modelo) e preencha:

```ini
SPOTIFY_CLIENT_ID=cole_o_client_id_aqui
SPOTIFY_REDIRECT_URI=http://127.0.0.1:8888/callback
```

Abrir o arquivo rapidamente:

```powershell
notepad "$env:APPDATA\likedsorter\.env"
```

Opcionais no mesmo arquivo: `LASTFM_API_KEY` (fonte extra de gêneros, uso não comercial) e `LIKEDSORTER_CONTACT` (seu e-mail/URL, usado como identificação no MusicBrainz).

Precedência: variáveis de ambiente reais > `.env` da pasta atual > `.env` do diretório de config.

## 5. Primeiro uso

### Modo menu (mais fácil)

```powershell
likedsorter
```

Sem argumentos num terminal, abre o menu interativo: escolha a ação por número e responda às perguntas. O `apply` mostra o plano e só grava depois de um "s" explícito.

### Modo comandos (passo a passo)

```powershell
likedsorter auth login                 # abre o navegador para autorizar
likedsorter sync                       # lê curtidas e gêneros (1ª vez demora; depois é incremental)
likedsorter stats                      # estatísticas da biblioteca
likedsorter plan --by=macro-genre      # SIMULAÇÃO: mostra o que seria criado
likedsorter apply --by=macro-genre     # cria/atualiza playlists (pede confirmação)
```

Se o navegador não abrir sozinho: `likedsorter auth login --no-browser` imprime o link para você abrir manualmente.

## 6. Receitas comuns

| Objetivo | Comando |
|---|---|
| Agrupar por família de gênero | `likedsorter plan --by=macro-genre --min-size=10` |
| Por década | `likedsorter plan --by=decade` |
| Por artista (só quem tem 5+ músicas) | `likedsorter plan --by=artist --min-size=5` |
| Combinar critérios | `likedsorter plan --by=macro-genre+decade` |
| Salvar o plano em arquivo | `likedsorter plan --by=year --format=md --out plano.md` |
| Ver duplicadas | `likedsorter dedupe` |
| Criar sem perguntar | `likedsorter apply --by=macro-genre --min-size=10 --yes` |
| Playlists públicas | `likedsorter apply --by=decade --public` |
| Ver configuração efetiva | `likedsorter config show` |
| Ajuda completa | `likedsorter help` |

Estratégias de `--by`: `artist`, `genre`, `macro-genre`, `decade`, `year`, `added-period`, `language` (experimental).

**Backup e recuperação:** antes de alterar playlists existentes, o programa salva um snapshot em `%APPDATA%\likedsorter\backups\<data-hora>\snapshot.json`. Para voltar:

```powershell
likedsorter restore --file "$env:APPDATA\likedsorter\backups\<data-hora>\snapshot.json"
```

Para o detalhamento de todas as flags e do comportamento, veja o [README](../README.md).

## 7. Onde ficam os arquivos

| O quê | Local |
|---|---|
| Executável | `%LOCALAPPDATA%\Programs\likedsorter\likedsorter.exe` |
| Credenciais (`.env`), `config.yaml`, `token.json`, `state.json`, `backups\` | `%APPDATA%\likedsorter\` |
| Cache de curtidas e artistas | `%LOCALAPPDATA%\likedsorter\` |

Dá para mudar com `LIKEDSORTER_CONFIG_DIR` e `LIKEDSORTER_CACHE_DIR`.

## 8. Atualizar e desinstalar

**Atualizar:** rode o instalador de novo com `-Build` (ou `-Exe` apontando para o novo `.exe`). Configuração e token são mantidos.

**Desinstalar:**

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Uninstall
```

Remove o executável e a entrada do PATH, **preservando** suas configurações. Para apagar também os dados locais:

```powershell
likedsorter auth logout        # antes de desinstalar: apaga o token local
Remove-Item -Recurse "$env:APPDATA\likedsorter", "$env:LOCALAPPDATA\likedsorter"
```

Para revogar o acesso no Spotify: <https://www.spotify.com/account/apps/>.

## 9. Solução de problemas

| Sintoma | Causa provável / solução |
|---|---|
| `likedsorter` não é reconhecido | Terminal aberto antes da instalação. Feche e abra outro; confira `$env:Path`. |
| `SPOTIFY_CLIENT_ID não configurado` | `.env` sem o ID ou ainda com `COLE_SEU_CLIENT_ID_AQUI`. Edite `%APPDATA%\likedsorter\.env`. |
| `INVALID_CLIENT: Invalid redirect URI` | O Redirect URI do Dashboard não é idêntico a `http://127.0.0.1:8888/callback`. |
| Erro 403 do Spotify | Seu e-mail não está em *User Management* do app, ou a conta não é Premium. |
| Login não conclui / porta ocupada | Feche o que usa a porta 8888 ou mude a porta no `.env` **e** no Dashboard. |
| Navegador não abre | Use `likedsorter auth login --no-browser` e abra o link manualmente. |
| `sync` lento na primeira vez | Normal: uma chamada por artista, mais MusicBrainz (~1 req/s). O resultado fica em cache e Ctrl+C retoma depois. |
| Muitos artistas "Sem gênero" | O Spotify deprecou o campo `genres`. Use `--genre-source=spotify,musicbrainz,lastfm`. |
| Scripts bloqueados no PowerShell | Use `powershell -ExecutionPolicy Bypass -File scripts\install.ps1` (vale só para esse comando). |
| Antivírus/SmartScreen alerta no `.exe` | O binário não é assinado. Compile você mesmo (`-Build`) para ter certeza da origem. |
| Algo estranho | Rode com `-v` (`likedsorter sync -v`) para logs detalhados; tokens nunca são exibidos. |
