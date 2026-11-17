// Production-page regression with isolated API fixtures. Build on the test cluster.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.CM_PLAYWRIGHT_MODULE || '../../e2e/node_modules/playwright');
const dist = path.resolve(import.meta.dirname, '../dist');
const server = http.createServer((request, response) => {
 let file=path.resolve(dist,'.'+new URL(request.url,'http://local').pathname);
 if(!file.startsWith(dist+path.sep)||!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');
 response.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(response);
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({headless:true,...(process.env.CM_CHROME_PATH?{executablePath:process.env.CM_CHROME_PATH}:{channel:'chrome'}),args:['--no-sandbox']});
try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}});page.on("pageerror",e=>console.log("PAGEERROR",e.message));page.on("console",m=>{if(m.type()==="error")console.log("CONSOLE",m.text())});let bodyReads=0;const cursors=[];let failNext=false;
 await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');localStorage.setItem('clawmanager_locale','en');window.WebSocket=class{static OPEN=1;readyState=1;close(){}send(){}}});
 await page.route('**/api/v1/**',async route=>{
 const url=new URL(route.request().url());let data={};
 if(url.pathname.endsWith('/auth/me'))data={id:1,username:'admin',role:'admin'};
 else if(url.pathname.endsWith('/admin/ai-audit/paged')){
 if(url.searchParams.has('body_kind')){bodyReads++;const text=JSON.stringify({id:1,request_payload:'COMPLETE_BODY_CONTENT',response_payload:'COMPLETE_RESPONSE_CONTENT',error_message:'COMPLETE_ERROR_DETAILS'});const offset=Number(url.searchParams.get('offset'));data={data:offset?text.slice(20):text.slice(0,20),next_offset:offset?0:20,total_bytes:text.length};}
 else {const cursor=url.searchParams.get('cursor')||'';cursors.push(cursor);if(cursor&&failNext){failNext=false;await route.fulfill({status:503,json:{error:'Fixture retry'}});return;}
 const id=cursor?201:1;data={trace_id:'paged',username:'admin',next_cursor:cursor?'':'next-snapshot',body_records:[{kind:'model_invocations',id}],invocations:[{id,trace_id:'paged',request_id:`req-${id}`,requested_model:'m',actual_provider_model:'m',provider_type:'openai',status:'completed',prompt_tokens:10,completion_tokens:5,total_tokens:15,created_at:'2026-11-17T12:00:00Z'}],audit_events:[],cost_records:[],risk_hits:[],messages:[],flow_nodes:[{id:`invocation-${id}`,kind:'model',title:'Model invocation',invocation_id:id,created_at:'2026-11-17T12:00:00Z'}]};}}
 else if(url.pathname.endsWith('/admin/ai-audit'))data={items:[],total:0,page:1,limit:20};
 await route.fulfill({json:{success:true,data}});
 });
 await page.goto(`http://127.0.0.1:${server.address().port}/admin/ai-audit?trace=paged`);
 bodyReads=0;await page.getByText('Trace metadata page 1',{exact:true}).waitFor().catch(async e=>{console.log("BODY",page.url(),cursors,await page.locator("body").innerText());throw e});assert.equal(bodyReads,0,'metadata page fetched bodies eagerly');
 await page.getByText('Read record bodies',{exact:true}).click();await page.getByRole('button',{name:'Read body',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('button:disabled')?.textContent?.includes('Read body'));await page.waitForTimeout(300);assert.equal(bodyReads,2,'body continuation was lost');await page.getByText('COMPLETE_BODY_CONTENT',{exact:true}).waitFor();await page.getByText('COMPLETE_RESPONSE_CONTENT',{exact:true}).waitFor();await page.getByText('COMPLETE_ERROR_DETAILS',{exact:true}).waitFor();
 failNext=true;await page.getByRole('button',{name:'Next',exact:true}).click();await page.waitForTimeout(300);await page.getByText('Trace metadata page 1',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Next',exact:true}).click();await page.getByText('Trace metadata page 2',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'Next',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'Prev',exact:true}).click();await page.getByText('Trace metadata page 1',{exact:true}).waitFor();assert.deepEqual(cursors.slice(-3),['next-snapshot','next-snapshot','']);
 await page.screenshot({path:process.env.CM_TRACE_SCREENSHOT||'/tmp/cm-trace-pagination-desktop.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile horizontal overflow');await page.screenshot({path:'/tmp/cm-trace-pagination-mobile.png',fullPage:true});
 console.log('PASS Trace metadata paging, lazy chunked bodies, next/previous, retry preserving current page, mobile overflow');
}finally{await browser.close();await new Promise(resolve=>{server.close(resolve);server.closeAllConnections()})}
