import assert from 'node:assert/strict';
import fs from 'node:fs';
import {chromium} from 'playwright';
const origin=process.env.OWNER_WORKBENCH_ORIGIN||'http://127.0.0.1:9178';
const browser=await chromium.launch({channel:'chrome',headless:true});
const owner='portal-fixture@example.com',now=new Date().toISOString();
const instances=[{id:1,name:'Ordinary instance',type:'hermes'},{id:2,name:'Team member',type:'opencode',team_id:186}].map(i=>({...i,owner,status:'running',runtime_generation:3,instance_mode:'lite',workspace_available:true,created_at:now,updated_at:now}));
const members=[{id:1,team_id:186,instance_id:2,member_key:'leader',display_name:'Team member',role:'leader',runtime_type:'opencode',instance_mode:'lite',status:'idle',availability:'idle'}];
const team={id:186,name:'Portal tools fixture',status:'running',communication_mode:'leader_mediated',shared_mount_path:'/team',created_at:now,updated_at:now};
const calls=[],projects=new Map();
fs.mkdirSync('e2e/reports/owner-portal-controls',{recursive:true});
let browserTools=true;
try{
 const page=await browser.newPage({viewport:{width:1440,height:900}});page.setDefaultTimeout(12000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.addInitScript(()=>{if(!localStorage.getItem('clawmanager_locale'))localStorage.setItem('clawmanager_locale','zh')});
 await page.route('**/fixture-agent/**',r=>r.fulfill({contentType:'text/html',body:'<p>Native agent fixture</p>'}));
 await page.route('**/api/**',async route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname;calls.push({path:p,method:r.method(),body:r.postData(),headers:r.headers()});
  let data={};
  if(p.endsWith('/session')||p.endsWith('/session/refresh'))data={owner,expires_at:new Date(Date.now()+3600000).toISOString()};
  if(p.endsWith('/instances'))data={instances:[instances[0]],total:1};
  if(p.endsWith('/team-capabilities'))data={enabled:true,can_create:true};
  if(p.endsWith('/teams'))data={teams:[{...team,members,member_count:1,available_count:1}],total:1};
  if(p.endsWith('/teams/186'))data={team,members,tasks:[],events:[],work_items:[]};
  if(p.endsWith('/tasks'))data={tasks:[],has_more:false};
  if(p.endsWith('/events'))data={events:[],has_more:false};
  if(p.endsWith('/dashboard'))data={summary:{questions:0,total_tokens:0,sessions:0},trend:[],last_activity:{}};
  if(p.endsWith('/growth'))data={xp:0,level:1,tasks:[],ledger:[],days:[]};
  if(p.endsWith('/reports'))data={start:'2026-11-16',end:'2026-11-22',ongoing:true,counts:{tokens:0},previous:{},runtime:[],days:[]};
  if(p.endsWith('/workspace/files'))data={entries:[{name:'.git',path:'.git',is_dir:true,size:0}]};
  const m=/\/instances\/(\d+)(.*)$/.exec(p);
  if(m){const id=Number(m[1]),suffix=m[2];
   if(!suffix)data={instance:instances.find(i=>i.id===id)};
   if(suffix==='/access')data={access_url:origin+'/fixture-agent/'+id,expires_at:new Date(Date.now()+3600000).toISOString(),workspace_available:true};
   if(suffix==='/workbench/capabilities')data={generation:3,git:true,preview:browserTools,terminal:browserTools};
   if(suffix==='/workbench/git'){
    const q=r.postDataJSON();
    if(q.operation==='projects')data={projects:projects.has(id)?[projects.get(id)]:[]};
    if(q.operation==='import'){const project={path:q.path||'.',name:q.name||'Fixture repo '+id};projects.set(id,project);data={project};}
    if(q.operation==='status')data={status:{branch:'main',ahead:0,behind:0,changes:[]}};
    if(q.operation==='branches')data={branches:[{name:'main',current:true,remote:false}]};
    if(q.operation==='history')data={history:[]};
   }
  }
  await route.fulfill({json:{success:true,data}});
 });
 await page.goto(origin+'/ieisystem/list-instances');
 const account=page.locator('.portal-unified-account');
 await account.locator('summary').click();
 const email=account.getByText(owner,{exact:true});await email.click();
 assert.equal(await account.evaluate(el=>el.open),true,'email click must keep the menu stable');
 await email.dblclick();assert.ok(await email.isVisible());
 await page.keyboard.press('Escape');
 assert.equal(await account.evaluate(el=>el.open),false,'Escape must close after selecting static email text');
 await account.locator('summary').click();
 await account.getByRole('button',{name:'刷新',exact:true}).focus();
 assert.equal(await account.evaluate(el=>el.open),true);
 await page.keyboard.press('Escape');assert.equal(await account.evaluate(el=>el.open),false);
 assert.ok(await account.locator('summary').evaluate(el=>el===document.activeElement));
 await account.locator('summary').click();await page.getByTestId('owner-overview-summary').click();
 assert.equal(await account.evaluate(el=>el.open),false,'outside click closes the account menu');
 await page.goto(origin+'/ieisystem/instances/1');
 const workbench=page.locator('.owner-instance-workbench .wb-root');
 await workbench.getByRole('tab',{name:'Git 项目',exact:true}).click();
 await workbench.getByRole('button',{name:'导入已有项目',exact:true}).last().click();
 await workbench.getByRole('option',{name:/^Fixture repo 1 ·/}).waitFor({state:'attached'});
 const frame=page.locator('.portal-unified-instance-panes>section').first();
 const rail=page.locator('.portal-unified-rail');
 const tools=page.locator('#iei-instance-workspace');
 for(const width of [1440,1280]){
  await page.setViewportSize({width,height:900});
  const [a,b,c,content]=await Promise.all([rail.boundingBox(),frame.boundingBox(),tools.boundingBox(),page.locator('.portal-unified-content').boundingBox()]);
  assert.ok(Math.abs(a.y-b.y)<1&&Math.abs(a.y-c.y)<1,'three panes align at the top');
  assert.ok(Math.abs(b.x-content.x)<1&&Math.abs(c.x+c.width-content.x-content.width)<1,'no extra frame padding around the two right panes');
 }
 await page.screenshot({path:'e2e/reports/owner-portal-controls/instance-git-desktop.png'});
 // Git is an independent capability; absence of web/terminal must not hide it.
 browserTools=false;await page.reload();await workbench.getByRole('tab',{name:'Git 项目',exact:true}).click();
 await workbench.getByRole('option',{name:/^Fixture repo 1 ·/}).waitFor({state:'attached'});
 await page.goto(origin+'/ieisystem/teams/186');
 const chat=page.locator('.team-chat-workspace');await chat.locator('.team-header-files-trigger').click();
 await chat.locator('.team-tools-toolbar').getByRole('button',{name:'实例工具',exact:true}).click();
 const toolPane=page.locator('.team-workspace-tools');
 await toolPane.getByRole('button',{name:'Git 项目',exact:true}).click();
 await toolPane.getByRole('button',{name:'导入已有项目',exact:true}).last().click();
 await toolPane.getByRole('option',{name:/^Fixture repo 2 ·/}).waitFor({state:'attached'});
 assert.ok(calls.some(c=>c.path==='/api/v1/ieisystem/instances/2/workbench/git'));
 await page.screenshot({path:'e2e/reports/owner-portal-controls/team-git-desktop.png'});
 assert.equal(calls.filter(c=>/\/tasks$/.test(c.path)&&c.method==='POST').length,0);
 const gitCalls=calls.filter(c=>c.path.endsWith('/workbench/git'));
 assert.ok(gitCalls.every(c=>!c.headers.authorization&&c.headers['x-owner-action']==='1'));
 for(const width of [1440,1280,768,390]){
  await page.setViewportSize({width,height:900});
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'page overflow at '+width);
 }
 assert.deepEqual(errors,[]);
 await page.screenshot({path:'e2e/reports/owner-portal-controls/team-git-mobile.png'});
 await page.evaluate(()=>localStorage.setItem('clawmanager_locale','en'));
 await page.goto(origin+'/ieisystem/instances/1');
 await workbench.getByRole('tab',{name:'Git projects',exact:true}).click();
 await workbench.getByRole('option',{name:/^Fixture repo 1 ·/}).waitFor({state:'attached'});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 console.log('PASS account email/focus, three pane layout, owner/Team Git import, capability isolation and narrow screens');
}finally{await browser.close();}
