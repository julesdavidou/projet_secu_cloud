param([ValidateSet('local','lab-ready')][string]$Profile='local',[switch]$AllowLan,[switch]$SkipInit,[string]$Topology='')
. "$PSScriptRoot/common.ps1"
if (-not (Test-Path "$PSScriptRoot/.env")) { Copy-Item "$PSScriptRoot/.env.example" "$PSScriptRoot/.env"; Load-LabEnv }
if ($env:LAB_BIND_IP -ne '127.0.0.1' -and -not $AllowLan) { throw 'LAN binding requires explicit -AllowLan and an isolated trusted network.' }
if ($Profile -eq 'lab-ready' -and ($env:LAB_PC_ADDRESS -match '^192\.0\.2\.' -or $env:LAB_PC_ADDRESS -eq 'localhost')) { throw 'Replace LAB_PC_ADDRESS with the PC LAN IP in .env.' }
if (-not $SkipInit) {
    if ($Profile -eq 'lab-ready' -and -not $Topology) { $Topology="$PSScriptRoot/config/topology.lab-ready.json" }
    if (-not $Topology) { $Topology="$PSScriptRoot/config/topology.local.json" }
    & "$PSScriptRoot/init.ps1" -Topology $Topology
}
$env:EDGE_MODE=(Get-Content "$PSScriptRoot/.runtime/mode" -Raw).Trim()
$profiles=@();if($Profile -eq 'local'){$profiles=@('--profile','local');if($env:EDGE_MODE -eq 'real-http'){$profiles+=@('--profile','real-http')}}
DC @profiles config --quiet
DC @profiles up -d --wait --wait-timeout 120
DC @profiles ps
