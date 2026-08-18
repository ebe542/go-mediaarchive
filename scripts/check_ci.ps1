# Validate CI and reproduce its project checks locally on Windows.
[CmdletBinding()]
param(
    [switch]$Act,
    [switch]$SkipRace
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot

function Assert-Command {
    param(
        [Parameter(Mandatory)]
        [string]$Name,
        [Parameter(Mandatory)]
        [string]$InstallationHint
    )

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command '$Name' was not found. Install it with: $InstallationHint"
    }
}

function Invoke-NativeCommand {
    param(
        [Parameter(Mandatory)]
        [string]$Command,
        [string[]]$Arguments = @()
    )

    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code ${LASTEXITCODE}: $Command"
    }
}

function Invoke-Check {
    param(
        [Parameter(Mandatory)]
        [string]$Description,
        [Parameter(Mandatory)]
        [scriptblock]$Action
    )

    Write-Host ""
    Write-Host "==> $Description"
    & $Action
}

Push-Location $projectRoot
try {
    Assert-Command -Name "actionlint" -InstallationHint "go install github.com/rhysd/actionlint/cmd/actionlint@latest"
    Invoke-Check "GitHub Actions workflow syntax" { Invoke-NativeCommand actionlint }

    Invoke-Check "CI project checks" {
        $arguments = @(
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            (Join-Path $PSScriptRoot "check_milestone.ps1")
        )

        if (-not $SkipRace) {
            $arguments += "-Race"
        }

        Invoke-NativeCommand powershell $arguments
    }

    if ($Act) {
        Assert-Command -Name "docker" -InstallationHint "install Docker Desktop and enable its Linux engine"
        Assert-Command -Name "act" -InstallationHint "winget install nektos.act"
        Invoke-Check "Docker availability" { Invoke-NativeCommand docker @("info") }
        Invoke-Check "GitHub Actions verify job" { Invoke-NativeCommand act @("pull_request", "--job", "verify") }
    }

    Write-Host ""
    Write-Host "All local CI checks passed."
}
finally {
    Pop-Location
}
