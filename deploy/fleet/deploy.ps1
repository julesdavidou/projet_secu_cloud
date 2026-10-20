param(
    [string]$CoordinatorAddress = '',
    [string]$Repository = $(if ((Split-Path $PSScriptRoot -Leaf) -eq 'fleet') { Join-Path $PSScriptRoot '../..' } else { Join-Path $PSScriptRoot '..\projet_secu_cloud' }),
    [string]$StateDirectory = $PSScriptRoot,
    [string]$Inventory = "$StateDirectory/inventory.local.json",
    [string[]]$Nodes = @(),
    [ValidateSet('registry','local')][string]$ImageSource = 'registry',
    [string]$RegistryRepository = 'julesdavidou/projet_secu_cloud',
    [string]$RegistryUser = '',
    [string]$Revision = '',
    [switch]$ForceRecreate,
    [switch]$CheckOnly,
    [switch]$ConfigureFirewall,
    [switch]$SkipBuild,
    [switch]$SkipFetch,
    [ValidateRange(1,5)][int]$Jobs = 2
)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/python.ps1"
$arguments = @('deploy', '--repository', $Repository, '--state-directory', $StateDirectory, '--inventory', $Inventory, '--jobs', "$Jobs")
if ($Nodes.Count) { $arguments += @('--nodes', ($Nodes -join ',')) }
$arguments += @('--image-source', $ImageSource, '--registry-repository', $RegistryRepository)
if ($RegistryUser) { $arguments += @('--registry-user', $RegistryUser) }
if ($Revision) { $arguments += @('--revision', $Revision) }
if ($ForceRecreate) { $arguments += '--force-recreate' }
if ($CheckOnly) { $arguments[0] = 'check' }
if ($CoordinatorAddress) { $arguments += @('--coordinator-address', $CoordinatorAddress) }
if ($ConfigureFirewall) { $arguments += '--configure-firewall' }
if ($SkipBuild) { $arguments += '--skip-build' }
if ($SkipFetch) { $arguments += '--skip-fetch' }
& (Get-FleetPython -StateDirectory $StateDirectory) "$PSScriptRoot/fleet.py" @arguments
exit $LASTEXITCODE
