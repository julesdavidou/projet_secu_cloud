param([switch]$DeleteTestVolumes)
. "$PSScriptRoot/common.ps1"
# Fixed project only: never global prune, never delete another Compose project.
if ($DeleteTestVolumes) {
    DC --profile local --profile real-http down --volumes --remove-orphans
    Write-Host 'Deleted only caribou-controllers test volumes; provisioned identities retained.'
} else {
    DC --profile local --profile real-http down
    Write-Host 'Volumes preserved. Use -DeleteTestVolumes only to erase this demo storage/catalog.'
}
