#requires -Version 7.0
[CmdletBinding(SupportsShouldProcess)]
param(
    [switch]$Dependencies,
    [switch]$LocalTools,
    [switch]$BuildOutputs,
    [switch]$FrontendOutput,
    [switch]$Staging,
    [ValidatePattern('^\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$')][string]$OldRelease,
    [ValidateRange(1,16)][int]$DeleteWorkers=8
)
$ErrorActionPreference='Stop'
$workspace=[IO.Path]::GetFullPath((Split-Path $PSScriptRoot -Parent))
$workspacePrefix=$workspace+[IO.Path]::DirectorySeparatorChar
$targets=[Collections.Generic.List[string]]::new()
foreach($relative in @('web/src/.umi','web/src/.umi-production')) {$targets.Add($relative)}
if($Dependencies){$targets.Add('web/node_modules')}
if($LocalTools){$targets.Add('.local')}
if($BuildOutputs){foreach($relative in @('bin','dist','web/dist')){$targets.Add($relative)}}
if($FrontendOutput){$targets.Add('web/dist')}
if($Staging){$targets.Add('bin/installer-licenses.zip')}
if($Staging -and (Test-Path -LiteralPath (Join-Path $workspace 'dist'))){
    Get-ChildItem -LiteralPath (Join-Path $workspace 'dist') -Directory | Where-Object {$_.Name -match '^altasci-\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$'} | ForEach-Object {$targets.Add('dist/'+$_.Name)}
}
if($Staging -and (Test-Path -LiteralPath (Join-Path $workspace 'bin'))){
    Get-ChildItem -LiteralPath (Join-Path $workspace 'bin') -File -Filter '*.wixpdb' | Where-Object {$_.Name -match '^AltasCITunnel-\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?-x64\.wixpdb$'} | ForEach-Object {$targets.Add('bin/'+$_.Name)}
}
if($OldRelease){
    foreach($relative in @("dist/altasci-$OldRelease","dist/altasci-$OldRelease.zip","dist/altasci-$OldRelease.zip.sha256","dist/altasci-$OldRelease-source.zip","dist/altasci-$OldRelease-source.zip.sha256","bin/AltasCITunnel-$OldRelease-x64.msi","bin/AltasCITunnel-$OldRelease-x64.wixpdb")){$targets.Add($relative)}
}
$fileCount=0
$bytes=0L
foreach($relative in ($targets | Select-Object -Unique)){
    $target=[IO.Path]::GetFullPath((Join-Path $workspace $relative))
    if(-not $target.StartsWith($workspacePrefix,[StringComparison]::OrdinalIgnoreCase)){throw "Outside workspace: $target"}
    if(-not (Test-Path -LiteralPath $target)){continue}
    $ancestor=$target
    while($ancestor.StartsWith($workspacePrefix,[StringComparison]::OrdinalIgnoreCase)){
        $item=Get-Item -LiteralPath $ancestor -Force
        if(($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw "Refusing linked path: $ancestor"}
        $ancestor=[IO.Path]::GetDirectoryName($ancestor)
    }
    $root=Get-Item -LiteralPath $target -Force
    if(($root.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw "Refusing linked target: $target"}
    if(-not $PSCmdlet.ShouldProcess($target,'Remove generated files')){continue}
    if(-not $root.PSIsContainer){
        $bytes+=$root.Length; $fileCount++
        if($root.IsReadOnly){$root.IsReadOnly=$false}
        [IO.File]::Delete($target)
        continue
    }
    # Walk without following links. Remove files individually; directory deletion
    # is explicitly nonrecursive and fails if anything remains or appears later.
    $pending=[Collections.Generic.Stack[string]]::new()
    $directories=[Collections.Generic.List[string]]::new()
    $files=[Collections.Generic.List[IO.FileInfo]]::new()
    $pending.Push($target)
    while($pending.Count -gt 0){
        $directory=$pending.Pop()
        $directories.Add($directory)
        foreach($entry in [IO.DirectoryInfo]::new($directory).EnumerateFileSystemInfos()){
            if(($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0){throw "Refusing linked entry: $($entry.FullName)"}
            if(($entry.Attributes -band [IO.FileAttributes]::Directory) -ne 0){$pending.Push($entry.FullName)}
            else{$bytes+=$entry.Length;$fileCount++;$files.Add($entry)}
        }
    }
    # Paths are independent. Wait for every file worker before deleting any
    # directory, and never use recursive deletion or traverse directory links.
    $files | ForEach-Object -Parallel {
        $ErrorActionPreference='Stop'
        if($_.IsReadOnly){$_.IsReadOnly=$false}
        [IO.File]::Delete($_.FullName)
    } -ThrottleLimit $DeleteWorkers
    for($i=$directories.Count-1;$i -ge 0;$i--){[IO.Directory]::Delete($directories[$i],$false)}
    Write-Output "Removed: $relative"
}
Write-Output ("Removed {0} files ({1:N1} MiB)." -f $fileCount,($bytes/1MB))
