import unittest
from segment_signer import SegmentSigner,verify_segment
from blocks import pad_block
class SISBindingTest(unittest.TestCase):
 def test_prototype_identity_and_complete_context_binding(self):
  signer=SegmentSigner("alice");payloads=[pad_block(b"payload")]
  context={"version":2,"owner_id":"alice","video_id":"v","name":"s","size_bytes":7,"block_count":1}
  entry=signer.sign_segment("v","s",payloads,context=context)
  self.assertTrue(verify_segment(signer.security_header(),entry,"v","s",payloads,context)[0])
  self.assertFalse(verify_segment(signer.security_header(),entry,"v","other",payloads,context)[0])
  self.assertFalse(verify_segment(signer.security_header(),entry,"v","s",[pad_block(b"corrupt")],context)[0])
  self.assertFalse(verify_segment(signer.security_header(),entry,"v","s",payloads,{**context,"size_bytes":6})[0])
if __name__=="__main__":unittest.main()
