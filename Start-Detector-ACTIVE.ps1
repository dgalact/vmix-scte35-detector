$ErrorActionPreference = "Stop"

$detector = Join-Path $PSScriptRoot "vmix-scte35-detector.exe"
$ffmpeg = Join-Path $PSScriptRoot "ffmpeg.exe"

if (-not (Test-Path $detector)) { throw "vmix-scte35-detector.exe was not found" }
if (-not (Test-Path $ffmpeg)) { throw "ffmpeg.exe was not found" }

$hostName = $env:VMIX_VPS_HOST
if ([string]::IsNullOrWhiteSpace($hostName)) { $hostName = Read-Host "VPS public IP or DNS name" }
if ([string]::IsNullOrWhiteSpace($hostName)) { throw "VPS address is required" }
$port = $env:VMIX_SRT_PORT
if ([string]::IsNullOrWhiteSpace($port)) { $port = Read-Host "SRT port [9001]" }
if ([string]::IsNullOrWhiteSpace($port)) { $port = "9001" }
$passphrase = ""
if ($env:VMIX_SRT_NO_PASSPHRASE -ne "1") {
    $securePass = Read-Host "SRT passphrase" -AsSecureString
    $passphrase = [System.Net.NetworkCredential]::new("", $securePass).Password
}

$query = "mode=caller&transtype=live&latency=2000000"
if (-not [string]::IsNullOrWhiteSpace($passphrase)) {
    $query += "&passphrase=$([System.Uri]::EscapeDataString($passphrase))"
}
$env:SCTE_SRT_URL = "srt://${hostName}:${port}?${query}"
$mainInput = $env:VMIX_MAIN_INPUT
if ([string]::IsNullOrWhiteSpace($mainInput)) { $mainInput = "SRT 9001" }
$adInput = $env:VMIX_AD_INPUT
if ([string]::IsNullOrWhiteSpace($adInput)) { $adInput = "AD_BREAK" }

try {
    & $detector `
        --ffmpeg $ffmpeg `
        --local-srt "127.0.0.1:10000" `
        --local-mode "caller" `
        --vmix "http://127.0.0.1:8088" `
        --main $mainInput `
        --ad $adInput `
        --dry-run=false
}
finally {
    Remove-Item Env:SCTE_SRT_URL -ErrorAction SilentlyContinue
    $passphrase = $null
}
