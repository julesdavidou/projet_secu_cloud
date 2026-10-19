$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function D {& docker @args;if($LASTEXITCODE -ne 0){throw "Docker failed ($LASTEXITCODE)"}}
D compose -f "$PSScriptRoot/compose.yaml" cp "$PSScriptRoot/operations-check.py" owner-a:/tmp/operations-check.py
D compose -f "$PSScriptRoot/compose.yaml" exec -T -e PYTHONPATH=/app/vendor-video owner-a python /tmp/operations-check.py
D compose -f "$PSScriptRoot/compose.yaml" cp owner-a:/data/test-results/operations.json "$PSScriptRoot/results/operations.json"
