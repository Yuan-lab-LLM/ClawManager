// Real App/Agent/core/browser, local disposable data; no model calls or K8s.
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { createHmac, randomBytes } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
const app=process.env.OPENCLAW102_APP_IMAGE||'clawmanager-hxc-app:team-102-candidate';
const image=process.env.OPENCLAW102_RUNTIME_IMAGE||'openclaw:team-102-final';
const legacy=process.env.OPENCLAW_LEGACY_RUNTIME==='1';
const fixturePath=name=>fileURLToPath(new URL(`./fixtures/${name}`,import.meta.url)).replaceAll('\\','/');
const prefix=`openclaw102-access-${process.pid}`, ns='openclaw102-local';
const fixtureRoot=`/workspaces/${prefix}`;
const workspaceAcceptance=process.env.OPENCLAW102_WORKSPACE==='1';
const dataVolume=process.env.OPENCLAW102_DATA_VOLUME||prefix+'-data';
const deploymentName='openclaw-runtime-u113-u123-u125-u127-u130-u132-u134';
const podName=deploymentName+'-6f8995dn4cs';
const names={mysql:prefix+'-mysql',redis:prefix+'-redis',app:prefix+'-app',runtime:prefix+'-runtime'};
const secret={control:randomBytes(32).toString('hex'),report:randomBytes(32).toString('hex'),jwt:randomBytes(32).toString('hex')};
const containers=[],frames=[];let browser,renewal,network=false,volume=false,renewalError,protocolStub;
function docker(args,input){const r=spawnSync('docker',args,{input,encoding:'utf8',timeout:180000});if(r.status!==0)throw Error(`local Docker ${args[0]} failed`);return r.stdout.trim();}
const envArgs=env=>Object.entries(env).flatMap(([k,v])=>['-e',`${k}=${v}`]);
function start(name,img,args=[],command=[]){docker(['run','-d','--name',name,'--network',prefix,...args,img,...command]);containers.push(name);}
function sql(query){return docker(['exec','-i',names.mysql,'mysql','-uroot','-pdisposable-local-only','-N','-B','clawmanager'],query);}
const ip=name=>docker(['inspect','-f','{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',name]);
const port=(name,p)=>JSON.parse(docker(['inspect','-f','{{json .NetworkSettings.Ports}}',name]))[`${p}/tcp`][0].HostPort;
const pause=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,name,timeout=90000){const end=Date.now()+timeout;while(Date.now()<end){if(await fn())return;await pause(500);}throw Error(name+' timeout');}
const encode=x=>Buffer.from(JSON.stringify(x)).toString('base64url');
function jwt(){const data=`${encode({alg:'HS256',typ:'JWT'})}.${encode({user_id:77,token_type:'access',exp:Math.floor(Date.now()/1000)+3600})}`;return data+'.'+createHmac('sha256',secret.jwt).update(data).digest('base64url');}
let controlBase;
async function control(route,body,method){const r=await fetch(controlBase+route,{method:method||(body?'POST':'GET'),headers:{'X-ClawManager-Control-Token':secret.control,'content-type':'application/json'},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(15000)});const result=await r.json();assert.equal(r.status,200,`Agent ${route}: ${result.error||result.message||''}`);return result;}
async function renew(){const previous=await control('/v1/proxy-policy');await control('/v1/proxy-policy',{namespace:ns,expected_revision:previous.revision,proxies:[ip(names.app)]},'PUT');}
try{
 docker(['network','create',prefix]);network=true;
 if(!process.env.OPENCLAW102_DATA_VOLUME){docker(['volume','create',dataVolume]);volume=true;}
 start(names.mysql,'mysql:8.4',['--tmpfs','/var/lib/mysql:rw,size=768m',...envArgs({MYSQL_ROOT_PASSWORD:'disposable-local-only',MYSQL_DATABASE:'clawmanager'})]);
 start(names.redis,'redis:7-alpine');
 await until(()=>{try{return sql('SELECT 1')==='1';}catch{return false;}},'database');
 const appDNS=`clawmanager-gateway.${ns}.svc.cluster.local`, backend=`http://${appDNS}:9001`;
 const common={RUNTIME_AGENT_CONTROL_TOKEN:secret.control,RUNTIME_AGENT_REPORT_TOKEN:secret.report};
 start(names.app,app,['--network-alias',appDNS,'-v',dataVolume+':/workspaces','-p','127.0.0.1::8443',...envArgs({...common,DB_HOST:names.mysql,DB_USER:'root',DB_PASSWORD:'disposable-local-only',DB_NAME:'clawmanager',JWT_SECRET:secret.jwt,RUNTIME_NAMESPACE:ns,RUNTIME_SCHEDULER_ENABLED:'false',PLATFORM_REDIS_URL:`redis://${names.redis}:6379/0`,TEAM_REDIS_URL:`redis://${names.redis}:6379/0`})]);
 const publicURL=`https://127.0.0.1:${port(names.app,8443)}`;
 await until(()=>{try{return sql("SELECT COUNT(*) FROM schema_migrations WHERE filename='072_add_team_workspace_file_audits.sql'")==='1';}catch{return false;}},'migrations');
 start(names.runtime,image,['-v',dataVolume+':/workspaces','--mount',`type=bind,source=${fixturePath('openclaw102-model-protocol-stub.mjs')},target=/tests/protocol-stub.mjs,readonly`,'-p','127.0.0.1::19090',...envArgs({...common,CLAWMANAGER_RUNTIME_TYPE:'openclaw',CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME:deploymentName,CLAWMANAGER_DEPLOYMENT_NAME:legacy?deploymentName:'stale-inherited-source',CLAWMANAGER_CONTROL_UI_ORIGIN:publicURL,RUNTIME_GATEWAY_AUTH_MODE:'trusted-proxy',POD_NAMESPACE:ns,POD_NAME:podName,POD_UID:prefix+'-uid',CLAWMANAGER_BACKEND_URL:backend,RUNTIME_AGENT_LISTEN_ADDR:'0.0.0.0:19090',RUNTIME_WORKSPACE_ROOT:fixtureRoot})]);
 controlBase=`http://127.0.0.1:${port(names.runtime,19090)}`;
 await until(async()=>{try{return(await control('/v1/health')).status==='ready';}catch{return false;}},'Agent /init startup',240000);
 await until(()=>sql(`SELECT COUNT(*) FROM runtime_pods WHERE namespace='${ns}' AND pod_name='${podName}'`)==='1','registration');
 const pod=sql(`SELECT id FROM runtime_pods WHERE namespace='${ns}' AND pod_name='${podName}'`);
 assert.equal(sql(`SELECT deployment_name FROM runtime_pods WHERE id=${pod}`),deploymentName,'real /init registration misidentified long upgrade deployment');
 const startupLogs=spawnSync('docker',['logs',names.runtime],{encoding:'utf8'});
 if(!legacy)assert.match(startupLogs.stdout+startupLogs.stderr,/Shared Runtime Agent owns Gateway lifecycle/);
 const defaultListener=spawnSync('docker',['exec',names.runtime,'curl','-fsS','--max-time','2','http://127.0.0.1:18789/health'],{encoding:'utf8'});
 // The historical 7.1 image predates shared-only Desktop startup suppression.
 // Do not impose that later Runtime contract on its App-access compatibility test.
 if(!legacy)assert.notEqual(defaultListener.status,0,'shared pool started an unrelated Desktop Gateway');
 if(legacy){
  const policy=await fetch(controlBase+'/v1/proxy-policy',{headers:{'X-ClawManager-Control-Token':secret.control}});
  assert.equal(policy.status,404,'legacy fixture must actually lack the new proxy policy API');
 }else{await renew();renewal=setInterval(()=>renew().catch(e=>{renewalError=e;}),10000);}
 sql("INSERT INTO users(id,username,email,password_hash,role,is_active) VALUES(77,'local-openclaw102','fixture@local.invalid','no-login','user',1)");
 const iid=9105,workspace=`${fixtureRoot}/openclaw/user-77/instance-${iid}`;
 if(workspaceAcceptance){
  const stub=prefix+'-model';
  start(stub,'node:24.21.0-bookworm-slim',['--mount',`type=bind,source=${fixturePath('openclaw102-workspace-model-stub.mjs')},target=/fixture.mjs,readonly`,'--entrypoint','node'],['/fixture.mjs']);
  await until(()=>docker(['logs',stub]).includes('LOCAL_WORKSPACE_STUB_READY'),'workspace protocol fixture');
  // The upstream default bootstrap prompt includes sensitive examples; retain
  // the App's normal secure-model routing policy rather than disabling it.
  sql(`INSERT INTO llm_models(display_name,provider_type,protocol_type,base_url,provider_model_name,provider_models_json,api_key,is_active,is_secure) VALUES('fixture','openai-compatible','openai-compatible','http://${stub}:19334/v1','local-smoke','[{"id":"local-smoke"},{"id":"local-second"}]','synthetic-upstream-key',1,1)`);
 }
 sql(`INSERT INTO instances(id,user_id,name,type,runtime_type,instance_mode,status,cpu_cores,memory_gb,disk_gb,os_type,os_version,workspace_path,runtime_generation,access_token) VALUES(${iid},77,'OpenClaw10.2','openclaw','gateway','lite','creating',1,1,1,'openclaw','lite','${workspace}',1,'local-instance-only')`);
 sql(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(${iid},${pod},'openclaw','gw-${iid}-1',20000,'${workspace}','creating',1)`);
 await control('/v1/gateways',{instance_id:iid,user_id:77,agent_type:'openclaw',workspace_path:workspace,gateway_port:20000,uid:209105,gid:1000,generation:1,environment:{CLAWMANAGER_INSTANCE_TOKEN:'local-instance-only',...(workspaceAcceptance?{CLAWMANAGER_LLM_BASE_URL:backend+'/api/v1/gateway/llm',CLAWMANAGER_LLM_API_KEY:'local-instance-only',CLAWMANAGER_LLM_MODEL:'["auto","local-smoke","local-second"]',CLAWMANAGER_LLM_PROVIDER_MODELS:'["auto/auto","fixture/local-smoke","fixture/local-second"]',OPENAI_MODEL:'auto'}:{})}});
 await until(()=>sql(`SELECT status FROM instances WHERE id=${iid}`)==='running','actual instance Running');
 // Context-level ignoreHTTPSErrors does not cover legacy ServiceWorker fetches.
 // Trust only this disposable App's public certificate, not arbitrary origins.
 const spki=docker(['exec',names.app,'sh','-c','openssl x509 -in /etc/nginx/tls/tls.crt -pubkey -noout | openssl pkey -pubin -outform DER | openssl dgst -sha256 -binary | openssl base64 -A']);
 assert.match(spki,/^[A-Za-z0-9+/]{43}=$/);
 browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',args:[`--ignore-certificate-errors-spki-list=${spki}`]});
 const ctx=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1600,height:1000}});
 await ctx.addInitScript(token=>{localStorage.setItem('access_token',token);localStorage.setItem('clawmanager_locale','zh');},jwt());
 const page=await ctx.newPage(),errors=[];
 page.on('pageerror',e=>errors.push(String(e)));
 page.on('websocket',socket=>{if(!socket.url().includes(`/instances/${iid}/proxy`))return;socket.on('framereceived',event=>{try{frames.push(JSON.parse(event.payload));}catch{}});});
 await page.goto(publicURL+`/instances/${iid}`);
 await page.locator('iframe:visible').first().waitFor({timeout:60000});
 await until(()=>frames.some(f=>f.type==='res'&&f.ok&&f.payload?.type==='hello-ok'),'real embedded Control UI signed WebSocket',60000);
 assert.deepEqual(errors,[]);
 assert.equal(renewalError,undefined);
 console.log(`PASS actual App proxy, owner authentication, embedded ${legacy?'legacy7.1':'10.2'} Control UI assets and authenticated signed WebSocket, real Agent Running; no model calls.`);
 if(workspaceAcceptance){
  const frame=page.frameLocator('iframe:visible').first();
  const composer=frame.locator('textarea:visible, [contenteditable="true"][role="textbox"]:visible').first();
  await composer.waitFor({timeout:60000});
  await composer.fill('LOCAL_WORKSPACE_REQUEST');
  await composer.press('Enter');
  if(process.env.EXPECT_UNPATCHED_WORKSPACE_FAILURE==='1'){
   await until(()=>frames.some(f=>f.event==='chat'&&f.payload?.state==='error'&&/staged file publish failed/.test(f.payload?.errorMessage||'')),'baseline file error');
   await frame.locator('details.chat-error__content > summary').first().click();
   await frame.getByText(/FsSafeError: staged file publish failed/).first().waitFor({timeout:10000});
   assert.equal(sql(`SELECT COUNT(*) FROM model_invocations WHERE instance_id=${iid}`),'0');
   console.log('CONFIRMED baseline embedded first Webchat fails during NFS workspace initialization before App AI gateway');
  }else{
   await frame.getByText('LOCAL_WORKSPACE_REPLY',{exact:true}).first().waitFor({timeout:90000});
   assert.equal(sql(`SELECT COUNT(*) FROM model_invocations WHERE instance_id=${iid} AND requested_model='auto' AND actual_provider_model='local-smoke' AND status='completed'`),'1');
   console.log('PASS fresh first Auto Webchat -> actual App AI gateway -> deterministic upstream -> visible reply');
   // The upstream toolbar can rerender when the just-finished turn refreshes
   // session/catalog state. Reopen only a closed picker; still require the real
   // selected model and completed App invocation below.
   await until(async()=>{
    const search=frame.locator('[data-chat-model-search]:visible');
    if(await search.count())return true;
    await frame.locator('[data-chat-model-select]:visible').first().click();
    try{await search.waitFor({timeout:1500});return true;}catch{return false;}
   },'stable model picker',15000);
   await frame.locator('[data-chat-model-search]:visible').fill('local-second');
   await frame.locator('[data-chat-model-option]:visible').filter({hasText:'local-second'}).first().click();
   await composer.fill('LOCAL_WORKSPACE_SECOND_REQUEST');
   await composer.press('Enter');
   await until(()=>sql(`SELECT COUNT(*) FROM model_invocations WHERE instance_id=${iid} AND actual_provider_model='local-second' AND status='completed'`)==='1','manual selection through actual App gateway');
   await until(()=>frame.getByText('LOCAL_WORKSPACE_REPLY',{exact:true}).count().then(n=>n>=2),'second visible Webchat reply');
   console.log('PASS explicit model switch and second Webchat preserve the initialized workspace');
  }
 }
 if(process.env.OPENCLAW102_INBOUND==='1'){
  protocolStub=spawn('docker',['exec',names.runtime,'node','/tests/protocol-stub.mjs'],{stdio:['ignore','pipe','pipe']});
  await new Promise((resolve,reject)=>{const t=setTimeout(()=>reject(Error('protocol stub start timeout')),15000);protocolStub.stdout.on('data',x=>{if(String(x).includes('LOCAL_PROTOCOL_STUB_READY')){clearTimeout(t);resolve();}});protocolStub.on('exit',code=>{if(code){clearTimeout(t);reject(Error('protocol stub exited'));}});});
  sql("INSERT INTO teams(id,user_id,name,status,communication_mode) VALUES(1,77,'Native10.2 protocol fixture','running','leader_mediated')");
  sql("INSERT INTO team_members(id,team_id,user_id,member_key,display_name,role,runtime_type,instance_mode,status,availability) VALUES(1,1,77,'leader','Leader','leader','openclaw','lite','idle','idle'),(2,1,77,'editor','Editor','worker','openclaw','lite','idle','idle')");
  const wid=9106,wspace=`${fixtureRoot}/openclaw/user-77/instance-${wid}`;
  sql(`INSERT INTO instances(id,user_id,name,type,runtime_type,instance_mode,status,cpu_cores,memory_gb,disk_gb,os_type,os_version,workspace_path,runtime_generation,access_token) VALUES(${wid},77,'Native Worker','openclaw','gateway','lite','creating',1,1,1,'openclaw','lite','${wspace}',1,'local-worker-only')`);
  sql(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(${wid},${pod},'openclaw','gw-${wid}-1',20003,'${wspace}','creating',1)`);
  sql(`UPDATE team_members SET instance_id=${wid} WHERE id=2`);
  await control('/v1/gateways',{instance_id:wid,user_id:77,agent_type:'openclaw',workspace_path:wspace,gateway_port:20003,uid:209106,gid:1000,generation:1,environment:{CLAWMANAGER_INSTANCE_TOKEN:'local-worker-only',CLAWMANAGER_LLM_BASE_URL:'http://127.0.0.1:19333/v1',CLAWMANAGER_LLM_API_KEY:'local-protocol-key-not-a-secret',CLAWMANAGER_LLM_MODEL:'local-smoke',CLAWMANAGER_TEAM_ENABLED:'true',CLAWMANAGER_TEAM_ID:'1',CLAWMANAGER_TEAM_MEMBER_ID:'editor',CLAWMANAGER_TEAM_ROLE:'worker',CLAWMANAGER_TEAM_REDIS_URL:`redis://${names.redis}:6379/0`,CLAWMANAGER_TEAM_AUTORUN:'true'}});
  await until(()=>sql(`SELECT status FROM instances WHERE id=${wid}`)==='running','actual idle Worker consumer');
  const response=await ctx.request.post(publicURL+'/api/v1/teams/1/tasks',{headers:{Authorization:'Bearer '+jwt()},data:{execution_type:'single_member',target_member_id:'editor',context_task_id:0,message_id:'native-protocol-1',payload:{prompt:'Protocol fixture: submit the completion tool for this single-member task.',title:'Native protocol fixture'}}});
  const body=await response.json();assert.ok(response.ok(),'real task dispatch rejected: '+JSON.stringify(body));const task=body.data.task||body.data;
  await until(()=>sql(`SELECT status FROM team_tasks WHERE id=${task.id}`)==='succeeded','actual native inbound/tools/completion ACK',180000);
  assert.equal(sql(`SELECT COUNT(*) FROM team_events WHERE task_id=${task.id} AND event_type='task_completed'`),'1');
  assert.equal(sql(`SELECT status FROM team_work_items WHERE root_task_id=${task.id}`),'succeeded');
  console.log('PASS actual native10.2 Redis consumer -> SDK inbound -> local deterministic API protocol -> explicit completion tool -> App ACK/root; zero real model calls, not semantic acceptance.');
 }
}catch(error){
 // Disposable data and synthetic credentials only. Never dump config/env/JWT.
 try{console.error('TASK_DIAGNOSTIC',sql('SELECT id,status FROM team_tasks'),sql('SELECT task_id,event_type FROM team_events ORDER BY id'));}catch{}
 try{console.error('MODEL_DIAGNOSTIC',sql('SELECT instance_id,requested_model,status,LEFT(error_message,300) FROM model_invocations ORDER BY id DESC LIMIT 4'));}catch{}
 try{console.error('APP_DIAGNOSTIC',docker(['logs','--tail','160',names.app]).split('\n').filter(x=>/\[ERROR\]|403|gateway.*failed/i.test(x)).join('\n'));}catch{}
 console.error('CHAT_DIAGNOSTIC',frames.filter(f=>f.event==='chat'&&f.payload?.state==='error').map(f=>f.payload?.errorMessage||f.payload?.message||'chat failed'));
 try{console.error('RUNTIME_DIAGNOSTIC',docker(['logs','--tail','120',names.runtime]).split('\n').filter(x=>/redis-team|dispatch|processing|Error|failed/i.test(x)).join('\n'));}catch{}
 try{console.error('GATEWAY_DIAGNOSTIC',docker(['exec',names.runtime,'sh','-c','find /workspaces -name "*.log" -type f -exec tail -n 70 {} \\;']).split('\n').filter(x=>/redis-team|dispatch|processing|Error|failed|provider|model|tool/i.test(x)).map(x=>x.replace(/(token|password|apiKey)\s*[=:]\s*[^\s,]+/gi,'$1=[redacted]')).join('\n'));}catch{}
 try{console.error('PROTOCOL_STATS',docker(['exec',names.runtime,'curl','-fsS','http://127.0.0.1:19333/stats']));}catch{}
 throw error;
}finally{
 clearInterval(renewal);await browser?.close();
 for(const name of containers.reverse())try{docker(['rm','-f',name]);}catch{}
 protocolStub?.kill();
 if(volume)try{docker(['volume','rm',dataVolume]);}catch{}
 if(network)try{docker(['network','rm',prefix]);}catch{}
}
