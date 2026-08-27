[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [string]$OutputDirectory = "dist"
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

if ($Version -cnotmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') {
    throw "Version must use vMAJOR.MINOR.PATCH: $Version"
}

$projectRoot = Split-Path -Parent $PSScriptRoot
$windowsTar = Join-Path $env:SystemRoot "System32\tar.exe"
if (-not (Test-Path -LiteralPath $windowsTar -PathType Leaf)) {
    throw "Windows tar executable not found: $windowsTar"
}

if ([System.IO.Path]::IsPathRooted($OutputDirectory)) {
    $resolvedOutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
}
else {
    $resolvedOutputDirectory = [System.IO.Path]::GetFullPath(
        (Join-Path $projectRoot $OutputDirectory)
    )
}

New-Item -ItemType Directory -Path $resolvedOutputDirectory -Force | Out-Null

$temporaryDirectory = Join-Path (
    [System.IO.Path]::GetTempPath()
) ("go-mediaarchive-release-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null

$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGOEnabled = $env:CGO_ENABLED

try {
    $releaseVersion = $Version.Substring(1)
    $targets = @(
        @{ OS = "linux"; Architecture = "amd64" },
        @{ OS = "linux"; Architecture = "arm64" },
        @{ OS = "windows"; Architecture = "amd64" },
        @{ OS = "darwin"; Architecture = "amd64" },
        @{ OS = "darwin"; Architecture = "arm64" }
    )
    $commands = @("server", "admin", "client")
    $archives = @()

    Push-Location $projectRoot
    try {
        foreach ($target in $targets) {
            $targetOS = $target.OS
            $targetArchitecture = $target.Architecture
            $archiveBase = "go-mediaarchive_${releaseVersion}_${targetOS}_${targetArchitecture}"
            $packageDirectory = Join-Path $temporaryDirectory $archiveBase
            New-Item -ItemType Directory -Path $packageDirectory | Out-Null

            foreach ($commandName in $commands) {
                $binaryName = "go-mediaarchive-$commandName"
                if ($targetOS -eq "windows") {
                    $binaryName += ".exe"
                }

                Write-Host "Building $commandName for $targetOS/$targetArchitecture"
                $env:CGO_ENABLED = "0"
                $env:GOOS = $targetOS
                $env:GOARCH = $targetArchitecture

                & go build `
                    -trimpath `
                    -o (Join-Path $packageDirectory $binaryName) `
                    "./cmd/$commandName"
                if ($LASTEXITCODE -ne 0) {
                    throw "Build failed for $commandName on $targetOS/$targetArchitecture"
                }
            }

            Copy-Item -LiteralPath "LICENSE", "README.md" -Destination $packageDirectory

            if ($targetOS -eq "windows") {
                $archivePath = Join-Path $resolvedOutputDirectory "$archiveBase.zip"
                if (Test-Path -LiteralPath $archivePath) {
                    Remove-Item -LiteralPath $archivePath
                }
                Compress-Archive -Path $packageDirectory -DestinationPath $archivePath
            }
            else {
                $archivePath = Join-Path $resolvedOutputDirectory "$archiveBase.tar.gz"
                if (Test-Path -LiteralPath $archivePath) {
                    Remove-Item -LiteralPath $archivePath
                }
                & $windowsTar -czf $archivePath -C $temporaryDirectory $archiveBase
                if ($LASTEXITCODE -ne 0) {
                    throw "Archive creation failed for $targetOS/$targetArchitecture"
                }
            }

            $archives += $archivePath
        }
    }
    finally {
        Pop-Location
    }

    $checksumLines = foreach ($archive in $archives) {
        $hash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $([System.IO.Path]::GetFileName($archive))"
    }
    $checksumPath = Join-Path $resolvedOutputDirectory "SHA256SUMS"
    [System.IO.File]::WriteAllText(
        $checksumPath,
        (($checksumLines -join "`n") + "`n"),
        [System.Text.UTF8Encoding]::new($false)
    )

    Write-Host "Created $($archives.Count) release archives and $checksumPath"
}
finally {
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    $env:CGO_ENABLED = $previousCGOEnabled

    $resolvedTemporaryDirectory = [System.IO.Path]::GetFullPath($temporaryDirectory)
    $expectedTemporaryRoot = [System.IO.Path]::GetFullPath(
        [System.IO.Path]::GetTempPath()
    )
    if (-not $resolvedTemporaryDirectory.StartsWith(
        $expectedTemporaryRoot,
        [System.StringComparison]::OrdinalIgnoreCase
    )) {
        throw "Unexpected temporary directory: $resolvedTemporaryDirectory"
    }
    if (Test-Path -LiteralPath $resolvedTemporaryDirectory) {
        Remove-Item -LiteralPath $resolvedTemporaryDirectory -Recurse
    }
}
