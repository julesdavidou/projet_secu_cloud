$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
& docker compose -f "$PSScriptRoot/compose.yaml" cp "$PSScriptRoot/smoke.py" web:/tmp/integration-smoke.py
if($LASTEXITCODE -ne 0){throw 'Copy smoke failed'}
& docker compose -f "$PSScriptRoot/compose.yaml" exec -T web python3 /tmp/integration-smoke.py
if($LASTEXITCODE -ne 0){throw 'Real integration smoke failed'}
