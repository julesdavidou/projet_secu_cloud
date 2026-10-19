param(
    [ValidateSet('real-http','mock')][string]$Mode = 'real-http',
    [string]$Topology = "$PSScriptRoot/config/topology.local.json",
    [switch]$SkipBuild
)
. "$PSScriptRoot/common.ps1"
if (-not (Test-Path "$PSScriptRoot/.env")) { Copy-Item "$PSScriptRoot/.env.example" "$PSScriptRoot/.env"; Load-LabEnv }
$repo = (Resolve-Path "$PSScriptRoot/../..").Path
if (-not $SkipBuild) { Invoke-Docker build -f "$PSScriptRoot/Dockerfile" --target runtime -t $env:CONTROLLERS_IMAGE $repo }
New-Item -ItemType Directory -Force "$PSScriptRoot/.runtime" | Out-Null
$topologyPath = (Resolve-Path $Topology).Path
$hostUid='1000'
if($env:OS -ne 'Windows_NT'){$hostUid=(& id -u).Trim()}
Invoke-Docker run --rm --user 0:0 --env "HOST_UID=$hostUid" --mount "type=bind,source=$topologyPath,target=/input/topology.json,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime,target=/output" $env:CONTROLLERS_IMAGE lab-admin -mode $Mode
$Mode | Set-Content -Encoding ascii "$PSScriptRoot/.runtime/mode"
Write-Host "PASS provisioned $Mode; secrets stay in .runtime/ (never commit or copy the whole directory to an Edge)."
