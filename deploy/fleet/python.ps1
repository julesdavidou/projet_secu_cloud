function Get-FleetPython {
    param([string]$StateDirectory = $PSScriptRoot)
    $env:PYTHONUTF8 = '1'
    $runtime = Join-Path $StateDirectory '.runtime'
    $venvPython = Join-Path $runtime 'venv/Scripts/python.exe'
    New-Item -ItemType Directory -Force $runtime | Out-Null
    if (-not (Test-Path -LiteralPath $venvPython)) {
        if (Get-Command py -ErrorAction SilentlyContinue) {
            & py -3 -m venv "$runtime/venv" | Out-Host
        } elseif (Get-Command python -ErrorAction SilentlyContinue) {
            & python -m venv "$runtime/venv" | Out-Host
        } else {
            throw 'Python 3.10+ est requis sur le PC (pas sur les Edge).'
        }
        if ($LASTEXITCODE -ne 0) { throw 'Creation de l environnement Python impossible.' }
    }
    & $venvPython -c "import sys,importlib.util; sys.exit(0 if sys.version_info >= (3,10) and importlib.util.find_spec('paramiko') and __import__('paramiko').__version__ == '4.0.0' else 1)"
    if ($LASTEXITCODE -ne 0) {
        Write-Host 'Installation de la dependance SSH dans le dossier local .runtime/venv...'
        & $venvPython -m pip install --disable-pip-version-check -r "$PSScriptRoot/requirements.txt" | Out-Host
        if ($LASTEXITCODE -ne 0) { throw 'Installation de Paramiko impossible. Verifier Internet/proxy et Python 3.10+.' }
    }
    return $venvPython
}
