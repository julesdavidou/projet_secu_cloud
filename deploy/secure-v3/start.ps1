param([switch]$SkipBuild,[string]$BuildProxy="")
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function D {& docker @args;if($LASTEXITCODE -ne 0){throw "Docker failed ($LASTEXITCODE)"}}
if(-not $SkipBuild){if($BuildProxy){D compose -f "$PSScriptRoot/compose.yaml" build --build-arg "http_proxy=$BuildProxy" --build-arg "https_proxy=$BuildProxy"}else{D compose -f "$PSScriptRoot/compose.yaml" build}}
New-Item -ItemType Directory -Force "$PSScriptRoot/.runtime"|Out-Null
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
D run --rm --user 0:0 --env "HOST_UID=$uidValue" --mount "type=bind,source=$PSScriptRoot/topology.json,target=/input/topology.json,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime,target=/output" caribou/secure-v3-control:dev lab-admin -mode real-http -secure-v3
D compose -f "$PSScriptRoot/compose.yaml" up -d --wait --wait-timeout 240
Write-Host 'Secure V3 viewer http://localhost:19690/integration/ ; owner agents http://localhost:19591 and :19592'
