param([Parameter(Mandatory)][string]$BindAddress,[switch]$Apply,[string]$OutputPath="$PSScriptRoot/.runtime/compose.lan.json")
$ErrorActionPreference='Stop'
$ip=[Net.IPAddress]::Parse($BindAddress)
$bytes=$ip.GetAddressBytes()
if($bytes.Length -ne 4 -or -not ($bytes[0] -eq 10 -or ($bytes[0] -eq 172 -and $bytes[1] -ge 16 -and $bytes[1] -le 31) -or ($bytes[0] -eq 192 -and $bytes[1] -eq 168))){throw 'Choose the PC private LAN IPv4 address, not 0.0.0.0 or loopback'}
$services=@{}
# Only mTLS endpoints are exported. Owner portals and the browser HTTP UI stay
# on loopback; no Docker socket or per-user storage port is published.
foreach($item in @(@('broker',19683,8883),@('central',19688,8443),@('zone-a',19691,8443),@('zone-b',19692,8443),@('is-a',19581,8443),@('is-b',19582,8443),@('auth',19583,8443),@('web',19689,8443),@('edge-a',19681,8443),@('edge-b',19682,8443),@('broker-a',19684,8883),@('broker-b',19685,8883),@('reconstructor-a',19584,8443),@('reconstructor-b',19585,8443))){$services[$item[0]]=@{ports=@("${BindAddress}:$($item[1]):$($item[2])")}}
$file=[IO.Path]::GetFullPath($OutputPath)
[IO.File]::WriteAllText($file,(@{services=$services}|ConvertTo-Json -Depth 8),[Text.UTF8Encoding]::new($false))
& docker compose -f "$PSScriptRoot/compose.yaml" -f $file config --quiet
if($LASTEXITCODE -ne 0){throw 'Generated LAN configuration invalid'}
if($Apply){& docker compose -f "$PSScriptRoot/compose.yaml" -f $file up -d --wait --wait-timeout 240;if($LASTEXITCODE -ne 0){throw 'LAN start failed'}}
Write-Host "Generated $file ; add -Apply to expose only these mTLS services on $BindAddress"
