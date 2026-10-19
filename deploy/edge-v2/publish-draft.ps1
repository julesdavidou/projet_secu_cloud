# Publishes only the dedicated integration branch and opens a draft, never merges.
$ErrorActionPreference='Stop'
$branch='codex/edge-mqtt-reconstruction-v2'
$actual=(& git branch --show-current).Trim()
if($actual -ne $branch){throw "Checkout $branch first"}
if(& git status --porcelain){throw 'Commit or preserve local changes before publishing'}
& git fetch origin
if($LASTEXITCODE -ne 0){throw 'GitHub fetch unavailable; nothing published'}
& git push -u origin $branch
if($LASTEXITCODE -ne 0){throw 'Branch push failed; no PR created'}
$credential="protocol=https`nhost=github.com`n`n"|git credential fill
if($LASTEXITCODE -ne 0){throw 'GitHub credentials unavailable'}
$fields=@{}
foreach($line in $credential){$pair=$line -split '=',2;if($pair.Count -eq 2){$fields[$pair[0]]=$pair[1]}}
$headers=@{Authorization=('Bearer '+$fields['password']);Accept='application/vnd.github+json'}
$api='https://api.github.com/repos/julesdavidou/projet_secu_cloud'
$existing=@(Invoke-RestMethod "$api/pulls?state=open&head=julesdavidou%3Acodex%2Fedge-mqtt-reconstruction-v2" -Headers $headers)
if($existing.Count){Write-Output $existing[0].html_url;return}
$body=Get-Content "$PSScriptRoot/../../docs/edge-v2/PR-DRAFT.md" -Raw
$payload=@{title='Integrate Edge MQTT3/5 and real block reconstruction without replacing placement';head=$branch;base='main';draft=$true;body=$body}|ConvertTo-Json
$pr=Invoke-RestMethod "$api/pulls" -Headers $headers -Method Post -ContentType application/json -Body ([Text.Encoding]::UTF8.GetBytes($payload))
Write-Output $pr.html_url
