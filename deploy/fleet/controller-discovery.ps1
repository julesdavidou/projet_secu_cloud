param([Parameter(Mandatory)][string]$InterfaceIPv4,[string]$RuntimeDirectory="$PSScriptRoot/../secure-v3/.runtime")
$ErrorActionPreference='Stop'
$runtime=[IO.Path]::GetFullPath($RuntimeDirectory)
$lan=[IO.Path]::GetFullPath("$PSScriptRoot/../secure-v3/lan.py")
if(-not(Get-NetIPAddress -IPAddress $InterfaceIPv4 -AddressFamily IPv4 -ErrorAction SilentlyContinue)){throw 'Adresse absente de ce PC'}
$python="$runtime/lan-pure-venv/Scripts/python.exe"
if(-not(Test-Path $python)){
 if(Get-Command py -ErrorAction SilentlyContinue){& py -3 -m venv "$runtime/lan-pure-venv"}else{& python -m venv "$runtime/lan-pure-venv"}
 if($LASTEXITCODE -ne 0){throw 'Environnement Python LAN impossible'}
 $old=$env:SKIP_CYTHON
 try{$env:SKIP_CYTHON='1';& $python -m pip install --no-binary zeroconf -r "$PSScriptRoot/../secure-v3/lan-requirements.txt";if($LASTEXITCODE -ne 0){throw 'Dépendances LAN impossibles'}}finally{if($null -eq $old){Remove-Item Env:SKIP_CYTHON -ErrorAction SilentlyContinue}else{$env:SKIP_CYTHON=$old}}
}
$stateFile="$runtime/pc-lan-processes.json"
if(Test-Path $stateFile){
 foreach($entry in (Get-Content $stateFile -Raw|ConvertFrom-Json).processes){
  $p=Get-CimInstance Win32_Process -Filter "ProcessId = $($entry.pid)"
  if($p){if(-not $p.CommandLine.Replace('/','\').Contains($lan.Replace('/','\'))){throw 'PID ne correspondant pas aux assistants de ce dépôt'};Stop-Process -Id $entry.pid}
 }
}
$commands=@()
foreach($entry in @(@('A','A1',19691),@('B','B1',19692))){
 $zone=$entry[0];$node=$entry[1];New-Item -ItemType Directory -Force "$runtime/lan-$node"|Out-Null
 $commands+=,@('-B','-u',$lan,'browse','--zone-url',"https://zone-${zone}:$($entry[2])",'--zone-ip','127.0.0.1','--tls-dir',"$runtime/pki/zone-$zone",'--interface',$InterfaceIPv4,'--output',"$runtime/lan-$node",'--self-node',$node,'--topology',"$runtime/config.json",'--results',"$runtime/lan-browse-$zone.json")
}
foreach($entry in @(@('A1',19681,19684,19584),@('B1',19682,19685,19585))){
 $commands+=,@('-B','-u',$lan,'advertise','--node',$entry[0],'--interface',$InterfaceIPv4,'--http-port',[string]$entry[1],'--broker-port',[string]$entry[2],'--worker-port',[string]$entry[3])
}
$records=@();$stamp=[guid]::NewGuid().ToString('N').Substring(0,8)
for($i=0;$i -lt $commands.Count;$i++){
 $quoted=($commands[$i]|ForEach-Object {'"'+$_+'"'}) -join ' '
 $log="$runtime/controller-discovery-$stamp-$i.log";$err="$runtime/controller-discovery-$stamp-$i.err.log"
 $p=Start-Process -FilePath $python -ArgumentList $quoted -WindowStyle Hidden -RedirectStandardOutput $log -RedirectStandardError $err -PassThru
 $records+=@{pid=$p.Id;role=$i;log=$log;errors=$err}
 @{ip=$InterfaceIPv4;processes=$records}|ConvertTo-Json -Depth 5|Set-Content $stateFile -Encoding utf8
 Start-Sleep -Milliseconds 400;$p.Refresh();if($p.HasExited){throw "Assistant LAN arrêté ; consulter $err"}
}
Write-Host 'Découverte des zones et annonces A1/B1 actives ; état dans .runtime/pc-lan-processes.json.'
