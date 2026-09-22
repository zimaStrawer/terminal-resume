# sizhou.ai terminal resume - one-line download & launch (no install) - Windows / PowerShell
#
# Usage (run in PowerShell):
#   irm https://zhangsizhou.pages.dev/ai.ps1 | iex
#
# Once the sizhou.ai domain is live, this shortens to:
#   irm https://sizhou.ai/ai.ps1 | iex
#

$ErrorActionPreference = 'Stop'

# Binaries are hosted on Cloudflare Pages (zhangsizhou.pages.dev), not GitHub
$baseUrl = if ($env:SIZHOU_BASE_URL) { $env:SIZHOU_BASE_URL } else { 'https://zhangsizhou.pages.dev' }
$binName = 'sizhou-resume.exe'

# Detect arch: win32-amd64 or win32-arm64
# PROCESSOR_ARCHITEW6432 reports the real CPU arch from a 32-bit process; prefer it
$arch = $env:PROCESSOR_ARCHITEW6432
if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
switch ($arch) {
    'AMD64' { $target = 'win32-amd64' }
    'ARM64' { $target = 'win32-arm64' }
    default {
        Write-Host "Unsupported architecture: $arch" -ForegroundColor Red
        return
    }
}

$url = "$baseUrl/terminal-resume-$target.exe"

# Download to a temp dir, auto-clean on exit
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
$outFile = Join-Path $tmp $binName

# Theme color #16B8F3: PowerShell 7 supports ANSI; Windows PowerShell 5.1 does not, degrade to plain
$psMajor = $PSVersionTable.PSVersion.Major
if ($psMajor -ge 7) {
    $C = [char]27 + '[38;2;22;184;243m'
    $R = [char]27 + '[0m'
    $cursorHide = [char]27 + '[?25l'
    $cursorShow = [char]27 + '[?25h'
    $useAnsi = $true
} else {
    $C = ''; $R = ''; $cursorHide = ''; $cursorShow = ''; $useAnsi = $false
}

Write-Host 'Starting SIZHOU Terminal...'

# Total size is parsed later from the download's own response headers (curl -D
# dumps them to disk the moment they arrive). Do NOT probe with a separate
# Range/HEAD request here: Cloudflare Pages ignores Range (answers 200 with the
# full body) and omits Content-Length on HEAD, so any probe downloads the whole
# ~9.5 MB once more before the real download even starts.
$total = 0
$headersFile = Join-Path $tmp 'headers'

# Background download: prefer the built-in curl.exe (Win10 1803+); fall back to Invoke-WebRequest
$curl = Get-Command curl.exe -ErrorAction SilentlyContinue
if ($curl) {
    # Quote paths: Start-Process joins ArgumentList with spaces WITHOUT quoting
    # on PowerShell 5.1, so a temp path containing spaces (e.g. "C:\Users\Zhang San\...")
    # would be split into broken curl arguments.
    $proc = Start-Process -FilePath $curl.Source `
        -ArgumentList @('-fsSL', '--retry', '3', '--connect-timeout', '15', '-D', "`"$headersFile`"", $url, '-o', "`"$outFile`"") `
        -PassThru -WindowStyle Hidden
} else {
    $wc = New-Object System.Net.WebClient
}

# Theme color block progress bar: filled with full blocks, remaining with light blocks, real %
$W = 30
$lastPct = -1
$spin = @([char]0x280B,[char]0x2819,[char]0x2839,[char]0x2838,[char]0x283C,[char]0x2834,[char]0x2826,[char]0x2827,[char]0x2807,[char]0x280F)

if ($useAnsi) { Write-Host -NoNewline $cursorHide }

if ($curl) {
    while (-not $proc.HasExited) {
        Start-Sleep -Milliseconds 80
        # Response headers arrive before the body: once curl -D has flushed them,
        # read the full-length Content-Length (only tried until it succeeds).
        if ($total -eq 0 -and (Test-Path $headersFile)) {
            foreach ($line in Get-Content $headersFile) {
                if ($line -match '^[Cc]ontent-[Ll]ength:\s*(\d+)\s*$') { $total = [long]$Matches[1] }
            }
        }
        $doneBytes = 0
        if (Test-Path $outFile) { $doneBytes = (Get-Item $outFile).Length }
        if ($total -gt 0) {
            $pct = [math]::Min(100, [int]($doneBytes * 100 / $total))
            if ($pct -ne $lastPct) {
                $fill = [int]($pct * $W / 100)
                $empty = $W - $fill
                $blockFull = [string][char]0x2588
                $blockEmpty = [string][char]0x2591
                $bar = ($blockFull * $fill) + ($blockEmpty * $empty)
                Write-Host -NoNewline ("`r  {0}{1}{2} {3,3}%" -f $C, $bar, $R, $pct)
                $lastPct = $pct
            }
        } else {
            # Spinner while total is still unknown; show downloaded size for real feedback
            $i = [int]([math]::Floor($doneBytes / 1024)) % 10
            if ($doneBytes -ge 1MB) {
                $size = '{0:F1} MB' -f ($doneBytes / 1MB)
            } else {
                $size = '{0} KB' -f [int]($doneBytes / 1KB)
            }
            Write-Host -NoNewline ("`r  {0}{1}{2} {3}" -f $C, $spin[$i], $R, $size)
        }
    }
    $curlExit = $proc.ExitCode
    $barFull = [string][char]0x2588 * $W
    Write-Host ("`r  {0}{1}{2} {3,3}%" -f $C, $barFull, $R, 100)
    if ($useAnsi) { Write-Host -NoNewline $cursorShow }

    if ($curlExit -ne 0 -or -not (Test-Path $outFile) -or (Get-Item $outFile).Length -lt 1024) {
        Write-Host 'Download failed. Please check your network and retry.' -ForegroundColor Red
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
        return
    }
} else {
    try {
        $wc.DownloadFile($url, $outFile)
    } catch {
        Write-Host 'Download failed. Please check your network and retry.' -ForegroundColor Red
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
        return
    }
}

# Launch directly (local TUI, no SSH)
& $outFile --local
$exitCode = $LASTEXITCODE

# Clean up temp dir
Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
if ($exitCode) { exit $exitCode }
