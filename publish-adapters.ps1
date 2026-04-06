[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ReleaseVersion,

    [string]$PublisherEnvPath = ".\publisher.env",

    [string[]]$Channels = @("latest", "stable"),

    [switch]$UseSshTunnel,

    [string]$SshHost,

    [string]$SshUser = "root",

    [string]$SshKeyPath,

    [int]$SshLocalPort = 9000,

    [string]$SshRemoteHost = "127.0.0.1",

    [int]$SshRemotePort = 9000,

    [int]$SshReadyTimeoutSec = 15,

    [switch]$BuildOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$env:PYTHONIOENCODING = "utf-8"

$RepoRoot = Split-Path -Parent $PSCommandPath
$AgentRoot = Join-Path $RepoRoot "agent"
$ReleaseDir = Join-Path $AgentRoot "tmp\release"
$PublisherEnvFullPath = if ([System.IO.Path]::IsPathRooted($PublisherEnvPath)) {
    $PublisherEnvPath
} else {
    Join-Path $RepoRoot $PublisherEnvPath
}

function Set-Utf8Location {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    Set-Location $Path
}

function Import-DotEnv {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        throw "Файл переменных окружения не найден: $Path"
    }

    Get-Content -LiteralPath $Path -Encoding UTF8 | ForEach-Object {
        $line = $_.Trim()
        if (-not $line -or $line.StartsWith('#')) {
            return
        }

        $parts = $line.Split('=', 2)
        if ($parts.Count -ne 2) {
            throw "Невалидная строка в ${Path}: $line"
        }

        $name = $parts[0].Trim()
        $value = $parts[1]
        Set-Item -Path "Env:$name" -Value $value
    }
}

function Assert-Env {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Names
    )

    foreach ($name in $Names) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if ([string]::IsNullOrWhiteSpace($value)) {
            throw "Не задана обязательная переменная окружения: $name"
        }
    }
}

function Test-TcpPortOpen {
    param(
        [Parameter(Mandatory = $true)]
        [string]$HostName,

        [Parameter(Mandatory = $true)]
        [int]$Port,

        [int]$TimeoutMs = 1000
    )

    $client = [System.Net.Sockets.TcpClient]::new()
    try {
        $asyncResult = $client.BeginConnect($HostName, $Port, $null, $null)
        if (-not $asyncResult.AsyncWaitHandle.WaitOne($TimeoutMs, $false)) {
            return $false
        }

        $client.EndConnect($asyncResult)
        return $true
    } catch {
        return $false
    } finally {
        $client.Dispose()
    }
}

