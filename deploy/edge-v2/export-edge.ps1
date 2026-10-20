param([Parameter(Mandatory)][string]$Id,[Parameter(Mandatory)][string]$Destination)
$ErrorActionPreference='Stop'
$cfg=Get-Content "$PSScriptRoot/.runtime/config.json" -Raw|ConvertFrom-Json
$node=@($cfg.nodes|Where-Object id -eq $Id)
if($node.Count -ne 1 -or $node[0].mode -ne 'real-http'){throw 'Select one provisioned real-http node'}
$endpoint=[uri]$node[0].endpoint
if($endpoint.Host -in @('edge-a','edge-b','localhost','127.0.0.1')){throw 'Provision explicit LAN endpoints and hub before export'}
$bridge=Get-Content "$PSScriptRoot/.runtime/broker-edge-$Id/bridge.conf" -Raw
if($bridge -match '(?m)^address broker:1883$'){throw 'Provision an actual reachable LAN hub with start.ps1 -ExternalEdges -Hub first'}
if(Test-Path $Destination){throw 'Destination exists; choose a new private directory'}
New-Item -ItemType Directory -Force "$Destination/edge-config/identity"|Out-Null
Copy-Item "$PSScriptRoot/.runtime/config.json" "$Destination/edge-config/config.json"
Copy-Item "$PSScriptRoot/.runtime/identities/edge-$Id/*" "$Destination/edge-config/identity/"
Copy-Item "$PSScriptRoot/.runtime/broker-edge-$Id" "$Destination/edge-config/broker" -Recurse
Copy-Item "$PSScriptRoot/compose.edge.yaml","$PSScriptRoot/broker-entrypoint.sh" $Destination
$origins=($cfg.nodes.endpoint -join ',')
@("EDGE_ID=$Id","EDGE_BIND_IP=$($endpoint.Host)","EDGE_HTTP_PORT=$($endpoint.Port)","MQTT_PROTOCOL=5","ALLOWED_EDGE_ORIGINS=$origins")|Set-Content "$Destination/.env" -Encoding utf8
Write-Host "Exported only edge-$Id credentials; transfer this directory privately and run docker compose -f compose.edge.yaml up -d --wait"
