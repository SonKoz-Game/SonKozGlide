param(
    [Parameter(Mandatory = $false)]
    [string]$Set
)

$ErrorActionPreference = 'Stop'

$configPath = Join-Path (Split-Path -Parent $PSScriptRoot) 'wails.json'
if (-not (Test-Path $configPath)) {
    Write-Error "wails.json bulunamadi: $configPath"
    exit 1
}

$raw = Get-Content $configPath -Raw

if ($Set) {
    if ($Set -notmatch '^\d+\.\d+\.\d+$') {
        Write-Error "Surum 'x.y.z' biciminde olmali, gelen: $Set"
        exit 1
    }

    $pattern = '(?<prefix>"productVersion"\s*:\s*")(?<value>[^"]*)(?<suffix>")'
    if ($raw -notmatch $pattern) {
        Write-Error "wails.json icinde productVersion bulunamadi"
        exit 1
    }

    $updated = [regex]::Replace($raw, $pattern, { param($m) $m.Groups['prefix'].Value + $Set + $m.Groups['suffix'].Value })

    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($configPath, $updated, $utf8NoBom)

    $raw = $updated
}

$config = ConvertFrom-Json $raw
$version = $config.info.productVersion

if ([string]::IsNullOrWhiteSpace($version)) {
    Write-Error "productVersion bos"
    exit 1
}

Write-Output $version
