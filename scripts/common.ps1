# Shared helpers for the build/fetch scripts. Dot-source, don't run.

# Gzips $Source to $Destination at maximum compression (the payload ships
# inside the app binary, so size beats speed here). SmallestSize needs .NET 6+
# (pwsh); Windows PowerShell 5.1 falls back to Optimal.
function Compress-GzipFile {
    param(
        [Parameter(Mandatory)] [string]$Source,
        [Parameter(Mandatory)] [string]$Destination
    )
    Add-Type -AssemblyName System.IO.Compression -ErrorAction SilentlyContinue
    $levelNames = [Enum]::GetNames([System.IO.Compression.CompressionLevel])
    $level = if ($levelNames -contains "SmallestSize") {
        [System.IO.Compression.CompressionLevel]::SmallestSize
    } else {
        [System.IO.Compression.CompressionLevel]::Optimal
    }
    $in = [System.IO.File]::OpenRead($Source)
    try {
        $out = [System.IO.File]::Create($Destination)
        try {
            $gz = New-Object System.IO.Compression.GZipStream($out, $level)
            try { $in.CopyTo($gz) } finally { $gz.Dispose() }
        } finally { $out.Dispose() }
    } finally { $in.Dispose() }
}

# SHA-256 (lowercase hex) of the DECOMPRESSED content of a .gz file, so an
# existing payload can be compared against the raw-binary pin.
function Get-DecompressedSha256 {
    param([Parameter(Mandatory)] [string]$GzipPath)
    $in = [System.IO.File]::OpenRead($GzipPath)
    try {
        $gz = New-Object System.IO.Compression.GZipStream(
            $in, [System.IO.Compression.CompressionMode]::Decompress)
        try {
            $sha = [System.Security.Cryptography.SHA256]::Create()
            try {
                [System.BitConverter]::ToString($sha.ComputeHash($gz)).Replace("-", "").ToLowerInvariant()
            } finally { $sha.Dispose() }
        } finally { $gz.Dispose() }
    } finally { $in.Dispose() }
}

# Writes the embedded VERSION file: plain text, no trailing newline.
function Write-CliVersionFile {
    param(
        [Parameter(Mandatory)] [string]$Path,
        [Parameter(Mandatory)] [string]$Version
    )
    [System.IO.File]::WriteAllText($Path, $Version)
}
