param([Parameter(Mandatory)][string]$Archive)
$ErrorActionPreference='Stop'
$archivePath=(Resolve-Path -LiteralPath $Archive).Path
$expected=((Get-Content -LiteralPath "$archivePath.sha256" -Raw).Trim() -split '\s+',2)[0]
if ((Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) {throw 'Archive checksum mismatch'}
$zip=[IO.Compression.ZipFile]::OpenRead($archivePath)
try {
    $entries=@{}
    foreach($entry in $zip.Entries) {
        if ($entry.FullName.EndsWith('/')) {continue}
        $name=$entry.FullName
        if ($name -match '(^|/)(\.\.|\.env(?:\..*)?|node_modules|\.git|\.umi(?:-production)?)(/|$)' -or $name.Contains('\') -or $name.StartsWith('/')) {throw "Unsafe release entry: $name"}
        if ($entries.ContainsKey($name)) {throw "Duplicate ZIP entry: $name"}
        $entries[$name]=$entry
    }
    $roots=@($entries.Keys | ForEach-Object {($_ -split '/')[0]} | Sort-Object -Unique)
    if ($roots.Count -ne 1) {throw 'Release must contain exactly one root directory'}
    $prefix=$roots[0]+'/'
    function Read-Entry([string]$Name) {
        $entry=$entries[$prefix+$Name]
        if (-not $entry) {throw "Missing release file: $Name"}
        $reader=[IO.StreamReader]::new($entry.Open())
        try {$reader.ReadToEnd()} finally {$reader.Dispose()}
    }
    $manifest=(Read-Entry 'release-manifest.json') | ConvertFrom-Json
    if ($roots[0] -ne "altasci-$($manifest.version)" -or $manifest.license -ne 'MIT' -or $manifest.author -ne '@phil616') {throw 'Release metadata mismatch'}
    foreach($required in @('LICENSE','THIRD_PARTY_NOTICES.md','licenses/THIRD_PARTY_LICENSES.txt','web/dist/index.html',"bin/AltasCITunnel-$($manifest.version)-x64.msi",$manifest.sourceArchive)) {
        if (-not $entries.ContainsKey($prefix+$required)) {throw "Missing release file: $required"}
    }
    foreach($platform in @('windows','linux-amd64','linux-arm64')) {
        foreach($app in @('altasci-control','altasci-edge','altascitunnel')) {
            $name=if($platform -eq 'windows'){"bin/$app.exe"}else{"bin/$platform/$app"}
            if (-not $entries.ContainsKey($prefix+$name)) {throw "Missing runtime: $name"}
            $reader=[IO.BinaryReader]::new($entries[$prefix+$name].Open())
            try {$header=$reader.ReadBytes(4096)} finally {$reader.Dispose()}
            if ($platform -eq 'windows') {
                if ($header.Length -lt 64 -or $header[0] -ne 0x4d -or $header[1] -ne 0x5a) {throw "Not a PE binary: $name"}
                $offset=[BitConverter]::ToInt32($header,0x3c)
                if ($offset -lt 0 -or $offset+6 -gt $header.Length -or [BitConverter]::ToUInt32($header,$offset) -ne 0x4550 -or [BitConverter]::ToUInt16($header,$offset+4) -ne 0x8664) {throw "Not Windows amd64: $name"}
            } else {
                $machine=if($platform -eq 'linux-amd64'){62}else{183}
                if ($header.Length -lt 20 -or [BitConverter]::ToUInt32($header,0) -ne 0x464c457f -or $header[4] -ne 2 -or $header[5] -ne 1 -or [BitConverter]::ToUInt16($header,18) -ne $machine) {throw "Wrong ELF architecture: $name"}
            }
        }
    }
    $checked=[Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach($line in ((Read-Entry 'checksums.txt') -split '\r?\n')) {
        if (-not $line) {continue}
        if ($line -notmatch '^([0-9a-f]{64})  (.+)$') {throw 'Invalid checksum record'}
        $digest=$Matches[1];$relative=$Matches[2]
        if (-not $checked.Add($relative)) {throw "Duplicate checksum: $relative"}
        $entry=$entries[$prefix+$relative]
        if (-not $entry) {throw "Missing checksummed file: $relative"}
        $stream=$entry.Open()
        try {$actual=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)).ToLowerInvariant()} finally {$stream.Dispose()}
        if ($actual -ne $digest) {throw "Payload checksum mismatch: $relative"}
    }
    if ($checked.Count -ne $entries.Count-1 -or $checked.Contains('checksums.txt')) {throw 'Checksum manifest does not cover every payload file'}
    $licenses=(Read-Entry 'licenses/dependency-inventory.json') | ConvertFrom-Json
    if ($licenses.schemaVersion -ne 2 -or -not $licenses.dependencies.Count) {throw 'Invalid license inventory'}
    foreach($dep in $licenses.dependencies) {
        if (-not $dep.notices.Count -or $dep.license -match 'AGPL|^GPL|UNKNOWN') {throw "Unreviewed distributed dependency: $($dep.name)"}
        if ($dep.license -eq 'MPL-2.0') {
            $entry=$entries[$prefix+'licenses/'+$dep.sourceArchive]
            if (-not $entry) {throw "Missing MPL source: $($dep.name)"}
            $stream=$entry.Open()
            try {$actual=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)).ToLowerInvariant()} finally {$stream.Dispose()}
            if ($actual -ne $dep.sourceSHA256) {throw "MPL source hash mismatch: $($dep.name)"}
        }
    }
    $stream=$entries[$prefix+$manifest.sourceArchive].Open()
    $source=[IO.Compression.ZipArchive]::new($stream,[IO.Compression.ZipArchiveMode]::Read)
    try {
        $names=@($source.Entries.FullName)
        foreach($required in @('LICENSE','go.mod','go.sum','web/package-lock.json','third_party/gost-x/LICENSE','.github/workflows/ci.yml')) {
            if ($names -cnotcontains $required) {throw "Missing project source: $required"}
        }
        foreach($name in $names) {if($name -match '(^|/)(\.env(?:\..*)?|node_modules|\.git|\.umi(?:-production)?)(/|$)'){throw "Forbidden source entry: $name"}}
    } finally {$source.Dispose();$stream.Dispose()}
    Write-Output "Package OK: $($manifest.version), $($entries.Count) files, all payload hashes and license sources verified."
} finally {$zip.Dispose()}
