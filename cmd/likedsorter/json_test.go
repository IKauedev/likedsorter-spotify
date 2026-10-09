package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestStripJSON(t *testing.T) {
	rest, ok := stripJSON([]string{"--by=year", "--json", "-x"})
	if !ok || strings.Join(rest, " ") != "--by=year -x" {
		t.Errorf("rest=%v ok=%v", rest, ok)
	}
	if _, ok := stripJSON([]string{"a"}); ok {
		t.Error("não deveria detectar --json")
	}
}

func TestWithJSONPrintsOnlyJSONOnStdout(t *testing.T) {
	var out bytes.Buffer
	err := withJSON([]string{"--json", "x"}, &out, func(a []string, text io.Writer) (any, error) {
		if len(a) != 1 || a[0] != "x" {
			t.Errorf("args = %v", a)
		}
		return map[string]int{"n": 3}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["n"] != 3 {
		t.Errorf("stdout não é o JSON esperado: %q (%v)", out.String(), err)
	}
}

func TestWithJSONTextModeAndErrors(t *testing.T) {
	var out bytes.Buffer
	_ = withJSON(nil, &out, func(a []string, text io.Writer) (any, error) {
		text.Write([]byte("olá"))
		return nil, nil
	})
	if out.String() != "olá" {
		t.Errorf("sem --json o texto vai para a saída: %q", out.String())
	}
	out.Reset()
	boom := errors.New("boom")
	if err := withJSON([]string{"--json"}, &out, func([]string, io.Writer) (any, error) { return nil, boom }); err != boom || out.Len() != 0 {
		t.Errorf("erro deveria propagar sem JSON: err=%v out=%q", err, out.String())
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar("X", 12, 24); !strings.Contains(got, "50%") || !strings.Contains(got, "(12/24)") || strings.Count(got, "#") != 12 {
		t.Errorf("barra: %q", got)
	}
	if got := progressBar("X", 5, 0); !strings.Contains(got, "5") {
		t.Errorf("total 0: %q", got)
	}
	if got := progressBar("X", 99, 10); !strings.Contains(got, "100%") {
		t.Errorf("excedente: %q", got)
	}
}
