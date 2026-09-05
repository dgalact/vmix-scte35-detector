$ErrorActionPreference = "Stop"

$detector = Join-Path $PSScriptRoot "vmix-scte35-detector.exe"
$ffmpeg = Join-Path $PSScriptRoot "ffmpeg.exe"

if (-not (Test-Path $detector)) {
    throw "vmix-scte35-detector.exe was not found in $PSScriptRoot"
}
if (-not (Test-Path $ffmpeg)) {
    throw "ffmpeg.exe was not found. Place a Windows FFmpeg build beside the detector."
}

$hostName = Read-Host "VPS public IP or DNS name"
if ([string]::IsNullOrWhiteSpace($hostName)) { throw "VPS address is required" }
$port = Read-Host "SRT port [9001]"
if ([string]::IsNullOrWhiteSpace($port)) { $port = "9001" }
$securePass = Read-Host "SRT passphrase" -AsSecureString
$passphrase = [System.Net.NetworkCredential]::new("", $securePass).Password

$query = "mode=caller&transtype=live&latency=2000000"
if (-not [string]::IsNullOrWhiteSpace($passphrase)) {
    $query += "&passphrase=$([System.Uri]::EscapeDataString($passphrase))"
}
$env:SCTE_SRT_URL = "srt://${hostName}:${port}?${query}"

try {
    & $detector `
        --ffmpeg $ffmpeg `
        --local-srt "127.0.0.1:10000" `
        --local-mode "caller" `
        --vmix "http://127.0.0.1:8088" `
        --main "SRT 9001" `
        --ad "AD_BREAK" `
        --dry-run=true
}
finally {
    Remove-Item Env:SCTE_SRT_URL -ErrorAction SilentlyContinue
    $passphrase = $null
}
