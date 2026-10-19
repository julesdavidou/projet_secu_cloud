#requires -Version 7.0
param([switch]$SkipRestart)
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function D {& docker @args;if($LASTEXITCODE -ne 0){throw 'Docker failed'}}
function DC {D compose -f "$PSScriptRoot/compose.yaml" @args}
function Assert($ok,[string]$label){if(-not $ok){throw $label};Write-Host "PASS $label"}
& "$PSScriptRoot/verify.ps1"
$baseline=Get-Content "$PSScriptRoot/results/verification.json" -Raw|ConvertFrom-Json -AsHashtable
$fixture=Get-Content "$PSScriptRoot/results/owner-a.json" -Raw|ConvertFrom-Json
$containers=@(D ps -a --filter label=caribou.project=caribou-secure-v3 --format '{{.ID}}')
Assert ($containers.Count -eq 4) 'four distinct owner/node storage containers'
$inventory=@();$volumes=@()
foreach($id in $containers){
    $c=(D inspect $id|ConvertFrom-Json)[0]
    $mounts=@($c.Mounts)
    Assert ($mounts.Count -eq 1 -and $mounts[0].Type -eq 'volume' -and $mounts[0].Destination -eq '/data') "one dedicated volume for $($c.Name)"
    Assert ($c.Config.User -eq '10001:10001' -and -not $c.HostConfig.Privileged -and @($c.HostConfig.PortBindings.PSObject.Properties).Count -eq 0 -and $c.HostConfig.Memory -eq 134217728) 'non-root storage, bounded memory, no host ports or engine socket'
    $volumes+=$mounts[0].Name
    $inventory+=@{container=$c.Name;owner=$c.Config.Labels.'caribou.owner';node=$c.Config.Labels.'caribou.node';volume=$mounts[0].Name;host_ports=0;docker_socket=$false}
}
Assert (@($volumes|Select-Object -Unique).Count -eq 4) 'four distinct persistent volumes'
# The preserved source is the FIRST asset in the colleague's block package;
# later extents may belong only to HLS and are not read by /original.
$unit=@($baseline.units|Where-Object {$_.slot -eq 'a' -and $_.extent_id -eq 0})[0]
Assert ($unit.object -match '^e-[a-f0-9]{64}\.bin$' -and $unit.owner_id -eq $fixture.owner) 'fault injection restricted to this generated fixture'
$target=@($inventory|Where-Object {$_.owner -eq $fixture.owner -and $_.node -eq $unit.node_id})[0]
$container=$target.container.TrimStart('/')
$path="/data/objects/tenants/$($fixture.owner)/objects/$($unit.object)"
$backup='/tmp/secure-test-'+[guid]::NewGuid().ToString('N')+'.bin'
$base='http://localhost:19690';$session=[Microsoft.PowerShell.Commands.WebRequestSession]::new()
$identity=Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType application/x-www-form-urlencoded -Body @{email='secure-owner-a@example.invalid';password='OnlyForLocalTest!2026'} -WebSession $session
Assert ($identity.client_id -eq $fixture.owner) 'Java account bound to fixture owner'
function Original {Invoke-WebRequest "$base/api/videos/$($fixture.video_id)/original" -WebSession $session -SkipHttpErrorCheck -TimeoutSec 120}
D exec $container cp $path $backup
try {
    D exec $container sh -c 'printf x | dd of="$1" bs=1 count=1 conv=notrunc 2>/dev/null' sh $path
    Assert ((D exec $container sha256sum $path) -notmatch "^$($unit.sha256) ") 'fault injection actually changed fixture bytes'
    Assert ((Original).StatusCode -eq 502) 'real corrupted block refused during fresh Edge reconstruction'
} finally {D exec $container cp $backup $path;D exec $container rm -- $backup}
D exec $container mv $path $backup
try {Assert ((Original).StatusCode -eq 502) 'real absent block refused'}finally {D exec $container mv $backup $path}
$raw=Original
Assert ($raw.StatusCode -eq 200 -and [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([byte[]]$raw.Content)).ToLowerInvariant() -eq $fixture.source_sha256) 'original restored byte for byte after both fault tests'
DC stop broker
try {& "$PSScriptRoot/verify.ps1" -PeerOnly}finally {DC up -d --wait --wait-timeout 120 broker}
Assert $true 'direct peer brokers exchange business ACK with central broker stopped'
if(-not $SkipRestart){
    foreach($id in $containers){D restart $id|Out-Null}
    DC restart central zone-a zone-b edge-a edge-b is-a is-b owner-a owner-b reconstructor-a reconstructor-b auth web
    DC up -d --wait --wait-timeout 180
    $oldSession=Invoke-WebRequest "$base/api/videos" -WebSession $session -SkipHttpErrorCheck
    Assert ($oldSession.StatusCode -eq 401) 'old Java session invalid after auth restart'
}
& "$PSScriptRoot/verify.ps1"
$after=Get-Content "$PSScriptRoot/results/verification.json" -Raw|ConvertFrom-Json -AsHashtable
foreach($zone in @('a','b')){Assert ($after.public_authorities[$zone].public_key -eq $baseline.public_authorities[$zone].public_key) "authority $zone retained its key"}
foreach($node in @('A1','B1')){Assert ($after.node_available[$node] -eq $baseline.node_available[$node]) "node $node retained quota without duplicate reservation"}
@{passed=$true;storage=$inventory;corruption='REJECTED_AND_RESTORED';missing='REJECTED_AND_RESTORED';peer_hub_down=$true;restart=(-not $SkipRestart);physical_lab='NOT_TESTED'}|ConvertTo-Json -Depth 8|Set-Content "$PSScriptRoot/results/resilience.json" -Encoding utf8
Write-Host 'PASS isolation, actual faults, hub-independent peer transport and durable restart'
