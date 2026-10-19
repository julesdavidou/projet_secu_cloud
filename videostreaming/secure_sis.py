"""Version 3 SIS/Hess adapter. Legacy colleague equations are preserved, not reused
with their unsafe hash-to-generator H1 or 32-bit H3 defaults. Not a security proof.
Private owner/zone keys never appear in a public envelope.
"""
from __future__ import annotations
import base64,hashlib,json,secrets
from functools import lru_cache
import numpy as np
from py_ecc.optimized_bls12_381 import G2,Z1,Z2,add,multiply,neg,pairing,curve_order,eq,is_inf
from py_ecc.bls.hash_to_curve import hash_to_G1
from py_ecc.bls.point_compression import compress_G1,compress_G2,decompress_G1,decompress_G2

SUITE='SIS-HESS-BLS12381-v3'
PARAMS={'l':128,'m':4096,'q':65537,'lambda_bits':256}
DST=b'CARIBOU_IBS_V3_BLS12381G1_XMD:SHA-256_SSWU_RO_'
MAX_BLOCKS=512

def canonical(value):return json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=False,allow_nan=False).encode()
def scalar():return secrets.randbelow(curve_order-1)+1
def h1(identity):
 if not isinstance(identity,str) or not 1<=len(identity)<=512:raise ValueError('invalid identity')
 return hash_to_G1(identity.encode(),DST,hashlib.sha256)
def encode_point(point,group=1):
 if is_inf(point):raise ValueError('identity point forbidden')
 values=(compress_G1(point),) if group==1 else compress_G2(point)
 return base64.urlsafe_b64encode(b''.join(int(v).to_bytes(48,'big') for v in values)).rstrip(b'=').decode()
def decode_point(value,group=1):
 if not isinstance(value,str) or len(value)!=(64 if group==1 else 128):raise ValueError('invalid point length')
 try:
  raw=base64.b64decode(value+'='*((-len(value))%4),altchars=b'-_',validate=True)
  integers=[int.from_bytes(raw[i:i+48],'big') for i in range(0,len(raw),48)]
  point=decompress_G1(integers[0]) if group==1 else decompress_G2(tuple(integers))
 except (ValueError,TypeError,AssertionError,OverflowError) as e:raise ValueError('invalid compressed point') from e
 if is_inf(point) or not eq(multiply(point,curve_order),Z1 if group==1 else Z2) or encode_point(point,group)!=value:raise ValueError('noncanonical point or wrong subgroup')
 return point

def zone_keys():
 secret=scalar();return secret,encode_point(multiply(G2,secret),2)
def issue_share(master,owner):return encode_point(multiply(h1(owner),master))
def aggregate_shares(shares):
 if not 1<=len(shares)<=6:raise ValueError('invalid share count')
 total=Z1
 for share in shares:total=add(total,decode_point(share))
 return encode_point(total)
def aggregate_public(public_keys):
 if not 1<=len(public_keys)<=6:raise ValueError('invalid zone count')
 total=Z2
 for public in public_keys:total=add(total,decode_point(public,2))
 return total

def params_ok(params):
 if not isinstance(params,dict) or set(params)!={'I0','a','C0'}:raise ValueError('invalid SIS generator schema')
 if any(type(v)is not int for v in params.values()):raise ValueError('invalid SIS parameter type')
 q=PARAMS['q']
 if not (0<params['I0']<q and 1<params['a']<q-1 and 0<params['C0']<q):raise ValueError('invalid SIS seed bounds')

def new_params():
 # Public randomness, not secret material. Reject visibly degenerate generators.
 for _ in range(32):
  params={'I0':secrets.randbelow(65536)+1,'a':secrets.randbelow(65533)+2,'C0':secrets.randbelow(65536)+1}
  try:matrix(params);return params
  except ValueError:pass
 raise ValueError('unable to generate nondegenerate SIS matrix')

@lru_cache(maxsize=8)
def _matrix(seed,a,carry):
 q=PARAMS['q'];values=[];x=seed;c=carry
 for _ in range(PARAMS['l']*PARAMS['m']):
  total=a*x+c;x=total%q;c=total//q;values.append(x)
 A=np.array(values,dtype=np.int64).reshape(PARAMS['l'],PARAMS['m'])
 if np.unique(A).size<1024:raise ValueError('degenerate SIS generator')
 A.flags.writeable=False
 return A

