param([switch]$SkipBuild,[string]$BuildProxy="",[string]$Topology="$PSScriptRoot/topology.json",[switch]$ExternalEdges,[string]$Hub="broker:1883",[string]$BindIP="127.0.0.1")
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function D { & docker @args; if($LASTEXITCODE -ne 0){throw "Docker failed ($LASTEXITCODE)"} }
if(-not $SkipBuild){
 if($BuildProxy){D compose -f "$PSScriptRoot/compose.yaml" build --build-arg "http_proxy=$BuildProxy" --build-arg "https_proxy=$BuildProxy"}else{D compose -f "$PSScriptRoot/compose.yaml" build}
}
$env:V2_BIND_IP=$BindIP
$composeArgs=@('-f',"$PSScriptRoot/compose.yaml")
if($ExternalEdges){
 if($Hub -eq 'broker:1883' -or $BindIP -eq '127.0.0.1'){throw 'ExternalEdges requires explicit -Hub PC_LAN_IP:1886 and -BindIP PC_LAN_IP'}
 $topologyValue=Get-Content $Topology -Raw|ConvertFrom-Json
 foreach($node in $topologyValue.nodes){if(([uri]$node.endpoint).Host -in @('edge-a','edge-b','localhost','127.0.0.1')){throw 'Replace every node endpoint with its real LAN address before external deployment'}}
 $composeArgs+=@('-f',"$PSScriptRoot/compose.external.yaml")
}
New-Item -ItemType Directory -Force "$PSScriptRoot/.runtime"|Out-Null
$prior='';if(Test-Path "$PSScriptRoot/.runtime/config.json"){$prior=(Get-FileHash "$PSScriptRoot/.runtime/config.json").Hash}
$source=(Resolve-Path $Topology).Path
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
D run --rm --user 0:0 --env "HOST_UID=$uidValue" --mount "type=bind,source=$source,target=/input/topology.json,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime,target=/output" caribou/edge-v2-control:dev lab-admin -mode real-http -edge-brokers -hub $Hub
$refresh=@();if($prior -and $prior -ne (Get-FileHash "$PSScriptRoot/.runtime/config.json").Hash){$refresh=@('--force-recreate')}
if($ExternalEdges){D compose -f "$PSScriptRoot/compose.yaml" stop edge-a edge-b broker-a broker-b reconstructor-a reconstructor-b}
D compose @composeArgs up -d @refresh --wait --wait-timeout 240
$webPort=if($env:V2_WEB_PORT){$env:V2_WEB_PORT}else{'19290'}
Write-Host "Edge V2 ready: http://${BindIP}:$webPort/integration/"
