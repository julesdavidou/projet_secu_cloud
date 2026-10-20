"""Offline reference comparison for a completed live recording; never invents
quality metrics for a camera stream without a reference. Requires FFmpeg.
"""
import argparse,json,re,subprocess

def quality(reference,reconstructed):
 def count(path):
  value=json.loads(subprocess.check_output(['ffprobe','-v','error','-select_streams','v:0','-count_frames','-show_entries','stream=nb_read_frames','-of','json',str(path)]))
  return int(value['streams'][0]['nb_read_frames'])
 result={'method':'decoded frames paired by ordinal position, full retained source reference; timestamps normalized ONLY for image comparison, not latency or timing fidelity','reference_frames':count(reference),'reconstructed_frames':count(reconstructed)}
 if result['reference_frames']!=result['reconstructed_frames']:raise ValueError('different decoded frame counts; ordinal full-reference comparison refused')
 for name,pattern in [('ssim',r'All:([0-9.]+)'),('psnr',r'average:([0-9.]+|inf)')]:
  process=subprocess.run(['ffmpeg','-hide_banner','-nostdin','-i',str(reference),'-i',str(reconstructed),'-filter_complex','[0:v]settb=AVTB,setpts=N/TB[a];[1:v]settb=AVTB,setpts=N/TB[b];[a][b]'+name,'-an','-fps_mode','passthrough','-f','null','-'],capture_output=True,check=True)
  found=re.findall(pattern,process.stderr.decode(errors='replace'))
  if not found:raise ValueError('quality metric missing')
  result[name]=float(found[-1]) if name=='ssim' else found[-1]
 return result
if __name__=='__main__':
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--reference',required=True);p.add_argument('--reconstructed',required=True);a=p.parse_args();print(json.dumps(quality(a.reference,a.reconstructed),indent=2))
