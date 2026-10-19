$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
New-Item -ItemType Directory -Path 'bin' -Force | Out-Null
foreach ($app in @('altasci-control','altasci-edge','altascitunnel')) {
    go build -trimpath -o "bin/$app.exe" "./cmd/$app"
    if ($LASTEXITCODE -ne 0) { throw "Build failed: $app" }
}
& (Join-Path $PSScriptRoot 'build-web.ps1')
Get-ChildItem -LiteralPath bin -File | Where-Object {$_.Extension -in @('.exe','.msi')} | Sort-Object Name | Get-FileHash -Algorithm SHA256 | ForEach-Object { '{0}  {1}' -f $_.Hash.ToLowerInvariant(),(Split-Path $_.Path -Leaf) } | Set-Content -LiteralPath 'bin/checksums.txt' -Encoding ascii
