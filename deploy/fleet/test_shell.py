"""Execute deployment shells against a fake Docker in an isolated Linux container.

Run only with CARIBOU_SHELL_TESTS=1, as root in a disposable container.
"""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


@unittest.skipUnless(os.environ.get('CARIBOU_SHELL_TESTS')=='1' and os.name=='posix','Isolated Linux shell test only')
class ShellTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='caribou-test-',dir='/home')
        self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)
        self.base=self.root/'cariboutv-lab'
        self.incoming=self.base/'incoming'; (self.incoming/'identity').mkdir(parents=True)
        self.bin=self.root/'bin'; self.bin.mkdir()
        self.events=self.root/'events'
        fake='''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]
with open(os.environ['EVENTS'],'a') as output: output.write(json.dumps(args)+'\\n')
if args[:2]==['image','inspect']:
    template=args[3]
    if template=='{{.Id}}': print(os.environ['ACTUAL_IMAGE'])
    elif 'revision' in template or 'source-revision' in template: print(os.environ['REVISION'])
    else: print('linux/arm64')
elif args and args[0]=='login':
    token=sys.stdin.read().strip()
    assert token=='not-a-real-token'
    config=Path(os.environ['DOCKER_CONFIG'])/'config.json'; config.write_text(token)
    Path(os.environ['EVENTS']+'.credential-path').write_text(str(config.parent))
    if os.environ.get('AUTH_FAIL'): sys.exit(1)
elif 'up' in args and os.environ.get('FAIL_UPDATE'):
    if Path('.env').read_text()=='new-config': sys.exit(42)
'''
        (self.bin/'docker').write_text(fake); (self.bin/'docker').chmod(0o755)
        (self.bin/'getent').write_text('#!/bin/sh\nexit 0\n'); (self.bin/'getent').chmod(0o755)
        self.env={**os.environ,'PATH':str(self.bin)+':'+os.environ['PATH'],'EVENTS':str(self.events),
                  'ACTUAL_IMAGE':'sha256:'+'a'*64,'REVISION':'b'*40}
        self.archive=self.incoming/'source.tar.gz'
        with tarfile.open(self.archive,'w:gz') as tar:
            data=b'services: {}\n'
            member=tarfile.TarInfo('deploy/lab/compose.edge.yaml'); member.size=len(data)
            tar.addfile(member,io.BytesIO(data))
        for name,data in [('edge.env','new-config'),('config.json','{}'),('manifest.json','{}'),('identity/mqtt.password','unrelated-test-secret')]:
            (self.incoming/name).write_text(data)

    def deploy(self):
        script=Path(__file__).with_name('remote-deploy.sh')
        digest=hashlib.sha256(self.archive.read_bytes()).hexdigest()
        return subprocess.run(['bash',str(script),str(self.base),'b'*40,digest,'test:image',
                               'sha256:'+'a'*64,'test-node','1024','sha256:'+'c'*64,'arm64','local'],
                              env=self.env,text=True,capture_output=True)

    def test_legacy_config_id_is_accepted_and_volumes_are_not_deleted(self):
        result=self.deploy()
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual((self.base/'deployment/.env').read_text(),'new-config')
        events=self.events.read_text()
        self.assertNotIn('prune',events); self.assertNotIn('down',events)

    def test_wrong_image_stops_before_active_configuration_changes(self):
        self.env['ACTUAL_IMAGE']='sha256:'+'d'*64
        result=self.deploy()
        self.assertNotEqual(result.returncode,0)
        self.assertFalse((self.base/'deployment/.env').exists())
        self.assertNotIn('"up"',self.events.read_text())

    def test_topology_change_recreates_containers_even_with_the_same_image(self):
        self.assertEqual(self.deploy().returncode,0)
        self.events.write_text('')
        (self.incoming/'config.json').write_text('{"new_node":"added"}')
        result=self.deploy()
        self.assertEqual(result.returncode,0,result.stderr)
        rows=[json.loads(line) for line in self.events.read_text().splitlines()]
        up=next(row for row in rows if 'up' in row)
        self.assertIn('--force-recreate',up)
        self.assertEqual((self.base/'deployment/edge-config/config.json').read_text(),'{"new_node":"added"}')

    def test_failed_update_restores_previous_configuration_and_attempts_restart(self):
        deployment=self.base/'deployment'; deployment.mkdir()
        (deployment/'.env').write_text('old-config')
        (deployment/'compose.edge.yaml').write_text('old-compose')
        (deployment/'identity').write_text('test-node:1024\n')
        self.env['FAIL_UPDATE']='1'
        result=self.deploy()
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((deployment/'.env').read_text(),'old-config')
        self.assertEqual((deployment/'compose.edge.yaml').read_text(),'old-compose')
        self.assertEqual(self.events.read_text().count('"up"'),2)

    def test_temporary_registry_credentials_are_removed_on_success_and_failure(self):
        for failed in (False,True):
            env={**self.env}
            if failed: env['AUTH_FAIL']='1'
            script=Path(__file__).with_name('remote-pull.sh')
            result=subprocess.run(['sh',str(script),'ghcr.io/owner/project@sha256:'+'c'*64,'linux/arm64',
                                   'sha256:'+'a'*64,'sha256:'+'c'*64,'b'*40],
                                  input='test-user\nnot-a-real-token\n',env=env,text=True,capture_output=True)
            self.assertEqual(result.returncode==0,not failed,result.stderr)
            directory=Path(Path(str(self.events)+'.credential-path').read_text())
            self.assertFalse(directory.exists())
            self.assertNotIn('not-a-real-token',result.stdout+result.stderr+self.events.read_text())


if __name__=='__main__': unittest.main()
