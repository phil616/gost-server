param([string]$Version='0.1.0',[string]$WixDll='',[string]$LicenseDirectory='web/dist/licenses')
$ErrorActionPreference='Stop'
function Test-MsiPayload {
    param([Parameter(Mandatory)][string]$Installer,[Parameter(Mandatory)][string]$Version,[string]$WixDll='')
    if (-not $IsWindows) {throw 'MSI inspection requires Windows'}
    $projectRoot=Split-Path $PSScriptRoot -Parent
    $msiPath=(Resolve-Path -LiteralPath $Installer).Path
    $tempParent=[IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar)
    $probe=Join-Path $tempParent ('altasci-msi-verify-'+[Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $probe | Out-Null
    $installerApi=$null;$db=$null
    try {
        $installerApi=New-Object -ComObject WindowsInstaller.Installer
        $db=$installerApi.OpenDatabase($msiPath,0)
        $view=$db.OpenView('SELECT `Value` FROM `Property` WHERE `Property` = ''ProductVersion''')
        try {
            $view.Execute();$record=$view.Fetch()
            if ($record.StringData(1) -ne ($Version -split '-')[0]) {throw 'MSI ProductVersion mismatch'}
        } finally {$view.Close()}
        $arguments=@('msi','decompile','-x',(Join-Path $probe 'payload'),'-o',(Join-Path $probe 'package.wxs'),$msiPath)
        if ($WixDll) {dotnet --roll-forward Major $WixDll @arguments} else {wix @arguments}
        if ($LASTEXITCODE -ne 0) {throw 'MSI extraction failed'}
        foreach($pair in @(
            @('AgentExecutable','bin/altascitunnel.exe'),
            @('ProjectLicense','LICENSE'),
            @('DependencyNotice','THIRD_PARTY_NOTICES.md')
        )) {
            $actual=Join-Path $probe ('payload/File/'+$pair[0])
            if ((Get-FileHash -LiteralPath $actual -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath (Join-Path $projectRoot $pair[1]) -Algorithm SHA256).Hash) {throw "MSI payload mismatch: $($pair[0])"}
        }
        $legal=Join-Path $probe 'licenses'
        [IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $probe 'payload/File/DependencyLicenses'),$legal)
        node (Join-Path $PSScriptRoot 'licenses.cjs') --verify $legal
        if ($LASTEXITCODE -ne 0) {throw 'Embedded MSI license validation failed'}
        Write-Output "MSI OK: $Version, extracted client/license/notice hashes and embedded MPL sources verified."
    } finally {
        if ($db) {[Runtime.InteropServices.Marshal]::FinalReleaseComObject($db) | Out-Null}
        if ($installerApi) {[Runtime.InteropServices.Marshal]::FinalReleaseComObject($installerApi) | Out-Null}
        if ([IO.Path]::GetDirectoryName($probe) -eq $tempParent -and [IO.Path]::GetFileName($probe).StartsWith('altasci-msi-verify-')) {
            Remove-Item -LiteralPath $probe -Recurse -Force
        }
    }
}
Set-Location (Split-Path $PSScriptRoot -Parent)
if (-not (Test-Path -LiteralPath 'bin/altascitunnel.exe')) { throw 'Build bin/altascitunnel.exe first.' }
node -e 'require("./scripts/release-metadata.cjs").parseTag("v"+process.argv[1])' $Version
if ($LASTEXITCODE -ne 0) {throw 'Invalid package version'}
$msiVersion=($Version -split '-')[0]
node scripts/licenses.cjs --verify $LicenseDirectory
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
    Test-MsiPayload -Installer "bin/AltasCITunnel-$Version-x64.msi" -Version $Version -WixDll $WixDll
} finally {$env:DOTNET_ROLL_FORWARD=$previousWixRollForward}
