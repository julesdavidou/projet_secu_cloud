#requires -Version 7.0
param([switch]$PeerOnly)
$ErrorActionPreference='Stop'
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
$options=@('run','--rm','--user',"${uidValue}:10001",'--cap-drop','ALL','--security-opt','no-new-privileges:true','--network','caribou-secure-v3_default',
    '--mount',"type=bind,source=$PSScriptRoot/verify.py,target=/test.py,readonly",
    '--mount',"type=bind,source=$PSScriptRoot/.runtime/pki/edge-A1,target=/pki/edge-A1,readonly")
if($PeerOnly){$options+=@('--env','PEER_ONLY=1')}else{
    foreach($actor in @('admin','edge-B1')){$options+=@('--mount',"type=bind,source=$PSScriptRoot/.runtime/pki/$actor,target=/pki/$actor,readonly")}
    foreach($node in @('A1','B1')){$options+=@('--mount',"type=bind,source=$PSScriptRoot/.runtime/tenant-$node,target=/registries/$node,readonly")}
    $options+=@('--mount',"type=bind,source=$PSScriptRoot/.runtime/operator,target=/operator,readonly",'--mount',"type=bind,source=$PSScriptRoot/results,target=/fixture")
}
$options+=@('caribou/secure-v3-security:dev','python','/test.py')
& docker @options
if($LASTEXITCODE -ne 0){throw 'V3 live verification failed'}
