"""Déploiement du main GitHub et tests du parc CaribouTV, sans push Git.

Le PC exécute les contrôleurs/web ; les cinq hôtes exécutent agent + bridge.
Les secrets locaux et rapports restent hors Git dans inventory.local.json/.runtime.
"""
from __future__ import annotations

import argparse
import concurrent.futures
import datetime as dt
import getpass
import tempfile
import gzip
import hashlib
import ipaddress
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import socket
import subprocess
import sys
import tarfile
import threading
import time
import urllib.error
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1] if HERE.name=='fleet' and HERE.parent.name=='deploy' else HERE.parent/'projet_secu_cloud'
from registry import Registry, RegistryError, require_digest
SENSITIVE = []
PRINT_LOCK = threading.Lock()


class FleetError(RuntimeError):
    pass


def log(message):
    with PRINT_LOCK:
        for secret in SENSITIVE: message = str(message).replace(secret,'[masqué]')
        print(message, flush=True)


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix('.tmp')
    temp.write_text(json.dumps(value, indent=2, ensure_ascii=False), encoding='utf-8')
    os.replace(temp, path)


def read_json(path, default=None):
    return json.loads(Path(path).read_text(encoding='utf-8-sig')) if Path(path).exists() else default


def file_hash(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024*1024), b''): digest.update(chunk)
    return digest.hexdigest()


def acquire_lock(runtime):
    stream = (runtime/'operation.lock').open('a+b')
    stream.seek(0)
    if os.name == 'nt':
        import msvcrt
        if os.fstat(stream.fileno()).st_size == 0: stream.write(b'0'); stream.flush()
        stream.seek(0)
        try: msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
        except OSError:
            stream.close()
            raise FleetError('Un autre programme fleet utilise déjà ce dépôt.')
    else:
        import fcntl
        try: fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            stream.close()
            raise FleetError('Un autre programme fleet utilise déjà ce dépôt.')
    return stream


def architecture(value):
    aliases = {'x86_64': 'amd64', 'amd64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64', 'armv7l':'arm/v7', 'arm/v7':'arm/v7'}
    if value not in aliases:
        raise FleetError(f'Architecture {value!r} non prise en charge : ARMv7, ARM64 ou AMD64 requis.')
    return aliases[value]


def compose_version(value, minimum=(2,24,4)):
    match = re.search(r'(\d+)\.(\d+)\.(\d+)', value)
    if not match or tuple(map(int, match.groups())) < minimum:
        raise FleetError(f'Docker Compose {".".join(map(str,minimum))}+ requis, reçu : {value.strip()}')


def load_inventory(path):
    inventory = read_json(path)
    if not isinstance(inventory, dict) or not isinstance(inventory.get('nodes'), list) or not inventory['nodes']:
        raise FleetError(f'Inventaire absent/invalide : {path}. Copier inventory.example.json puis renseigner les accès.')
    if inventory.get('remote_directory', 'cariboutv-lab') != 'cariboutv-lab':
        raise FleetError('Le dossier distant doit être cariboutv-lab, sous le HOME du compte SSH.')
    ids, hosts = set(), set()
    for node in inventory['nodes']:
        if not re.fullmatch(r'[a-zA-Z0-9_-]{1,64}', node.get('id', '')):
            raise FleetError('Identifiant Edge invalide.')
        if node['id'] in ids or node['host'] in hosts:
            raise FleetError('Identifiants et adresses des Edge doivent être uniques.')
        ids.add(node['id']); hosts.add(node['host'])
        ipaddress.IPv4Address(node['host'])
        if node.get('zone') not in ('A', 'B'):
            raise FleetError('Le Compose actuel fournit les zones A/B uniquement.')
        if not re.fullmatch(r'[a-zA-Z0-9_-]+', node.get('user', '')):
            raise FleetError('Compte SSH invalide.')
        if not node.get('password') or node['password'] == 'CHANGE_ME':
            raise FleetError(f"Mot de passe à renseigner pour {node['id']}.")
        if not isinstance(node.get('quota_bytes'), int) or not 0 < node['quota_bytes'] <= 2**63 - 1:
            raise FleetError('Quota positif en octets requis.')
        if not isinstance(node.get('http_port'), int) or not 1024 <= node['http_port'] <= 65535:
            raise FleetError('Port HTTP Edge invalide.')
    for key, default in [('mqtt_port',1884),('web_port',18090),('central_port',19088)]:
        port = inventory.setdefault(key, default)
        if not isinstance(port, int) or not 1024 <= port <= 65535:
            raise FleetError(f'Port {key} invalide.')
    return inventory


def select_nodes(inventory, selection):
    """Filter the active fleet without changing the stored inventory."""
    if selection is None:
        return inventory
    ids = [value.strip() for value in selection.split(',')] if isinstance(selection, str) else list(selection)
    if not ids or any(not isinstance(value, str) or not value for value in ids):
        raise FleetError('Sélection vide : fournir au moins un identifiant à -Nodes.')
    if len(ids) != len(set(ids)):
        raise FleetError('Un identifiant apparaît plusieurs fois dans -Nodes.')
    known = {node['id'] for node in inventory['nodes']}
    unknown = set(ids) - known
    if unknown:
        raise FleetError('Nœuds inconnus : '+', '.join(sorted(unknown))+'. Disponibles : '+', '.join(sorted(known)))
    return {**inventory, 'nodes':[node for node in inventory['nodes'] if node['id'] in ids]}


