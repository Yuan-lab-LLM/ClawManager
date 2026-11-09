// Real built App + MySQL + Redis + installed Runtime tools and production UI.
// Isolated local fixtures only; zero real model calls / live tenant mutations.
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHmac, randomBytes } from 'node:crypto';
import fs from 'node:fs';
import { chromium } from 'playwright';

const tag=process.env.TEAM_FOLLOWUP_TAG || 'team-member-followup-20261010-v1';
const runtimeTag=process.env.TEAM_FOLLOWUP_RUNTIME_TAG || tag;
const image=process.env.TEAM_FOLLOWUP_APP_IMAGE || `10.130.14.23:5000/clawmanager-hxc-app:${tag}`;
const openImage=process.env.TEAM_FOLLOWUP_OPENCLAW_IMAGE || `10.130.14.23:5000/agentsruntime/openclaw-lite:${runtimeTag}`;
const hermesImage=process.env.TEAM_FOLLOWUP_HERMES_IMAGE || `10.130.14.23:5000/agentsruntime/hermes-lite:${runtimeTag}`;
const runtimeRoot='D:/test/AgentsRuntime';
const prefix=`team-followup-${process.pid}`, jwt=randomBytes(32).toString('hex'), agentToken=randomBytes(32).toString('hex');
const names={mysql:`${prefix}-mysql`,redis:`${prefix}-redis`,app:`${prefix}-app`}, containers=[];
const volume=`${prefix}-workspaces`, shared='/workspaces/teams/user-77/team-1-shared';
let browser,networkCreated=false,volumeCreated=false;
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
function docker(args,input){const r=spawnSync('docker',args,{input,encoding:'utf8',timeout:240000});if(r.status!==0)throw new Error(`isolated docker ${args[0]} failed: ${r.stderr?.slice(-1800)}`);return r.stdout.trim();}
function start(name,img,args=[]){docker(['run','-d','--name',name,'--network',prefix,'--label',`clawmanager.team-followup=${prefix}`,...args,img]);containers.push(name);}
const envArgs=env=>Object.entries(env).flatMap(([key,val])=>['-e',`${key}=${val}`]);
function sql(q){return docker(['exec','-i',names.mysql,'mysql','--default-character-set=utf8mb4','-uroot','-pdisposable-local-only','-N','-B','clawmanager'],q);}
function redis(...args){return docker(['exec',names.redis,'redis-cli','--raw',...args]);}
async function until(check,description){const end=Date.now()+90000;while(Date.now()<end){if(await check())return;await pause(500);}throw new Error(`${description} timed out`);}
function token(){const enc=v=>Buffer.from(JSON.stringify(v)).toString('base64url');const p=`${enc({alg:'HS256',typ:'JWT'})}.${enc({user_id:77,token_type:'access',exp:Math.floor(Date.now()/1000)+3600})}`;return `${p}.${createHmac('sha256',jwt).update(p).digest('base64url')}`;}
function emit(id,member,extra={}){const root=`team-1-task-${id}`;const p={protocolVersion:4,event:'completion_proposed',eventId:`proposal-${randomBytes(8).toString('hex')}`,completionId:`completion-${randomBytes(8).toString('hex')}`,attemptId:'attempt',memberId:member,taskId:root,rootTaskId:root,rootMessageId:sql(`SELECT message_id FROM team_tasks WHERE id=${id}`),assignmentId:'single-member',revision:1,status:'succeeded',summary:'修改完成',resultMarkdown:'修改后的实际报告',explicitCompletion:true,completionSource:'team_complete_task',...extra};redis('XADD','claw:team:1:events','*','payload',JSON.stringify(p));return p;}

