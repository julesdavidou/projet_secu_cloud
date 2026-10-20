"""Read-only real browser-path check (except Java login/logout sessions).
Requires an existing private test video and its independently recorded SHA-256.
This does not prove a second physical computer can pass Windows firewalls.
"""
import argparse
import getpass
import hashlib
import http.cookiejar
import json
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request


def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('--portal',default='http://localhost:18790');p.add_argument('--email',required=True)
 p.add_argument('--other-email',required=True);p.add_argument('--video',required=True);p.add_argument('--expected-sha256',required=True)
 p.add_argument('--output',type=Path,required=True);a=p.parse_args()
 password=getpass.getpass('Java password (not saved): ');other=getpass.getpass('Other user password (not saved): ')
 c=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
 def request(path,data=None,headers=None):
  try:
   with c.open(urllib.request.Request(a.portal.rstrip('/')+path,data,headers or {}),timeout=240) as r:return r.status,r.read()
  except urllib.error.HTTPError as e:return e.code,e.read()
 def login(email,pwd):
  status,raw=request('/api/auth/login',urllib.parse.urlencode({'email':email,'password':pwd}).encode())
  assert status==200 and json.loads(raw).get('status')=='AUTHENTICATED','Java authentication failed'
 status,raw=request('/integration/');assert status==200 and len(raw)>100
 login(a.email,password)
 status,raw=request('/api/videos');assert status==200 and a.video in raw.decode()
 status,raw=request('/api/videos/'+a.video+'/original');assert status==200
 digest=hashlib.sha256(raw).hexdigest();assert digest==a.expected_sha256,'original source differs'
 size=len(raw)
 status,raw=request('/api/videos/'+a.video+'/playback');assert status==200
 playback=json.loads(raw);status,raw=request(playback['url'],headers={'Range':'bytes=0-127'});assert status==206 and len(raw)==128
 status,_=request('/api/auth/logout',b'',{'Origin':'http://foreign.invalid'});assert status==403,'cross-origin operation accepted'
 status,_=request('/ready',headers={'Host':'foreign.invalid:18790'});assert status==403,'DNS rebinding Host accepted'
 request('/api/auth/logout',b'');login(a.other_email,other)
 status,_=request('/api/videos/'+a.video+'/original');assert status==404,'private video crossed user boundary'
 request('/api/auth/logout',b'')
 result={'portal':a.portal,'source_sha256':digest,'source_size':size,'java_authentication':'PASS','real_original_and_playback_range':'PASS','user_isolation':'PASS','cross_origin_and_host_refusal':'PASS','storage':'existing real Edge files, no mock','separate_physical_pc':'NOT_TESTED'}
 a.output.write_text(json.dumps(result,indent=2),encoding='utf-8');print(json.dumps(result,indent=2))


if __name__=='__main__':main()