def topology(inventory):
    return {'zones':['A','B'], 'node_ttl_seconds':30, 'lease_seconds':300, 'fanout':2,
            'nodes':[{'id':n['id'], 'zone':n['zone'], 'quota_bytes':n['quota_bytes'],
                      'endpoint':f"http://{n['host']}:{n['http_port']}", 'allowed_zones':['A','B'],
                      'mode':'real-http'} for n in inventory['nodes']]}


def local(args, *, cwd=None, env=None, stream=False, timeout=1800):
    result = subprocess.run([str(a) for a in args], cwd=cwd or ROOT, env=env, timeout=timeout,
                            text=True, encoding='utf-8', errors='replace',
                            stdout=None if stream else subprocess.PIPE,
                            stderr=None if stream else subprocess.PIPE)
    if result.returncode:
        detail = (result.stderr or result.stdout or '')[-4000:]
        raise FleetError(f'Commande locale échouée ({result.returncode}) : {args[0]} {args[1]}\n{detail}')
    return (result.stdout or '').strip()


class SSH:
    def __init__(self, node, runtime):
        import paramiko
        self.node = node
        self.client = paramiko.SSHClient()
        keys = runtime / 'known-hosts' / node['id']
        keys.parent.mkdir(parents=True, exist_ok=True)
        keys.touch(exist_ok=True)
        self.client.load_host_keys(str(keys))
        self.client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        try:
            self.client.connect(node['host'], port=node.get('ssh_port',22), username=node['user'],
                                password=node['password'], allow_agent=False, look_for_keys=False,
                                timeout=8, auth_timeout=12, banner_timeout=12)
            self.client.get_transport().set_keepalive(15)
        except Exception:
            self.client.close()
            raise

    def __enter__(self): return self
    def __exit__(self, *unused): self.client.close()

    def run(self, command, *, sudo=False, timeout=30, check=True, stream=False, input_data=None):
        if sudo:
            # Read the password ourselves: sudo may have NOPASSWD/cached credentials.
            # This always leaves the remaining stdin exclusively for the command.
            command = ("IFS= read -r fleet_sudo; printf '%s\\n' \"$fleet_sudo\" | "
                       'sudo -S -p "" -- true && sudo -n -- sh -c '+shlex.quote(command))
        channel = self.client.get_transport().open_session(timeout=10)
        channel.exec_command(command)
        if sudo:
            channel.sendall((self.node['password']+'\n').encode())
        if input_data is not None: channel.sendall(input_data.encode())
        channel.shutdown_write()
        deadline = time.monotonic() + timeout
        output, errors = [], []
        try:
            while True:
                if channel.recv_ready():
                    chunk = channel.recv(65536).decode('utf-8', 'replace')
                    output.append(chunk)
                    if stream: log(f"[{self.node['id']}] {chunk.rstrip()}")
                if channel.recv_stderr_ready():
                    chunk = channel.recv_stderr(65536).decode('utf-8','replace')
                    errors.append(chunk)
                    if stream: log(f"[{self.node['id']}] {chunk.rstrip()}")
                if channel.exit_status_ready() and not channel.recv_ready() and not channel.recv_stderr_ready():
                    break
                if time.monotonic() > deadline:
                    raise FleetError(f"{self.node['id']} : délai SSH dépassé ({timeout}s)")
                time.sleep(.04)
            status = channel.recv_exit_status()
        finally:
            channel.close()
        text = ''.join(output).strip()
        if status and check:
            detail = (''.join(errors) or text)[-3000:].replace(self.node['password'], '[masqué]')
            raise FleetError(f"{self.node['id']} : commande distante échouée ({status}) : {detail}")
        return text if check else (status, text)

    def put(self, source, destination):
        with self.client.open_sftp() as sftp:
            last = [time.monotonic()]
            def progress(done, total):
                if time.monotonic() - last[0] > 15:
                    log(f"[{self.node['id']}] transfert {Path(source).name} : {done*100//max(1,total)} %")
                    last[0] = time.monotonic()
            sftp.put(str(source), destination, callback=progress)


def inspect_node(node, runtime):
    with SSH(node, runtime) as ssh:
        home = ssh.run('printf %s "$HOME"')
        if not (home.startswith('/home/') or home == '/root') or '\n' in home or '..' in PurePosixPath(home).parts:
            raise FleetError('HOME distant invalide.')
        if ssh.run('uname -s') != 'Linux': raise FleetError('Un OS Linux est requis.')
        ssh.run('true', sudo=True)
        info = ssh.run("docker info --format '{{.OSType}} {{.Architecture}}'", sudo=True).split()
        if len(info) != 2 or info[0] != 'linux': raise FleetError('Docker doit utiliser des conteneurs Linux.')
        arch = architecture(info[1])
        bits = ssh.run('getconf LONG_BIT')
        if bits != ('32' if arch=='arm/v7' else '64'): raise FleetError('Architecture du système incohérente avec Docker.')
        dedicated = shlex.quote(home+'/cariboutv-lab/tools/docker-compose')
        code,version = ssh.run(f'if [ -x {dedicated} ]; then {dedicated} version --short; else docker compose version --short; fi',sudo=True,check=False)
        install_compose = bool(code)
        if not install_compose:
            try: compose_version(version,minimum=(2,20,0))
            except FleetError: install_compose=True
        ssh.run('command -v tar; command -v sha256sum; command -v flock; command -v install')
        remote_time = int(ssh.run('date +%s'))
        if abs(remote_time - time.time()) > 3:
            raise FleetError('Horloge désynchronisée : synchroniser NTP avant les baux et heartbeats.')
        disk = ssh.run('df -Pk '+shlex.quote(home)).splitlines()[-1].split()
        if int(disk[3])*1024 < 512*1024*1024: raise FleetError('Moins de 512 Mio libres sur le HOME distant.')
        log(f"[{node['id']}] Dossier cible : {home}/cariboutv-lab (ce contrôle ne le crée pas).")
        if install_compose:
            log(f"[{node['id']}] Compose absent/incompatible : installation automatique prévue au déploiement, pas encore effectuée.")
        else:
            log(f"[{node['id']}] Compose disponible : {version}.")
        return {'home':home, 'architecture':arch, 'endpoint':f"http://{node['host']}:{node['http_port']}",
                'compose_version':version or None,'install_compose':install_compose}


