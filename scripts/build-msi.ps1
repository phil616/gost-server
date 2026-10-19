param([string]$Version='0.1.0',[string]$WixDll='',[string]$LicenseDirectory='web/dist/licenses')
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
if (-not (Test-Path -LiteralPath 'bin/altascitunnel.exe')) { throw 'Build bin/altascitunnel.exe first.' }
node -e 'require("./scripts/release-metadata.cjs").parseTag("v"+process.argv[1])' $Version
if ($LASTEXITCODE -ne 0) {throw 'Invalid package version'}
$msiVersion=($Version -split '-')[0]
node scripts/verify-licenses.cjs $LicenseDirectory
if ($LASTEXITCODE -ne 0) {throw 'MSI requires a complete license payload. Build the frontend first.'}
$legalArchive=Join-Path (Resolve-Path bin).Path 'installer-licenses.zip'
if (Test-Path -LiteralPath $legalArchive) {Remove-Item -LiteralPath $legalArchive}
[IO.Compression.ZipFile]::CreateFromDirectory((Resolve-Path -LiteralPath $LicenseDirectory).Path,$legalArchive)
$sourceDir=(Resolve-Path -LiteralPath 'bin').Path
$wixArguments=@('build','deploy/windows/Package.wxs','-arch','x64','-d',"SourceDir=$sourceDir",'-d',"ProjectRoot=$PWD",'-d',"Version=$msiVersion",'-o',"bin/AltasCITunnel-$Version-x64.msi")
$previousWixRollForward=$env:DOTNET_ROLL_FORWARD
try {
    $env:DOTNET_ROLL_FORWARD='Major'
    if ($WixDll) {dotnet --roll-forward Major $WixDll @wixArguments}
    else {wix @wixArguments}
    if ($LASTEXITCODE -ne 0) { throw 'WiX MSI build failed.' }
    & (Join-Path $PSScriptRoot 'verify-msi.ps1') -Installer "bin/AltasCITunnel-$Version-x64.msi" -Version $Version -WixDll $WixDll
} finally {$env:DOTNET_ROLL_FORWARD=$previousWixRollForward}
