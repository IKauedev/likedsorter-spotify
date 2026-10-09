#!/usr/bin/env bash
# Falha se a cobertura de algum pacote crítico ficar abaixo do mínimo.
# Uso: scripts/check-coverage.sh [mínimo%]   (padrão: 70)
set -euo pipefail

min="${1:-70}"
pkgs=(grouping planner cache)
status=0

for p in "${pkgs[@]}"; do
  out="$(go test -count=1 -cover "./internal/${p}" 2>&1)" || { echo "$out"; exit 1; }
  pct="$(grep -oE 'coverage: [0-9]+(\.[0-9]+)?%' <<<"$out" | grep -oE '[0-9]+(\.[0-9]+)?' | head -n1)"
  if [ -z "$pct" ]; then
    echo "não consegui ler a cobertura de internal/${p}:"; echo "$out"; exit 1
  fi
  if awk -v c="$pct" -v m="$min" 'BEGIN { exit !(c+0 < m+0) }'; then
    printf 'FALHOU  internal/%-9s %6s%%  (mínimo %s%%)\n' "$p" "$pct" "$min"
    status=1
  else
    printf 'ok      internal/%-9s %6s%%  (mínimo %s%%)\n' "$p" "$pct" "$min"
  fi
done
exit $status
