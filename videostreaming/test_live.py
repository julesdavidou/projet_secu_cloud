import unittest,struct,tempfile
from pathlib import Path
from live_publish import entries
from secure_live import boxes,fragment_package
class LiveContracts(unittest.TestCase):
 def test_finalized_timeline_and_untrusted_paths(self):
  text='#EXTM3U\n#EXT-X-MAP:URI="init.mp4"\n#EXTINF:4.0,\n#EXT-X-PROGRAM-DATE-TIME:2026-10-20T08:00:00.000+00:00\nseg_00000.m4s\n'
  values,ended=entries(text);self.assertEqual(values[0]['duration_ms'],4000);self.assertFalse(ended)
  for invalid in [text.replace('seg_00000.m4s','../other.m4s'),text.replace('2026-10-20T08:00:00.000+00:00','2026-10-20T08:00:00'),text+'seg_00000.m4s\n']:
   with self.assertRaises(ValueError):entries(invalid)
 def test_fragment_bounds_and_variable_original(self):
  with self.assertRaises(ValueError):boxes(struct.pack('>I4s',100,b'mdat'))
  with tempfile.TemporaryDirectory() as folder:
   raw=b'x'*125013;pack=fragment_package(raw,Path(folder),'v-test',4000)
   self.assertEqual([b['size_bytes'] for b in pack['assets'][0]['blocks']],[125000,13]);self.assertEqual(b''.join((Path(folder)/p).read_bytes() for p in pack['files']),raw)
