# Local build driver (invoked by the Makefile).
#
#   build (default): compile the sibling ../tokitoki-cli from source, gzip it
#                    into embedded/, then `cargo build --release` so the app
#                    binary embeds the payload.
#   clean:           `cargo clean` plus the fetched/bundled CLI payload.
#
# The bundled CLI is stamped with the pinned release version by default
# (scripts/cli-release-pins.ps1) so the app's never-downgrade seeding logic
# sees a comparable version; override with -CliVersion for local experiments.
#
# -Version stamps the APP's own version (TOKITOKI_VERSION): without it the
# binary reports "dev" and refuses self-updates. Release builds must pass it;
# local builds normally leave it empty on purpose.

[CmdletBinding()]
param(
    [ValidateSet("build", "clean")] [string]$Task = "build",
    [ValidateSet("amd64", "arm64")] [string]$Arch = "amd64",
    [string]$CliVersion = "",
    [string]$Version = "",
    [string]$Commit = "",
    [string]$BuildDate = ""
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot "cli-release-pins.ps1")
. (Join-Path $PSScriptRoot "common.ps1")

$embedded = Join-Path $root "embedded"
$gzPath = Join-Path $embedded "tokitoki.exe.gz"
$versionPath = Join-Path $embedded "VERSION"

# cargo from PATH, falling back to the default rustup location.
$cargoCmd = Get-Command cargo -ErrorAction SilentlyContinue
$cargo = if ($cargoCmd) { $cargoCmd.Source } else { Join-Path $env:USERPROFILE ".cargo\bin\cargo.exe" }
if (-not (Test-Path $cargo)) { throw "cargo not found on PATH or in ~\.cargo\bin" }

function Invoke-Clean {
    Push-Location $root
    try { & $cargo clean } finally { Pop-Location }
    Remove-Item -Force -ErrorAction SilentlyContinue $gzPath, $versionPath,
        (Join-Path $embedded "tokitoki.exe"), (Join-Path $embedded ".tokitoki-local.exe")
    Write-Host "clean: done"
}

function Invoke-Build {
    $cliSource = Join-Path (Split-Path -Parent $root) "tokitoki-cli"
    if (-not (Test-Path (Join-Path $cliSource "go.mod"))) {
        throw "tokitoki-cli source not found at $cliSource"
    }

    $version = if ($CliVersion) { $CliVersion } else { $TokitokiCliTag.TrimStart("v") }
    if ($version -notmatch '^\d+\.\d+\.\d+$') {
        throw "CLI version must be x.y.z (got '$version'); the app refuses to seed unparsable versions"
    }

    New-Item -ItemType Directory -Force $embedded | Out-Null
    $staging = Join-Path $embedded ".tokitoki-local.exe"

    Write-Host "build: compiling local CLI $version ($Arch) from $cliSource"
    $versionVar = "github.com/tokitoki-dev/tokitoki-cli/internal/buildinfo.Version"
    $env:CGO_ENABLED = "0"; $env:GOOS = "windows"; $env:GOARCH = $Arch
    try {
        go build -C $cliSource -trimpath -buildvcs=false `
            -ldflags "-s -w -X $versionVar=$version" `
            -o $staging ./cmd/tokitoki
        if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
    } finally {
        Remove-Item Env:\CGO_ENABLED, Env:\GOOS, Env:\GOARCH -ErrorAction SilentlyContinue
    }

    Write-Host "build: bundling CLI payload into embedded/"
    Compress-GzipFile -Source $staging -Destination $gzPath
    Write-CliVersionFile -Path $versionPath -Version $version
    Remove-Item $staging

    Write-Host "build: cargo build --release"
    Push-Location $root
    try {
        if ($Version) {
            if ($Version -notmatch '^\d+\.\d+\.\d+$') {
                throw "app version must be x.y.z (got '$Version')"
            }
            $env:TOKITOKI_VERSION = $Version
            $env:TOKITOKI_COMMIT = if ($Commit) { $Commit } else { "local" }
            $env:TOKITOKI_BUILD_DATE = if ($BuildDate) { $BuildDate } else {
                (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
            }
        }
        & $cargo build --release --locked
        if ($LASTEXITCODE -ne 0) { throw "cargo build failed ($LASTEXITCODE)" }
    } finally {
        Remove-Item Env:\TOKITOKI_VERSION, Env:\TOKITOKI_COMMIT, Env:\TOKITOKI_BUILD_DATE `
            -ErrorAction SilentlyContinue
        Pop-Location
    }
    $stamp = if ($Version) { $Version } else { "dev" }
    Write-Host "build: done -> target/release/tokitoki-windows.exe (app $stamp, embedded CLI $version)"
}

switch ($Task) {
    "build" { Invoke-Build }
    "clean" { Invoke-Clean }
}
