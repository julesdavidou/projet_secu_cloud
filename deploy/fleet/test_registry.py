import copy
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import urllib.error

import fleet
from registry import Registry, RegistryError, digest_bytes, image_name, require_digest


class RegistryTests(unittest.TestCase):
    def setUp(self):
        self.commit='a'*40
        self.registry=Registry('test-user','private-test-token')
        self.config={'os':'linux','architecture':'arm','variant':'v7',
                     'config':{'Labels':{'org.opencontainers.image.revision':self.commit}}}
        self.config_data=json.dumps(self.config).encode()
        self.config_digest=digest_bytes(self.config_data)
        self.manifest={'schemaVersion':2,'config':{'digest':self.config_digest}}
        self.manifest_data=json.dumps(self.manifest).encode()
        self.manifest_digest=digest_bytes(self.manifest_data)
        self.index={'schemaVersion':2,'manifests':[{'digest':self.manifest_digest,
                    'platform':{'os':'linux','architecture':'arm','variant':'v7'}}]}
        self.index_data=json.dumps(self.index).encode()

    def resolver(self,url,headers):
        if '/token?' in url: return b'{"token":"bearer-test"}',{}
        if '/blobs/' in url: return self.config_data,{}
        if url.endswith(self.manifest_digest): return self.manifest_data,{}
        return self.index_data,{'Docker-Content-Digest':digest_bytes(self.index_data)}

    def plan(self):
        with patch.object(self.registry,'_request',side_effect=self.resolver):
            return self.registry.resolve('julesdavidou/projet_secu_cloud','control',self.commit,'linux/arm/v7')

    def test_names_platform_and_pinned_child_digest(self):
        plan=self.plan()
        self.assertEqual(plan['reference'],'ghcr.io/julesdavidou/projet_secu_cloud-control@'+self.manifest_digest)
        self.assertEqual(plan['config_digest'],self.config_digest)
        self.assertEqual(plan['platform'],'linux/arm/v7')
        self.assertNotIn('private-test-token',json.dumps(plan))
        self.assertEqual(image_name('owner/project','web'),'owner/project-web')

    def test_secure_portal_capability_is_required_only_for_client_bundles(self):
        with patch.object(self.registry,'_request',side_effect=self.resolver),self.assertRaisesRegex(RegistryError,'portail'):
            self.registry.resolve('owner/project','control',self.commit,'linux/arm/v7',require_client_portal=True)
        self.plan()  # Existing Fleet legacy deployments stay compatible.

    def test_unsupported_platform_and_missing_platform(self):
        for platform in ('linux/arm64','linux/mips'):
            with self.subTest(platform=platform),patch.object(self.registry,'_request',side_effect=self.resolver),self.assertRaises(RegistryError):
                self.registry.resolve('owner/project','control',self.commit,platform)

    def test_manifest_or_blob_tampering_is_rejected(self):
        for corrupt in ('manifest','config'):
            def request(url,headers):
                data,metadata=self.resolver(url,headers)
                if (corrupt=='manifest' and url.endswith(self.manifest_digest)) or (corrupt=='config' and '/blobs/' in url):
                    return data+b' ',metadata
                return data,metadata
            with self.subTest(corrupt=corrupt),patch.object(self.registry,'_request',side_effect=request),self.assertRaisesRegex(RegistryError,'digest'):
                self.registry.resolve('owner/project','control',self.commit,'linux/arm/v7')

    def test_changed_revision_is_rejected_even_if_all_hashes_match(self):
        original=copy.deepcopy(self.config)
        self.config['config']['Labels']['org.opencontainers.image.revision']='b'*40
        # Recompute the entire graph; integrity alone is not commit verification.
        self.config_data=json.dumps(self.config).encode()
        self.manifest['config']['digest']=digest_bytes(self.config_data)
        self.manifest_data=json.dumps(self.manifest).encode()
        self.manifest_digest=digest_bytes(self.manifest_data)
        self.index['manifests'][0]['digest']=self.manifest_digest
        self.index_data=json.dumps(self.index).encode()
        with patch.object(self.registry,'_request',side_effect=self.resolver),self.assertRaisesRegex(RegistryError,'révision'):
            self.registry.resolve('owner/project','control',self.commit,'linux/arm/v7')

    def test_missing_image_and_authentication_failures_are_actionable_and_redacted(self):
        for status,message in ((401,'Authentification'),(403,'interdit'),(404,'absent')):
            error=urllib.error.HTTPError('https://ghcr.io/test',status,'private-test-token',{},None)
            with self.subTest(status=status),patch('urllib.request.urlopen',side_effect=error),self.assertRaisesRegex(RegistryError,message) as caught:
                self.registry._request('https://ghcr.io/test',{})
            self.assertNotIn('private-test-token',str(caught.exception))

    def test_dns_failure_recommends_local_mode(self):
        with patch('urllib.request.urlopen',side_effect=urllib.error.URLError('DNS')),self.assertRaisesRegex(RegistryError,'ImageSource local'):
            self.registry._request('https://ghcr.io/test',{})

    def test_invalid_digest_is_rejected(self):
        for value in ('latest','sha256:abc','sha256:'+'G'*64):
            with self.subTest(value=value),self.assertRaises(RegistryError): require_digest(value)

    def test_export_config_digest_differs_from_containerd_index_and_is_used(self):
        with tempfile.TemporaryDirectory() as directory:
            archive=Path(directory)/'image.tar'
            config=copy.deepcopy(self.config)
            config['config']['Labels']['org.caribou.source-revision']=self.commit
            data=json.dumps(config).encode()
            with tarfile.open(archive,'w') as tar:
                for name,content in [('manifest.json',json.dumps([{'RepoTags':['test:image'],'Config':'blobs/config.json'}]).encode()),('blobs/config.json',data)]:
                    member=tarfile.TarInfo(name); member.size=len(content); tar.addfile(member,io.BytesIO(content))
            self.assertEqual(fleet.archive_config_digest(archive,'test:image',self.commit,'arm/v7'),digest_bytes(data))
            with self.assertRaises(fleet.FleetError): fleet.archive_config_digest(archive,'test:image','b'*40,'arm/v7')

    def test_loaded_image_accepts_legacy_config_or_containerd_manifest_and_checks_revision(self):
        plan=self.plan()
        info={'Id':plan['config_digest'],'Os':'linux','Architecture':'arm',
              'Config':{'Labels':{'org.opencontainers.image.revision':self.commit}}}
        fleet.validate_image(info,plan)
        info['Id']=plan['digest']; fleet.validate_image(info,plan)
        info['Config']['Labels']['org.opencontainers.image.revision']='b'*40
        with self.assertRaises(fleet.FleetError): fleet.validate_image(info,plan)

    def test_private_edge_pull_failure_preserves_services_and_sends_token_only_over_stdin(self):
        plan=self.plan(); calls=[]; uploads=[]
        class FakeSSH:
            def __init__(self,*args): pass
            def __enter__(self): return self
            def __exit__(self,*args): pass
            def put(self,*args): uploads.append(args)
            def run(self,command,**kwargs):
                calls.append((command,kwargs))
                if kwargs.get('check') is False: return 1,''
                if kwargs.get('input_data'): raise fleet.FleetError('GHCR HTTPS inaccessible')
                return ''
        with patch.object(fleet,'SSH',FakeSSH),self.assertRaisesRegex(fleet.FleetError,'HTTPS'):
            fleet.prepare_registry_node({'id':'camera'},{'home':'/home/camera'},plan,self.registry,Path('.'))
        self.assertTrue(calls[-1][1]['input_data'].endswith('private-test-token\n'))
        self.assertFalse(any('private-test-token' in command for command,_ in calls))
        self.assertFalse(any('compose' in command or 'docker load' in command for command,_ in calls))
        self.assertTrue(all(str(source).endswith('remote-pull.sh') for source,_ in uploads))

    def test_cached_edge_image_needs_no_credentials_or_transfer(self):
        plan=self.plan()
        info={'Id':plan['config_digest'],'Os':'linux','Architecture':'arm',
              'Config':{'Labels':{'org.opencontainers.image.revision':self.commit}}}
        class FakeSSH:
            def __init__(self,*args): pass
            def __enter__(self): return self
            def __exit__(self,*args): pass
            def run(self,*args,**kwargs): return 0,json.dumps([info])
            def put(self,*args): raise AssertionError('cached image must not transfer files')
        with patch.object(fleet,'SSH',FakeSSH):
            result=fleet.prepare_registry_node({'id':'camera'},{'home':'/home/camera'},plan,self.registry,Path('.'))
        self.assertTrue(result['cached'])

    def test_pc_login_failure_cleans_temporary_credentials_and_hides_token_in_argv(self):
        from types import SimpleNamespace
        directories=[]
        def login(args,**kwargs):
            directory=Path(args[2]); directories.append(directory)
            (directory/'config.json').write_text('private-test-token')
            self.assertNotIn('private-test-token',' '.join(args))
            self.assertEqual(kwargs['input'],'private-test-token\n')
            return SimpleNamespace(returncode=1)
        with patch.object(fleet,'image_info',return_value=None),\
             patch.object(fleet,'local',return_value='"test-user","S-1-5-21-123"'),\
             patch.object(fleet.subprocess,'run',side_effect=login),self.assertRaisesRegex(fleet.FleetError,'refusée'):
            fleet.pull_local(self.plan(),self.registry,Path('.'))
        self.assertTrue(directories)
        self.assertFalse(directories[0].exists())

    def test_registry_deployment_never_builds_saves_or_transfers_an_image_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            here=Path(directory); repo=here/'repo'; (repo/'.git').mkdir(parents=True)
            inv=json.loads((fleet.HERE/'inventory.example.json').read_text())
            for node in inv['nodes']: node['password']='fake'
            fleet.write_json(here/'inventory.json',inv)
            def resolve(repository,component,commit,platform):
                return {'reference':f'ghcr.io/owner/project-{component}@sha256:'+('c'*64),'digest':'sha256:'+'c'*64,
                        'config_digest':'sha256:'+'d'*64,'commit':commit,'platform':platform}
            with patch.object(fleet,'HERE',here),patch.object(fleet,'ROOT',repo),\
                 patch.object(fleet,'inspect_node',return_value={'architecture':'arm64','install_compose':False}),\
                 patch.object(fleet,'prepare_source',return_value=(self.commit,here/'archive',repo)),\
                 patch.object(fleet.Registry,'resolve',side_effect=resolve),patch.object(fleet,'pull_local'),\
                 patch.object(fleet,'prepare_registry_node',return_value={'cached':True}),\
                 patch.object(fleet,'deploy_node',return_value={'ok':True}) as deploy,patch.object(fleet,'tests') as health,\
                 patch.object(fleet,'local',side_effect=lambda args,**kw: '2.29.7' if args[:3]==['docker','compose','version'] else 'linux') as commands,\
                 patch.object(fleet,'build_image') as build,patch.object(fleet,'archive_config_digest') as exported,\
                 patch.dict(os.environ,{'CARIBOU_GHCR_USERNAME':'test-user','CARIBOU_GHCR_TOKEN':'private-test-token'}):
                result=fleet.main(['deploy','--inventory',str(here/'inventory.json'),'--nodes','jetson02,raspberry04',
                                   '--coordinator-address','192.168.2.64'])
            self.assertEqual(result,0)
            build.assert_not_called(); exported.assert_not_called()
            self.assertFalse(any('save' in call.args[0] for call in commands.call_args_list))
            for call in deploy.call_args_list: self.assertIsNone(call.args[8])
            health.assert_called_once()
