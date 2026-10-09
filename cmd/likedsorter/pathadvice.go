package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// pathHas informa se dir está no PATH DESTE processo (que pode ser mais antigo que o do sistema).
func pathHas(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(dir)) {
			return true
		}
	}
	return false
}

// printPathAdvice explica como usar o comando AGORA. Terminais já abertos (e os abertos de dentro
// deles, como no VS Code) não enxergam o PATH novo; só terminais iniciados depois pelo Windows enxergam.
func printPathAdvice(out io.Writer, dir, exe string) {
	if pathHas(dir) {
		return
	}
	fmt.Fprintln(out, "\nEste terminal ainda não enxerga o novo PATH (terminais abertos antes da instalação, ou abertos de dentro de")
	fmt.Fprintln(out, "outro programa como o VS Code, mantêm o PATH antigo). Para usar \"likedsorter\" AGORA neste terminal:")
	if runtime.GOOS == "windows" {
		fmt.Fprintf(out, "  PowerShell:  $env:Path += \";%s\"\n", dir)
		fmt.Fprintf(out, "  cmd:         set PATH=%%PATH%%;%s\n", dir)
		fmt.Fprintln(out, "Em definitivo: abra um terminal novo pelo menu Iniciar (ou reinicie o VS Code / faça logoff).")
	} else {
		fmt.Fprintf(out, "  export PATH=\"%s:$PATH\"\n", dir)
	}
	fmt.Fprintf(out, "Ou chame pelo caminho completo: %s\n", exe)
}
