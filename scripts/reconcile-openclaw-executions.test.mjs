import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {pathToFileURL} from 'node:url';
import {createHash,createHmac,randomUUID} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {planExecutionReconciliation,applyExecutionReconciliation,planExecutionRecovery,resumeExecutionReconciliation,planStaleRecoveryRetirement,retireStaleRecoveryGuard,runExecutionReconciliationCLI} from './reconcile-openclaw-executions.mjs';

const base='http://gateway.invalid/api/gateway/llm',key='local-test-key',generation=7;
const hash=value=>createHash('sha256').update(value).digest('hex');
const namespace=hash(JSON.stringify([base,key,generation]));
let directory,journal,root,calls;
const task=(id='t')=>({generation,session_id:'s',task_id:id});
const state={revoked:false,finished:true,runtime_acknowledged:false};
const fetcher=async(url,options)=>{
  calls.push({url,options});assert.equal(url,base+'/runtime/tasks/inspect');assert.equal(options.headers.Authorization,'Bearer '+key);
  const body=JSON.parse(options.body);
  return Response.json({generation:8,read_only:true,receipt_replay_allowed:false,tasks:body.tasks.map(value=>({...value,state}))});
};
const config=()=>({directory,baseURL:base,apiKey:key,generation:8,sourceGeneration:generation,fetcher});
const approval=digest=>({digest,confirmRuntimeStopped:true,reason:'TEST-123 verified stopped runtime'});
const recoveryApproval=(archiveID,digest)=>({...approval(digest),archiveID,confirmMaintenanceStopped:true});
async function interruptArchive(phase='rename'){
  put();const plan=await planExecutionReconciliation(config());
  const originals={open:fs.openSync,rename:fs.renameSync,rmdir:fs.rmdirSync};
  if(phase==='rename')fs.renameSync=()=>{throw Object.assign(new Error('injected'),{code:'EIO'});};
  if(['prepared','completed'].includes(phase))fs.openSync=(file,...args)=>{if(path.basename(file)===phase+'.json')throw Object.assign(new Error('injected'),{code:'EIO'});return originals.open(file,...args);};
  if(phase==='unlock')fs.rmdirSync=(file,...args)=>{if(path.basename(file)==='.task-maintenance.lock')throw Object.assign(new Error('injected'),{code:'EIO'});return originals.rmdir(file,...args);};
  let failure;
  try{await applyExecutionReconciliation(config(),approval(plan.approval_digest));assert.fail('expected interruption');}catch(error){failure=error;}
  finally{fs.openSync=originals.open;fs.renameSync=originals.rename;fs.rmdirSync=originals.rmdir;}
  assert.equal(failure.maintenanceLockRetained,true);assert.ok(failure.archiveID);
  return {archiveID:failure.archiveID,digest:plan.approval_digest,destination:path.join(directory,'z-task-execution-archive',failure.archiveID)};
}
function put(id='t',cancelled=false){
  const receipt={...task(id),cancelled};
  const mac=createHmac('sha256',key).update(JSON.stringify([namespace,generation,'s',id,cancelled])).digest('hex');
  const receiptFile=path.join(root,hash(JSON.stringify([generation,'s',id]))+'.json');
  fs.writeFileSync(receiptFile,JSON.stringify({receipt,mac}));
  const marker=path.join(journal,randomUUID()+'.json');
  fs.writeFileSync(marker,JSON.stringify({version:1,namespace,...task(id)}));
  return {receiptFile,marker};
}
function tree(dir){return fs.readdirSync(dir).sort().flatMap(name=>{const file=path.join(dir,name),stat=fs.lstatSync(file);return stat.isDirectory()?[[file,'directory'],...tree(file)]:[[file,fs.readFileSync(file,'utf8')]];});}
test.beforeEach(()=>{directory=fs.mkdtempSync(path.join(os.tmpdir(),'cm-execution-reconcile-'));journal=path.join(directory,'z-task-executions');root=path.join(directory,namespace);fs.mkdirSync(journal);fs.mkdirSync(root);calls=[];});
test.afterEach(()=>fs.rmSync(directory,{recursive:true,force:true}));

test('writer claim blocks planning and approved apply without touching evidence',async()=>{
 put();const plan=await planExecutionReconciliation(config());
 fs.mkdirSync(path.join(directory,'.task-writer.lock'));const before=tree(directory);calls=[];
 await assert.rejects(planExecutionReconciliation(config()),{code:'RUNTIME_WRITER_CLAIM_PRESENT'});
 await assert.rejects(applyExecutionReconciliation(config(),approval(plan.approval_digest)),{code:'RUNTIME_WRITER_CLAIM_PRESENT'});
 assert.equal(calls.length,0);assert.deepEqual(tree(directory),before);
});

test('writer appearing during maintenance lock acquisition prevents archive',async()=>{
 const {marker}=put();const plan=await planExecutionReconciliation(config());const original=fs.mkdirSync;
 fs.mkdirSync=(file,...args)=>{const result=original(file,...args);if(path.basename(file)==='.task-maintenance.lock')original(path.join(directory,'.task-writer.lock'));return result;};
 try{await assert.rejects(applyExecutionReconciliation(config(),approval(plan.approval_digest)),{code:'RUNTIME_WRITER_CLAIM_PRESENT'});}finally{fs.mkdirSync=original;}
 assert.ok(fs.existsSync(marker));assert.ok(fs.existsSync(path.join(directory,'.task-writer.lock')));
});

