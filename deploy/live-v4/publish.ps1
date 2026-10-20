param([Parameter(Mandatory)][string]$Email,[ValidateSet('a','b')][string]$Slot='a',[string]$Title='Direct CaribouTV',[switch]$Public,[string]$FilePath,[switch]$Transcode,[ValidateRange(1,3600)][int]$MaxSeconds=3600,[PSCredential]$Credential)
$ErrorActionPreference='Stop'
$file="$PSScriptRoot/.runtime/compose.json";if(-not(Test-Path $file)){throw 'Run deploy/live-v4/start.ps1 first'}
if(-not $Credential){$Credential=Get-Credential -UserName $Email -Message 'Compte Java pour le direct ; mot de passe non enregistré'}
$clear=$credential.GetNetworkCredential().Password
$port=if($Slot -eq 'a'){20591}else{20592};$rtmpPort=if($Slot -eq 'a'){21935}else{21936}
try{
 $identity=Invoke-RestMethod "http://localhost:$port/owner/login" -Method Post -ContentType application/json -Body (@{email=$Email;password=$clear}|ConvertTo-Json)
 foreach($node in @('A1','B1')){& "$PSScriptRoot/../secure-v3/provision-tenant.ps1" -Owner $identity.owner_id -Node $node -Project caribou-live-v4 -ControlImage caribou/live-v4-control:dev -SecurityImage caribou/live-v4-security:dev -RuntimeDirectory "$PSScriptRoot/.runtime"}
 $capture='/data/captures/'+[guid]::NewGuid().ToString('N');$argsLive=@('python','/app/vendor-video/live_publish.py','--owner-url','http://127.0.0.1:9300','--password-stdin','--email',$Email,'--title',$Title,'--hls-dir',$capture,'--max-seconds',[string]$MaxSeconds)
 if($Public){$argsLive+='--public'};if($Transcode){$argsLive+='--transcode'}
 if($FilePath){
  $target='/tmp/live-input-'+[guid]::NewGuid().ToString('N')+'.mp4'
  & docker compose --project-directory $PSScriptRoot -f $file cp ([IO.Path]::GetFullPath($FilePath)) "owner-${Slot}:$target"
  if($LASTEXITCODE -ne 0){throw 'Source copy failed'}
  $argsLive+=@('--file',$target)
 }else{
  $argsLive+=@('--rtmp-listen','rtmp://0.0.0.0:1935/live/stream')
  Write-Host "OBS: serveur rtmp://127.0.0.1:$rtmpPort/live ; clé stream ; H264/AAC, intervalle d'image clé 2 s. Arrêter OBS termine le direct."
 }
 $clear|& docker compose --project-directory $PSScriptRoot -f $file exec -T "owner-$Slot" @argsLive
 if($LASTEXITCODE -ne 0){throw 'Live failed; inspect the publisher measurements and local FFmpeg log'}
}finally{$clear=$null;$credential=$null}
