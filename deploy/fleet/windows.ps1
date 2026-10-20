param([string]$BundleDirectory=$PSScriptRoot,[Nullable[double]]$QuotaGiB,[string]$RegistryUser='',[string]$InterfaceIPv4='',[switch]$NoDiscovery)
$ErrorActionPreference='Stop'
$BundleDirectory=[IO.Path]::GetFullPath($BundleDirectory)
$profile=Get-Content "$BundleDirectory/profile.json" -Raw|ConvertFrom-Json
if(Get-Command py -ErrorAction SilentlyContinue){$python='py';$prefix=@('-3','-B')}elseif(Get-Command python -ErrorAction SilentlyContinue){$python='python';$prefix=@('-B')}else{throw 'Python 3.10+ requis. Installer les prérequis avant la séance.'}
if(-not(Get-Command pwsh -ErrorAction SilentlyContinue)){throw 'PowerShell 7 requis'}
if($profile.roles -contains 'storage'){
 if($null -eq $QuotaGiB){$QuotaGiB=[double](Read-Host "Quota projet en GiB (maximum $($profile.max_quota_bytes/1GB))")}
}
$argsInstall=@('install','--bundle',$BundleDirectory)
if($null -ne $QuotaGiB){$argsInstall+=@('--quota-gib',([double]$QuotaGiB).ToString([Globalization.CultureInfo]::InvariantCulture))}
if($RegistryUser){$argsInstall+=@('--registry-user',$RegistryUser)}
& $python @prefix "$BundleDirectory/secure_client.py" @argsInstall
if($LASTEXITCODE -ne 0){throw 'Installation refusée ; volumes conservés'}
if(($profile.roles -contains 'storage') -and -not $NoDiscovery){
 $venv="$BundleDirectory/.runtime/lan-venv";$lanPython="$venv/Scripts/python.exe"
 if(-not(Test-Path $lanPython)){& $python @prefix -m venv $venv;if($LASTEXITCODE -ne 0){throw 'Création Python LAN impossible'}}
 $oldSkip=$env:SKIP_CYTHON
 try{$env:SKIP_CYTHON='1';& $lanPython -m pip install --disable-pip-version-check --no-binary zeroconf -r "$BundleDirectory/lan-requirements.txt";if($LASTEXITCODE -ne 0){throw 'Dépendances LAN indisponibles'}}finally{if($null -eq $oldSkip){Remove-Item Env:SKIP_CYTHON -ErrorAction SilentlyContinue}else{$env:SKIP_CYTHON=$oldSkip}}
 if(-not $InterfaceIPv4){$InterfaceIPv4=(Get-NetIPAddress -InterfaceAlias 'Wi-Fi' -AddressFamily IPv4|Where-Object {$_.AddressState -eq 'Preferred'}|Select-Object -First 1).IPAddress}
 if(-not $InterfaceIPv4){throw 'Préciser -InterfaceIPv4 pour annoncer la bonne interface LAN'}
 $stateFile="$BundleDirectory/.runtime/discovery-processes.json"
 if(Test-Path $stateFile){
  foreach($entry in (Get-Content $stateFile -Raw|ConvertFrom-Json)){
   $process=Get-CimInstance Win32_Process -Filter "ProcessId = $($entry.pid)"
   if($process){if(-not $process.CommandLine.Contains("$BundleDirectory/lan.py")){throw 'PID ne correspondant pas à ce bundle'};Stop-Process -Id $entry.pid}
  }
 }
 $offset=$profile.port_offset;$node=$profile.node_id;$zone=(Get-Content "$BundleDirectory/.runtime/config.json" -Raw|ConvertFrom-Json).nodes|Where-Object {$_.id -eq $node}
 $url="https://zone-$($zone.zone):$(if($zone.zone -eq 'A'){19691+$offset}else{19692+$offset})"
 $commands=@(@('-B','-u',"$BundleDirectory/lan.py",'advertise','--node',$node,'--interface',$InterfaceIPv4,'--http-port',[string]$profile.storage_ports.http,'--broker-port',[string]$profile.storage_ports.broker,'--worker-port',[string]$profile.storage_ports.worker),@('-B','-u',"$BundleDirectory/lan.py",'mesh','--self-node',$node,'--zone-url',$url,'--zone-ip',$profile.controller_ip,'--tls-dir',"$BundleDirectory/.runtime/pki/edge-$node",'--topology',"$BundleDirectory/.runtime/config.json",'--output',"$BundleDirectory/.runtime/lan-$node"))
 # Windows Start-Process joins arrays; quote every argument including paths with spaces.
 # These fixed flags, IDs, IPs, URLs and file paths contain no embedded quotes
 # or terminal backslash; Windows file names cannot contain a double quote.
 $quoted=@();foreach($cmdArgs in $commands){$quoted+=,@($cmdArgs|ForEach-Object {'"'+$_+'"'})}
 $pids=@();for($i=0;$i -lt 2;$i++){$p=Start-Process -FilePath $lanPython -ArgumentList ($quoted[$i] -join ' ') -WindowStyle Hidden -RedirectStandardOutput "$BundleDirectory/.runtime/discovery-$i.log" -RedirectStandardError "$BundleDirectory/.runtime/discovery-$i.err.log" -PassThru;$pids+=@{pid=$p.Id;role=$i}}
 $pids|ConvertTo-Json|Set-Content $stateFile -Encoding utf8
 Write-Host 'Découverte lancée. L admission et un test de lecture réel doivent encore être vérifiés avec le contrôleur.'
}
if($profile.roles -contains 'viewer'){Write-Host "Spectateur : $($profile.viewer_url) ; connexion Java habituelle."}
if($profile.roles -contains 'publisher'){Write-Host "Producteur : $($profile.owner_url) ; puis ./publish.ps1 -Email VOTRE_COMPTE. OBS : $($profile.rtmp_url), clé stream."}