test('default planning is read-only and includes no key, paths or namespace',async()=>{
  put();const before=tree(directory),result=await planExecutionReconciliation(config());
  assert.equal(result.read_only,true);assert.equal(result.eligible,1);assert.match(result.approval_digest,/^[a-f0-9]{64}$/);
  assert.equal(result.receipt_replay_allowed,false);assert.deepEqual(tree(directory),before);
  for(const secret of [key,directory,namespace])assert.ok(!JSON.stringify(result).includes(secret));
});
test('approved apply rechecks server, archives whole journal with audit and retains receipts',async()=>{
  const {receiptFile,marker}=put(),original=fs.readFileSync(receiptFile,'utf8');
  const plan=await planExecutionReconciliation(config()),result=await applyExecutionReconciliation(config(),approval(plan.approval_digest));
  assert.equal(result.action,'archived');assert.equal(result.count,1);assert.equal(result.restart_required,true);assert.equal(calls.length,2);
  assert.equal(fs.existsSync(journal),false);assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),false);
  assert.equal(fs.readFileSync(receiptFile,'utf8'),original);
  const archive=path.join(directory,'z-task-execution-archive',result.archive_id);
  assert.ok(fs.existsSync(path.join(archive,'z-task-executions',path.basename(marker))));
  const audit=JSON.parse(fs.readFileSync(path.join(archive,'prepared.json'),'utf8'));
  assert.equal(audit.approval_digest,plan.approval_digest);assert.equal(audit.operator_confirmed_runtime_stopped,true);
  assert.ok(!JSON.stringify(audit).includes(key));assert.equal(JSON.parse(fs.readFileSync(path.join(archive,'completed.json'),'utf8')).receipts_retained,true);
  await assert.rejects(applyExecutionReconciliation(config(),approval(plan.approval_digest)),{code:'SOURCE_NOT_FOUND'});
  assert.equal(fs.readdirSync(path.join(directory,'z-task-execution-archive')).length,1);
});
test('missing operator approval performs no mutation or network',async()=>{
  put();const before=tree(directory);
  for(const value of [{},{digest:'0'.repeat(64),reason:'ticket'},{digest:'0'.repeat(64),confirmRuntimeStopped:true,reason:''}])await assert.rejects(applyExecutionReconciliation(config(),value),{code:'EXPLICIT_MAINTENANCE_APPROVAL_REQUIRED'});
  assert.equal(calls.length,0);assert.deepEqual(tree(directory),before);
});
test('missing signed receipt, mixed namespace, duplicate and partial markers cannot be planned',async()=>{
  const {receiptFile,marker}=put();let content=fs.readFileSync(marker,'utf8');
  fs.unlinkSync(receiptFile);await assert.rejects(planExecutionReconciliation(config()),{code:'SETTLED_RECEIPT_REQUIRED'});
  const next=put('other');fs.writeFileSync(marker,content.replace(namespace,'a'.repeat(64)));await assert.rejects(planExecutionReconciliation(config()),{code:'UNVERIFIABLE_EXECUTION_JOURNAL'});
  fs.writeFileSync(marker,fs.readFileSync(next.marker));await assert.rejects(planExecutionReconciliation(config()),{code:'DUPLICATE_EXECUTION_IDENTITY'});
  fs.writeFileSync(marker,'partial');await assert.rejects(planExecutionReconciliation(config()),{code:'UNVERIFIABLE_EXECUTION_JOURNAL'});
  assert.equal(calls.length,0);
});
test('only matching settled states qualify; old servers and changed authorization fail closed',async()=>{
  put();const before=tree(directory);
  for(const status of [403,404,503])await assert.rejects(planExecutionReconciliation({...config(),fetcher:async()=>new Response('',{status})}));
  for(const value of [null,{...state,finished:false},{...state,revoked:true},{revoked:true,finished:true,runtime_acknowledged:true}]){
    await assert.rejects(planExecutionReconciliation({...config(),fetcher:async()=>Response.json({generation:8,read_only:true,receipt_replay_allowed:false,tasks:[{...task(),state:value}]})}),{code:'MATCHING_SERVER_COMPLETION_REQUIRED'});
  }
  assert.deepEqual(tree(directory),before);
});
test('a verified cancellation is eligible without sending any finish request',async()=>{
  put('t',true);const result=await planExecutionReconciliation({...config(),fetcher:async url=>{assert.ok(url.endsWith('/inspect'));return Response.json({generation:8,read_only:true,receipt_replay_allowed:false,tasks:[{...task(),state:{revoked:true,finished:true,runtime_acknowledged:true}}]});}});
  assert.equal(result.tasks[0].status,'cancellation_recorded');
});
test('changed or foreign approval digest releases only its own empty maintenance lock',async()=>{
  put();const plan=await planExecutionReconciliation(config());put('second');const before=tree(directory);
  await assert.rejects(applyExecutionReconciliation(config(),approval(plan.approval_digest)),error=>error.code==='APPROVED_PLAN_CHANGED'&&!error.maintenanceLockRetained);
  assert.deepEqual(tree(directory),before);
});
test('existing maintenance lock is never adopted or removed',async()=>{
  put();const plan=await planExecutionReconciliation(config());fs.mkdirSync(path.join(directory,'.task-maintenance.lock'));const before=tree(directory);
  await assert.rejects(applyExecutionReconciliation(config(),approval(plan.approval_digest)),error=>error.code==='MAINTENANCE_ALREADY_LOCKED'&&error.maintenanceLockRetained);
  assert.deepEqual(tree(directory),before);
});

