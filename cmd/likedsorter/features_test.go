package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	for _, sh := range []string{"powershell", "bash", "zsh"} {
		var b bytes.Buffer
		if err := runCompletion([]string{sh}, &b); err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		for _, cmd := range []string{"playlist", "doctor", "schedule"} {
			if !strings.Contains(b.String(), cmd) {
				t.Errorf("%s: faltou %q", sh, cmd)
			}
		}
	}
	if err := runCompletion([]string{"fish"}, &bytes.Buffer{}); err == nil {
		t.Error("shell desconhecido deveria falhar")
	}
}

func TestAutoRefusesRemoval(t *testing.T) {
	for _, a := range [][]string{{"--allow-remove"}, {"--mode=recreate"}, {"--mode", "recreate"}} {
		err := runAuto(context.Background(), a, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "nunca remove") {
			t.Errorf("auto %v deveria ser recusado, err=%v", a, err)
		}
	}
}

func TestHelpTopicsResolve(t *testing.T) {
	for _, topic := range []string{"instalar", "segredos", "comecar", "comandos", "playlists", "estrategias", "regras", "automatico", "config", "arquivos", "seguranca", "problemas", "tudo", "topicos"} {
		var b bytes.Buffer
		if err := runHelp([]string{topic}, &b); err != nil || b.Len() == 0 {
			t.Errorf("help %s: err=%v len=%d", topic, err, b.Len())
		}
	}
	if err := runHelp([]string{"inexistente"}, &bytes.Buffer{}); err == nil {
		t.Error("tópico inexistente deveria falhar")
	}
}

func TestUsageMentionsNewCommands(t *testing.T) {
	for _, c := range []string{"start", "doctor", "update", "playlist", "genre", "rules", "auto", "schedule", "completion"} {
		if !strings.Contains(usage, "  "+c+" ") {
			t.Errorf("usage não lista %q", c)
		}
	}
}
