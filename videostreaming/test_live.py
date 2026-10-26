import unittest,struct,tempfile
from pathlib import Path
from live_publish import entries,ffmpeg_command
from secure_live import boxes,fragment_package
class LiveContracts(unittest.TestCase):
 def test_transcoding_forces_four_second_keyframes_and_copy_remains_available(self):
  from types import SimpleNamespace
  args=SimpleNamespace(file=None,rtmp_listen='rtmp://0.0.0.0:1935/live/stream',transcode=True,segment_seconds=4,hls_dir=Path('/tmp/capture'))
  command=ffmpeg_command(args)
  self.assertIn('expr:gte(t,n_forced*4)',command);self.assertIn('libx264',command)
  args.transcode=False;command=ffmpeg_command(args)
  self.assertIn('copy',command);self.assertNotIn('-force_key_frames',command)
 def test_finalized_timeline_and_untrusted_paths(self):
  text='#EXTM3U\n#EXT-X-MAP:URI="init.mp4"\n#EXTINF:4.0,\n#EXT-X-PROGRAM-DATE-TIME:2026-10-20T08:00:00.000+00:00\nseg_00000.m4s\n'
  values,ended=entries(text);self.assertEqual(values[0]['duration_ms'],4000);self.assertFalse(ended)
  for invalid in [text.replace('seg_00000.m4s','../other.m4s'),text.replace('2026-10-20T08:00:00.000+00:00','2026-10-20T08:00:00'),text+'seg_00000.m4s\n']:
   with self.assertRaises(ValueError):entries(invalid)
 def test_fragment_bounds_and_variable_original(self):
  with self.assertRaises(ValueError):boxes(struct.pack('>I4s',100,b'mdat'))
  with tempfile.TemporaryDirectory() as folder:
   raw=b'x'*125013;pack=fragment_package(raw,Path(folder),'v-test',4000,block_size=125000)
   self.assertEqual([b['size_bytes'] for b in pack['assets'][0]['blocks']],[125000,13]);self.assertEqual(b''.join((Path(folder)/p).read_bytes() for p in pack['files']),raw)
 def test_live_large_blocks_preserve_every_original_byte(self):
  from unittest.mock import patch
  import os
  with tempfile.TemporaryDirectory() as folder,patch.dict(os.environ,{},clear=True):
   raw=bytes(range(256))*14677;pack=fragment_package(raw,Path(folder),'v-test',4000)
   self.assertEqual(len(pack['files']),4)
   self.assertEqual(b''.join((Path(folder)/p).read_bytes() for p in pack['files']),raw)
   self.assertEqual(pack['assets'][0]['block_size'],1048576)
   for size in (0,1048577,True):
    with self.assertRaises(ValueError):fragment_package(raw,Path(folder),'v-test',4000,block_size=size)
