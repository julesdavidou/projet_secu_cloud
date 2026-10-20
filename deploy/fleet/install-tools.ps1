param(
    [string]$Destination = (Join-Path $PSScriptRoot '../../../cariboutv-outils')
)
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path $Destination | Out-Null
# Update only public source files. Never overwrite the local inventory/runtime.
foreach ($name in @('deploy.ps1','test.ps1','python.ps1','fleet.py','registry.py',
                    'remote-deploy.sh','remote-pull.sh','test-video.py','test_fleet.py',
                    'test_registry.py','requirements.txt','inventory.example.json','README.md')) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) -Destination (Join-Path $Destination $name) -Force
}
Write-Host "Outils mis a jour dans $Destination ; inventaire et rapports conserves."
