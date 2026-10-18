param([switch]$SkipRestart)
. "$PSScriptRoot/common.ps1"
trap { Write-Host "FAIL $($_.Exception.Message)" -ForegroundColor Red; exit 1 }
$cfg=Get-Content -Raw "$PSScriptRoot/.runtime/config.json" | ConvertFrom-Json
$mode=(Get-Content -Raw "$PSScriptRoot/.runtime/mode").Trim()
$env:EDGE_MODE=$mode
if($cfg.nodes.Count -ne 2 -or $cfg.zones.Count -ne 2 -or $cfg.nodes[0].quota_bytes -ne 1048576){throw 'This destructive demo smoke requires the supplied fresh 2-node local topology (1 MiB/node). Use diagnostics for laboratory topology.'}
$source=$cfg.nodes[0].id;$other=$cfg.nodes[1].id;$zoneA=$cfg.zones[0];$zoneB=$cfg.zones[1]
$port=18088;if($env:CENTRAL_PORT){$port=[int]$env:CENTRAL_PORT};$base="http://127.0.0.1:$port"
$auth=@{Authorization="Bearer $(Get-LabSecret 'operator.token')"}
$profiles=@('--profile','local');if($mode -eq 'real-http'){$profiles+=@('--profile','real-http')}
function Assert([bool]$Condition,[string]$Message){if(-not $Condition){throw $Message};Write-Host "PASS $Message" -ForegroundColor Green}
function API([string]$Path,$Body=$null){if($null -eq $Body){return Invoke-RestMethod "$base$Path" -Headers $auth};Invoke-RestMethod "$base$Path" -Method Post -ContentType application/json -Headers $auth -Body ($Body|ConvertTo-Json -Depth 40 -Compress)}
function RPC([string]$Target,[string]$Op,$Body){API "/api/v1/rpc/$Target/$Op" $Body}
function Stats([string]$Zone){RPC "zone-$Zone" STATS @{}}
function Record([string]$Video){API "/api/v1/videos/$Video/placements"}
function Snapshot {API '/api/v1/snapshot'}
function Reject([scriptblock]$Action,[int]$Expected,[string]$Label){$status=0;try{& $Action|Out-Null}catch{if($_.Exception.Response){$status=[int]$_.Exception.Response.StatusCode}else{throw}};Assert ($status -eq $Expected) "$Label (HTTP $status)"}
function Poll([scriptblock]$Predicate,[string]$Label){for($i=0;$i -lt 45;$i++){if(& $Predicate){Write-Host "PASS $Label";return};Start-Sleep -Seconds 1};throw "timeout: $Label"}
$run='s'+[DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$dir=Join-Path $PSScriptRoot "smoke-results/$run";New-Item -ItemType Directory -Force $dir|Out-Null
function Payload([string]$Name,[int]$Length,[byte]$Fill){$bytes=New-Object byte[] $Length;for($j=0;$j -lt $Length;$j++){$bytes[$j]=$Fill};$path=Join-Path $dir $Name;[IO.File]::WriteAllBytes($path,$bytes);return $path}
function Upload([string]$Suffix,[string[]]$Files,[int]$Target){
    $segments=@();$contents=@();$sum=0
    for($i=0;$i -lt $Files.Count;$i++){$size=(Get-Item $Files[$i]).Length;$sum+=$size;$segments+=@{segment_id=$i;size_bytes=$size};$contents+=@{extent_id=$i;sha256=(Get-FileHash $Files[$i] -Algorithm SHA256).Hash.ToLowerInvariant()}}
    $request=@{manifest=@{request_id="$run-$Suffix";video_id="$run-$Suffix";source_node_id=$source;total_size_bytes=$sum;segments=$segments};target_size_bytes=$Target;contents=$contents}
    $record=API '/api/v1/uploads' $request
    return @{request=$request;record=$record}
}
function NodeToken([string]$Id){@{Authorization="Bearer $(Get-LabSecret "edge-$Id.http-token")"}}
function Send([object]$Unit,[string]$Video,[string]$File){Invoke-WebRequest -UseBasicParsing -Method Put -Uri "$($Unit.endpoint)/store/$Video/$($Unit.extent_id)" -Headers (NodeToken $Unit.node_id) -InFile $File -ContentType application/octet-stream|Out-Null}
function Complete($Upload,[string[]]$Files){
    $video=$Upload.record.upload.manifest.video_id
    if($mode -eq 'mock'){foreach($node in ($Upload.record.units.node_id|Select-Object -Unique)){RPC "edge-$node" MOCK_STORE @{video_id=$video;reservation_id='unused'}|Out-Null};Write-Host "MOCK ${video}: no file storage claimed";return}
    foreach($unit in $Upload.record.units){Send $unit $video $Files[$unit.extent_id]}
}
function VerifyFiles($Record){
    if($mode -eq 'mock'){Write-Host 'SKIPPED disk verification (MOCK mode)';return}
    foreach($unit in $Record.units){
        $agent='agent-a';if($unit.node_id -eq $other){$agent='agent-b'}
        $path="/data/tenants/project/objects/$($unit.object)"
        $line=DC @profiles exec -T $agent sha256sum $path
        Assert ($line -match "^$($unit.sha256) ") "disk SHA256 $($unit.node_id)/$($unit.extent_id)"
        $size=DC @profiles exec -T $agent stat -c %s $path
        Assert ([long]$size -eq [long]$unit.size_bytes) "disk length $($unit.node_id)/$($unit.extent_id)"
        $dest=Join-Path $dir "read-$($unit.node_id)-$($unit.extent_id).bin"
        Invoke-WebRequest -UseBasicParsing "$($unit.endpoint)/store/$($Record.upload.manifest.video_id)/$($unit.extent_id)" -Headers (NodeToken $unit.node_id) -OutFile $dest
        Assert ((Get-FileHash $dest -Algorithm SHA256).Hash.ToLowerInvariant() -eq $unit.sha256) 'HTTP reread matches expected content'
    }
}
DC @profiles up -d --wait --wait-timeout 120
Assert ((Invoke-RestMethod "$base/ready").ready) 'Central ready'
Reject {Invoke-WebRequest -UseBasicParsing "$base/api/v1/snapshot"} 401 'unauthenticated control API denied'
Poll { $s=Snapshot;@($s.nodes.PSObject.Properties).Count -eq 2 -and (Stats $zoneA).summary.available_for_project_bytes -eq 1048576 -and (Stats $zoneB).summary.available_for_project_bytes -eq 1048576 } 'two registered live nodes, provisioned project quota'
# Reserve directly for 2 seconds to prove release without storing files.
$before=(Stats $zoneA).summary.available_for_project_bytes
$expiry=@{request_id="$run-expire";video_id="$run-expire";reservation_id="$run-expire";extents=@(@{extent_id=0;first_segment=0;last_segment=0;size_bytes=64});expires_at=[DateTime]::UtcNow.AddSeconds(2).ToString('o')}
RPC "zone-$zoneA" RESERVE $expiry|Out-Null
Assert ((Stats $zoneA).summary.available_for_project_bytes -eq ($before-64)) 'reservation debited once'
Start-Sleep -Seconds 3
Assert ((Stats $zoneA).summary.available_for_project_bytes -eq $before) 'expired reservation capacity released'
$bOffers=(Stats $zoneB).offers
$small=Payload small.bin 16384 37
$local=Upload local @($small) 16384
Assert (@($local.record.units|Where-Object zone_id -ne $zoneA).Count -eq 0) 'local upload stays in source zone'
Assert ((Stats $zoneB).offers -eq $bOffers) 'zero remote OFFER for local fit'
Assert ((Record "$run-local").state -ne 'READY') 'PREPARE_STORE never means READY'
Complete $local @($small)
Poll {(Record "$run-local").state -eq 'READY'} 'local completion acknowledged'
$first=Payload first.bin 614400 41;$second=Payload second.bin 614400 43
$remote=Upload remote @($first,$second) 614400
Assert (@($remote.record.units.node_id|Select-Object -Unique).Count -eq 2) 'Go plan uses two distinct Edges'
Assert ((Stats $zoneB).offers -gt $bOffers) 'remote OFFER/RESERVE exercised'
if($mode -eq 'real-http'){
    $unit=$remote.record.units[0];$bad=Payload corrupt.bin 614400 99;$short=Payload short.bin 100 41
    Reject {Send $unit "$run-remote" $bad} 422 'wrong SHA256 rejected by real storage'
    Reject {Send $unit "$run-remote" $short} 422 'truncated unit rejected'
    Assert ((Record "$run-remote").state -ne 'READY') 'rejected writes never READY'
    Send $unit "$run-remote" $first
    Assert ((Record "$run-remote").state -ne 'READY') 'one complete extent does not complete video'
    Send $remote.record.units[1] "$run-remote" $second
}else{Complete $remote @($first,$second)}
Poll {(Record "$run-remote").state -eq 'READY'} 'two completed extents make READY'
$record=Record "$run-remote";VerifyFiles $record
$eventCount=(Snapshot).catalog.processed_events
$duplicate=API '/api/v1/uploads' $remote.request
foreach($node in ($remote.record.units.node_id|Select-Object -Unique)){
    RPC "edge-$node" REPLAY_COMPLETE @{video_id="$run-remote";reservation_id='unused'}|Out-Null
}
if($mode -eq 'real-http'){Send $remote.record.units[0] "$run-remote" $first}else{Complete $remote @($first,$second)}
Assert ((Snapshot).catalog.processed_events -eq $eventCount) 'duplicate MQTT business requests produce no second central mutation'
$tooLarge=Payload too-large.bin 2097152 47
Reject {Upload reject @($tooLarge) 2097152} 409 'extent exceeding every quota refused'
RPC "edge-$other" CONTROL @{state='AVAILABLE';pause=$true}|Out-Null
Start-Sleep -Seconds ($cfg.node_ttl_seconds+2)
RPC "edge-$other" RETAINED_ONLY @{}|Out-Null
Start-Sleep -Seconds 1
Assert ((Stats $zoneB).summary.available_for_project_bytes -eq 0) 'retained AVAILABLE without fresh heartbeat is ineligible'
RPC "edge-$other" CONTROL @{state='AVAILABLE';pause=$false}|Out-Null
if(-not $SkipRestart){
    $restart=@('central','zone-a','zone-b','edge-a');if($mode -eq 'real-http'){$restart+='agent-a'}
    DC @profiles restart @restart
    DC @profiles up -d --wait --wait-timeout 120
    Poll {(Record "$run-remote").state -eq 'READY'} 'catalog READY survives controller and Edge restart'
    Assert ((Snapshot).catalog.processed_events -eq $eventCount) 'restart does not replay mutations'
    VerifyFiles (Record "$run-remote")
}else{Write-Host 'SKIPPED controller/Edge restart (requested)'}
$locationBefore=(Record "$run-remote").units|Where-Object node_id -eq $source
RPC "edge-$source" MOVE @{node_id=$source;zone_id=$zoneB;generation=0}|Out-Null
Poll { $now=Record "$run-remote";@($now.units|Where-Object { $_.node_id -eq $source -and $_.zone_id -eq $zoneB }).Count -eq @($locationBefore).Count } 'node mobility changes zone lookup without rewriting extents'
VerifyFiles (Record "$run-remote")
RPC "edge-$source" MOVE @{node_id=$source;zone_id=$zoneA;generation=0}|Out-Null
Snapshot|ConvertTo-Json -Depth 40|Set-Content -Encoding utf8 (Join-Path $dir snapshot.json)
Write-Host "PASS complete $mode smoke; evidence $dir"
Write-Host 'NOT_TESTED physical Raspberry Pi/Jetson and external laboratory network. SKIPPED live/SIS/RTP (outside scope).'
