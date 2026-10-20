$ErrorActionPreference='Stop'
$compose="$PSScriptRoot/compose.yaml"
& docker compose -f $compose cp "$PSScriptRoot/network_smoke.py" web:/tmp/network_smoke.py
if($LASTEXITCODE -ne 0){throw 'Copy failed'}
& docker compose -f $compose exec -T web python3 /tmp/network_smoke.py
if($LASTEXITCODE -ne 0){throw 'Network integration failed'}

$aclResult=& docker compose -f $compose exec -T broker-a sh -c 'mosquitto_pub -h 127.0.0.1 -u edge-A1 -P "$(cat /provision/edge-A1.password)" -V mqttv5 -q 1 -d -t lab/placement/v1/central/request/admin -m "{}"' 2>&1
if(($aclResult|Out-String) -notmatch 'Not authorized|RC:135'){throw 'Broker did not reject identity spoofing'}
Write-Host 'PASS broker ACL: Edge cannot impersonate admin (PUBACK reason 135)'
