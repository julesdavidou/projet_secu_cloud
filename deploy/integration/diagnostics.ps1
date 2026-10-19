$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
& docker compose -f "$PSScriptRoot/compose.yaml" ps
& docker compose -f "$PSScriptRoot/compose.yaml" logs --no-color --tail 30 central zone-a zone-b edge-a edge-b web auth
$webPort='18090';if($env:WEB_PORT){$webPort=$env:WEB_PORT}
Invoke-RestMethod "http://127.0.0.1:$webPort/ready"|ConvertTo-Json
Write-Host 'Broker ACL credentials and HTTP tokens are intentionally omitted. External lab: use the external overlay for ps/logs.'
