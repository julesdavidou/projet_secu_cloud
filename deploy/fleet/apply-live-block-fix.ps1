param([string]$BundleDirectory=$PSScriptRoot)
$ErrorActionPreference='Stop'
$folder=[IO.Path]::GetFullPath($BundleDirectory)
$composeFile=Join-Path $folder 'compose.json'
$module=Join-Path $folder 'secure_live.py'
if(-not(Test-Path $composeFile) -or -not(Test-Path $module)){throw 'Extraire les fichiers du correctif dans le dossier contenant compose.json et start.ps1'}
$profile=Get-Content (Join-Path $folder 'profile.json') -Raw|ConvertFrom-Json
$compose=Get-Content $composeFile -Raw|ConvertFrom-Json
if($compose.name -ne ('caribou-client-'+$profile.client_id) -or -not $compose.services.owner){throw 'Paquet producteur personnel attendu'}
$mount='./secure_live.py:/app/vendor-video/secure_live.py:ro'
if(@($compose.services.owner.volumes | Where-Object {$_ -like '*:/app/vendor-video/secure_live.py*' -and $_ -ne $mount}).Count){throw 'Un autre correctif occupe déjà ce chemin : aucune modification'}
if(-not(Test-Path ($composeFile+'.before-live-block-fix.bak'))){Copy-Item -LiteralPath $composeFile -Destination ($composeFile+'.before-live-block-fix.bak')}
if($mount -notin $compose.services.owner.volumes){$compose.services.owner.volumes=@($compose.services.owner.volumes)+$mount}
[IO.File]::WriteAllText($composeFile,($compose|ConvertTo-Json -Depth 50),[Text.UTF8Encoding]::new($false))
& docker compose --project-directory $folder -f $composeFile config --quiet
if($LASTEXITCODE -ne 0){Copy-Item -LiteralPath ($composeFile+'.before-live-block-fix.bak') -Destination $composeFile;throw 'Configuration refusée ; ancienne configuration restaurée'}
& docker compose --project-directory $folder -f $composeFile up -d --no-deps --force-recreate --wait --wait-timeout 120 owner
if($LASTEXITCODE -ne 0){throw 'Agent indisponible ; volumes conservés, consulter docker compose logs owner'}
Write-Host 'Correctif live monté en lecture seule : blocs de 1 Mio. Clés, compte et volume propriétaire conservés. Reconnecter le portail propriétaire si ouvert.'
