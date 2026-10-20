param([Parameter(Mandatory)][string]$BindAddress,[string]$ControlImage='caribou/secure-v3-control:dev',[string]$RuntimeDirectory="$PSScriptRoot/../secure-v3/.runtime",[string]$ProjectDirectory="$PSScriptRoot/../secure-v3",[int]$PortOffset=0)
$ErrorActionPreference='Stop'
$ip=[Net.IPAddress]::Parse($BindAddress);if($ip.AddressFamily -ne 'InterNetwork' -or $ip.ToString() -notmatch '^(192\.168\.|10\.|172\.(1[6-9]|2[0-9]|3[01])\.)'){throw 'Adresse LAN IPv4 privée requise'}
$capability=& docker image inspect $ControlImage --format '{{index .Config.Labels "io.caribou.client-portal"}}'
if($LASTEXITCODE -ne 0 -or $capability.Trim() -ne '1'){throw 'Image sans portail client : construire le nouveau code avant activation'}
$runtime=[IO.Path]::GetFullPath($RuntimeDirectory);$project=[IO.Path]::GetFullPath($ProjectDirectory)
if(-not(Test-Path "$runtime/pki/viewer-gateway/tls.crt")){throw 'Provisionner les certificats client avec prepare-client.ps1 d abord'}
$gateway=@{services=@{'viewer-gateway'=@{image=$ControlImage;command=@('tls-run','-proxy-only');restart='unless-stopped';environment=@{TLS_UPSTREAM='http://web:8080';TLS_ALLOWED_PEERS='viewer-lab-*';TLS_CA_FILE='/run/tls/ca.crt';TLS_CERT_FILE='/run/tls/tls.crt';TLS_KEY_FILE='/run/tls/tls.key'};volumes=@("${runtime}/pki/viewer-gateway:/run/tls:ro");ports=@("${BindAddress}:$(19693+$PortOffset):8443");cap_drop=@('ALL');security_opt=@('no-new-privileges:true');healthcheck=@{test=@('CMD-SHELL','nc -z 127.0.0.1 8443');interval='3s';timeout='2s';retries=30}}}}
$file="$runtime/client.gateway.json";[IO.File]::WriteAllText($file,($gateway|ConvertTo-Json -Depth 15),[Text.UTF8Encoding]::new($false))
& docker compose --project-directory $project -f "$project/compose.yaml" -f $file up -d --no-deps --wait viewer-gateway
if($LASTEXITCODE -ne 0){throw 'Passerelle client indisponible'}
Write-Host "Passerelle mTLS active sur ${BindAddress}:$(19693+$PortOffset) ; les navigateurs passent par leur portail local."
