param([Parameter(Mandatory)][string]$Archive)
$ErrorActionPreference='Stop'
$original=(Resolve-Path -LiteralPath $Archive).Path
$tempParent=[IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar)
$probe=Join-Path $tempParent ('altasci-package-test-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $probe | Out-Null
try {
    $copy=Join-Path $probe 'altered.zip'
    Copy-Item -LiteralPath $original -Destination $copy
    Copy-Item -LiteralPath "$original.sha256" -Destination "$copy.sha256"
    $zip=[IO.Compression.ZipFile]::Open($copy,[IO.Compression.ZipArchiveMode]::Update)
    try {
        $readme=@($zip.Entries | Where-Object {$_.FullName -match '^altasci-[^/]+/README.md$'})
        if ($readme.Count -ne 1) {throw 'Unexpected archive layout'}
        $name=$readme[0].FullName;$readme[0].Delete()
        $writer=[IO.StreamWriter]::new($zip.CreateEntry($name).Open())
        try {$writer.Write('deliberately modified payload')} finally {$writer.Dispose()}
    } finally {$zip.Dispose()}
    $rejected=$false
    try {& (Join-Path $PSScriptRoot 'verify-package.ps1') -Archive $copy} catch {
        if ($_.Exception.Message -notmatch 'Archive checksum mismatch') {throw}
        $rejected=$true
    }
    if (-not $rejected) {throw 'Corrupt archive was accepted'}
    # Even if an intermediary recalculates the outer hash, the payload gate fails.
    '{0}  altered.zip' -f (Get-FileHash -LiteralPath $copy -Algorithm SHA256).Hash.ToLowerInvariant() |
        Set-Content -LiteralPath "$copy.sha256" -Encoding ascii
    $rejected=$false
    try {& (Join-Path $PSScriptRoot 'verify-package.ps1') -Archive $copy} catch {
        if ($_.Exception.Message -notmatch 'Payload checksum mismatch: README.md') {throw}
        $rejected=$true
    }
    if (-not $rejected) {throw 'Corrupt payload with a recalculated outer hash was accepted'}
    Write-Output 'Package rejection tests OK: changed archive and changed payload with recalculated outer hash both rejected.'
} finally {
    if ([IO.Path]::GetDirectoryName($probe) -eq $tempParent -and [IO.Path]::GetFileName($probe).StartsWith('altasci-package-test-')) {
        Remove-Item -LiteralPath $probe -Recurse -Force
    }
}