function Start-SshTunnelProcess {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SshHostName,

        [Parameter(Mandatory = $true)]
        [string]$User,

        [string]$KeyPath,

        [Parameter(Mandatory = $true)]
        [int]$LocalPort,

        [Parameter(Mandatory = $true)]
        [string]$RemoteHost,

        [Parameter(Mandatory = $true)]
        [int]$RemotePort,

        [Parameter(Mandatory = $true)]
        [int]$ReadyTimeoutSec
    )

    $sshCommand = Get-Command ssh.exe -ErrorAction SilentlyContinue
    if ($null -eq $sshCommand) {
        throw 'Не найден ssh.exe. Установите OpenSSH Client в Windows.'
    }

    if ([string]::IsNullOrWhiteSpace($SshHostName)) {
        throw 'Для SSH-туннеля нужно указать -SshHost.'
    }

    if (-not [string]::IsNullOrWhiteSpace($KeyPath)) {
        if (-not (Test-Path -LiteralPath $KeyPath)) {
            throw ('Файл приватного ключа не найден: {0}' -f $KeyPath)
        }
        if ((Get-Item -LiteralPath $KeyPath).PSIsContainer) {
            throw ('Параметр -SshKeyPath должен указывать на файл приватного ключа, а не на каталог: {0}' -f $KeyPath)
        }
    }

    if (Test-TcpPortOpen -HostName "127.0.0.1" -Port $LocalPort -TimeoutMs 300) {
        throw ('Локальный порт {0} уже занят. Освободите его или укажите другой -SshLocalPort.' -f $LocalPort)
    }

    $stdoutPath = Join-Path ([System.IO.Path]::GetTempPath()) ("gosss-publisher-ssh-{0}.out.log" -f ([System.Guid]::NewGuid().ToString("N")))
    $stderrPath = Join-Path ([System.IO.Path]::GetTempPath()) ("gosss-publisher-ssh-{0}.err.log" -f ([System.Guid]::NewGuid().ToString("N")))

    $arguments = @(
        "-N",
        "-o", "BatchMode=yes",
        "-o", "StrictHostKeyChecking=accept-new",
        "-o", "ExitOnForwardFailure=yes",
        "-o", "ConnectTimeout=10",
        "-o", "ServerAliveInterval=30",
        "-o", "ServerAliveCountMax=3",
        "-L", "${LocalPort}:${RemoteHost}:${RemotePort}"
    )

    if (-not [string]::IsNullOrWhiteSpace($KeyPath)) {
        $arguments += @("-i", $KeyPath)
    }

    $arguments += "$User@$SshHostName"

    $process = Start-Process `
        -FilePath $sshCommand.Source `
        -ArgumentList $arguments `
        -PassThru `
        -WindowStyle Hidden `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath
    $deadline = (Get-Date).AddSeconds($ReadyTimeoutSec)

    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 300

        if ($process.HasExited) {
            $sshError = if (Test-Path -LiteralPath $stderrPath) {
                (Get-Content -LiteralPath $stderrPath -Encoding UTF8 -Raw).Trim()
            } else {
                ""
            }
            if ([string]::IsNullOrWhiteSpace($sshError)) {
                throw ('SSH-туннель завершился раньше времени с кодом {0}.' -f $process.ExitCode)
            }
            throw ('SSH-туннель завершился раньше времени с кодом {0}. stderr: {1}' -f $process.ExitCode, $sshError)
        }

        if (Test-TcpPortOpen -HostName "127.0.0.1" -Port $LocalPort -TimeoutMs 300) {
            return [PSCustomObject]@{
                Process    = $process
                StdOutPath = $stdoutPath
                StdErrPath = $stderrPath
            }
        }
    }

    try {
        if (-not $process.HasExited) {
            Stop-Process -Id $process.Id -Force
        }
    } catch {
    }

    $sshError = if (Test-Path -LiteralPath $stderrPath) {
        (Get-Content -LiteralPath $stderrPath -Encoding UTF8 -Raw).Trim()
    } else {
        ""
    }
    if ([string]::IsNullOrWhiteSpace($sshError)) {
        throw ('SSH-туннель не поднялся за {0}с.' -f $ReadyTimeoutSec)
    }
    throw ('SSH-туннель не поднялся за {0}с. stderr: {1}' -f $ReadyTimeoutSec, $sshError)
}

function Invoke-GoBuildAdapter {
    param(
        [Parameter(Mandatory = $true)]
        [string]$GoArch,

        [Parameter(Mandatory = $true)]
        [string]$OutputFile,

        [Parameter(Mandatory = $true)]
        [string]$CommandPath
    )

    $env:GOOS = "windows"
    $env:GOARCH = $GoArch

    & go build -ldflags "-X main.AdapterVersion=$ReleaseVersion" -o $OutputFile $CommandPath
    if ($LASTEXITCODE -ne 0) {
        throw "Сборка $CommandPath завершилась с кодом $LASTEXITCODE"
    }
}

function Invoke-AdapterPublish {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(Mandatory = $true)]
        [string]$AdapterID,

        [Parameter(Mandatory = $true)]
        [string]$Title,

        [Parameter(Mandatory = $true)]
        [string]$Description,

        [Parameter(Mandatory = $true)]
        [string]$TargetArch
    )

    $arguments = @(
        "run", ".\cmd\adapter-publisher",
        "publish",
        "--file", $FilePath,
        "--adapter-id", $AdapterID,
        "--version", $ReleaseVersion,
        "--title", $Title,
        "--description", $Description,
        "--target-os", "windows",
        "--target-arch", $TargetArch,
        "--protocol-version", "1"
    )

    foreach ($channel in $Channels) {
        if (-not [string]::IsNullOrWhiteSpace($channel)) {
            $arguments += @("--promote", $channel.Trim())
        }
    }

    & go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Публикация адаптера $AdapterID завершилась с кодом $LASTEXITCODE"
    }
}

$sshTunnel = $null

try {
    Set-Utf8Location -Path $RepoRoot
    Import-DotEnv -Path $PublisherEnvFullPath

    if ($UseSshTunnel) {
        $sshTunnel = Start-SshTunnelProcess `
            -SshHostName $SshHost `
            -User $SshUser `
            -KeyPath $SshKeyPath `
            -LocalPort $SshLocalPort `
            -RemoteHost $SshRemoteHost `
            -RemotePort $SshRemotePort `
            -ReadyTimeoutSec $SshReadyTimeoutSec

        $env:S3_ENDPOINT = "http://127.0.0.1:$SshLocalPort"
        Write-Host ('SSH-туннель поднят: 127.0.0.1:{0} -> {1}@{2} -> {3}:{4}' -f $SshLocalPort, $SshUser, $SshHost, $SshRemoteHost, $SshRemotePort)
    }

    Assert-Env -Names @(
        "S3_ENDPOINT",
        "S3_ACCESS_KEY",
        "S3_SECRET_KEY",
        "AGENT_ADAPTER_CATALOG_ENABLED",
        "AGENT_ADAPTER_CATALOG_BUCKET",
        "AGENT_ADAPTER_CATALOG_PUBLIC_BASE_URL",
        "AGENT_ADAPTER_CATALOG_KEY"
    )

    New-Item -ItemType Directory -Force -Path $ReleaseDir | Out-Null

    Set-Utf8Location -Path $AgentRoot

    Invoke-GoBuildAdapter -GoArch "386" -OutputFile ".\tmp\release\fiscal-atol-adapter-386.exe" -CommandPath ".\cmd\fiscal-atol-adapter"
    Invoke-GoBuildAdapter -GoArch "386" -OutputFile ".\tmp\release\fiscal-shtrih-adapter-386.exe" -CommandPath ".\cmd\fiscal-shtrih-adapter"
    Invoke-GoBuildAdapter -GoArch "amd64" -OutputFile ".\tmp\release\fiscal-mitsu-adapter.exe" -CommandPath ".\cmd\fiscal-mitsu-adapter"

    if ($BuildOnly) {
        Write-Host "Сборка завершена. Бинарники находятся в $ReleaseDir"
        exit 0
    }

    Set-Utf8Location -Path $RepoRoot

    Invoke-AdapterPublish `
        -FilePath ".\agent\tmp\release\fiscal-atol-adapter-386.exe" `
        -AdapterID "fiscal-atol" `
        -Title "Фискальный адаптер АТОЛ" `
        -Description "Windows x86 release" `
        -TargetArch "386"

    Invoke-AdapterPublish `
        -FilePath ".\agent\tmp\release\fiscal-shtrih-adapter-386.exe" `
        -AdapterID "fiscal-shtrih" `
        -Title "Фискальный адаптер Штрих" `
        -Description "Windows x86 release" `
        -TargetArch "386"

    Invoke-AdapterPublish `
        -FilePath ".\agent\tmp\release\fiscal-mitsu-adapter.exe" `
        -AdapterID "fiscal-mitsu" `
        -Title "Фискальный адаптер Mitsu" `
        -Description "Windows x64 release" `
        -TargetArch "amd64"

    Write-Host "Сборка и публикация адаптеров завершены успешно."
} finally {
    if ($null -ne $sshTunnel) {
        try {
            if (-not $sshTunnel.Process.HasExited) {
                Stop-Process -Id $sshTunnel.Process.Id -Force
                Write-Host 'SSH-туннель остановлен.'
            }
        } catch {
        }
    }
}
