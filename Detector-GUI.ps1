﻿$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()

$script:detectorProcess = $null
$script:settingsPath = Join-Path $PSScriptRoot "settings.local.cmd"
$script:logPath = Join-Path $PSScriptRoot "detector.log"
$script:statusPath = Join-Path $PSScriptRoot "relay.status"

function Read-LocalSettings {
    $values = @{}
    if (Test-Path $script:settingsPath) {
        foreach ($line in Get-Content $script:settingsPath) {
            if ($line -match '^set\s+"([^=]+)=(.*)"\s*$') {
                $values[$matches[1]] = $matches[2]
            }
            elseif ($line -match '^set\s+([^=]+)=(.*)\s*$') {
                $values[$matches[1].Trim()] = $matches[2].Trim()
            }
        }
    }
    return $values
}

function Add-Label([string]$text, [int]$x, [int]$y, [int]$w = 145) {
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $text
    $label.Location = New-Object System.Drawing.Point($x, $y)
    $label.Size = New-Object System.Drawing.Size($w, 22)
    $form.Controls.Add($label)
    return $label
}

function Add-TextBox([string]$value, [int]$x, [int]$y, [int]$w = 230) {
    $box = New-Object System.Windows.Forms.TextBox
    $box.Text = $value
    $box.Location = New-Object System.Drawing.Point($x, $y)
    $box.Size = New-Object System.Drawing.Size($w, 24)
    $form.Controls.Add($box)
    return $box
}

