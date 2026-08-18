# Run the quality checks required before completing a milestone on Windows.
[CmdletBinding()]
param(
    [switch]$Race
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot

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

function Test-GoFormatting {
    $unformattedFiles = & gofmt -l .
    if ($LASTEXITCODE -ne 0) {
        throw "gofmt failed with exit code $LASTEXITCODE"
    }

    if ($unformattedFiles) {
        throw (
            "The following Go files require formatting:`n" +
            ($unformattedFiles -join "`n") +
            "`nRun: go fmt ./..."
        )
    }
}

function Test-GitWhitespace {
    Invoke-NativeCommand git @("diff", "--check")
    Invoke-NativeCommand git @("diff", "--cached", "--check")
}

Push-Location $projectRoot
try {
    Write-Host "Checking milestone in $projectRoot"
    Invoke-Check "Go version" { Invoke-NativeCommand go @("version") }
    Invoke-Check "Go formatting" { Test-GoFormatting }
    Invoke-Check "Module files" { Invoke-NativeCommand go @("mod", "tidy", "-diff") }
    Invoke-Check "Module checksums" { Invoke-NativeCommand go @("mod", "verify") }
    Invoke-Check "Static analysis" { Invoke-NativeCommand go @("vet", "./...") }
    Invoke-Check "Tests" { Invoke-NativeCommand go @("test", "-count=1", "-cover", "./...") }

    if ($Race) {
        Invoke-Check "Race detector" { Invoke-NativeCommand go @("test", "-count=1", "-race", "./...") }
    }

    Invoke-Check "Build" { Invoke-NativeCommand go @("build", "./...") }
    Invoke-Check "Git whitespace" { Test-GitWhitespace }

    Write-Host ""
    Write-Host "All milestone checks passed."
}
finally {
    Pop-Location
}
