param([switch]$SkipBuild,[string]$BuildProxy="")
$ErrorActionPreference='Stop'
if(-not(Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function D {& docker @args;if($LASTEXITCODE -ne 0){throw 'Docker failed'}}
$runtime=Join-Path $PSScriptRoot '.runtime';New-Item -ItemType Directory -Force $runtime|Out-Null
$compose=Get-Content "$PSScriptRoot/../secure-v3/compose.yaml" -Raw|ConvertFrom-Json -AsHashtable
$compose.name='caribou-live-v4'
foreach($name in @($compose.services.Keys)){
 $service=$compose.services[$name]
 if($service.image -like 'caribou/secure-v3-*'){$service.image=$service.image.Replace('caribou/secure-v3-','caribou/live-v4-')}
 if($service.ContainsKey('volumes')){$service.volumes=@($service.volumes|ForEach-Object {if($_ -like './.runtime/*'){$_}elseif($_ -like './*'){$_.Replace('./','../secure-v3/')}else{$_}})}
 if($service.ContainsKey('ports')){$service.ports=@($service.ports|ForEach-Object {$parts=$_ -split ':';$parts[1]=[string]([int]$parts[1]+1000);$parts -join ':'})}
}
# Input is trusted local RTMP, available only while its publisher command runs.
$compose.services['owner-a'].ports+=@('127.0.0.1:21935:1935')
$compose.services['owner-b'].ports+=@('127.0.0.1:21936:1935')
$file=Join-Path $runtime 'compose.json';[IO.File]::WriteAllText($file,($compose|ConvertTo-Json -Depth 30),[Text.UTF8Encoding]::new($false))
function DC {D compose --project-directory $PSScriptRoot -f $file @args}
if(-not $SkipBuild){if($BuildProxy){DC build --build-arg "http_proxy=$BuildProxy" --build-arg "https_proxy=$BuildProxy"}else{DC build}}
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
D run --rm --user 0:0 --env "HOST_UID=$uidValue" --mount "type=bind,source=$PSScriptRoot/../secure-v3/topology.json,target=/input/topology.json,readonly" --mount "type=bind,source=$runtime,target=/output" caribou/live-v4-control:dev lab-admin -mode real-http -secure-v3
foreach($name in @('tenant-A1','tenant-B1','lan-A1','lan-B1')){New-Item -ItemType Directory -Force (Join-Path $runtime $name)|Out-Null}
DC up -d --wait --wait-timeout 240
Write-Host 'Live V4 http://localhost:20690/live/ ; owner portals :20591/:20592 ; local OBS inputs :21935/:21936'
