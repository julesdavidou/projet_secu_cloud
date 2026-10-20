# Reproducible audit of default 32-bit H3, synthetic data only.
import hashlib,json
from SISIntegrity import SecurityParams,SISIntegrity,SISParams
seen={};left=right=None
for i in range(1000000):
 value=b'caribou-sis-audit-'+i.to_bytes(8,'big')
 short=hashlib.sha256(value).digest()[:4]
 if short in seen:
  left=seen[short];right=value;break
 seen[short]=value
assert left is not None,'No collision within bounded audit budget'
sis=SISIntegrity(SecurityParams())
A=sis.generate_matrix_A(SISParams(I0=17,a=23,C0=7))
V=sis.compute_V(A,sis.build_data_matrix([left]))
accepted=sis.verify_block(right,A,V[0])
print(json.dumps({'fixture':'synthetic fixed-length bytes; no production data','default_h3_bits':sis.params.lambda_bits,'candidates_examined':i+1,'different_payloads':left!=right,'different_full_sha256':hashlib.sha256(left).digest()!=hashlib.sha256(right).digest(),'changed_block_accepted_by_prototype_SIS':accepted,'current_V2_full_SHA256_check_would_reject':True},indent=2))
assert accepted and left!=right