function Quote-Argument([string]$value) {
    return '"' + $value.Replace('\', '\\').Replace('"', '\"') + '"'
}

function Set-Status([string]$text, [System.Drawing.Color]$color) {
    $statusPanel.BackColor = $color
    $statusLabel.Text = $text
}

function Get-VmixStatus {
    $client = New-Object System.Net.WebClient
    $client.Encoding = [System.Text.Encoding]::UTF8
    [xml]$state = $client.DownloadString("http://127.0.0.1:8088/api")
    $active = [string]$state.vmix.active
    foreach ($input in $state.vmix.inputs.input) {
        if ([string]$input.number -eq $active -or [string]$input.key -eq $active) {
            $name = [string]$input.title
            if (-not [string]::IsNullOrWhiteSpace([string]$input.shortTitle)) { $name = [string]$input.shortTitle }
            return [PSCustomObject]@{ Name = $name; State = [string]$input.state; Number = [string]$input.number }
        }
    }
    return [PSCustomObject]@{ Name = $active; State = "Unknown"; Number = $active }
}

$settings = Read-LocalSettings
$form = New-Object System.Windows.Forms.Form
$form.Text = "vMix SCTE-35 Detector"
$form.ClientSize = New-Object System.Drawing.Size(430, 420)
$form.FormBorderStyle = [System.Windows.Forms.FormBorderStyle]::FixedSingle
$form.MaximizeBox = $false
$form.StartPosition = [System.Windows.Forms.FormStartPosition]::CenterScreen
$form.Font = New-Object System.Drawing.Font("Segoe UI", 9)

$statusPanel = New-Object System.Windows.Forms.Panel
$statusPanel.Location = New-Object System.Drawing.Point(14, 12)
$statusPanel.Size = New-Object System.Drawing.Size(402, 68)
$form.Controls.Add($statusPanel)

$statusLabel = New-Object System.Windows.Forms.Label
$statusLabel.Dock = [System.Windows.Forms.DockStyle]::Fill
$statusLabel.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
$statusLabel.Font = New-Object System.Drawing.Font("Segoe UI", 16, [System.Drawing.FontStyle]::Bold)
$statusLabel.ForeColor = [System.Drawing.Color]::White
$statusPanel.Controls.Add($statusLabel)
Set-Status "ОСТАНОВЛЕНО" ([System.Drawing.Color]::FromArgb(90, 90, 90))

Add-Label "Поток от Relay (UDP)" 16 98 145
$inputBox = Add-TextBox "udp://127.0.0.1:10002" 168 95 248

Add-Label "Основной вход vMix" 16 132 145
$mainValue = $settings["VMIX_MAIN_INPUT"]
if ([string]::IsNullOrWhiteSpace($mainValue)) { $mainValue = "1" }
$mainBox = Add-TextBox $mainValue 168 129 248

Add-Label "Рекламная заготовка" 16 166 145
$adValue = $settings["VMIX_AD_INPUT"]
if ([string]::IsNullOrWhiteSpace($adValue)) { $adValue = "Blank" }
$adBox = Add-TextBox $adValue 168 163 248

# Режим работы
$modeGroup = New-Object System.Windows.Forms.GroupBox
$modeGroup.Text = "Режим управления vMix"
$modeGroup.Location = New-Object System.Drawing.Point(16, 200)
$modeGroup.Size = New-Object System.Drawing.Size(400, 62)
$form.Controls.Add($modeGroup)

$autoRadio = New-Object System.Windows.Forms.RadioButton
$autoRadio.Text = "АВТОМАТ (переключать vMix по SCTE-35)"
$autoRadio.Location = New-Object System.Drawing.Point(16, 22)
$autoRadio.Size = New-Object System.Drawing.Size(260, 24)
$autoRadio.Checked = $true
$modeGroup.Controls.Add($autoRadio)

$manualRadio = New-Object System.Windows.Forms.RadioButton
$manualRadio.Text = "РУЧНОЙ"
$manualRadio.Location = New-Object System.Drawing.Point(285, 22)
$manualRadio.Size = New-Object System.Drawing.Size(100, 24)
$modeGroup.Controls.Add($manualRadio)

$startButton = New-Object System.Windows.Forms.Button
$startButton.Text = "ЗАПУСТИТЬ ДЕТЕКТОР"
$startButton.Location = New-Object System.Drawing.Point(16, 276)
$startButton.Size = New-Object System.Drawing.Size(195, 42)
$startButton.BackColor = [System.Drawing.Color]::FromArgb(40, 145, 70)
$startButton.ForeColor = [System.Drawing.Color]::White
$startButton.FlatStyle = [System.Windows.Forms.FlatStyle]::Flat
$startButton.Font = New-Object System.Drawing.Font("Segoe UI", 9, [System.Drawing.FontStyle]::Bold)
$form.Controls.Add($startButton)

$stopButton = New-Object System.Windows.Forms.Button
$stopButton.Text = "ОСТАНОВИТЬ"
$stopButton.Location = New-Object System.Drawing.Point(221, 276)
$stopButton.Size = New-Object System.Drawing.Size(195, 42)
$stopButton.Enabled = $false
$stopButton.FlatStyle = [System.Windows.Forms.FlatStyle]::Flat
$form.Controls.Add($stopButton)

$detailLabel = New-Object System.Windows.Forms.Label
$detailLabel.Location = New-Object System.Drawing.Point(16, 332)
$detailLabel.Size = New-Object System.Drawing.Size(400, 48)
$detailLabel.TextAlign = [System.Drawing.ContentAlignment]::TopCenter
$detailLabel.ForeColor = [System.Drawing.Color]::DimGray
$detailLabel.Text = "START-RELAY должен быть запущен для подачи потока на 127.0.0.1"
$form.Controls.Add($detailLabel)

$startButton.Add_Click({
    try {
        if ([string]::IsNullOrWhiteSpace($inputBox.Text)) { throw "Укажи URL входного потока (udp://...)" }
        $detector = Join-Path $PSScriptRoot "vmix-scte35-detector.exe"
        $ffmpeg = Join-Path $PSScriptRoot "ffmpeg.exe"
        if (-not (Test-Path $detector)) { throw "Не найден vmix-scte35-detector.exe" }
        if (-not (Test-Path $ffmpeg)) { throw "Не найден ffmpeg.exe" }

        $dryRun = if ($autoRadio.Checked) { "false" } else { "true" }

        $arguments = @(
            "--ffmpeg", (Quote-Argument $ffmpeg),
            "--input", (Quote-Argument $inputBox.Text),
            "--vmix", "http://127.0.0.1:8088",
            "--main", (Quote-Argument $mainBox.Text),
            "--ad", (Quote-Argument $adBox.Text),
            "--dry-run=$dryRun",
            "--log", (Quote-Argument $script:logPath),
            "--status", (Quote-Argument $script:statusPath)
        ) -join " "

        $info = New-Object System.Diagnostics.ProcessStartInfo
        $info.FileName = $detector
        $info.Arguments = $arguments
        $info.WorkingDirectory = $PSScriptRoot
        $info.UseShellExecute = $false
        $info.CreateNoWindow = $true
        $info.EnvironmentVariables["SCTE_INPUT_URL"] = $inputBox.Text
        $script:detectorProcess = [System.Diagnostics.Process]::Start($info)

        $startButton.Enabled = $false
        $stopButton.Enabled = $true
        $inputBox.Enabled = $false
        $mainBox.Enabled = $false
        $adBox.Enabled = $false
        $autoRadio.Enabled = $false
        $manualRadio.Enabled = $false

        $modeText = if ($autoRadio.Checked) { "АВТОМАТ" } else { "РУЧНОЙ МОНИТОРИНГ" }
        Set-Status "ПОДКЛЮЧЕНИЕ ($modeText)..." ([System.Drawing.Color]::FromArgb(210, 140, 20))
        $detailLabel.Text = "Ожидание пакетов SCTE-35..."
    }
    catch {
        [System.Windows.Forms.MessageBox]::Show($_.Exception.Message, "Ошибка запуска", "OK", "Error") | Out-Null
    }
})

$stopButton.Add_Click({
    if ($null -ne $script:detectorProcess -and -not $script:detectorProcess.HasExited) {
        & taskkill.exe /PID $script:detectorProcess.Id /T /F 2>$null | Out-Null
    }
    $script:detectorProcess = $null
    $startButton.Enabled = $true
    $stopButton.Enabled = $false
    $inputBox.Enabled = $true
    $mainBox.Enabled = $true
    $adBox.Enabled = $true
    $autoRadio.Enabled = $true
    $manualRadio.Enabled = $true
    Set-Status "ОСТАНОВЛЕНО" ([System.Drawing.Color]::FromArgb(90, 90, 90))
    $detailLabel.Text = "Детектор остановлен. Эфир в vMix продолжается через релей."
})

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 750
$timer.Add_Tick({
    if ($null -eq $script:detectorProcess) { return }
    if ($script:detectorProcess.HasExited) {
        Set-Status "ОШИБКА" ([System.Drawing.Color]::FromArgb(175, 45, 45))
        $detailLabel.Text = "Детектор завершился. Смотри detector.log"
        $startButton.Enabled = $true
        $stopButton.Enabled = $false
        $inputBox.Enabled = $true
        $mainBox.Enabled = $true
        $adBox.Enabled = $true
        $autoRadio.Enabled = $true
        $manualRadio.Enabled = $true
        $script:detectorProcess = $null
        return
    }
    try {
        $relayState = ""
        if (Test-Path $script:statusPath) { $relayState = (Get-Content $script:statusPath -Raw).Trim() }
        if ($relayState -ne "LIVE") {
            Set-Status "ОЖИДАНИЕ ПОТОКА" ([System.Drawing.Color]::FromArgb(210, 140, 20))
            $detailLabel.Text = "Статус: $relayState"
            return
        }
        $vmixStatus = Get-VmixStatus
        $activeName = $vmixStatus.Name
        $activeNum = [string]$vmixStatus.Number
        $isAd = ($activeName -ieq $adBox.Text -or $activeNum -eq $adBox.Text)

        $modePrefix = if ($manualRadio.Checked) { "[РУЧНОЙ] " } else { "" }

        if ($isAd) {
            Set-Status ($modePrefix + "ИДЕТ РЕКЛАМА") ([System.Drawing.Color]::FromArgb(200, 35, 35))
            $detailLabel.Text = "В Program активен: $activeName"
        }
        else {
            Set-Status ($modePrefix + "ЭФИР") ([System.Drawing.Color]::FromArgb(35, 145, 70))
            $detailLabel.Text = "В Program активен: $activeName"
        }
    }
    catch {
        Set-Status "НЕТ СВЯЗИ С vMix" ([System.Drawing.Color]::FromArgb(210, 140, 20))
        $detailLabel.Text = $_.Exception.Message
    }
})
$timer.Start()

$form.Add_FormClosing({
    if ($null -ne $script:detectorProcess -and -not $script:detectorProcess.HasExited) {
        & taskkill.exe /PID $script:detectorProcess.Id /T /F 2>$null | Out-Null
    }
})

[void]$form.ShowDialog()
