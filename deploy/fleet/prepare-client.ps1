param([Parameter(Mandatory)][ValidatePattern('^[a-z][a-z0-9-]{2,31}$')][string]$ClientID,[Parameter(Mandatory)][string]$ControllerAddress,[string]$Roles='viewer',[string]$NodeID,[string[]]$Owners=@(),[Parameter(Mandatory)][string]$OutputDirectory,[string]$ControlImage='caribou/secure-v3-control:dev',[string]$SecurityImage='caribou/secure-v3-security:dev',[string]$RuntimeDirectory="$PSScriptRoot/../secure-v3/.runtime",[switch]$IncludeImages,[int]$PortOffset=0,[int]$ClientPortOffset=0,[string]$AccountFile="",[string]$ImagesArchive="")
$ErrorActionPreference='Stop'
$runtime=[IO.Path]::GetFullPath($RuntimeDirectory)
if(Test-Path -LiteralPath $OutputDirectory){throw 'Dossier existant : aucun bundle ni secret ne sera remplacé'}
if(Get-Command py -ErrorAction SilentlyContinue){$python='py';$prefix=@('-3','-B')}elseif(Get-Command python -ErrorAction SilentlyContinue){$python='python';$prefix=@('-B')}else{throw 'Python 3.10+ requis'}
$selected=$Roles.ToLowerInvariant().Split(',');$actors=@()
if($selected -contains 'publisher'){$actors+='owner-lab-'+$ClientID}
if(($selected -contains 'viewer') -or ($selected -contains 'publisher')){$actors+='viewer-lab-'+$ClientID}
if($AccountFile){
 $account=Get-Content -LiteralPath $AccountFile -Raw|ConvertFrom-Json
 if($account.client_id -ne $ClientID -or $account.owner_id -notin $Owners -or $selected -notcontains 'publisher'){throw 'Compte privé différent des rôles/propriétaires admis'}
}
$uidValue='1000';if($env:OS -ne 'Windows_NT'){$uidValue=(& id -u).Trim()}
 $provisionArgs=@("-mode","real-http","-secure-v3")
if($actors.Count){$provisionArgs+=@("-clients",($actors -join ","))}
& docker run --rm --network none --user 0:0 --env "HOST_UID=$uidValue" --mount "type=bind,source=$runtime/config.json,target=/input/topology.json,readonly" --mount "type=bind,source=$runtime,target=/output" $ControlImage lab-admin @provisionArgs
if($LASTEXITCODE -ne 0){throw 'Provisionnement client refusé'}
$argsBundle=@('bundle','--runtime',$runtime,'--output',([IO.Path]::GetFullPath($OutputDirectory)),'--client',$ClientID,'--roles',$Roles,'--controller-ip',$ControllerAddress,'--control-image',$ControlImage,'--security-image',$SecurityImage,'--port-offset',[string]$PortOffset,'--client-port-offset',[string]$ClientPortOffset)
if($NodeID){$argsBundle+=@('--node',$NodeID)}
if($Owners.Count){$argsBundle+=@('--owners',($Owners -join ','))}
if($IncludeImages){$argsBundle+='--save-images'}
if($ImagesArchive){$argsBundle+=@('--images-archive',([IO.Path]::GetFullPath($ImagesArchive)))}
& $python @prefix "$PSScriptRoot/secure_client.py" @argsBundle
if($LASTEXITCODE -ne 0){throw 'Export refusé'}

if($AccountFile){Copy-Item -LiteralPath $AccountFile -Destination (Join-Path $OutputDirectory "account.private.json")}
