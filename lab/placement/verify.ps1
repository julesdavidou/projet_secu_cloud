param([switch]$Fresh)
$ErrorActionPreference='Stop'
Set-Location $PSScriptRoot
if (!(Test-Path -LiteralPath '.env')) { Copy-Item -LiteralPath '.env.example' -Destination '.env' }
$composeArgs=@('compose','--profile','placement')
function dc { & docker @composeArgs @args; if ($LASTEXITCODE -ne 0) { throw "docker compose failed ($LASTEXITCODE)" } }
# Fresh removes ONLY this dedicated demonstration project's simulated data.
if ($Fresh) { dc down -v --remove-orphans }
dc config --quiet
dc up -d --build --wait broker central zone-a zone-b a1 a2 b1 b2
dc run --rm --no-deps --build scenario scenario
$before=dc run --rm --no-deps scenario snapshot
dc restart central
dc up -d --wait central
$after=dc run --rm --no-deps scenario snapshot
if (($before -join "`n") -cne ($after -join "`n")) { throw 'Central snapshot changed after restart' }
Write-Output 'PASS 07 central restart preserves exact authoritative catalog'
$capacityBefore=dc run --rm --no-deps scenario capacities
dc restart zone-a zone-b
dc up -d --wait zone-a zone-b
$recovered=$false
for ($i=0;$i -lt 20;$i++) {
  $capacityAfter=dc run --rm --no-deps scenario capacities
  if (($capacityBefore -join "`n") -ceq ($capacityAfter -join "`n")) { $recovered=$true; break }
  Start-Sleep -Milliseconds 500
}
if (!$recovered) { throw 'Zone restart changed available project capacity' }
Write-Output 'PASS 11 zone restart restores durable debits and waits for fresh node liveness'
dc run --rm --no-deps scenario reconcile
dc logs --no-color
# Preserve DB for inspection. `docker compose --profile placement down` stops
# the lab without deleting volumes; use -v only to reset simulated test data.
