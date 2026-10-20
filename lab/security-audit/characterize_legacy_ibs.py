# Defensive characterization of the existing prototype, synthetic identities only.
import hashlib,json
from IBSSignature import IBSSignature
from SISIntegrity import SISParams
from py_ecc.bn128 import multiply
ibs=IBSSignature()
master,public=ibs.setup_zone_keys()
owner='audit-owner-A';victim='audit-owner-B'
owner_share=ibs.issue_secret_share(master,owner)
# The simulated attacker receives only its own share and public functions.
owner_factor=int.from_bytes(hashlib.sha256(owner.encode()).digest(),'big')%ibs.r_order
recoverable_master_point=multiply(owner_share,pow(owner_factor,-1,ibs.r_order))
victim_factor=int.from_bytes(hashlib.sha256(victim.encode()).digest(),'big')%ibs.r_order
forged_victim_share=multiply(recoverable_master_point,victim_factor)
params=SISParams(I0=17,a=23,C0=7)
message=['synthetic-audit-message',[[1,2,3]]]
valid=ibs.verify_signature(owner,None,public,ibs.generate_signature(message,params,owner_share),message,params)
forged=ibs.verify_signature(victim,None,public,ibs.generate_signature(message,params,forged_victim_share),message,params)
print(json.dumps({'fixture':'synthetic identities and generated test keys; no production credentials','legitimate_signature_valid':valid,'cross_owner_forgery_from_own_share_accepted':forged,'private_master_scalar_used_by_attacker':False,'production_files_modified':False},indent=2))
assert valid and forged,'Characterization changed: review the prototype before proceeding'
