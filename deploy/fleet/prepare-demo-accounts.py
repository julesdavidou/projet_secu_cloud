"""Explicitly approved demo accounts using the existing Java login contract.
Passwords go only into the private output directory, never stdout or Git.
Existing output is reused and authenticated; credentials are never rotated.
"""
import argparse
import json
from pathlib import Path
import secrets
import urllib.parse
import urllib.request

from secure_client import client_actors, read, write


def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--portal',default='http://localhost:19690')
 p.add_argument('--clients',required=True);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
 url=urllib.parse.urlsplit(a.portal)
 if url.scheme!='http' or url.hostname not in ['localhost','127.0.0.1'] or url.path not in ('','/') or url.query or url.fragment or url.username:raise ValueError('explicit controller loopback portal required')
 clients=a.clients.split(',')
 if len(clients)!=len(set(clients)):raise ValueError('duplicate client ids')
 for client in clients:client_actors(client,['viewer','publisher'])
 a.output.mkdir(parents=True,exist_ok=True,mode=0o700);result=[]
 for client in clients:
  path=a.output/(client+'.json')
  account=read(path) if path.exists() else {'client_id':client,'email':client+'@caribou.lab.invalid','password':secrets.token_urlsafe(24)}
  if account.get('client_id')!=client or account.get('email')!=client+'@caribou.lab.invalid':raise ValueError('existing account metadata differs; no replacement')
  # Save the random password before the network request, to allow safe retry.
  write(path,account)
  data=urllib.parse.urlencode({'email':account['email'],'password':account['password']}).encode()
  with urllib.request.urlopen(a.portal.rstrip('/')+'/api/auth/login',data,timeout=30) as r:value=json.load(r)
  if value.get('status')!='AUTHENTICATED':raise ValueError('Java account authentication refused')
  if account.get('owner_id',value['client_id'])!=value['client_id']:raise ValueError('existing owner identity differs; no replacement')
  account['owner_id']=value['client_id'];write(path,account);result.append({'client_id':client,'owner_id':account['owner_id']})
 print(json.dumps({'accounts':result,'passwords':'private output files only'},indent=2))


if __name__=='__main__':main()
