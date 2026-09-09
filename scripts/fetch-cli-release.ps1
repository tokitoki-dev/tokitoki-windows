# CI: fetch the PINNED CLI release from GitHub, verify its digest, and gzip
# it into embedded/ so the subsequent cargo build embeds it.
#
# The pin (tag + per-arch sha256) lives in scripts/cli-release-pins.ps1 and
# is the only trusted input; the download is rejected unless its digest
# matches exactly. Short-circuits when the existing payload already matches.

[CmdletBinding()]
param(
    [ValidateSet("amd64", "arm64")] [string]$Arch = "amd64"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot "cli-release-pins.ps1")
. (Join-Path $PSScriptRoot "common.ps1")

if ($TokitokiCliTag -notmatch '^v\d+\.\d+\.\d+$') {
    throw "invalid CLI release pin tag: '$TokitokiCliTag'"
}
$pinnedSha = $TokitokiCliSha256[$Arch]
if ($pinnedSha -notmatch '^[0-9a-f]{64}$') {
    throw "invalid sha256 pin for ${Arch}: '$pinnedSha'"
}
$version = $TokitokiCliTag.TrimStart("v")

$embedded = Join-Path $root "embedded"
New-Item -ItemType Directory -Force $embedded | Out-Null
$gzPath = Join-Path $embedded "tokitoki.exe.gz"
$versionPath = Join-Path $embedded "VERSION"

if ((Test-Path $gzPath) -and (Test-Path $versionPath) -and
    ((Get-Content $versionPath -Raw) -eq $version) -and
    ((Get-DecompressedSha256 $gzPath) -eq $pinnedSha)) {
    Write-Host "fetch: payload already matches pin $TokitokiCliTag ($Arch); nothing to do"
    exit 0
}

$url = "https://github.com/tokitoki-dev/tokitoki-cli/releases/download/$TokitokiCliTag/tokitoki-windows-$Arch.exe"
$staging = Join-Path $embedded ".tokitoki-fetch.exe"
Write-Host "fetch: downloading $url"
try {
    Invoke-WebRequest -Uri $url -OutFile $staging -MaximumRedirection 5

    $actualSha = (Get-FileHash $staging -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualSha -ne $pinnedSha) {
        throw "sha256 mismatch for ${Arch}: expected $pinnedSha, got $actualSha"
    }

    Write-Host "fetch: digest verified; bundling payload into embedded/"
    Compress-GzipFile -Source $staging -Destination $gzPath
    Write-CliVersionFile -Path $versionPath -Version $version
} finally {
    Remove-Item $staging -ErrorAction SilentlyContinue
}
Write-Host "fetch: done -> embedded/tokitoki.exe.gz (CLI $version, $Arch)"