def parallel(nodes, jobs, action, report):
    results = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=jobs) as executor:
        futures = {executor.submit(action,n):n for n in nodes}
        for future in concurrent.futures.as_completed(futures):
            node = futures[future]
            try:
                value = future.result()
                results[node['id']] = value
                report.append({'node':node['id'], 'ok':True, 'details':value})
                log(f"PASS {node['id']}")
            except Exception as error:
                message = str(error).replace(node['password'],'[masqué]')
                report.append({'node':node['id'], 'ok':False, 'error':message})
                log(f"FAIL {node['id']} : {message}")
    if len(results) != len(nodes): raise FleetError('Une ou plusieurs machines ont échoué ; consulter le rapport.')
    return results


def prepare_source(runtime, skip_fetch, revision=None):
    if not skip_fetch: local(['git','fetch','origin','main'], stream=True)
    if revision and not re.fullmatch('[0-9a-f]{40}',revision): raise FleetError('-Revision attend un SHA Git complet de 40 caractères.')
    commit = local(['git','rev-parse',revision or 'refs/remotes/origin/main'])
    if not re.fullmatch('[0-9a-f]{40}',commit): raise FleetError('Référence origin/main invalide.')
    archive = runtime / f'source-{commit}.tar.gz'
    source = runtime / 'sources' / commit
    if not archive.exists():
        raw = runtime / 'source.tar'
        local(['git','-c','core.autocrlf=false','archive','--format=tar','--output',str(raw),commit])
        pending_archive = archive.with_suffix('.tmp')
        with raw.open('rb') as src, pending_archive.open('wb') as dst:
            with gzip.GzipFile(fileobj=dst, mode='wb', mtime=0) as zipped: shutil.copyfileobj(src,zipped)
        os.replace(pending_archive,archive)
        raw.unlink()
    source.mkdir(parents=True, exist_ok=True)
    if not (source/'.complete').exists():
        with tarfile.open(archive) as tar:
            members = tar.getmembers()
            for member in members:
                p = PurePosixPath(member.name)
                if p.is_absolute() or '..' in p.parts or not (member.isfile() or member.isdir()):
                    raise FleetError('Archive Git refusée : chemin/lien non pris en charge.')
            if sys.version_info >= (3,12): tar.extractall(source, members=members, filter='data')
            else: tar.extractall(source,members=members)
        (source/'.complete').write_text(commit,encoding='ascii')
    for file in ('deploy/integration/start.ps1','deploy/lab/compose.edge.yaml','deploy/lab/Dockerfile'):
        if not (source/file).is_file(): raise FleetError('Le commit ne contient pas encore le déploiement intégré.')
    # Les scripts PC sont ceux de cette copie du dépôt, donc exigés identiques à main.
    for file in ('deploy/integration/start.ps1','deploy/integration/compose.yaml',
                 'deploy/integration/compose.external.yaml','deploy/lab/config/mosquitto.conf',
                 'deploy/lab/config/broker-entrypoint.sh'):
        if (ROOT/file).read_bytes().replace(b'\r\n',b'\n') != (source/file).read_bytes().replace(b'\r\n',b'\n'):
            raise FleetError(f'{file} diffère du commit demandé : mettre le dépôt PC à cette version avant de déployer.')
    return commit, archive, source


def image_info(tag):
    try: return json.loads(local(['docker','image','inspect',tag]))[0]
    except FleetError: return None


def build_image(source, commit, tag, dockerfile, *, arch=None, target=None, skip=False):
    info = image_info(tag)
    expected = info and (info.get('Config',{}).get('Labels') or {}).get('org.caribou.source-revision') == commit
    if arch: expected = expected and info.get('Architecture') == arch.split('/')[0]
    if arch == 'arm/v7' and info and info.get('Variant') not in (None,'v7'): expected = False
    if not expected:
        if skip: raise FleetError(f'Image {tag} absente ou provenant d’un autre commit : relancer sans -SkipBuild.')
        args = ['docker','buildx','build','--load'] if arch else ['docker','build']
        if arch: args += ['--platform',f'linux/{arch}']
        args += ['--build-arg',f'VERSION={commit}','--label',f'org.opencontainers.image.revision={commit}','--label',f'org.caribou.source-revision={commit}','-f',str(source/dockerfile),'-t',tag]
        if target: args += ['--target',target]
        log(f'Construction {tag}...')
        local(args+[str(source)],stream=True)
        info = image_info(tag)
    if not info: raise FleetError(f'Image introuvable : {tag}')
    if (info.get('Config',{}).get('Labels') or {}).get('org.caribou.source-revision') != commit:
        raise FleetError('Révision de l’image locale incorrecte après construction.')
    return info['Id']


