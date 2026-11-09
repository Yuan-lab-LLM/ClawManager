// Exact local App image + MySQL/Redis + real IEI authentication and Team/file
// HTTP routes. Seeded chat history is a fixture, not real model acceptance.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {createCipheriv,createHmac,randomBytes} from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {chromium} from 'playwright';

const image=process.env.OWNER_TEAM_IMAGE||'10.130.14.23:5000/clawmanager-hxc-app:team-northbound-owner-20261023-rc1';
const prefix=`owner-team-qa-${process.pid}`,names={mysql:`${prefix}-mysql`,redis:`${prefix}-redis`,app:`${prefix}-app`};
const containers=[],jwtSecret=randomBytes(32).toString('hex'),sessionSecret=randomBytes(32).toString('hex');
const aesKey='fixture-key-1234',aesIV='fixture-iv-12345',owner='alice@example.com';
let browser,network=false,volume=false;
let kubeFixture;
function docker(args,input){const r=spawnSync('docker',args,{input,encoding:'utf8',timeout:120000});if(r.status!==0){const safe=(r.stderr||'').split('\n').filter(x=>! /password|secret|token|bearer|authorization/i.test(x)).join('\n');throw Error(`Fixture Docker ${args[0]} failed: ${safe.slice(-1500)}`)}return r.stdout.trim();}
function sql(q){return docker(['exec','-i',names.mysql,'mysql','--default-character-set=utf8mb4','-uroot','-pdisposable-local-only','-N','-B','clawmanager'],q)}
const escape=s=>String(s).replaceAll('\\','\\\\').replaceAll("'","''");
const env=e=>Object.entries(e).flatMap(([k,v])=>['-e',`${k}=${v}`]);
function start(name,img,args=[]){docker(['run','-d','--name',name,'--label',`codex.task=${prefix}`,'--network',prefix,...args,img]);containers.push(name)}
async function until(fn,what){const end=Date.now()+90000;while(Date.now()<end){try{if(await fn())return}catch{}await new Promise(r=>setTimeout(r,500))}throw Error(`${what} timed out`)}
function externalToken(email){const stamp=new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(new Date());const cipher=createCipheriv('aes-128-cbc',Buffer.from(aesKey),Buffer.from(aesIV));return Buffer.concat([cipher.update(`${email}+${stamp}`,'utf8'),cipher.final()]).toString('base64')}
function cmToken(){const e=o=>Buffer.from(JSON.stringify(o)).toString('base64url');const value=`${e({alg:'HS256',typ:'JWT'})}.${e({user_id:77,token_type:'access',iat:Math.floor(Date.now()/1000),exp:Math.floor(Date.now()/1000)+3600})}`;return `${value}.${createHmac('sha256',jwtSecret).update(value).digest('base64url')}`}
try{
 docker(['network','create',prefix]);network=true;docker(['volume','create',`${prefix}-data`]);volume=true;
 start(names.mysql,'mysql:8.4',['--tmpfs','/var/lib/mysql:rw,size=768m',...env({MYSQL_ROOT_PASSWORD:'disposable-local-only',MYSQL_DATABASE:'clawmanager'})]);start(names.redis,'redis:7-alpine');
 await until(()=>sql('SELECT 1')==='1','MySQL');
 const kubeArgs=[],kubeEnv={};
 if(process.env.TEAM_PRESENCE_ACCEPTANCE==='1'){
  kubeFixture=fs.mkdtempSync(path.join(os.tmpdir(),'owner-observation-'));
  fs.copyFileSync('fixtures/team-observation-k8s.mjs',path.join(kubeFixture,'server.mjs'));
  fs.writeFileSync(path.join(kubeFixture,'config'),`apiVersion: v1\nkind: Config\nclusters:\n- name: fixture\n  cluster:\n    server: http://${prefix}-k8s:8080\ncontexts:\n- name: fixture\n  context:\n    cluster: fixture\n    user: fixture\ncurrent-context: fixture\nusers:\n- name: fixture\n  user: {}\n`);
  docker(['run','-d','--name',`${prefix}-k8s`,'--network',prefix,'-v',`${kubeFixture}:/fixture:ro`,'--entrypoint','node','node:24-alpine','/fixture/server.mjs']);containers.push(`${prefix}-k8s`);
  kubeArgs.push('-v',`${kubeFixture}:/fixture:ro`);kubeEnv.KUBECONFIG='/fixture/config';kubeEnv.K8S_MODE='outofcluster';kubeEnv.CLAWMANAGER_LEADER_ELECTION='false';
 }
 start(names.app,image,['-p','127.0.0.1::8443','-v',`${prefix}-data:/workspaces`,...kubeArgs,...env({DB_HOST:names.mysql,DB_USER:'root',DB_PASSWORD:'disposable-local-only',DB_NAME:'clawmanager',JWT_SECRET:jwtSecret,RUNTIME_NAMESPACE:'owner-team-isolated',RUNTIME_SCHEDULER_ENABLED:'false',PLATFORM_REDIS_URL:`redis://${names.redis}:6379/0`,TEAM_REDIS_URL:`redis://${names.redis}:6379/0`,IEISYSTEM_SSO_ENABLED:'true',IEISYSTEM_SSO_KEY:aesKey,IEISYSTEM_SSO_IV:aesIV,IEISYSTEM_SESSION_SECRET:sessionSecret,...kubeEnv})]);
 await until(()=>sql("SELECT COUNT(*) FROM schema_migrations WHERE filename='077_add_northbound_owner_teams.sql'")==='1','App migration077');
 await until(()=>docker(['inspect','-f','{{.State.Status}}',names.app])==='running' && JSON.parse(docker(['inspect','-f','{{json .NetworkSettings.Ports}}',names.app]))['8443/tcp']?.length,'App process and binding');
 const port=JSON.parse(docker(['inspect','-f','{{json .NetworkSettings.Ports}}',names.app]))['8443/tcp'][0].HostPort,origin=`https://127.0.0.1:${port}`;
 sql("INSERT INTO users(id,username,email,password_hash,role,is_active) VALUES(77,'owner-fixture','provider@local.invalid','not-a-login','user',1)");
 sql("UPDATE northbound_admin_settings SET owner_teams_enabled=1,owner_team_creation_user_id=77 WHERE id=1");
 sql(`INSERT INTO teams(id,user_id,name,display_name,owner,resource_scope,status,communication_mode) VALUES(1,77,'nb-internal-secret','AI 资讯简报组','${owner}','northbound','running','leader_mediated'),(2,77,'原 CM 团队',NULL,NULL,'cm','running','leader_mediated'),(3,77,'nb-foreign','其他所有者团队','bob@example.com','northbound','running','leader_mediated')`);
 sql(`INSERT INTO instances(id,user_id,owner,name,type,runtime_type,instance_mode,status,cpu_cores,memory_gb,disk_gb,os_type,os_version) VALUES(101,77,'${owner}','fixture-leader','openclaw','gateway','lite','error',2,4,5,'openclaw','latest'),(102,77,'${owner}','fixture-worker','hermes','gateway','lite','error',2,4,5,'hermes','latest')`);
 sql("INSERT INTO team_members(id,team_id,user_id,instance_id,member_key,display_name,role,runtime_type,instance_mode,status,availability) VALUES(1001,1,77,101,'leader','主编','leader','openclaw','lite','idle','unknown'),(1002,1,77,102,'worker','资讯筛选员','worker','hermes','lite','idle','unknown')");
 if(process.env.TEAM_PRESENCE_ACCEPTANCE==='1'){
  sql("UPDATE instances SET status='running',runtime_generation=1 WHERE id IN (101,102)");
  for(const [index,type,instance,member] of [[1,'openclaw',101,'leader'],[2,'hermes',102,'worker']]){
   sql(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,state,capacity,used_slots,last_seen_at) VALUES(${900+index},'${type}','owner-team-isolated','pod-${type}','pod-${type}','10.1.2.${index+2}','${type}','registry/${type}:fixture','ready',100,1,UTC_TIMESTAMP())`);
   sql(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation,last_health_at) VALUES(${instance},${900+index},'${type}','gw-${instance}-1',20000,'/workspaces/${type}/user-77/instance-${instance}','running',1,UTC_TIMESTAMP())`);
   docker(['exec',names.redis,'redis-cli','HSET','claw:team:1:presence',member,JSON.stringify({teamId:'1',memberId:member,liveness:'online',lastSeenAt:new Date().toISOString()})]);
  }
 }
 const prompt=JSON.stringify({prompt:'请生成 AI 简报',user_key:`owner-${owner}`,user_label:owner});
 const answer=JSON.stringify({resultMarkdown:'简报已完成。请查看 [报告](/team/report.md)。',summary:'简报已完成'});
 sql(`INSERT INTO team_tasks(id,team_id,target_member_id,created_by,message_id,status,workflow_state,payload_json,result_json) VALUES(1,1,1001,77,'seeded-history','succeeded','completed','${escape(prompt)}','${escape(answer)}')`);
 sql(`INSERT INTO team_events(team_id,task_id,member_id,event_type,payload_json) VALUES(1,1,1001,'task_completed','${escape(JSON.stringify({from:'leader',text:'简报已完成。',resultMarkdown:'简报已完成。',chatPolicy:'visible'}))}'),(1,1,1001,'reply','${escape(JSON.stringify({text:'PRIVATE_ANALYSIS_SENTINEL',channel:'analysis'}))}')`);
 const root='/workspaces/teams/user-77/team-1-shared';docker(['exec',names.app,'mkdir','-p',root]);docker(['exec','-i',names.app,'tee',`${root}/report.md`],'# 原始报告\n');
 const spec={schemaVersion:1,name:'Owner 模板',summary:'fixture duties',resolvedMemberCount:2,members:[{memberId:'leader',displayName:'模板主编',role:'leader',isLeader:true},{memberId:'worker',displayName:'模板编辑',role:'worker',isLeader:false}].map(m=>({...m,summary:'负责本角色任务',mission:'按要求完成并正式提交',responsibilities:['核对资料'],boundaries:['保留原始引用'],expectedInputs:['用户目标'],deliverables:['正文结果'],acceptanceCriteria:['结果可核对'],collaborationNotes:['遵循 Leader 分工'],capabilityTags:['research']}))};
 if(process.env.TEAM_COMPACT_TEMPLATE_ACCEPTANCE==='1'){
  for(const [index,name] of ['境外采集员','渠道分析师','风险核验员','报告编辑员'].entries())spec.members.push({...spec.members[1],memberId:'extra-'+index,displayName:name});
  spec.resolvedMemberCount=6;
 }
 sql(`INSERT INTO custom_team_templates(id,user_id,resource_scope,owner,name,intent,resolved_member_count,spec_json,revision) VALUES(1,77,'northbound','${owner}','Owner 模板','fixture intent',${spec.resolvedMemberCount},'${escape(JSON.stringify(spec))}',1)`);
 browser=await chromium.launch({channel:'chrome',headless:true});const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:1000}});
 const login=await context.request.post(`${origin}/api/v1/ieisystem/session`,{data:{token:externalToken(owner)}});assert.equal(login.status(),200);
 await until(async()=> (await (await context.request.get(`${origin}/api/v1/ieisystem/team-capabilities`)).json()).data.enabled,'owner feature enabled');
 const list=await context.request.get(`${origin}/api/v1/ieisystem/teams`);assert.equal((await list.json()).data.total,1);
 if(process.env.TEAM_PRESENCE_ACCEPTANCE==='1'){
  const summaries=(await list.json()).data.teams;assert.equal(summaries[0].available_count,2,JSON.stringify(summaries[0]));
  assert.ok(summaries[0].members.every(m=>!m.last_seen_at&&m.runtime_observation.state==='online'),'first task was required');
  const old=await context.request.get(`${origin}/api/v1/ieisystem/teams/1/members`);assert.equal(old.status(),200);assert.ok((await old.json()).data.every(m=>m.runtime_observation.state==='online'));
 }
 assert.equal((await context.request.get(`${origin}/api/v1/ieisystem/teams/2`)).status(),404);assert.equal((await context.request.get(`${origin}/api/v1/ieisystem/teams/3`)).status(),404);
 const instances=await (await context.request.get(`${origin}/api/v1/ieisystem/instances`)).json();assert.equal(instances.data.total,0,'Team members must not appear as ordinary instances');
 const cm=await context.request.get(`${origin}/api/v1/teams/1`,{headers:{Authorization:`Bearer ${cmToken()}`}});assert.equal(cm.status(),404,'old CM route must reject owner Team');
 const details=await context.request.get(`${origin}/api/v1/ieisystem/teams/1`);const raw=await details.text();assert.ok(!/PRIVATE_ANALYSIS_SENTINEL|nb-internal-secret|redis_url|team_token_secret|shared_pvc_name/.test(raw));
 assert.equal((await context.request.post(`${origin}/api/v1/ieisystem/teams`,{headers:{Origin:'https://foreign.invalid'},data:{}})).status(),403);
 const page=await context.newPage();page.setDefaultTimeout(15000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
 let rendererRequests=0;
 if(process.env.TEAM_PREVIEW_ACCEPTANCE==='1'){
  for(const route of ['/index.html','/ieisystem/teams/1','/teams/1']){const response=await context.request.get(origin+route);assert.equal(response.status(),200);assert.match(response.headers()['cache-control'],/no-store/)}
  const missing=await context.request.get(origin+'/assets/nonexistent-previous-renderer.js');assert.equal(missing.status(),404);
  await page.route('**/assets/WorkspaceMarkdownPreview*',route=>{rendererRequests++;return route.abort()});
 }
 await page.route('**/api/v1/ieisystem/dashboard**',r=>r.fulfill({status:503,json:{message:'fixture unavailable statistics'}}));
 await page.goto(`${origin}/ieisystem/list-instances?view=overview&workspace_tab=teams&dashboard_tab=team&selected_team_id=1&expanded_team_id=1`);
 await page.getByRole('heading',{name:'团队空间',exact:true}).waitFor();assert.equal(await page.getByRole('heading',{name:'工作空间尚未分配'}).count(),0);

 if(process.env.TEAM_LIGHT_WORKBENCH_ACCEPTANCE==='1'){
  // Ordinary instances use the real owner list; Team members stay excluded.
  sql(`INSERT INTO instances(id,user_id,owner,name,type,runtime_type,instance_mode,status,cpu_cores,memory_gb,disk_gb,os_type,os_version) VALUES(120,77,'${owner}','portal-openclaw-fixture','openclaw','gateway','lite','stopped',2,4,5,'openclaw','latest'),(121,77,'${owner}','portal-hermes-fixture','hermes','gateway','lite','stopped',2,4,5,'hermes','latest')`);
  await page.reload();await page.locator('.ot-team-card').waitFor();
  await page.locator('.ot-team-card').getByRole('button',{name:'收起',exact:true}).click();
  const teamBox=await page.locator('.ot-team-card').boundingBox(),teamLogo=await page.locator('.ot-card-avatar').boundingBox();
  const neutral=await page.locator('.ot-team-card').evaluate(e=>getComputedStyle(e).backgroundColor);
  await page.locator('.ot-team-card').hover();assert.equal(await page.locator('.ot-team-card').evaluate(e=>getComputedStyle(e,'::before').animationName),'ot-edge-orbit');
  await page.emulateMedia({reducedMotion:'reduce'});assert.equal(await page.locator('.ot-team-card').evaluate(e=>getComputedStyle(e,'::before').animationName),'none');await page.emulateMedia({reducedMotion:'no-preference'});
  await page.getByRole('tab',{name:/^实例 ·/}).click();await page.locator('.portal-runtime-card').first().waitFor();
  const ordinaryBox=await page.locator('.portal-runtime-card').first().boundingBox(),ordinaryLogo=await page.locator('.instance-avatar').first().boundingBox();
  assert.ok(Math.abs(teamBox.width-ordinaryBox.width)<1,'Team/instance width mismatch');assert.ok(Math.abs(teamBox.height-ordinaryBox.height)<2,'Team/instance height mismatch');assert.equal(teamLogo.width,ordinaryLogo.width,'logo container mismatch');
  assert.equal(neutral,await page.locator('.portal-runtime-card').first().evaluate(e=>getComputedStyle(e).backgroundColor),'Team has tinted background');
  await page.getByRole('tab',{name:/^Team ·/}).click();await page.locator('.ot-card-title').click();
 }
 await page.locator('.ot-team-card').getByRole('link',{name:'进入 ↗',exact:true}).first().waitFor();
 assert.equal(await page.locator('.ot-team-card a[href="/ieisystem/instances/101"]').count(),1);
 const card=page.locator('.ot-team-card').first();
 assert.equal(await card.locator('.ot-card-watermark img').getAttribute('src'),'/team-expanded.png');
 assert.ok(await card.locator('.ot-card-watermark img').evaluate(e=>e.complete&&e.naturalWidth>0));
 assert.ok(Number(await card.locator('.ot-card-watermark img').evaluate(e=>getComputedStyle(e).opacity))>0);
 await card.screenshot({path:'../reports/owner-team-card-expanded.png'});
 await card.getByRole('button',{name:'收起',exact:true}).click();
 assert.equal(await card.getAttribute('data-expanded'),'false');
 assert.equal(await card.locator('.ot-card-avatar img').getAttribute('src'),'/team-square.png');
 const directEntry=card.getByRole('link',{name:'进入团队 AI 资讯简报组',exact:true});
 await directEntry.waitFor();
 await card.screenshot({path:'../reports/owner-team-card-collapsed.png'});
 await directEntry.click();await page.getByRole('link',{name:'我的团队空间'}).waitFor();
 await page.getByRole('link',{name:'我的团队空间'}).click();
 assert.equal(await card.getAttribute('data-expanded'),'false','enter must not toggle card');
 await card.locator('.ot-card-title').focus();await page.keyboard.press('Enter');
 await until(async()=>await card.getAttribute('data-expanded')==='true','keyboard Team card expansion');
 assert.ok(page.url().includes('expanded_team_id=1')&&page.url().includes('selected_team_id=1'));
 await card.locator('a[href="/ieisystem/instances/101"]').click();
 assert.ok(page.url().includes('/ieisystem/instances/101'),'member must enter northbound instance');
 await page.goBack();await card.locator('a[href="/ieisystem/instances/101"]').waitFor();
 if(process.env.TEAM_FAST_ACCEPTANCE!=='1'){
 await page.getByRole('button',{name:'新建 Team',exact:true}).first().click();const dialog=page.locator('dialog.ot-modal');await dialog.waitFor();await until(async()=>await dialog.evaluate(e=>e.matches(':modal')),'native create dialog');await until(async()=>await dialog.locator('select').count()>2,'template member selectors');await page.keyboard.press('Escape');await dialog.waitFor({state:'detached'});

 if(process.env.TEAM_LIGHT_WORKBENCH_ACCEPTANCE==='1'){
  await page.getByRole('button',{name:'管理',exact:true}).click();const management=page.getByRole('dialog',{name:'管理 Team',exact:true});await management.waitFor();await management.getByLabel('团队名称',{exact:true}).fill('未保存的管理草稿');
  assert.equal(await page.evaluate(()=>window.dispatchEvent(new Event('clawmanager:before-page-refresh',{cancelable:true}))),false);
  page.once('dialog',d=>d.dismiss());await management.getByRole('button',{name:'关闭管理 Team',exact:true}).click();assert.equal(await management.count(),1);page.once('dialog',d=>d.accept());await management.getByRole('button',{name:'关闭管理 Team',exact:true}).click();
  await page.getByRole('tab',{name:'团队模板',exact:true}).click();await page.getByLabel('模板名称',{exact:true}).waitFor();
  const field=page.getByLabel('团队目标与分工',{exact:true});await field.fill('刷新和切换时保留的模板草稿');await page.getByRole('button',{name:'刷新模板',exact:true}).click();await until(async()=>!(await page.getByRole('button',{name:'刷新模板',exact:true}).isDisabled()),'template refresh');assert.equal(await field.inputValue(),'刷新和切换时保留的模板草稿');
  await page.getByRole('tab',{name:'我的 Team',exact:true}).click();await page.getByRole('tab',{name:'团队模板',exact:true}).click();await field.waitFor();assert.equal(await field.inputValue(),'刷新和切换时保留的模板草稿','draft lost on tab switch');
  await page.locator('.ot-member-tab').first().click();await page.getByRole('button',{name:'调整职责',exact:true}).first().click();const adjust=page.getByRole('dialog',{name:'调整成员职责'});await adjust.getByLabel('调整说明').fill('保留引用来源');page.once('dialog',d=>d.accept());await adjust.getByRole('button',{name:'关闭调整成员职责',exact:true}).click();
  assert.equal(await page.locator('.ot-member-tab').count(),process.env.TEAM_COMPACT_TEMPLATE_ACCEPTANCE==='1'?6:2);
  if(process.env.TEAM_COMPACT_TEMPLATE_ACCEPTANCE==='1'){for(let i=0;i<6;i++){await page.locator('.ot-member-tab').nth(i).click();assert.equal(await page.locator('.ot-member-tab[aria-selected=true]').count(),1)}await page.getByRole('tab',{name:'协作与边界',exact:true}).click();await page.getByText('所需输入',{exact:true}).waitFor();await page.getByRole('tab',{name:'职责与交付',exact:true}).click();}
  for(const width of [1920,1440,1024,768,390,320]){
   await page.setViewportSize({width,height:900});const bounds=await page.locator('.ot-template-editor').evaluate(e=>{const w=e.closest('.ot-workspace').getBoundingClientRect(),b=e.getBoundingClientRect(),f=e.closest('.ot-template-area').querySelector('.ot-template-footer').getBoundingClientRect();return{outer:document.documentElement.scrollWidth>innerWidth+2,scroll:e.scrollHeight>e.clientHeight,editorBottom:b.bottom,footerTop:f.top,footerBottom:f.bottom,workBottom:w.bottom}});
   assert.equal(bounds.outer,false,'horizontal overflow '+width);assert.ok(bounds.editorBottom<=bounds.footerTop+1,'editor overlaps footer '+width);assert.ok(bounds.footerBottom<=bounds.workBottom+1,'footer exceeds panel '+width);
   await page.screenshot({path:`../reports/team-light-template-${width}.png`});
  }
  await page.setViewportSize({width:1440,height:1000});await field.fill('fixture intent');
 }
 await page.getByRole('tab',{name:'团队模板',exact:true}).click();await page.locator('.ot-template-list').getByRole('button',{name:/Owner 模板/}).click();await page.getByLabel('模板名称',{exact:true}).fill('Owner 模板已改名');await page.getByRole('button',{name:'仅保存名称',exact:true}).click();await until(()=>sql('SELECT revision FROM custom_team_templates WHERE id=1')==='2','template metadata CAS');
 await page.getByRole('tab',{name:/模板编辑/}).click();await page.getByRole('button',{name:'模板编辑图标',exact:true}).click();const icon=page.locator('.team-avatar-picker-options:popover-open [data-icon-choice]:not([data-icon-choice="auto"])').first();await icon.click();await until(()=>sql('SELECT revision FROM custom_team_templates WHERE id=1')==='3','template icon CAS');
 assert.equal((await context.request.patch(`${origin}/api/v1/ieisystem/custom-team-templates/1`,{data:{name:'stale name',expected_revision:1}})).status(),409);
 await page.getByText('更多',{exact:true}).click();page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'删除模板',exact:true}).click();await until(()=>sql('SELECT COUNT(*) FROM custom_team_templates WHERE id=1')==='0','template deletion');assert.equal(sql("SELECT display_name FROM teams WHERE id=1"),'AI 资讯简报组');
 if(process.env.TEAM_COMPACT_TEMPLATE_ACCEPTANCE==='1'){const layout=await page.locator('.ot-workspace').boundingBox();await page.getByRole('button',{name:'操作记录',exact:true}).click();const history=page.getByRole('dialog',{name:'操作记录',exact:true});await history.waitFor();assert.ok(await history.locator('.ot-notice-record').count()>0);await history.getByRole('button',{name:'关闭',exact:true}).click();assert.deepEqual(await page.locator('.ot-workspace').boundingBox(),layout);}await page.getByRole('tab',{name:'我的 Team',exact:true}).click();
 fs.mkdirSync('reports',{recursive:true});
 for(const width of [1920,1440,1024,768,390,320]){await page.setViewportSize({width,height:1000});await page.screenshot({path:`reports/owner-team-${width}.png`});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2),false,`horizontal overflow ${width}`)}
 await page.setViewportSize({width:1440,height:1000});await page.emulateMedia({colorScheme:'dark'});await page.evaluate(()=>document.documentElement.classList.add('dark'));await page.screenshot({path:'reports/owner-team-dark.png'});await page.evaluate(()=>document.documentElement.classList.remove('dark'));await page.emulateMedia({colorScheme:'light'});
 }
 await page.setViewportSize({width:1440,height:1000});await page.getByRole('tab',{name:'我的 Team',exact:true}).click();await page.getByRole('link',{name:/进入团队对话/}).click();const workspace=page.locator('.team-chat-workspace:visible');await workspace.waitFor();
 assert.equal(await page.locator('.ot-northbound-layout').count(),1);assert.equal(await page.getByText('PRIVATE_ANALYSIS_SENTINEL').count(),0);
 await workspace.locator('.team-tools-toolbar').getByRole('button',{name:'文件',exact:true}).click();const files=page.locator('.team-files-host:visible');
 if(process.env.TEAM_PREVIEW_ACCEPTANCE==='1'){
  const composer=page.getByRole('textbox',{name:'任务内容'});await composer.fill('预览时保留的草稿');
  await files.getByRole('button',{name:'report.md',exact:true}).click();
  const preview=page.locator('[data-workspace-preview]');await preview.getByRole('heading',{name:'原始报告',exact:true}).waitFor();
  await preview.getByRole('button',{name:'关闭',exact:true}).click();assert.equal(await composer.inputValue(),'预览时保留的草稿');await composer.fill('');
 }
 await files.locator('.team-file-row').filter({has:page.getByRole('button',{name:'report.md',exact:true})}).getByRole('button',{name:'编辑内容',exact:true}).click();
 const editor=page.locator('[data-workspace-editor]');await editor.locator('.cm-content').fill('# 北向实时编辑完成\n');await editor.getByRole('button',{name:'保存',exact:true}).click();await editor.getByText('已保存。不会自动重启实例或执行脚本。').waitFor();await editor.getByRole('button',{name:'关闭编辑器'}).click();
 assert.equal(docker(['exec',names.app,'cat',`${root}/report.md`]),'# 北向实时编辑完成');
 const text=(await (await context.request.get(`${origin}/api/v1/ieisystem/teams/1/workspace/text?path=report.md`)).json()).data;
 assert.equal((await context.request.put(`${origin}/api/v1/ieisystem/teams/1/workspace/text`,{data:{path:'report.md',text:'wrong',version:'stale'}})).status(),409);
 assert.equal((await context.request.get(`${origin}/api/v1/ieisystem/teams/1/workspace/text?path=..%2Foutside`)).status(),422);
 assert.ok(text.text.includes('北向实时编辑完成'));
 if(process.env.TEAM_PREVIEW_ACCEPTANCE==='1'){
  assert.equal(rendererRequests,0,'preview still requests a first-click renderer module');
  const composer=page.getByRole('textbox',{name:'任务内容'});await composer.fill('尚未发送的草稿');
  await page.route('**/index.html',async route=>{const response=await route.fetch();let html=await response.text();html=html.replace(/(<script[^>]*src=")[^"]+/, '$1/assets/future-version-fixture.js');await route.fulfill({response,body:html})});
  await page.evaluate(()=>window.dispatchEvent(new Event('focus')));await page.locator('[data-page-version-notice]').waitFor();
  await page.getByRole('button',{name:'刷新页面',exact:true}).click();await page.getByText('请先保存文件、复制或处理未发送内容，再刷新。当前内容已保留。',{exact:true}).waitFor();assert.equal(await composer.inputValue(),'尚未发送的草稿');
  await composer.fill('');await page.unroute('**/index.html');
 }
 await page.getByRole('link',{name:'我的团队空间'}).click();await page.getByRole('heading',{name:'团队空间',exact:true}).waitFor();assert.ok(page.url().includes('selected_team_id=1'));
 assert.equal(sql('SELECT COUNT(*) FROM team_tasks'),'1','navigation/file actions must not dispatch');
 // Runtime/model boundary is simulated by a protocol4 formal tool receipt;
 // actual App dispatch, Redis consumer, validation, history and UI are real.
 const send={message_id:'owner-worker-formal-fixture',target_member_id:'worker',execution_type:'single_member',context_task_id:0,payload:{prompt:'只回答，不生成文件'}};
 const dispatch=await context.request.post(`${origin}/api/v1/ieisystem/teams/1/tasks`,{data:send});assert.equal(dispatch.status(),200,await dispatch.text());const task=(await dispatch.json()).data;
 const replay=await context.request.post(`${origin}/api/v1/ieisystem/teams/1/tasks`,{data:send});assert.equal((await replay.json()).data.id,task.id);
 const event={protocolVersion:4,event:'completion_proposed',eventId:'owner-fixture-proposal',completionId:'owner-fixture-completion',attemptId:'attempt',memberId:'worker',taskId:`team-1-task-${task.id}`,rootTaskId:`team-1-task-${task.id}`,rootMessageId:send.message_id,assignmentId:'single-member',revision:1,status:'succeeded',summary:'正式交付完成',resultMarkdown:'这是本次正式答案，无需额外生成文件。',explicitCompletion:true,completionSource:'team_complete_task'};
 docker(['exec',names.redis,'redis-cli','XADD','claw:team:1:events','*','payload',JSON.stringify(event)]);
 await until(()=>sql(`SELECT status FROM team_tasks WHERE id=${task.id}`)==='succeeded','owner task accepted formal completion');
 assert.equal(sql(`SELECT COUNT(*) FROM team_work_items WHERE root_task_id=${task.id}`),'1');
 await page.goto(`${origin}/ieisystem/teams/1`);await page.getByText(event.resultMarkdown,{exact:true}).waitFor();
 docker(['exec',names.redis,'redis-cli','XADD','claw:team:1:events','*','payload',JSON.stringify({...event,event:'progress',eventId:'late-fixture-progress',status:'running',explicitCompletion:false})]);
 await new Promise(r=>setTimeout(r,800));assert.equal(sql(`SELECT status FROM team_tasks WHERE id=${task.id}`),'succeeded');
 await context.request.delete(`${origin}/api/v1/ieisystem/session`);assert.equal((await context.request.get(`${origin}/api/v1/ieisystem/teams`)).status(),401);
 assert.deepEqual(errors,[]);console.log(process.env.TEAM_FAST_ACCEPTANCE==='1'?'PASS final image focused: no-task independent liveness/list+members, IEI/owner isolation, actual Markdown preview+retained draft, HTML cache policy/404, no first-click renderer request, file editor/CAS, version refresh guard, formal completion+late protection (simulated Runtime/model)':'PASS exact image: IEI AES/cookie, owner/type isolation, hidden events, member filtering, real file CAS/editor, master-detail/6 widths/back, no accidental sends, actual Redis formal completion+late terminal protection (simulated Runtime/model), logout');
}catch(error){if(containers.includes(names.app)){const logs=spawnSync('docker',['logs',names.app,'--tail','200'],{encoding:'utf8'});const safe=(logs.stdout+logs.stderr).split('\n').filter(x=>/fatal|panic|migration|error/i.test(x)&&!/password|secret|token|bearer|authorization|@tcp/i.test(x));console.log(safe.slice(-8).join('\n'))}throw error}
finally{await browser?.close();for(const name of containers.reverse())try{docker(['rm','-f',name])}catch{}if(volume)try{docker(['volume','rm',`${prefix}-data`])}catch{}if(network)try{docker(['network','rm',prefix])}catch{}if(kubeFixture)fs.rmSync(kubeFixture,{recursive:true,force:true})}
