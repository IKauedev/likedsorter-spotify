package main

import (
	"fmt"
	"strings"
)

// glyphs são letras em bloco de 5 linhas por 5 colunas.
var glyphs = map[rune][5]string{
	'L': {"█    ", "█    ", "█    ", "█    ", "█████"},
	'I': {"█████", "  █  ", "  █  ", "  █  ", "█████"},
	'K': {"█   █", "█  █ ", "███  ", "█  █ ", "█   █"},
	'E': {"█████", "█    ", "████ ", "█    ", "█████"},
	'D': {"████ ", "█   █", "█   █", "█   █", "████ "},
	'S': {" ████", "█    ", " ███ ", "    █", "████ "},
	'O': {" ███ ", "█   █", "█   █", "█   █", " ███ "},
	'R': {"████ ", "█   █", "████ ", "█  █ ", "█   █"},
	'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
}

// bannerRows desenha o texto (só as letras de glyphs; outras viram espaço).
func bannerRows(text string) [5]string {
	var rows [5]string
	for _, r := range strings.ToUpper(text) {
		g, ok := glyphs[r]
		if !ok {
			g = [5]string{"     ", "     ", "     ", "     ", "     "}
		}
		for i := range rows {
			rows[i] += g[i] + " "
		}
	}
	return rows
}

// blueShades vai do azul claro ao azul profundo (cores 256 do xterm), uma por linha.
var blueShades = [5]int{159, 117, 75, 33, 27}

// banner devolve a tela de abertura; com color=false sai em texto puro.
func banner(color bool) string {
	var b strings.Builder
	b.WriteString("\n")
	for i, row := range bannerRows("LIKEDSORTER") {
		if color {
			fmt.Fprintf(&b, "  \x1b[1;38;5;%dm%s\x1b[0m\n", blueShades[i], row)
		} else {
			b.WriteString("  " + row + "\n")
		}
	}
	sub := "organizador de curtidas do Spotify · " + version
	if color {
		sub = "\x1b[38;5;75m" + sub + "\x1b[0m"
	}
	b.WriteString("\n  " + sub + "\n")
	return b.String()
}
