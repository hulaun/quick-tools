<#
.SYNOPSIS
    Builds quick-tools.

.DESCRIPTION
    Produces quicktools.exe in the repository root, beside scripts/ and
    snippets/ -- the same layout a release ships in. Building into a subfolder
    would make the app read a different copy of scripts/ than the one you edit.

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
if ($Dev) {
    go build -o quicktools.exe ./cmd/quicktools
    Write-Host "built quicktools.exe (dev: console attached)"
} else {
    go build -trimpath -ldflags "-H windowsgui -s -w" -o quicktools.exe ./cmd/quicktools
    Write-Host "built quicktools.exe (release: no console)"
}

$size = (Get-Item quicktools.exe).Length / 1MB
Write-Host ("size: {0:N1} MB" -f $size)
