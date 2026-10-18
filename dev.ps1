[CmdletBinding()]
param(
    [ValidateSet("start", "stop", "status")]
    [string]$Action = "start",
    [switch]$Background,
    [switch]$NoBuild
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$utf8 = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8

$root = $PSScriptRoot
$configPath = Join-Path $root "config.yaml"
$binDir = Join-Path $root "bin"
$binaryPath = Join-Path $binDir "cli-proxy-api-dev.exe"
$statePath = Join-Path $binDir "dev-server.json"
$stdoutPath = Join-Path $binDir "dev-server.stdout.log"
$stderrPath = Join-Path $binDir "dev-server.stderr.log"

function Get-DevServer {
    if (-not (Test-Path -LiteralPath $statePath -PathType Leaf)) {
        return $null
    }

    $state = Get-Content -LiteralPath $statePath -Raw -Encoding UTF8 | ConvertFrom-Json
    $serverProcess = Get-Process -Id $state.processId -ErrorAction SilentlyContinue
    if ($null -eq $serverProcess) {
        return $null
    }
    if ($serverProcess.Path -ne $binaryPath) {
        return $null
    }
    if ($serverProcess.StartTime.ToUniversalTime().ToString("o") -ne $state.startedAt) {
        return $null
    }
    return $serverProcess
}

Push-Location -LiteralPath $root
try {
    $serverProcess = Get-DevServer
    if ($Action -eq "status") {
        if ($null -eq $serverProcess) {
            Write-Host "The background development server is not running."
        } else {
            Write-Host "The background development server is running (PID $($serverProcess.Id))."
            Write-Host "stdout: $stdoutPath"
            Write-Host "stderr: $stderrPath"
        }
        return
    }

    if ($Action -eq "stop") {
        if ($null -ne $serverProcess) {
            Stop-Process -InputObject $serverProcess
            $serverProcess.WaitForExit()
            Write-Host "Stopped the background development server."
        } else {
            Write-Host "The background development server is not running."
        }
        if (Test-Path -LiteralPath $statePath -PathType Leaf) {
            Remove-Item -LiteralPath $statePath
        }
        return
    }

    if ($null -ne $serverProcess) {
        throw "The background development server is already running (PID $($serverProcess.Id)). Stop it with: .\dev.ps1 -Action stop"
    }
    if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
        throw "Missing config.yaml. See docs/development_CN.md for local configuration."
    }
    if (-not $NoBuild) {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw "Go 1.26 or newer must be installed and available on PATH."
        }
        if (-not (Test-Path -LiteralPath $binDir -PathType Container)) {
            New-Item -ItemType Directory -Path $binDir | Out-Null
        }
        Write-Host "Building the current source..."
        & go build -o $binaryPath ./cmd/server
        if ($LASTEXITCODE -ne 0) {
            throw "Go build failed with exit code $LASTEXITCODE. The server was not started."
        }
    }
    if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
        throw "The development binary is missing. Run without -NoBuild first."
    }

    $serverArgs = @("--config", ('"{0}"' -f $configPath))
    if ($Background) {
        $serverProcess = Start-Process -FilePath $binaryPath -ArgumentList $serverArgs -WorkingDirectory $root -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath -PassThru
        $state = @{
            processId = $serverProcess.Id
            startedAt = $serverProcess.StartTime.ToUniversalTime().ToString("o")
        }
        [System.IO.File]::WriteAllText($statePath, ($state | ConvertTo-Json), $utf8)
        Write-Host "Started the background development process (PID $($serverProcess.Id))."
        Write-Host "Check readiness at the configured server URL's /healthz endpoint."
        Write-Host "stdout: $stdoutPath"
        Write-Host "stderr: $stderrPath"
        Write-Host "Stop with: .\dev.ps1 -Action stop"
    } else {
        Write-Host "Starting the development server. Press Ctrl+C to stop."
        & $binaryPath --config $configPath
        if ($LASTEXITCODE -ne 0) {
            throw "The development server exited with code $LASTEXITCODE."
        }
    }
} finally {
    Pop-Location
}