def compose_binary(arch,runtime):
    filename = 'docker-compose-linux-'+{'arm/v7':'armv7','arm64':'aarch64','amd64':'x86_64'}[arch]
    destination = runtime/'tools'/filename
    destination.parent.mkdir(parents=True,exist_ok=True)
    url = 'https://github.com/docker/compose/releases/download/v2.26.1/'+filename
    with urllib.request.urlopen(url+'.sha256',timeout=60) as response:
        expected = response.read().decode().split()[0]
    if not re.fullmatch('[a-fA-F0-9]{64}',expected): raise FleetError('SHA256 du binaire Compose invalide.')
    if not destination.exists() or file_hash(destination) != expected:
        log(f'Téléchargement du Compose dédié {arch}...')
        with urllib.request.urlopen(url,timeout=120) as response, destination.with_suffix('.tmp').open('wb') as output:
            shutil.copyfileobj(response,output)
        if file_hash(destination.with_suffix('.tmp')) != expected: raise FleetError('SHA256 du binaire Compose incorrect.')
        os.replace(destination.with_suffix('.tmp'),destination)
    return destination,expected


def archive_config_digest(archive,tag,commit,arch):
    # Docker Desktop/containerd .Id may be an OCI index. Legacy Engines use
    # sha256(config JSON). Docker save's manifest.json identifies that config.
    with tarfile.open(archive) as tar:
        rows = json.load(tar.extractfile('manifest.json'))
        matches = [row for row in rows if tag in (row.get('RepoTags') or [])]
        if len(matches)!=1: raise FleetError('Archive Docker sans image unique pour le tag demandé.')
        data = tar.extractfile(matches[0]['Config']).read()
    info = json.loads(data)
    labels = info.get('config',{}).get('Labels') or {}
    if info.get('os')!='linux' or info.get('architecture')!=arch.split('/')[0] or labels.get('org.caribou.source-revision')!=commit:
        raise FleetError('Plateforme/révision de la configuration exportée incorrecte.')
    if labels.get('org.opencontainers.image.revision',commit)!=commit:
        raise FleetError('Révision OCI de la configuration exportée incorrecte.')
    if arch=='arm/v7' and info.get('variant')!='v7': raise FleetError('Archive ARM sans variante v7 confirmée.')
    return 'sha256:'+hashlib.sha256(data).hexdigest()


def validate_image(info,plan):
    accepted = {plan['config_digest'],plan.get('digest'),plan.get('transport_id')}
    labels = info.get('Config',{}).get('Labels') or {}
    if info.get('Id') not in accepted:
        raise FleetError('Identifiant image différent du digest vérifié.')
    if (labels.get('org.opencontainers.image.revision') or labels.get('org.caribou.source-revision'))!=plan['commit']:
        raise FleetError('Image provenant d’un autre commit.')
    if info.get('Os')!='linux' or info.get('Architecture')!=plan['platform'].split('/')[1]:
        raise FleetError('Architecture image incorrecte.')


def pull_local(plan,registry,runtime):
    info = image_info(plan['reference'])
    if info:
        validate_image(info,plan)
        return
    with tempfile.TemporaryDirectory(prefix='caribou-ghcr-') as temporary:
        directory = Path(temporary)
        if os.name=='nt':
            # Windows chmod alone does not restrict NTFS ACLs.
            sid = local(['whoami','/user','/fo','csv','/nh']).split(',')[-1].strip('"')
            if not re.fullmatch(r'S-1-[0-9-]+',sid): raise FleetError('SID Windows introuvable pour protéger les credentials temporaires.')
            local(['icacls',temporary,'/inheritance:r','/grant:r',f'*{sid}:(OI)(CI)F'])
        else:
            directory.chmod(0o700)
        args = ['docker','--config',temporary]
        try:
            result = subprocess.run(args+['login','ghcr.io','--username',registry.username,'--password-stdin'],
                                    input=registry.password+'\n',text=True,capture_output=True,timeout=60)
            if result.returncode: raise FleetError('Connexion Docker GHCR refusée (HTTPS/PAT/droits du package).')
            local(args+['pull','--platform',plan['platform'],plan['reference']],timeout=1200)
        finally:
            # No persistent credential, including when login/pull fails.
            for file in directory.iterdir():
                if file.is_file(): file.unlink()
    validate_image(image_info(plan['reference']) or {},plan)


def prepare_registry_node(node,inspection,plan,registry,runtime):
    with SSH(node,runtime) as ssh:
        status,text = ssh.run('docker image inspect '+shlex.quote(plan['reference']),sudo=True,check=False)
        if status==0:
            validate_image(json.loads(text)[0],plan)
            return {'cached':True,'image':plan['reference'],'platform':plan['platform']}
        directory = inspection['home']+'/cariboutv-lab/tools'
        ssh.run('mkdir -p '+shlex.quote(directory))
        script = directory+'/remote-pull.sh'
        ssh.put(HERE/'remote-pull.sh',script)
        arguments = [plan['reference'],plan['platform'],plan['config_digest'],plan['digest'],plan['commit']]
        ssh.run('sh '+shlex.quote(script)+' '+' '.join(map(shlex.quote,arguments)),sudo=True,timeout=1200,
                input_data=registry.username+'\n'+registry.password+'\n')
        text = ssh.run('docker image inspect '+shlex.quote(plan['reference']),sudo=True)
        validate_image(json.loads(text)[0],plan)
        return {'cached':False,'image':plan['reference'],'platform':plan['platform']}


