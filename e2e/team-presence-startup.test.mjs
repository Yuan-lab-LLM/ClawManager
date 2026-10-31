// Real installed 7.1/10.2 Agents and Redis. No inbox messages or model calls.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
const images=(process.env.TEAM_PRESENCE_IMAGES||'10.130.14.23:5000/agentsruntime/openclaw-lite:master-20260824-737ad4c,10.130.14.23:5000/agentsruntime/openclaw-lite:team-openclaw-10.2-recovery-20261013-v1').split(',');
const observationMs=Math.max(5000,Number(process.env.TEAM_PRESENCE_OBSERVE_MS||300000));
const prefix=`presence-startup-${process.pid}`,redis=`${prefix}-redis`,containers=[],gateways=[],policyTimers=[];
let policyError;
const pause=ms=>new Promise(r=>setTimeout(r,ms));let network=false;
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8',timeout:120000});if(r.status!==0)throw Error(`Isolated Docker ${args[0]} failed`);return r.stdout.trim()}
function start(name,image,args=[]){docker(['run','-d','--name',name,'--network',prefix,'--label',`codex.task=${prefix}`,...args,image]);containers.push(name)}
const env=e=>Object.entries(e).flatMap(([k,v])=>['-e',`${k}=${v}`]);
try{
 docker(['network','create',prefix]);network=true;start(redis,'redis:7-alpine');
 for(let n=0;n<images.length;n++){
  const name=`${prefix}-${n}`,token=randomBytes(24).toString('hex'),image=images[n];
  start(name,image,['--tmpfs','/workspaces:rw,size=384m','-p','127.0.0.1::19090','--entrypoint','/usr/local/bin/clawmanager-agent',...env({CLAWMANAGER_RUNTIME_TYPE:'openclaw',RUNTIME_AGENT_MODE:'runtime-pod',RUNTIME_AGENT_CONTROL_TOKEN:token,RUNTIME_AGENT_REPORT_TOKEN:token,CLAWMANAGER_BACKEND_URL:'http://127.0.0.1:9',CLAWMANAGER_RUNTIME_IMAGE_REF:image,POD_NAME:name,POD_NAMESPACE:'isolated',POD_UID:name,RUNTIME_WORKSPACE_ROOT:'/workspaces',RUNTIME_GATEWAY_STARTUP_HEALTH_TIMEOUT:'180s'})]);
  const port=JSON.parse(docker(['inspect','-f','{{json .NetworkSettings.Ports}}',name]))['19090/tcp'][0].HostPort;
  const request=async(path,method='GET',body)=>{const r=await fetch(`http://127.0.0.1:${port}${path}`,{method,headers:{'X-ClawManager-Control-Token':token,'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(100000)});if(!r.ok)throw Error(`Native Agent ${method} ${path} returned ${r.status}`);const text=await r.text();return text?JSON.parse(text):null};
  let ready=false;for(let k=0;k<60;k++){try{await request('/v1/health');ready=true;break}catch{}await pause(500)}assert.ok(ready);
  const renewPolicy=async()=>{const policy=await request('/v1/proxy-policy');await request('/v1/proxy-policy','PUT',{expected_revision:policy.revision,namespace:'isolated',proxies:['10.1.2.3']})};
  try{await renewPolicy();policyTimers.push(setInterval(()=>void renewPolicy().catch(err=>{policyError=err}),20000))}catch(err){if(!/404|405|501/.test(String(err)))throw err}
  const team=800000+n,instance=2140000000+n,shared=`/workspaces/teams/user-1/team-${team}-shared`;
  const probe={instance_id:instance,user_id:1,agent_type:'openclaw',workspace_path:`/workspaces/openclaw/user-1/instance-${instance}`,generation:1,gateway_port:20000,port_range:{start:20000,end:20002},uid:1000,gid:1000,memory_mb:1024,disk_quota_mb:64,environment:{CLAWMANAGER_LLM_BASE_URL:'http://127.0.0.1:9/v1',CLAWMANAGER_LLM_API_KEY:'isolated-unused',CLAWMANAGER_LLM_MODEL:'["auto"]',CLAWMANAGER_TEAM_ENABLED:'true',CLAWMANAGER_TEAM_ID:String(team),CLAWMANAGER_TEAM_MEMBER_ID:'worker',CLAWMANAGER_TEAM_ROLE:'worker',CLAWMANAGER_TEAM_REDIS_URL:`redis://${redis}:6379/0`,CLAWMANAGER_TEAM_SHARED_DIR:shared,CLAWMANAGER_TEAM_CONFIG_JSON:JSON.stringify({teamId:String(team),members:[{memberId:'worker',role:'worker'}]}),CLAWMANAGER_OPENCLAW_SCHEDULED_TASKS_JSON:'[]'}};
  let response=await request('/v1/gateways','POST',probe);for(let k=0;response.status!=='running'&&k<480;k++){await pause(500);response=await request('/v1/gateways','POST',probe);if(['error','failed'].includes(response.status))break}assert.equal(response.status,'running');
  gateways.push({team,request,id:response.gateway_id,image});
  console.log(`Native Gateway ready without task: ${image}`);
 }
 const startTime=Date.now();let samples=0;const firstHeartbeat=new Map(),lastHeartbeat=new Map();
 while(Date.now()-startTime<observationMs){
  if(policyError)throw policyError;
  for(const g of gateways){
   const raw=docker(['exec',redis,'redis-cli','--raw','HGET',`claw:team:${g.team}:presence`,'worker']);
   if(!raw&&Date.now()-startTime<45000)continue;
   const status=JSON.parse(raw);assert.equal(status.liveness,'online');assert.equal(status.memberId,'worker');assert.ok(Date.now()-Date.parse(status.lastSeenAt)<45000,'idle heartbeat expired');
   if(!firstHeartbeat.has(g.team))firstHeartbeat.set(g.team,Date.parse(status.lastSeenAt));lastHeartbeat.set(g.team,Date.parse(status.lastSeenAt));
   assert.equal(docker(['exec',redis,'redis-cli','EXISTS',`claw:team:${g.team}:inbox:worker`]),'1');
   assert.equal(docker(['exec',redis,'redis-cli','XLEN',`claw:team:${g.team}:inbox:worker`]),'0','a task was sent');
  }
  samples++;await pause(10000);
 }
 assert.ok(samples>=Math.min(10,Math.ceil(observationMs/10000)));
 for(const g of gateways){assert.ok(firstHeartbeat.has(g.team)&&lastHeartbeat.get(g.team)-firstHeartbeat.get(g.team)>=Math.max(0,observationMs-60000),'native idle heartbeats did not advance across the observation window');console.log(`PASS no-task idle presence for >=${observationMs/1000} seconds: ${g.image}`);await g.request(`/v1/gateways/${g.id}`,'DELETE')}
}catch(error){for(const name of containers.filter(n=>n!==redis)){const r=spawnSync('docker',['logs','--tail','45',name],{encoding:'utf8'});console.error((r.stdout+r.stderr).split('\n').filter(line=>!/password|token|secret|authorization/i.test(line)).slice(-25).join('\n'))}throw error}
finally{for(const timer of policyTimers)clearInterval(timer);for(const name of containers.reverse()){try{docker(['rm','-f',name])}catch{}}if(network)try{docker(['network','rm',prefix])}catch{}}
