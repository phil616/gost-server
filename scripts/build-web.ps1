param([switch]$Verify,[switch]$KeepWorkdir,[string]$Version='dev')
$ErrorActionPreference='Stop'
$projectRoot=Split-Path $PSScriptRoot -Parent
$webRoot=Join-Path $projectRoot 'web'
$tempRoot=[IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\')
$buildRoot=Join-Path $tempRoot ('altasci-web-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $buildRoot | Out-Null
$previousVersion=$env:ALTASCI_VERSION
try {
    $env:ALTASCI_VERSION=$Version
    foreach ($file in @('package.json','package-lock.json','.npmrc','.umirc.ts','tsconfig.json','license-manifest-plugin.cjs')) {Copy-Item -LiteralPath (Join-Path $webRoot $file) -Destination $buildRoot}
    Copy-Item -LiteralPath (Join-Path $webRoot 'src') -Destination (Join-Path $buildRoot 'src') -Recurse
    Push-Location -LiteralPath $buildRoot
    try {
        npm ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) {throw 'Frontend dependency installation failed'}
        if ($Verify) {
            npm run typecheck
            if ($LASTEXITCODE -ne 0) {throw 'Type checking failed'}
            npm test
            if ($LASTEXITCODE -ne 0) {throw 'Frontend tests failed'}
        }
        npm run build
        if ($LASTEXITCODE -ne 0) {throw 'Frontend build failed'}
    } finally {Pop-Location}
    node (Join-Path $PSScriptRoot 'collect-licenses.cjs') (Join-Path $buildRoot 'dist/licenses') $buildRoot
    if ($LASTEXITCODE -ne 0) {throw 'Dependency notice collection failed'}
    $output=Join-Path $webRoot 'dist'
    & (Join-Path $PSScriptRoot 'clean.ps1') -FrontendOutput
    New-Item -ItemType Directory -Path $output -Force | Out-Null
    Copy-Item -Path (Join-Path $buildRoot 'dist/*') -Destination $output -Recurse -Force
} finally {
    $env:ALTASCI_VERSION=$previousVersion
    if ($KeepWorkdir) {Write-Output "Frontend work directory: $buildRoot"}
    elseif ([IO.Path]::GetDirectoryName($buildRoot) -eq $tempRoot -and [IO.Path]::GetFileName($buildRoot).StartsWith('altasci-web-')) {
        Remove-Item -LiteralPath $buildRoot -Recurse -Force
    }
}
