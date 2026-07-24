[CmdletBinding()]
param(
    [ValidateSet("build", "debug", "test", "generate", "clean", "size")]
    [string]$Task = "build",

    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",

    [string]$Version = "0.1.0",
    [string]$Commit = "local",
    [string]$BuildDate = "unknown",
    [string]$Go = "go"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$App = "tokitoki-windows"
$Pkg = "./cmd/tokitoki-windows"
$DistDir = Join-Path $Root "dist"
$Manifest = Join-Path $Root "cmd/tokitoki-windows/tokitoki-windows.exe.manifest"
$ResourcesGo = Join-Path $Root "cmd/tokitoki-windows/resources.go"
$IconSvg = Join-Path $Root "assets/app-icon.svg"
$IconIco = Join-Path $Root "assets/app-icon.ico"
$VersionPkg = "github.com/tokitoki-dev/tokitoki-windows/internal/version"

function Stop-Build {
    param(
        [string]$Message,
        [int]$Code = 1
    )

    [Console]::Error.WriteLine($Message)
    exit $Code
}

function Invoke-Checked {
    param(
        [string]$FilePath,
        [string[]]$Arguments
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        Stop-Build "$FilePath failed with exit code $LASTEXITCODE" $LASTEXITCODE
    }
}

function Ensure-Dist {
    New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
}

function Find-Magick {
    $command = Get-Command magick -ErrorAction SilentlyContinue
    if ($command) {
        return $command.Source
    }

    $candidates = @(
        "C:\Program Files\ImageMagick-7.1.2-Q16-HDRI\magick.exe",
        "C:\Program Files\ImageMagick-7.1.2-Q16\magick.exe",
        "C:\Program Files\ImageMagick-7.1.2-Q8\magick.exe"
    )
    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return $candidate
        }
    }

    Stop-Build "ImageMagick magick.exe was not found. Install ImageMagick or add magick.exe to PATH."
}

function Ensure-Icon {
    $needsGenerate = -not (Test-Path -LiteralPath $IconIco -PathType Leaf)
    if (-not $needsGenerate) {
        $iconTime = (Get-Item -LiteralPath $IconIco).LastWriteTimeUtc
        $sourceTime = (Get-Item -LiteralPath $IconSvg).LastWriteTimeUtc
        $needsGenerate = $sourceTime -gt $iconTime
    }

    if ($needsGenerate) {
        $magick = Find-Magick
        Invoke-Checked $magick @(
            "-background", "none",
            $IconSvg,
            "-define", "icon:auto-resize=256,128,64,48,32,24,16",
            $IconIco
        )
    }
}

function Get-ResourcePath {
    param([string]$TargetArch)
    Join-Path $Root "cmd/tokitoki-windows/rsrc_windows_$TargetArch.syso"
}

function Ensure-Resource {
    param([string]$TargetArch)

    $resource = Get-ResourcePath -TargetArch $TargetArch
    $needsGenerate = -not (Test-Path -LiteralPath $resource -PathType Leaf)

    if (-not $needsGenerate) {
        $resourceTime = (Get-Item -LiteralPath $resource).LastWriteTimeUtc
        foreach ($source in @($Manifest, $ResourcesGo, $IconIco)) {
            if ((Get-Item -LiteralPath $source).LastWriteTimeUtc -gt $resourceTime) {
                $needsGenerate = $true
                break
            }
        }
    }

    if ($needsGenerate) {
        Invoke-Checked $Go @(
            "run", "github.com/akavel/rsrc@latest",
            "-arch", $TargetArch,
            "-manifest", $Manifest,
            "-ico", $IconIco,
            "-o", $resource
        )
    }
}

function Get-LdFlags {
    param([switch]$Debug)

    $flags = @(
        "-X", "$VersionPkg.Version=$Version",
        "-X", "$VersionPkg.Commit=$Commit",
        "-X", "$VersionPkg.BuildDate=$BuildDate"
    )

    if (-not $Debug) {
        $flags = @("-s", "-w", "-H", "windowsgui") + $flags
    }

    $flags -join " "
}

function Build-App {
    param([switch]$Debug)

    Ensure-Resource -TargetArch $Arch
    Ensure-Dist

    $env:GOOS = "windows"
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = "0"

    if ($Debug) {
        $out = Join-Path $DistDir "$App-$Arch-debug.exe"
    } else {
        $out = Join-Path $DistDir "$App-$Arch.exe"
    }

    Invoke-Checked $Go @(
        "build",
        "-ldflags", (Get-LdFlags -Debug:$Debug),
        "-o", $out,
        $Pkg
    )

    if (-not $Debug -and $Arch -eq "amd64") {
        Install-CompatCopy -Source $out
    }
}

# The unsuffixed copy is the path the README tells people to run, so it has to
# end up holding this build. Windows refuses to overwrite it while that copy is
# running, but it does allow a rename — the same move the app's own updater
# makes — so the running file steps aside and the fresh build takes its name.
# Failing instead would leave a stale binary at the documented path.
function Install-CompatCopy {
    param([string]$Source)

    $target = Join-Path $DistDir "$App.exe"
    $stale = "$target.old"
    Remove-Item -Force -LiteralPath $stale -ErrorAction SilentlyContinue

    try {
        Copy-Item -Force -LiteralPath $Source -Destination $target -ErrorAction Stop
        return
    } catch {
        if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
            throw
        }
    }

    Rename-Item -LiteralPath $target -NewName "$App.exe.old" -ErrorAction Stop
    Copy-Item -Force -LiteralPath $Source -Destination $target
    Write-Host "note: $App.exe was in use; the running copy is now $App.exe.old"
}

function Generate-Resources {
    foreach ($targetArch in @("amd64", "arm64")) {
        $resource = Get-ResourcePath -TargetArch $targetArch
        Invoke-Checked $Go @(
            "run", "github.com/akavel/rsrc@latest",
            "-arch", $targetArch,
            "-manifest", $Manifest,
            "-ico", $IconIco,
            "-o", $resource
        )
    }
}

Push-Location $Root
try {
    switch ($Task) {
        "build" {
            Ensure-Icon
            Build-App
        }
        "debug" {
            Ensure-Icon
            Build-App -Debug
        }
        "test" {
            Invoke-Checked $Go @("test", "./...")
        }
        "generate" {
            Ensure-Icon
            Generate-Resources
        }
        "clean" {
            Remove-Item -Recurse -Force -LiteralPath $DistDir -ErrorAction SilentlyContinue
        }
        "size" {
            Ensure-Icon
            Build-App
            Get-Item -LiteralPath (Join-Path $DistDir "$App-$Arch.exe") | Select-Object FullName, Length
        }
    }
} finally {
    Pop-Location
}
