// Targeted browser regression against the real application through a dev server
// or downloaded production bundle. Every API/native page below is a fixture.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { chromium } from 'playwright';

const origin = process.env.OWNER_WORKBENCH_ORIGIN || 'http://127.0.0.1:9178';
const screenshots = path.resolve(import.meta.dirname, 'reports/owner-workbench');
fs.mkdirSync(screenshots, {recursive:true});
const browser = await chromium.launch({channel:'chrome',headless:true});
const page = await browser.newPage({viewport:{width:1440,height:1000}});
page.setDefaultTimeout(45000);
const errors=[], calls=[], sessions=new Map();
page.on('pageerror',e=>errors.push(e.message));
await page.addInitScript(()=>{localStorage.setItem('clawmanager_locale',sessionStorage.getItem('workbench-test-locale')||'zh');localStorage.setItem('access_token','cm-fixture-never-send-to-owner');});
await page.routeWebSocket(/\/api\/v1\/ws/,()=>{});
await page.route('**/fixture-agent**',route=>route.fulfill({contentType:'text/html',body:'<p>Fixture native instance</p>'}));
await page.route('**/fixture-preview**',route=>route.fulfill({contentType:'text/html',body:'<h1>Fixture local web service</h1>'}));
const owner='portal-accept-20260923@example.com',now=new Date().toISOString();
const members=['openclaw','hermes','opencode','deepseek-harness'].map((runtime,i)=>({id:i+1,team_id:184,instance_id:616+i,user_id:1,member_key:i?'worker-'+i:'leader',display_name:i?'成员 '+i:'团队负责人',role:i?'worker':'leader',runtime_type:runtime,instance_mode:'lite',status:'idle',availability:'idle',runtime_status:'idle',progress:0,created_at:now,updated_at:now}));
const instances = new Map([
 [609,{id:609,name:'Hermes long instance name to verify narrow layouts',type:'hermes'}],
 [590,{id:590,name:'DeepSeek personal instance',type:'deepseek-harness'}],
 ...members.map(m=>[m.instance_id,{id:m.instance_id,name:m.display_name,type:m.runtime_type,team_id:184}]),
].map(([id,i])=>[id,{...i,owner,runtime_type:'gateway',runtime_generation:3,instance_mode:'lite',status:'running',workspace_available:true,workspace_root:'Workspace',created_at:now,updated_at:now}]));
const team={id:184,user_id:1,name:'四种实例协作团队',status:'running',communication_mode:'leader_mediated',shared_mount_path:'/team',created_at:now,updated_at:now};
const detail={team,members,leader_member_id:'leader',tasks:[{id:402,team_id:184,target_member_id:1,message_id:'task-402',status:'succeeded',payload:{title:'论文精读',prompt:'只精读两篇论文'},result:{result:'笔记见 /team/report.md'},created_at:now,updated_at:now}],events:[],work_items:[]};
await page.route('**/api/v1/**',async route=>{
 const r=route.request(),u=new URL(r.url()),p=u.pathname;calls.push({method:r.method(),path:p,query:u.search,headers:r.headers(),body:r.postData()});
 const send=data=>route.fulfill({json:{success:true,data}});
 if(p.endsWith('/auth/me'))return send({id:1,username:'admin',role:'admin',status:'active'});
 if(p.endsWith('/version'))return send({version:'fixture'});
 if(p.endsWith('/ieisystem/session')||p.endsWith('/session/refresh'))return send({owner,expires_at:new Date(Date.now()+3600000).toISOString()});
 if(p.endsWith('/lifecycle-operations/restart-609'))return send({operation:{operation_id:'restart-609',status:'succeeded'}});
 if(p.endsWith('/team-capabilities'))return send({enabled:true,can_create:true});
 if(p.endsWith('/ieisystem/dashboard'))return send({summary:{questions:5,total_tokens:50000,sessions:2},last_activity:{},trend:[]});
 if(p.endsWith('/ieisystem/growth'))return send({xp:0,level:1,tasks:[],ledger:[],days:[]});
 if(p.endsWith('/ieisystem/reports'))return send({start:'2026-11-16',end:'2026-11-22',ongoing:true,counts:{tokens:50000,sessions:2},active_days:1,previous:{},runtime:[],days:[]});
 if(p==='/api/v1/ieisystem/instances'){const index=Number(u.searchParams.get('page')||1);return send({instances:index===1?[instances.get(609)]:[instances.get(590)],total:2,page:index,limit:1});}
 if(p.endsWith('/teams')&&r.method()==='GET')return send({teams:[{...team,members,member_count:4,available_count:4}],total:1,page:1,limit:100});
 if(p.endsWith('/teams/184'))return send(detail);
 if(p.endsWith('/teams/184/tasks'))return send({tasks:detail.tasks,has_more:false});
 if(p.endsWith('/teams/184/events'))return send({events:[],has_more:false});
 if(p.endsWith('/workspace/files'))return send({entries:[{name:'report.md',path:'report.md',type:'file',is_dir:false,previewable:true,size:123,modified_at:now}]});
 if(p.endsWith('/workspace/preview'))return send({preview:{kind:'text',text:'# Notebook',path:'report.md'},kind:'text',text:'# Notebook',path:'report.md'});
 const m=/\/instances\/(\d+)(.*)$/.exec(p);
 if(m){const id=Number(m[1]),suffix=m[2];
  if(!suffix)return send({instance:instances.get(id)});
  if(suffix==='/restart'&&r.method()==='POST')return send({operation:{operation_id:'restart-609',status:'running'}});
  if(suffix==='/access')return send({access_url:origin+'/fixture-agent/'+id,expires_at:new Date(Date.now()+3600000).toISOString(),workspace_available:true,workspace_root:'Workspace'});
  if(suffix==='/workbench/capabilities')return send({generation:3,workspace:'/workspace',git:false,terminal:true,preview:true,processes:false});
  if(suffix==='/workbench/terminals'){
   if(r.method()==='POST'){const value={id:'session_'+id+'_'+(sessions.size+1),name:'Terminal '+id,generation:3,cwd:'/workspace',exited:false,created_at:now};sessions.set(value.id,{...value,instance:id});return send(value);}
   return send({sessions:[...sessions.values()].filter(s=>s.instance===id)});
  }
  if(suffix.includes('/workbench/terminals/')&&suffix.endsWith('/output'))return send({data:Buffer.from('workspace ready\r\n$ ').toString('base64'),cursor:1,truncated:false,exited:false});
  if(suffix.includes('/workbench/terminals/'))return send({});
  if(suffix==='/workbench/previews')return send({url:origin+'/fixture-preview/'+id,generation:3});
 }
 return send({});
});
const root=()=>page.locator('.owner-instance-workbench .wb-root');
const toolPane=()=>page.locator('.team-workspace-tools');
const checkOverflow=async()=>assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'page overflows');
try{
 await page.goto(origin+'/ieisystem/list-instances');
 const account=page.locator('.portal-unified-account');
 await account.locator('summary').click();
 await account.getByText(owner,{exact:true}).click();
 await page.waitForTimeout(150);
 assert.deepEqual(errors,[],'clicking the owner email must not crash the portal');
 await page.goto(origin+'/ieisystem/instances/609');
 await root().getByRole('tab',{name:'预览与终端',exact:true}).click();
 assert.deepEqual(await root().locator('.wb-panels [role="tab"]').allTextContents(),['文件','Git 项目','预览与终端']);
 await root().getByRole('button',{name:'新建终端',exact:true}).click();
 await root().locator('.xterm').waitFor();
 assert.equal([...sessions.values()].filter(s=>s.instance===609).length,1);
 await root().locator('.wb-create-button').click();
 await root().locator('.wb-popover').getByRole('button',{name:'网页',exact:true}).click();
 const mixedTabs=root().locator('.wb-program-tabs [role="tab"]');
 assert.equal(await mixedTabs.count(),2,'website and terminal must share a single tab strip');
 await mixedTabs.first().click();await root().locator('.xterm:visible').waitFor();
 await mixedTabs.first().focus();await page.keyboard.press('End');
 await root().locator('.wb-browser:visible').waitFor();
 await root().getByRole('tab',{name:'文件',exact:true}).click();
 await root().getByRole('tab',{name:'预览与终端',exact:true}).click();
 assert.equal(await mixedTabs.last().getAttribute('aria-selected'),'true','returning to preview lost its active website');
 await mixedTabs.first().click();await root().locator('.xterm:visible').waitFor();
 assert.equal([...sessions.values()].filter(s=>s.instance===609).length,1,'switching mixed tabs recreated the terminal');
 const rail=page.locator('.portal-unified-rail');
 const toolbar=page.locator('.portal-instance-toolbar');
 const restart=toolbar.getByRole('button',{name:'重启实例',exact:true});
 const refresh=toolbar.getByRole('button',{name:'刷新访问',exact:true});
 await restart.waitFor();await refresh.waitFor();
 assert.equal(await toolbar.locator('h1').textContent(),instances.get(609).name);
 assert.equal(await toolbar.innerText(),instances.get(609).name);
 assert.equal(await page.getByRole('button',{name:'切换实例',exact:true}).count(),0);
 assert.equal(await page.getByRole('link',{name:'返回我的总览',exact:true}).count(),0);
 const leftToggle=toolbar.locator('[aria-controls="owner-workspace-rail"]');
 const rightToggle=toolbar.locator('[aria-controls="iei-instance-workspace"]');
 const fullscreen=toolbar.locator('[aria-pressed]');
 assert.equal(await leftToggle.evaluate(node=>node.nextElementSibling?.tagName),'H1');
 assert.ok(await rightToggle.evaluate(node=>node===node.parentElement?.lastElementChild));
 await leftToggle.click();await rail.waitFor({state:'hidden'});
 assert.ok(await root().isVisible());
 assert.equal(await fullscreen.getAttribute('title'),'全屏');
 await rightToggle.click();await root().waitFor({state:'hidden'});
 assert.equal(await fullscreen.getAttribute('title'),'退出全屏');
 assert.equal(await leftToggle.isEnabled(),true);
 assert.equal(await rightToggle.isEnabled(),true);
 await fullscreen.click();await rail.waitFor();await root().waitFor();
 await rightToggle.click();await root().waitFor({state:'hidden'});
 await fullscreen.click();await page.waitForFunction(()=>!!document.fullscreenElement);
 await rail.waitFor({state:'hidden'});
 assert.ok(await leftToggle.isDisabled()&&await rightToggle.isDisabled());
 await restart.click();
 await page.getByRole('dialog',{name:'确认重启实例？'}).getByRole('button',{name:'取消',exact:true}).click();
 await fullscreen.click();await page.waitForFunction(()=>!document.fullscreenElement);await rail.waitFor();
 assert.ok(await leftToggle.isEnabled()&&await rightToggle.isEnabled());
 assert.equal(await rightToggle.getAttribute('aria-expanded'),'false');
 await rightToggle.click();await root().waitFor();
 await page.evaluate(()=>Object.defineProperty(document,'fullscreenEnabled',{value:false,configurable:true}));
 await fullscreen.click();await rail.waitFor({state:'hidden'});await root().waitFor({state:'hidden'});
 assert.ok(await leftToggle.isDisabled()&&await rightToggle.isDisabled());
 assert.equal(await page.evaluate(()=>document.fullscreenElement),null);
 await fullscreen.click();await rail.waitFor();await root().waitFor();
 await page.evaluate(()=>Reflect.deleteProperty(document,'fullscreenEnabled'));
 const restartCalls=()=>calls.filter(c=>c.method==='POST'&&c.path.endsWith('/instances/609/restart')).length;
 await refresh.click();await root().waitFor();
 await restart.click();
 const confirm=page.getByRole('dialog',{name:'确认重启实例？'});
 await confirm.getByText(instances.get(609).name,{exact:false}).waitFor();
 assert.equal(restartCalls(),0);
 await page.keyboard.press('Escape');
 assert.ok(await restart.evaluate(el=>el===document.activeElement));
 await restart.click();
 await confirm.getByRole('button',{name:'取消',exact:true}).click();
 assert.equal(restartCalls(),0);
 await restart.click();
 await confirm.getByRole('button',{name:'确认重启',exact:true}).click();
 await page.getByText('实例已完成重启并重新连接。',{exact:true}).waitFor();
 assert.equal(restartCalls(),1);
 await rail.locator('[data-instance-id="590"] .instance-summary').click();
 await page.waitForURL(/\/ieisystem\/instances\/590(?:\?|$)/);await root().waitFor();
 assert.ok(calls.some(c=>c.path==='/api/v1/ieisystem/instances'&&c.query.includes('page=2')),'missing second instance page');
 await rail.getByRole('tab',{name:/^Team ·/}).click();
 await rail.locator('[data-team-id="184"] .ot-card-title').click();
 await page.locator('.team-chat-workspace').waitFor();
 const headingActions=page.locator('.team-chat-header-actions');
 await headingActions.locator('.team-header-files-trigger').waitFor();
 await headingActions.getByRole('button',{name:'Team 操作',exact:true}).click();
 await headingActions.getByRole('region',{name:'Team 操作'}).getByRole('button',{name:/删除/}).click();
 const deleteDialog=page.getByRole('dialog',{name:/删除/});
 await deleteDialog.getByRole('button',{name:'取消',exact:true}).click();
 assert.equal(calls.filter(call=>call.method==='DELETE').length,0);
 assert.equal(await page.getByRole('link',{name:'我的团队空间',exact:true}).count(),0);
 await rail.getByRole('tab',{name:'Team 设置',exact:true}).click();
 const guideButton=page.getByRole('button',{name:'使用指南',exact:true});
 await guideButton.click();
 const guide=page.getByRole('dialog',{name:'Team 使用指南'});
 assert.equal(await guide.getByRole('tab').count(),8);
 for(const tab of await guide.getByRole('tab').all()) {
  const title=await tab.innerText();await tab.click();
  await guide.getByRole('tabpanel').getByRole('heading',{name:title,exact:true}).waitFor();
  assert.ok((await guide.getByRole('tabpanel').innerText()).length>450,'full chapter '+title);
 }
 await guide.getByRole('tab',{name:'共享文件与编辑',exact:true}).click();
 await guide.getByRole('heading',{name:'网页与终端（实例工具）',exact:true}).waitFor();
 assert.ok(await guide.getByRole('table').count()>=2);
 await guide.getByRole('tab',{name:'共享文件与编辑',exact:true}).focus();
 await page.keyboard.press('End');
 assert.equal(await guide.getByRole('tab',{selected:true}).innerText(),'操作记录与常见问题');
 await page.keyboard.press('Home');
 assert.equal(await guide.getByRole('tab',{selected:true}).innerText(),'第一次使用');
 await page.screenshot({path:path.join(screenshots,'full-team-guide-desktop.png')});
 await page.setViewportSize({width:390,height:844});
 await guide.getByRole('tab',{name:'共享文件与编辑',exact:true}).click();
 await guide.getByRole('tabpanel').evaluate(el=>el.scrollTo({top:el.scrollHeight}));
 const guideBox=await guide.boundingBox();assert.ok(guideBox.x>=0&&guideBox.x+guideBox.width<=390&&guideBox.y>=0&&guideBox.y+guideBox.height<=844);
 await page.screenshot({path:path.join(screenshots,'full-team-guide-mobile.png')});
 await page.setViewportSize({width:1440,height:1000});
 await page.keyboard.press('Escape');await guide.waitFor({state:'hidden'});
 assert.ok(await guideButton.evaluate(node=>node===document.activeElement));
 assert.ok(await page.getByRole('button',{name:'操作记录',exact:true}).isVisible());
 assert.ok(await page.getByRole('tab',{name:'团队模板',exact:true}).isVisible());
 assert.equal(new URL(page.url()).searchParams.get('selected_team_id'),'184');
 await rail.locator('[data-team-id="184"] .ot-card-title').click();
 await page.locator('.team-chat-workspace').waitFor();
 assert.equal(await page.getByRole('button',{name:'切换实例',exact:true}).count(),0);
 assert.equal(await rail.locator('.ot-directory-member').count(),4);
 await rail.getByRole('link',{name:/成员 2.*#618/}).click();
 await page.waitForURL(/\/ieisystem\/instances\/618(?:\?|$)/);await root().waitFor();
 await root().getByRole('tab',{name:'预览与终端',exact:true}).click();
 await root().getByRole('button',{name:'新建网页',exact:true}).click();
 await root().locator('.wb-browser input').fill('http://localhost:3000');
 await root().locator('.wb-browser input').press('Enter');
 await root().locator('.wb-preview-frame').waitFor();
 for(const width of [1440,1280,768,390,320]){
  await page.setViewportSize({width,height:1000});await checkOverflow();
  const heading=await toolbar.locator('h1').boundingBox();
  const controls=await toolbar.locator('.portal-instance-toolbar-actions').boundingBox();
  assert.ok(heading.x+heading.width<=controls.x+1,'title and controls overlap at '+width);
  if(width<768){
   await rail.getByRole('button',{name:'工作区列表',exact:true}).click();
   await rail.getByRole('link',{name:/成员 2.*#618/}).waitFor();
   await rail.getByRole('button',{name:'工作区列表',exact:true}).click();
  }
 }
 await page.setViewportSize({width:1440,height:1000});
 await page.goto(origin+'/ieisystem/list-instances');
 await rail.getByRole('tab',{name:'总览',exact:true}).waitFor();
 await rail.getByRole('tab',{name:'Team 设置',exact:true}).waitFor();
 assert.equal(await rail.getByRole('tab',{name:'总览',exact:true}).getAttribute('aria-selected'),'true');
 assert.equal(await rail.locator('[data-primary="true"]').allTextContents().then(items=>items.join(',')),'总览,Team 设置');
 await rail.locator('[data-instance-id="609"]').waitFor();
 await page.getByTestId('metric-tokens').getByText('5万',{exact:true}).waitFor();
 await rail.evaluate(node=>window.workspaceRail=node);
 const card=rail.locator('[data-instance-id="609"]');
 await card.hover();
 const entry=card.getByTestId('instance-quick-actions').getByRole('link',{name:/进入实例/});
 assert.ok(await entry.isVisible(),'hover entry is unavailable');
 await card.locator('.instance-more').click();
 const menu=card.getByRole('menu');
 await menu.waitFor();
 assert.deepEqual((await menu.getByRole('menuitem').allTextContents()).map(text=>text.trim()),['实例详情','进入实例','重启实例','重置实例']);
 await page.keyboard.press('Escape');
 const preview=page.getByTestId('instance-hover-details');
 await page.mouse.move(1,1);
 const cardHeight=(await card.boundingBox()).height;
 await card.hover();
 await page.waitForTimeout(950);
 assert.equal(await preview.count(),0,'instance details opened before the hover delay');
 await preview.getByText('深度任务智能体',{exact:true}).waitFor();
 assert.equal(await preview.getAttribute('data-side'),'right');
 assert.equal((await card.boundingBox()).height,cardHeight,'the card expanded in the list');
 const watermark=preview.locator('.instance-detail-watermark img');
 assert.ok(await watermark.evaluate(img=>img.complete&&img.naturalWidth>0));
 assert.ok((await watermark.boundingBox()).x>(await preview.locator('h3').boundingBox()).x);
 await preview.hover();
 await page.waitForTimeout(220);
 assert.ok(await preview.isVisible(),'leaving the card for its details dismissed the preview');
 await page.keyboard.press('Escape');
 await preview.waitFor({state:'detached'});
 assert.equal(await card.locator('.instance-card-expand').count(),0);
 await card.locator('.instance-summary').click();
 await page.waitForURL(/\/ieisystem\/instances\/609(?:\?|$)/);
 await root().waitFor();
 assert.equal(await card.locator('.instance-summary').getAttribute('aria-current'),'page');
 await rail.getByRole('tab',{name:/^Team ·/}).click();
 await rail.locator('[data-team-id="184"] .ot-card-title').click();
 await page.waitForURL(/\/ieisystem\/teams\/184(?:\?|$)/);
 await page.locator('.team-chat-workspace[data-mode="single"]').waitFor();
 assert.ok(await rail.evaluate(node=>window.workspaceRail===node),'workspace navigation remounted the sidebar');
 const worker=rail.getByRole('link',{name:/成员 2.*#618/});
 await worker.click();
 await page.waitForURL(/\/ieisystem\/instances\/618(?:\?|$)/);
 await root().waitFor();
 assert.equal(await worker.getAttribute('aria-current'),'page');
 assert.equal(await page.getByRole('link',{name:'返回团队',exact:true}).count(),0);
 await rail.locator('[data-team-id="184"] .ot-card-title').click();
 await page.waitForURL(/\/ieisystem\/teams\/184(?:\?|$)/);
 await page.locator('.team-chat-workspace').waitFor();
 const composer=page.locator('.team-chat-workspace textarea').first();
 await composer.fill('尚未发送的任务');
 await rail.getByRole('tab',{name:'总览',exact:true}).click();
 await page.getByRole('alert').filter({hasText:'请先保存或关闭当前编辑'}).waitFor();
 assert.equal(await composer.inputValue(),'尚未发送的任务');
 await composer.fill('');
 await rail.getByRole('tab',{name:'总览',exact:true}).click();
 await page.getByTestId('metric-tokens').waitFor();
 await page.goBack();
 await page.locator('.team-chat-workspace').waitFor();
 await page.reload();
 await worker.waitFor();
 await checkOverflow();
 for(const route of ['/ieisystem/teams/184','/teams/184']){
  await page.setViewportSize({width:1440,height:1000});await page.goto(origin+route);const w=page.locator('.team-chat-workspace');await w.waitFor();
  const ownerRoute=route.includes('ieisystem');
  assert.equal(await w.locator('.team-chat-header-actions .cm-team-header-actions').count(),ownerRoute?1:0);
  assert.equal(await page.locator('.cm-team-header-actions').count(),1);
  await w.locator('.team-header-files-trigger').click();await w.locator('.team-file-row').first().waitFor();
  const showTerminal=async()=>{
   if(ownerRoute){await w.locator('.owner-team-tool-views').getByRole('button',{name:'实例工具',exact:true}).click();await toolPane().getByRole('button',{name:'终端',exact:true}).click();}
   else await w.locator('.team-tools-toolbar').getByRole('button',{name:'终端',exact:true}).click();
  };
  await showTerminal();
  const selector=toolPane().getByRole('combobox',{name:'工具运行成员'});
  for(const id of route.includes('ieisystem')?[616,617,618,619]:[616]){
   console.log('Checking Team terminal',route,id);
   await selector.selectOption(String(id));await toolPane().getByRole('button',{name:'新建终端',exact:true}).click();await toolPane().locator('.xterm').waitFor();
   assert.ok([...sessions.values()].some(s=>s.instance===id));
  }
  const before=sessions.size;
  await w.locator('.team-tools-toolbar').getByRole('button',{name:'文件',exact:true}).click();await w.locator('.team-file-row').first().waitFor();
  await showTerminal();await toolPane().locator('.xterm').waitFor();assert.equal(sessions.size,before,'restoring a terminal created a duplicate session');
  for(const width of [1440,1280,768,390,320]){await page.setViewportSize({width,height:1000});await checkOverflow();await page.screenshot({path:path.join(screenshots,(route.includes('ieisystem')?'owner':'cm')+'-team-'+width+'.png')});}
 }
 for(const locale of ['zh','en','ja','ko','de']){
  await page.evaluate(locale=>sessionStorage.setItem('workbench-test-locale',locale),locale);
  await page.goto(origin+'/ieisystem/instances/609');
  const panels=root().locator('.wb-panels [role="tab"]');await panels.last().click();
  assert.equal(await panels.count(),3,'merged workbench navigation changed in '+locale);
  for(const width of [1440,1280,768,390]){
   await page.setViewportSize({width,height:1000});await checkOverflow();
   await page.screenshot({path:path.join(screenshots,'merged-preview-'+locale+'-'+width+'.png')});
  }
 }
 const ownerCalls=calls.filter(c=>c.path.startsWith('/api/v1/ieisystem/')&&c.path.includes('/workbench/'));
 assert.ok(ownerCalls.length>0);assert.ok(ownerCalls.every(c=>!c.headers.authorization),'owner workbench leaked CM JWT');
 assert.ok(ownerCalls.filter(c=>c.method!=='GET').every(c=>c.headers['x-owner-action']==='1'),'missing explicit owner action');
 assert.equal(calls.filter(c=>/\/teams\/184\/tasks/.test(c.path)&&c.method==='POST').length,0,'tool selection dispatched a model task');
 assert.deepEqual(errors,[]);
 console.log(JSON.stringify({result:'PASS',checks:'owner-only auth, paginated switch, all members, browser, four Runtime terminals, session restore, CM/owner Team, widths, no dispatch',sessions:sessions.size}));
}catch(error){await page.screenshot({path:path.join(screenshots,'failure.png')});console.log((await page.locator('.team-workspace-tools').innerText().catch(()=>page.locator('body').innerText())).slice(0,2200));throw error;}finally{await browser.close();}
