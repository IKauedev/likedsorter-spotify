//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// O PATH do usuário fica em HKCU\Environment. É lido sem expandir variáveis (para
// preservar %VAR% existentes) e, ao final, um SetEnvironmentVariable avisa o
// Windows da mudança (broadcast) para novos terminais enxergarem o PATH.
const psPathScript = `
$ErrorActionPreference = 'Stop'
$dir = $env:LS_DIR.TrimEnd('\')
$key = Get-Item 'HKCU:\Environment'
$cur = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
$parts = @($cur -split ';' | Where-Object { $_ })
$has = [bool]($parts | Where-Object { $_.TrimEnd('\') -ieq $dir })
if ($env:LS_OP -eq 'add') {
  if ($has) { 'unchanged'; exit 0 }
  $parts += $dir
} else {
  if (-not $has) { 'unchanged'; exit 0 }
  $parts = @($parts | Where-Object { $_.TrimEnd('\') -ine $dir })
}
Set-ItemProperty -Path 'HKCU:\Environment' -Name Path -Value ($parts -join ';') -Type ExpandString
'changed'
`

func editUserPath(op, dir string) (bool, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psPathScript)
	cmd.Env = append(os.Environ(), "LS_OP="+op, "LS_DIR="+dir)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("powershell: %w: %s", err, strings.TrimSpace(string(b)))
	}
	changed := strings.TrimSpace(string(b)) == "changed" // "unchanged" também contém "changed"
	if changed {
		broadcastEnvChange()
	}
	return changed, nil
}

// broadcastEnvChange avisa o Explorer e os demais programas (WM_SETTINGCHANGE "Environment") para que
// novos terminais abertos por eles já enxerguem o PATH atualizado, sem logoff.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xFFFF
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := syscall.NewLazyDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var result uintptr
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}

func addToUserPath(dir string) (bool, string, error) {
	changed, err := editUserPath("add", dir)
	return changed, "", err
}

func removeFromUserPath(dir string) (bool, string, error) {
	changed, err := editUserPath("remove", dir)
	return changed, "", err
}
