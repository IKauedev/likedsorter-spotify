package main

import (
	"fmt"
	"strings"
)

// progressBar desenha "Rótulo [########------------]  42% (840/2000)" (uma linha, usando retorno de carro).
func progressBar(label string, done, total int) string {
	const width = 24
	if total <= 0 {
		return fmt.Sprintf("%s: %d", label, done)
	}
	done = min(max(done, 0), total)
	filled := done * width / total
	return fmt.Sprintf("%s [%s%s] %3d%% (%d/%d)   ", label,
		strings.Repeat("#", filled), strings.Repeat("-", width-filled), done*100/total, done, total)
}
