# Executed INSIDE Windows Sandbox by the .wsb configuration generated below.
param([string]$InputDirectory='C:\input',[string]$ResultsDirectory='C:\results')
$ErrorActionPreference='Stop'
if ($env:USERNAME -ne 'WDAGUtilityAccount') { throw 'This destructive installation acceptance script runs only in Windows Sandbox.' }
New-Item -ItemType Directory -Force $ResultsDirectory | Out-Null
Start-Transcript -Path "$ResultsDirectory\transcript.txt" -Force
$checks=New-Object System.Collections.Generic.List[string]
$children=New-Object System.Collections.Generic.List[System.Diagnostics.Process]
$agent='C:\Program Files\AltasCITunnel\altascitunnel.exe'
function Check([bool]$Condition,[string]$Name) { if (-not $Condition) { throw "FAIL: $Name" }; $checks.Add($Name); Write-Host "PASS: $Name" }
function Wait-Until([scriptblock]$Condition,[string]$Name) {
    $deadline=(Get-Date).AddSeconds(45)
    do { if (& $Condition) { return }; Start-Sleep -Milliseconds 300 } while ((Get-Date) -lt $deadline)
    throw "Timeout: $Name"
}
function MSI([string]$Name,[string[]]$Arguments) {
    $p=Start-Process msiexec.exe -ArgumentList ($Arguments+@('/qn','/norestart','/l*v',"$ResultsDirectory\$Name.log")) -Wait -PassThru -WindowStyle Hidden
    Check ($p.ExitCode -in @(0,3010)) "$Name MSI exit=$($p.ExitCode)"
    if ($Name -ne 'uninstall') { Wait-Until { $s=& $agent status 2>$null; $LASTEXITCODE -eq 0 } 'service IPC ready' }
}
function Status { $s=& $agent status; if ($LASTEXITCODE -ne 0) { throw 'IPC status failed' }; return ($s | ConvertFrom-Json) }
function Data-Roundtrip($Ready,[string]$Stage) {
    foreach ($visitor in $Ready.visitors) {
        $protocol=$visitor.protocol; $port=if($protocol -eq 'tcp'){29111}else{29112}
        $listen="${protocol}://127.0.0.1:$port/203.0.113.9:65000"
        if($protocol -eq 'udp') { $listen+='?keepalive=true&ttl=60s' }
        $p=Start-Process "$InputDirectory\gost.exe" -ArgumentList @('-L',$listen,'-F',$visitor.forward) -PassThru -WindowStyle Hidden -RedirectStandardError "$ResultsDirectory\visitor-$Stage-$protocol.log"
        $children.Add($p); Start-Sleep -Seconds 2
        $payload=[Text.Encoding]::UTF8.GetBytes("installed-$Stage-$protocol")
        try {
            if($protocol -eq 'tcp') {
                $c=New-Object Net.Sockets.TcpClient; $c.Connect('127.0.0.1',$port)
                $stream=$c.GetStream(); $stream.ReadTimeout=10000; $stream.Write($payload,0,$payload.Length)
                $b=New-Object byte[] $payload.Length; $offset=0
                while($offset -lt $b.Length){ $n=$stream.Read($b,$offset,$b.Length-$offset); if($n -eq 0){throw 'EOF'};$offset+=$n }
            } else {
                $c=New-Object Net.Sockets.UdpClient; $c.Client.ReceiveTimeout=10000; $c.Connect('127.0.0.1',$port)
                [void]$c.Send($payload,$payload.Length); $remote=New-Object Net.IPEndPoint([Net.IPAddress]::Any,0);$b=$c.Receive([ref]$remote)
            }
            Check ([Text.Encoding]::UTF8.GetString($b) -eq [Text.Encoding]::UTF8.GetString($payload)) "$Stage $protocol WSS fixed-target roundtrip"
        } finally { if($c){$c.Close()};if(-not $p.HasExited){Stop-Process -Id $p.Id -Force};Start-Sleep -Milliseconds 400 }
    }
}
try {
    # Sandbox-only workaround for the MSI signature verification delay:
    # https://github.com/microsoft/Windows-Sandbox/issues/68
    Set-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy' -Name VerifiedAndReputablePolicyState -Value 0
    & CiTool.exe -r
    if ($LASTEXITCODE -ne 0) { throw 'Sandbox CI policy refresh failed' }
    $fixtureDir='C:\fixture'
    $fixture=Start-Process "$InputDirectory\systemfixture.exe" -ArgumentList @('--output',$fixtureDir) -PassThru -WindowStyle Hidden -RedirectStandardError "$ResultsDirectory\fixture.log"
    $children.Add($fixture)
    Wait-Until {Test-Path "$fixtureDir\info.json"} 'fixture startup'
    Import-Certificate -FilePath "$fixtureDir\ca.pem" -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
    MSI 'install' @('/i',"$InputDirectory\AltasCITunnel-0.1.0-x64.msi")
    $svc=Get-CimInstance Win32_Service -Filter "Name='AltasCITunnel'"
    Check ($svc.StartName -eq 'NT AUTHORITY\LocalService' -and $svc.State -eq 'Running') 'SCM runs service as LocalService'
    Check ((Status).state -eq 'unregistered') 'new installation is unregistered'
    Check (@(([Environment]::GetEnvironmentVariable('PATH','Machine') -split ';') | ForEach-Object {$_.TrimEnd('\')}) -contains (Split-Path $agent)) 'system PATH installed'
    & icacls.exe C:\ProgramData\AltasCITunnel | Out-File "$ResultsDirectory\state-acl.txt"
    & sc.exe qfailure AltasCITunnel | Out-File "$ResultsDirectory\service-recovery.txt"
    $info=Get-Content "$fixtureDir\info.json" -Raw | ConvertFrom-Json
    & $agent register --server $info.server --token $info.token
    Check ($LASTEXITCODE -eq 0) 'registered using installed service IPC'
    Wait-Until {Test-Path "$fixtureDir\ready.json"} 'authenticated provider bindings'
    $ready=Get-Content "$fixtureDir\ready.json" -Raw | ConvertFrom-Json
    $identity=Get-Content C:\ProgramData\AltasCITunnel\identity.json -Raw | ConvertFrom-Json
    Check ($identity.client_id -eq $ready.client_id -and $identity.client_id.Length -gt 0) 'server assigned persistent ClientID'
    Data-Roundtrip $ready 'install'
    $pidBefore=(Get-CimInstance Win32_Service -Filter "Name='AltasCITunnel'").ProcessId
    Stop-Process -Id $pidBefore -Force
    Wait-Until { $svc=Get-CimInstance Win32_Service -Filter "Name='AltasCITunnel'"; $svc.State -eq 'Running' -and $svc.ProcessId -ne $pidBefore } 'SCM crash recovery'
    Wait-Until { $s=& $agent status 2>$null; $LASTEXITCODE -eq 0 } 'IPC after crash'
    Check ($true) 'SCM automatically restarts crashed service'
    MSI 'repair' @('/fa',"$InputDirectory\AltasCITunnel-0.1.0-x64.msi")
    Check ((Get-Content C:\ProgramData\AltasCITunnel\identity.json -Raw | ConvertFrom-Json).private_key -eq $identity.private_key) 'repair preserves identity'
    Data-Roundtrip $ready 'repair'
    MSI 'upgrade' @('/i',"$InputDirectory\AltasCITunnel-0.1.1-x64.msi")
    $upgraded=Get-Content C:\ProgramData\AltasCITunnel\identity.json -Raw | ConvertFrom-Json
    Check ($upgraded.private_key -eq $identity.private_key -and $upgraded.client_id -eq $identity.client_id) 'upgrade preserves registered identity'
    Data-Roundtrip $ready 'upgrade'
    MSI 'uninstall' @('/x',"$InputDirectory\AltasCITunnel-0.1.1-x64.msi")
    Check (-not (Get-Service AltasCITunnel -ErrorAction SilentlyContinue)) 'uninstall removes service'
    Check (-not (Test-Path $agent)) 'uninstall removes executable'
    Check (-not (Test-Path C:\ProgramData\AltasCITunnel\identity.json)) 'uninstall removes identity'
    Check (-not (Test-Path C:\ProgramData\AltasCITunnel\config.json)) 'uninstall removes credentials/config'
    Check (-not (@(([Environment]::GetEnvironmentVariable('PATH','Machine') -split ';') | ForEach-Object {$_.TrimEnd('\')}) -contains (Split-Path $agent))) 'uninstall removes system PATH entry'
    @{passed=$true;checks=@($checks);os=(Get-CimInstance Win32_OperatingSystem).Caption;completed=(Get-Date).ToString('o')} | ConvertTo-Json -Depth 5 | Set-Content "$ResultsDirectory\result.json" -Encoding UTF8
} catch {
    $_ | Out-String | Set-Content "$ResultsDirectory\failure.txt" -Encoding UTF8
    @{passed=$false;checks=@($checks);error=$_.ToString()} | ConvertTo-Json -Depth 5 | Set-Content "$ResultsDirectory\result.json" -Encoding UTF8
} finally {
    foreach($p in $children){if(-not $p.HasExited){Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue}}
    Stop-Transcript
}
