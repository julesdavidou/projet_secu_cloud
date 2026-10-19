import json,tempfile,time,unittest
from pathlib import Path
from datetime import datetime,timezone
from lan import peer_files
from broker_contract import outgoing
from export_edge import export

class NetworkContractTests(unittest.TestCase):
 def test_mesh_never_forwards_another_actors_origin(self):
  cfg={'zones':['A','B'],'nodes':[{'id':'PI1'},{'id':'JETSON1'}]}
  for topic in outgoing(cfg,'PI1'):
   self.assertNotIn('#',topic);self.assertTrue(topic.endswith('edge-PI1') or '/node/PI1/' in topic,topic)
  with tempfile.TemporaryDirectory() as folder:
   expiry=datetime.fromtimestamp(time.time()+25,timezone.utc).isoformat()
   peer_files(folder,{'broker-jetson1:8883':{'address':'192.168.1.23:8883','expires_at':expiry}},cfg,'PI1')
   conf=(Path(folder)/'peers.conf').read_text();self.assertIn('bridge_insecure false',conf);self.assertIn('address broker-JETSON1:8883',conf);self.assertNotIn('topic #',conf)
   peer_files(folder,{},cfg,'PI1');self.assertEqual((Path(folder)/'expires').read_text(),'0')
 def test_export_copies_only_the_nodes_credentials(self):
  with tempfile.TemporaryDirectory() as folder:
   root=Path(folder);runtime=root/'runtime';runtime.mkdir();(runtime/'config.json').write_text(json.dumps({'zones':['A','B'],'nodes':[{'id':'PI1','zone':'A','endpoint':'https://edge-PI1:8443'}]}))
   roles=['edge-PI1','broker-PI1','reconstructor-PI1','storage-issuer-PI1','authority','authority-A','owner-a']
   for role in roles:
    path=runtime/'pki'/role;path.mkdir(parents=True)
    for name in ['ca.crt','tls.crt','tls.key']:(path/name).write_text('TEST_PLACEHOLDER_NOT_A_KEY')
   (runtime/'identities'/'edge-PI1').mkdir(parents=True);(runtime/'broker').mkdir();(runtime/'broker'/'acl').write_text('TEST_ACL')
   output=root/'bundle';export('PI1','192.168.1.10',output,'arm64',runtime)
   self.assertEqual({p.name for p in (output/'.runtime'/'pki').iterdir()},set(roles[:4]))
   config=json.loads((output/'compose.json').read_text());self.assertEqual(config['name'],'caribou-edge-pi1')
   self.assertNotIn('docker.sock',json.dumps(config));self.assertNotIn('privileged',json.dumps(config));self.assertNotIn('operator.token',json.dumps(config))
   with self.assertRaises(ValueError):export('PI1','192.168.1.10',output,'arm64',runtime)
   with self.assertRaises(ValueError):export('A1','192.168.1.10',root/'bad','arm64',runtime)
if __name__=='__main__':unittest.main()
