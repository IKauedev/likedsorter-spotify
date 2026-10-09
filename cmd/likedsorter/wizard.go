package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ikauedeveloper/likedsorter/internal/auth"
	"github.com/ikauedeveloper/likedsorter/internal/config"
)

const dashboardURL = "https://developer.spotify.com/dashboard"

// credentialsMissing diz se ainda falta o Client ID (assistente deve rodar).
func credentialsMissing() bool {
	_, err := config.Load()
	return errors.Is(err, config.ErrMissingClientID)
}

// reloadEnv aplica no processo os valores gravados pelo setup, inclusive sobre
// um placeholder que já tenha sido carregado do .env de exemplo.
func reloadEnv(path string) {
	vals, err := config.ReadEnvFile(path)
	if err != nil {
		return
	}
	for k, v := range vals {
		_ = os.Setenv(k, v)
	}
}

func (m *menu) say(format string, a ...any) { fmt.Fprintf(m.out, format+"\n", a...) }

// wizard conduz do zero até a primeira sincronização. Cada etapa é pulada se já estiver pronta.
func (m *menu) wizard() error {
	m.say("%s", m.paint("1;38;5;75", "━━ Assistente de configuração ━━"))
	m.say("Vamos deixar tudo pronto em poucos passos. Pode interromper com Ctrl+C e retomar depois com `likedsorter start`.\n")

	// 1/4 Instalação
	m.say("%s", m.paint("1", "1/4  Instalação no PC"))
	if _, err := exec.LookPath("likedsorter"); err == nil {
		m.say("  ✔ o comando \"likedsorter\" já funciona em qualquer pasta.\n")
	} else if exe, _ := os.Executable(); isTemporaryPath(exe) {
		m.say("  • rodando de uma pasta temporária; pulando a instalação.\n")
	} else {
		ok, err := m.confirm("  Instalar o likedsorter neste PC (para usar de qualquer pasta)?", true)
		if err != nil {
			return err
		}
		if ok {
			m.exec("install")
		}
		m.say("")
	}

	// 2/4 Credenciais
	m.say("%s", m.paint("1", "2/4  Credenciais do Spotify"))
	path, err := config.EnvFilePath()
	if err != nil {
		return err
	}
	if credentialsMissing() {
		m.say("  O Spotify exige que você crie um \"app\" gratuito para obter um Client ID (leva ~3 minutos):")
		m.say("   a) Abra %s e clique em \"Create app\".", dashboardURL)
		m.say("   b) Em \"Redirect URIs\" cadastre EXATAMENTE:  %s", config.DefaultRedirectURI)
		m.say("   c) Marque \"Web API\" e salve.")
		m.say("   d) Em Settings → User Management, adicione o e-mail da sua conta Spotify.")
		m.say("   e) Em Settings, copie o \"Client ID\".")
		m.say("  (Sua conta precisa ser Premium; é uma exigência do Spotify para apps em Development Mode.)\n")
		if ok, err := m.confirm("  Abrir o Dashboard no navegador agora?", true); err != nil {
			return err
		} else if ok {
			if err := auth.OpenBrowser(dashboardURL); err != nil {
				m.say("  Não consegui abrir o navegador; acesse o link manualmente.")
			}
		}
		for attempt := 0; credentialsMissing(); attempt++ {
			if attempt == 3 {
				return errors.New("sem Client ID; rode `likedsorter start` quando tiver copiado o ID")
			}
			if err := interactiveSetup(path, m.ask, m.out); err != nil {
				if errors.Is(err, io.EOF) {
					return err
				}
				m.say("  ✖ %v\n", err)
				continue
			}
			reloadEnv(path)
		}
		m.say("")
	} else {
		m.say("  ✔ Client ID já configurado (%s).\n", path)
	}

	// 3/4 Login
	m.say("%s", m.paint("1", "3/4  Autorizar sua conta"))
	store, err := tokenStore()
	if err != nil {
		return err
	}
	loggedIn := false
	if _, err := store.Load(); err == nil {
		m.say("  ✔ você já está autenticado.\n")
		loggedIn = true
	} else {
		m.say("  O navegador vai abrir para você autorizar o acesso às suas curtidas e playlists.")
		ok, err := m.confirm("  Autorizar agora?", true)
		if err != nil {
			return err
		}
		if ok {
			loggedIn = m.exec("auth", "login")
		}
		m.say("")
	}

	// 4/4 Primeira sincronização
	m.say("%s", m.paint("1", "4/4  Ler suas músicas curtidas"))
	if !loggedIn {
		m.say("  Pulado (faça o login primeiro: `likedsorter auth login`).\n")
	} else {
		ok, err := m.confirm("  Sincronizar agora? (a 1ª vez pode levar alguns minutos; é incremental depois)", true)
		if err != nil {
			return err
		}
		if ok {
			m.exec("sync")
		}
	}

	m.say("\n%s", m.paint("1;32", "Pronto!"))
	m.say("Próximos passos:")
	m.say("  likedsorter plan      mostra como suas curtidas ficariam em playlists (não altera nada)")
	m.say("  likedsorter apply     cria as playlists (pede confirmação)")
	m.say("  likedsorter           abre o menu")
	return nil
}

// isTemporaryPath detecta binários de `go run` / pastas temporárias.
func isTemporaryPath(p string) bool {
	tmp := filepath.Clean(os.TempDir())
	return len(p) >= len(tmp) && filepath.Clean(p)[:len(tmp)] == tmp
}

func runStart(ctx context.Context, in io.Reader, out io.Writer, g globals) error {
	if !stdinIsTerminal() {
		return errors.New("o assistente precisa de um terminal interativo; use `likedsorter setup --client-id ...` em scripts")
	}
	return newMenu(ctx, in, out, g).wizard()
}
