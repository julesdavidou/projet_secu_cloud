param([switch]$SkipRestart)
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
$compose="$PSScriptRoot/compose.yaml"
function DC { & docker compose -f $compose @args; if($LASTEXITCODE -ne 0){throw 'Docker command failed'} }
function Assert($condition,[string]$label){if(-not $condition){throw $label};Write-Host "PASS $label"}
New-Item -ItemType Directory -Force "$PSScriptRoot/results"|Out-Null
DC cp web:/data/test-results/. "$PSScriptRoot/results/"
$latest=Get-ChildItem "$PSScriptRoot/results" -Filter 'network-*.json' | Sort-Object LastWriteTime -Descending | Select-Object -First 1
if(-not $latest){throw 'Run network-smoke.ps1 first'}
$test=Get-Content $latest.FullName -Raw|ConvertFrom-Json
$webPort='19290';if($env:V2_WEB_PORT){$webPort=$env:V2_WEB_PORT};$base="http://127.0.0.1:$webPort"
$centralPort='19288';if($env:V2_CENTRAL_PORT){$centralPort=$env:V2_CENTRAL_PORT}
$token=(Get-Content "$PSScriptRoot/.runtime/operator/operator.token" -Raw).Trim()
$headers=@{Authorization="Bearer $token"}
$session=New-Object Microsoft.PowerShell.Commands.WebRequestSession
function API([string]$path){Invoke-RestMethod "$base$path" -WebSession $session}
function Login {Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType application/x-www-form-urlencoded -Body @{email="$($test.run)@example.invalid";password="TestOnly-$($test.run)"} -WebSession $session}
function Reject([string]$path,[int]$code){$status=0;try{API $path|Out-Null}catch{if($_.Exception.Response){$status=[int]$_.Exception.Response.StatusCode}else{throw}};Assert ($status -eq $code) "$path rejected HTTP $code"}
Assert ((Login).client_id -eq $test.owner) 'stable Java identity before restart'
$snapshot=Invoke-RestMethod "http://127.0.0.1:$centralPort/api/v1/snapshot" -Headers $headers
$record=$snapshot.uploads.($test.video_id)
Assert ($record.state -eq 'READY') 'central SQLite lookup remains READY'
foreach($unit in $record.units){
 $agent=if($unit.node_id -eq 'A1'){'edge-a'}elseif($unit.node_id -eq 'B1'){'edge-b'}else{throw 'Local topology only'}
 if($unit.object -notmatch '^e-[a-f0-9]{64}\.bin$'){throw 'Invalid stored object path'}
 $line=DC exec -T $agent sha256sum "/data/files/tenants/project/objects/$($unit.object)"
 Assert ($line -match "^$($unit.sha256) ") "actual disk SHA256 $($unit.node_id)/$($unit.extent_id)"
}
# Alter only this run's generated fixture, preserve exact bytes in /tmp, restore in finally.
$unit=$record.units[0];$agent=if($unit.node_id -eq 'A1'){'edge-a'}else{'edge-b'}
$path="/data/files/tenants/project/objects/$($unit.object)";$backup="/tmp/$($test.run)-backup.bin"
$null=API "/api/videos/$($test.video_id)/playback"
DC exec -T $agent cp $path $backup
try {
 DC exec -T $agent sh -c 'printf x | dd of="$1" bs=1 count=1 conv=notrunc 2>/dev/null' sh $path
 Reject "/api/videos/$($test.video_id)/original" 502
 Reject "/api/videos/$($test.video_id)/playback" 502
 Reject "/media/$($test.video_id)/video.mp4" 409
} finally {
 DC exec -T $agent cp $backup $path
 DC exec -T $agent rm -- $backup
}
$line=DC exec -T $agent sha256sum $path
Assert ($line -match "^$($unit.sha256) ") 'fixture restored exactly after corruption test'
$null=API "/api/videos/$($test.video_id)/playback"
# Hide only our fixture block and restore it even if an assertion fails.
DC exec -T $agent mv $path $backup
try {Reject "/api/videos/$($test.video_id)/original" 502} finally {DC exec -T $agent mv $backup $path}
$bytes=(Invoke-WebRequest "$base/api/videos/$($test.video_id)/original" -WebSession $session).Content
if($bytes -is [string]){throw 'Expected binary MP4 response'}
$digest=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
Assert ($digest -eq $test.source_sha256) 'exact original after missing/corrupt block restoration'
# Restart an Edge broker and require fresh business replies and restored quota.
$before=(Invoke-RestMethod "http://127.0.0.1:$centralPort/api/v1/snapshot" -Headers $headers).nodes.A1.node
DC stop broker-a
try {
 Start-Sleep -Seconds 7
 $status=0
 try {Invoke-RestMethod "http://127.0.0.1:$centralPort/api/v1/rpc/edge-A1/STATUS" -Headers $headers -Method Post -ContentType application/json -Body '{}'|Out-Null}catch{$status=[int]$_.Exception.Response.StatusCode}
 Assert ($status -ne 200 -and $status -ne 0) 'offline broker gives no fabricated business ACK'
} finally {DC up -d --wait --wait-timeout 120 broker-a}
$recovered=$false
for($i=0;$i -lt 60;$i++){
 try {$reply=Invoke-RestMethod "http://127.0.0.1:$centralPort/api/v1/rpc/edge-A1/REQUEST_STATUS" -Headers $headers -Method Post -ContentType application/json -Body '{}';if($reply.node.node_id -eq 'A1'){$recovered=$true;break}}catch{}
 Start-Sleep -Seconds 1
}
Assert $recovered 'MQTT3 re-subscription and business ACK after broker restart'
$after=(Invoke-RestMethod "http://127.0.0.1:$centralPort/api/v1/snapshot" -Headers $headers).nodes.A1.node
Assert ($after.available_for_project_bytes -eq $before.available_for_project_bytes) 'no duplicated quota after reconnect'
if(-not $SkipRestart){
 DC restart central zone-a zone-b edge-a edge-b reconstructor-a reconstructor-b auth web
 DC up -d --wait --wait-timeout 180
 Reject '/api/videos' 401
 Assert ((Login).client_id -eq $test.owner) 'persisted credentials and stable ID after auth restart'
 Assert ((Login).client_id -eq $test.owner) 'owner identity persisted across restart'
 Assert (@((API '/api/videos').items.id) -contains $test.video_id) 'catalogue and ownership persisted across restart'
 $null=API "/api/videos/$($test.video_id)/playback"
 Assert $true 'Edge files reread and playback reverified after restart'
}
Write-Host 'PASS real missing/corrupt blocks refused and restored, broker reconnect, durable restart. Physical lab NOT_TESTED.'