def pc_environment(inventory, address):
    result = os.environ.copy()
    result.pop('CARIBOU_GHCR_TOKEN',None)
    a = next((n for n in inventory['nodes'] if n['zone']=='A'),inventory['nodes'][0])
    b = next((n for n in inventory['nodes'] if n['zone']=='B'),inventory['nodes'][-1])
    result.update(LAB_BIND_IP=address, WEB_BIND_IP=address, MQTT_PORT=str(inventory['mqtt_port']),
                  CENTRAL_PORT=str(inventory['central_port']),WEB_PORT=str(inventory['web_port']),
                  EDGE_A_ID=a['id'],EDGE_B_ID=b['id'],ZONE_A_ID='A',ZONE_B_ID='B',
                  COMPOSE_PROFILES='',EDGE_MODE='real-http')
    return result


def compose_args():
    return ['docker','compose','-f',str(ROOT/'deploy/integration/compose.yaml'),
            '-f',str(ROOT/'deploy/integration/compose.external.yaml')]


def install_remote_compose(ssh,base,compose_tool):
    """Installer et exécuter Compose dans le dossier dédié, sans changer Docker global."""
    binary,checksum = compose_tool
    incoming = base+'/incoming'
    destination = base+'/tools/docker-compose'
    log(f"[{ssh.node['id']}] Installation automatique de Compose dans {destination}...")
    ssh.put(binary,incoming+'/docker-compose')
    ssh.run('printf '+shlex.quote('%s  %s\n')+' '+shlex.quote(checksum)+' '+shlex.quote(incoming+'/docker-compose')+' | sha256sum -c -')
    ssh.run('mkdir -p '+shlex.quote(base+'/tools')+' && install -m 755 '+shlex.quote(incoming+'/docker-compose')+' '+shlex.quote(destination),sudo=True)
    version = ssh.run(shlex.quote(destination)+' version --short',sudo=True)
    compose_version(version,minimum=(2,20,0))
    log(f"[{ssh.node['id']}] Compose installé et vérifié : {version} ({destination}).")
    return version


def firewall(inventory, address):
    if os.name != 'nt': raise FleetError('-ConfigureFirewall concerne uniquement le PC Windows.')
    peers = ','.join("'"+n['host']+"'" for n in inventory['nodes'])
    script = f"""$ErrorActionPreference='Stop'
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if(-not $admin){{throw 'Relancer ce programme depuis PowerShell administrateur pour configurer le pare-feu.'}}
$rule=Get-NetFirewallRule -DisplayName 'Caribou Fleet MQTT' -ErrorAction SilentlyContinue
if($rule){{ $rule | Set-NetFirewallRule -Enabled True -Direction Inbound -Action Allow -Profile Any; $rule | Get-NetFirewallPortFilter | Set-NetFirewallPortFilter -Protocol TCP -LocalPort {inventory['mqtt_port']}; $rule | Get-NetFirewallAddressFilter | Set-NetFirewallAddressFilter -LocalAddress '{address}' -RemoteAddress @({peers}) }}
else{{New-NetFirewallRule -DisplayName 'Caribou Fleet MQTT' -Direction Inbound -Action Allow -Protocol TCP -LocalAddress '{address}' -LocalPort {inventory['mqtt_port']} -RemoteAddress @({peers}) -Profile Any | Out-Null}}
"""
    local(['powershell.exe','-NoProfile','-Command',script],stream=True)


def deploy_node(node, inspection, inventory, address, commit, archive, image, image_id, image_archive, runtime, compose_tool=None, image_plan=None, force_recreate=False):
    base = inspection['home']+'/cariboutv-lab'
    incoming = base+'/incoming'
    private = ROOT/'deploy/integration/.runtime'
    staging = runtime/'payloads'/node['id']
    staging.mkdir(parents=True,exist_ok=True)
    environment = f"CONTROLLERS_IMAGE={image}\nEDGE_ID={node['id']}\nLAB_PC_ADDRESS={address}\nMQTT_PORT={inventory['mqtt_port']}\nEDGE_BIND_IP={node['host']}\nEDGE_HTTP_PORT={node['http_port']}\n"
    (staging/'edge.env').write_text(environment,encoding='ascii')
    write_json(staging/'manifest.json',{'commit':commit,'edge_id':node['id'],'quota_bytes':node['quota_bytes'],
                                       'image':image,'image_id':image_id,'coordinator':address, 'verified_image':image_plan})
    digest = file_hash(archive)
    log(f"[{node['id']}] Déploiement vers {base}...")
    with SSH(node,runtime) as ssh:
        # Tester depuis chaque Edge : une connexion PC->Edge ne prouve pas le retour MQTT.
        ssh.run('timeout 6 bash -c '+shlex.quote(f'exec 3<>/dev/tcp/{address}/{inventory["mqtt_port"]}'))
        ssh.run('mkdir -p '+shlex.quote(incoming+'/identity')+' && chmod 700 '+shlex.quote(incoming)+' '+shlex.quote(incoming+'/identity'))
        if compose_tool:
            install_remote_compose(ssh,base,compose_tool)
        ssh.put(archive,incoming+'/source.tar.gz')
        # Ne pas retransférer une image déjà chargée et vérifiée.
        status, loaded = ssh.run('docker image inspect --format '+shlex.quote('{{.Id}}')+' '+shlex.quote(image),sudo=True,check=False)
        if image_archive is not None and (status or loaded != image_id): ssh.put(image_archive,incoming+'/image.tar')
        ssh.put(staging/'edge.env',incoming+'/edge.env')
        ssh.put(staging/'manifest.json',incoming+'/manifest.json')
        ssh.put(private/'config.json',incoming+'/config.json')
        for secret in (private/'identities'/f"edge-{node['id']}").iterdir():
            if secret.is_file(): ssh.put(secret,incoming+'/identity/'+secret.name)
        ssh.put(HERE/'remote-deploy.sh',incoming+'/remote-deploy.sh')
        args = [base,commit,digest,image,image_id,node['id'],str(node['quota_bytes']),
                (image_plan or {}).get('transport_id') or (image_plan or {}).get('digest') or image_id,inspection.get('architecture',''),
                'registry' if image_archive is None else 'local','1' if force_recreate else '0']
        ssh.run('bash '+shlex.quote(incoming+'/remote-deploy.sh')+' '+' '.join(map(shlex.quote,args)),
                sudo=True,timeout=600,stream=True)
    return {'commit':commit,'directory':base+'/repo','image':image,'endpoint':inspection['endpoint']}