test('apply rejects authorization revoked after planning and leaves the source intact',async()=>{
  put();const plan=await planExecutionReconciliation(config()),before=tree(directory);
  await assert.rejects(applyExecutionReconciliation({...config(),fetcher:async()=>new Response('',{status:403})},approval(plan.approval_digest)),error=>error.code==='CURRENT_INSTANCE_AUTHORIZATION_DENIED'&&!error.maintenanceLockRetained);
  assert.deepEqual(tree(directory),before);
});
test('journal change during server inspection is detected before archive',async()=>{
  const {marker}=put();const original=fs.readFileSync(marker,'utf8');
  await assert.rejects(planExecutionReconciliation({...config(),fetcher:async(...args)=>{fs.writeFileSync(marker,original+' ');return fetcher(...args);}}),{code:'SOURCE_CHANGED_DURING_READ'});
  assert.equal(fs.existsSync(path.join(directory,'z-task-execution-archive')),false);
});
test('rename failure retains journal, prepared audit and maintenance lock',async()=>{
  put();const plan=await planExecutionReconciliation(config()),rename=fs.renameSync;
  fs.renameSync=()=>{throw Object.assign(new Error('sensitive filesystem detail'),{code:'EIO'});};
  try{await assert.rejects(applyExecutionReconciliation(config(),approval(plan.approval_digest)),error=>error.code==='EXECUTION_MAINTENANCE_FAILED'&&error.maintenanceLockRetained&&!!error.archiveID&&!error.message.includes('sensitive'));}finally{fs.renameSync=rename;}
  assert.equal(fs.existsSync(journal),true);assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),true);
  const archiveRoot=path.join(directory,'z-task-execution-archive'),archive=path.join(archiveRoot,fs.readdirSync(archiveRoot)[0]);
  assert.equal(fs.existsSync(path.join(archive,'prepared.json')),true);assert.equal(fs.existsSync(path.join(archive,'completed.json')),false);
});
test('post-rename audit failure retains archive and lock without deleting receipts',async()=>{
  const {receiptFile}=put();const plan=await planExecutionReconciliation(config()),open=fs.openSync;
  fs.openSync=(file,...args)=>{if(path.basename(file)==='completed.json')throw Object.assign(new Error('injected'),{code:'EIO'});return open(file,...args);};
  try{await assert.rejects(applyExecutionReconciliation(config(),approval(plan.approval_digest)),error=>error.maintenanceLockRetained);}finally{fs.openSync=open;}
  assert.equal(fs.existsSync(journal),false);assert.equal(fs.existsSync(receiptFile),true);assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),true);
});
test('CLI defaults to help/read-only and rejects secret arguments and incomplete apply',async()=>{
  const messages=[];await runExecutionReconciliationCLI(['--help'],{},value=>messages.push(value));assert.ok(messages[0].includes('Default is read-only'));
  await assert.rejects(runExecutionReconciliationCLI(['--api-key',key],{}),{code:'INVALID_ARGUMENTS'});
  await assert.rejects(runExecutionReconciliationCLI(['--source-generation','7','--confirm-runtime-stopped'],{CLAWMANAGER_GATEWAY_GENERATION:'8'}),{code:'INVALID_ARGUMENTS'});
});
test('actual runtime writer and guard support archive then restart',{skip:!process.env.OPENCLAW_RECEIPT_STORE_MODULE},async()=>{
  const moduleURL=pathToFileURL(path.resolve(process.env.OPENCLAW_RECEIPT_STORE_MODULE));
  const {createTaskReceiptStore}=await import(moduleURL.href),{createRuntimeTaskGuard}=await import(new URL('./runtime_task_guard.mjs',moduleURL).href);
  const store=createTaskReceiptStore({directory,baseURL:base,apiKey:key,generation});
  store.beginExecution(task());store.put({...task(),cancelled:false});
  const blocked=createRuntimeTaskGuard({baseURL:base,apiKey:key,generation:8,receiptDirectory:directory,fetcher});
  try{assert.equal(blocked.receiptStatus().recoveredExecutions,1);const plan=await planExecutionReconciliation(config());await applyExecutionReconciliation(config(),approval(plan.approval_digest));
    assert.equal(blocked.receiptStatus().storageBlocked,true,'old guard requires restart');
    const restarted=createRuntimeTaskGuard({baseURL:base,apiKey:key,generation:8,receiptDirectory:directory,fetcher});try{assert.equal(restarted.receiptStatus().storageBlocked,false);}finally{restarted.stopReceiptRetries();}
  }finally{blocked.stopReceiptRetries();}
});

