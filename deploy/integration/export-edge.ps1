param([Parameter(Mandatory)][string]$Id,[Parameter(Mandatory)][string]$Destination)
$ErrorActionPreference='Stop'
$cfg=Get-Content "$PSScriptRoot/.runtime/config.json" -Raw|ConvertFrom-Json
$node=@($cfg.nodes|Where-Object id -eq $Id)
if($node.Count -ne 1 -or $node[0].mode -ne 'real-http'){throw 'Select a provisioned real-http node'}
if(Test-Path -LiteralPath $Destination){throw 'Destination exists; choose a new private folder'}
New-Item -ItemType Directory -Path "$Destination/edge-config/identity" -Force|Out-Null
Copy-Item "$PSScriptRoot/.runtime/config.json" "$Destination/edge-config/config.json"
Copy-Item "$PSScriptRoot/.runtime/identities/edge-$Id/*" "$Destination/edge-config/identity/"
Copy-Item "$PSScriptRoot/../lab/compose.edge.yaml" "$Destination/compose.edge.yaml"
Write-Host "Exported only edge-$Id identity and public topology. Keep private; never commit. Set CONTROLLERS_IMAGE=caribou/integration-control:dev and MQTT_PORT=1884 on this Edge."
