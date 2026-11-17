// Production-page audit list interactions against declared isolated API fixtures.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url);
const {chromium}=require(process.env.CM_PLAYWRIGHT_MODULE||'../../e2e/node_modules/playwright');
const dist=path.resolve(import.meta.dirname,'../dist');
const server=http.createServer((request,response)=>{let file=path.resolve(dist,'.'+new URL(request.url,'http://local').pathname);if(!file.startsWith(dist+path.sep)||!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');response.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(response);});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({headless:true,...(process.env.CM_CHROME_PATH?{executablePath:process.env.CM_CHROME_PATH}:{channel:'chrome'}),args:['--no-sandbox']});
try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}});const requests=[];const pageErrors=[];page.on('pageerror',e=>pageErrors.push(e.message));let exports=0;
 const items=Array.from({length:25},(_,index)=>{const id=25-index;return {trace_id:`trace-${String(id).padStart(3,'0')}`,request_id:`request-${id}`,username:'fixture-admin',requested_model:'auto',actual_provider_model:id===3?'special-model':'model',provider_type:'openai',status:id===3?'failed':'completed',prompt_tokens:10,completion_tokens:5,total_tokens:15,created_at:new Date(Date.now()-3600000).toISOString()}});
 await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');localStorage.setItem('clawmanager_locale','en');window.WebSocket=class{static OPEN=1;readyState=1;close(){}send(){}}});
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url());let data={};
  if(url.pathname.endsWith('/auth/me'))data={id:1,username:'fixture-admin',role:'admin'};
  else if(url.pathname.endsWith('/admin/ai-audit')){
   const query=Object.fromEntries(url.searchParams);requests.push(query);
   const filtered=items.filter(item=>(!query.search||item.trace_id.includes(query.search)||item.username.includes(query.search))&&(!query.status||item.status===query.status)&&(!query.model||item.actual_provider_model.includes(query.model)));
   const offset=Number(query.cursor?.replace('offset-','')||0),limit=Number(query.limit||20);
   data={items:filtered.slice(offset,offset+limit),total:filtered.length,page:Number(query.page||1),limit,since:query.since,until:query.until,next_cursor:offset+limit<filtered.length?`offset-${offset+limit}`:''};
  }else if(url.pathname.endsWith('/admin/history/model_invocations/export')){exports++;await route.fulfill({headers:{'content-type':'application/x-ndjson','x-history-has-more':'false'},body:'{"kind":"model_invocations","id":1}\n'});return;}
  await route.fulfill({json:{success:true,data}});
 });
 await page.goto(`http://127.0.0.1:${server.address().port}/admin/ai-audit`);
 await page.getByText('trace-025',{exact:true}).waitFor();assert.equal(requests.length,1);assert.equal(Date.parse(requests[0].until)-Date.parse(requests[0].since),30*86400000);
 await page.getByRole('button',{name:'Next',exact:true}).click();await page.getByText('trace-005',{exact:true}).waitFor();assert.equal(requests[1].cursor,'offset-20');assert.equal(requests[1].since,requests[0].since);assert.equal(requests[1].until,requests[0].until);assert.equal(await page.getByRole('button',{name:'Next',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'Prev',exact:true}).click();await page.getByText('trace-025',{exact:true}).waitFor();assert.equal(requests.at(-1).cursor,undefined);
 await page.locator('input[type=text]').first().fill('trace-003');
 await page.getByRole('combobox',{name:'All statuses'}).selectOption('failed');await page.locator('input[type=text]').nth(1).fill('special');
 await page.getByRole('button',{name:'Apply filters',exact:true}).click();await page.getByText('trace-003',{exact:true}).waitFor();assert.equal(requests.at(-1).cursor,undefined);assert.equal(requests.at(-1).search,'trace-003');assert.equal(requests.at(-1).status,'failed');assert.equal(requests.at(-1).model,'special');assert.equal(await page.locator('tbody tr').count(),1);
 const beforeInvalid=requests.length;await page.locator('input[type=datetime-local]').first().fill('2020-01-01T00:00');await page.getByRole('button',{name:'Apply filters',exact:true}).click();await page.getByRole('alert').filter({hasText:'at most 90 days'}).waitFor();assert.equal(requests.length,beforeInvalid,'invalid window sent network request');
 await page.getByText('Older history and archive export',{exact:true}).click();const download=page.waitForEvent('download');await page.getByRole('button',{name:'Download first page',exact:true}).click();await download;assert.equal(exports,1);
 await page.screenshot({path:process.env.CM_AUDIT_SCREENSHOT||'/tmp/cm-audit-list-desktop.png',fullPage:true});await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile horizontal overflow');await page.screenshot({path:'/tmp/cm-audit-list-mobile.png',fullPage:true});assert.deepEqual(pageErrors,[]);
 console.log('PASS audit list default30d, frozen cursor next/previous, combined search/status/model, invalid90d window, archive download, desktop/mobile; API fixtures declared');
}finally{await browser.close();await new Promise(resolve=>{server.close(resolve);server.closeAllConnections()})}