def matrix(params):params_ok(params);return _matrix(params['I0'],params['a'],params['C0'])
def fingerprint(payloads,params):
 if not 1<=len(payloads)<=MAX_BLOCKS:raise ValueError('invalid block count')
 if any(not isinstance(p,bytes) or not 0<len(p)<=1024*1024 for p in payloads):raise ValueError('invalid block payload')
 hashes=np.frombuffer(b''.join(hashlib.sha256(p).digest() for p in payloads),dtype=np.uint8).reshape(len(payloads),32)
 bits=np.unpackbits(hashes,axis=1).astype(np.int64)
 X=np.tile(bits,(1,PARAMS['m']//PARAMS['lambda_bits'])).T
 return ((matrix(params)@X)%PARAMS['q']).T.tolist()

def _challenge(context,V,params,r):
 # Pairing output is encoded field-by-field; never Python repr or pickle.
 raw=b''.join(int(c).to_bytes(48,'big') for c in r.coeffs)
 return int.from_bytes(hashlib.sha256(b'caribou-hess-v3\x00'+canonical({'context':context,'V':V,'paramA':params})+raw).digest(),'big')%curve_order

def sign_value(private,context,V,params):
 S=decode_point(private)
 for _ in range(8):
  k=scalar();P1=multiply(h1('nonce:'+secrets.token_hex(32)),scalar())
  r=pairing(G2,P1)**k;w1=_challenge(context,V,params,r)
  if w1:return {'w1':str(w1),'w2':encode_point(add(multiply(S,w1),multiply(P1,k)))}
 raise ValueError('invalid signature challenge')

def verify_value(owner,public,context,V,params,signature,delegation=None):
 try:
  if not isinstance(signature,dict) or set(signature)!={'w1','w2'}:return False
  text=signature['w1']
  if not isinstance(text,str) or not text.isdigit() or len(text)>78:return False
  w1=int(text)
  if not 0<w1<curve_order or str(w1)!=text:return False
  w2=decode_point(signature['w2'])
  term=pairing(public,neg(h1(owner)))
  if delegation:
   term*=pairing(decode_point(delegation['public_key'],2),neg(h1(delegation['effective_id'])))
  r=pairing(G2,w2)*term**w1
  return w1==_challenge(context,V,params,r)
 except (ValueError,TypeError,KeyError,AssertionError,OverflowError):return False

def context_for(owner,video,asset,revision=1,previous='',public=False):
 return {'suite':SUITE,'owner_id':owner,'video_id':video,'revision':revision,'previous':previous,'public':bool(public),
 'name':asset['name'],'size_bytes':asset['size_bytes'],'sha256':asset['sha256'],'block_size':asset['block_size'],
 'blocks':[{k:b[k] for k in ['index','size_bytes','sha256']} for b in sorted(asset['blocks'],key=lambda b:b['index'])]}

def sign_asset(private,owner,video,asset,payloads,zones,revision=1,previous='',public=False):
 params=new_params();V=fingerprint(payloads,params);context=context_for(owner,video,asset,revision,previous,public)
 return {'suite':SUITE,'owner_id':owner,'zones':zones,'sec_params':PARAMS.copy(),'sis_params':params,
 'context':context,'V':V,'signature':sign_value(private,context,V,params)}

def verify_asset(envelope,owner,video,asset,payloads,trusted,revision=1,previous='',public=False):
 try:
  if envelope['suite']!=SUITE or envelope['owner_id']!=owner or envelope['sec_params']!=PARAMS:return False
  expected=context_for(owner,video,asset,revision,previous,public)
  if envelope['context']!=expected:return False
  zones=envelope['zones']
  if not isinstance(zones,list) or not 1<=len(zones)<=6:return False
  seen=set();keys=[]
  for z in zones:
   if set(z)!={'zone_id','epoch'} or type(z['epoch'])is not int or z['epoch']<1:return False
   key=(z['zone_id'],z['epoch'])
   if key in seen or key not in trusted:return False
   seen.add(key);keys.append(trusted[key])
  V=fingerprint(payloads,envelope['sis_params'])
  if envelope['V']!=V:return False
  return verify_value(owner,aggregate_public(keys),expected,V,envelope['sis_params'],envelope['signature'])
 except (ValueError,TypeError,KeyError,AssertionError,OverflowError):return False

# Authenticated operations use the SAME identity keys. They do not claim that a
# hash or a transport certificate is an IBS signature.
def sign_operation(private,operation):
 params={'I0':17,'a':23,'C0':7}
 return {'suite':SUITE,'operation':operation,'signature':sign_value(private,{'operation':operation},[],params)}
def verify_operation(proof,owner,public):
 try:return proof['suite']==SUITE and proof['operation']['owner_id']==owner and verify_value(owner,public,{'operation':proof['operation']},[],{'I0':17,'a':23,'C0':7},proof['signature'])
 except (KeyError,TypeError,ValueError):return False

def delegate(private,mandate):
 effective='delegate:'+hashlib.sha256(canonical(mandate)).hexdigest();y=scalar()
 key=encode_point(add(decode_point(private),multiply(h1(effective),y)))
 public=encode_point(multiply(G2,y),2)
 certificate=sign_operation(private,{'owner_id':mandate['owner_id'],'kind':'delegation','mandate':mandate,'public_key':public,'effective_id':effective})
 return key,certificate
