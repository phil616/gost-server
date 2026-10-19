param([string]$Version='0.1.0',[string]$FrontendDist='web/dist')
$ErrorActionPreference='Stop'
$projectRoot=Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $projectRoot
node -e 'require("./scripts/release-metadata.cjs").parseTag("v"+process.argv[1])' $Version
if ($LASTEXITCODE -ne 0) {throw 'Invalid release version'}
$frontendRoot=(Resolve-Path -LiteralPath $FrontendDist).Path
if (-not (Test-Path -LiteralPath (Join-Path $frontendRoot 'index.html'))) {throw 'Build the frontend first'}
$webManifest=Get-Content -LiteralPath (Join-Path $frontendRoot 'license-modules.json') -Raw | ConvertFrom-Json
if ($webManifest.version -ne $Version) {throw 'Frontend version mismatch. Rebuild with build-web.ps1 -Version matching this package.'}
node scripts/verify-licenses.cjs (Join-Path $frontendRoot 'licenses')
if ($LASTEXITCODE -ne 0) {throw 'Incomplete release licenses'}
$releaseRoot=Join-Path $projectRoot "dist/altasci-$Version"
$archive=Join-Path $projectRoot "dist/altasci-$Version.zip"
foreach($output in @($releaseRoot,$archive,"dist/altasci-$Version-source.zip")) {
    if (Test-Path -LiteralPath $output) {throw "Refusing to overwrite: $output"}
}
$installer="bin/AltasCITunnel-$Version-x64.msi"
foreach ($file in @('bin/altasci-control.exe','bin/altasci-edge.exe','bin/altascitunnel.exe',$installer)) {
    if (-not (Test-Path -LiteralPath $file)) {throw "Missing required artifact: $file"}
}
foreach ($arch in @('linux-amd64','linux-arm64')) {
    foreach ($app in @('altasci-control','altasci-edge','altascitunnel')) {
        if (-not (Test-Path -LiteralPath "bin/$arch/$app")) {throw "Missing required artifact: bin/$arch/$app"}
    }
}
New-Item -ItemType Directory -Path "$releaseRoot/bin","$releaseRoot/web" -Force | Out-Null
foreach ($app in @('altasci-control','altasci-edge','altascitunnel')) {Copy-Item -LiteralPath "bin/$app.exe" -Destination "$releaseRoot/bin"}
Copy-Item -LiteralPath $installer -Destination "$releaseRoot/bin"
foreach ($arch in @('linux-amd64','linux-arm64')) {
    New-Item -ItemType Directory -Path "$releaseRoot/bin/$arch" | Out-Null
    foreach ($app in @('altasci-control','altasci-edge','altascitunnel')) {Copy-Item -LiteralPath "bin/$arch/$app" -Destination "$releaseRoot/bin/$arch"}
}
Copy-Item -LiteralPath $frontendRoot -Destination "$releaseRoot/web/dist" -Recurse
foreach ($item in @('deploy','scripts','docs','patches','README.md','LICENSE','THIRD_PARTY_NOTICES.md','CONTRIBUTING.md','SECURITY.md','Dockerfile','.dockerignore','compose.yaml','compose.env.example')) {
    Copy-Item -LiteralPath $item -Destination $releaseRoot -Recurse
}
Copy-Item -LiteralPath (Join-Path $frontendRoot 'licenses') -Destination "$releaseRoot/licenses" -Recurse

# Include this working tree's source, including new files, but never ignored secrets
# or build outputs. Do not git archive HEAD: it can silently omit uncommitted work.
$sourceArchive=Join-Path $projectRoot "dist/altasci-$Version-source.zip"
$sourceZip=[IO.Compression.ZipFile]::Open($sourceArchive,[IO.Compression.ZipArchiveMode]::Create)
try {
    $sourceFiles=git -c core.quotepath=false ls-files --cached --others --exclude-standard
    if ($LASTEXITCODE -ne 0) {throw 'Cannot enumerate source checkout'}
    foreach ($file in ($sourceFiles | Sort-Object -Unique)) {
        if ($file -notmatch '^(cmd/|internal/|web/|third_party/|deploy/|scripts/|docs/|patches/|\.github/|[^/]+$)') {continue}
        if ($file -match '(^|/)(\.env(?:\..*)?|node_modules|\.git|\.umi(?:-production)?|test-data|\.test-data)(/|$)' -or $file -match '\.(key|pfx|msi|log)$') {throw "Forbidden source path: $file"}
        if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {continue} # tracked deletion
        if ((Get-Item -LiteralPath $file).Attributes -band [IO.FileAttributes]::ReparsePoint) {throw "Source link not allowed: $file"}
        [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($sourceZip,(Join-Path $projectRoot $file),$file,[IO.Compression.CompressionLevel]::Optimal) | Out-Null
    }
} finally {$sourceZip.Dispose()}
New-Item -ItemType Directory -Path "$releaseRoot/sources" | Out-Null
Copy-Item -LiteralPath $sourceArchive -Destination "$releaseRoot/sources"
$commit=git rev-parse HEAD
if ($LASTEXITCODE -ne 0) {throw 'Cannot determine source commit'}
$dirty= [bool](git status --porcelain --untracked-files=normal)
@{
    schemaVersion=1;version=$Version;msiVersion=($Version -split '-')[0];commit=$commit;dirty=$dirty
    builtAtUTC=[DateTime]::UtcNow.ToString('o');license='MIT';author='@phil616'
    platforms=@('windows-amd64','linux-amd64','linux-arm64');sourceArchive="sources/altasci-$Version-source.zip"
} | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath "$releaseRoot/release-manifest.json" -Encoding utf8NoBOM
$hashes=Get-ChildItem -LiteralPath $releaseRoot -Recurse -File | Sort-Object FullName | ForEach-Object {
    $relative=[IO.Path]::GetRelativePath($releaseRoot,$_.FullName).Replace('\','/')
    '{0}  {1}' -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(),$relative
}
$hashes | Set-Content -LiteralPath "$releaseRoot/checksums.txt" -Encoding utf8NoBOM
# ZipFile preserves dotfiles; Compress-Archive omits hidden files on some hosts.
[IO.Compression.ZipFile]::CreateFromDirectory($releaseRoot,$archive,[IO.Compression.CompressionLevel]::Optimal,$true)
foreach($file in @($archive,$sourceArchive)) {
    '{0}  {1}' -f (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant(),(Split-Path $file -Leaf) |
        Set-Content -LiteralPath "$file.sha256" -Encoding ascii
}
& (Join-Path $PSScriptRoot 'verify-package.ps1') -Archive $archive
