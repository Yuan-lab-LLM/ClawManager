import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { createServer } from '../node_modules/vite/dist/node/index.js';
const require=createRequire(process.env.WORKSPACE_E2E_PACKAGE || '/root/cm-uiux08-20261111/source-e737ca5040e637f271ea5f394ca35aeac985ab98/e2e/package.json');
const {chromium}=require('playwright');
const root=path.resolve(import.meta.dirname,'..');
const out=process.env.INSTANCE_LAYOUT_SCREENSHOTS || '/tmp/cm-instance-layout-browser';
await fs.mkdir(out,{recursive:true});
let vite,browser;const errors=[],writes=[];
let frameFailure=false,state='running',mode='lite',skillsError=false,releaseSync,syncPosts=0,installable=false,releaseInstall,instanceName='Workspace layout fixture',renameError=false,holdMeta=false,releaseMeta;
let releaseDestination;let holdInventory=false;const releaseInventory=[];
let removedCurrent=false,metaFailure=0,holdStatus=false;const pendingStatuses=[];const deletionRequests=[];const batchDeletionRequests=[];
 const renames=[];let listError=false,accountID=71,listCount=55,metaDelay=0,metaCalls=0;
const names={en:'Runtime management',zh:'运行管理',ja:'実行管理',ko:'실행 관리',de:'Laufzeitverwaltung'};
try {
 vite=await createServer({root,server:{port:0,host:'127.0.0.1',hmr:false},logLevel:'error'});await vite.listen();
 browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH || '/opt/google/chrome/chrome',args:['--no-sandbox']});
 const page=await browser.newPage({viewport:{width:1440,height:900}});page.setDefaultTimeout(10000);
 page.on('pageerror',e=>errors.push(e.message));page.on('console',message=>{if(message.text().includes('Error caught by boundary'))errors.push(message.text());});
 await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');if(!localStorage.getItem('clawmanager_locale'))localStorage.setItem('clawmanager_locale','en');});
 await page.routeWebSocket('**/*',socket=>socket.close());
 await page.route('**/api/**',async route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname;
  if(p==='/api/v1/layout-frame'&&frameFailure)return route.fulfill({status:503,contentType:'text/html',body:'Agent service temporarily unavailable'});
  if(p==='/api/v1/instances/37/status'&&holdStatus)await new Promise(resolve=>pendingStatuses.push(resolve));
  if(p==='/api/v1/instances/batch/delete'&&r.method()==='POST'){batchDeletionRequests.push(r.postDataJSON());return route.fulfill({status:207,json:{success:true,data:{requested:1,deleted:0,failed:1,results:[{instance_id:37,name:instanceName,status:'failed',error:'Instance lifecycle operation is already in progress'}]}}});}
  if(p==='/api/v1/instances/37'&&r.method()==='DELETE'){deletionRequests.push(37);removedCurrent=true;return route.fulfill({status:202,json:{success:true,data:null}});}
  if(p==='/api/v1/instances/37'&&r.method()==='GET'&&metaFailure)return route.fulfill({status:metaFailure,json:{error:'Metadata temporarily unavailable'}});
  if(p==='/api/v1/instances/37'&&r.method()==='GET'&&removedCurrent)return route.fulfill({status:404,json:{error:'Instance not found'}});
  if(p==='/api/v1/instances/37'&&r.method()==='PUT'){renames.push(r.postDataJSON());if(renameError)return route.fulfill({status:503,json:{error:'Rename temporarily unavailable'}});instanceName=r.postDataJSON().name;return route.fulfill({json:{success:true,data:null}});}
  if(p.endsWith('/access')&&r.method()==='POST')return route.fulfill({json:{success:true,data:{token:'frame-fixture',access_url:'/api/v1/layout-frame',proxy_url:'/api/v1/layout-frame',expires_at:'2099-01-01T00:00:00Z',browser_origin_mode:'chrome-single-host',manager_origin:new URL(vite.resolvedUrls.local[0]).origin}}});
  if(p==='/api/v1/instances/37/skills'&&r.method()==='POST'){await new Promise(resolve=>{releaseInstall=resolve;});return route.fulfill({status:503,json:{error:'Fixture install failed while closed'}});}
  if(p.endsWith('/skills/sync')&&r.method()==='POST'){syncPosts++;await new Promise(resolve=>{releaseSync=resolve;});return route.fulfill({status:503,json:{error:'Fixture sync failed after reopen'}});}
  if(r.method()!=='GET'){writes.push(p);return route.fulfill({json:{success:true,data:{}}});}
  let data=[];
  const instance={id:37,name:instanceName,type:'openclaw',instance_mode:mode,runtime_type:'gateway',user_id:71,status:state,stopped_reason:state==='stopped'?'manual':null,runtime_generation:1,cpu_cores:2,memory_gb:4,disk_gb:10,gpu_enabled:false,gpu_count:0,os_type:'linux',os_version:'ubuntu',storage_class:'standard',mount_path:'/workspace',workspace_path:'/workspace',workspace_usage_bytes:4000000,workspace_usage_observed_at:'2026-11-12T00:00:00Z',created_at:'2026-11-01T00:00:00Z',updated_at:'2026-11-12T00:00:00Z'};
  if(p.endsWith('/auth/me'))data={id:accountID,username:'Fixture',role:'admin',is_active:true};
  else if(p.endsWith('/version'))data={version:'fixture',commit:'fixture',build_time:'fixture'};
  else if(p==='/api/v1/instances/37'){metaCalls++;data={instance};if(metaDelay)await new Promise(resolve=>setTimeout(resolve,metaDelay));if(holdMeta){holdMeta=false;await new Promise(resolve=>{releaseMeta=resolve;});}}
  else if(p==='/api/v1/instances/38')data={instance:{...instance,id:38,name:accountID===72?'Account 72 current':'Another Agent'}};
  else if(/^\/api\/v1\/instances\/1\d\d$/.test(p)){const targetId=Number(p.split('/').at(-1));data={instance:{...instance,id:targetId,name:'Agent '+targetId,...(targetId===100?{status:'error'}:targetId===101?{status:'stopped',stopped_reason:'manual'}:targetId===102?{status:'stopped',stopped_reason:'idle_timeout'}:{})}};if(targetId===105&&releaseDestination===true)await new Promise(resolve=>{releaseDestination=resolve;});}
  else if(p==='/api/v1/instances'){if(holdInventory)await new Promise(resolve=>releaseInventory.push(resolve));if(listError)return route.fulfill({status:503,json:{error:'List unavailable'}});const rows=accountID===72?[{...instance,id:500,name:'Private Agent 72'}]:[{...instance,id:38,name:'Another Agent'},instance,...Array.from({length:53},(_,i)=>({...instance,id:i+100,name:'Agent '+(i+100),...(i===0?{status:'error'}:i===1?{status:'stopped',stopped_reason:'manual'}:i===2?{status:'stopped',stopped_reason:'idle_timeout'}:{})}))];const found=rows.slice(0,listCount).filter(row=>!removedCurrent||row.id!==37).filter(row=>row.name.toLowerCase().includes((u.searchParams.get('query')||'').toLowerCase()));const pageNumber=Number(u.searchParams.get('page')||1),limit=Number(u.searchParams.get('limit')||50);data={instances:found.slice((pageNumber-1)*limit,pageNumber*limit),total:found.length,page:pageNumber,limit};}
  else if(p.endsWith('/status')){const targetId=Number(p.split('/').at(-2));const targetState=targetId===100?'error':[101,102].includes(targetId)?'stopped':state;data={instance_status:{...instance,instance_id:targetId,status:targetState,availability:targetState==='running'?'available':'unavailable'}};}
  else if(p.endsWith('/runtime'))data={commands:[],skills:[]};
  else if(p.endsWith('/external-access'))data={external_access:null};
  else if(p.endsWith('/workbench/capabilities'))data={generation:1,git:true,terminal:true,preview:true,processes:false,browser_origin_mode:'chrome-single-host',manager_origin:new URL(vite.resolvedUrls.local[0]).origin};
  else if(p.endsWith('/workspace/files'))data={entries:[]};
  else if(p.endsWith('/desktop-access'))data={access_url:'/api/v1/layout-frame',proxy_url:'/api/v1/layout-frame'};
  else if(p.endsWith('/layout-frame'))return route.fulfill({contentType:'text/html',body:'<html><body>Agent conversation fixture<textarea aria-label="Conversation draft"></textarea></body></html>'});
  else if(p.endsWith('/environment-overrides'))data={names:[]};
  else if(p.endsWith('/skill-hub/catalog'))data={items:installable?[{id:901,name:'Fixture installable skill',status:'active',visibility:'public',risk_level:'low'}]:[],total:installable?1:0};
  else if(p.endsWith('/skills')&&skillsError)return route.fulfill({status:503,json:{error:'Skills temporarily unavailable'}});
  return route.fulfill({json:{success:true,data}});
 });
 const url=vite.resolvedUrls.local[0]+'instances/37';
 const ready=async()=>{await page.goto(url);await page.getByRole('heading',{name:instanceName,exact:true}).waitFor().catch(async error=>{console.log('Instance readiness:',page.url(),errors,(await page.locator('body').innerText()).slice(0,3000));throw error;});};
 await ready();
 // Lifecycle-independent native metadata must never reload the outer manager.
 await page.locator('iframe[title$=" service"]').waitFor();
 const sentinel=await page.evaluate(()=>{window.__switchDocument=crypto.randomUUID();window.__switchShell=document.querySelector('aside');return {id:window.__switchDocument,timeOrigin:performance.timeOrigin};});
 for(const targetId of [102,37,101,37,100,37,103,102,101,100,103,37]){
  await page.locator(`.cm-instance-switcher-row[href="/instances/${targetId}"]`).click();
  await page.getByRole('heading',{name:targetId===37?instanceName:'Agent '+targetId,exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>({id:window.__switchDocument,timeOrigin:performance.timeOrigin})),sentinel,`switching to ${targetId} reloaded the manager document`);
  assert.equal(await page.evaluate(()=>window.__switchShell?.isConnected),true,'instance switching replaced the outer navigation');
 }
 console.log('PASS running/sleeping/stopped/error native switches preserve manager document and outer navigation');
 assert.equal(await page.getByRole('button',{name:'Expand instance management',exact:true}).count(),0,'list header has no duplicate management fullscreen entry');
 // Navigation recovery stays beside status even when an Agent cannot run.
 const restoreList=page.getByRole('button',{name:'Show instance list',exact:true});
 for(const nextMode of ['lite','pro'])for(const nextState of ['running','stopped','error']){
  mode=nextMode;state=nextState;frameFailure=nextState==='running';
  await page.setViewportSize({width:1440,height:900});await ready();
  await page.getByRole('button',{name:'Hide instance list',exact:true}).click();
  await ready();
  assert.equal(await page.locator('.cm-instance-frame-identity > [data-instance-list-restore]').isVisible(),true,'reloaded collapsed list recovers from the instance identity row');
  for(const width of [1440,1280,768,390]){
   await page.setViewportSize({width,height:900});
   if(width>=1280){
    assert.equal(await restoreList.isVisible(),true,`${nextMode} ${nextState} restore at ${width}`);
    const bounds=await restoreList.boundingBox(),statusBounds=await page.locator('.cm-instance-frame-identity > span').boundingBox(),splitBounds=await page.locator('.cm-instance-split').boundingBox(),agentBounds=await page.locator('.cm-instance-agent-pane').boundingBox();assert.ok(bounds.x>=statusBounds.x+statusBounds.width,'list recovery follows lifecycle status');assert.ok(Math.abs(bounds.y+bounds.height/2-statusBounds.y-statusBounds.height/2)<1,'status and recovery share one row');assert.ok(Math.abs(agentBounds.y-splitBounds.y)<1,'recovery does not create an extra split row');assert.ok(bounds.x>=0&&bounds.x+bounds.width<=width);
   }else{
    const trigger=page.getByRole('button',{name:'Instances',exact:true});await trigger.focus();await page.keyboard.press('Enter');
    const picker=page.getByRole('dialog',{name:'Instances',exact:true});await picker.waitFor();
    assert.equal(await picker.getByRole('link',{name:'Create Instance',exact:true}).isVisible(),true);
    await page.keyboard.press('Escape');assert.equal(await trigger.evaluate(e=>e===document.activeElement),true);
   }
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,`collapsed navigation has no overflow: ${nextMode} ${nextState} ${width}`);
   await page.screenshot({path:path.join(out,`collapsed-${nextMode}-${nextState}-${width}.png`),fullPage:true});
  }
  await page.setViewportSize({width:1440,height:900});await restoreList.focus();await page.keyboard.press('Enter');
  assert.equal(await page.locator('.cm-instance-list-pane').isVisible(),true);
  assert.equal(await page.getByRole('button',{name:'Hide instance list',exact:true}).evaluate(e=>e===document.activeElement),true,'restoring returns focus to the visible list header');
 }
 frameFailure=false;
 for(const nextMode of ['lite','pro'])for(const width of [768,390]){
  mode=nextMode;state='error';await page.setViewportSize({width:1440,height:900});await ready();
  await page.getByRole('button',{name:'Hide instance list',exact:true}).click();await page.setViewportSize({width,height:900});
  await page.getByRole('button',{name:'Enter fullscreen',exact:true}).click();await page.waitForFunction(()=>!!document.fullscreenElement);
  await restoreList.waitFor();const bounds=await restoreList.boundingBox();
  const headerBounds=await page.locator('.cm-instance-frame-header').boundingBox();assert.ok(bounds.y>=headerBounds.y&&bounds.y+bounds.height<=headerBounds.y+headerBounds.height,'fullscreen recovery remains inside the identity header');assert.equal(await restoreList.evaluate(e=>e.previousElementSibling?.tagName), 'SPAN','fullscreen recovery follows lifecycle status');
  await page.screenshot({path:path.join(out,`collapsed-fullscreen-${nextMode}-${width}.png`)});
  await restoreList.focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>!document.fullscreenElement);
  await page.waitForFunction(()=>document.activeElement?.classList.contains('cm-instance-picker-trigger'));
  assert.equal(await page.getByRole('button',{name:'Instances',exact:true}).evaluate(e=>e===document.activeElement),true,'narrow fullscreen recovery focuses the visible left picker');
 }
 mode='lite';state='running';await page.setViewportSize({width:1440,height:900});
 await page.evaluate(()=>localStorage.setItem('clawmanager_locale','zh'));await ready();
 await page.getByRole('button',{name:'隐藏实例列表',exact:true}).click();
 const chineseRestore=page.getByRole('button',{name:'展开实例列表',exact:true});await chineseRestore.waitFor();
 await page.screenshot({path:path.join(out,'collapsed-left-zh-1440.png'),fullPage:true});await chineseRestore.click();
 await page.evaluate(()=>localStorage.setItem('clawmanager_locale','en'));
 await ready();holdStatus=true;removedCurrent=true;await page.getByRole('heading',{name:'Another Agent',exact:true}).waitFor();assert.ok(page.url().endsWith('/instances/38'),'removed current instance returns to remaining authorized instance');assert.equal(await page.getByText('Instance not found',{exact:true}).count(),0);assert.ok(pendingStatuses.length>0,'status request stays pending while metadata404 recovers');holdStatus=false;pendingStatuses.splice(0).forEach(resolve=>resolve());removedCurrent=false;await ready();
 metaFailure=503;await page.goto(url);await page.getByText('Metadata temporarily unavailable',{exact:true}).waitFor();assert.ok(page.url().endsWith('/instances/37'),'server outage must not masquerade as deletion');metaFailure=0;await ready();
 await page.getByRole('button',{name:'Runtime management',exact:true}).click();await page.getByRole('menuitem',{name:/^Delete/}).click();await page.getByRole('dialog',{name:'Delete',exact:true}).getByRole('button',{name:'Delete',exact:true}).click();await page.getByRole('heading',{name:'Another Agent',exact:true}).waitFor();assert.ok(page.url().endsWith('/instances/38'),'runtime delete resumes remaining work');
 removedCurrent=false;await ready();await page.locator('.cm-instance-list-pane').getByRole('button',{name:'Instance management',exact:true}).click();const failedBatchOverview=page.getByRole('dialog',{name:'My Instances',exact:true});const failedBatchRow=failedBatchOverview.getByRole('row').filter({has:page.getByRole('link',{name:instanceName,exact:true})});await failedBatchRow.getByRole('checkbox').check();await failedBatchOverview.getByRole('button',{name:'Batch Delete (1)',exact:true}).click();await page.getByRole('dialog',{name:'Batch Delete',exact:true}).getByRole('button',{name:'Batch Delete',exact:true}).click();await failedBatchOverview.getByRole('alert').getByText(instanceName+': Instance lifecycle operation is already in progress',{exact:true}).waitFor();assert.equal(await failedBatchRow.getByRole('checkbox').isChecked(),true,'failed items remain selected for retry');assert.deepEqual(batchDeletionRequests,[{instance_ids:[37]}]);await page.keyboard.press('Escape');
 removedCurrent=false;await ready();await page.locator('.cm-instance-list-pane').getByRole('button',{name:'Instance management',exact:true}).click();const deletionOverview=page.getByRole('dialog',{name:'My Instances',exact:true});await deletionOverview.getByRole('button',{name:'Actions: '+instanceName,exact:true}).click();await page.getByRole('menuitem',{name:/^Delete/}).click();await page.getByRole('dialog',{name:'Delete',exact:true}).getByRole('button',{name:'Delete',exact:true}).click();await page.getByRole('heading',{name:'Another Agent',exact:true}).waitFor();assert.ok(page.url().endsWith('/instances/38'),'management delete cannot strand current detail');assert.deepEqual(deletionRequests,[37,37]);
 listCount=0;await page.goto(url);await page.getByText('No instances',{exact:true}).waitFor();assert.equal(await page.getByRole('link',{name:'Create Instance',exact:true}).isVisible(),true,'last deletion shows creation in main window');
 listCount=55;removedCurrent=false;state='deleting';await page.evaluate(()=>sessionStorage.removeItem('cm:instance-navigation:last:v1:71'));await page.goto(vite.resolvedUrls.local[0]+'instances');await page.getByText('No instances',{exact:true}).waitFor();assert.equal(await page.getByRole('link',{name:'Create Instance',exact:true}).isVisible(),true,'pending deletions are not reopened');state='running';await ready();
 const createEntry=page.locator('.cm-instance-list-pane').getByRole('link',{name:'Create Instance',exact:true});await createEntry.waitFor();assert.equal(await createEntry.getAttribute('href'),'/instances/new');assert.deepEqual(await page.locator('.cm-instance-list-pane .cm-instance-switcher-shortcuts').locator(':scope > *').allTextContents(),['Create Instance','Instance management']);assert.equal(await page.locator('.cm-header-actions a[href="/instances/new"]').count(),0,'no top-right creation entry');
 await page.locator('.cm-instance-list-pane').getByRole('button',{name:'Instance management',exact:true}).click();await page.getByRole('dialog',{name:'My Instances',exact:true}).waitFor();await page.keyboard.press('Escape');
 const searchFocus=page.locator('.cm-instance-switcher-search input');await searchFocus.click();
 const focusStyle=await searchFocus.evaluate(input=>{const field=input.closest('.cm-instance-switcher-search');const style=getComputedStyle(input),outer=getComputedStyle(field);return {inner:style.outlineStyle,outer:outer.outlineColor,width:outer.outlineWidth};});
 assert.equal(focusStyle.inner,'none','composite search uses one focus indicator, without an inner black frame');
 assert.notEqual(focusStyle.outer,'rgb(0, 0, 0)');assert.notEqual(focusStyle.outer,'rgb(36, 36, 36)');assert.equal(focusStyle.width,'2px');
 assert.equal(await page.locator('.cm-instance-frame-header').getByRole('button',{name:'Hide instance list',exact:true}).count(),0,'hide belongs beside the instance list expansion, not Agent fullscreen');
 assert.deepEqual(await page.locator('.cm-instance-list-pane .cm-instance-switcher-header').getByRole('button').evaluateAll(buttons=>buttons.map(button=>button.getAttribute('aria-label'))),['Hide instance list'],'Hide is the only list header action');
 assert.equal(await page.locator('.cm-instance-detail-header').count(),0,'duplicate page header removed');
 assert.equal(await page.getByText('No reply recorded',{exact:true}).count(),0);
 assert.equal(await page.locator('.cm-instance-frame-header').getByRole('button',{name:'Runtime management',exact:true}).isVisible(),true);
 assert.equal(await page.locator('.cm-instance-frame-header').getByRole('button',{name:'Skill Management',exact:true}).isVisible(),true);
 assert.equal(await page.locator('.cm-instance-frame-header').getByRole('button',{name:'Share Link',exact:true}).innerText(),'');
 assert.equal(await page.locator('.cm-instance-switcher-status-label').count(),0,'status is communicated by tint and accessible name');
 assert.equal(await page.getByRole('button',{name:'Hide instance list',exact:true}).isVisible(),true);
 const faultIcon=page.locator('.cm-instance-switcher-row[data-status="error"] [data-agent-state="error"]').first();await faultIcon.waitFor();
 assert.equal(await faultIcon.locator('.cm-agent-state-art').evaluate(e=>getComputedStyle(e).animationName),'none','error brand never shakes');
 assert.notEqual(await faultIcon.locator('.cm-agent-state-warning').evaluate(e=>getComputedStyle(e).animationName),'none','only error warning animates');
 assert.equal(await faultIcon.evaluate(e=>getComputedStyle(e,'::before').content),'none','no error halo');
 assert.equal(await page.locator('.cm-instance-switcher-row[data-status="error"]').first().evaluate(e=>getComputedStyle(e).backgroundImage),'none','status uses icon, not colored row');
 assert.equal(await page.locator('.cm-instance-switcher-row[data-status="auto_sleep"] [data-agent-state="sleeping"]').count(),1,'automatic sleep maps to sleeping icon');

 assert.equal(await page.getByRole('link',{name:'Back',exact:true}).count(),0);
 assert.equal(await page.getByText('Runtime gateway',{exact:true}).count(),0);
 const rename=page.getByRole('button',{name:'Rename instance',exact:true});await rename.waitFor();
 await rename.click();const nameInput=page.getByRole('textbox',{name:'Instance name',exact:true});assert.notEqual(await nameInput.evaluate(e=>getComputedStyle(e).outlineColor),'rgb(0, 0, 0)','inline form input uses shared focus token');await nameInput.fill('ab');await page.getByRole('button',{name:'Save',exact:true}).click();
 await page.getByRole('alert').filter({hasText:'3–50'}).waitFor();assert.deepEqual(renames,[]);
 await page.keyboard.press('Escape');await page.waitForFunction(()=>document.activeElement?.getAttribute('aria-label')==='Rename instance');assert.equal(await rename.evaluate(e=>e===document.activeElement),true);
 renameError=true;await rename.click();await nameInput.fill('Retry this name');await page.keyboard.press('Enter');await page.getByRole('alert').filter({hasText:'Rename temporarily unavailable'}).waitFor();
 assert.equal(await nameInput.inputValue(),'Retry this name');await page.keyboard.press('Escape');renameError=false;
 holdMeta=true;for(let i=0;i<150&&!releaseMeta;i++)await page.waitForTimeout(50);assert.ok(releaseMeta,'hold an older metadata request');
 await rename.click();await nameInput.fill('  Renamed Agent 37  ');await page.keyboard.press('Enter');await page.getByRole('heading',{name:'Renamed Agent 37',exact:true}).waitFor();releaseMeta();await page.waitForTimeout(100);
 assert.equal(await page.getByRole('heading',{name:'Renamed Agent 37',exact:true}).isVisible(),true,'stale metadata does not revert rename');assert.deepEqual(renames.at(-1),{name:'Renamed Agent 37'});
 const switcher=page.locator('.cm-instance-switcher');await switcher.locator('a[aria-current="page"]').waitFor();assert.match(await switcher.locator('a[aria-current="page"]').innerText(),/Renamed Agent 37/);
 const selectedRow=switcher.locator('a[aria-current="page"]');await selectedRow.focus();assert.equal(await selectedRow.evaluate(e=>getComputedStyle(e,'::after').borderTopWidth),'1px','selected border persists while keyboard focused');
 const search=switcher.getByRole('searchbox');await search.fill('Agent 152');await switcher.locator('a[href="/instances/152"]').waitFor();await search.fill('');await switcher.locator('a[href="/instances/38"]').waitFor();
 await switcher.locator('.cm-instance-switcher-scroll[aria-busy="false"]').waitFor();await switcher.locator('.cm-instance-switcher-pagination button').last().click();await switcher.locator('.cm-instance-switcher-rows a[href="/instances/152"]').waitFor();
 assert.equal(await switcher.locator('a[aria-current="page"]').count(),1);listCount=50;await switcher.locator('.cm-instance-switcher-rows a[href="/instances/38"]').waitFor();listCount=55;
 await search.fill('No matching instance');await switcher.locator('.cm-instance-switcher-scroll').getByText('No matching instances',{exact:true}).waitFor();
 listError=true;await search.fill('Retry query');await switcher.locator('.cm-instance-switcher-error').waitFor();listError=false;await switcher.locator('.cm-instance-switcher-error button').click();await switcher.locator('.cm-instance-switcher-scroll').getByText('No matching instances',{exact:true}).waitFor();
 await search.fill('');await switcher.locator('a[href="/instances/38"]').waitFor();

 const menu=page.getByRole('button',{name:names.en,exact:true});
 try { await menu.waitFor(); } catch(error) {
  console.log('Body:',await page.locator('body').innerText());console.log('Visible buttons:',await page.getByRole('button').allTextContents());console.log('Page errors:',errors);
  await page.screenshot({path:path.join(out,'failure.png'),fullPage:true});throw error;
 }
 assert.equal(await page.getByText('Access available',{exact:true}).count(),0);
 assert.equal(await page.getByRole('heading',{name:'Session Token Usage',exact:true}).count(),0);
 assert.equal(await page.getByRole('button',{name:'Stop',exact:true}).isVisible(),false);
 await menu.focus();await page.keyboard.press('ArrowDown');
 await page.getByRole('menuitem',{name:'Stop',exact:true}).waitFor();
 await page.keyboard.press('Escape');assert.equal(await menu.evaluate(e=>e===document.activeElement),true);
 await menu.click();await page.getByRole('menuitem',{name:/^Delete/}).click();
 await page.getByRole('dialog').waitFor();await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();assert.deepEqual(writes,[]);
 const skills=page.getByRole('button',{name:'Skill Management',exact:true});
 await skills.click();const dialog=page.getByRole('dialog',{name:'Skill Management',exact:true});await dialog.waitFor();
 await dialog.locator('input[type="search"]').first().waitFor();
 assert.equal(await dialog.getByRole('button',{name:'Expand',exact:true}).count(),0);
 await page.screenshot({path:path.join(out,'skills-desktop.png')});
 await dialog.locator('input[type="search"]').first().fill('Retained skill filter');
 await dialog.getByRole('button',{name:'Sync Skills',exact:true}).click();
 await dialog.getByRole('button',{name:'Syncing…',exact:true}).waitFor();
 await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'});assert.equal(await skills.evaluate(e=>e===document.activeElement),true);
 await skills.click();await dialog.waitFor();
 assert.equal(await dialog.locator('input[type="search"]').first().inputValue(),'Retained skill filter','closing skills retains working state');
 assert.equal(await dialog.getByRole('button',{name:'Syncing…',exact:true}).isDisabled(),true,'reopening preserves active sync guard');
 assert.equal(syncPosts,1);releaseSync();await dialog.getByText('Fixture sync failed after reopen',{exact:true}).waitFor();
 await page.keyboard.press('Escape');
 installable=true;await skills.click();await dialog.getByText('Fixture installable skill',{exact:true}).waitFor();
 await dialog.getByRole('button',{name:'Install to instance',exact:true}).click();await page.waitForFunction(()=>document.querySelector('dialog[open] button')!==null);
 await page.keyboard.press('Escape');
 while(!releaseInstall)await page.waitForTimeout(10);
 releaseInstall();await page.waitForTimeout(50);await skills.click();
 await dialog.getByText('Fixture install failed while closed',{exact:true}).waitFor();
 await dialog.getByText('Fixture installable skill',{exact:true}).waitFor();
 assert.equal(await dialog.getByText('Fixture install failed while closed',{exact:true}).isVisible(),true,'successful inventory refresh preserves failed action');
 await page.keyboard.press('Escape');


 const separator=page.getByRole('separator',{name:'Resize Agent and workspace',exact:true});
 await separator.waitFor();
 const split=page.locator('.cm-instance-split');
 const shares=()=>split.evaluate(e=>{const panes=[...e.querySelectorAll(':scope > [data-instance-pane]')].map(node=>node.getBoundingClientRect().width);const total=panes.reduce((sum,n)=>sum+n,0);return panes.map(n=>n/total);});
 const ratio=async()=>(await shares())[1];
 assert.ok((await shares()).every((n,i)=>Math.abs(n-[.12,.68,.2][i])<.015),'default three panes follow 1.2:6.8:2');
 const listSeparator=page.getByRole('separator',{name:'Resize instance list',exact:true});await listSeparator.waitFor();const listBox=await listSeparator.boundingBox();await page.mouse.move(listBox.x+listBox.width/2,listBox.y+60);await page.mouse.down();await page.mouse.move(listBox.x+40,listBox.y+60,{steps:5});await page.mouse.up();assert.ok((await shares())[0]>.14);await listSeparator.dblclick({position:{x:8,y:48}});
 const frame=page.locator('iframe[title$=" service"]');await frame.waitFor();const frameNode=await frame.elementHandle();
 const draft=page.frameLocator('iframe[title$=" service"]').getByRole('textbox',{name:'Conversation draft'});await draft.fill('Keep this unsent draft');
 const box=await separator.boundingBox();await page.mouse.move(box.x+box.width/2,box.y+60);await page.mouse.down();await page.mouse.move(box.x-110,box.y+60,{steps:10});await page.mouse.up();
 assert.ok(await ratio()<.66,'pointer dragging changes panel widths');
 assert.equal(await split.getAttribute('data-dragging'),'false');
 assert.equal(await draft.inputValue(),'Keep this unsent draft');
 assert.equal(await frame.evaluate((e,original)=>e===original,frameNode),true,'resizing preserves the native frame');
 await separator.focus();const before=Number(await separator.getAttribute('aria-valuenow'));await page.keyboard.press('ArrowRight');assert.ok(Number(await separator.getAttribute('aria-valuenow'))>before);
 await page.keyboard.press('End');assert.equal(Number(await separator.getAttribute('aria-valuenow')),73);
 await page.keyboard.press('Home');assert.equal(Number(await separator.getAttribute('aria-valuenow')),35);
 await separator.dblclick();assert.ok(Math.abs(await ratio()-.68)<.015);
 const hide=page.locator('.wb-instance-agent button').filter({has:page.locator('svg.lucide-panel-right-close')});await hide.click();assert.equal(await separator.isVisible(),false);
 const show=page.locator('.wb-instance-agent button').filter({has:page.locator('svg.lucide-panel-right-open')});await show.click();await separator.waitFor();
 await listSeparator.focus();await page.keyboard.press('Home');await separator.focus();await page.keyboard.press('Home');await hide.click();await listSeparator.focus();await page.keyboard.press('End');await show.click();
 assert.ok((await shares())[1]>=.35-.005,'restoring tools preserves Agent minimum after hidden-pane resizing');await listSeparator.dblclick({position:{x:8,y:48}});

 const hideList=page.getByRole('button',{name:'Hide instance list',exact:true});await hideList.click();
 assert.equal(await split.getAttribute('data-list'),'false');assert.equal(await listSeparator.count(),0);assert.equal(await page.getByRole('button',{name:'Show instance list',exact:true}).evaluate(e=>e===document.activeElement),true,'restore receives focus after list header collapse');
 assert.ok(Math.abs((await shares())[1]-.8)<.015,'hidden list frees space for Agent');
 await separator.focus();await page.keyboard.press('Home');
 await page.getByRole('button',{name:'Show instance list',exact:true}).click();
 assert.ok((await shares())[1]>=.35-.005,'restoring list preserves Agent minimum');await listSeparator.dblclick({position:{x:8,y:48}});
 await hideList.click();await page.getByRole('button',{name:'Enter fullscreen',exact:true}).click();await page.waitForFunction(()=>!!document.fullscreenElement);
 await page.getByRole('button',{name:'Show instance list',exact:true}).click();await page.waitForFunction(()=>!document.fullscreenElement);assert.equal(await hideList.evaluate(e=>e===document.activeElement),true,'restoring list exits Agent fullscreen and keeps visible focus');
 const overviewButton=page.locator('.cm-instance-list-pane').getByRole('button',{name:'Instance management',exact:true});await overviewButton.click();
 const overview=page.getByRole('dialog',{name:'My Instances',exact:true});await overview.waitFor();
 assert.equal(await overview.locator('table').isVisible(),true,'original management table available in expansion');
 await page.keyboard.press('Escape');await overview.waitFor({state:'hidden'});assert.equal(await overviewButton.evaluate(e=>e===document.activeElement),true);

 for(const [width,height] of [[2048,1152],[1440,900],[1280,800],[768,1024],[390,844]]){
  await page.setViewportSize({width,height});await page.screenshot({path:path.join(out,`instance-${width}.png`),fullPage:true});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,`no page overflow ${width}`);if(width>=1280){assert.equal(await createEntry.isVisible(),true);const createBox=await createEntry.boundingBox();assert.ok(createBox.x>=0&&createBox.x+createBox.width<=width);}else{await page.getByRole('button',{name:'Instances',exact:true}).click();const picker=page.getByRole('dialog',{name:'Instances',exact:true});assert.equal(await picker.getByRole('link',{name:'Create Instance',exact:true}).isVisible(),true);assert.equal(await picker.getByRole('button',{name:'Instance management',exact:true}).isVisible(),true);await page.keyboard.press('Escape');}
  assert.equal(await separator.isVisible(),width>=1280);
  const header=page.locator('.cm-instance-split .wb-header');
  const tabs=await header.getByRole('tablist').boundingBox(),expand=await header.locator(':scope > button').boundingBox();
  assert.ok(tabs.x+tabs.width<=expand.x+1,'workbench tabs leave room for fullscreen control');
  const tools=split.locator('[data-instance-pane="tools"]'),toolBounds=await tools.boundingBox();
  for(const label of ['Refresh','Search files','New folder','Upload']){const button=tools.getByRole('button',{name:label,exact:true});const bounds=await button.boundingBox();assert.ok(bounds.x>=toolBounds.x&&bounds.x+bounds.width<=toolBounds.x+toolBounds.width+1,`${label} remains reachable at ${width}`);}

 }
 await page.getByRole('button',{name:'Share Link',exact:true}).click();
 const sharePanel=page.locator('[data-panel="share-link-popover"]');await sharePanel.waitFor();
 assert.equal(await sharePanel.evaluate(e=>e.matches(':popover-open')),true,'share remains in top layer after header move');
 const shareBounds=await sharePanel.boundingBox();assert.ok(shareBounds.x>=0&&shareBounds.x+shareBounds.width<=390&&shareBounds.y+shareBounds.height<=844,'share panel fits narrow viewport');await page.keyboard.press('Escape');
 const pickerTrigger=page.getByRole('button',{name:'Instances',exact:true});await pickerTrigger.click();const picker=page.getByRole('dialog',{name:'Instances',exact:true});await picker.waitFor();await picker.getByRole('searchbox').fill('Another Agent');await picker.locator('a[href="/instances/38"]').waitFor();const hiddenPoll=page.waitForResponse(response=>{const u=new URL(response.url());return u.pathname==='/api/v1/instances'&&!u.searchParams.get('query')&&response.status()===200;});await hiddenPoll;assert.equal(await page.evaluate(async()=>{const{readInstanceList}=await import('/src/lib/instanceNavigation.ts');return readInstanceList(71,'Another Agent',1)?.data.instances[0]?.name;}),'Another Agent','hidden desktop polling cannot overwrite active mobile list snapshot');await page.keyboard.press('Escape');await picker.waitFor({state:'hidden'});assert.equal(await pickerTrigger.evaluate(e=>e===document.activeElement),true);
 const shortName=instanceName;instanceName='Long authorized instance name '.repeat(3).slice(0,50);
 for(const locale of ['en','zh','ja','ko','de']){
  await page.evaluate(value=>localStorage.setItem('clawmanager_locale',value),locale);await ready();await page.getByRole('button',{name:names[locale],exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,`localized narrow layout ${locale}`);
 }
 instanceName=shortName;await page.evaluate(()=>localStorage.setItem('clawmanager_locale','en'));skillsError=true;await ready();await page.getByRole('button',{name:'Skill Management',exact:true}).click();await page.getByRole('dialog').getByText('Skills temporarily unavailable',{exact:true}).waitFor();await page.keyboard.press('Escape');
 for(const next of ['stopped','deleting']){
  state=next;await ready();await menu.click();
  if(next==='stopped')assert.equal(await page.getByRole('menuitem',{name:'Start',exact:true}).isVisible(),true);
  else assert.equal(await page.getByRole('menuitem',{name:/^Delete/}).getAttribute('aria-disabled'),'true');
  await page.keyboard.press('Escape');
 }
 state='running';mode='pro';await page.setViewportSize({width:1440,height:900});await ready();assert.ok(Math.abs(await ratio()-.68)<.015,'Pro shares the same split');
 for(const width of [768,390]){await page.setViewportSize({width,height:900});const dimensions=await split.locator(':scope > [data-instance-pane]').evaluateAll(panes=>panes.map(pane=>pane.getBoundingClientRect().height));assert.ok(dimensions[1]>=280&&dimensions[2]>=560,'Pro panels keep usable narrow-screen heights');}
 await page.setViewportSize({width:1440,height:900});

 assert.equal(await page.getByRole('heading',{name:'Session Token Usage',exact:true}).count(),0);
 await page.screenshot({path:path.join(out,'instance-pro.png'),fullPage:true});
 const beforeSlow=metaCalls;metaDelay=6000;await ready();metaDelay=0;assert.equal(metaCalls-beforeSlow,1,'slow metadata completes without overlapping polling');
 await page.setViewportSize({width:1440,height:900});
 await switcher.getByRole('searchbox').fill('');await switcher.locator('.cm-instance-switcher-scroll[aria-busy="false"]').waitFor();
 const scroll=switcher.locator('.cm-instance-switcher-scroll');await scroll.evaluate(e=>{e.scrollTop=480;});await page.waitForTimeout(50);
 const savedScroll=await scroll.evaluate(e=>e.scrollTop);assert.ok(savedScroll>0);
 const shell=await page.locator('.app-topbar').elementHandle();await page.evaluate(()=>window.__cmDocumentMarker='same-document');
 const priorFrame=await page.locator('iframe[title$=" service"]').elementHandle();
 releaseDestination=true;await switcher.locator('a[href="/instances/105"]').click();
 while(typeof releaseDestination!=='function')await page.waitForTimeout(10);
 assert.ok(page.url().endsWith('/instances/37'),'destination preparation retains current route');
 assert.equal(await page.locator('iframe[title$=" service"]').evaluate((node,previous)=>node===previous,priorFrame),true,'preparation retains conversation while waiting');
 releaseDestination();await page.getByRole('heading',{name:'Agent 105',exact:true}).waitFor();
 assert.equal(await page.locator('.app-topbar').evaluate((node,previous)=>node===previous,shell),true,'SPA switching retains global shell');
 assert.equal(await page.evaluate(()=>window.__cmDocumentMarker),'same-document','normal switching keeps document');
 await switcher.locator('.cm-instance-switcher-rows a[href="/instances/105"]').waitFor();
 assert.ok(Math.abs(await scroll.evaluate(e=>e.scrollTop)-savedScroll)<2,'switching retains list scroll position');
 holdInventory=true;
 await page.reload();await page.getByRole('heading',{name:'Agent 105',exact:true}).waitFor();
 await switcher.locator('.cm-instance-switcher-rows a[href="/instances/105"]').waitFor({timeout:2000});
 assert.ok(Math.abs(await scroll.evaluate(e=>e.scrollTop)-savedScroll)<2,'document reload restores inventory and scroll without waiting for network');
 holdInventory=false;releaseInventory.splice(0).forEach(resolve=>resolve());
 await page.goto(vite.resolvedUrls.local[0]+'instances');await page.getByRole('heading',{name:'Agent 105',exact:true}).waitFor();
 assert.ok(page.url().endsWith('/instances/105'),'instances entry restores previous authorized workbench');
 await page.goto(vite.resolvedUrls.local[0]+'instances?q=Agent+152&status=running');await overview.waitFor().catch(async error=>{console.log('Legacy URL:',page.url(),'Errors:',errors,'Body:',(await page.locator('body').innerText()).slice(0,2400));throw error;});
 assert.equal(await overview.locator('.cm-search-field input').inputValue(),'Agent 152','legacy query filters initialize expanded management');
 await page.keyboard.press('Escape');await overview.waitFor({state:'hidden'});
 await page.setViewportSize({width:1440,height:900});await page.locator('.cm-instance-switcher a[href="/instances/38"]').click();await page.getByRole('heading',{name:'Another Agent',exact:true}).waitFor();
 accountID=72;await page.evaluate(async()=>{const {useAuthStore}=await import('/src/stores/authStore.ts');useAuthStore.setState({user:{id:72,username:'Account 72',role:'admin'},isAuthenticated:true});});
 assert.equal((await page.locator('.cm-instance-switcher-name').allTextContents()).some(name=>name==='Another Agent'),false,'former account instance must disappear immediately');
 await page.getByRole('heading',{name:'Account 72 current',exact:true}).waitFor();await page.locator('.cm-instance-switcher-name').getByText('Private Agent 72',{exact:true}).waitFor();
 await createEntry.click();await page.waitForURL('**/instances/new');await page.locator('form').waitFor();await page.goBack();await page.getByRole('heading',{name:'Account 72 current',exact:true}).waitFor();
 listCount=0;await page.evaluate(()=>sessionStorage.removeItem('cm:instance-navigation:last:v1:72'));await page.setViewportSize({width:390,height:844});await page.goto(vite.resolvedUrls.local[0]+'instances');await page.getByText('No instances',{exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'Instance management',exact:true}).count(),0,'empty workspace only offers creation'); assert.equal(await page.getByRole('dialog').count(),0,'empty workspace does not obstruct creation with a management dialog');const emptyCreate=page.getByRole('link',{name:'Create Instance',exact:true});assert.equal(await emptyCreate.isVisible(),true);await emptyCreate.click();await page.waitForURL('**/instances/new');await page.locator('form').waitFor();
 assert.deepEqual(writes,[]);assert.deepEqual(errors,[]);
 console.log('PASS real instance route: compact lifecycle menu, skills modal/focus/errors, rename/list search/pages/errors/account isolation, default 1.2:6.8:2, pointer/keyboard resize, preserved iframe, hide/show and collapsed-refresh/service-error/fullscreen recovery, Lite/Pro, five widths, five locales, no mutations/page errors');
} finally {await browser?.close();await vite?.close();}
