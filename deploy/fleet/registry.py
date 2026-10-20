"""GHCR read-only resolution. No Docker credentials are written by this module."""
from __future__ import annotations

import base64
import hashlib
import json
import re
import urllib.error
import urllib.parse
import urllib.request


class RegistryError(RuntimeError):
    pass


def digest_bytes(data):
    return 'sha256:'+hashlib.sha256(data).hexdigest()


def require_digest(value):
    if not isinstance(value,str) or not re.fullmatch(r'sha256:[0-9a-f]{64}',value):
        raise RegistryError('Digest SHA256 invalide.')
    return value


def image_name(repository, component):
    if not re.fullmatch(r'[a-z0-9][a-z0-9_.-]*/[a-z0-9][a-z0-9_.-]*',repository):
        raise RegistryError('Repository attendu sous la forme owner/repository en minuscules.')
    if component not in ('control','auth','web'):
        raise RegistryError('Composant inconnu.')
    return f'{repository}-{component}'


class Registry:
    def __init__(self, username, password):
        if not re.fullmatch(r'[a-zA-Z0-9-]+',username or '') or not password or any(c in password for c in '\r\n'):
            raise RegistryError('Identifiant GHCR et PAT classic read:packages requis.')
        self.username, self.password = username,password
        self.tokens = {}

    def _request(self,url,headers):
        try:
            with urllib.request.urlopen(urllib.request.Request(url,headers=headers),timeout=45) as response:
                data = response.read(16*1024*1024+1)
                if len(data)>16*1024*1024: raise RegistryError('Métadonnées GHCR trop volumineuses.')
                return data,response.headers
        except urllib.error.HTTPError as error:
            reasons = {401:'Authentification GHCR refusée : vérifier le PAT classic read:packages et sa date d’expiration.',
                       403:'Accès GHCR interdit : vérifier les droits du package et le SSO.',
                       404:'Image/commit absent de GHCR (ou package privé inaccessible) : attendre la CI ou choisir -ImageSource local.'}
            raise RegistryError(reasons.get(error.code,f'GHCR répond HTTP {error.code}.')) from None
        except (urllib.error.URLError,TimeoutError,OSError):
            raise RegistryError('GHCR inaccessible (DNS/HTTPS/proxy). Vérifier le réseau ou utiliser -ImageSource local.') from None

    def _token(self,name):
        if name not in self.tokens:
            query = urllib.parse.urlencode({'service':'ghcr.io','scope':f'repository:{name}:pull'})
            basic = base64.b64encode(f'{self.username}:{self.password}'.encode()).decode()
            data,_ = self._request('https://ghcr.io/token?'+query,{'Authorization':'Basic '+basic})
            token = json.loads(data).get('token')
            if not token: raise RegistryError('GHCR n’a pas fourni de jeton de lecture.')
            self.tokens[name] = token
        return self.tokens[name]

    def _object(self,name,kind,reference,expected=None):
        headers = {'Authorization':'Bearer '+self._token(name),
                   'Accept':','.join(['application/vnd.oci.image.index.v1+json','application/vnd.oci.image.manifest.v1+json',
                                      'application/vnd.docker.distribution.manifest.list.v2+json','application/vnd.docker.distribution.manifest.v2+json'])}
        data,headers = self._request(f'https://ghcr.io/v2/{name}/{kind}/{reference}',headers)
        actual = digest_bytes(data)
        advertised = headers.get('Docker-Content-Digest')
        if (expected and require_digest(expected)!=actual) or (advertised and require_digest(advertised)!=actual):
            raise RegistryError('Le contenu GHCR ne correspond pas au digest annoncé.')
        return json.loads(data),actual

    def resolve(self,repository,component,commit,platform):
        if not re.fullmatch('[0-9a-f]{40}',commit): raise RegistryError('Commit Git complet requis.')
        if platform not in ('linux/amd64','linux/arm64','linux/arm/v7'): raise RegistryError('Plateforme non prise en charge.')
        name = image_name(repository,component)
        manifest,index_digest = self._object(name,'manifests','sha-'+commit)
        parts = platform.split('/')
        arch,variant = parts[1],parts[2] if len(parts)>2 else ''
        manifest_digest = index_digest
        if 'manifests' in manifest:
            matches = [item for item in manifest['manifests'] if item.get('platform',{}).get('os')=='linux'
                       and item['platform'].get('architecture')==arch
                       and (not variant or item['platform'].get('variant')==variant)]
            if len(matches)!=1: raise RegistryError(f'{component} : plateforme {platform} absente/ambiguë dans GHCR.')
            manifest_digest = require_digest(matches[0]['digest'])
            manifest,_ = self._object(name,'manifests',manifest_digest,manifest_digest)
        config_digest = require_digest(manifest['config']['digest'])
        config,_ = self._object(name,'blobs',config_digest,config_digest)
        labels = config.get('config',{}).get('Labels') or {}
        if labels.get('org.opencontainers.image.revision')!=commit:
            raise RegistryError(f'{component} : révision OCI différente du commit demandé.')
        if config.get('os')!='linux' or config.get('architecture')!=arch or (variant and config.get('variant')!=variant):
            raise RegistryError(f'{component} : configuration incompatible avec {platform}.')
        return {'reference':f'ghcr.io/{name}@{manifest_digest}','digest':manifest_digest,
                'index_digest':index_digest,'config_digest':config_digest,'platform':platform,'commit':commit}