for(const phase of ['prepared','rename','completed','unlock'])test('resume safely after interruption at '+phase,async()=>{
  const {archiveID,digest,destination}=await interruptArchive(phase),before=tree(directory);
  const review=await planExecutionRecovery(config(),archiveID);
  assert.equal(review.read_only,true);assert.equal(review.approval_digest,digest);assert.deepEqual(tree(directory),before);
  assert.equal(review.stage,['prepared','rename'].includes(phase)?'before_move':'after_move');
  const result=await resumeExecutionReconciliation(config(),recoveryApproval(archiveID,digest));
  assert.equal(result.action,'archived');assert.equal(result.resumed,true);
  assert.ok(fs.existsSync(path.join(destination,'prepared.json')));assert.ok(fs.existsSync(path.join(destination,'completed.json')));
  assert.ok(fs.existsSync(path.join(destination,'z-task-executions')));assert.equal(fs.existsSync(journal),false);
  assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),false);assert.equal(fs.readdirSync(root).length,1);
  const archived=tree(directory);
  assert.equal((await resumeExecutionReconciliation(config(),recoveryApproval(archiveID,digest))).action,'already_archived');
  assert.deepEqual(tree(directory),archived);
});

test('recovery requires explicit confirmation of stopped maintenance as well as runtime',async()=>{
  const {archiveID,digest}=await interruptArchive(),before=tree(directory);
  await assert.rejects(resumeExecutionReconciliation(config(),{...approval(digest),archiveID}),{code:'EXPLICIT_RECOVERY_APPROVAL_REQUIRED'});
  assert.deepEqual(tree(directory),before);
});
test('tampered signed binding is rejected before any recovery query or write',async()=>{
  const {archiveID}=await interruptArchive(),file=path.join(directory,'.task-maintenance.lock','operation.json');
  const value=JSON.parse(fs.readFileSync(file,'utf8'));value.operation.reason='tampered';fs.writeFileSync(file,JSON.stringify(value));const before=tree(directory),count=calls.length;
  await assert.rejects(planExecutionRecovery(config(),archiveID),{code:'MAINTENANCE_OPERATION_AUTHENTICATION_FAILED'});
  assert.equal(calls.length,count);assert.deepEqual(tree(directory),before);
});
test('a different lock cannot be unlocked by an earlier archive record',async()=>{
  const {archiveID}=await interruptArchive('unlock');
  const lock=path.join(directory,'.task-maintenance.lock');fs.renameSync(lock,path.join(directory,'retained-previous-lock'));fs.mkdirSync(lock);
  const before=tree(directory);await assert.rejects(planExecutionRecovery(config(),archiveID),{code:'MAINTENANCE_LOCK_CHANGED'});assert.deepEqual(tree(directory),before);
});
test('recovery fails closed on partial audit, mixed source/archive and invalid archive ID',async()=>{
  const {archiveID,destination}=await interruptArchive('completed');
  for(const invalid of ['../outside','.',archiveID+'/child'])await assert.rejects(planExecutionRecovery(config(),invalid),{code:'INVALID_ARCHIVE_ID'});
  fs.mkdirSync(journal);await assert.rejects(planExecutionRecovery(config(),archiveID),{code:'AMBIGUOUS_ARCHIVE_STATE'});fs.rmdirSync(journal);
  fs.writeFileSync(path.join(destination,'completed.json'),'{partial');const before=tree(directory);
  await assert.rejects(planExecutionRecovery(config(),archiveID),{code:'INVALID_MAINTENANCE_RECORD'});assert.deepEqual(tree(directory),before);
});
test('revoked authorization after interruption leaves records and lock unchanged',async()=>{
  const {archiveID,digest}=await interruptArchive('completed'),before=tree(directory);
  await assert.rejects(resumeExecutionReconciliation({...config(),fetcher:async()=>new Response('',{status:403})},recoveryApproval(archiveID,digest)),{code:'CURRENT_INSTANCE_AUTHORIZATION_DENIED'});
  assert.deepEqual(tree(directory),before);
});
test('existing recovery guard is never stolen or removed',async()=>{
  const {archiveID,digest}=await interruptArchive(),guard=path.join(directory,'.task-maintenance.lock','resuming');fs.mkdirSync(guard);const before=tree(directory);
  await assert.rejects(resumeExecutionReconciliation(config(),recoveryApproval(archiveID,digest)),{code:'MAINTENANCE_RECOVERY_BUSY'});assert.deepEqual(tree(directory),before);
});
test('recovery failure releases only its own guard and can be retried',async()=>{
  const {archiveID,digest}=await interruptArchive(),rename=fs.renameSync;
  fs.renameSync=()=>{throw Object.assign(new Error('injected'),{code:'EIO'});};
  try{await assert.rejects(resumeExecutionReconciliation(config(),recoveryApproval(archiveID,digest)),error=>error.maintenanceLockRetained);}finally{fs.renameSync=rename;}
  assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock','resuming')),false);assert.equal(fs.existsSync(journal),true);
  assert.equal((await resumeExecutionReconciliation(config(),recoveryApproval(archiveID,digest))).action,'archived');
});
test('recovery CLI rejects incomplete or cross-mode flags',async()=>{
  const env={CLAWMANAGER_GATEWAY_GENERATION:'8'};
  await assert.rejects(runExecutionReconciliationCLI(['--source-generation','7','--confirm-maintenance-stopped'],env),{code:'INVALID_ARGUMENTS'});
  await assert.rejects(runExecutionReconciliationCLI(['--source-generation','7','--resume','../outside'],env),{code:'INVALID_ARCHIVE_ID'});
});

