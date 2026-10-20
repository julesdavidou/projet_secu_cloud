param([Parameter(Mandatory)][string]$Email,[Parameter(Mandatory)][ValidatePattern('^l-[a-f0-9]{32}$')][string]$LiveID,[ValidateSet('a','b')][string]$Slot='a')
$ErrorActionPreference='Stop';$credential=Get-Credential -UserName $Email -Message 'Compte propriétaire du direct';$session=[Microsoft.PowerShell.Commands.WebRequestSession]::new();$port=if($Slot -eq 'a'){20591}else{20592}
try{
 $identity=Invoke-RestMethod "http://localhost:$port/owner/login" -Method Post -WebSession $session -ContentType application/json -Body (@{email=$Email;password=$credential.GetNetworkCredential().Password}|ConvertTo-Json)
 $r=Invoke-RestMethod "http://localhost:$port/owner/live/end" -Method Post -WebSession $session -Headers @{'X-CSRF-Token'=$identity.csrf} -ContentType application/json -Body (@{live_id=$LiveID;action_id=('live-stop-'+[guid]::NewGuid().ToString('N'));reason='owner_requested_stop'}|ConvertTo-Json)
 Write-Host "ENDED $($r.state.id) ; le producteur s'arrête à son prochain contrôle, sous réserve de disponibilité réseau."
}finally{$credential=$null}
