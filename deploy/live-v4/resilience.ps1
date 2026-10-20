$ErrorActionPreference='Stop'
$runtime="$PSScriptRoot/.runtime";$results="$PSScriptRoot/results"
function D {& docker @args;if($LASTEXITCODE -ne 0){throw 'Docker failed'}}
function DC {D compose --project-directory $PSScriptRoot -f "$runtime/compose.json" @args}
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
function Verify([string]$phase){D run --rm --user "${uidValue}:10001" --cap-drop ALL --security-opt no-new-privileges:true --network caribou-live-v4_default -e "LIVE_VERIFY_PHASE=$phase" --mount "type=bind,source=$PSScriptRoot/live-verify.py,target=/test.py,readonly" --mount "type=bind,source=$runtime/pki/admin,target=/pki,readonly" --mount "type=bind,source=$runtime/operator,target=/operator,readonly" --mount "type=bind,source=$results,target=/fixture" caribou/live-v4-security:dev python /test.py}
Verify before
$fixture=Get-Content "$results/live.json" -Raw|ConvertFrom-Json;$target=Get-Content "$results/fault-target.json" -Raw|ConvertFrom-Json
if($target.object -notmatch '^e-[a-f0-9]{64}\.bin$' -or $target.video_id -notin @($fixture.verified_fragments.video_id) -or $target.owner_id -notmatch '^client-[a-f0-9-]+$'){throw 'Refusing fault target outside this generated fixture'}
$ids=@(D ps -q --filter label=caribou.project=caribou-live-v4 --filter "label=caribou.owner=$($target.owner_id)" --filter "label=caribou.node=$($target.node_id)")
if($ids.Count -ne 1){throw 'Expected exact isolated owner storage'}
$container=$ids[0];$file="/data/objects/tenants/$($target.owner_id)/objects/$($target.object)";$backup='/tmp/live-test-'+[guid]::NewGuid().ToString('N')+'.bin'
$session=[Microsoft.PowerShell.Commands.WebRequestSession]::new()
function Login {Invoke-RestMethod http://localhost:20690/api/auth/login -Method Post -WebSession $session -ContentType application/x-www-form-urlencoded -Body @{email='secure-owner-a@example.invalid';password='OnlyForLocalTest!2026'}|Out-Null}
function Segment {Invoke-WebRequest "http://localhost:20690/api/live/$($fixture.live_id)/segments/0/segment.m4s" -WebSession $session -SkipHttpErrorCheck -TimeoutSec 120}
Login
D exec $container cp $file $backup
try{
 D exec $container sh -c 'printf x | dd of="$1" bs=1 count=1 conv=notrunc 2>/dev/null' sh $file
 DC restart web|Out-Null;DC up -d --wait --wait-timeout 180|Out-Null
 if((Segment).StatusCode -ne 502){throw 'Corrupt live block was not refused on cold reconstruction'}
}finally{D exec $container cp $backup $file;D exec $container rm -- $backup}
if((Segment).StatusCode -ne 200){throw 'Restored live fragment unreadable'}
D exec $container mv $file $backup
try{
 DC restart web|Out-Null;DC up -d --wait --wait-timeout 180|Out-Null
 if((Segment).StatusCode -ne 502){throw 'Absent live block was not refused'}
}finally{D exec $container mv $backup $file}
foreach($id in @(D ps -aq --filter label=caribou.project=caribou-live-v4)){D restart $id|Out-Null}
DC restart central zone-a zone-b edge-a edge-b is-a is-b owner-a owner-b reconstructor-a reconstructor-b web auth|Out-Null
DC up -d --wait --wait-timeout 240|Out-Null
Verify after
@{passed=$true;live_id=$fixture.live_id;corruption='REFUSED_AND_RESTORED';absence='REFUSED_AND_RESTORED';restart='SAME_HEAD_ORDER_END_PRIVACY_AND_BYTES';physical_lab='NOT_TESTED'}|ConvertTo-Json|Set-Content "$results/live-resilience.json" -Encoding utf8
Write-Host 'PASS live corruption/absence and durable full restart; previous files retained'
