import hashlib,time,unittest,copy
from py_ecc.optimized_bls12_381 import G2,multiply
from secure_sis import *
class SecureSISTest(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.master,cls.pub=zone_keys();cls.owner='client-a';cls.private=issue_share(cls.master,cls.owner)
  cls.payloads=[b'first block',b'variable tail'];cls.asset={'name':'original.mp4','size_bytes':24,'sha256':hashlib.sha256(b''.join(cls.payloads)).hexdigest(),'block_size':16,'blocks':[{'index':i,'size_bytes':len(p),'sha256':hashlib.sha256(p).hexdigest()} for i,p in enumerate(cls.payloads)]}
  start=time.perf_counter();cls.proof=sign_asset(cls.private,cls.owner,'video',cls.asset,cls.payloads,[{'zone_id':'A','epoch':1}]);print('SIS_V3_SIGN_SECONDS',round(time.perf_counter()-start,3),flush=True)
  cls.trusted={('A',1):cls.pub}
 def verify(self,proof=None,payloads=None,owner=None,trusted=None):return verify_asset(proof or self.proof,owner or self.owner,'video',self.asset,payloads or self.payloads,trusted or self.trusted)
 def test_valid_and_corrupt(self):
  start=time.perf_counter();self.assertTrue(self.verify());print('SIS_V3_VERIFY_SECONDS',round(time.perf_counter()-start,3),flush=True)
  self.assertFalse(self.verify(payloads=[b'corrupt',self.payloads[1]]))
 def test_identity_anchor_context_and_params(self):
  self.assertFalse(self.verify(owner='client-b'))
  _,wrong=zone_keys();self.assertFalse(self.verify(trusted={('A',1):wrong}))
  for path,value in [('context','bad'),('sec_params',{}),('suite','legacy')]:
   proof=copy.deepcopy(self.proof);proof[path]=value;self.assertFalse(self.verify(proof))
  proof=copy.deepcopy(self.proof);proof['zones']*=2;self.assertFalse(self.verify(proof))
 def test_old_scalar_ratio_attack_fails(self):
  h=lambda s:int.from_bytes(hashlib.sha256(s.encode()).digest(),'big')%curve_order
  false_key=encode_point(multiply(decode_point(self.private),h('client-b')*pow(h(self.owner),-1,curve_order)%curve_order))
  forged=sign_asset(false_key,'client-b','video',self.asset,self.payloads,[{'zone_id':'A','epoch':1}])
  self.assertFalse(self.verify(proof=forged,owner='client-b'))
 def test_zone_accumulation(self):
  m2,p2=zone_keys();combined=aggregate_shares([self.private,issue_share(m2,self.owner)])
  zones=[{'zone_id':'A','epoch':1},{'zone_id':'B','epoch':1}]
  proof=sign_asset(combined,self.owner,'video',self.asset,self.payloads,zones)
  self.assertTrue(self.verify(proof=proof,trusted={**self.trusted,('B',1):p2}))
  self.assertTrue(self.verify()) # historical A-only signature remains valid
 def test_malformed_points(self):
  for value in ['',None,'A'*64,'x'*128]:
   with self.assertRaises((ValueError,TypeError)):decode_point(value)
 def test_operation_and_scoped_delegation(self):
  op={'owner_id':self.owner,'kind':'delete','video_id':'video','nonce':'one','revision':1}
  signed=sign_operation(self.private,op)
  self.assertTrue(verify_operation(signed,self.owner,aggregate_public([self.pub])))
  signed['operation']['revision']=2;self.assertFalse(verify_operation(signed,self.owner,aggregate_public([self.pub])))
  mandate={'owner_id':self.owner,'delegate_id':'edge-a','video_id':'video','operations':['replace'],'expires':int(time.time())+60}
  key,cert=delegate(self.private,mandate);self.assertTrue(verify_operation(cert,self.owner,aggregate_public([self.pub])))
  effective=cert['operation']['effective_id'];pub=cert['operation']['public_key'];params={'I0':17,'a':23,'C0':7}
  sig=sign_value(key,{'operation':op},[],params)
  self.assertTrue(verify_value(self.owner,aggregate_public([self.pub]),{'operation':op},[],params,sig,{'effective_id':effective,'public_key':pub}))
if __name__=='__main__':unittest.main()
