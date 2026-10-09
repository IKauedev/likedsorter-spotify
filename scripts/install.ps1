<#
.SYNOPSIS
  Instala o likedsorter no Windows (por usuário, sem precisar de administrador).

.DESCRIPTION
  - Copia likedsorter.exe para %LOCALAPPDATA%\Programs\likedsorter
  - Adiciona essa pasta ao PATH do usuário
  - Cria %APPDATA%\likedsorter\.env a partir do .env.example (se ainda não existir)

.PARAMETER Exe
  Caminho do executável. Padrão: ..\bin\likedsorter.exe, ou likedsorter.exe ao lado do script.

.PARAMETER Build
  Compila a partir do código-fonte (exige Go 1.22+) antes de instalar.

.PARAMETER Uninstall
  Remove o executável e a entrada do PATH. Não apaga configuração, token nem cache.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Build
#>
[CmdletBinding()]
param(
    [string]$Exe,
    [switch]$Build,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

$root       = Split-Path -Parent $PSScriptRoot
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\likedsorter'
$configDir  = Join-Path $env:APPDATA 'likedsorter'

function Remove-FromUserPath([string]$dir) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $current) { return }
    $parts = $current -split ';' | Where-Object { $_ -and ($_.TrimEnd('\') -ne $dir.TrimEnd('\')) }
    [Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
}

if ($Uninstall) {
    Remove-FromUserPath $installDir
    if (Test-Path $installDir) { Remove-Item -Recurse -Force $installDir }
    Write-Host "Removido: $installDir"
    Write-Host "Configuração e token preservados em: $configDir"
    Write-Host "Para apagar tudo: Remove-Item -Recurse `"$configDir`", `"$env:LOCALAPPDATA\likedsorter`""
    return
}

if ($Build) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw 'Go não encontrado. Instale Go 1.22+ (https://go.dev/dl/) ou use um .exe já compilado.'
    }
    $Exe = Join-Path $root 'bin\likedsorter.exe'
    Push-Location $root
    try {
        $ver = (git describe --tags --always --dirty 2>$null)
        if (-not $ver) { $ver = 'dev' }
        go build -trimpath -ldflags "-s -w -X main.version=$ver" -o $Exe ./cmd/likedsorter
        if ($LASTEXITCODE -ne 0) { throw 'Falha na compilação.' }
    } finally { Pop-Location }
}

if (-not $Exe) {
    foreach ($c in @((Join-Path $root 'bin\likedsorter.exe'), (Join-Path $PSScriptRoot 'likedsorter.exe'), (Join-Path $root 'likedsorter.exe'))) {
        if (Test-Path $c) { $Exe = $c; break }
    }
}
if (-not $Exe -or -not (Test-Path $Exe)) {
    throw 'likedsorter.exe não encontrado. Use -Build ou informe -Exe <caminho>.'
}

New-Item -ItemType Directory -Force $installDir | Out-Null
Copy-Item -Force $Exe (Join-Path $installDir 'likedsorter.exe')

# PATH do usuário (sem duplicar)
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries  = if ($userPath) { $userPath -split ';' } else { @() }
if (-not ($entries | Where-Object { $_.TrimEnd('\') -eq $installDir })) {
    $novo = @($entries | Where-Object { $_ }) + $installDir
    [Environment]::SetEnvironmentVariable('Path', ($novo -join ';'), 'User')
    Write-Host "Adicionado ao PATH do usuário: $installDir"
}

# Modelo de credenciais
New-Item -ItemType Directory -Force $configDir | Out-Null
$envFile = Join-Path $configDir '.env'
$example = Join-Path $root '.env.example'
if (-not (Test-Path $envFile) -and (Test-Path $example)) {
    Copy-Item $example $envFile
    Write-Host "Modelo de credenciais criado: $envFile"
}

Write-Host ''
Write-Host "Instalado em $installDir" -ForegroundColor Green
Write-Host 'Próximos passos:'
Write-Host "  1. Edite $envFile e preencha SPOTIFY_CLIENT_ID"
Write-Host '  2. Abra um NOVO terminal e rode:  likedsorter auth login'
Write-Host '  3. Depois:                        likedsorter   (menu interativo)'
