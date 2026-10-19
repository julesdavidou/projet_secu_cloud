. "$PSScriptRoot/common.ps1"
DC --profile local --profile real-http ps -a
DC --profile local --profile real-http logs --tail 80
Get-NetIPAddress -AddressFamily IPv4 | Select-Object InterfaceAlias,IPAddress
$base=Get-CentralBase
Invoke-RestMethod "$base/health"
Invoke-RestMethod "$base/ready"
if(Test-Path "$PSScriptRoot/.runtime/operator/operator.token"){
    $headers=@{Authorization="Bearer $(Get-LabSecret 'operator.token')"}
    Invoke-RestMethod "$base/api/v1/snapshot" -Headers $headers | ConvertTo-Json -Depth 40
}
Write-Host 'Secrets intentionally omitted. Inspect local .runtime/config.json for advertised IPs and quota.'
