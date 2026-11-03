// Built App, real MySQL/Redis and production browser. Lifecycle fixtures are
// local database facts, not Kubernetes provisioning or real model acceptance.
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHmac, randomBytes } from 'node:crypto';
import fs from 'node:fs';
import { chromium } from 'playwright';

const tag=process.env.TEAM_STARTUP_TAG||'team-member-startup-state-20261012-v1';
const image=`10.130.14.23:5000/clawmanager-hxc-app:${tag}`;
const prefix=`team-startup-${process.pid}`,jwt=randomBytes(32).toString('hex');
const names={mysql:`${prefix}-mysql`,redis:`${prefix}-redis`,app:`${prefix}-app`},containers=[];
let browser,networkCreated=false;
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
function docker(args,input){const r=spawnSync('docker',args,{input,encoding:'utf8',timeout:180000});if(r.status!==0)throw new Error(`isolated Docker ${args[0]} failed (${r.status})`);return r.stdout.trim();}
const envArgs=env=>Object.entries(env).flatMap(([k,v])=>['-e',`${k}=${v}`]);
function start(name,img,args=[]){docker(['run','-d','--name',name,'--network',prefix,'--label',`clawmanager.startup-test=${prefix}`,...args,img]);containers.push(name);}
const sql=q=>docker(['exec','-i',names.mysql,'mysql','--default-character-set=utf8mb4','-uroot','-pdisposable-local-only','-N','-B','clawmanager'],q);
async function until(check,description){const deadline=Date.now()+90000;while(Date.now()<deadline){if(await check())return;await pause(300);}throw new Error(`${description} timed out`);}
function token(id=77){const enc=v=>Buffer.from(JSON.stringify(v)).toString('base64url');const body=`${enc({alg:'HS256',typ:'JWT'})}.${enc({user_id:id,token_type:'access',exp:Math.floor(Date.now()/1000)+3600})}`;return `${body}.${createHmac('sha256',jwt).update(body).digest('base64url')}`;}
try{
  docker(['network','create',prefix]);networkCreated=true;
  start(names.mysql,'mysql:8.4',['--tmpfs','/var/lib/mysql:rw,size=768m',...envArgs({MYSQL_ROOT_PASSWORD:'disposable-local-only',MYSQL_DATABASE:'clawmanager'})]);
  start(names.redis,'redis:7-alpine');
  await until(()=>{try{return sql('SELECT 1')==='1';}catch{return false;}},'database');
  start(names.app,image,['-p','127.0.0.1::8443',...envArgs({DB_HOST:names.mysql,DB_USER:'root',DB_PASSWORD:'disposable-local-only',DB_NAME:'clawmanager',JWT_SECRET:jwt,RUNTIME_NAMESPACE:'team-startup-isolated',RUNTIME_SCHEDULER_ENABLED:'false',PLATFORM_REDIS_URL:`redis://${names.redis}:6379/0`})]);
  await until(()=>{try{return sql("SELECT COUNT(*) FROM schema_migrations WHERE filename='072_add_team_workspace_file_audits.sql'")==='1';}catch{return false;}},'App migrations');
  console.log('CANDIDATE_APP',docker(['inspect','-f','{{.Image}}',names.app]));
  sql("INSERT INTO users(id,username,email,password_hash,role,is_active) VALUES(77,'startup-owner','startup@local.invalid','no-login','user',1),(78,'foreign-owner','foreign@local.invalid','no-login','user',1)");
  // Exclude the fixtures from background sweeps while proving GET has no writes.
  sql("INSERT INTO teams(id,user_id,name,status,communication_mode) VALUES(1,77,'成员启动状态验收','failed','leader_mediated'),(2,77,'Peer与Pro状态验收','failed','peer')");
  for(const [id,type,mode,runtime] of [[901,'openclaw','lite','gateway'],[902,'openclaw','lite','gateway'],[903,'hermes','lite','gateway'],[904,'hermes','lite','gateway'],[905,'openclaw','pro','desktop']])sql(`INSERT INTO instances(id,user_id,name,type,instance_mode,runtime_type,status,cpu_cores,memory_gb,disk_gb,os_type,os_version,runtime_generation) VALUES(${id},77,'fixture-${id}','${type}','${mode}','${runtime}','${id===905?'stopped':'creating'}',2,4,20,'linux','fixture',1)`);
  for(const [id,team,key,role,type,mode,iid] of [[1,1,'leader','leader','openclaw','lite',901],[2,1,'collector','worker','openclaw','lite',902],[3,1,'editor','worker','hermes','lite',903],[4,2,'peer-reader','worker','hermes','lite',904],[5,2,'peer-pro','worker','openclaw','pro',905]])sql(`INSERT INTO team_members(id,team_id,user_id,instance_id,member_key,display_name,role,runtime_type,instance_mode,status,availability,runtime_status,blocked_reason) VALUES(${id},${team},77,${iid},'${key}','${key}','${role}','${type}','${mode}','creating','offline','starting','Runtime unavailable: linked instance is starting')`);
  sql("INSERT INTO team_tasks(id,team_id,target_member_id,created_by,message_id,status,workflow_state,payload_json) VALUES(1,1,1,77,'old-root','succeeded','completed','{}'),(2,1,2,77,'active-root','running','executing','{\"prompt\":\"保持正在执行的任务\",\"executionType\":\"single_member\",\"executorMemberKey\":\"collector\"}')");
  sql("INSERT INTO team_work_items(team_id,root_task_id,owner_member_id,work_id,title,status,revision,required_for_root) VALUES(1,2,2,'single-member','进行中的工作','running',1,1)");
  sql("UPDATE team_members SET status='busy',current_task_id=2,progress=37 WHERE id=2");
  const port=JSON.parse(docker(['inspect','-f','{{json .NetworkSettings.Ports}}',names.app]))['8443/tcp'][0].HostPort,origin=`https://127.0.0.1:${port}`;
  browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',headless:true});
  const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:1000}});
  await context.addInitScript(t=>{localStorage.setItem('access_token',t);localStorage.setItem('clawmanager_locale','zh');},token());
  async function details(team=1){const r=await context.request.get(`${origin}/api/v1/teams/${team}`,{headers:{Authorization:`Bearer ${token()}`}});assert.ok(r.ok());return(await r.json()).data;}
  const persisted=()=>sql('SELECT id,status,availability,IFNULL(runtime_status,""),IFNULL(blocked_reason,""),IFNULL(current_task_id,0),progress,updated_at FROM team_members ORDER BY id');
  const before=persisted(),tasksBefore=sql('SELECT id,status,workflow_state,updated_at FROM team_tasks ORDER BY id');
  let data=await details();assert.equal(data.members.length,3);
  assert.ok(data.members.every(m=>m.availability==='unknown'&&!m.blocked_reason&&m.runtime_status==='creating'));
  assert.equal(data.members.find(m=>m.id===2).status,'busy');assert.equal(data.members.find(m=>m.id===2).current_task_id,2);
  const peerStart=(await details(2)).members;
  assert.equal(peerStart.find(m=>m.id===4).runtime_status,'creating');assert.equal(peerStart.find(m=>m.id===5).availability,'offline');assert.equal(peerStart.find(m=>m.id===5).runtime_status,'stopped');assert.equal(persisted(),before);
  const foreign=await context.request.get(`${origin}/api/v1/teams/1`,{headers:{Authorization:`Bearer ${token(78)}`}});assert.ok(!foreign.ok());
  sql("UPDATE instances SET status='running' WHERE id IN(901,902,903,904)");
  data=await details();assert.equal(data.members.find(m=>m.id===1).availability,'idle');assert.equal(data.members.find(m=>m.id===2).availability,'busy');assert.equal(data.members.find(m=>m.id===2).progress,37);
  assert.equal((await details(2)).members.find(m=>m.id===4).availability,'idle');assert.equal(persisted(),before);
  assert.equal(sql('SELECT id,status,workflow_state,updated_at FROM team_tasks ORDER BY id'),tasksBefore,'liveness must not complete/reopen a root');
  console.log('PASS real HTTP current-instance projection, Leader/Peer, OpenClaw/Hermes Lite and stopped Pro, no GET writes, owner and task/assignment boundaries');
  sql("UPDATE instances SET status='creating' WHERE id IN(901,902,903)");
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(String(e)));page.setDefaultTimeout(15000);
  await page.goto(`${origin}/teams/1`);const workspace=page.locator('.team-chat-workspace:visible');await workspace.waitFor();
  assert.equal(await workspace.locator('.team-roster-mini-state').filter({hasText:'启动中'}).count(),3);
  await workspace.getByText('3 人启动中',{exact:true}).waitFor();
  assert.equal(await workspace.locator('.team-roster-mini[data-tone=error]').count(),0);
  await workspace.getByRole('button',{name:'展开成员与团队配置'}).click();
  assert.equal(await workspace.locator('.team-roster-card [data-tone]').filter({hasText:'运行 · 启动中'}).count(),3);
  assert.equal(await workspace.locator('.team-roster-summary[data-error=true]').count(),0);
  await workspace.getByRole('button',{name:'收起成员',exact:true}).click();
  fs.mkdirSync('reports',{recursive:true});await page.screenshot({path:'reports/team-member-startup-transition.png'});
  // The production5s refresh, not reload/navigation or the30s repair, updates it.
  sql("UPDATE instances SET status='running' WHERE id IN(901,902,903)");
  await until(async()=>await workspace.getByText('3 人在线',{exact:true}).count()===1,'automatic refresh');
  await workspace.locator('.team-roster-mini[data-member-id="2"] .team-roster-mini-state').filter({hasText:'运行忙碌'}).waitFor();
  assert.equal(await workspace.getByText('3 人启动中',{exact:true}).count(),0);
  // A genuine runtime failure remains visible, then clears on real recovery.
  sql("UPDATE instances SET status='error',runtime_error_message='fixture gateway exited' WHERE id=903");
  await workspace.locator('.team-roster-mini[data-member-id="3"] .team-roster-mini-state').filter({hasText:'运行异常'}).waitFor();
  await workspace.getByText('2 人在线',{exact:true}).waitFor();
  sql("UPDATE instances SET status='running',runtime_error_message=NULL WHERE id=903");
  await workspace.getByText('3 人在线',{exact:true}).waitFor();
  // Independent business blockers cannot be erased merely because Runtime is up.
  sql("UPDATE team_members SET blocked_reason='Required source file missing',availability='blocked' WHERE id=2");
  await workspace.locator('.team-roster-mini[data-member-id="2"] .team-roster-mini-state').filter({hasText:'运行异常'}).waitFor();
  assert.equal(sql('SELECT id,status,workflow_state,updated_at FROM team_tasks ORDER BY id'),tasksBefore);
  await page.screenshot({path:'reports/team-member-startup-recovered.png'});
  await page.setViewportSize({width:390,height:844});await workspace.waitFor();await workspace.getByRole('button',{name:'展开成员与团队配置'}).scrollIntoViewIfNeeded();
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.deepEqual(errors,[]);
  console.log('PASS actual production rail/cards/startup counts; automatic transition without re-entry, real failure/recovery, independent blocker, mobile and no model calls');
}finally{
  await browser?.close();
  for(const name of containers.reverse())try{docker(['rm','-f','-v',name]);}catch{}
  if(networkCreated)try{docker(['network','rm',prefix]);}catch{}
}
