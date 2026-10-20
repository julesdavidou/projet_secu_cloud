param(
    [string]$CoordinatorAddress = '',
    [string]$Repository = $(if ((Split-Path $PSScriptRoot -Leaf) -eq 'fleet') { Join-Path $PSScriptRoot '../..' } else { Join-Path $PSScriptRoot '..\projet_secu_cloud' }),
    [string]$StateDirectory = $PSScriptRoot,
    [string]$Inventory = "$StateDirectory/inventory.local.json",
    [string[]]$Nodes = @(),
    [switch]$HealthOnly,
    [ValidateRange(1,5)][int]$Jobs = 2
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/python.ps1"
$arguments = @('test', '--repository', $Repository, '--state-directory', $StateDirectory, '--inventory', $Inventory, '--jobs', "$Jobs")
if ($Nodes.Count) { $arguments += @('--nodes', ($Nodes -join ',')) }
if ($CoordinatorAddress) { $arguments += @('--coordinator-address', $CoordinatorAddress) }
if ($HealthOnly) { $arguments += '--health-only' }
& (Get-FleetPython -StateDirectory $StateDirectory) "$PSScriptRoot/fleet.py" @arguments
exit $LASTEXITCODE
