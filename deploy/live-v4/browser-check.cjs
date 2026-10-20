// Actual browser HLS rendering while a real-time RTMP fixture is still active.
const { chromium }=require('playwright');
const { spawn,execFileSync }=require('node:child_process');
const fs=require('node:fs'),path=require('node:path');
const root=process.cwd(),results=path.join(root,'deploy/live-v4/results');fs.mkdirSync(results,{recursive:true});
const dc=['compose','--project-directory',path.join(root,'deploy/live-v4'),'-f',path.join(root,'deploy/live-v4/.runtime/compose.json')];
const delay=ms=>new Promise(r=>setTimeout(r,ms));
(async()=>{
 const browser=await chromium.launch({headless:true,args:['--autoplay-policy=no-user-gesture-required']});
 const page=await browser.newPage({viewport:{width:1200,height:900},acceptDownloads:true});let child,childExit=null,id=null,sourceEndedAt=null,log='';const errors=[];let resourceTimer,resourceBusy=false;const resourceSamples=[];
 const collectResources=()=>{if(resourceBusy)return;resourceBusy=true;try{const ids=[...new Set([...execFileSync('docker',['ps','-q','--filter','label=com.docker.compose.project=caribou-live-v4'],{encoding:'utf8'}).trim().split(/\s+/),...execFileSync('docker',['ps','-q','--filter','label=caribou.project=caribou-live-v4'],{encoding:'utf8'}).trim().split(/\s+/)])].filter(Boolean);require('node:child_process').execFile('docker',['stats','--no-stream','--format','{{json .}}',...ids],{timeout:15000},(error,stdout)=>{resourceBusy=false;if(!error&&resourceSamples.length<120){resourceSamples.push({utc:new Date().toISOString(),containers:stdout.trim().split('\n').filter(Boolean).map(line=>JSON.parse(line))});fs.writeFileSync(path.join(results,'docker-resources.json'),JSON.stringify({measurement:'Docker CPU/RAM/network/block IO during live test',samples:resourceSamples},null,2));}});}catch{resourceBusy=false;}};
 page.on('pageerror',e=>errors.push(e.message));
 try{
  await page.goto('http://127.0.0.1:20690/live/');await page.fill('#email','secure-owner-a@example.invalid');await page.fill('#password','OnlyForLocalTest!2026');await page.click('#login button');await page.waitForSelector('#viewer:not([hidden])');
  execFileSync('docker',[...dc,'cp',path.join(root,'deploy/live-v4/live-check.py'),'owner-a:/tmp/live-check.py'],{stdio:'pipe'});
  collectResources();resourceTimer=setInterval(collectResources,3000);
  child=spawn('docker',[...dc,'exec','-T','-e','PYTHONPATH=/app/vendor-video','owner-a','python','-u','/tmp/live-check.py'],{stdio:['ignore','pipe','pipe']});
  const append=raw=>{const text=raw.toString();log+=text;fs.appendFileSync(path.join(results,'live-publisher.log'),text);for(const line of log.split('\n')){try{if(line.startsWith('SOURCE_FINISHED ')){sourceEndedAt=JSON.parse(line.slice(16)).source_end_unix_ms;continue;}const value=JSON.parse(line);if(value.state==='CREATED'&&value.live_id)id=value.live_id;}catch{}}};
  child.stdout.on('data',append);child.stderr.on('data',append);child.on('error',e=>{errors.push(e.message);childExit=-1;});child.on('exit',code=>childExit=code);
  for(let i=0;i<180&&!id&&childExit===null;i++)await delay(500);
  if(!id)throw new Error('Live fixture did not create a session: '+log.slice(-1200));
  await page.fill('#liveid',id);await page.click('#watch');
  await page.waitForFunction(()=>{try{const s=JSON.parse(document.getElementById('stats').textContent);return s.first_display_ms!==null&&s.rendered_frames>10;}catch{return false;}},{},{timeout:120000});
  const firstObservedAt=Date.now();const first=JSON.parse(await page.locator('#stats').textContent());const r=await page.request.get('http://127.0.0.1:20690/api/live/'+id);const state=(await r.json()).state;
  if(state.ended)throw new Error('Browser only rendered after live ended');
  if(errors.length)throw new Error('Browser errors: '+errors.join('; '));
  await page.screenshot({path:path.join(results,'live-browser.png')});
  for(let i=0;i<600&&childExit===null;i++)await delay(500);
  if(childExit!==0)throw new Error('Live producer integration failed/timeout: '+log.slice(-2000));
  if(!sourceEndedAt||firstObservedAt>=sourceEndedAt)throw new Error('Browser did not render while RTMP input was still active');
  await page.waitForFunction(()=>document.getElementById('video').ended,{},{timeout:90000});await delay(1100);
  const downloadPromise=page.waitForEvent('download');await page.click('#export');const download=await downloadPromise;await download.saveAs(path.join(results,'browser-metrics.json'));
  const metrics=JSON.parse(fs.readFileSync(path.join(results,'browser-metrics.json'),'utf8'));
  if(!metrics.samples.some(s=>s.rendered_frames>10)||!metrics.downloads.length)throw new Error('No actual browser frame/download measurements');
  const report={passed:true,live_id:id,first_frames_before_end:true,browser_observation_unix_ms:firstObservedAt,rtmp_source_end_unix_ms:sourceEndedAt,browser:'Playwright Chromium headless AMD64',first_snapshot:first,download_count:metrics.downloads.length,page_errors:errors,glass_to_glass:'NOT_MEASURED; programme time only',physical_lab:'NOT_TESTED'};
  fs.writeFileSync(path.join(results,'browser-verification.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
 }finally{clearInterval(resourceTimer);await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
