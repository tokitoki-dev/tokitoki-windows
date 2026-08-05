# Fetches the pinned tokitoki CLI release asset for one architecture,
# verifies it, and places it where go:embed bakes it into the app:
# internal/agentcli/embedded/tokitoki.exe (+ VERSION). A file already in
# place that matches the pinned digest is left alone, so local rebuilds and
# CI cache hits cost no download.

[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet("amd64", "arm64")]
    [string]$Arch,

    [string]$Go = "go"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ScriptDir = $PSScriptRoot
$Root = Split-Path -Parent $ScriptDir
. (Join-Path $ScriptDir "cli-release-pins.ps1")

if ($TokitokiCliTag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') {
    throw "invalid pinned CLI tag: $TokitokiCliTag"
}
$expectedSha = $TokitokiCliSha256[$Arch]
if ($expectedSha -notmatch '^[0-9a-f]{64}$') {
    throw "invalid pinned CLI SHA-256 for ${Arch}: $expectedSha"
}
$expectedVersion = $TokitokiCliTag.TrimStart("v")

$EmbedDir = Join-Path $Root "internal/agentcli/embedded"
$Binary = Join-Path $EmbedDir "tokitoki.exe"
$VersionFile = Join-Path $EmbedDir "VERSION"

function Get-Sha256([string]$Path) {
    (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
}

if ((Test-Path -LiteralPath $Binary -PathType Leaf) -and
    (Get-Sha256 $Binary) -eq $expectedSha) {
    Set-Content -LiteralPath $VersionFile -Value $expectedVersion -NoNewline
    Write-Host "embedded CLI already matches $TokitokiCliTag ($Arch)"
    exit 0
}

$asset = "tokitoki-windows-$Arch.exe"
$url = "https://github.com/tokitoki-dev/tokitoki-cli/releases/download/$TokitokiCliTag/$asset"
# Staging keeps the .exe extension: Windows refuses to execute anything else,
# and the version self-check below runs this very file.
$staging = Join-Path $EmbedDir ".tokitoki-fetch.exe"

New-Item -ItemType Directory -Force -Path $EmbedDir | Out-Null
Remove-Item -LiteralPath $staging -Force -ErrorAction SilentlyContinue

Write-Host "fetching $asset ($TokitokiCliTag)"
Invoke-WebRequest -Uri $url -OutFile $staging -MaximumRedirection 5

$actualSha = Get-Sha256 $staging
if ($actualSha -ne $expectedSha) {
    Remove-Item -LiteralPath $staging -Force
    throw "$asset digest $actualSha does not match pinned $expectedSha"
}

# The digest already proves the bytes; these two checks prove the pins
# themselves point at what they claim. `go version -m` reads the build stamp
# for any arch; running the binary only works for the host's.
$info = (& $Go version -m $staging) -join "`n"
if ($LASTEXITCODE -ne 0 -or $info -notmatch "GOARCH=$Arch") {
    Remove-Item -LiteralPath $staging -Force
    throw "$asset is not a $Arch Windows binary"
}
$hostArch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
if ($Arch -eq $hostArch) {
    $reported = "$(& $staging version)".Trim()
    if ($LASTEXITCODE -ne 0 -or $reported -ne $expectedVersion) {
        Remove-Item -LiteralPath $staging -Force
        throw "$asset reports version '$reported', pinned tag is '$expectedVersion'"
    }
}

Move-Item -LiteralPath $staging -Destination $Binary -Force
Set-Content -LiteralPath $VersionFile -Value $expectedVersion -NoNewline
Write-Host "embedded CLI $TokitokiCliTag ($Arch) ready"
