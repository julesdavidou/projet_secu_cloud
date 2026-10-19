import base64,copy,hashlib,time,unittest
from secure_sis import *
from secure_operations import patch_blocks

class OperationsTest(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.master,cls.public=zone_keys();cls.owner='client-owner';cls.key=issue_share(cls.master,cls.owner);cls.zones=[{'zone_id':'A','epoch':1}]
  cls.mandate={'owner_id':cls.owner,'video_id':'video-root','delegate_id':'reconstructor-A1','operations':['modify_blocks'],'not_before':int(time.time())-1,'expires':int(time.time())+60,'max_revision':10,'max_bytes':1000,'max_budget_microcredits':1000,'nonce':'grant-0000000000000000001'}
  cls.dk,cls.certificate=delegate(cls.key,cls.mandate);cls.certificate['zones']=cls.zones
 def test_real_delegated_signature_and_root_scope(self):
  payload=[b'block'];asset={'name':'original.mp4','size_bytes':5,'sha256':hashlib.sha256(b'block').hexdigest(),'block_size':8,'blocks':[{'index':0,'size_bytes':5,'sha256':hashlib.sha256(b'block').hexdigest()}]}
  proof=sign_asset(self.dk,self.owner,'new-video',asset,payload,self.zones,root_id='video-root',operation_kind='modify_blocks',delegation=self.certificate)
  self.assertTrue(verify_asset(proof,self.owner,'new-video',asset,payload,{('A',1):self.public},root_id='video-root',operation_kind='modify_blocks'))
  self.assertFalse(verify_asset(proof,self.owner,'new-video',asset,payload,{('A',1):self.public},root_id='another-root',operation_kind='modify_blocks'))
  without=copy.deepcopy(proof);without.pop('delegation');self.assertFalse(verify_asset(without,self.owner,'new-video',asset,payload,{('A',1):self.public},root_id='video-root',operation_kind='modify_blocks'))
 def test_delegated_operation_requires_extra_pairing(self):
  op={'owner_id':self.owner,'video_id':'video-root','kind':'modify_blocks'};proof=sign_operation(self.dk,op);proof.update(zones=self.zones,delegation=self.certificate)
  verified=verify_signed_operation(proof,aggregate_public([self.public]));self.assertTrue(verified['verified']);self.assertEqual(verified['delegate_grant_id'],hashlib.sha256(canonical(self.mandate)).hexdigest())
  self.assertFalse(verify_operation(proof,self.owner,aggregate_public([self.public])))
 def test_variable_block_edits_and_order(self):
  b=lambda x:base64.b64encode(x).decode()
  self.assertEqual(patch_blocks(b'abcdefghij',4,'insert_blocks',[{'index':1,'delete_count':0,'blocks':[b(b'XY')]}]),b'abcdXYefghij')
  self.assertEqual(patch_blocks(b'abcdefghij',4,'delete_blocks',[{'index':1,'delete_count':1,'blocks':[]}]),b'abcdij')
  self.assertEqual(patch_blocks(b'abcdefghij',4,'modify_blocks',[{'index':1,'delete_count':1,'blocks':[b(b'XY')]}]),b'abcdXYij')
  for edits in [[{'index':3,'delete_count':1,'blocks':[]}],[{'index':1,'delete_count':1,'blocks':[]},{'index':0,'delete_count':1,'blocks':[]}]]:
   with self.assertRaises(ValueError):patch_blocks(b'abcdefghij',4,'delete_blocks',edits)
if __name__=='__main__':unittest.main()
