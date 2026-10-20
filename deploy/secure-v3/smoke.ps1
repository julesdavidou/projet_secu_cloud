#requires -Version 7.0
param()
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function DC {& docker compose -f "$PSScriptRoot/compose.yaml" @args;if($LASTEXITCODE -ne 0){throw 'Docker Compose failed'}}
New-Item -ItemType Directory -Force "$PSScriptRoot/results"|Out-Null
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
function Hold([string]$action,[string]$slot){
    & docker run --rm --user "${uidValue}:10001" --cap-drop ALL --security-opt no-new-privileges:true --network caribou-secure-v3_default --env "HOLD_ACTION=$action" --env "HOLD_SLOT=$slot" --mount "type=bind,source=$PSScriptRoot/control-test.py,target=/test.py,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime/pki/admin,target=/pki,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime/operator,target=/operator,readonly" --mount "type=bind,source=$PSScriptRoot/results,target=/fixture" caribou/secure-v3-security:dev python /test.py
    if($LASTEXITCODE -ne 0){throw "Capacity reservation $action failed"}
}
foreach($slot in @('a','b')){
    $port=if($slot -eq 'a'){19591}else{19592}
    $identity=Invoke-RestMethod "http://localhost:$port/owner/login" -Method Post -ContentType application/json -Body (@{email="secure-owner-$slot@example.invalid";password='OnlyForLocalTest!2026'}|ConvertTo-Json)
    # This is the explicit administrator CLI approved by the user. Applications
    # and owner agents never get permission to create their own Docker resources.
    foreach($node in @('A1','B1')){& "$PSScriptRoot/provision-tenant.ps1" -Owner $identity.owner_id -Node $node}
    DC cp "$PSScriptRoot/smoke.py" "owner-${slot}:/tmp/secure-smoke.py"
    try {
        Hold reserve $slot
        DC exec -T -e PYTHONPATH=/app/vendor-video -e "SMOKE_OWNER_SLOT=$slot" "owner-$slot" python /tmp/secure-smoke.py
    } finally {Hold cancel $slot}
    DC cp "owner-${slot}:/data/test-results/latest.json" "$PSScriptRoot/results/owner-$slot.json"
}
Write-Host 'PASS two real owners published and read byte-identical videos from isolated storage. No physical ARM test.'
