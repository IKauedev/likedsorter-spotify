package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/config"
)

const taskName = "likedsorter"

// runAuto mantém as playlists em dia sem interação: lê as curtidas novas (sync
// incremental) e aplica o plano com --yes. Por segurança NUNCA remove faixas.
func runAuto(ctx context.Context, args []string, out io.Writer) error {
	var rest []string
	logFile := ""
	rotate := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(a, "=")
		switch {
		case name == "--log-file" || name == "-log-file":
			if hasVal {
				logFile = val
			} else if i+1 < len(args) {
				i++
				logFile = args[i]
			} else {
				return errors.New("--log-file exige um caminho")
			}
		case name == "--allow-remove" || name == "-allow-remove",
			(name == "--mode" || name == "-mode") && (val == "recreate" || (!hasVal && i+1 < len(args) && args[i+1] == "recreate")):
			return errors.New("o modo automático nunca remove faixas: --allow-remove e --mode=recreate não são aceitos em `auto`")
		case name == "--rotate" || name == "-rotate":
			rotate = true
		case name == "--yes" || name == "-yes":
			// já implícito
		default:
			rest = append(rest, a)
		}
	}

	if rotate {
		if by := byFlagValue(rest); by != "listening" {
			return errors.New("--rotate só vale com --by=listening: as playlists \"Mais ouvidas\" precisam trocar de faixas, e só elas podem perder itens")
		}
		rest = append(rest, "--allow-remove")
	}

	w := out
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		w = io.MultiWriter(out, f)
		fmt.Fprintf(f, "\n===== %s =====\n", time.Now().Format(time.RFC3339))
	}
	msg := w
	if hasJSON(rest) {
		msg = os.Stderr
	}
	fmt.Fprintln(msg, "likedsorter auto: sincronizando e aplicando (sem remover nada)...")
	// Acumula o histórico de reprodução (silencioso se o login não incluir esse acesso).
	if added, _, herr := syncRecentPlays(ctx); herr == nil {
		fmt.Fprintf(msg, "histórico de reprodução: +%d nova(s)\n", added)
	} else if !errors.Is(herr, errNoListening) {
		fmt.Fprintln(msg, "aviso: histórico de reprodução não atualizado:", herr)
	}
	err := run(ctx, append([]string{"apply", "--yes"}, rest...), w)
	if err != nil {
		fmt.Fprintln(msg, "erro:", err)
	}
	return err
}

func defaultLogFile() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "auto.log")
}

// runSchedule: schedule install | status | remove (Agendador de Tarefas no Windows; cron nos demais).
func runSchedule(ctx context.Context, args []string, out io.Writer) error {
	const uso = "uso: likedsorter schedule install [--every hourly|daily|weekly] [--at HH:MM] [-- flags do apply] | status | remove"
	if len(args) == 0 {
		return errors.New(uso)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		fl := flag.NewFlagSet("schedule install", flag.ContinueOnError)
		every := fl.String("every", "daily", "hourly | daily | weekly")
		at := fl.String("at", "03:00", "horário (HH:MM) para daily/weekly")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		if _, err := time.Parse("15:04", *at); err != nil {
			return fmt.Errorf("--at deve ser HH:MM, recebi %q", *at)
		}
		extra := strings.TrimSpace(strings.Join(fl.Args(), " "))
		cmdline := fmt.Sprintf("\"%s\" auto --log-file \"%s\"", exe, defaultLogFile())
		if extra != "" {
			cmdline += " " + extra
		}

		if runtime.GOOS != "windows" {
			spec := map[string]string{"hourly": "0 * * * *", "daily": fmt.Sprintf("%s %s * * *", (*at)[3:], (*at)[:2]), "weekly": fmt.Sprintf("%s %s * * 0", (*at)[3:], (*at)[:2])}[*every]
			if spec == "" {
				return fmt.Errorf("--every deve ser hourly, daily ou weekly")
			}
			fmt.Fprintln(out, "Adicione esta linha ao seu crontab (crontab -e):")
			fmt.Fprintf(out, "  %s %s\n", spec, cmdline)
			return nil
		}

		sc := []string{"/SC"}
		switch *every {
		case "hourly":
			sc = append(sc, "HOURLY")
		case "daily":
			sc = append(sc, "DAILY", "/ST", *at)
		case "weekly":
			sc = append(sc, "WEEKLY", "/D", "SUN", "/ST", *at)
		default:
			return fmt.Errorf("--every deve ser hourly, daily ou weekly")
		}
		if len(cmdline) > 255 {
			return errors.New("o comando agendado ficou longo demais (limite do Windows: 261 caracteres); use um caminho de log mais curto")
		}
		a := append([]string{"/Create", "/F", "/TN", taskName}, sc...)
		a = append(a, "/TR", cmdline)
		if b, err := exec.CommandContext(ctx, "schtasks", a...).CombinedOutput(); err != nil { //nolint:gosec // G204: argumentos fixos + caminho do próprio executável
			return fmt.Errorf("schtasks: %w: %s", err, strings.TrimSpace(string(b)))
		}
		fmt.Fprintf(out, "Tarefa \"%s\" criada (%s%s).\n", taskName, *every, map[bool]string{true: "", false: " às " + *at}[*every == "hourly"])
		fmt.Fprintf(out, "Comando: %s\nLog: %s\n", cmdline, defaultLogFile())
		fmt.Fprintln(out, "Roda enquanto você estiver logado no Windows, só adiciona músicas (nunca remove). Teste agora: schtasks /Run /TN "+taskName)
		return nil

	case "status":
		asJSON := hasJSON(args[1:])
		if runtime.GOOS != "windows" {
			fmt.Fprintln(out, "Veja com: crontab -l | grep likedsorter")
			return nil
		}
		b, err := exec.CommandContext(ctx, "schtasks", "/Query", "/TN", taskName, "/FO", "LIST", "/V").CombinedOutput()
		if err != nil {
			if asJSON {
				return writeJSON(out, map[string]any{"scheduled": false})
			}
			fmt.Fprintln(out, "Nenhuma tarefa agendada do likedsorter.")
			return nil
		}
		if asJSON {
			return writeJSON(out, map[string]any{"scheduled": true, "task": taskName})
		}
		for _, l := range strings.Split(string(b), "\n") {
			for _, k := range []string{"Status", "Próxima", "Next Run", "Última", "Last Run", "Tarefa a Executar", "Task To Run", "Resultado", "Last Result"} {
				if strings.Contains(l, k) {
					fmt.Fprintln(out, strings.TrimSpace(l))
					break
				}
			}
		}
		return nil

	case "remove":
		if runtime.GOOS != "windows" {
			fmt.Fprintln(out, "Remova a linha do likedsorter com: crontab -e")
			return nil
		}
		if b, err := exec.CommandContext(ctx, "schtasks", "/Delete", "/F", "/TN", taskName).CombinedOutput(); err != nil {
			return fmt.Errorf("schtasks: %w: %s", err, strings.TrimSpace(string(b)))
		}
		fmt.Fprintln(out, "Tarefa agendada removida.")
		return nil
	}
	return fmt.Errorf("subcomando schedule desconhecido %q\n%s", args[0], uso)
}

// byFlagValue extrai o valor de --by de uma lista de argumentos ("" se ausente).
func byFlagValue(args []string) string {
	for i, a := range args {
		name, val, hasVal := strings.Cut(a, "=")
		if name == "--by" || name == "-by" {
			if hasVal {
				return val
			}
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}