test('real process exit just after journal rename can be recovered',async()=>{
  put();const review=await planExecutionReconciliation(config());
  const source=`import fs from 'node:fs';
    import {applyExecutionReconciliation} from ${JSON.stringify(new URL('./reconcile-openclaw-executions.mjs',import.meta.url).href)};
    const rename=fs.renameSync;fs.renameSync=(...args)=>{rename(...args);process.exit(29);};
    const options=${JSON.stringify({...config(),fetcher:undefined})};
    options.fetcher=async(_url,{body})=>Response.json({generation:8,read_only:true,receipt_replay_allowed:false,tasks:JSON.parse(body).tasks.map(task=>({...task,state:${JSON.stringify(state)}}))});
    await applyExecutionReconciliation(options,${JSON.stringify(approval(review.approval_digest))});`;
  const child=spawnSync(process.execPath,['--input-type=module','-e',source],{encoding:'utf8',timeout:10000});
  assert.equal(child.status,29,child.stderr);
  const binding=JSON.parse(fs.readFileSync(path.join(directory,'.task-maintenance.lock','operation.json'),'utf8'));
  const archiveID=binding.operation.archive_id;
  assert.equal((await planExecutionRecovery(config(),archiveID)).stage,'after_move');
  const result=await resumeExecutionReconciliation(config(),recoveryApproval(archiveID,review.approval_digest));
  assert.equal(result.action,'archived');assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),false);
});

test('lock replacement during recovery server query is rejected before archival writes',async()=>{
  const {archiveID,digest}=await interruptArchive(),lock=path.join(directory,'.task-maintenance.lock');let replaced=false;
  const replacementFetcher=async(...args)=>{if(!replaced){replaced=true;fs.renameSync(lock,path.join(directory,'original-maintenance-lock'));fs.mkdirSync(lock);}return fetcher(...args);};
  await assert.rejects(resumeExecutionReconciliation({...config(),fetcher:replacementFetcher},recoveryApproval(archiveID,digest)),{code:'MAINTENANCE_LOCK_CHANGED'});
  assert.ok(fs.existsSync(journal));assert.equal(fs.readdirSync(lock).length,0);
});

