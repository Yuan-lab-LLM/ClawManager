// Real hook regression: a slow read must survive the next polling/focus event.
import assert from 'node:assert/strict';
import {chromium} from 'playwright';
const origin=process.env.OWNER_WORKBENCH_ORIGIN||'http://127.0.0.1:9178';
const browser=await chromium.launch({channel:'chrome',headless:true});
let release;
try {
 const page=await browser.newPage();
 let reads=0,finish;
 const pending=new Promise(resolve=>{finish=resolve});
 release=finish;
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  let data={};
  if(path.endsWith('/session')||path.endsWith('/session/refresh'))data={owner:'slow-read@example.com',expires_at:new Date(Date.now()+3600000).toISOString()};
  if(path.endsWith('/instances'))data={instances:[],total:0};
  if(path.endsWith('/team-capabilities'))data={enabled:true,can_create:true};
  if(path.endsWith('/dashboard'))data={summary:{questions:0,total_tokens:0,sessions:0},last_activity:{},trend:[]};
  if(path.endsWith('/growth'))data={xp:0,level:1,tasks:[],ledger:[],days:[]};
  if(path.endsWith('/reports'))data={start:'2026-11-16',end:'2026-11-22',ongoing:true,counts:{tokens:0,sessions:0},active_days:0,previous:{},runtime:[],days:[]};
  if(path.endsWith('/teams')){reads++;await pending;data={teams:[],total:7};}
  await route.fulfill({json:{data}}).catch(()=>{});
 });
 await page.goto(origin+'/ieisystem/list-instances');
 await page.getByRole('tab',{name:'Team · 0',exact:true}).waitFor();
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await page.waitForTimeout(10500);
 assert.equal(reads,1,'polling/focus overlapped an unfinished Team read');
 finish();
 await page.getByRole('tab',{name:'Team · 7',exact:true}).waitFor({timeout:5000});
 await page.evaluate(()=>window.dispatchEvent(new Event('online')));
 await page.waitForTimeout(500);
 assert.equal(reads,2,'completed reads must allow later refreshes');
 console.log('PASS slow Team response survives polling/focus; later refresh remains available');
} finally {release?.();await browser.close();}
