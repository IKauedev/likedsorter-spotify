package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// completionTree: comando → subcomandos/flags mais usados (para autocompletar).
var completionTree = map[string][]string{
	"menu": nil, "start": nil, "install": {"--dir", "--no-path", "--uninstall"},
	"setup":  {"--client-id", "--redirect-uri", "--lastfm-key", "--discogs-token", "--ai-provider", "--ai-key", "--ai-model", "--ai-base-url", "--contact", "--show", "--unset"},
	"doctor": {"--offline", "--json"}, "update": {"--check", "--yes", "--repo"},
	"auth": {"login", "status", "logout"}, "sync": {"--full-sync", "--refresh", "--no-cache", "--no-enrich", "--genre-source"},
	"stats": {"--format", "--since", "--filter-artist", "--filter-genre"}, "groups": {"--by", "--min-size", "--examples", "--dump-macro-map"},
	"plan":  {"--by", "--min-size", "--format", "--out", "--detail", "--skip-existing", "--rules", "--split-over", "--split-by", "--name-template"},
	"apply": {"--by", "--min-size", "--mode", "--public", "--private", "--allow-remove", "--yes", "--dry-run", "--json", "--max-playlists", "--rules", "--split-over", "--split-by"},
	"auto":  {"--by", "--min-size", "--log-file", "--public"}, "schedule": {"install", "status", "remove"},
	"playlist": {"list", "add", "search"}, "genre": {"list", "set", "unset", "missing"}, "rules": {"init", "path", "check"}, "suggest": {"--by", "--min-size", "--top", "--min-count", "--emit-rules", "--json"},
	"dedupe": {"--format", "--out", "--to-playlist"}, "export": {"--liked", "--out"}, "restore": {"--file"},
	"config": {"show", "get", "set", "unset", "path", "init"}, "cache": {"path", "info", "clear"}, "ai": {"setup", "test", "classify", "rules", "genres"}, "top": {"tracks", "artists", "--range", "--limit", "--to-playlist", "--json"}, "history": {"sync", "stats", "import", "path", "clear"}, "completion": {"powershell", "bash", "zsh"}, "version": nil, "help": nil,
}

var byValues = []string{"artist", "genre", "macro-genre", "decade", "year", "added-period", "language", "rules", "listening"}

func sortedCommands() []string {
	cmds := make([]string, 0, len(completionTree))
	for c := range completionTree {
		cmds = append(cmds, c)
	}
	sort.Strings(cmds)
	return cmds
}

func runCompletion(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("uso: likedsorter completion powershell|bash|zsh\n  PowerShell: likedsorter completion powershell | Out-String | Invoke-Expression   (ou adicione ao $PROFILE)\n  bash:       source <(likedsorter completion bash)\n  zsh:        source <(likedsorter completion zsh)")
	}
	cmds := sortedCommands()
	switch args[0] {
	case "powershell":
		var b strings.Builder
		b.WriteString("Register-ArgumentCompleter -Native -CommandName likedsorter -ScriptBlock {\n  param($wordToComplete, $commandAst, $cursorPosition)\n")
		b.WriteString("  $tokens = $commandAst.CommandElements | ForEach-Object { $_.ToString() }\n")
		b.WriteString("  $tree = @{\n")
		for _, c := range cmds {
			fmt.Fprintf(&b, "    '%s' = @(%s)\n", c, psList(completionTree[c]))
		}
		b.WriteString("  }\n")
		fmt.Fprintf(&b, "  $by = @(%s)\n", psList(byValues))
		b.WriteString(`  $opts = if ($tokens.Count -le 1 -or ($tokens.Count -eq 2 -and $wordToComplete)) { @($tree.Keys | Sort-Object) }
          elseif ($tokens[-1] -eq '--by' -or ($tokens.Count -ge 2 -and $tokens[-2] -eq '--by')) { $by }
          elseif ($tree.ContainsKey($tokens[1])) { $tree[$tokens[1]] } else { @() }
  $opts | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }
}
`)
		_, err := io.WriteString(out, b.String())
		return err

	case "bash":
		var b strings.Builder
		b.WriteString("_likedsorter() {\n  local cur prev cmd\n  cur=\"${COMP_WORDS[COMP_CWORD]}\"; prev=\"${COMP_WORDS[COMP_CWORD-1]}\"; cmd=\"${COMP_WORDS[1]}\"\n")
		fmt.Fprintf(&b, "  if [ \"$prev\" = \"--by\" ]; then COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ); return; fi\n", strings.Join(byValues, " "))
		fmt.Fprintf(&b, "  if [ \"$COMP_CWORD\" -eq 1 ]; then COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ); return; fi\n  case \"$cmd\" in\n", strings.Join(cmds, " "))
		for _, c := range cmds {
			if len(completionTree[c]) > 0 {
				fmt.Fprintf(&b, "    %s) COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") );;\n", c, strings.Join(completionTree[c], " "))
			}
		}
		b.WriteString("  esac\n}\ncomplete -F _likedsorter likedsorter\n")
		_, err := io.WriteString(out, b.String())
		return err

	case "zsh":
		// zsh entende o script bash via bashcompinit.
		fmt.Fprintln(out, "autoload -U +X bashcompinit && bashcompinit")
		return runCompletion([]string{"bash"}, out)
	}
	return fmt.Errorf("shell desconhecido %q (use powershell, bash ou zsh)", args[0])
}

func psList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "'" + s + "'"
	}
	return strings.Join(q, ",")
}
