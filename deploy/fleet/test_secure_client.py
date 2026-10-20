"""Bundle/installer contracts with synthetic certificates and fake Docker only.
Actual mTLS/storage proofs are separate from these unit tests.
"""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import secure_client as sc


class SecureClientTests(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
  self.root=Path(self.temp.name);self.runtime=self.root/'runtime';self.runtime.mkdir()
  self.owner='client-00000000-0000-0000-0000-000000000001'
  self.images={k:{'reference':'test:'+k,'image_id':'sha256:fixture','platform':'linux/amd64'} for k in ['control','security','broker']}
  sc.write(self.runtime/'config.json',{'zones':['A','B'],'nodes':[{'id':'PI1','zone':'A','quota_bytes':2**30,'endpoint':'https://edge-PI1:8443','allowed_zones':['A','B'],'mode':'real-http'}]})
  # Deliberately synthetic PEM: never used by an actual TLS connection.
  for actor in ['viewer-lab-test','owner-lab-test','edge-PI1','broker-PI1','reconstructor-PI1','storage-issuer-PI1','authority','admin']:
   d=self.runtime/'pki'/actor;d.mkdir(parents=True)
   for f in ['ca.crt','tls.crt','tls.key']:(d/f).write_text('-----BEGIN CERTIFICATE-----\nZmFrZQ==\n-----END CERTIFICATE-----\n')
  d=self.runtime/'identities/edge-PI1';d.mkdir(parents=True);(d/'identity').write_text('synthetic')
  (self.runtime/'broker').mkdir();(self.runtime/'broker/acl').write_text('synthetic ACL')

 def bundle(self,roles='viewer',**kw):
  out=self.root/'bundle with spaces'
  sc.make_bundle(self.runtime,out,'test',sc.roles(roles),'192.168.2.51',self.images,kw.pop('owners',[]),**kw)
  return out

 def test_roles_quota_and_identity_admission(self):
  self.assertEqual(sc.roles('publisher'),['publisher','viewer'])
  self.assertEqual(sc.quota_bytes('.25',2**30),2**28)
  for value in ['nan','inf','-1','0','2',None]:
   with self.subTest(value=value),self.assertRaises(ValueError):sc.quota_bytes(value,2**30)
  for value in ['admin','viewer,root']:
   with self.assertRaises(ValueError):sc.roles(value)
  with self.assertRaisesRegex(ValueError,'admitted'):self.bundle('storage',node='UNKNOWN',owners=[self.owner])
  with self.assertRaisesRegex(ValueError,'owner'):self.bundle('storage',node='PI1')

 def test_viewer_exports_only_its_leaf_and_loopback_port(self):
  out=self.bundle(client_offset=100);compose=sc.read(out/'compose.json')
  self.assertEqual(set(compose['services']),{'portal'})
  self.assertEqual(compose['services']['portal']['ports'],['127.0.0.1:18890:8080'])
  self.assertEqual(sorted(p.name for p in (out/'.runtime/pki').iterdir()),['viewer-lab-test'])
  self.assertFalse(any(p.name=='ca.key' for p in out.rglob('*')))
  with self.assertRaisesRegex(ValueError,'exists'):self.bundle()

 def test_storage_reuses_v3_export_without_controller_or_owner_keys(self):
  out=self.bundle('storage,publisher',node='PI1',owners=[self.owner]);c=sc.read(out/'compose.json')
  self.assertEqual(set(c['services']),{'edge','broker','reconstructor','owner','portal'})
  self.assertEqual(c['services']['edge']['image'],'test:control')
  self.assertTrue((out/'provision-tenant.ps1').is_file())
  self.assertFalse((out/'.runtime/pki/authority').exists())
  self.assertFalse((out/'.runtime/pki/admin').exists())
  self.assertNotIn('/var/run/docker.sock',json.dumps(c))
  self.assertEqual(sc.read(out/'profile.json')['max_quota_bytes'],2**30)

 def test_node_admission_only_appends_and_refuses_existing_ids(self):
  import runpy
  admit=runpy.run_path(str(sc.HERE/'admit-storage.py'))['admitted_config']
  cfg=sc.read(self.runtime/'config.json');updated=admit(cfg,'JULES1','A',2**30)
  self.assertEqual(updated['nodes'][:-1],cfg['nodes'])
  self.assertEqual(len(cfg['nodes']),1)
  for node,zone,quota in [('PI1','A',2**30),('JULES1','UNKNOWN',2**30),('JULES1','A',0)]:
   with self.assertRaises(ValueError):admit(cfg,node,zone,quota)

 def test_installed_quota_cannot_be_silently_changed(self):
  out=self.bundle('storage',node='PI1',owners=[self.owner])
  sc.write(out/'.runtime/installation.json',{'roles':['storage'],'quota_bytes':2**28,'client_id':'test'})
  def docker(args,**kw):
   if args[1:3]==['compose','version']:return '2.40.0'
   if args[1]=='info':return 'linux/amd64'
   self.fail('Docker mutation before quota refusal: '+str(args))
  with patch.object(sc,'command',side_effect=docker),self.assertRaisesRegex(ValueError,'migration'):sc.install(out,'.5')


if __name__=='__main__':unittest.main()
