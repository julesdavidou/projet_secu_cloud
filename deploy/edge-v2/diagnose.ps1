$ErrorActionPreference='Stop'
& docker version
& docker compose -f "$PSScriptRoot/compose.yaml" ps
& docker compose -f "$PSScriptRoot/compose.yaml" logs --no-color --tail 40 central zone-a zone-b edge-a edge-b broker-a broker-b reconstructor-a reconstructor-b web
$token=(Get-Content "$PSScriptRoot/.runtime/operator/operator.token" -Raw).Trim()
$port=if($env:V2_CENTRAL_PORT){$env:V2_CENTRAL_PORT}else{'19288'}
$bind=if($env:V2_BIND_IP){$env:V2_BIND_IP}else{'127.0.0.1'}
$s=Invoke-RestMethod "http://${bind}:$port/api/v1/snapshot" -Headers @{Authorization="Bearer $token"}
$s.nodes|ConvertTo-Json -Depth 8
