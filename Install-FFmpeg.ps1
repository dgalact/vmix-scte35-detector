$ErrorActionPreference = "Stop"
$ProgressPreference = 'SilentlyContinue'

$archiveUrl = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
$checksumUrl = "$archiveUrl.sha256"
$archive = Join-Path $env:TEMP "vmix-scte35-ffmpeg.zip"
$extract = Join-Path $env:TEMP "vmix-scte35-ffmpeg"

Write-Host "Downloading Windows FFmpeg essentials build from gyan.dev..."
Invoke-WebRequest -Uri $archiveUrl -OutFile $archive

Write-Host "Verifying SHA256 checksum..."
$expectedText = (Invoke-WebRequest -Uri $checksumUrl).Content.Trim()
$expected = ($expectedText -split '\s+')[0].ToLowerInvariant()
$actual = (Get-FileHash -Path $archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) {
    Remove-Item $archive -Force -ErrorAction SilentlyContinue
    throw "FFmpeg archive checksum mismatch. Expected $expected, got $actual"
}

Write-Host "Extracting ffmpeg.exe..."
if (Test-Path $extract) { Remove-Item -Recurse -Force $extract }
Expand-Archive -Path $archive -DestinationPath $extract
$source = Get-ChildItem -Path $extract -Filter ffmpeg.exe -Recurse | Select-Object -First 1
if ($null -eq $source) { throw "ffmpeg.exe was not found in the downloaded archive" }
Copy-Item -Path $source.FullName -Destination (Join-Path $PSScriptRoot "ffmpeg.exe") -Force

Remove-Item $archive -Force -ErrorAction SilentlyContinue
Remove-Item $extract -Recurse -Force -ErrorAction SilentlyContinue
Write-Host "SUCCESS: ffmpeg.exe is ready in $PSScriptRoot"
