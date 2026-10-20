param([switch]$WithVod)
$ErrorActionPreference='Stop'
New-Item -ItemType Directory -Force "$PSScriptRoot/results"|Out-Null
if(-not (Get-Command docker -ErrorAction SilentlyContinue)){$env:PATH="${env:LOCALAPPDATA}/Programs/DockerDesktop/resources/bin;"+$env:PATH}
function D {& docker @args;if($LASTEXITCODE -ne 0){throw "Docker failed ($LASTEXITCODE)"}}
function IPOf([string]$name){$v=(& docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $name).Trim();if($LASTEXITCODE -ne 0 -or $v -notmatch '^\d+\.\d+\.\d+\.\d+$'){throw "Missing bridge IP: $name"};return $v}
$aIP=IPOf 'caribou-secure-v3-edge-a-1';$bIP=IPOf 'caribou-secure-v3-edge-b-1'
$brokerA=IPOf 'caribou-secure-v3-broker-a-1';$brokerB=IPOf 'caribou-secure-v3-broker-b-1'
$workerA=IPOf 'caribou-secure-v3-reconstructor-a-1';$workerB=IPOf 'caribou-secure-v3-reconstructor-b-1'
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
$names=@()
try{
 foreach($entry in @(@('a','A1',$brokerA,$workerA),@('b','B1',$brokerB,$workerB))){
  $name='caribou-v3-dns-advert-'+$entry[0]+'-'+[guid]::NewGuid().ToString('N').Substring(0,8);$names+=$name
  D run -d --rm --name $name --cap-drop ALL --security-opt no-new-privileges:true --network "container:caribou-secure-v3-edge-$($entry[0])-1" caribou/secure-v3-security:dev python /app/lan.py advertise --node $entry[1] --broker-address $entry[2] --worker-address $entry[3] --worker-port 8443 --duration 360
 }
 foreach($entry in @(@('a','A','A1'),@('b','B','B1'))){
  $name='caribou-v3-dns-browse-'+$entry[0]+'-'+[guid]::NewGuid().ToString('N').Substring(0,8);$names+=$name
  $out="$PSScriptRoot/.runtime/lan-$($entry[2])";New-Item -ItemType Directory -Force $out|Out-Null
  D run -d --rm --name $name --user "${uidValue}:10001" --cap-drop ALL --security-opt no-new-privileges:true --network caribou-secure-v3_default --mount "type=bind,source=$PSScriptRoot/.runtime/pki/zone-$($entry[1]),target=/pki,readonly" --mount "type=bind,source=$out,target=/out" --mount "type=bind,source=$PSScriptRoot/topology.json,target=/topology.json,readonly" caribou/secure-v3-security:dev python /app/lan.py browse --zone-url "https://zone-$($entry[0]):8443" --tls-dir /pki --output /out --self-node $entry[2] --topology /topology.json --duration 360
 }
 D run --rm --network caribou-secure-v3_default --user "${uidValue}:10001" --cap-drop ALL --security-opt no-new-privileges:true --env "EDGE_A_IP=$aIP" --env "EDGE_B_IP=$bIP" --mount "type=bind,source=$PSScriptRoot/discovery-check.py,target=/test.py,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime/pki/zone-A,target=/pki/zone-A,readonly" --mount "type=bind,source=$PSScriptRoot/.runtime/pki/edge-A1,target=/pki/edge-A1,readonly" --mount "type=bind,source=$PSScriptRoot/results,target=/results" caribou/secure-v3-security:dev python /test.py
 # Verify generated peer links are loaded, then test ACKs with the hub absent.
 D compose -f "$PSScriptRoot/compose.yaml" exec -T broker-a sh -c 'grep -q "connection discovered-A1-to-B1" /tmp/secure/broker.conf'
 D compose -f "$PSScriptRoot/compose.yaml" stop broker
 try {
  & "$PSScriptRoot/verify.ps1" -PeerOnly
  # More than a complete 30-second lease: peer discovery and bridges must
  # remain live without the hub, not merely reuse an unexpired startup cache.
  Start-Sleep -Seconds 35
  D compose -f "$PSScriptRoot/compose.yaml" exec -T broker-a sh -c 'grep -q "connection discovered-A1-to-B1" /tmp/secure/broker.conf'
  & "$PSScriptRoot/verify.ps1" -PeerOnly
 } finally {D compose -f "$PSScriptRoot/compose.yaml" start broker}
 if($WithVod){D compose -f "$PSScriptRoot/compose.yaml" up -d --wait --wait-timeout 120;& "$PSScriptRoot/smoke.ps1"}
}finally{
 foreach($name in $names){& docker logs --tail 15 $name; & docker stop --time 2 $name|Out-Null}
}
