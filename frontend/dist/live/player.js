const $=id=>document.getElementById(id), video=$('video');
let hls,clockOffset=0,clockUncertainty=null,started=0,firstFrame=null,stallStart=null,stalls=0,stallMS=0,rendered=0,baseFrame=0,baseTime=0,samples=[],downloads=[],timer,liveID='',frameCallback;
const json=async(path,options={})=>{const before=Date.now(),r=await fetch(path,{credentials:'same-origin',...options});if(!r.ok)throw new Error(`HTTP ${r.status}: ${await r.text()}`);const value=await r.json();if(value.server_unix_ms){const after=Date.now();clockOffset=value.server_unix_ms-(before+after)/2;clockUncertainty=(after-before)/2;}return value;};
const error=e=>{$('status').textContent=e.message||String(e);};
async function refresh(){const data=await json('/api/live');$('streams').replaceChildren(...data.items.map(item=>{const option=document.createElement('option');option.value=item.id;option.textContent=`${item.title} — ${item.ended?'terminé':'en cours'}`;return option;}));}
$('login').onsubmit=async e=>{e.preventDefault();try{const r=await json('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({email:$('email').value,password:$('password').value})});if(r.status!=='AUTHENTICATED')throw new Error('Connexion refusée');$('password').value='';$('viewer').hidden=false;await refresh();const requested=new URL(location.href).searchParams.get('id');if(requested)$('liveid').value=requested;}catch(e){error(e);$('viewer').hidden=false;}};
$('refresh').onclick=()=>refresh().catch(error);
function snapshot(){const now=performance.now(),quality=video.getVideoPlaybackQuality?.();const ahead=video.buffered.length?Math.max(0,video.buffered.end(video.buffered.length-1)-video.currentTime):0;const playingDate=hls?.playingDate;
return {elapsed_ms:Math.round(now-started),first_display_ms:firstFrame===null?null:Math.round(firstFrame),first_display_basis:video.requestVideoFrameCallback?'requestVideoFrameCallback':'playing-event fallback',rendered_frames:rendered,dropped_frames:quality?.droppedVideoFrames??null,total_video_frames:quality?.totalVideoFrames??null,render_fps:now>baseTime?(rendered-baseFrame)*1000/(now-baseTime):null,stall_count:stalls,stall_ms:Math.round(stallMS+(stallStart===null?0:now-stallStart)),buffer_ahead_seconds:Number(ahead.toFixed(3)),live_edge_distance_seconds:hls?.latency??null,programme_age_estimate_ms:playingDate?Math.round(Date.now()+clockOffset-playingDate.getTime()):null,clock_sync_rtt_uncertainty_ms:clockUncertainty,paused:video.paused,ended:video.ended,ready_state:video.readyState,last_fragment_download:downloads.at(-1)??null};}
function frame(now,metadata){if(firstFrame===null){firstFrame=now-started;video.dispatchEvent(new Event('firstverifiedframe'));}rendered++;if(!baseTime){baseTime=now;baseFrame=rendered;}if(now-baseTime>5000){baseTime=now;baseFrame=rendered;}frameCallback=video.requestVideoFrameCallback(frame);}
video.addEventListener('waiting',()=>{if(firstFrame!==null&&stallStart===null&&!video.paused){stallStart=performance.now();stalls++;}});
video.addEventListener('playing',()=>{if(stallStart!==null){stallMS+=performance.now()-stallStart;stallStart=null;}if(!video.requestVideoFrameCallback&&firstFrame===null)firstFrame=performance.now()-started;});
video.addEventListener('ended',()=>{if(stallStart!==null){stallMS+=performance.now()-stallStart;stallStart=null;}});
async function watch(){
 if(hls)hls.destroy();if(timer)clearInterval(timer);if(frameCallback!==undefined)video.cancelVideoFrameCallback?.(frameCallback);video.removeAttribute('src');video.load();
 liveID=$('liveid').value.trim()||$('streams').value;if(!/^l-[a-f0-9]{32}$/.test(liveID))throw new Error('Choisir un direct valide');
 started=performance.now();firstFrame=null;stallStart=null;stalls=0;stallMS=0;rendered=0;baseTime=0;samples=[];downloads=[];$('status').textContent='Attente du premier fragment vérifié…';
 for(let attempts=0;attempts<120;attempts++){const {state}=await json('/api/live/'+liveID);if(state.chunks.length)break;if(state.ended)throw new Error('Direct terminé sans fragment');if(attempts===119)throw new Error('Premier fragment indisponible après deux minutes');await new Promise(resolve=>setTimeout(resolve,1000));}
 const url='/api/live/'+liveID+'/index.m3u8';
 if(Hls.isSupported()){
  hls=new Hls({liveSyncDurationCount:1,liveMaxLatencyDurationCount:3,maxLiveSyncPlaybackRate:1.2,maxBufferLength:12,backBufferLength:8});hls.attachMedia(video);
  hls.on(Hls.Events.MEDIA_ATTACHED,()=>hls.loadSource(url));hls.on(Hls.Events.MANIFEST_PARSED,()=>video.play().catch(error));
  hls.on(Hls.Events.FRAG_LOADED,(_,d)=>{const stats=d.part?.stats||d.frag?.stats;const t=stats?.loading;if(t&&downloads.length<1024)downloads.push({sequence:d.frag.sn,bytes:stats.total,download_ms:t.end-t.start,ttfb_ms:t.first-t.start,server_timing:d.networkDetails?.getResponseHeader?.('Server-Timing')??null});});
  hls.on(Hls.Events.ERROR,(_,data)=>{if(data.fatal)error(new Error('Lecture interrompue : '+data.details));});
 }else if(video.canPlayType('application/vnd.apple.mpegurl')){video.src=url;await video.play();}else throw new Error('Navigateur sans support HLS/MSE');
 if(video.requestVideoFrameCallback)frameCallback=video.requestVideoFrameCallback(frame);
 timer=setInterval(()=>{const sample=snapshot();if(samples.length<3600)samples.push(sample);$('stats').textContent=JSON.stringify(sample,null,2);$('status').textContent=firstFrame===null?'Chargement et vérification…':'Lecture — fragments vérifiés SIS/IBS';},1000);
}
$('watch').onclick=()=>watch().catch(error);
$('export').onclick=()=>{const data={live_id:liveID,user_agent:navigator.userAgent,measurement:'browser playback, not camera-to-screen',programme_clock:'FFmpeg programme date-time, assumes publisher/server clocks agree',samples,downloads};const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([JSON.stringify(data,null,2)],{type:'application/json'}));a.download='live-'+liveID+'-browser.json';a.click();setTimeout(()=>URL.revokeObjectURL(a.href),5000);};
window.addEventListener('pagehide',()=>{hls?.destroy();clearInterval(timer);});
