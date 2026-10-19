param([switch]$SkipBuild, [string]$BuildProxy="", [string]$Topology="$PSScriptRoot/topology.json", [switch]$ExternalEdges)
$ErrorActionPreference='Stop'
$docker=Get-Command docker -ErrorAction SilentlyContinue
if(-not $docker){$env:PATH="${env:LOCALAPPDATA}\Programs\DockerDesktop\resources\bin;"+$env:PATH}
function Invoke-Docker { & docker @args; if($LASTEXITCODE -ne 0){throw "Docker failed ($LASTEXITCODE)"} }
$repo=(Resolve-Path "$PSScriptRoot/../..").Path
if(-not $SkipBuild){
 if($BuildProxy){Invoke-Docker compose -f "$PSScriptRoot/compose.yaml" build --build-arg "http_proxy=$BuildProxy" --build-arg "https_proxy=$BuildProxy"}else{Invoke-Docker compose -f "$PSScriptRoot/compose.yaml" build}
}
New-Item -ItemType Directory -Force "$PSScriptRoot/.runtime" | Out-Null
$priorConfig=''
if(Test-Path "$PSScriptRoot/.runtime/config.json"){$priorConfig=(Get-FileHash "$PSScriptRoot/.runtime/config.json" -Algorithm SHA256).Hash}
$topology=(Resolve-Path $Topology).Path
$hostUid='1000'
if($env:OS -ne 'Windows_NT'){$hostUid=(& id -u).Trim()}
Invoke-Docker run --rm --user 0:0 --env "HOST_UID=$hostUid" --mount "type=bind,source=$topology,target=/input/topology.json,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime,target=/output" caribou/integration-control:dev lab-admin -mode real-http
$composeArgs=@("compose","-f","$PSScriptRoot/compose.yaml")
if($ExternalEdges){$composeArgs+=@("-f","$PSScriptRoot/compose.external.yaml")}
# Stop only this project's local Edge identities before connecting physical hosts.
if($ExternalEdges){Invoke-Docker compose -f "$PSScriptRoot/compose.yaml" stop edge-a edge-b agent-a agent-b}
$refresh=@()
$nextConfig=(Get-FileHash "$PSScriptRoot/.runtime/config.json" -Algorithm SHA256).Hash
if($priorConfig -and $priorConfig -ne $nextConfig){
 Write-Host 'Topology changed: refreshing services with preserved volumes and existing secrets.'
 $refresh=@('--force-recreate')
}
Invoke-Docker @composeArgs up -d @refresh --wait --wait-timeout 180
Write-Host 'Prêt : http://localhost:18090/integration/ puis http://localhost:18090/'
