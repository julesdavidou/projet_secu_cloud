param(
    [Parameter(Mandatory)][string[]]$Files,
    [Parameter(Mandatory)][string]$SourceNodeId,
    [string]$VideoId=('manual-'+[DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds())
)
. "$PSScriptRoot/common.ps1"
$base=Get-CentralBase
$auth=@{Authorization="Bearer $(Get-LabSecret 'operator.token')"}
$segments=@();$contents=@();$total=0
for($i=0;$i -lt $Files.Count;$i++){
    $Files[$i]=(Resolve-Path $Files[$i]).Path
    $size=(Get-Item $Files[$i]).Length
    if($size -le 0 -or $size -gt 1GB){throw 'Use positive files <= 1 GiB; prefer small synthetic segments for tomorrow.'}
    $total+=$size
    $segments+=@{segment_id=$i;size_bytes=$size}
    $contents+=@{extent_id=$i;sha256=(Get-FileHash $Files[$i] -Algorithm SHA256).Hash.ToLowerInvariant()}
}
# Target=1 guarantees one indivisible positive segment per extent in the Go
# contract. The client supplies content hashes; only Go chooses destinations.
$request=@{manifest=@{request_id=$VideoId;video_id=$VideoId;source_node_id=$SourceNodeId;total_size_bytes=$total;segments=$segments};target_size_bytes=1;contents=$contents}
$plan=Invoke-RestMethod "$base/api/v1/uploads" -Method Post -Headers $auth -ContentType application/json -Body ($request|ConvertTo-Json -Depth 20)
Write-Host "Go plan: $($plan.units.Count) extents, expires $($plan.expires_at). PREPARE is not completion."
foreach($unit in $plan.units){
    if($unit.first_segment -ne $unit.last_segment -or $unit.extent_id -ne $unit.first_segment){throw 'Unexpected Go extent boundaries; refusing to guess a client-side plan.'}
    $nodeAuth=@{Authorization="Bearer $(Get-LabSecret "edge-$($unit.node_id).http-token")"}
    Invoke-WebRequest -UseBasicParsing "$($unit.endpoint)/store/$VideoId/$($unit.extent_id)" -Method Put -Headers $nodeAuth -ContentType application/octet-stream -InFile $Files[$unit.first_segment]|Out-Null
    Write-Host "STORED verified extent $($unit.extent_id) on $($unit.node_id) via $($unit.endpoint)"
}
$result=Invoke-RestMethod "$base/api/v1/videos/$VideoId/placements" -Headers $auth
if($result.state -ne 'READY' -or @($result.units|Where-Object proof -ne 'HTTP_SHA256_VERIFIED').Count){throw 'Central has not confirmed all real verified completions.'}
$result|ConvertTo-Json -Depth 30
