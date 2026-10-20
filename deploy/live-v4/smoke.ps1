$ErrorActionPreference='Stop'
$base=[IO.Path]::GetFullPath("$PSScriptRoot/../secure-v3");$runtime="$PSScriptRoot/.runtime";$results="$PSScriptRoot/results";New-Item -ItemType Directory -Force $results|Out-Null
function D {& docker @args;if($LASTEXITCODE -ne 0){throw 'Docker failed'}}
function DC {D compose --project-directory $PSScriptRoot -f "$runtime/compose.json" @args}
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
function Hold([string]$action,[string]$slot){D run --rm --user "${uidValue}:10001" --cap-drop ALL --security-opt no-new-privileges:true --network caribou-live-v4_default -e "HOLD_ACTION=$action" -e "HOLD_SLOT=$slot" --mount "type=bind,source=$base/control-test.py,target=/test.py,readonly" --mount "type=bind,source=$runtime/pki/admin,target=/pki,readonly" --mount "type=bind,source=$runtime/operator,target=/operator,readonly" --mount "type=bind,source=$results,target=/fixture" caribou/live-v4-security:dev python /test.py}
foreach($slot in @('a','b')){
 $port=if($slot -eq 'a'){20591}else{20592};$identity=Invoke-RestMethod "http://localhost:$port/owner/login" -Method Post -ContentType application/json -Body (@{email="secure-owner-$slot@example.invalid";password='OnlyForLocalTest!2026'}|ConvertTo-Json)
 foreach($node in @('A1','B1')){& "$base/provision-tenant.ps1" -Owner $identity.owner_id -Node $node -Project caribou-live-v4 -ControlImage caribou/live-v4-control:dev -SecurityImage caribou/live-v4-security:dev -RuntimeDirectory $runtime}
 DC cp "$base/smoke.py" "owner-${slot}:/tmp/vod-regression.py"
 try{Hold reserve $slot;DC exec -T -e PYTHONPATH=/app/vendor-video -e "SMOKE_OWNER_SLOT=$slot" "owner-$slot" python /tmp/vod-regression.py}finally{Hold cancel $slot}
 DC cp "owner-${slot}:/data/test-results/latest.json" "$results/owner-$slot.json"
}
Write-Host 'PASS existing two-owner/multizone signed VOD unchanged in independent live lab'
