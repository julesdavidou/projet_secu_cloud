param([switch]$SkipRestart)
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
$compose="$PSScriptRoot/compose.yaml"
function DC { & docker compose -f $compose @args; if($LASTEXITCODE -ne 0){throw 'Docker command failed'} }
function Assert($condition,[string]$label){if(-not $condition){throw $label};Write-Host "PASS $label"}
New-Item -ItemType Directory -Force "$PSScriptRoot/results"|Out-Null
DC cp web:/data/test-results/. "$PSScriptRoot/results/"
$latest=Get-ChildItem "$PSScriptRoot/results" -Filter 'smoke-*.json' | Sort-Object LastWriteTime -Descending | Select-Object -First 1
if(-not $latest){throw 'Run smoke.ps1 first'}
$test=Get-Content $latest.FullName -Raw|ConvertFrom-Json
$webPort='18090';if($env:WEB_PORT){$webPort=$env:WEB_PORT};$base="http://127.0.0.1:$webPort"
$centralPort='19088';if($env:CENTRAL_PORT){$centralPort=$env:CENTRAL_PORT}
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
 $agent=if($unit.node_id -eq 'A1'){'agent-a'}elseif($unit.node_id -eq 'B1'){'agent-b'}else{throw 'Local topology only'}
 if($unit.object -notmatch '^e-[a-f0-9]{64}\.bin$'){throw 'Invalid stored object path'}
 $line=DC exec -T $agent sha256sum "/data/tenants/project/objects/$($unit.object)"
 Assert ($line -match "^$($unit.sha256) ") "actual disk SHA256 $($unit.node_id)/$($unit.extent_id)"
}
# Alter only this run's generated fixture, preserve exact bytes in /tmp, restore in finally.
$unit=$record.units[0];$agent=if($unit.node_id -eq 'A1'){'agent-a'}else{'agent-b'}
$path="/data/tenants/project/objects/$($unit.object)";$backup="/tmp/$($test.run)-backup.bin"
$null=API "/api/videos/$($test.video_id)/playback"
DC exec -T $agent cp $path $backup
try {
 DC exec -T $agent sh -c 'printf x | dd of="$1" bs=1 count=1 conv=notrunc 2>/dev/null' sh $path
 Reject "/api/videos/$($test.video_id)/playback" 502
 Reject "/media/$($test.video_id)/video.mp4" 409
} finally {
 DC exec -T $agent cp $backup $path
 DC exec -T $agent rm -- $backup
}
$line=DC exec -T $agent sha256sum $path
Assert ($line -match "^$($unit.sha256) ") 'fixture restored exactly after corruption test'
$null=API "/api/videos/$($test.video_id)/playback"
if(-not $SkipRestart){
 DC restart central zone-a zone-b edge-a edge-b agent-a agent-b auth web
 DC up -d --wait --wait-timeout 180
 Reject '/api/videos' 401
 Assert ((Login).client_id -eq $test.owner) 'persisted credentials and stable ID after auth restart'
 Assert ((API '/api/me').name -eq 'Integration Test') 'profile persisted across restart'
 Assert (@((API '/api/videos').items.id) -contains $test.video_id) 'catalogue and ownership persisted across restart'
 $null=API "/api/videos/$($test.video_id)/playback"
 Assert $true 'Edge files reread and playback reverified after restart'
}
Write-Host 'PASS real disk corruption refused, bytes restored, durable metadata checked. Physical lab NOT_TESTED.'
