# Build driver for tokitoki-windows-c (invoked by the Makefile).
#
#   build (default): compile ../tokitoki-cli, gzip it into embedded/, then a
#                    size-optimized release exe (payload embedded via app.rc).
#   debug:           console-subsystem exe with symbols, no CLI bundling.
#   test:            compile and run the unit-test exe (core sources + tests).
#   clean:           remove build output and the CLI payload.
#
# MSVC only: locates VsDevCmd.bat, generates a response file, and runs cl/rc
# inside that environment. Zero third-party libraries — everything links
# against inbox Windows DLLs.

[CmdletBinding()]
param(
    [ValidateSet("build", "debug", "test", "clean")] [string]$Task = "build",
    [ValidateSet("amd64", "arm64")] [string]$Arch = "amd64",
    [string]$CliVersion = "",
    [string]$Version = "dev",
    # CI/release bundle the pinned CLI via fetch-cli-release.ps1 first, so the
    # `build` task must not also rebuild the CLI from ../tokitoki-cli source
    # (which is absent on the runner). Local `make build` leaves this off and
    # compiles the sibling CLI as usual.
    [switch]$SkipCliBundle
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot "cli-release-pins.ps1")
. (Join-Path $PSScriptRoot "common.ps1")

$buildDir = Join-Path $root "build"
$embedded = Join-Path $root "embedded"
$gzPath = Join-Path $embedded "tokitoki.exe.gz"
$versionPath = Join-Path $embedded "VERSION"

# Core sources: everything unit-testable (no UI, no wWinMain).
$coreSources = @(
    "src\util\wstr.c", "src\util\buf.c", "src\util\json.c", "src\util\inflate.c",
    "src\util\sha256.c", "src\util\http.c",
    "src\version.c", "src\settings.c", "src\data_dirs.c", "src\agent_cli.c",
    "src\app_update.c", "src\syncer.c", "src\watcher.c",
    "src\instance.c", "src\launch.c", "src\logo.c", "src\app.c"
)
$uiSources = @(
    "src\ui\tray.c", "src\ui\theme.c", "src\ui\task_dialog.c",
    "src\ui\settings_dialog.c", "src\ui\updater.c", "src\ui\ui.c"
)
$testSources = Get-ChildItem (Join-Path $root "tests") -Filter "*.c" |
    ForEach-Object { "tests\$($_.Name)" }

$libs = @(
    "kernel32.lib", "user32.lib", "gdi32.lib", "shell32.lib", "advapi32.lib",
    "comctl32.lib", "winhttp.lib", "bcrypt.lib", "ole32.lib", "oleaut32.lib",
    "uxtheme.lib", "dwmapi.lib"
)

function Find-VsDevCmd {
    $candidates = Get-ChildItem "C:\Program Files\Microsoft Visual Studio\*\*\Common7\Tools\VsDevCmd.bat" -ErrorAction SilentlyContinue
    if (-not $candidates) { throw "VsDevCmd.bat not found; install VS Build Tools" }
    $candidates[0].FullName
}

# Runs one command line inside the VsDevCmd environment. Output is captured
# and echoed here (not returned) so callers can use a function's return value
# without the tool's stdout leaking into it.
function Invoke-Msvc {
    param([Parameter(Mandatory)] [string]$CommandLine)
    $devCmd = Find-VsDevCmd
    $vsArch = if ($Arch -eq "arm64") { "arm64" } else { "amd64" }
    # VsDevCmd writes a benign vswhere warning to stderr; capture stdout+stderr
    # without letting the Stop preference promote that write to a terminating
    # error. Merging inside cmd keeps it off PowerShell's error stream.
    $output = cmd /c "`"$devCmd`" -arch=$vsArch -no_logo 2>&1 && $CommandLine 2>&1"
    $output | ForEach-Object { Write-Host $_ }
    if ($LASTEXITCODE -ne 0) { throw "command failed ($LASTEXITCODE): $CommandLine" }
}

function Compile-Resources {
    param([bool]$EmbedCli)
    $res = Join-Path $buildDir "app.res"
    $flag = if ($EmbedCli) { "/dEMBED_CLI" } else { "" }
    # Stamp VERSIONINFO from the same version the C code gets, so the exe's
    # file properties match version_utf8(). A generated header avoids fragile
    # comma/quote escaping through rc's /d flag. "dev" -> 0.0.0.0 fields.
    $header = Join-Path $buildDir "version_res.h"
    if ($Version -match '^(\d+)\.(\d+)\.(\d+)$') {
        Set-Content $header -Encoding ascii @"
#define TOKITOKI_VER_FIELDS $($Matches[1]), $($Matches[2]), $($Matches[3]), 0
#define TOKITOKI_VERSION_STR "$Version"
"@
    } else {
        Set-Content $header -Encoding ascii @"
#define TOKITOKI_VER_FIELDS 0, 0, 0, 0
#define TOKITOKI_VERSION_STR "$Version"
"@
    }
    Invoke-Msvc "rc /nologo $flag /I`"$buildDir`" /fo `"$res`" `"$root\res\app.rc`""
    $res
}

# Writes a cl response file (quoting survives; cmd escaping does not).
function Write-ResponseFile {
    param([string]$Path, [string[]]$Lines)
    Set-Content -Path $Path -Value ($Lines -join "`r`n") -Encoding ascii
}

$commonFlags = @(
    "/nologo", "/std:c17", "/utf-8", "/W4", "/WX", "/permissive-",
    "/DUNICODE", "/D_UNICODE", "/DWIN32_LEAN_AND_MEAN", "/D_CRT_SECURE_NO_WARNINGS",
    "/DTOKITOKI_VERSION=\`"$Version\`"",
    "/I`"$root\src`"", "/I`"$root\res`""
)