async function crashRecovery(){
  const interrupted=await interruptArchive();
  const source=`import {resumeExecutionReconciliation} from ${JSON.stringify(new URL('./reconcile-openclaw-executions.mjs',import.meta.url).href)};
    const options=${JSON.stringify({...config(),fetcher:undefined})};let requests=0;
    options.fetcher=async(_url,{body})=>{if(++requests===2)process.exit(37);return Response.json({generation:8,read_only:true,receipt_replay_allowed:false,tasks:JSON.parse(body).tasks.map(task=>({...task,state:${JSON.stringify(state)}}))});};
    await resumeExecutionReconciliation(options,${JSON.stringify(recoveryApproval(interrupted.archiveID,interrupted.digest))});`;
  const child=spawnSync(process.execPath,['--input-type=module','-e',source],{encoding:'utf8',timeout:10000});
  assert.equal(child.status,37,child.stderr);
  const ownerFile=path.join(directory,'.task-maintenance.lock','resuming','owner.json');
  const owner=JSON.parse(fs.readFileSync(ownerFile,'utf8'));assert.equal(owner.owner.pid,child.pid);
  return {...interrupted,ownerFile,owner};
}
function rewriteOwner(file,edit){
  const value=JSON.parse(fs.readFileSync(file,'utf8'));edit(value.owner);
  value.mac=createHmac('sha256',key).update(JSON.stringify(['execution-recovery-owner-v1',value.owner])).digest('hex');fs.writeFileSync(file,JSON.stringify(value));
}
const linuxOnly={skip:process.platform!=='linux'};
test('crashed recovery leaves a signed process owner; unsupported hosts refuse retirement',async()=>{
  const {archiveID,owner}=await crashRecovery();assert.match(owner.mac,/^[a-f0-9]{64}$/);
  const before=tree(directory);
  if(process.platform!=='linux')await assert.rejects(planStaleRecoveryRetirement(config(),archiveID),{code:'RECOVERY_OWNER_PROBE_UNSUPPORTED'});
  else assert.equal((await planStaleRecoveryRetirement(config(),archiveID)).owner_state,'absent_on_same_linux_boot_and_pid_namespace');
  assert.deepEqual(tree(directory),before);
});
test('tampered recovery owner never authorizes retirement',async()=>{
  const {archiveID,ownerFile}=await crashRecovery();const value=JSON.parse(fs.readFileSync(ownerFile,'utf8'));value.owner.pid++;fs.writeFileSync(ownerFile,JSON.stringify(value));const before=tree(directory);
  await assert.rejects(planStaleRecoveryRetirement(config(),archiveID),{code:'RECOVERY_OWNER_AUTHENTICATION_FAILED'});assert.deepEqual(tree(directory),before);
});
test('Linux dead owner can be archived before normal recovery resumes',linuxOnly,async()=>{
  const {archiveID,digest,owner}=await crashRecovery(),before=tree(directory);
  const review=await planStaleRecoveryRetirement(config(),archiveID);assert.deepEqual(tree(directory),before);
  await assert.rejects(retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,digest)),{code:'APPROVED_PLAN_CHANGED'});
  const result=await retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,review.approval_digest));
  assert.equal(result.maintenance_lock_retained,true);assert.equal(result.action,'recovery_guard_retired');
  const retired=path.join(directory,'z-task-execution-archive',archiveID,'recovery-guards',owner.owner.token);
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(retired,'owner.json'),'utf8')),owner);assert.ok(fs.existsSync(path.join(retired,'retirement.json')));
  assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock','resuming')),false);
  assert.equal((await resumeExecutionReconciliation(config(),recoveryApproval(archiveID,digest))).action,'archived');
});
test('Linux live or reused PID is never treated as a dead owner',linuxOnly,async()=>{
  const {archiveID,ownerFile}=await crashRecovery();rewriteOwner(ownerFile,value=>{value.pid=process.pid;});const before=tree(directory);
  await assert.rejects(planStaleRecoveryRetirement(config(),archiveID),{code:'RECOVERY_OWNER_STILL_RUNNING'});assert.deepEqual(tree(directory),before);
});
test('Linux cross-boot or cross-namespace ownership is rejected',linuxOnly,async()=>{
  const {archiveID,ownerFile}=await crashRecovery();rewriteOwner(ownerFile,value=>{value.scope='a'.repeat(64);});const before=tree(directory);
  await assert.rejects(planStaleRecoveryRetirement(config(),archiveID),{code:'RECOVERY_OWNER_SCOPE_MISMATCH'});assert.deepEqual(tree(directory),before);
});
test('Linux process probe permission errors do not authorize retirement',linuxOnly,async()=>{
  const {archiveID}=await crashRecovery(),kill=process.kill;process.kill=()=>{throw Object.assign(new Error('denied'),{code:'EPERM'});};
  try{await assert.rejects(planStaleRecoveryRetirement(config(),archiveID),{code:'RECOVERY_OWNER_STATE_UNKNOWN'});}finally{process.kill=kill;}
});
test('Linux failed retirement retains the guard for a verified retry',linuxOnly,async()=>{
  const {archiveID}=await crashRecovery(),review=await planStaleRecoveryRetirement(config(),archiveID),rename=fs.renameSync;
  fs.renameSync=()=>{throw Object.assign(new Error('injected'),{code:'EIO'});};
  try{await assert.rejects(retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,review.approval_digest)),error=>error.maintenanceLockRetained);}finally{fs.renameSync=rename;}
  assert.ok(fs.existsSync(path.join(directory,'.task-maintenance.lock','resuming','owner.json')));
  assert.equal((await retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,review.approval_digest))).action,'recovery_guard_retired');
});
test('Linux retired owner audit remains authenticated during later recovery',linuxOnly,async()=>{
  const {archiveID,owner}=await crashRecovery(),review=await planStaleRecoveryRetirement(config(),archiveID);
  await retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,review.approval_digest));
  const file=path.join(directory,'z-task-execution-archive',archiveID,'recovery-guards',owner.owner.token,'retirement.json');
  const value=JSON.parse(fs.readFileSync(file,'utf8'));value.record.reason='tampered';fs.writeFileSync(file,JSON.stringify(value));
  await assert.rejects(planExecutionRecovery(config(),archiveID),{code:'INVALID_MAINTENANCE_RECORD'});
});
test('retirement mode requires an archive ID and explicit execution approvals',async()=>{
  await assert.rejects(runExecutionReconciliationCLI(['--source-generation','7','--retire-stale-recovery'],{CLAWMANAGER_GATEWAY_GENERATION:'8'}),{code:'INVALID_ARGUMENTS'});
  await assert.rejects(retireStaleRecoveryGuard(config(),{}),{code:'EXPLICIT_RECOVERY_APPROVAL_REQUIRED'});
});

test('Linux full recovery history retains the current guard without appending an unreadable entry',linuxOnly,async()=>{
  const {archiveID,destination,owner}=await crashRecovery();
  const history=path.join(destination,'recovery-guards');fs.mkdirSync(history);
  for(let i=0;i<256;i++){
    const token=randomUUID(),entry=path.join(history,token);fs.mkdirSync(entry);
    const stat=fs.statSync(entry,{bigint:true});
    const record={...owner.owner,token,guard_identity:{device:String(stat.dev),inode:String(stat.ino),birthtime_ns:String(stat.birthtimeNs)}};
    const mac=createHmac('sha256',key).update(JSON.stringify(['execution-recovery-owner-v1',record])).digest('hex');
    fs.writeFileSync(path.join(entry,'owner.json'),JSON.stringify({owner:record,mac}));
  }
  const review=await planStaleRecoveryRetirement(config(),archiveID),before=tree(directory);
  await assert.rejects(retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,review.approval_digest)),{code:'RECOVERY_HISTORY_FULL'});
  assert.deepEqual(tree(directory),before);assert.equal(fs.readdirSync(history).length,256);
});

