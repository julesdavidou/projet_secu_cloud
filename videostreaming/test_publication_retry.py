"""Transport doubles only; real stored files are covered by Docker smoke."""
import tempfile,unittest
from pathlib import Path
from unittest.mock import patch
import httpx
import secure_services as services

class RetryTests(unittest.TestCase):
 def test_lost_reply_reuses_exact_archive(self):
  calls=[]
  class Client:
   def __enter__(self):return self
   def __exit__(self,*args):pass
   def post(self,url,**kwargs):
    calls.append(kwargs['content'])
    req=httpx.Request('POST',url)
    if len(calls)==1:raise httpx.ReadTimeout('test lost ACK',request=req)
    return httpx.Response(201,json={'id':'v-fixture'},request=req)
  with tempfile.TemporaryDirectory() as folder:
   root=Path(folder);(root/'outbox').mkdir();archive=root/'packet.tar';archive.write_bytes(b'exact signed fixture bytes')
   with patch.object(services,'DATA',root),patch.object(services,'client',return_value=Client()),patch.object(services.time,'sleep'):
    result=services.deliver_publication({'owner':'client-fixture','cookie':'test-only'},archive,'v-fixture')
   self.assertEqual(result['id'],'v-fixture');self.assertEqual(calls,[b'exact signed fixture bytes']*2)
   self.assertTrue((root/'outbox'/'v-fixture.json').exists())
 def test_signature_refusal_is_not_retried(self):
  class Client:
   def __enter__(self):return self
   def __exit__(self,*args):pass
   def post(self,url,**kwargs):return httpx.Response(422,json={'stage':'integrity','retryable':False},request=httpx.Request('POST',url))
  with tempfile.TemporaryDirectory() as folder:
   path=Path(folder)/'packet.tar';path.write_bytes(b'fixture')
   with patch.object(services,'client',return_value=Client()),patch.object(services.time,'sleep') as sleep:
    with self.assertRaises(services.PublicationError) as error:services.deliver_publication({'owner':'client-fixture','cookie':'test-only'},path,'v-fixture')
   self.assertEqual(error.exception.status,422);sleep.assert_not_called()

if __name__=='__main__':unittest.main()
