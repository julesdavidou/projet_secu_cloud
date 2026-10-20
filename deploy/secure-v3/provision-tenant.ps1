#requires -Version 7.0
<# Run once as the Docker administrator, after the owner agent's first login.
   This script never mounts the Docker socket into a container. It creates one
   non-root storage process and named volume per owner/node; no data is removed.
#>
param(
    [Parameter(Mandatory)][ValidatePattern('^client-[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$')][string]$Owner,
    [Parameter(Mandatory)][ValidatePattern('^[A-Za-z0-9][A-Za-z0-9_.-]{0,100}$')][string]$Node,
 [ValidatePattern('^[a-z0-9][a-z0-9-]{0,62}$')][string]$Project='caribou-secure-v3',
 [string]$ControlImage='caribou/secure-v3-control:dev',
 [string]$SecurityImage='caribou/secure-v3-security:dev',
 [string]$RuntimeDirectory="$PSScriptRoot/.runtime"
)
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function D { $result=& docker @args; if($LASTEXITCODE -ne 0){throw "Docker command failed ($LASTEXITCODE)"}; return $result }
$deploymentProject=$Project
$nodeLower=$Node.ToLowerInvariant()
$identity=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes("$deploymentProject/$Node/$Owner"))).ToLowerInvariant().Substring(0,24)
$name="$deploymentProject-$nodeLower-$identity"
$volume="$name-data"
$dns="tenant-$identity.storage-$nodeLower.internal"
$runtime=[IO.Path]::GetFullPath($RuntimeDirectory)
$topology=Get-Content -LiteralPath (Join-Path $runtime 'config.json') -Raw|ConvertFrom-Json
if(@($topology.nodes|Where-Object {$_.id -eq $Node}).Count -ne 1){throw 'Node is not provisioned in config.json'}
$directory=Join-Path $runtime "tenant-$Node"
$leaf=Join-Path $runtime "tenant-cert-$Node-$identity"
$issuer=Join-Path $runtime "pki/storage-issuer-$Node"
if(-not (Test-Path -LiteralPath "$issuer/tls.key")){throw 'Run start.ps1 first: the node storage issuer is missing'}
New-Item -ItemType Directory -Force $directory,$leaf|Out-Null
$lock=[IO.File]::Open((Join-Path $directory 'admin.lock'),[IO.FileMode]::OpenOrCreate,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None)
try {
    $registryFile=Join-Path $directory 'tenants.json'
    $registry=@{}
    if(Test-Path -LiteralPath $registryFile){$registry=Get-Content -LiteralPath $registryFile -Raw|ConvertFrom-Json -AsHashtable}
    if($registry.Count -ge 16 -and -not $registry.ContainsKey($Owner)){throw 'Node tenant limit (16) reached; explicit capacity review required'}
    $target=$registry[$Owner]
    if($null -eq $target){$target=@{owner_id=$Owner;endpoint="https://${dns}:8443";token=[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLowerInvariant()}}
    if($target.owner_id -ne $Owner -or $target.endpoint -ne "https://${dns}:8443" -or $target.token -notmatch '^[a-f0-9]{64}$'){throw 'Invalid existing tenant entry; refusing replacement'}
    $existing=@(D ps -a --filter "name=^/${name}$" --format '{{.ID}}')
    if($existing.Count -gt 0){
        $container=(D inspect $existing[0]|ConvertFrom-Json)[0]
        if($container.Config.Labels.'caribou.project' -ne $deploymentProject -or $container.Config.Labels.'caribou.owner' -ne $Owner -or $container.Config.Labels.'caribou.node' -ne $Node -or @($container.Mounts|Where-Object {$_.Type -eq 'volume' -and $_.Name -eq $volume -and $_.Destination -eq '/data'}).Count -ne 1){throw 'Existing container identity/mount mismatch; refusing modification'}
        if(-not $registry.ContainsKey($Owner)){throw 'Existing container without registry token; explicit recovery required'}
        if(-not $container.State.Running){D start $name|Out-Null}
    } else {
        $uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
        if(-not (Test-Path -LiteralPath "$leaf/tls.crt") -or -not (Test-Path -LiteralPath "$leaf/tls.key")){
            if((Test-Path -LiteralPath "$leaf/tls.crt") -or (Test-Path -LiteralPath "$leaf/tls.key")){throw 'Partial storage certificate: explicit recovery required'}
            D run --rm --user "${uidValue}:10001" --cap-drop ALL --security-opt no-new-privileges:true --mount "type=bind,source=$issuer,target=/run/issuer,readonly" --mount "type=bind,source=$leaf,target=/out" $SecurityImage storage-certificate -name $dns -out /out|Out-Null
        }
        $image=(D image inspect $ControlImage --format '{{.Id}}').Trim()
        $crt=[Convert]::ToBase64String([IO.File]::ReadAllBytes("$leaf/tls.crt"))
        $key=[Convert]::ToBase64String([IO.File]::ReadAllBytes("$leaf/tls.key"))
        $ca=[Convert]::ToBase64String([IO.File]::ReadAllBytes("$issuer/ca.crt"))
        $init=@'
set -eu
umask 077
mkdir -p /data/.identity
printf '%s' "$TENANT_TLS_CERT" | base64 -d > /data/.identity/tls.crt
printf '%s' "$TENANT_TLS_KEY" | base64 -d > /data/.identity/tls.key
printf '%s' "$TENANT_TLS_CA" | base64 -d > /data/.identity/ca.crt
unset TENANT_TLS_CERT TENANT_TLS_KEY TENANT_TLS_CA
printf '%s' "$TENANT_STORAGE_TOKEN" > /data/.identity/http-token
unset TENANT_STORAGE_TOKEN
exec tls-run edge-agent
'@
        # Docker's named volume inherits /data ownership from the image. No host
        # port, privileged flag, host namespace or engine socket is granted.
        D run -d --name $name --label "caribou.project=$deploymentProject" --label "caribou.owner=$Owner" --label "caribou.node=$Node" --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges:true --memory 128m --cpus 0.5 --pids-limit 64 --restart unless-stopped --network "${deploymentProject}_default" --network-alias $dns --mount "type=volume,source=$volume,target=/data" --env "STORAGE_TENANT_ID=$Owner" --env 'STORAGE_ROOT=/data/objects' --env "TENANT_STORAGE_TOKEN=$($target.token)" --env 'HTTP_AUTH_TOKEN_FILE=/data/.identity/http-token' --env 'EDGE_CONTROL_ENABLED=0' --env 'MQTT_BROKER=' --env 'STORAGE_HTTP_ADDR=127.0.0.1:8080' --env 'TLS_UPSTREAM=http://127.0.0.1:8080' --env "TLS_ALLOWED_PEERS=edge-$Node" --env 'TLS_CA_FILE=/data/.identity/ca.crt' --env 'TLS_CERT_FILE=/data/.identity/tls.crt' --env 'TLS_KEY_FILE=/data/.identity/tls.key' --env "TENANT_TLS_CERT=$crt" --env "TENANT_TLS_KEY=$key" --env "TENANT_TLS_CA=$ca" --health-cmd 'wget -q -O - http://127.0.0.1:8080/health' --health-interval 2s --health-timeout 2s --health-retries 20 $image sh -c $init|Out-Null
    }
    $healthy=$false
    for($attempt=0;$attempt -lt 30;$attempt++){
        $state=(D inspect $name --format '{{.State.Health.Status}}').Trim()
        if($state -eq 'healthy'){$healthy=$true;break}
        if($state -eq 'unhealthy'){break}
        Start-Sleep -Seconds 1
    }
    if(-not $healthy){throw "Storage unhealthy: docker logs $name (no registry published)"}
    $registry[$Owner]=$target
    $temp=Join-Path $directory ('tenants-'+[guid]::NewGuid().ToString('N')+'.tmp')
    [IO.File]::WriteAllText($temp,($registry|ConvertTo-Json -Depth 8),[Text.UTF8Encoding]::new($false))
    if($env:OS -ne 'Windows_NT'){
        # The host account need not belong to GID 10001. This short-lived helper
        # runs as that same UID with the storage group and only this directory.
        $uidValue=(& id -u).Trim()
        D run --rm --user "${uidValue}:10001" --cap-drop ALL --security-opt no-new-privileges:true --mount "type=bind,source=$directory,target=/registry" $ControlImage sh -c 'chgrp 10001 "/registry/$1" && chmod 640 "/registry/$1"' sh ([IO.Path]::GetFileName($temp))|Out-Null
    }
    Move-Item -LiteralPath $temp -Destination $registryFile -Force
    Write-Host "READY owner=$Owner node=$Node container=$name volume=$volume (no published port)"
} finally {$lock.Dispose()}