def http_json(url, token=None, timeout=10):
    request = urllib.request.Request(url,headers={'Authorization':'Bearer '+token} if token else {})
    # Les réseaux privés ne passent pas par le proxy HTTP éventuel du PC.
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=timeout) as response:
        return json.load(response)


def validate_nodes(snapshot, inventory, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    found = {}
    for spec in inventory['nodes']:
        registered = snapshot.get('nodes',{}).get(spec['id'])
        if not registered: raise FleetError(f"{spec['id']} absent du catalogue Central.")
        node = registered['node']
        expected = f"http://{spec['host']}:{spec['http_port']}"
        if registered['endpoint'] != expected or node['zone_id'] != spec['zone']:
            raise FleetError(f"{spec['id']} : endpoint/zone incorrect.")
        timestamp = re.sub(r'(\.\d{6})\d+',r'\1',node['heartbeat_at']).replace('Z','+00:00')
        age = (now-dt.datetime.fromisoformat(timestamp)).total_seconds()
        if not -2 <= age <= 30 or node['state'] != 'AVAILABLE':
            raise FleetError(f"{spec['id']} : heartbeat périmé ou état {node['state']} (âge {age:.1f}s).")
        found[spec['id']] = {'state':node['state'],'heartbeat_age_seconds':round(age,2),
                             'available_bytes':node['available_for_project_bytes'],'endpoint':expected}
    return found


def tests(inventory, address, runtime, jobs, report, health_only=False):
    env = pc_environment(inventory,address)
    env.update(read_json(runtime/'settings.json',{}).get('central_images',{}))
    log('Vérification HTTP et Docker sur chaque Edge...')
    def health(node):
        base = f"http://{node['host']}:{node['http_port']}"
        http_json(base+'/ready')
        with SSH(node,runtime) as ssh:
            home = ssh.run('printf %s "$HOME"')
            directory = home+'/cariboutv-lab/deployment'
            tool = shlex.quote(home+'/cariboutv-lab/tools/docker-compose')
            # Fonction pour transmettre les mêmes arguments au binaire dédié ou global.
            command = f'compose() {{ if [ -x {tool} ]; then {tool} "$@"; else docker compose "$@"; fi; }}; '
            text = ssh.run(command+'cd '+shlex.quote(directory)+' && compose -p caribou-fleet -f compose.edge.yaml ps --format json',sudo=True)
            rows = parse_ps(text)
            if {r.get('Service') for r in rows} != {'edge-agent','edge-bridge'}:
                raise FleetError('Les deux conteneurs attendus ne sont pas présents.')
            if any(r.get('State') != 'running' or r.get('Health') != 'healthy' for r in rows):
                raise FleetError('Un conteneur est arrêté ou non healthy.')
            manifest = json.loads(ssh.run('cat '+shlex.quote(directory+'/manifest.json'),sudo=True))
            if manifest['edge_id'] != node['id']: raise FleetError('Mauvaise identité déployée.')
            expected = read_json(runtime/'last-deploy.json',{}).get('commit')
            if expected and manifest['commit'] != expected: raise FleetError('Version distante différente du dernier déploiement demandé.')
            plan = manifest.get('verified_image')
            if plan:
                info = json.loads(ssh.run('docker image inspect '+shlex.quote(manifest['image']),sudo=True))[0]
                validate_image(info,plan)
                for row in rows:
                    container = row.get('ID') or row.get('Name') or row.get('Names')
                    if not container: raise FleetError('Identifiant de conteneur absent du contrôle de santé.')
                    running = json.loads(ssh.run('docker inspect '+shlex.quote(container),sudo=True))[0]
                    if running.get('Image') not in (plan['config_digest'],plan.get('transport_id'),plan.get('digest')):
                        raise FleetError('Un conteneur utilise une autre image que celle vérifiée.')
        return {'ready':True,'commit':manifest['commit'],'services':sorted(r['Service'] for r in rows)}
    parallel(inventory['nodes'],jobs,health,report.setdefault('edge_health',[]))
    http_json(f"http://{address}:{inventory['web_port']}/ready")
    token_path = ROOT/'deploy/integration/.runtime/operator/operator.token'
    token = token_path.read_text(encoding='utf-8').strip()
    deadline = time.monotonic()+60
    while True:
        try:
            snapshot = http_json(f"http://{address}:{inventory['central_port']}/api/v1/snapshot",token)
            report['registration'] = validate_nodes(snapshot,inventory)
            break
        except (FleetError,urllib.error.URLError,KeyError) as error:
            if time.monotonic() > deadline: raise FleetError(f'Enregistrement MQTT des nœuds : {error}') from error
            time.sleep(2)
    log(f"PASS web/auth/Central et {len(inventory['nodes'])} identités MQTT fraîches.")
    if health_only:
        report['video'] = {'status':'SKIPPED','reason':'-HealthOnly demandé ; aucun média créé.'}
        return
    log('Test vidéo réel : création d’une courte vidéo PRIVÉE conservée dans le stockage de test.')
    args = compose_args()
    local(args+['cp',str(HERE/'test-video.py'),'web:/tmp/fleet-test-video.py'],env=env)
    output = local(args+['exec','-T','web','python3','/tmp/fleet-test-video.py'],env=env,timeout=360)
    for line in output.splitlines():
        if line.startswith('FLEET_RESULT '): report['video'] = json.loads(line.removeprefix('FLEET_RESULT '))
        else: log(line)
    if 'video' not in report: raise FleetError('Le test vidéo n’a pas produit de résultat vérifiable.')


def parse_ps(text):
    try:
        value = json.loads(text)
        return value if isinstance(value,list) else [value]
    except json.JSONDecodeError:
        return [json.loads(line) for line in text.splitlines() if line.strip()]


def main(argv=None):
    global ROOT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['check','deploy','test'])
    parser.add_argument('--repository',type=Path,help='Dépôt Git local ; par défaut projet_secu_cloud à côté de ce dossier.')
    parser.add_argument('--inventory',type=Path,default=HERE/'inventory.local.json')
    parser.add_argument('--state-directory',type=Path,help='Dossier externe des accès et rapports.')
    parser.add_argument('--image-source',choices=['registry','local'],default='registry')
    parser.add_argument('--registry-repository',default='julesdavidou/projet_secu_cloud')
    parser.add_argument('--registry-user')
    parser.add_argument('--revision',help='SHA Git complet ; défaut origin/main.')
    parser.add_argument('--force-recreate',action='store_true',help='Recréer les conteneurs sélectionnés, même si le digest est inchangé.')
    parser.add_argument('--nodes',help='Identifiants séparés par des virgules ; tests : dernière sélection déployée par défaut.')
    parser.add_argument('--coordinator-address')
    parser.add_argument('--configure-firewall',action='store_true')
    parser.add_argument('--skip-fetch',action='store_true')
    parser.add_argument('--skip-build',action='store_true')
    parser.add_argument('--health-only',action='store_true')
    parser.add_argument('--jobs',type=int,choices=range(1,6),default=2)
    options = parser.parse_args(argv)
    state = options.state_directory or HERE
    if options.state_directory and options.inventory == HERE/'inventory.local.json': options.inventory = state/'inventory.local.json'
    runtime = state/'.runtime'
    runtime.mkdir(parents=True,exist_ok=True)
    report = {'action':options.action,'started_at':dt.datetime.now(dt.timezone.utc).isoformat(),'ok':False}
    operation_lock = None
    try:
        operation_lock = acquire_lock(runtime)
        if options.repository:
            ROOT = options.repository.expanduser().resolve()
        if options.action != 'check' and not (ROOT / '.git').exists():
            raise FleetError(f'Dépôt Git introuvable : {ROOT}. Fournir -Repository CHEMIN_DU_DEPOT.')
        inventory = load_inventory(options.inventory)
        settings = read_json(runtime/'settings.json',{})
        selection = options.nodes
        if selection is None and options.action == 'test':
            selection = settings.get('active_nodes')
            if selection is None:
                selection = read_json(runtime/'last-deploy.json',{}).get('nodes')
        all_ids = [node['id'] for node in inventory['nodes']]
        inventory = select_nodes(inventory,selection)
        report['selected_nodes'] = [node['id'] for node in inventory['nodes']]
        report['excluded_nodes'] = [node_id for node_id in all_ids if node_id not in report['selected_nodes']]
        log('Nœuds sélectionnés : '+', '.join(report['selected_nodes']))
        if report['excluded_nodes']:
            log('Nœuds exclus (aucun accès SSH) : '+', '.join(report['excluded_nodes']))
        address = options.coordinator_address or settings.get('coordinator_address')
        if options.action != 'check' or address:
            if not address: raise FleetError('Premier lancement : fournir -CoordinatorAddress IP_LAN_DU_PC.')
            ip = ipaddress.IPv4Address(address)
            if ip.is_loopback or ip.is_unspecified or ip.is_multicast: raise FleetError('Une IPv4 LAN joignable depuis les hôtes sélectionnés est requise.')
        if options.action == 'test':
            tests(inventory,address,runtime,options.jobs,report,options.health_only)
        else:
            log('Contrôle SSH/sudo/Docker/architecture/horloges ; aucun conteneur distant modifié...')
            inspections = parallel(inventory['nodes'],options.jobs,lambda n:inspect_node(n,runtime),report.setdefault('preflight',[]))
            if options.action == 'deploy':
                compose_version(local(['docker','compose','version','--short']))
                if local(['docker','info','--format','{{.OSType}}']) != 'linux': raise FleetError('Docker Desktop doit utiliser les conteneurs Linux.')
                if options.configure_firewall: firewall(inventory,address)
                commit,archive,source = prepare_source(runtime,options.skip_fetch,options.revision)
                report['commit'] = commit
                log(f'Déploiement du commit sélectionné : {commit}')
                topo = topology(inventory)
                prior = read_json(ROOT/'deploy/integration/.runtime/config.json',{})
                for old in prior.get('nodes',[]):
                    matching = next((n for n in topo['nodes'] if n['id']==old['id']),None)
                    if matching and matching['quota_bytes'] != old['quota_bytes']:
                        raise FleetError(f"Quota existant modifié pour {old['id']} : réconciliation requise.")
                topo_path = runtime/'topology.json'
                write_json(topo_path,topo)
                images, compose_tools, plans = {}, {}, {}
                central_images = {}
                if options.image_source == 'registry':
                    username = options.registry_user or os.environ.get('CARIBOU_GHCR_USERNAME') or input('Compte GitHub pour GHCR : ').strip()
                    password = os.environ.get('CARIBOU_GHCR_TOKEN') or getpass.getpass('PAT classic GHCR (read:packages, saisie masquée) : ')
                    SENSITIVE.append(password)
                    registry = Registry(username,password)
                    for component in ('control','auth','web'):
                        plans[component] = registry.resolve(options.registry_repository,component,commit,'linux/amd64')
                    for arch in sorted({v['architecture'] for v in inspections.values()}):
                        plans[arch] = registry.resolve(options.registry_repository,'control',commit,'linux/'+arch)
                    report['images'] = plans
                    # Resolve every image before changing any running service.
                    for component in ('control','auth','web'):
                        pull_local(plans[component],registry,runtime)
                        central_images[component.upper()+'_IMAGE'] = plans[component]['reference']
                    def stage_pull(node):
                        plan = plans[inspections[node['id']]['architecture']]
                        return prepare_registry_node(node,inspections[node['id']],plan,registry,runtime)
                    parallel(inventory['nodes'],options.jobs,stage_pull,report.setdefault('registry_preflight',[]))
                    for arch in {v['architecture'] for v in inspections.values()}:
                        plan = plans[arch]
                        images[arch] = plan['reference'],plan['config_digest'],None
                else:
                    if options.skip_build: log('-SkipBuild : utilisation exclusive des images locales du commit demandé.')
                    for tag,file,target in [('caribou/integration-control:dev','deploy/lab/Dockerfile','runtime'),
                                             ('caribou/integration-auth:dev','deploy/integration/Dockerfile.auth',None),
                                             ('caribou/integration-web:dev','deploy/integration/Dockerfile.web',None)]:
                        build_image(source,commit,tag,file,target=target,skip=options.skip_build)
                    for arch in sorted({v['architecture'] for v in inspections.values()}):
                        tag = f'caribou/fleet-control:{commit[:12]}-{arch.replace("/","-")}'
                        transport_id = build_image(source,commit,tag,'deploy/lab/Dockerfile',arch=arch,target='runtime',skip=options.skip_build)
                        image_archive = runtime/f"control-{arch.replace('/','-')}-{transport_id.split(':')[1][:16]}.tar"
                        if not image_archive.exists():
                            pending_image = image_archive.with_suffix('.tmp')
                            local(['docker','image','save','-o',str(pending_image),tag],stream=True)
                            os.replace(pending_image,image_archive)
                        config_digest = archive_config_digest(image_archive,tag,commit,arch)
                        plans[arch] = {'reference':tag,'config_digest':config_digest,'transport_id':transport_id,
                                       'platform':'linux/'+arch,'commit':commit}
                        images[arch] = tag,config_digest,image_archive
                    report['images'] = plans
                for arch in {v['architecture'] for v in inspections.values()}:
                    if any(v['architecture']==arch and v['install_compose'] for v in inspections.values()):
                        compose_tools[arch] = compose_binary(arch,runtime)
                env = pc_environment(inventory,address)
                env.update(central_images)
                env['CARIBOU_FORCE_RECREATE'] = '1' if options.force_recreate else '0'
                if options.image_source == 'local':
                    env.update(CONTROL_IMAGE='caribou/integration-control:dev',AUTH_IMAGE='caribou/integration-auth:dev',WEB_IMAGE='caribou/integration-web:dev')
                write_json(runtime/'settings.json',{**settings,'coordinator_address':address,
                                                   'active_nodes':report['selected_nodes'],'central_images':central_images})
                write_json(runtime/'last-deploy.json',{'commit':commit,'nodes':report['selected_nodes'],
                                                     'image_source':options.image_source,'images':plans})
                report['central_update_started'] = True
                local(['powershell.exe','-NoProfile','-ExecutionPolicy','Bypass','-File',
                       str(ROOT/'deploy/integration/start.ps1'),'-SkipBuild','-ExternalEdges','-Topology',str(topo_path)],env=env,stream=True)
                def update(node):
                    image,image_id,image_archive = images[inspections[node['id']]['architecture']]
                    tool = compose_tools.get(inspections[node['id']]['architecture']) if inspections[node['id']]['install_compose'] else None
                    return deploy_node(node,inspections[node['id']],inventory,address,commit,archive,image,image_id,image_archive,runtime,tool,image_plan=plans[inspections[node['id']]['architecture']],force_recreate=options.force_recreate)
                parallel(inventory['nodes'],options.jobs,update,report.setdefault('deployment',[]))
                tests(inventory,address,runtime,options.jobs,report,health_only=True)
                log(f"Site : http://{address}:{inventory['web_port']}/integration/")
        report['ok'] = True
        log(f'PASS terminé. Rapports : {runtime / "reports"}')
        return 0
    except KeyboardInterrupt:
        report['error'] = 'Interrompu par l’utilisateur ; aucun volume supprimé.'
        log(report['error']); return 130
    except Exception as error:
        message = str(error)
        for secret in SENSITIVE: message = message.replace(secret,'[masqué]')
        for node in locals().get('inventory',{}).get('nodes',[]): message = message.replace(node['password'],'[masqué]')
        report['error'] = message
        log('FAIL '+message)
        if options.action == 'deploy' and 'deployment' not in report:
            log('Aucun conteneur Edge n’a été mis à jour. Des images/fichiers préparatoires peuvent avoir été copiés ; consulter les étapes du rapport.')
        return 1
    finally:
        stamp = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
        write_json(runtime/'reports'/f'{options.action}-{stamp}.json',report)
        if operation_lock: operation_lock.close()


if __name__ == '__main__':
    raise SystemExit(main())
