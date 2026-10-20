param([ValidateRange(1,3600)][int]$Seconds=60,[ValidateRange(1,60)][int]$IntervalSeconds=2,[string]$OutputPath="$PSScriptRoot/results/docker-resources.json")
$ErrorActionPreference='Stop';$samples=@();$end=(Get-Date).AddSeconds($Seconds)
New-Item -ItemType Directory -Force ([IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($OutputPath)))|Out-Null
while((Get-Date) -lt $end){
 $ids=@(@(& docker ps -q --filter label=com.docker.compose.project=caribou-live-v4)+@(& docker ps -q --filter label=caribou.project=caribou-live-v4)|Where-Object {$_}|Sort-Object -Unique)
 if($LASTEXITCODE -ne 0 -or $ids.Count -eq 0){throw 'Live V4 project is not running'}
 $lines=& docker stats --no-stream --format '{{json .}}' @ids
 if($LASTEXITCODE -ne 0){throw 'Docker stats failed'}
 $samples+=@{utc=(Get-Date).ToUniversalTime().ToString('o');containers=@($lines|ForEach-Object {$_|ConvertFrom-Json})}
 Start-Sleep -Seconds $IntervalSeconds
}
@{measurement='Docker engine CPU/RAM/network/block IO samples; not physical ARM';interval_seconds=$IntervalSeconds;samples=$samples}|ConvertTo-Json -Depth 8|Set-Content -LiteralPath $OutputPath -Encoding utf8
Write-Host "Resource samples: $OutputPath"
