"""Read-only checks against actual V3 data; run by the explicit Docker admin.
No engine socket is exposed to this short-lived test process. Secrets are never
included in its output or JSON report. PEER_ONLY also works with the hub stopped.
"""
import datetime,hashlib,json,os,secrets,ssl,threading,time
from pathlib import Path
import httpx
import paho.mqtt.client as mqtt

ROOT=Path('/fixture')
def context(actor):
    folder=Path('/pki')/actor
    c=ssl.create_default_context(cafile=str(folder/'ca.crt'))
    c.minimum_version=ssl.TLSVersion.TLSv1_3
    c.load_cert_chain(str(folder/'tls.crt'),str(folder/'tls.key'))
    return c

def peer_ping():
    sender='edge-A1';target='edge-B1';root='lab/placement/v1/'
    corr=secrets.token_hex(16);topic=root+sender+'/reply/'+target
    replies=[];connected=threading.Event();subscribed=threading.Event();replied=threading.Event();denied=threading.Event()
    client=mqtt.Client(mqtt.CallbackAPIVersion.VERSION2,client_id='verification-'+secrets.token_hex(8),protocol=mqtt.MQTTv5)
    client.tls_set_context(context(sender))
    client.username_pw_set('shared-laboratory-username','not-used-for-identity')
    def on_connect(c,u,f,reason,props):
        assert not reason.is_failure,reason
        c.subscribe(topic,qos=1);connected.set()
    def on_subscribe(c,u,mid,reasons,props):
        if all(not r.is_failure for r in reasons):subscribed.set()
    def on_message(c,u,message):
        value=json.loads(message.payload)
        if value.get('correlation_id')==corr:replies.append(value);replied.set()
    def on_publish(c,u,mid,reason,props):
        if reason.is_failure:denied.set()
    client.on_connect=on_connect;client.on_subscribe=on_subscribe;client.on_message=on_message;client.on_publish=on_publish
    client.connect('broker-a',8883,keepalive=10);client.loop_start()
    try:
        assert connected.wait(10) and subscribed.wait(10),'peer subscriptions not ready'
        expires=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=15)).isoformat().replace('+00:00','Z')
        body=json.dumps({'op':'PEER_PING','body':{},'expires_at':expires,'correlation_id':corr,'reply_to':topic}).encode()
        for _ in range(2):client.publish(root+target+'/request/'+sender,body,qos=1).wait_for_publish(5)
        assert replied.wait(10),'no direct peer business ACK'
        time.sleep(.3)
        assert replies and all(r==replies[0] for r in replies),replies
        assert not replies[0].get('error') and replies[0]['body']['node_id']=='B1',replies
        print('PASS peer MQTT business ACK and duplicate correlation replay (MQTT3 Edge to MQTT5 Edge)',flush=True)
        client.publish(root+'central/request/admin',b'{}',qos=1).wait_for_publish(5)
        assert denied.wait(5),'Edge was able to impersonate admin topic'
        print('PASS MQTT mTLS identity overrides shared username; admin topic spoof refused',flush=True)
    finally:client.disconnect();client.loop_stop()

peer_ping()
if os.environ.get('PEER_ONLY')=='1':raise SystemExit(0)

