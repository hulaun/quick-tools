<#
.SYNOPSIS
    Builds quick-tools.

.DESCRIPTION
    Produces bin/quicktools.exe. Relative paths in config.json resolve against
    the folder holding the exe, except that a bin folder is stepped out of (see
    config.Root) -- so the binary sits in bin/ while storage/ and scripts/ stay
    at the root, which is both the development layout and the layout a release
    ships in.

.PARAMETER Dev
    Build with a console attached, so panics and log output are visible.
    Without this the binary is linked as a GUI application and has no console.

.PARAMETER Resources
    Regenerate rsrc_windows_*.syso from winres/. Only needed after changing the
    icon or the manifest. Requires go-winres:
        go install github.com/tc-hib/go-winres@latest
#>
param(
    [switch]$Dev,
    [switch]$Resources
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if ($Resources) {
    $winres = Join-Path (go env GOPATH) "bin\go-winres.exe"
    if (-not (Test-Path $winres)) {
        throw "go-winres not found. Run: go install github.com/tc-hib/go-winres@latest"
    }
    Copy-Item "internal\ui\icon.ico" "winres\icon.ico" -Force
    & $winres make --in winres/winres.json --out cmd/quicktools/rsrc
    Write-Host "regenerated cmd/quicktools/rsrc_windows_*.syso"
}

Write-Host "vet..."
go vet ./...
# One finding is expected and documented: the unsafe.Pointer conversion in
# internal/winapi/clipboard.go, which is sound because the memory comes from
# GlobalAlloc and so lives outside the Go heap.

Write-Host "test..."
go test ./...

Write-Host "build..."
$out = "bin\quicktools.exe"
if (-not (Test-Path "bin")) { New-Item -ItemType Directory "bin" | Out-Null }
if ($Dev) {
    go build -o $out ./cmd/quicktools
    Write-Host "built $out (dev: console attached)"
} else {
    go build -trimpath -ldflags "-H windowsgui -s -w" -o $out ./cmd/quicktools
    Write-Host "built $out (release: no console)"
}

$size = (Get-Item $out).Length / 1MB
Write-Host ("size: {0:N1} MB" -f $size)