try {
  docker(['network','create',prefix]);networkCreated=true;
  docker(['volume','create',volume]);volumeCreated=true;
  start(names.mysql,'mysql:8.4',['--tmpfs','/var/lib/mysql:rw,size=768m',...envArgs({MYSQL_ROOT_PASSWORD:'disposable-local-only',MYSQL_DATABASE:'clawmanager'})]);
  start(names.redis,'redis:7-alpine');
  await until(()=>{try{return sql('SELECT 1')==='1';}catch{return false;}},'database');
  start(names.app,image,['-v',`${volume}:/workspaces`,'-p','127.0.0.1::8443',...envArgs({DB_HOST:names.mysql,DB_USER:'root',DB_PASSWORD:'disposable-local-only',DB_NAME:'clawmanager',JWT_SECRET:jwt,RUNTIME_AGENT_CONTROL_TOKEN:agentToken,RUNTIME_AGENT_REPORT_TOKEN:agentToken,RUNTIME_NAMESPACE:'team-followup-isolated',RUNTIME_SCHEDULER_ENABLED:'false',PLATFORM_REDIS_URL:`redis://${names.redis}:6379/0`,TEAM_REDIS_URL:`redis://${names.redis}:6379/0`})]);
  await until(()=>{try{return sql("SELECT COUNT(*) FROM schema_migrations WHERE filename='072_add_team_workspace_file_audits.sql'")==='1';}catch{return false;}},'candidate migrations');
  sql("INSERT INTO users(id,username,email,password_hash,role,is_active) VALUES(77,'followup-owner','followup@local.invalid','no-login','user',1)");
  sql("INSERT INTO teams(id,user_id,name,status,communication_mode) VALUES(1,77,'单成员隔离验收','running','leader_mediated'),(2,77,'另一团队','running','peer')");
  sql("INSERT INTO team_members(id,team_id,user_id,member_key,display_name,role,runtime_type,instance_mode,status,availability) VALUES(1,1,77,'leader','主编','leader','openclaw','lite','idle','idle'),(2,1,77,'open-worker','编辑员','worker','openclaw','lite','idle','idle'),(3,1,77,'hermes-worker','编辑员','worker','hermes','lite','idle','idle')");
  sql(`INSERT INTO team_tasks(id,team_id,target_member_id,created_by,message_id,status,workflow_state,payload_json) VALUES(1,1,1,77,'old-input','succeeded','completed','{"prompt":"Write report with verified sources"}'),(2,2,1,77,'foreign','succeeded','completed','{}')`);
  for(const [memberId,key] of [[2,'open-worker'],[3,'hermes-worker']]){
    const ref=`/team/artifacts/team-1-task-1/members/${key}/old/report.md`;
    sql(`INSERT INTO team_work_items(team_id,root_task_id,work_id,owner_member_id,title,status,result_json,artifact_refs_json) VALUES(1,1,'old-${key}',${memberId},'Collected source A and B','succeeded','{"resultMarkdown":"Collected source A and B","artifactRefs":["${ref}"]}','["${ref}"]')`);
    docker(['exec',names.app,'mkdir','-p',`${shared}/artifacts/team-1-task-1/members/${key}/old`]);
    docker(['exec','-i',names.app,'sh','-c','tee "$1" >/dev/null','_',`${shared}/artifacts/team-1-task-1/members/${key}/old/report.md`],'original citations\n');
  }
  docker(['exec','-i',names.app,'sh','-c','tee "$1" >/dev/null','_',`${shared}/team.json`],JSON.stringify({teamId:'1',communicationMode:'leader_mediated',members:[{memberId:'leader',role:'leader',isLeader:true},{memberId:'open-worker',role:'worker'},{memberId:'hermes-worker',role:'worker'}]}));
  docker(['restart',names.app]);
  const port=JSON.parse(docker(['inspect','-f','{{json .NetworkSettings.Ports}}',names.app]))['8443/tcp'][0].HostPort;
  const origin=`https://127.0.0.1:${port}`;
  browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL || 'chrome',headless:true});
  const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:1000}});
  await context.addInitScript(t=>{localStorage.setItem('access_token',t);localStorage.setItem('clawmanager_locale','zh');},token());
  const headers={Authorization:`Bearer ${token()}`};
  await until(async()=>{try{return(await context.request.get(`${origin}/api/v1/version`,{timeout:5000})).ok();}catch{return false;}},'App startup');
  async function dispatch(member,ctx,messageId=randomBytes(8).toString('hex'),mode='single_member'){
    const response=await context.request.post(`${origin}/api/v1/teams/1/tasks`,{headers,data:{target_member_id:member,execution_type:mode,context_task_id:ctx,message_id:messageId,payload:{title:'Shorten the previous report',prompt:'Shorten the previous report',responseLocale:'zh-CN'}}});
    return {response,body:await response.json()};
  }
  // Actual HTTP ownership and mode boundaries before any Runtime consumes.
  assert.ok(!(await dispatch('leader')).response.ok());
  assert.ok(!(await dispatch('open-worker',2)).response.ok());
  assert.ok(!(await dispatch('missing')).response.ok());
  const open=await dispatch('open-worker',1,'stable-open'); assert.ok(open.response.ok(),JSON.stringify(open.body));
  const openTask=open.body.data.task || open.body.data;
  const replay=await dispatch('open-worker',1,'stable-open');assert.equal((replay.body.data.task || replay.body.data).id,openTask.id);
  assert.equal(redis('XLEN','claw:team:1:inbox:open-worker'),'1');
  const conflicting=await context.request.post(`${origin}/api/v1/teams/1/tasks`,{headers,data:{target_member_id:'open-worker',execution_type:'single_member',context_task_id:0,message_id:'stable-open',payload:{prompt:'different'}}});assert.ok(!conflicting.ok());
  const hermes=await dispatch('hermes-worker',1,'stable-hermes');assert.ok(hermes.response.ok(),JSON.stringify(hermes.body));const hermesTask=hermes.body.data.task || hermes.body.data;
  assert.equal(sql(`SELECT COUNT(*) FROM team_work_items WHERE root_task_id=${openTask.id}`),'1');
  assert.equal(sql('SELECT status FROM team_tasks WHERE id=1'),'succeeded');
  // Inject contradictory IDs and an unauthorized author through real Redis.
  const rejected=[emit(openTask.id,'open-worker',{taskId:'team-1-task-1'}),emit(openTask.id,'leader'),emit(openTask.id,'open-worker',{artifactRefs:['/team/artifacts/missing/report.md']})];
  await until(()=>rejected.every(p=>redis('GET',`claw:team:1:completion-ack:${p.completionId}:attempt`).includes('rejected')),'rejection ACKs');
  assert.equal(sql(`SELECT status FROM team_tasks WHERE id=${openTask.id}`),'dispatched');
  assert.equal(sql(`SELECT COUNT(*) FROM team_events WHERE task_id=${openTask.id} AND event_type='completion_rejected'`),'1');
  const common=['--rm','-i','--network',prefix,'-v',`${volume}:/workspaces`];
  const openResult=docker(['run',...common,...envArgs({TEAM_FOLLOWUP_REDIS_URL:`redis://${names.redis}:6379/0`,TEAM_FOLLOWUP_SHARED:shared}), '--entrypoint','node',openImage,'--input-type=module'],fs.readFileSync(`${runtimeRoot}/plugins/openclaw-redis-team/scripts/single-member-image-test.mjs`,'utf8'));
  console.log(openResult);
  const hermesResult=docker(['run',...common,...envArgs({CLAWMANAGER_TEAM_ENABLED:'true',CLAWMANAGER_TEAM_REDIS_URL:`redis://${names.redis}:6379/0`,CLAWMANAGER_TEAM_ID:'1',CLAWMANAGER_TEAM_MEMBER_ID:'hermes-worker',CLAWMANAGER_TEAM_ROLE:'worker',CLAWMANAGER_TEAM_SHARED_DIR:shared}), '--entrypoint','python',hermesImage,'-'],fs.readFileSync(`${runtimeRoot}/plugins/hermes-redis-team/single_member_image_test.py`,'utf8'));
  console.log(hermesResult);
  for(const task of [openTask,hermesTask]){
    assert.equal(sql(`SELECT status FROM team_tasks WHERE id=${task.id}`),'succeeded');
    assert.equal(sql(`SELECT status FROM team_work_items WHERE root_task_id=${task.id}`),'succeeded');
    assert.equal(sql(`SELECT COUNT(*) FROM team_events WHERE task_id=${task.id} AND event_type='task_completed'`),'1');
  }
  assert.equal(redis('XLEN','claw:team:1:inbox:leader'),'0');
  if(process.env.TEAM_COMPLETION_RECOVERY==='1') {
    const recovery=await dispatch('open-worker',1,'callback-recovery');assert.ok(recovery.response.ok());
    const recoveryTask=recovery.body.data.task || recovery.body.data;
    // Force the asynchronous-history ordering that made "latest inbox row"
    // an unsafe test selector. The actual task is still the HTTP-created root.
    redis('XADD','claw:team:1:inbox:open-worker','*','payload',JSON.stringify({protocolVersion:4,messageId:'late-fixture-notice',teamId:'1',from:'clawmanager-monitor',to:'open-worker',taskId:`team-1-task-${openTask.id}`,rootTaskId:`team-1-task-${openTask.id}`,assignmentId:'single-member',revision:1,intent:'assignment_status_check',requiresCompletion:false,prompt:'Historical completed assignment check',createdAt:new Date().toISOString()}));
    console.log(docker(['run',...common,...envArgs({TEAM_FOLLOWUP_REDIS_URL:`redis://${names.redis}:6379/0`,TEAM_FOLLOWUP_SHARED:shared,TEAM_RECOVERY_ROOT:`team-1-task-${recoveryTask.id}`}), '--entrypoint','node',openImage,'--input-type=module'],fs.readFileSync(`${runtimeRoot}/plugins/openclaw-redis-team/scripts/completion-recovery-image-test.mjs`,'utf8')));
    assert.equal(sql(`SELECT status FROM team_tasks WHERE id=${recoveryTask.id}`),'succeeded');
    assert.equal(sql(`SELECT COUNT(*) FROM team_events WHERE task_id=${recoveryTask.id} AND event_type='root_assignment_recovery_requested'`),'1');
    assert.equal(sql(`SELECT COUNT(*) FROM team_work_items WHERE root_task_id=${recoveryTask.id}`),'1');
    assert.equal(redis('XLEN','claw:team:1:inbox:leader'),'0');
    const hr=await dispatch('hermes-worker',1,'hermes-callback-recovery');assert.ok(hr.response.ok());
    const ht=hr.body.data.task || hr.body.data;
    redis('XADD','claw:team:1:inbox:hermes-worker','*','payload',JSON.stringify({protocolVersion:4,messageId:'late-hermes-fixture-notice',teamId:'1',from:'clawmanager-monitor',to:'hermes-worker',taskId:`team-1-task-${hermesTask.id}`,rootTaskId:`team-1-task-${hermesTask.id}`,assignmentId:'single-member',revision:1,intent:'assignment_status_check',requiresCompletion:false,prompt:'Historical completed assignment check',createdAt:new Date().toISOString()}));
    console.log(docker(['run',...common,...envArgs({TEAM_RECOVERY_ROOT:`team-1-task-${ht.id}`,CLAWMANAGER_TEAM_ENABLED:'true',CLAWMANAGER_TEAM_REDIS_URL:`redis://${names.redis}:6379/0`,CLAWMANAGER_TEAM_ID:'1',CLAWMANAGER_TEAM_MEMBER_ID:'hermes-worker',CLAWMANAGER_TEAM_ROLE:'worker',CLAWMANAGER_TEAM_SHARED_DIR:shared}), '--entrypoint','python',hermesImage,'-'],fs.readFileSync(`${runtimeRoot}/plugins/hermes-redis-team/completion_recovery_image_test.py`,'utf8')));
    assert.equal(sql(`SELECT status FROM team_tasks WHERE id=${ht.id}`),'succeeded');
    assert.equal(sql(`SELECT COUNT(*) FROM team_events WHERE task_id=${ht.id} AND event_type='root_assignment_recovery_requested'`),'1');
    assert.equal(redis('XLEN','claw:team:1:inbox:leader'),'0');
  }
  emit(openTask.id,'open-worker',{event:'progress',status:'running',explicitCompletion:false});
  await pause(500); assert.equal(sql(`SELECT status FROM team_tasks WHERE id=${openTask.id}`),'succeeded');
  const follow=await dispatch('hermes-worker',undefined,'next-followup');assert.ok(follow.response.ok());
  const followTask=follow.body.data.task || follow.body.data;assert.equal(followTask.payload.contextTaskId,Number(sql("SELECT MAX(root_task_id) FROM team_work_items WHERE owner_member_id=3 AND status='succeeded'")));assert.match(followTask.payload.followupContext.text,/original|保留|verified sources|原始引用/);
  const cleared=await dispatch('open-worker',0,'cleared'); assert.ok(cleared.response.ok());assert.equal((cleared.body.data.task || cleared.body.data).payload.followupContext,undefined);
  const failedTask=cleared.body.data.task || cleared.body.data;
  emit(failedTask.id,'open-worker',{status:'failed',summary:'文件缺失',resultMarkdown:'所需文件不存在，未伪造结果'});
  await until(()=>sql(`SELECT status FROM team_tasks WHERE id=${failedTask.id}`)==='failed','explicit failure');
  assert.equal(sql(`SELECT status FROM team_work_items WHERE root_task_id=${failedTask.id}`),'failed');
  const ordinary=await dispatch('',undefined,'ordinary','team');assert.ok(ordinary.response.ok());assert.equal((ordinary.body.data.task || ordinary.body.data).target_member_id,1);
  const page=await context.newPage(), errors=[];page.on('pageerror',e=>errors.push(String(e)));
  await page.goto(`${origin}/teams/1`);const workspace=page.locator('.team-chat-workspace:visible');await workspace.waitFor();
  if(process.env.TEAM_COMPLETION_RECOVERY==='1') {
    for(const answer of ['本轮完整答案：资讯来自十二个已核对的来源。保留原始引用，不需要额外文件。','本轮完整答案：已核对十二个来源，沿用原始引用，无需额外文件。']) {
      const result=workspace.getByText(answer,{exact:true});await result.waitFor();assert.equal(await result.count(),1,'only the accepted delivery appears in real chat');
    }
    assert.equal(await workspace.getByText('[ROOT_TASK_RECOVERY]',{exact:false}).count(),0,'internal recovery prompt stays hidden');
  }
  const composer=workspace.locator('.team-task-composer');
  await composer.getByRole('button',{name:'选择任务执行成员'}).click();
  await composer.getByRole('combobox').fill('hermes-worker');await page.keyboard.press('Enter');
  assert.match(await composer.innerText(),/单成员/);await composer.getByRole('button',{name:'移除参考任务'}).click();
  await composer.getByRole('textbox',{name:'任务内容'}).fill('保留中文输入');
  // Cancel selection must not alter the user's message or chosen member.
  await composer.getByRole('button',{name:'选择任务执行成员'}).click();await page.keyboard.press('Escape');
  assert.equal(await composer.getByRole('textbox',{name:'任务内容'}).inputValue(),'保留中文输入');
  assert.equal(await composer.getByRole('listbox').count(),0);
  // The server accepts a request but its response is lost. Retry must use the
  // same reference and idempotency key, and create only one root/inbox delivery.
  const sent=[];let browserTask;
  await page.route('**/api/v1/teams/1/tasks',async route=>{
    if(route.request().method()!=='POST')return route.continue();
    sent.push(route.request().postDataJSON());
    const response=await route.fetch();const body=await response.json();browserTask=body.data.task||body.data;
    if(sent.length===1)return route.fulfill({status:503,json:{error:'本地模拟：响应中断，请重试'}});
    return route.fulfill({response});
  });
  await composer.getByRole('button',{name:'发送任务',exact:true}).click();
  await composer.getByRole('alert').waitFor();
  assert.equal(await composer.getByRole('textbox',{name:'任务内容'}).inputValue(),'保留中文输入');
  await composer.getByRole('button',{name:'发送任务',exact:true}).click();
  await until(async()=>await composer.getByRole('textbox',{name:'任务内容'}).inputValue()==='','successful retry');
  assert.deepEqual(sent[0],sent[1]);assert.equal(sent[0].target_member_id,'hermes-worker');assert.equal(sent[0].context_task_id,0);
  assert.equal(sql(`SELECT COUNT(*) FROM team_tasks WHERE message_id='${sent[0].message_id}'`),'1');
  assert.equal(sql(`SELECT COUNT(*) FROM team_work_items WHERE root_task_id=${browserTask.id}`),'1');
  await workspace.locator('.team-workflow-order').filter({hasText:'单成员执行'}).waitFor();
  assert.equal(await workspace.locator('.team-workflow-node').count(),1);
  await page.unroute('**/api/v1/teams/1/tasks');
  await composer.getByRole('button',{name:'选择任务执行成员'}).click();await composer.getByRole('combobox').fill('leader');await page.keyboard.press('Enter');
  assert.match(await composer.innerText(),/团队协作/);assert.equal(await composer.locator('.team-reference-chip').count(),0);
  await composer.getByRole('textbox',{name:'任务内容'}).fill('@');
  await composer.getByRole('textbox',{name:'任务内容'}).fill('');
  await composer.getByRole('textbox',{name:'任务内容'}).press('@');await composer.getByRole('combobox').waitFor();await page.keyboard.press('Escape');
  fs.mkdirSync('reports',{recursive:true});await page.screenshot({path:'reports/team-member-followup-desktop.png'});
  await page.setViewportSize({width:390,height:844});await workspace.waitFor();
  await composer.getByRole('button',{name:'选择任务执行成员'}).scrollIntoViewIfNeeded();
  await composer.getByRole('button',{name:'选择任务执行成员'}).click();
  await composer.getByRole('listbox').waitFor();
  const popup=await composer.locator('.team-recipient-popover').boundingBox();
  assert.ok(popup.x>=0 && popup.x+popup.width<=390,'mobile picker stays within viewport');
  assert.equal(await composer.locator('[role=option]>.team-functional-badge').count(),3,'all option badges must be measured');
  for(const badge of await composer.locator('[role=option]>.team-functional-badge').all()){
    const box=await badge.boundingBox();assert.ok(box.width<=40 && box.height<=40,'functional badge must not stretch with option text');
  }
  await page.screenshot({path:'reports/team-member-followup-mobile.png'});await page.keyboard.press('Escape');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  assert.deepEqual(errors,[]);
  console.log('PASS actual App/MySQL/Redis + both installed Runtime completion ACKs, memory payload, old artifact preservation, one-worker ledger/history and production composer. Real model semantic acceptance NOT performed.');
} catch(error) {
  try { console.error('ISOLATED_FAILURE_DB',sql("SELECT id,status FROM team_tasks; SELECT event_type,JSON_UNQUOTE(JSON_EXTRACT(payload_json,'$.eventKind')) FROM team_events ORDER BY id DESC LIMIT 8; SELECT status,attempts,last_error FROM team_event_outbox ORDER BY id DESC LIMIT 6")); } catch {}
  throw error;
} finally {
  await browser?.close();
  for(const name of containers.reverse())try{docker(['rm','-f',name]);}catch{}
  if(volumeCreated)try{docker(['volume','rm',volume]);}catch{}
  if(networkCreated)try{docker(['network','rm',prefix]);}catch{}
}
