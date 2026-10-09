package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSplitLine(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"plan --by=year", []string{"plan", "--by=year"}},
		{`plan --name-template "Meu som • {group}" -v`, []string{"plan", "--name-template", "Meu som • {group}", "-v"}},
		{`a '' b`, []string{"a", "", "b"}},
	}
	for _, c := range cases {
		got, err := splitLine(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitLine(%q) = %q, %v; quer %q", c.in, got, err, c.want)
		}
	}
	if _, err := splitLine(`a "b`); err == nil {
		t.Error("esperava erro para aspas não fechadas")
	}
}

func TestJoinArgs(t *testing.T) {
	if got := joinArgs([]string{"plan", "--name-template", "a b"}); got != `plan --name-template "a b"` {
		t.Errorf("got %q", got)
	}
}

func TestMenuSaiEAjuda(t *testing.T) {
	var out strings.Builder
	// opção inválida, ajuda (+Enter para voltar) e sair
	in := strings.NewReader("zzz\nh\n\n0\n")
	if err := runMenu(context.Background(), in, &out, globals{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Opção inválida", "Comandos:", "Até logo!"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("saída sem %q:\n%s", want, out.String())
		}
	}
}

func TestMenuFimDaEntrada(t *testing.T) {
	var out strings.Builder
	if err := runMenu(context.Background(), strings.NewReader(""), &out, globals{}); err != nil {
		t.Fatal(err)
	}
}

func TestMenuComandoLivre(t *testing.T) {
	var out strings.Builder
	in := strings.NewReader("c\nversion\n\n0\n")
	if err := runMenu(context.Background(), in, &out, globals{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "likedsorter version") {
		t.Errorf("saída inesperada:\n%s", out.String())
	}
}
