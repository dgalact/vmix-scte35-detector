$ErrorActionPreference = "Stop"

$archiveUrl = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
$checksumUrl = "$archiveUrl.sha256"
$archive = Join-Path $env:TEMP "vmix-scte35-ffmpeg.zip"
$extract = Join-Path $env:TEMP "vmix-scte35-ffmpeg"

Write-Host "Downloading the Windows FFmpeg essentials build..."
Invoke-WebRequest -Uri $archiveUrl -OutFile $archive
$expectedText = (Invoke-WebRequest -Uri $checksumUrl).Content.Trim()
$expected = ($expectedText -split '\s+')[0].ToLowerInvariant()
$actual = (Get-FileHash -Path $archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) {
    throw "FFmpeg archive checksum mismatch. Expected $expected, got $actual"
}

if (Test-Path $extract) { Remove-Item -Recurse -Force $extract }
Expand-Archive -Path $archive -DestinationPath $extract
$source = Get-ChildItem -Path $extract -Filter ffmpeg.exe -Recurse | Select-Object -First 1
if ($null -eq $source) { throw "ffmpeg.exe was not found in the downloaded archive" }
Copy-Item -Path $source.FullName -Destination (Join-Path $PSScriptRoot "ffmpeg.exe") -Force

Remove-Item $archive -Force
Remove-Item $extract -Recurse -Force
Write-Host "ffmpeg.exe is ready in $PSScriptRoot"
