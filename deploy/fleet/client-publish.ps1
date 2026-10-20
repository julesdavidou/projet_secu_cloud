param([Parameter(Mandatory)][string]$Email,[string]$Title='Direct laboratoire',[string]$FilePath,[switch]$Public,[ValidateRange(1,3600)][int]$MaxSeconds=3600,[PSCredential]$Credential)
$ErrorActionPreference='Stop'
$profile=Get-Content "$PSScriptRoot/profile.json" -Raw|ConvertFrom-Json
if($profile.roles -notcontains 'publisher'){throw 'Ce bundle ne possède pas le rôle producteur'}
if(-not $Credential){$Credential=Get-Credential -UserName $Email -Message 'Compte Java ; clé privée conservée dans cet agent personnel'}
$password=$Credential.GetNetworkCredential().Password
try{
 $capture='/data/captures/'+[guid]::NewGuid().ToString('N')
 $command=@('python','/app/vendor-video/live_publish.py','--owner-url','http://127.0.0.1:9300','--password-stdin','--email',$Email,'--title',$Title,'--hls-dir',$capture,'--max-seconds',[string]$MaxSeconds)
 if($Public){$command+='--public'}
 if($FilePath){$target='/tmp/source-'+[guid]::NewGuid().ToString('N')+'.mp4';& docker compose --project-directory $PSScriptRoot -f "$PSScriptRoot/compose.json" cp ([IO.Path]::GetFullPath($FilePath)) "owner:$target";if($LASTEXITCODE -ne 0){throw 'Copie source refusée'};$command+=@('--file',$target)}else{$command+=@('--rtmp-listen','rtmp://0.0.0.0:1935/live/stream');Write-Host 'Attendre le message CREATED puis lancer OBS : rtmp://127.0.0.1:22935/live, clé stream, H264/AAC, GOP 2 s.'}
 $password|& docker compose --project-directory $PSScriptRoot -f "$PSScriptRoot/compose.json" exec -T owner @command
 if($LASTEXITCODE -ne 0){throw 'Publication interrompue ; consulter les mesures du producteur'}
}finally{$password=$null;$Credential=$null}
