"""Test réel exécuté dans web : une seule vidéo privée de deux secondes.

Les médias de test restent stockés ; aucun volume/fichier existant n’est effacé.
Le scheduler choisit les destinations, ce test ne force aucun placement.
"""
import hashlib
import http.cookiejar
import json
from pathlib import Path
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import uuid


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def run():
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                       urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    anonymous = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    base = 'http://127.0.0.1:8080'
    checks = []
    def request(path, method='GET', body=None, headers=None, expected=200, client=opener):
        headers = dict(headers or {})
        if isinstance(body,dict):
            body = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        url = path if path.startswith('http') else base+path
        try:
            response = client.open(urllib.request.Request(url,data=body,method=method,headers=headers),timeout=180)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            data = response.read()
            require(response.status == expected, f'{method} {path} : HTTP {response.status}, attendu {expected} : {data[:200]!r}')
            return data, response.headers
    def js(*args, **kwargs): return json.loads(request(*args,**kwargs)[0])
    def passed(name):
        checks.append(name)
        print('PASS '+name,flush=True)
    request('/ready')
    request('/api/videos',client=anonymous,expected=401)
    name = 'fleet-'+uuid.uuid4().hex
    session = js('/api/auth/login','POST',urllib.parse.urlencode({'email':name+'@example.invalid',
                 'password':uuid.uuid4().hex}).encode(),{'Content-Type':'application/x-www-form-urlencoded'})
    require(session.get('status') == 'AUTHENTICATED','Authentification Java refusée')
    passed('authentification Java et accès anonyme refusé')
    with tempfile.TemporaryDirectory(prefix='fleet-video-') as directory:
        source = Path(directory)/'source.mp4'
        subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','testsrc2=size=320x180:rate=24',
                        '-f','lavfi','-i','sine=frequency=440:sample_rate=44100','-t','2',
                        '-c:v','libx264','-pix_fmt','yuv420p','-c:a','aac',str(source)],check=True,timeout=60)
        published = js('/api/videos?'+urllib.parse.urlencode({'title':name,'category':'projets','public':'false'}),
                       'POST',source.read_bytes(),{'Content-Type':'video/mp4'},expected=201)
        video = published['id']
        require(published['state']=='READY','Le stockage n’a pas atteint READY')
        record = js('/api/videos/'+video+'/placement')
        units = record.get('units',[])
        require(bool(units),'Aucune unité stockée')
        require(all(u['state']=='STORED' and u['proof']=='HTTP_SHA256_VERIFIED' for u in units),'Preuve de stockage incorrecte')
        require(video in [item['id'] for item in js('/api/videos')['items']],'Vidéo absente du catalogue')
        passed('publication privée, segmentation, placement Go, stockage réel READY')
        stored_bytes = 0
        for unit in units:
            token = (Path('/run/web-secrets')/('edge-'+unit['node_id']+'.http-token')).read_text().strip()
            raw,_ = request(unit['endpoint']+'/store/'+urllib.parse.quote(video,safe='')+'/'+str(unit['extent_id']),
                            headers={'Authorization':'Bearer '+token},client=anonymous)
            require(hashlib.sha256(raw).hexdigest()==unit['sha256'],'SHA256 incorrect après relecture Edge')
            require(len(raw)==unit['size_bytes'],'Taille incorrecte après relecture Edge')
            stored_bytes += len(raw)
        passed('relecture directe des unités Edge et vérification taille/SHA256')
        playback = js('/api/videos/'+video+'/playback')
        raw,_ = request(playback['url'])
        decoded = Path(directory)/'read.mp4'
        decoded.write_bytes(raw)
        subprocess.run(['ffmpeg','-v','error','-i',str(decoded),'-f','null','-'],check=True,timeout=60)
        info = json.loads(subprocess.check_output(['ffprobe','-v','error','-show_entries','format=duration',
                                                  '-of','json',str(decoded)],timeout=30))
        require(1.5<float(info['format']['duration'])<2.5,'Durée du média reconstruit incorrecte')
        part,headers = request(playback['url'],headers={'Range':'bytes=0-127'},expected=206)
        require(part==raw[:128] and headers['Content-Range'].startswith('bytes 0-127/'),'Range206 incorrect')
        request(playback['url'],client=anonymous,expected=401)
        passed('reconstruction MP4, décodage audio/vidéo et lecture partielle HTTP206')
        profile = js('/api/me/favorites/'+video,'PUT')
        require(video in profile['favorites'],'Favori non enregistré')
        profile = js('/api/me/progress/'+video,'PUT',{'position':1,'duration':2})
        require(any(h['videoId']==video and h['position']==1 for h in profile['history']),'Progression non enregistrée')
        request('/api/videos?title=invalid','POST',b'not-a-video',{'Content-Type':'video/mp4'},expected=422)
        passed('favoris, progression et rejet d’un faux fichier vidéo')
        result = {'status':'PASS','run':name,'video_id':video,'private':True,'checks':checks,
                  'stored_bytes':stored_bytes,'read_bytes':len(raw),'sha256_mp4':hashlib.sha256(raw).hexdigest(),
                  'nodes_used':sorted({u['node_id'] for u in units}),
                  'note':'Seuls ces nœuds ont transporté les unités de cette vidéo ; santé/enregistrement testés séparément sur tous les nœuds.'}
        print('FLEET_RESULT '+json.dumps(result),flush=True)


if __name__ == '__main__':
    run()
