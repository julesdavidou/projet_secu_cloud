param([Parameter(Mandatory)][string]$Id,[Parameter(Mandatory)][string]$Destination)
. "$PSScriptRoot/common.ps1"
$cfg=Get-Content -Raw "$PSScriptRoot/.runtime/config.json"|ConvertFrom-Json
$node=@($cfg.nodes|Where-Object id -eq $Id)
if($node.Count -ne 1 -or $node[0].mode -ne 'real-http'){throw 'Select one provisioned real-http node.'}
if(Test-Path $Destination){throw 'Destination already exists: select a new private directory.'}
New-Item -ItemType Directory -Path "$Destination/edge-config/identity" -Force|Out-Null
Copy-Item "$PSScriptRoot/.runtime/config.json" "$Destination/edge-config/config.json"
Copy-Item "$PSScriptRoot/.runtime/identities/edge-$Id/*" "$Destination/edge-config/identity/"
Copy-Item "$PSScriptRoot/compose.edge.yaml" "$Destination/compose.edge.yaml"
Write-Host "PASS exported only edge-$Id credentials and public topology. Transfer privately; never commit this directory."
