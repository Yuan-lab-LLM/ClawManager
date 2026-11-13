// Real Router/Auth and native popover regression against the remote Vite source.
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
const require=createRequire(process.env.WORKSPACE_E2E_PACKAGE || new URL('../../e2e/package.json',import.meta.url));
const {chromium}=require('playwright');
const base=process.env.CM_UI_URL||'http://127.0.0.1:9438';
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--no-sandbox']});
try {
 const page=await browser.newPage({viewport:{width:1280,height:800}});
 const errors=[],writes=[];let listCalls=0;let empty=false;let fail=false;let stopped=false;let holdNext=false;let holdStarted;let releaseHold;
 page.on('pageerror',error=>errors.push(error.message));
 await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');localStorage.setItem('clawmanager_locale','en');});
 const instance={id:701,name:'Long authorized Agent '.repeat(8),user_id:71,type:'openclaw',instance_mode:'lite',runtime_type:'gateway',status:'running',os_type:'linux',os_version:'ubuntu',cpu_cores:2,memory_gb:4,disk_gb:10,gpu_enabled:false,gpu_count:0,pod_name:'gateway-pool-very-long-authorized-name',pod_namespace:'clawmanager-system',pod_ip:'10.0.0.1',created_at:'2026-11-12T00:00:00Z',updated_at:'2026-11-12T00:00:00Z',monitoring_since:'2026-11-12T00:00:00Z',auto_stop_disabled:false};
 await page.route('**/api/**',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname;
  let data=[];
  if(path.endsWith('/auth/me'))data={id:71,username:'Administrator',role:'admin',is_active:true};
  else if(path.endsWith('/version'))data={version:'fluent-fixture'};
  else if(path.endsWith('/auto-stop')){writes.push({path,method:request.method(),body:request.postDataJSON()});data={auto_stop_disabled:true,monitoring_since:instance.monitoring_since};}
  else if(path.endsWith('/stop')){stopped=true;data={};}
  else if(path.endsWith('/admin/instances')){listCalls++;if(fail)return route.fulfill({status:503,json:{error:'Fixture list unavailable'}});data={instances:empty?[]:[{...instance,status:stopped?'stopped':'running'},{...instance,id:702,name:'Second Agent'}],total:empty?0:2};if(holdNext){holdNext=false;holdStarted();await new Promise(resolve=>releaseHold=resolve);}}
  else if(path.endsWith('/users'))data={users:[{id:71,username:'Administrator',role:'admin'}],total:1};
  return route.fulfill({json:{success:true,data}});
 });
 await page.goto(base+'/admin/instances');
 const rows=page.locator('tbody tr');await rows.first().waitFor();
 const search=page.getByRole('button',{name:'Search',exact:true});
 await search.waitFor({timeout:5000});
 assert.equal(await page.getByRole('textbox').isVisible(),false,'overview search starts collapsed');
 await search.click();
 const field=page.getByRole('textbox',{name:'Search',exact:true});
 assert.equal(await field.evaluate(el=>el===document.activeElement),true,'expanded search receives focus');
 const focus=await field.evaluate(el=>{const input=getComputedStyle(el),field=getComputedStyle(el.closest('.cm-search-field'));return {inner:input.outlineStyle,outer:field.outlineColor,width:field.outlineWidth};});
 assert.equal(focus.inner,'none','shared composite search has only one focus indicator');assert.equal(focus.width,'2px');assert.notEqual(focus.outer,'rgb(0, 0, 0)');assert.notEqual(focus.outer,'rgb(36, 36, 36)');
 await field.fill('Second Agent');assert.equal(await rows.count(),1,'expanded search preserves filtering');
 await page.getByRole('button',{name:'Clear search',exact:true}).click();assert.equal(await rows.count(),2);
 await field.press('Escape');await page.waitForFunction(()=>document.activeElement?.getAttribute('aria-label')==='Search'&&document.activeElement?.tagName==='BUTTON');
 const previous=listCalls;
 const refreshed=page.waitForResponse(response=>new URL(response.url()).pathname.endsWith('/admin/instances'),{timeout:5000});
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));await refreshed;
 assert.ok(listCalls>previous,'stable lists refresh on focus without an overview refresh button');
 const trigger=rows.first().getByRole('button',{name:'Details',exact:true});
 await trigger.focus();await page.keyboard.press('Enter');
 const panel=page.locator('[popover]:popover-open');await panel.waitFor();
 assert.equal(await panel.getByRole('switch').count(),1,'auto-stop control remains reachable');
 assert.equal(await rows.first().evaluate(el=>el.getBoundingClientRect().height),44,'opening details does not expand the table row');
 const box=await panel.boundingBox();assert.ok(box.x>=0&&box.y>=0&&box.x+box.width<=1280&&box.y+box.height<=800,'popover stays inside viewport');
 assert.match(await panel.innerText(),/clawmanager-system/,'full binding information is available to keyboard and touch');
 await page.keyboard.press('Escape');assert.equal(await panel.count(),0);assert.equal(await trigger.evaluate(el=>el===document.activeElement),true,'Escape restores invoker focus');
 await trigger.click();await panel.getByRole('switch').click();await page.waitForFunction(()=>document.querySelector('[role=switch]')?.checked);
 assert.equal(writes.length,1);assert.equal(writes[0].body.auto_stop_disabled,true,'keep-running control retains exact API semantics');
 await page.keyboard.press('Escape');
 await page.setViewportSize({width:390,height:844});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'wide table scroll is local');
 await trigger.scrollIntoViewIfNeeded();await trigger.click();await panel.waitFor();
 const narrow=await panel.boundingBox();assert.ok(narrow.x>=0&&narrow.x+narrow.width<=390,'popover fits narrow viewport');
 await page.setViewportSize({width:1280,height:800});await panel.waitFor({state:'hidden'});
 const held=new Promise(resolve=>holdStarted=resolve);holdNext=true;
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));await held;
 const stop=rows.first().getByRole('button',{name:'Stop',exact:true});
 const stoppedResponse=page.waitForResponse(response=>new URL(response.url()).pathname.endsWith('/stop'));
 await stop.click();await stoppedResponse;
 await page.waitForFunction(()=>[...document.querySelectorAll('tbody tr:first-child button')].some(button=>button.textContent.trim()==='Stop'&&!button.disabled));
 releaseHold();await rows.first().getByText('Stopped',{exact:true}).waitFor({timeout:5000});
 empty=true;await page.evaluate(()=>window.dispatchEvent(new Event('online')));await rows.first().waitFor({state:'hidden'});
 fail=true;await page.reload();await page.getByRole('button',{name:'Retry',exact:true}).waitFor();
 fail=false;empty=false;await page.getByRole('button',{name:'Retry',exact:true}).click();await rows.first().waitFor();
 assert.deepEqual(errors,[]);
 console.log('PASS collapsed search/filtering, native auto-stop popover keyboard/focus, compact rows, unchanged policy request, viewport containment');
}finally{await browser.close();}
