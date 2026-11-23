// A real Team file panel must render even when a read exceeds its 5s poll interval.
import assert from 'node:assert/strict';
import {chromium} from 'playwright';
const origin=process.env.OWNER_WORKBENCH_ORIGIN||'http://127.0.0.1:9178';
const browser=await chromium.launch({channel:'chrome',headless:true});
let release;
try {
 const page=await browser.newPage();
 const owner='slow-files@example.com',now=new Date().toISOString();
 const team={id:186,name:'Slow files fixture',status:'running',communication_mode:'leader_mediated',shared_mount_path:'/team',created_at:now,updated_at:now};
 let reads=0,started;
 const pending=new Promise(resolve=>{release=resolve});
 const initial=new Promise(resolve=>{started=resolve});
 await page.addInitScript(()=>localStorage.setItem('clawmanager_locale','zh'));
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  let data={};
  if(path.endsWith('/session')||path.endsWith('/session/refresh'))data={owner,expires_at:new Date(Date.now()+3600000).toISOString()};
  if(path.endsWith('/instances'))data={instances:[],total:0};
  if(path.endsWith('/team-capabilities'))data={enabled:true,can_create:true};
  if(path.endsWith('/teams'))data={teams:[{...team,members:[],member_count:0,available_count:0}],total:1};
  if(path.endsWith('/teams/186'))data={team,members:[],tasks:[],events:[],work_items:[]};
  if(path.endsWith('/tasks'))data={tasks:[],has_more:false};
  if(path.endsWith('/events'))data={events:[],has_more:false};
  if(path.endsWith('/workspace/files')){reads++;started();await pending;data={entries:[{name:'report.md',path:'report.md',is_dir:false,type:'file',size:10,modified_at:now}]};}
  await route.fulfill({json:{data}}).catch(()=>{});
 });
 await page.goto(origin+'/ieisystem/teams/186');
 await page.locator('.team-header-files-trigger').click();
 await initial;
 // Opening the panel activates its initially hidden browser and cancels that
 // hidden read. Measure only background polls after this visibility transition.
 await page.waitForTimeout(500);
 const initialReads=reads;
 await page.waitForTimeout(10500);
 assert.equal(reads,initialReads,'background refresh cancelled/restarted an unfinished file read');
 release();
 await page.locator('.team-file-row').getByText('report.md',{exact:true}).waitFor({timeout:5000});
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await page.waitForTimeout(500);
 assert.ok(reads>initialReads,'later refresh is still available');
 console.log('PASS slow Team files survive background polling; later focus still refreshes');
} finally {release?.();await browser.close();}