test('Linux concurrent retirement preserves a single archived guard',linuxOnly,async()=>{
  const {archiveID,destination,owner}=await crashRecovery(),review=await planStaleRecoveryRetirement(config(),archiveID);
  const outcomes=await Promise.allSettled([retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,review.approval_digest)),retireStaleRecoveryGuard(config(),recoveryApproval(archiveID,review.approval_digest))]);
  assert.equal(outcomes.filter(value=>value.status==='fulfilled').length,1);
  const history=path.join(destination,'recovery-guards');assert.deepEqual(fs.readdirSync(history),[owner.owner.token]);
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(history,owner.owner.token,'owner.json'),'utf8')),owner);
  assert.ok(fs.existsSync(path.join(directory,'.task-maintenance.lock')));
});

const oldSource={baseURL:'http://old-gateway.invalid/api/gateway/llm',apiKey:'original-rotated-test-key',generation:6};
function putSource(source,id='older',cancelled=false){
  const ns=hash(JSON.stringify([source.baseURL,source.apiKey,source.generation]));
  const dir=path.join(directory,ns);fs.mkdirSync(dir,{recursive:true});
  const receipt={generation:source.generation,session_id:'s',task_id:id,cancelled};
  const mac=createHmac('sha256',source.apiKey).update(JSON.stringify([ns,source.generation,'s',id,cancelled])).digest('hex');
  const receiptFile=path.join(dir,hash(JSON.stringify([source.generation,'s',id]))+'.json');
  fs.writeFileSync(receiptFile,JSON.stringify({receipt,mac}));
  const marker=path.join(journal,randomUUID()+'.json');
  fs.writeFileSync(marker,JSON.stringify({version:1,namespace:ns,generation:source.generation,session_id:'s',task_id:id}));
  return {receiptFile,marker,namespace:ns};
}
const mixedConfig=()=>({...config(),additionalSources:[oldSource]});

test('explicit mixed generations and rotated credentials archive together while retaining original receipts',async()=>{
  const a=put(),b=putSource(oldSource),before=tree(directory);
  const plan=await planExecutionReconciliation(mixedConfig());
  assert.equal(plan.source_count,2);assert.deepEqual(plan.source_generations,[6,7]);assert.equal(plan.eligible,2);
  assert.deepEqual(tree(directory),before);
  for(const secret of [key,oldSource.apiKey,oldSource.baseURL,b.namespace])assert.ok(!JSON.stringify(plan).includes(secret));
  const receipts=[a,b].map(item=>fs.readFileSync(item.receiptFile,'utf8'));
  const result=await applyExecutionReconciliation(mixedConfig(),approval(plan.approval_digest));
  assert.equal(result.count,2);assert.equal(result.receipt_replay_allowed,false);
  assert.equal(fs.existsSync(journal),false);
  assert.deepEqual([a,b].map(item=>fs.readFileSync(item.receiptFile,'utf8')),receipts);
  const archived=tree(path.join(directory,'z-task-execution-archive'));
  assert.ok(!JSON.stringify(archived).includes(oldSource.apiKey));
  assert.ok(calls.every(call=>call.url===base+'/runtime/tasks/inspect'&&call.options.headers.Authorization==='Bearer '+key));
  const again=await resumeExecutionReconciliation(mixedConfig(),recoveryApproval(result.archive_id,plan.approval_digest));
  assert.equal(again.action,'already_archived');
});

test('same generation key rotation is supported for distinct task identities',async()=>{
  put();const rotated={...oldSource,generation};putSource(rotated);
  const result=await planExecutionReconciliation({...config(),additionalSources:[rotated]});
  assert.equal(result.eligible,2);assert.deepEqual(result.source_generations,[7]);
});

test('mixed source manifest rejects duplicates, malformed, excessive and future sources without network or writes',async()=>{
  put();putSource(oldSource);const before=tree(directory);
  for(const sources of [{},[null],[{...oldSource,unknown:'secret'}],[{...oldSource,generation:9}],Array(8).fill(oldSource),[oldSource,oldSource],[{baseURL:base,apiKey:key,generation}]]){
    await assert.rejects(planExecutionReconciliation({...config(),additionalSources:sources}));
  }
  assert.equal(calls.length,0);assert.deepEqual(tree(directory),before);
});

test('single-source mode cannot adopt a mixed journal and aggregate scan limits apply to all sources',async()=>{
  put();putSource(oldSource);const before=tree(directory);
  await assert.rejects(planExecutionReconciliation(config()),{code:'UNVERIFIABLE_EXECUTION_JOURNAL'});
  await assert.rejects(planExecutionReconciliation({...mixedConfig(),maxEntries:1}),{code:'SOURCE_SCAN_LIMIT_EXCEEDED'});
  assert.equal(calls.length,0);assert.deepEqual(tree(directory),before);
});

