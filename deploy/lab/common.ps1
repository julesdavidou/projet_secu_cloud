Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    $dockerBin = Join-Path $env:LOCALAPPDATA 'Programs/DockerDesktop/resources/bin'
    if (Test-Path $dockerBin) { $env:PATH = "$dockerBin;$env:PATH" }
}
function Load-LabEnv {
    if (Test-Path "$PSScriptRoot/.env") {
        foreach ($line in Get-Content "$PSScriptRoot/.env") {
            if ($line -match '^([A-Z_0-9]+)=(.*)$') {
                if (-not [Environment]::GetEnvironmentVariable($Matches[1])) {
                    [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
                }
            }
        }
    }
}
function Invoke-Docker {
    & docker @args
    if ($LASTEXITCODE -ne 0) { throw "docker failed ($LASTEXITCODE): $($args -join ' ')" }
}
function DC { Invoke-Docker compose -f "$PSScriptRoot/compose.yaml" @args }
function Get-LabSecret([string]$Name) { (Get-Content -Raw "$PSScriptRoot/.runtime/operator/$Name").Trim() }
function Get-CentralBase {
    $address='127.0.0.1';if($env:LAB_BIND_IP -and $env:LAB_BIND_IP -ne '0.0.0.0'){$address=$env:LAB_BIND_IP}
    $port='18088';if($env:CENTRAL_PORT){$port=$env:CENTRAL_PORT}
    return "http://${address}:$port"
}
Load-LabEnv
