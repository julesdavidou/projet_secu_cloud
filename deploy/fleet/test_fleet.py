import copy
import datetime as dt
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import fleet
import runpy


class FleetTests(unittest.TestCase):
    def test_video_probe_resolves_node_id_from_deployed_topology(self):
        endpoint = runpy.run_path(str(fleet.HERE/'test-video.py'))['storage_endpoint']
        topology = {'nodes':[{'id':'PI1','endpoint':'http://192.168.2.76:18081/'}]}
        self.assertEqual(endpoint({'node_id':'PI1'},topology),'http://192.168.2.76:18081')
        with self.assertRaises(RuntimeError): endpoint({'node_id':'UNKNOWN'},topology)
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.directory = Path(self.temp.name)
        self.addCleanup(self.temp.cleanup)
        self.inventory = json.loads((fleet.HERE/'inventory.example.json').read_text())
        for node in self.inventory['nodes']: node['password'] = 'test-password'

    def test_inventory_rejects_duplicate_ids_and_shell_values(self):
        path = self.directory/'inventory.json'
        fleet.write_json(path,self.inventory)
        self.assertEqual(len(fleet.load_inventory(path)['nodes']),5)
        for change in ({'id':'bad; command'}, {'host':'127.0.0.1;command'}, {'id':'camera'}, {'quota_bytes':0}):
            changed = copy.deepcopy(self.inventory)
            changed['nodes'][0].update(change)
            fleet.write_json(path,changed)
            with self.assertRaises((fleet.FleetError,ValueError)): fleet.load_inventory(path)

    def test_topology_announces_actual_lan_addresses_and_separate_identities(self):
        topology = fleet.topology(self.inventory)
        self.assertEqual(topology['zones'],['A','B'])
        self.assertEqual(len({n['id'] for n in topology['nodes']}),5)
        self.assertEqual(topology['nodes'][0]['endpoint'],'http://192.168.1.6:18081')
        self.assertTrue(all('password' not in n for n in topology['nodes']))

    def test_selection_limits_topology_without_changing_inventory(self):
        original = copy.deepcopy(self.inventory)
        selected = fleet.select_nodes(self.inventory,'jetson02, raspberry04,raspberry05')
        self.assertEqual([n['id'] for n in selected['nodes']],['jetson02','raspberry04','raspberry05'])
        self.assertEqual([n['id'] for n in fleet.topology(selected)['nodes']],['jetson02','raspberry04','raspberry05'])
        self.assertEqual(self.inventory,original)
        self.assertIs(fleet.select_nodes(self.inventory,None),self.inventory)

    def test_invalid_selection_is_rejected(self):
        for value in ('',',','camera,','unknown','camera,camera',[]):
            with self.subTest(value=value),self.assertRaises(fleet.FleetError):
                fleet.select_nodes(self.inventory,value)

    @unittest.skipUnless(sys.platform == 'win32','PowerShell Windows requis')
    def test_powershell_launchers_forward_the_node_array_as_one_argument(self):
        for name,action in (('deploy.ps1','deploy'),('test.ps1','test')):
            (self.directory/name).write_bytes((fleet.HERE/name).read_bytes())
            (self.directory/'python.ps1').write_text(
                "function Get-FleetPython { return '"+sys.executable.replace("'","''")+"' }",encoding='utf-8')
            (self.directory/'fleet.py').write_text('import json,sys; print(json.dumps(sys.argv[1:]))',encoding='utf-8')
            script = str(self.directory/name).replace("'","''")
            output = subprocess.check_output(['powershell.exe','-NoProfile','-ExecutionPolicy','Bypass',
                                              '-Command',f"& '{script}' -Nodes jetson02,raspberry04,raspberry05 -Jobs 3"],text=True)
            arguments = json.loads(output)
            self.assertEqual(arguments[0],action)
            self.assertEqual(arguments[arguments.index('--nodes')+1],'jetson02,raspberry04,raspberry05')
            self.assertEqual(arguments[arguments.index('--jobs')+1],'3')

    def test_selected_deployment_never_contacts_excluded_hosts_and_remembers_selection(self):
        here = self.directory/'tools'; here.mkdir()
        repository = self.directory/'repo'; (repository/'.git').mkdir(parents=True)
        runtime = here/'.runtime'; runtime.mkdir()
        (runtime/'control-arm64-abcd.tar').write_bytes(b'cached test image')
        inventory_path = here/'inventory.json'; fleet.write_json(inventory_path,self.inventory)
        wanted = ['jetson02','raspberry04','raspberry05']
        checked,updated = [],[]
        def inspect(node,*args):
            checked.append(node['id'])
            if node['id'] not in wanted: raise RuntimeError('excluded host is unreachable')
            return {'architecture':'arm64','install_compose':False}
        def update(node,*args):
            updated.append(node['id'])
            return {'ok':True}
        def command(args,**kwargs):
            if args[:3] == ['docker','compose','version']: return '2.29.7'
            if args[:2] == ['docker','info']: return 'linux'
            return ''
        with patch.object(fleet,'HERE',here),patch.object(fleet,'ROOT',repository),\
             patch.object(fleet,'inspect_node',side_effect=inspect),\
             patch.object(fleet,'prepare_source',return_value=('a'*40,here/'source.tar.gz',repository)),\
             patch.object(fleet,'build_image',return_value='sha256:abcd'),\
             patch.object(fleet,'local',side_effect=command),patch.object(fleet,'firewall') as firewall,\
             patch.object(fleet,'deploy_node',side_effect=lambda node,*args,**kw: update(node,*args)),patch.object(fleet,'tests') as tests,\
             patch.object(fleet,'archive_config_digest',return_value='sha256:'+'b'*64):
            result = fleet.main(['deploy','--inventory',str(inventory_path),'--nodes',','.join(wanted),
                                 '--coordinator-address','192.168.2.64','--configure-firewall','--jobs','3','--image-source','local'])
        self.assertEqual(result,0)
        self.assertCountEqual(checked,wanted)
        self.assertCountEqual(updated,wanted)
        self.assertEqual([n['id'] for n in firewall.call_args.args[0]['nodes']],wanted)
        self.assertEqual([n['id'] for n in tests.call_args.args[0]['nodes']],wanted)
        self.assertEqual([n['id'] for n in fleet.read_json(runtime/'topology.json')['nodes']],wanted)
        self.assertEqual(fleet.read_json(runtime/'settings.json')['active_nodes'],wanted)
        report = fleet.read_json(next((runtime/'reports').glob('*.json')))
        self.assertEqual(report['selected_nodes'],wanted)
        self.assertEqual(report['excluded_nodes'],['camera','jetson03'])

    def test_standalone_tests_reuse_selection_and_allow_explicit_override(self):
        here = self.directory/'tools'; here.mkdir()
        repository = self.directory/'repo'; (repository/'.git').mkdir(parents=True)
        inventory_path = here/'inventory.json'; fleet.write_json(inventory_path,self.inventory)
        runtime = here/'.runtime'
        fleet.write_json(runtime/'settings.json',{'coordinator_address':'192.168.2.64','active_nodes':['jetson02','raspberry04','raspberry05']})
        for extra,wanted in (([],['jetson02','raspberry04','raspberry05']),(['--nodes','camera'],['camera'])):
            with self.subTest(extra=extra),patch.object(fleet,'HERE',here),patch.object(fleet,'ROOT',repository),\
                 patch.object(fleet,'tests') as tests,patch.object(fleet,'inspect_node') as inspect:
                result = fleet.main(['test','--inventory',str(inventory_path),*extra])
            self.assertEqual(result,0)
            self.assertEqual([n['id'] for n in tests.call_args.args[0]['nodes']],wanted)
            inspect.assert_not_called()

    def test_unknown_selected_node_fails_before_ssh(self):
        here = self.directory/'tools'; here.mkdir()
        inventory_path = here/'inventory.json'; fleet.write_json(inventory_path,self.inventory)
        with patch.object(fleet,'HERE',here),patch.object(fleet,'inspect_node') as inspect:
            result = fleet.main(['check','--inventory',str(inventory_path),'--nodes','typo'])
        self.assertEqual(result,1)
        inspect.assert_not_called()

    def test_platforms_include_real_32_bit_raspberry_and_older_edge_compose(self):
        self.assertEqual(fleet.architecture('armv7l'),'arm/v7')
        self.assertEqual(fleet.architecture('aarch64'),'arm64')
        self.assertEqual(fleet.architecture('x86_64'),'amd64')
        fleet.compose_version('v2.20.3',minimum=(2,20,0))
        with self.assertRaises(fleet.FleetError): fleet.compose_version('v2.20.3')
        with self.assertRaises(fleet.FleetError): fleet.architecture('armv6l')

    def test_available_nodes_require_fresh_heartbeats_and_correct_endpoints(self):
        now = dt.datetime.now(dt.timezone.utc)
        snapshot = {'nodes':{n['id']:{'endpoint':f"http://{n['host']}:{n['http_port']}",
                    'node':{'zone_id':n['zone'],'state':'AVAILABLE','heartbeat_at':now.isoformat(),
                            'available_for_project_bytes':n['quota_bytes']}} for n in self.inventory['nodes']}}
        self.assertEqual(len(fleet.validate_nodes(snapshot,self.inventory,now)),5)
        for mutation in ({'state':'UNAVAILABLE'},{'heartbeat_at':(now-dt.timedelta(seconds=45)).isoformat()}):
            invalid = copy.deepcopy(snapshot)
            invalid['nodes']['camera']['node'].update(mutation)
            with self.assertRaises(fleet.FleetError): fleet.validate_nodes(invalid,self.inventory,now)
        snapshot['nodes']['camera']['endpoint']='http://edge-a:8088'
        with self.assertRaises(fleet.FleetError): fleet.validate_nodes(snapshot,self.inventory,now)

    def test_compose_ps_accepts_both_versions_of_json_output(self):
        rows = [{'Service':'edge-agent','State':'running'},{'Service':'edge-bridge','State':'running'}]
        self.assertEqual(fleet.parse_ps(json.dumps(rows)),rows)
        self.assertEqual(fleet.parse_ps('\n'.join(map(json.dumps,rows))),rows)

    def test_old_unlabelled_image_is_rebuilt_and_armv7_platform_is_explicit(self):
        old = {'Config':{'Labels':None},'Architecture':'arm','Id':'old'}
        new = {'Config':{'Labels':{'org.caribou.source-revision':'a'*40}},'Architecture':'arm','Id':'new'}
        with patch.object(fleet,'image_info',side_effect=[old,new]),patch.object(fleet,'local') as command:
            result=fleet.build_image(self.directory,'a'*40,'test:arm-v7','Dockerfile',arch='arm/v7')
        self.assertEqual(result,'new')
        self.assertIn('linux/arm/v7',command.call_args.args[0])
        self.assertIn('--load',command.call_args.args[0])
        with patch.object(fleet,'image_info',return_value=old):
            with self.assertRaises(fleet.FleetError): fleet.build_image(self.directory,'a'*40,'test:arm-v7','Dockerfile',skip=True)

    def test_operation_lock_prevents_concurrent_mutations(self):
        first = fleet.acquire_lock(self.directory)
        try:
            with self.assertRaises(fleet.FleetError): fleet.acquire_lock(self.directory)
        finally: first.close()
        fleet.acquire_lock(self.directory).close()

    def test_automatic_compose_install_checks_checksum_permissions_and_execution(self):
        calls,uploads = [],[]
        class FakeSSH:
            node={'id':'camera'}
            def run(self,command,**kwargs):
                calls.append((command,kwargs))
                return '2.26.1' if command.endswith('version --short') else ''
            def put(self,source,destination): uploads.append((source,destination))
        version = fleet.install_remote_compose(FakeSSH(),'/home/camera/cariboutv-lab',(Path('compose-armv7'),'a'*64))
        self.assertEqual(version,'2.26.1')
        self.assertEqual(uploads[0][1],'/home/camera/cariboutv-lab/incoming/docker-compose')
        self.assertTrue(any('sha256sum -c -' in command for command,_ in calls))
        self.assertTrue(any('install -m 755' in command and kwargs['sudo'] for command,kwargs in calls))
        self.assertTrue(calls[-1][0].endswith('/tools/docker-compose version --short'))

    def test_broken_compose_binary_is_not_reported_as_installed(self):
        class FakeSSH:
            node={'id':'camera'}
            def run(self,command,**kwargs): return 'invalid version'
            def put(self,*args): pass
        with self.assertRaises(fleet.FleetError):
            fleet.install_remote_compose(FakeSSH(),'/home/camera/cariboutv-lab',(Path('compose-armv7'),'a'*64))

    def test_source_snapshot_is_committed_main_not_uncommitted_working_files(self):
        root = self.directory/'repo'
        root.mkdir()
        def git(*args):
            return subprocess.check_output(['git','-C',str(root),*args],stderr=subprocess.PIPE,text=True).strip()
        git('init')
        for name in ('deploy/integration/start.ps1','deploy/integration/compose.yaml',
                     'deploy/integration/compose.external.yaml','deploy/lab/config/mosquitto.conf',
                     'deploy/lab/config/broker-entrypoint.sh','deploy/lab/compose.edge.yaml','deploy/lab/Dockerfile'):
            file = root/name
            file.parent.mkdir(parents=True,exist_ok=True)
            file.write_text('original\n')
        (root/'app.go').write_text('committed source')
        git('add','.')
        git('-c','user.name=Test','-c','user.email=test@example.invalid','commit','-m','fixture')
        commit = git('rev-parse','HEAD')
        git('update-ref','refs/remotes/origin/main',commit)
        (root/'app.go').write_text('uncommitted source must not deploy')
        runtime = self.directory/'runtime'; runtime.mkdir()
        with patch.object(fleet,'ROOT',root):
            selected,archive,source = fleet.prepare_source(runtime,skip_fetch=True)
        self.assertEqual(selected,commit)
        self.assertEqual((source/'app.go').read_text(),'committed source')
        self.assertTrue(archive.exists())

    def test_remote_payload_contains_only_the_selected_edges_credentials(self):
        root = self.directory/'repo'; runtime = self.directory/'runtime'
        runtime.mkdir()
        private = root/'deploy/integration/.runtime'
        identity = private/'identities/edge-camera'
        identity.mkdir(parents=True)
        (identity/'mqtt.password').write_text('edge-only-test')
        (private/'config.json').write_text('{}')
        archive = self.directory/'source.tar.gz'; archive.write_bytes(b'test archive')
        image_archive = self.directory/'image.tar'; image_archive.write_bytes(b'not needed when cached')
        calls,uploads = [],[]
        class FakeSSH:
            def __init__(self,*args): pass
            def __enter__(self): return self
            def __exit__(self,*args): pass
            def run(self,command,**kwargs):
                calls.append(command)
                return (0,'sha256:cached') if kwargs.get('check') is False else ''
            def put(self,source,destination): uploads.append((str(source),destination))
        camera = self.inventory['nodes'][1]
        with patch.object(fleet,'ROOT',root),patch.object(fleet,'SSH',FakeSSH):
            fleet.deploy_node(camera,{'home':'/home/camera','endpoint':'http://192.168.2.54:18081'},
                              self.inventory,'192.168.2.64','a'*40,archive,'test:image','sha256:cached',image_archive,runtime)
        self.assertTrue(any('edge-camera' in source for source,_ in uploads))
        self.assertFalse(any('operator' in source or 'edge-jetson' in source for source,_ in uploads))
        self.assertFalse(any(source.endswith('image.tar') for source,_ in uploads))
        self.assertTrue(any('remote-deploy.sh' in command for command in calls))
        self.assertFalse(any('down' in command or 'prune' in command for command in calls))

    def test_failed_preflight_never_builds_or_starts_any_service(self):
        here = self.directory/'deploy/fleet'; here.mkdir(parents=True)
        repository = self.directory/'repository'; (repository/'.git').mkdir(parents=True)
        inventory_path = here/'inventory.json'
        fleet.write_json(inventory_path,self.inventory)
        with patch.object(fleet,'HERE',here),patch.object(fleet,'ROOT',repository),patch.object(fleet,'inspect_node',side_effect=RuntimeError('SSH failed')),\
             patch.object(fleet,'prepare_source') as prepare,patch.object(fleet,'local') as commands:
            self.assertEqual(fleet.main(['deploy','--inventory',str(inventory_path),'--coordinator-address','192.168.2.64']),1)
        prepare.assert_not_called(); commands.assert_not_called()
        report = fleet.read_json(next((here/'.runtime/reports').glob('*.json')))
        self.assertFalse(report['ok'])
        self.assertEqual(len(report['preflight']),5)

    def test_external_tool_can_select_a_repository_with_spaces(self):
        here = self.directory/'cariboutv-outils'; here.mkdir()
        repository = self.directory/'autre dossier'/'repository'; (repository/'.git').mkdir(parents=True)
        inventory_path = here/'inventory.json'; fleet.write_json(inventory_path,self.inventory)
        with patch.object(fleet,'HERE',here),patch.object(fleet,'ROOT',self.directory/'missing'),\
             patch.object(fleet,'inspect_node',return_value={'ready':True}):
            result = fleet.main(['check','--inventory',str(inventory_path),'--repository',str(repository)])
            self.assertEqual(fleet.ROOT,repository.resolve())
        self.assertEqual(result,0)

    def test_missing_repository_stops_before_contacting_any_machine(self):
        here = self.directory/'cariboutv-outils'; here.mkdir()
        with patch.object(fleet,'HERE',here),patch.object(fleet,'ROOT',self.directory/'missing'),\
             patch.object(fleet,'inspect_node') as inspect:
            result = fleet.main(['deploy','--coordinator-address','192.168.2.64'])
        self.assertEqual(result,1)
        inspect.assert_not_called()

    def test_video_test_has_no_effect_when_imported(self):
        import importlib.util
        module_spec = importlib.util.spec_from_file_location('video_test',fleet.HERE/'test-video.py')
        module = importlib.util.module_from_spec(module_spec)
        with patch('subprocess.run') as commands:
            module_spec.loader.exec_module(module)
        commands.assert_not_called()


if __name__ == '__main__':
    unittest.main()
