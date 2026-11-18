// Native Gateway startup on isolated tmpfs, matching the pool validation probe.
// No provider calls and no persistent user workspace are involved.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
const images=(process.env.POOL_TEST_IMAGES||'10.130.14.23:5000/agentsruntime/openclaw-lite:master-20260824-737ad4c,10.130.14.23:5000/agentsruntime/openclaw-lite:team-openclaw-10.2-recovery-20261013-v1').split(',');
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8',timeout:120000});if(r.status!==0)throw Error(`docker ${args[0]} failed`);return r.stdout.trim()}
for(let i=0;i<images.length;i++){
 const image=images[i],name=`pool-validation-${process.pid}-${i}`,token=randomBytes(24).toString('hex');let started=false;
 try{
  docker(['run','-d','--name',name,'--label','codex.task=pool-repair-20261026','--tmpfs','/workspaces:rw,size=256m','-p','127.0.0.1::19090','--entrypoint','/usr/local/bin/clawmanager-agent',...Object.entries({CLAWMANAGER_RUNTIME_TYPE:'openclaw',RUNTIME_AGENT_MODE:'runtime-pod',RUNTIME_AGENT_CONTROL_TOKEN:token,RUNTIME_AGENT_REPORT_TOKEN:token,CLAWMANAGER_BACKEND_URL:'http://127.0.0.1:9',CLAWMANAGER_RUNTIME_IMAGE_REF:image,POD_NAME:name,POD_NAMESPACE:'isolated',POD_UID:name,RUNTIME_WORKSPACE_ROOT:'/workspaces'}).flatMap(([k,v])=>['-e',`${k}=${v}`]),image]);started=true;
  const port=JSON.parse(docker(['inspect','-f','{{json .NetworkSettings.Ports}}',name]))['19090/tcp'][0].HostPort;
  async function request(path,method='GET',body){const r=await fetch(`http://127.0.0.1:${port}${path}`,{method,headers:{'X-ClawManager-Control-Token':token,'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(100000)});const text=await r.text();if(!r.ok)throw Error(`Runtime ${method} ${path}: ${r.status} ${text.slice(0,350)}`);return text?JSON.parse(text):null}
  let ready=false;for(let n=0;n<45;n++){try{await request('/v1/health');ready=true;break}catch{}await new Promise(r=>setTimeout(r,500))}assert.ok(ready,'Agent did not start');
  try{const policy=await request('/v1/proxy-policy');await request('/v1/proxy-policy','PUT',{expected_revision:policy.revision,namespace:'isolated',proxies:['10.1.2.3']})}catch(err){if(!/404|405|501/.test(String(err)))throw err}
  const instance=2140000000,path=`/workspaces/openclaw/user-1/instance-${instance}`;
  const probe={instance_id:instance,user_id:1,agent_type:'openclaw',workspace_path:path,generation:1,gateway_port:20000,port_range:{start:20000,end:20002},uid:1000,gid:1000,memory_mb:512,disk_quota_mb:64,environment:{CLAWMANAGER_LLM_BASE_URL:'http://127.0.0.1:9/v1',CLAWMANAGER_LLM_API_KEY:'isolated-validation-only',CLAWMANAGER_LLM_MODEL:'["auto"]',CLAWMANAGER_OPENCLAW_SCHEDULED_TASKS_JSON:'[]'}};
  const result=await request('/v1/gateways','POST',probe);
  assert.equal(result.gateway_id,`gw-${instance}-1`);assert.equal(result.port,20000);assert.ok(['starting','running'].includes(result.status));
  let running=result.status==='running';for(let n=0;!running&&n<120;n++){await new Promise(r=>setTimeout(r,500));const state=await request('/v1/gateways','POST',probe);assert.equal(state.gateway_id,result.gateway_id);if(['failed','error','stopped'].includes(state.status))throw Error(`Probe Gateway failed: ${state.status}`);running=state.status==='running'}assert.ok(running,'Native Gateway readiness timed out');
  assert.equal(docker(['exec',name,'sh','-c',`test -s ${path}/home/.openclaw/openclaw.json && echo CONFIG_WRITTEN`]),'CONFIG_WRITTEN');
  await request(`/v1/gateways/${result.gateway_id}`,'DELETE');
  console.log(`PASS native isolated Gateway startup/config/delete: ${image}`);
 }catch(err){if(started){const logs=spawnSync('docker',['logs','--tail','35',name],{encoding:'utf8'});console.error((logs.stdout+'\n'+logs.stderr).replaceAll(token,'[redacted]'))}throw err}finally{if(started){docker(['stop','-t','10',name]);docker(['rm',name])}}
}
