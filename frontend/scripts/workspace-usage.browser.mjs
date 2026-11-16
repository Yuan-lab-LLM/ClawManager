import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {createRequire} from 'node:module';
const {chromium}=createRequire(new URL('../../e2e/package.json',import.meta.url))('playwright');
const dist=path.resolve(import.meta.dirname,'../dist');
const output=path.resolve(import.meta.dirname,'../../workspace-usage-browser');fs.mkdirSync(output,{recursive:true});
const server=http.createServer((request,response)=>{
 let file=path.resolve(dist,'.'+new URL(request.url,'http://local').pathname);
 if(!file.startsWith(dist+path.sep)||!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');
 response.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(response);
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({executablePath:'/opt/google/chrome/chrome',headless:true,args:['--no-sandbox']});
const instances=[
 {id:701,name:'No measurement',workspace_usage_bytes:0,workspace_usage_observed_at:null},
 {id:702,name:'Measured empty',workspace_usage_bytes:0,workspace_usage_observed_at:'2026-11-12T00:00:00Z'},
 {id:703,name:'Last successful sample',workspace_usage_bytes:2048,workspace_usage_observed_at:'2025-01-01T00:00:00Z'},
].map(i=>({...i,user_id:1,type:'openclaw',instance_mode:'lite',runtime_type:'gateway',status:'stopped',created_at:'2026-11-01T00:00:00Z',updated_at:'2026-11-01T00:00:00Z'}));
try{
 for(const locale of ['en','zh','ja','ko','de']){
  const page=await browser.newPage();
  const pageErrors=[];
  page.on("pageerror",error=>pageErrors.push(error.message));
  await page.addInitScript(locale=>{localStorage.setItem('access_token','fixture');localStorage.setItem('clawmanager_locale',locale);},locale);
  await page.route('**/api/v1/**',async route=>{
   const p=new URL(route.request().url()).pathname;let data={};
   if(p.endsWith('/auth/me'))data={id:1,username:'fixture',role:'user'};
   else if(p.endsWith('/instances'))data={instances,total:3,page:1,page_size:100};
   else if(/\/instances\/70[123]$/.test(p))data={instance:instances.find(i=>p.endsWith('/'+i.id))};
   else if(/\/instances\/70[123]\/status$/.test(p)){const i=instances.find(i=>p.includes('/'+i.id+'/'));data={instance_status:{...i,instance_id:i.id,availability:'unavailable'}};}
   else if(p.endsWith('/workspace/files'))data={entries:[]};
   else if(p.endsWith('/instances/batch/lifecycle'))data=[];
   else if(p.endsWith('/session-usage'))data={summary:{total_prompt_tokens:0,total_completion_tokens:0,total_tokens:0,total_estimated_cost:0,currency:'USD',session_count:0},compliance:{fallback_session_count:0,has_fallback_sessions:false,recent_fallback_audit_count:0},items:[],total:0,page:1,limit:10};
   else if(p.endsWith('/skills')||p.endsWith('/skill-hub/tags'))data=[];
   else if(p.endsWith('/teams'))data={teams:[],total:0};
   else if(p.endsWith('/system-settings/images'))data={images:[]};
   else if(p.includes('/commands'))data={commands:[],total:0};
   else data={items:[],batches:[]};
   await route.fulfill({json:{success:true,data}}).catch(()=>{});
  });
  for(const width of (locale==='en'||locale==='zh'?[1440,1280,768,390]:[390])){
   await page.setViewportSize({width,height:900});
   await page.goto(`http://127.0.0.1:${server.address().port}/instances`);
   const row=name=>page.locator('tbody tr').filter({hasText:name});
   const unavailable=locale==='zh'?'不可用':'Unavailable';
   await row('No measurement').getByRole('cell',{name:unavailable,exact:true}).waitFor();
   await row('Measured empty').getByRole('cell',{name:/^0 B ·/}).waitFor();
   await row('Last successful sample').getByRole('cell',{name:/^2.0 KB ·/}).waitFor();
   assert.ok(await row('Last successful sample').getByRole('cell',{name:/^2.0 KB ·/}).getAttribute('title'));
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'page overflow at '+locale+width);
   if(width===390)await row('No measurement').getByRole('cell',{name:unavailable,exact:true}).scrollIntoViewIfNeeded();
   await page.screenshot({path:path.join(output,`list-${locale}-${width}.png`),fullPage:true});
  }
  if(locale==='en'||locale==='zh'){
   await page.setViewportSize({width:1440,height:900});
   await page.goto(`http://127.0.0.1:${server.address().port}/instances/701`);
   await page.locator('h1').filter({hasText:'No measurement'}).waitFor({timeout:5000});
   await page.locator('span[title]').filter({hasText:locale==='zh'?'不可用':'Unavailable'}).first().waitFor();
   await page.screenshot({path:path.join(output,`detail-${locale}-1440.png`),fullPage:true});
   for(const [id,prefix] of [[702,'0 B ·'],[703,'2.0 KB ·']]){
    await page.goto(`http://127.0.0.1:${server.address().port}/instances/${id}`);
    await page.locator('span[title]').filter({hasText:prefix.split(' ·')[0]}).first().waitFor({timeout:5000});
   }
  }
  assert.deepEqual(pageErrors,[],`uncaught page errors for ${locale}`);
  await page.close();
 }
 console.log('PASS real instance list missing/empty/prior sample semantics; EN/ZH 1440/1280/768/390; JA/KO/DE 390; detail missing/empty/prior sample EN/ZH; no page overflow');
}finally{await browser.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve));}