function Invoke-Build {
    param([bool]$Release)

    New-Item -ItemType Directory -Force $buildDir | Out-Null
    $embed = (Test-Path $gzPath) -and (Test-Path $versionPath)
    $res = Compile-Resources -EmbedCli $embed

    $out = Join-Path $buildDir "tokitoki-windows.exe"
    $objDir = Join-Path $buildDir "obj"
    New-Item -ItemType Directory -Force $objDir | Out-Null

    $flags = if ($Release) {
        @("/O1", "/GL", "/MT", "/DNDEBUG")
    } else {
        @("/Od", "/Zi", "/MTd", "/DTOKITOKI_DEBUG")
    }
    $subsystem = if ($Release) { "/SUBSYSTEM:WINDOWS" } else { "/SUBSYSTEM:CONSOLE /ENTRY:wWinMainCRTStartup /DEBUG" }
    $ltcg = if ($Release) { "/LTCG /OPT:REF /OPT:ICF" } else { "" }

    # Response files must not contain /link — linker options go on the
    # command line itself.
    $sources = ($coreSources + $uiSources + @("src\main.c")) |
        ForEach-Object { "`"$root\$_`"" }
    $rsp = Join-Path $buildDir "app.rsp"
    Write-ResponseFile $rsp ($commonFlags + $flags + $sources + @(
            "`"$res`"", "/Fo`"$objDir`"\\", "/Fe`"$out`""))
    Invoke-Msvc "cl @`"$rsp`" /link $subsystem $ltcg $($libs -join ' ')"
    Write-Host "build: done -> $out"
}

function Invoke-Test {
    New-Item -ItemType Directory -Force $buildDir | Out-Null
    $objDir = Join-Path $buildDir "obj-test"
    New-Item -ItemType Directory -Force $objDir | Out-Null
    $out = Join-Path $buildDir "tokitoki-tests.exe"

    $sources = ($coreSources + $testSources) | ForEach-Object { "`"$root\$_`"" }
    $rsp = Join-Path $buildDir "test.rsp"
    Write-ResponseFile $rsp ($commonFlags + @("/Od", "/Zi", "/MTd") + $sources + @(
            "/I`"$root\tests`"", "/Fo`"$objDir`"\\", "/Fe`"$out`""))
    Invoke-Msvc "cl @`"$rsp`" /link /SUBSYSTEM:CONSOLE /DEBUG $($libs -join ' ')"

    Push-Location $root
    try {
        & $out
        if ($LASTEXITCODE -ne 0) { throw "tests failed ($LASTEXITCODE)" }
    } finally { Pop-Location }
}

function Invoke-BundleLocalCli {
    $cliSource = Join-Path (Split-Path -Parent $root) "tokitoki-cli"
    if (-not (Test-Path (Join-Path $cliSource "go.mod"))) {
        throw "tokitoki-cli source not found at $cliSource"
    }
    $cliVer = if ($CliVersion) { $CliVersion } else { $TokitokiCliTag.TrimStart("v") }
    if ($cliVer -notmatch '^\d+\.\d+\.\d+$') {
        throw "CLI version must be x.y.z (got '$cliVer')"
    }
    New-Item -ItemType Directory -Force $embedded | Out-Null
    $staging = Join-Path $embedded ".tokitoki-local.exe"
    Write-Host "build: compiling local CLI $cliVer ($Arch) from $cliSource"
    $versionVar = "github.com/tokitoki-dev/tokitoki-cli/internal/buildinfo.Version"
    $env:CGO_ENABLED = "0"; $env:GOOS = "windows"; $env:GOARCH = $Arch
    try {
        go build -C $cliSource -trimpath -buildvcs=false `
            -ldflags "-s -w -X $versionVar=$cliVer" `
            -o $staging ./cmd/tokitoki
        if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
    } finally {
        Remove-Item Env:\CGO_ENABLED, Env:\GOOS, Env:\GOARCH -ErrorAction SilentlyContinue
    }
    Compress-GzipFile -Source $staging -Destination $gzPath
    Write-CliVersionFile -Path $versionPath -Version $cliVer
    Remove-Item $staging
}

switch ($Task) {
    "build" {
        if (-not $SkipCliBundle) {
            Invoke-BundleLocalCli
        }
        Invoke-Build -Release $true
    }
    "debug" {
        Invoke-Build -Release $false
    }
    "test" {
        Invoke-Test
    }
    "clean" {
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $buildDir
        Remove-Item -Force -ErrorAction SilentlyContinue $gzPath, $versionPath,
            (Join-Path $embedded "tokitoki.exe"), (Join-Path $embedded ".tokitoki-local.exe")
        Write-Host "clean: done"
    }
}
