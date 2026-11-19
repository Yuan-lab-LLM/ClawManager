// Real routed production pages with explicitly mocked API data and a moving clock.
import assert from 'node:assert/strict';
import fs from 'node:fs';import path from 'node:path';import http from 'node:http';import {createRequire} from 'node:module';
const require=createRequire(import.meta.url);const {chromium}=require(process.env.CM_PLAYWRIGHT_MODULE);
const dist=path.resolve(import.meta.dirname,'../dist');
const server=http.createServer((q,r)=>{let file=path.resolve(dist,'.'+new URL(q.url,'http://local').pathname);if(!file.startsWith(dist+path.sep)||!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');r.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(r)});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CM_CHROME_PATH,args:['--no-sandbox']});
try{
 for(const kind of ['admin','lite','pro']){
  const page=await browser.newPage({viewport:{width:1440,height:1000}});const requests=[];let failCursor=false,fail503=false,holdNext=false,releaseOld,holdCursor=false,releaseCursor;let downloads=0;page.on('download',()=>downloads++);const t0=Date.parse('2026-11-18T10:00:00Z');const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const instanceUsageRequests = [];
  page.on('request', request => {
   if (/\/instances\/9\/session-usage(?:\/|$)/.test(new URL(request.url()).pathname)) {
    instanceUsageRequests.push(request.url());
   }
  });
  await page.clock.install({time:new Date(t0)});
  await page.addInitScript(locale=>{localStorage.setItem('access_token','fixture');localStorage.setItem('clawmanager_locale',locale);localStorage.setItem('clawmanager.instance-panel.session-usage.9','false');window.WebSocket=class{static OPEN=1;readyState=1;close(){}send(){}}}, kind === 'pro' ? 'zh' : 'en');
  await page.route('**/api/v1/**',async route=>{const u=new URL(route.request().url());let data={};
   if(u.pathname.endsWith('/auth/me'))data={id:1,username:'admin',role:'admin'};
   else if(u.pathname.endsWith('/admin/session-usage/overview')){
    const query=Object.fromEntries(u.searchParams);requests.push(query);const fresh=Date.parse(query.until)>t0+20000;const summary={total_tokens:fresh?777:100,total_prompt_tokens:10,total_completion_tokens:5,total_estimated_cost:2,currency:'USD',session_count:11};const compliance={has_fallback_sessions:false,fallback_session_count:0,recent_fallback_audit_count:0};
    if(query.cursor&&holdCursor){holdCursor=false;await new Promise(resolve=>{releaseCursor=resolve});}
    if(query.cursor&&failCursor){await route.fulfill({status:409,json:{error:'Usage changed',data:{state:'cursor_stale'}}});return;}
    if(fail503){fail503=false;await route.fulfill({status:503,json:{error:'Fixture catchup in progress'}});return;}
    data={summary,compliance,total:61,items:[{instance_id:9,instance_name:fresh?'New facts':'Initial facts',instance_type:'openclaw',user_id:1,summary,compliance}],next_cursor:query.cursor?'page-three-chain':'frozen-chain'};
    if(holdNext){holdNext=false;data.items[0].instance_name='Stale response';await new Promise(resolve=>{releaseOld=resolve});}
   }else if(u.pathname.endsWith('/instances/9'))data={instance:{id:9,user_id:1,name:'Fixture instance',type:kind==='pro'?'hermes':'openclaw',status:'stopped',runtime_type:'docker',instance_mode:kind==='pro'?'pro':'lite',cpu_cores:1,memory_gb:1,disk_gb:1,os_type:'linux',os_version:'fixture'}};
   else if(u.pathname.endsWith('/instances/9/status'))data={instance_status:{id:9,status:'stopped',availability:'unavailable'}};
   else if(u.pathname.endsWith('/instances'))data={instances:[],items:[],total:0,limit:20};
   await route.fulfill({json:{success:true,data}});
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/${kind==='admin'?'admin/session-usage':'instances/9'}`);
  if (kind !== 'admin') {
   const workspace = page.locator(kind === 'pro' ? '[data-layout="pro-desktop-workspace"]' : '.wb-instance-section');
   await workspace.waitFor();
   await page.clock.fastForward(16000);
   for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({width, height:900});
    assert.ok(await workspace.isVisible(), `${kind} workspace is hidden at ${width}px`);
    assert.equal(await page.getByText(/Session Token Usage|会话 Token 用量/).count(), 0);
   }
   assert.deepEqual(instanceUsageRequests, []);
   assert.deepEqual(errors, []);
   await page.close();
   console.log(`PASS ${kind} detail has a workspace and no session token usage panel or requests`);
   continue;
  }
  await page.getByText('Initial facts',{exact:true}).waitFor().catch(async e=>{console.log(kind,page.url(),requests,errors,await page.locator('body').innerText());throw e});const initial=requests.at(-1);assert.ok(Math.abs(Date.parse(initial.until)-Date.parse(initial.since)-30*86400000)<1000);
  await page.clock.fastForward(21000);await page.getByText('Refresh',{exact:true}).click();await page.getByText('New facts',{exact:true}).waitFor({timeout:3000});assert.ok(Date.parse(requests.at(-1).until)>Date.parse(initial.until),'manual refresh kept stale upper bound');assert.equal(requests.at(-1).page,'1');assert.ok(!requests.at(-1).cursor);
  holdCursor=true;await page.getByRole('button',{name:'Next',exact:true}).click();await page.waitForTimeout(150);assert.ok(releaseCursor,'page two was not held');const heldCount=requests.length;
  if(await page.getByRole('button',{name:'Next',exact:true}).count())await page.getByRole('button',{name:'Next',exact:true}).evaluate(button=>button.click());await page.waitForTimeout(150);
  assert.equal(requests.length,heldCount,'double Next emitted a cursorless page before predecessor completed');
  if(await page.getByRole('button',{name:'Next',exact:true}).count())assert.equal(await page.getByRole('button',{name:'Next',exact:true}).isDisabled(),true,'Next remained enabled while predecessor pending');
  failCursor=true;releaseCursor();await page.getByText(/Usage changed during pagination/).waitFor();failCursor=false;
  assert.ok(requests.every(query=>Number(query.page||1)<=1||query.cursor),'non-first page bypassed population cursor');
  await page.getByText('Refresh',{exact:true}).click();await page.waitForTimeout(100);
  const bound=requests.at(-1).until;await page.getByRole('button',{name:'Next',exact:true}).click();await page.waitForTimeout(100);assert.equal(requests.at(-1).cursor,'frozen-chain');assert.equal(requests.at(-1).until,bound);
  await page.getByLabel('Auto refresh (15s)',{exact:true}).check();const n=requests.length;await page.clock.fastForward(16000);await page.waitForTimeout(100);assert.equal(requests.length,n,'older page auto-refresh changed context');
  await page.getByText('Refresh',{exact:true}).click();await page.waitForTimeout(100);assert.equal(requests.at(-1).page,'1');const refreshed=requests.at(-1).until;await page.clock.fastForward(16000);await page.waitForTimeout(100);assert.ok(requests.at(-1).until>refreshed,'first page auto-refresh did not move window');
  await page.getByLabel('Auto refresh (15s)',{exact:true}).uncheck();
  await page.getByRole('button',{name:'Next',exact:true}).click();await page.waitForTimeout(100);
  await page.locator('input:not([type])').fill('filtered');await page.clock.fastForward(350);await page.waitForTimeout(150);assert.equal(requests.at(-1).page,'1');assert.ok(!requests.at(-1).cursor);assert.equal(requests.at(-1).search,'filtered');
  const range=page.locator('select').filter({has:page.locator('option[value="90d"]')});await range.selectOption('7d');await page.waitForTimeout(100);assert.equal(Date.parse(requests.at(-1).until)-Date.parse(requests.at(-1).since),7*86400000);assert.ok(!requests.at(-1).cursor);
  holdNext=true;await page.getByText('Refresh',{exact:true}).click();await page.waitForTimeout(100);assert.ok(releaseOld);await page.clock.fastForward(2000);await page.getByText('Refresh',{exact:true}).click();await page.waitForTimeout(100);releaseOld();await page.waitForTimeout(100);assert.equal(await page.getByText('Stale response',{exact:true}).count(),0,'old in-flight result overwrote refresh');
  failCursor=true;await page.getByRole('button',{name:'Next',exact:true}).click();await page.getByText(/Usage changed during pagination/).waitFor();await page.getByRole('button',{name:'Export CSV',exact:true}).click();await page.waitForTimeout(200);assert.equal(downloads,0,'partial export downloaded after stale cursor');failCursor=false;
  await page.getByText('Refresh',{exact:true}).click();await page.waitForTimeout(100);assert.equal(requests.at(-1).page,'1');assert.ok(!requests.at(-1).cursor);
  fail503=true;await page.getByText('Refresh',{exact:true}).click();await page.getByText('Fixture catchup in progress',{exact:true}).waitFor();await page.getByText('Refresh',{exact:true}).click();await page.getByText('New facts',{exact:true}).waitFor();
  assert.deepEqual(errors,[]);await page.screenshot({path:`/tmp/cm-session-refresh-${kind}.png`,fullPage:true});await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'mobile overflow');await page.screenshot({path:`/tmp/cm-session-refresh-${kind}-mobile.png`,fullPage:true});await page.close();console.log(`PASS ${kind} actual route moving window, frozen next, older page preserved, filter resets, stale response fence, 409/export refusal, 503 retry, mobile`);
 }
}finally{await browser.close();await new Promise(resolve=>{server.close(resolve);server.closeAllConnections()})}
