param([Parameter(Mandatory)][string]$BindAddress,[string]$ControlImage='caribou/windows-control:dev')
$ErrorActionPreference='Stop'
$project=[IO.Path]::GetFullPath("$PSScriptRoot/../secure-v3");$runtime="$project/.runtime"
if(-not(Test-Path "$runtime/config.json") -or -not(Test-Path "$runtime/pki/authority/ca.key")){throw 'Runtime V3 initial manquant ; ne pas inventer/remplacer la CA du laboratoire'}
# Resume from the already admitted topology, never the two-node default example.
$cfg=Get-Content "$runtime/config.json" -Raw|ConvertFrom-Json
$base=Get-Content "$project/compose.yaml" -Raw|ConvertFrom-Json
$services=@{};$origins=($cfg.nodes.endpoint -join ',')
foreach($prop in $base.services.PSObject.Properties){if($prop.Value.environment.ALLOWED_EDGE_ORIGINS){$services[$prop.Name]=@{environment=@{ALLOWED_EDGE_ORIGINS=$origins}}}}
[IO.File]::WriteAllText("$runtime/client.storage.json",(@{services=$services}|ConvertTo-Json -Depth 10),[Text.UTF8Encoding]::new($false))
& "$project/enable-lan.ps1" -BindAddress $BindAddress
& "$PSScriptRoot/enable-client-gateway.ps1" -BindAddress $BindAddress -ControlImage $ControlImage
& docker compose -f "$project/compose.yaml" -f "$runtime/compose.lan.json" -f "$runtime/client.storage.json" -f "$runtime/client.gateway.json" up -d --wait --wait-timeout 240
if($LASTEXITCODE -ne 0){throw 'Reprise des contrôleurs en échec ; volumes conservés'}
& "$PSScriptRoot/controller-discovery.ps1" -InterfaceIPv4 $BindAddress -RuntimeDirectory $runtime
Write-Host 'Contrôleurs repris avec leurs admissions, données et clés existantes.'