test('one unconfirmed historical task blocks the entire mixed archive',async()=>{
  put();putSource(oldSource);const before=tree(directory);
  const options={...mixedConfig(),fetcher:async(url,request)=>{
    assert.equal(url,base+'/runtime/tasks/inspect');
    return Response.json({generation:8,read_only:true,receipt_replay_allowed:false,tasks:JSON.parse(request.body).tasks.map(t=>({...t,state:t.generation===6?null:state}))});
  }};
  await assert.rejects(planExecutionReconciliation(options),{code:'MATCHING_SERVER_COMPLETION_REQUIRED'});
  assert.deepEqual(tree(directory),before);
});

test('receipt evidence must match the marker original namespace, not just its task identity',async()=>{
  put();const rotated={...oldSource,generation},b=putSource(rotated,'other');
  // Move only the marker into the primary namespace; its receipt is still
  // authenticated by the rotated key and must not authorize this marker.
  const marker=JSON.parse(fs.readFileSync(b.marker));marker.namespace=namespace;
  fs.writeFileSync(b.marker,JSON.stringify(marker));
  await assert.rejects(planExecutionReconciliation({...config(),additionalSources:[rotated]}),{code:'SETTLED_RECEIPT_REQUIRED'});
  assert.equal(calls.length,0);
});

test('duplicate logical task identities across credentials fail closed',async()=>{
  put();putSource({...oldSource,generation},'t');
  await assert.rejects(planExecutionReconciliation({...config(),additionalSources:[{...oldSource,generation}]}),{code:'DUPLICATE_EXECUTION_IDENTITY'});
});

test('all original receipt sources are rechecked after server awaits',async()=>{
  put();const b=putSource(oldSource);let once=false;
  await assert.rejects(planExecutionReconciliation({...mixedConfig(),fetcher:async(...args)=>{
    const result=await fetcher(...args);
    if(!once){once=true;fs.unlinkSync(b.receiptFile);}
    return result;
  }}));
  assert.equal(fs.existsSync(journal),true);assert.equal(fs.existsSync(path.join(directory,'z-task-execution-archive')),false);
});

test('source manifest order is stable, but adding a namespace invalidates approval',async()=>{
  put();putSource(oldSource);const third={...oldSource,generation:5};putSource(third,'oldest');
  const options={...config(),additionalSources:[oldSource,third]};
  const a=await planExecutionReconciliation(options),b=await planExecutionReconciliation({...options,additionalSources:[third,oldSource]});
  assert.equal(a.approval_digest,b.approval_digest);
  const extra={...oldSource,generation:4},ns=hash(JSON.stringify([extra.baseURL,extra.apiKey,extra.generation]));fs.mkdirSync(path.join(directory,ns));
  const before=tree(directory);
  await assert.rejects(applyExecutionReconciliation({...options,additionalSources:[oldSource,third,extra]},approval(a.approval_digest)),{code:'APPROVED_PLAN_CHANGED'});
  assert.deepEqual(tree(directory),before);
});

test('mixed generation archive resumes after directory move without replay or rewriting receipts',async()=>{
  put();putSource(oldSource);const options=mixedConfig(),plan=await planExecutionReconciliation(options);
  const open=fs.openSync;let interrupted;
  fs.openSync=(file,...args)=>{if(path.basename(file)==='completed.json')throw Object.assign(new Error('injected'),{code:'EIO'});return open(file,...args);};
  try{await applyExecutionReconciliation(options,approval(plan.approval_digest));assert.fail('expected failure');}catch(error){interrupted=error;}finally{fs.openSync=open;}
  assert.equal(interrupted.maintenanceLockRetained,true);assert.ok(interrupted.archiveID);
  await assert.rejects(planExecutionRecovery(config(),interrupted.archiveID));
  const review=await planExecutionRecovery(options,interrupted.archiveID);assert.equal(review.stage,'after_move');
  const result=await resumeExecutionReconciliation(options,recoveryApproval(interrupted.archiveID,review.approval_digest));
  assert.equal(result.count,2);assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),false);
});

test('CLI additional sources are opt-in, bounded, environment-only and do not print credentials',async()=>{
  put();putSource(oldSource);
  const env={CLAWMANAGER_TASK_RECEIPT_DIR:directory,CLAWMANAGER_LLM_BASE_URL:base,CLAWMANAGER_LLM_API_KEY:key,CLAWMANAGER_GATEWAY_GENERATION:'8',CLAWMANAGER_RECEIPT_ADDITIONAL_SOURCES_JSON:JSON.stringify([oldSource])};
  const original=globalThis.fetch;globalThis.fetch=fetcher;
  try{
    await assert.rejects(runExecutionReconciliationCLI(['--source-generation','7'],env),{code:'UNVERIFIABLE_EXECUTION_JOURNAL'});
    let output='';await runExecutionReconciliationCLI(['--source-generation','7','--additional-sources'],env,value=>{output=value;});
    assert.equal(JSON.parse(output).source_count,2);assert.ok(!output.includes(oldSource.apiKey));
    for(const raw of [undefined,'invalid','[]','null',' '.repeat(65537)+'[]']){
      await assert.rejects(runExecutionReconciliationCLI(['--source-generation','7','--additional-sources'],{...env,CLAWMANAGER_RECEIPT_ADDITIONAL_SOURCES_JSON:raw}),{code:'INVALID_CONFIGURATION'});
    }
  }finally{globalThis.fetch=original;}
});
