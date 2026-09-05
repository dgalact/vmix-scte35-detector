$ErrorActionPreference = "Stop"

$hostName = Read-Host "SRT server public IP or DNS"
if ([string]::IsNullOrWhiteSpace($hostName)) { throw "SRT server is required" }
$port = Read-Host "SRT port (for example 5030 or 9001)"
if ($port -notmatch '^\d+$' -or [int]$port -lt 1 -or [int]$port -gt 65535) {
    throw "Port must be between 1 and 65535"
}
$usesPassphrase = Read-Host "Does this SRT stream use a passphrase? [y/N]"
$noPassphrase = if ($usesPassphrase -match '^[yY]') { "0" } else { "1" }
$mainInput = Read-Host "vMix main input name [SRT 9001]"
if ([string]::IsNullOrWhiteSpace($mainInput)) { $mainInput = "SRT 9001" }
$adInput = Read-Host "vMix advertising input name [AD_BREAK]"
if ([string]::IsNullOrWhiteSpace($adInput)) { $adInput = "AD_BREAK" }

function Escape-CmdValue([string]$value) {
    if ($value -match '[\r\n"]') { throw "Quotes and line breaks are not allowed" }
    return $value.Replace('%', '%%')
}

$lines = @(
    '@echo off',
    ('set "VMIX_VPS_HOST={0}"' -f (Escape-CmdValue $hostName)),
    ('set "VMIX_SRT_PORT={0}"' -f $port),
    ('set "VMIX_SRT_NO_PASSPHRASE={0}"' -f $noPassphrase),
    ('set "VMIX_MAIN_INPUT={0}"' -f (Escape-CmdValue $mainInput)),
    ('set "VMIX_AD_INPUT={0}"' -f (Escape-CmdValue $adInput))
)
Set-Content -Path (Join-Path $PSScriptRoot 'settings.local.cmd') -Value $lines -Encoding ASCII
Write-Host "Configuration saved. Start with START-AUTOMATION.cmd"
