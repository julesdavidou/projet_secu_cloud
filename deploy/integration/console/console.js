const status = document.querySelector('#status');
async function request(path, options={}) { const response=await fetch(path,{credentials:'same-origin',...options}); const text=await response.text(); let data; try{data=JSON.parse(text)}catch{data=text} if(!response.ok)throw new Error(response.status+' '+JSON.stringify(data));return data; }
async function run(action){status.textContent='Opération en cours…';try{status.textContent=JSON.stringify(await action(),null,2)}catch(error){status.textContent=error.message}}
document.querySelector('#login').onsubmit=event=>{event.preventDefault();run(()=>request('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams(new FormData(event.target))}));};
document.querySelector('#logout').onclick=()=>run(()=>request('/api/auth/logout',{method:'POST'}));
document.querySelector('#upload').onsubmit=event=>{event.preventDefault();const form=new FormData(event.target);run(()=>request('/api/videos?'+new URLSearchParams({title:form.get('title'),public:form.has('public')?'true':'false'}),{method:'POST',headers:{'Content-Type':'video/mp4'},body:form.get('video')}));};
document.querySelector('#diag').onclick=()=>run(async()=>({videos:await request('/api/videos'),nodes:await request('/api/nodes'),capabilities:await request('/api/capabilities')}));