fixtures={s:json.loads((ROOT/f'owner-{s}.json').read_text()) for s in ('a','b')}
headers={'Authorization':'Bearer '+Path('/operator/operator.token').read_text().strip()}
with httpx.Client(verify=context('admin'),timeout=90,trust_env=False) as admin:
    r=admin.get('https://central:8443/api/v1/snapshot',headers=headers);r.raise_for_status();snapshot=r.json()
    units=[]
    for slot,test in fixtures.items():
        record=snapshot['uploads'][test['video_id']]
        assert record['state']=='READY' and record['upload']['owner_id']==test['owner']
        assert {u['node_id'] for u in record['units']}=={'A1','B1'},'fixture must use two real zones'
        for unit in record['units']:
            node=unit['node_id'];owner=test['owner']
            registry=json.loads(Path('/registries/'+node+'/tenants.json').read_text())
            target=registry[owner]
            assert target['owner_id']==owner and unit['owner_id']==owner
            auth={'Authorization':'Bearer '+target['token']}
            with httpx.Client(verify=context('edge-'+node),timeout=15,trust_env=False) as edge:
                route=target['endpoint']+'/v1/storage/'+owner+'/'+unit['object']
                raw=edge.get(route,headers=auth);assert raw.status_code==200,(node,raw.status_code)
                assert len(raw.content)==unit['size_bytes'] and hashlib.sha256(raw.content).hexdigest()==unit['sha256']
                other=fixtures['b' if slot=='a' else 'a']['owner']
                wrong=edge.get(target['endpoint']+'/v1/storage/'+other+'/'+unit['object'],headers=auth)
                assert wrong.status_code==400,'cross-owner namespace accepted'
                wrong=edge.get(route,headers={'Authorization':'Bearer '+registry[other]['token']})
                assert wrong.status_code==401,'cross-container token accepted'
                wrong=edge.get(route)
                assert wrong.status_code==401,'anonymous storage accepted'
            opposite='edge-'+('B1' if node=='A1' else 'A1')
            with httpx.Client(verify=context(opposite),timeout=15,trust_env=False) as edge:
                assert edge.get(route,headers=auth).status_code==403,'opposite Edge certificate accepted'
            units.append({**unit,'video_id':test['video_id'],'slot':slot})
        print('PASS actual SHA256 files, two zones and tenant/token/certificate isolation for owner '+slot,flush=True)
    for slot,test in fixtures.items():
        with httpx.Client(verify=context('admin'),timeout=90,trust_env=False) as viewer:
            r=viewer.post('https://web:8443/api/auth/login',data={'email':'secure-owner-'+slot+'@example.invalid','password':'OnlyForLocalTest!2026'})
            assert r.status_code==200 and r.json()['client_id']==test['owner']
            other=fixtures['b' if slot=='a' else 'a']
            for suffix in ('','/playback','/original'):
                assert viewer.get('https://web:8443/api/videos/'+other['video_id']+suffix).status_code in (403,404),'private other-owner video exposed'
            original=viewer.get('https://web:8443/api/videos/'+test['video_id']+'/original')
            assert original.status_code==200 and hashlib.sha256(original.content).hexdigest()==test['source_sha256']
            # A historical publication remains verifiable after adding the second
            # zone's identity share. No key material is written to the report.
            with httpx.Client(verify=context('admin'),timeout=30,trust_env=False) as local:
                port='owner-'+slot
                login=local.post('http://'+port+':9300/owner/login',json={'email':'secure-owner-'+slot+'@example.invalid','password':'OnlyForLocalTest!2026'})
                assert login.status_code==200
                csrf=login.json()['csrf']
                enrolled=local.post('http://'+port+':9300/owner/zone',json={'zone_id':'B' if slot=='a' else 'A'},headers={'X-CSRF-Token':csrf})
                assert enrolled.status_code==200,enrolled.text
                assert {z['zone_id'] for z in enrolled.json()['zones']}=={'A','B'}
            original=viewer.get('https://web:8443/api/videos/'+test['video_id']+'/original')
            assert original.status_code==200 and hashlib.sha256(original.content).hexdigest()==test['source_sha256']
        print('PASS private web access and old proof after owner mobility to another authority for '+slot,flush=True)
    def move(zone):
        r=admin.post('https://central:8443/api/v1/rpc/edge-A1/MOVE',headers=headers,json={'node_id':'A1','zone_id':zone,'generation':0});r.raise_for_status()
        for _ in range(30):
            r=admin.get('https://central:8443/api/v1/snapshot',headers=headers);r.raise_for_status()
            if r.json()['nodes']['A1']['node']['zone_id']==zone:return
            time.sleep(.3)
        raise AssertionError('logical node mobility did not converge')
    test=fixtures['a']
    before=[(u['extent_id'],u['node_id'],u['object'],u['sha256']) for u in snapshot['uploads'][test['video_id']]['units']]
    try:
        move('B')
        r=admin.get('https://central:8443/api/v1/videos/'+test['video_id']+'/placements',headers=headers);r.raise_for_status();moved=r.json()
        assert [(u['extent_id'],u['node_id'],u['object'],u['sha256']) for u in moved['units']]==before
        assert all(u['zone_id']=='B' for u in moved['units'])
        with httpx.Client(verify=context('admin'),timeout=90,trust_env=False) as viewer:
            r=viewer.post('https://web:8443/api/auth/login',data={'email':'secure-owner-a@example.invalid','password':'OnlyForLocalTest!2026'});r.raise_for_status()
            r=viewer.get('https://web:8443/api/videos/'+test['video_id']+'/original')
            assert r.status_code==200 and hashlib.sha256(r.content).hexdigest()==test['source_sha256']
    finally:move('A')
    print('PASS logical node mobility A to B and back, stable node/object identities and exact reconstruction',flush=True)
    public_keys={}
    for zone in ('a','b'):
        r=admin.get('https://is-'+zone+':8443/v3/authority/info');r.raise_for_status();public_keys[zone]=r.json()
    report={'verified':True,'units':units,'owners':{s:t['owner'] for s,t in fixtures.items()},'public_authorities':public_keys,'node_available':{n:r['node']['available_for_project_bytes'] for n,r in snapshot['nodes'].items()},'physical_lab':'NOT_TESTED'}
    temp=ROOT/('verification-'+secrets.token_hex(8)+'.tmp')
    temp.write_text(json.dumps(report,indent=2));os.replace(temp,ROOT/'verification.json')
print('PASS signed VOD, real isolated storage, user access and authority mobility',flush=True)
