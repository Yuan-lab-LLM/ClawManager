// Mocked production-page regression; run npm run build before this script.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';
const { chromium } = createRequire(new URL('../../e2e/package.json', import.meta.url))('playwright');
const dist = path.resolve(import.meta.dirname, '../dist');
const server = http.createServer((request, response) => {
  let file = path.resolve(dist, '.' + new URL(request.url, 'http://local').pathname);
  if (!file.startsWith(dist + path.sep) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
  response.setHeader('Content-Type', ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[path.extname(file)] || 'application/octet-stream');
  fs.createReadStream(file).pipe(response);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const page = await browser.newPage({viewport:{width:1500,height:900}});
  let state='running', calls=0, teamCalls=0, releaseOld, fail=false, hold=false, releaseHold;
  let heldResponse=Promise.resolve();
  const oldResponse=new Promise(resolve=>releaseOld=resolve);
  await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');localStorage.setItem('clawmanager_locale','en');});
  await page.route('**/api/v1/**',async route=>{
    const u=new URL(route.request().url());let data={};
    if(u.pathname.endsWith('/auth/me'))data={id:1,username:'admin',role:'admin'};
    else if(u.pathname.endsWith('/instances')){
      calls++;if(hold)await heldResponse;
      if(fail){await route.fulfill({status:503,json:{error:'temporary test outage'}}).catch(()=>{});return;}
      const q=u.searchParams.get('q')||u.searchParams.get('query')||'';
      if(q==='older')await oldResponse;
      const instances=u.searchParams.get('status')==='running'&&state!=='running'?[]:[{id:229,user_id:1,name:q||'lite-hermes-030',type:'hermes',instance_mode:'lite',runtime_type:'gateway',status:state,stopped_reason:state==='stopped'?'idle_timeout':undefined,created_at:'2026-11-10T09:27:40Z'}];
      data={instances,total:instances.length,page:1,page_size:100};
    } else if(u.pathname.endsWith('/teams')){teamCalls++;data={teams:[],total:0};}
    else if(u.pathname.endsWith('/system-settings/images'))data={images:[]};
    else data={items:[],batches:[]};
    await route.fulfill({json:{success:true,data}}).catch(()=>{});
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/instances`);
  const row=()=>page.locator('tbody tr').filter({hasText:'lite-hermes-030'});
  await row().getByText('Running',{exact:true}).waitFor();
  await row().getByRole('checkbox').check();const teamsBefore=teamCalls;
  state='stopped';await row().getByText('Auto Sleep',{exact:true}).waitFor({timeout:7500});
  assert.equal(await row().getByRole('checkbox').isChecked(),true,'refresh lost checked row');
  assert.equal(teamCalls,teamsBefore,'status polling refreshed team inventory');
  state='running';await page.goto(`http://127.0.0.1:${server.address().port}/instances?status=running`);
  await row().getByText('Running',{exact:true}).waitFor();
  await page.evaluate(()=>{Object.defineProperty(document,'visibilityState',{configurable:true,get:()=> 'hidden'});document.dispatchEvent(new Event('visibilitychange'));});
  state='stopped';await row().waitFor({state:'detached',timeout:7500});
  state='running';await row().getByText('Running',{exact:true}).waitFor({timeout:7500});
  await page.evaluate(()=>{Object.defineProperty(document,'visibilityState',{configurable:true,get:()=> 'visible'});document.dispatchEvent(new Event('visibilitychange'));});
  // A window focus is distinct from document visibility when windows are tiled.
  await page.waitForTimeout(500);state='stopped';await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
  await row().waitFor({state:'detached',timeout:1800});
  console.log('PASS sleep/start synchronization, background and empty filtered lists, focus, selection retention, no team polling');
  state='running';await page.goto(`http://127.0.0.1:${server.address().port}/instances`);
  await row().waitFor();const input=page.getByPlaceholder(/ID \/ Search/);
  await input.fill('older');await page.waitForRequest(r=>new URL(r.url()).pathname.endsWith('/instances')&&(new URL(r.url()).searchParams.get('q')||new URL(r.url()).searchParams.get('query'))==='older');
  await input.fill('newer');await page.locator('tbody tr').filter({hasText:'newer'}).waitFor();releaseOld();await page.waitForTimeout(500);
  assert.equal(await page.locator('tbody tr').filter({hasText:'older'}).count(),0,'old response replaced newer filter');
  console.log('PASS late response cannot overwrite current filters');
  await input.fill('');await row().waitFor();await page.waitForTimeout(200);
  heldResponse=new Promise(resolve=>releaseHold=resolve);hold=true;const beforeHeld=calls;
  await page.evaluate(()=>{for(let i=0;i<8;i++)window.dispatchEvent(new Event('focus'));});
  await page.waitForTimeout(300);assert.equal(calls,beforeHeld+1,'focus events duplicated an in-flight request');
  hold=false;releaseHold();await page.waitForTimeout(300);
  fail=true;await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
  await page.getByRole('status').filter({hasText:'Failed to load instances'}).waitFor({timeout:7000});
  assert.equal(await row().isVisible(),true,'transient error hid last successful rows');
  await page.evaluate(()=>window.dispatchEvent(new Event('offline')));
  fail=false;state='stopped';await page.evaluate(()=>window.dispatchEvent(new Event('online')));
  await row().getByText('Auto Sleep',{exact:true}).waitFor({timeout:2500});
  console.log('PASS request deduplication, retained rows during failure and reconnect recovery');
} finally {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await browser.close();
}

