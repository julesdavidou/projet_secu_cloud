import asyncio
import dataclasses
import hashlib
import unittest
import httpx
from blocks import segment_to_blocks,reassemble_segment,BlockEnvelope
from reconstruction_contract import reconstruct_bytes,validate_job,signed_context

class BlocksTest(unittest.TestCase):
 def test_variable_length_and_order(self):
  for size in [1,7,257,125000]:
   for length in [1,size,max(1,size-1),size+1,size*3+2]:
    data=bytes(i%251 for i in range(length));blocks=segment_to_blocks("v","seg",3,data,size)
    self.assertEqual(reassemble_segment(list(reversed(blocks))),data)
 def test_missing_duplicate_identity_size_padding(self):
  blocks=segment_to_blocks("v","seg",0,b"abcdefghij",4)
  for bad in [blocks[:-1],blocks+[blocks[0]], [blocks[0],blocks[0],blocks[2]], [dataclasses.replace(blocks[0],video_id="other")]+blocks[1:], [dataclasses.replace(blocks[0],segment_byte_length=11)]+blocks[1:],blocks[:2]+[dataclasses.replace(blocks[2],payload=b"ijXX")]]:
   with self.assertRaises(ValueError):reassemble_segment(bad)

class JobTest(unittest.IsolatedAsyncioTestCase):
 def fixture(self):
  raw=b"original-variable-data";size=7;parts=[raw[i:i+size] for i in range(0,len(raw),size)]
  job={"version":2,"video_id":"v","owner_id":"alice","asset":{"name":"original.mp4","size_bytes":len(raw),"sha256":hashlib.sha256(raw).hexdigest(),"block_size":size,"blocks":[{"index":i,"extent_id":i,"size_bytes":len(part),"sha256":hashlib.sha256(part).hexdigest(),"node_id":"A1","endpoint":"http://edge","read_capability":"test"}for i,part in enumerate(parts)]}}
  return raw,parts,job
 async def reconstruct(self,job,parts):
  def handler(request):
   index=int(request.url.path.rsplit("/",1)[1]);return httpx.Response(200,content=parts[index])
  async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:return await reconstruct_bytes(job,{"http://edge"},client)
 async def test_reordered_real_bytes(self):
  raw,parts,job=self.fixture();job["asset"]["blocks"].reverse();self.assertEqual(await self.reconstruct(job,parts),raw)
 async def test_corrupt_missing_duplicate_and_false_final_hash(self):
  raw,parts,job=self.fixture();parts[0]=b"X"+parts[0][1:]
  with self.assertRaises(ValueError):await self.reconstruct(job,parts)
  for change in [lambda j:j["asset"]["blocks"].pop(),lambda j:j["asset"]["blocks"].append(j["asset"]["blocks"][0]),lambda j:j["asset"].update(sha256="0"*64)]:
   raw,parts,job=self.fixture();change(job)
   with self.assertRaises(ValueError):await self.reconstruct(job,parts)
 async def test_wrong_origin_and_unpinned_crypto_refused(self):
  raw,parts,job=self.fixture()
  with self.assertRaises(ValueError):validate_job(job,{"http://unrelated"})
  job["security"]={"owner_id":"alice","PK_s":"untrusted"}
  with self.assertRaises(ValueError):await self.reconstruct(job,parts)

if __name__=="__main__":unittest.main()
