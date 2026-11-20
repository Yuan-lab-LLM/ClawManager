import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {createHash,createHmac,randomUUID} from 'node:crypto';
import {pathToFileURL} from 'node:url';
import {spawn,spawnSync} from 'node:child_process';
import {planWriterRetirement,applyWriterRetirement,planWriterRecovery,resumeWriterRetirement,planWriterGuardRetirement,retireWriterRecoveryGuard,runWriterRetirementCLI} from './reconcile-openclaw-writer.mjs';
const baseURL='http://gateway.invalid/api/gateway/llm',apiKey='offline-test-key',generation=7;
const hash=b=>createHash('sha256').update(b).digest('hex');
const mac=value=>createHmac('sha256',apiKey).update(JSON.stringify(value)).digest('hex');
let directory,claim,owner,receipt,ownerBytes;
test.beforeEach(()=>{
 directory=fs.mkdtempSync(path.join(os.tmpdir(),'cm-writer-retire-'));claim=path.join(directory,'.task-writer.lock');fs.mkdirSync(claim);
 owner=path.join(claim,'owner.json');ownerBytes=JSON.stringify({version:1,generation,owner:randomUUID()});fs.writeFileSync(owner,ownerBytes);
 const root=path.join(directory,'z-runtime-shutdowns');fs.mkdirSync(root);const namespace=hash(JSON.stringify([baseURL,apiKey,generation])),id=hash(ownerBytes);
 const record={version:1,namespace,generation,claim_id:id,native_cleanup_confirmed:true};receipt=path.join(root,namespace+'-'+id+'.json');
 fs.writeFileSync(receipt,JSON.stringify({record,mac:mac(['native-cleanup-v1',namespace,generation,id,true])}));
});
test.afterEach(()=>fs.rmSync(directory,{recursive:true,force:true}));
const options=()=>({directory,baseURL,apiKey,generation});
const approval=digest=>({digest,confirmRuntimeStopped:true,fenceReference:'TEST-FENCE-1',reason:'verified stopped and isolated'});
const recoveryApproval=(archiveID,digest)=>({...approval(digest),archiveID,confirmMaintenanceStopped:true});
function interruptWriter(phase){
 const plan=planWriterRetirement(options());const original={open:fs.openSync,rename:fs.renameSync,rmdir:fs.rmdirSync};
 if(phase==='before_prepared'||phase==='before_completed')fs.openSync=(file,...args)=>{if(path.basename(file)===(phase==='before_prepared'?'prepared.json':'completed.json'))throw Object.assign(new Error('injected'),{code:'EIO'});return original.open(file,...args);};
 if(phase==='before_move')fs.renameSync=()=>{throw Object.assign(new Error('injected'),{code:'EIO'});};
 if(phase==='before_unlock')fs.rmdirSync=(dir,...args)=>{if(path.basename(dir)==='.task-maintenance.lock')throw Object.assign(new Error('injected'),{code:'EIO'});return original.rmdir(dir,...args);};
 let failure;try{applyWriterRetirement(options(),approval(plan.approval_digest));assert.fail('injected interruption required');}catch(e){failure=e;}finally{fs.openSync=original.open;fs.renameSync=original.rename;fs.rmdirSync=original.rmdir;}
 assert.ok(failure.archiveID);assert.equal(failure.maintenanceLockRetained,true);
 return {archiveID:failure.archiveID,digest:plan.approval_digest};
}
function crashWriterRecovery(){
 const stopped=interruptWriter('before_move'),script=new URL('./reconcile-openclaw-writer.mjs',import.meta.url).href;
 const code=`import fs from 'node:fs';import {resumeWriterRetirement} from ${JSON.stringify(script)};const rename=fs.renameSync;fs.renameSync=(...a)=>{rename(...a);process.exit(74);};resumeWriterRetirement(JSON.parse(process.env.WRITER_TEST_OPTIONS),JSON.parse(process.env.WRITER_TEST_APPROVAL));`;
 const child=spawnSync(process.execPath,['--input-type=module','-e',code],{env:{...process.env,WRITER_TEST_OPTIONS:JSON.stringify(options()),WRITER_TEST_APPROVAL:JSON.stringify(recoveryApproval(stopped.archiveID,stopped.digest))},encoding:'utf8'});
 assert.equal(child.status,74,child.stderr);
 return {...stopped,guard:path.join(directory,'.task-maintenance.lock','writer-resuming')};
}
function changeRecoveryOwner(guard,change){const file=path.join(guard,'owner.json'),value=JSON.parse(fs.readFileSync(file,'utf8'));change(value.owner);value.mac=mac(['writer-recovery-owner-v1',value.owner]);fs.writeFileSync(file,JSON.stringify(value));}
const tree=dir=>fs.readdirSync(dir).sort().flatMap(n=>{const p=path.join(dir,n);return fs.lstatSync(p).isDirectory()?[[p,'dir'],...tree(p)]:[[p,fs.readFileSync(p,'utf8')]];});
test('read-only plan requires exact signed cleanup and discloses no source secrets',()=>{
 const before=tree(directory),p=planWriterRetirement(options());assert.equal(p.read_only,true);assert.equal(p.release_confirmed,false);assert.equal(p.requires_runtime_fence,true);
 assert.deepEqual(tree(directory),before);assert.ok(!JSON.stringify(p).includes(apiKey));assert.ok(!JSON.stringify(p).includes(directory));
});
test('approved retirement archives original owner and preserves signed receipt',()=>{
 const before=fs.readFileSync(receipt,'utf8'),p=planWriterRetirement(options());const result=applyWriterRetirement(options(),approval(p.approval_digest));
 assert.equal(result.action,'restart_required');assert.equal(result.receipt_replay_allowed,false);assert.equal(fs.existsSync(claim),false);
 const dest=path.join(directory,'z-runtime-writer-archive',result.archive_id);assert.equal(fs.readFileSync(path.join(dest,'writer','owner.json'),'utf8'),ownerBytes);
 assert.equal(fs.readFileSync(receipt,'utf8'),before);assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),false);
 const prepared=JSON.parse(fs.readFileSync(path.join(dest,'prepared.json'),'utf8'));assert.equal(prepared.operation.fence_reference,'TEST-FENCE-1');assert.equal(prepared.mac,mac(['writer-retirement-operation-v1',prepared.operation]));
 assert.equal(fs.readFileSync(path.join(dest,'prepared.json'),'utf8'),fs.readFileSync(path.join(dest,'completed.json'),'utf8'));
});
test('missing administrator fence and changed approval do not mutate source',()=>{
 const p=planWriterRetirement(options()),before=tree(directory);
 for(const change of [{confirmRuntimeStopped:false},{fenceReference:''},{reason:''},{digest:'f'.repeat(64)}])assert.throws(()=>applyWriterRetirement(options(),{...approval(p.approval_digest),...change}));
 assert.deepEqual(tree(directory),before);
});
test('missing or tampered cleanup cannot retire crashed ownership',()=>{
 const data=fs.readFileSync(receipt,'utf8');fs.writeFileSync(receipt,data.replace('"native_cleanup_confirmed":true','"native_cleanup_confirmed":false'));
 assert.throws(()=>planWriterRetirement(options()),{code:'NATIVE_CLEANUP_NOT_CONFIRMED'});fs.unlinkSync(receipt);
 assert.throws(()=>planWriterRetirement(options()));assert.ok(fs.existsSync(owner));
});
test('pending executions and partial claims are uncertainty, not stop evidence',()=>{
 const journal=path.join(directory,'z-task-executions');fs.mkdirSync(journal);fs.writeFileSync(path.join(journal,'partial'),'unknown');
 assert.throws(()=>planWriterRetirement(options()),{code:'EXECUTION_RECONCILIATION_REQUIRED'});
 fs.rmSync(journal,{recursive:true});fs.writeFileSync(owner,'partial');assert.throws(()=>planWriterRetirement(options()),{code:'INVALID_WRITER_EVIDENCE'});
});
test('claim replacement after planning invalidates approval',()=>{
 const p=planWriterRetirement(options());fs.writeFileSync(owner,JSON.stringify({version:1,generation,owner:randomUUID()}));
 assert.throws(()=>applyWriterRetirement(options(),approval(p.approval_digest)));assert.ok(fs.existsSync(owner));
});
test('failure after archive move retains maintenance barrier and original evidence',()=>{
 const p=planWriterRetirement(options()),open=fs.openSync;
 fs.openSync=(file,...args)=>{if(path.basename(file)==='completed.json')throw Object.assign(new Error('injected'),{code:'EIO'});return open(file,...args);};
 let failure;try{applyWriterRetirement(options(),approval(p.approval_digest));assert.fail('must fail');}catch(e){failure=e;}finally{fs.openSync=open;}
 assert.equal(failure.maintenanceLockRetained,true);assert.ok(failure.archiveID);assert.ok(fs.existsSync(receipt));
 assert.equal(fs.readFileSync(path.join(directory,'z-runtime-writer-archive',failure.archiveID,'writer','owner.json'),'utf8'),ownerBytes);
 assert.throws(()=>planWriterRetirement(options()),{code:'MAINTENANCE_ALREADY_LOCKED'});
});
test('writer changing during maintenance acquisition prevents archive',()=>{
 const p=planWriterRetirement(options()),mkdir=fs.mkdirSync;
 fs.mkdirSync=(dir,...args)=>{const result=mkdir(dir,...args);if(path.basename(dir)==='.task-maintenance.lock')fs.writeFileSync(owner,'changed');return result;};
 try{assert.throws(()=>applyWriterRetirement(options(),approval(p.approval_digest)));}finally{fs.mkdirSync=mkdir;}
 assert.equal(fs.readFileSync(owner,'utf8'),'changed');assert.ok(fs.existsSync(path.join(directory,'.task-maintenance.lock')));
});
test('CLI is read-only by default and accepts no credential arguments',()=>{
 const env={CLAWMANAGER_TASK_RECEIPT_DIR:directory,CLAWMANAGER_LLM_BASE_URL:baseURL,CLAWMANAGER_LLM_API_KEY:apiKey};let out='';
 runWriterRetirementCLI(['--source-generation','7'],env,v=>out=v);assert.equal(JSON.parse(out).read_only,true);
 for(const args of [['--api-key','secret'],['--source-generation','7','--apply'],['--source-generation','7','--confirm-runtime-stopped']])assert.throws(()=>runWriterRetirementCLI(args,env,()=>{}));
 assert.ok(fs.existsSync(owner));
});

test('offline reader verifies actual runtime writer and shutdown receipt',{skip:!process.env.OPENCLAW_RECEIPT_STORE_MODULE},async()=>{
 const root=path.dirname(process.env.OPENCLAW_RECEIPT_STORE_MODULE);
 const {acquireRuntimeWriter}=await import(pathToFileURL(path.join(root,'runtime_writer_guard.mjs')).href);
 const {createShutdownReceiptStore}=await import(pathToFileURL(path.join(root,'shutdown_receipt_store.mjs')).href);
 fs.rmSync(claim,{recursive:true});
 const writer=acquireRuntimeWriter({directory,generation});
 const store=createShutdownReceiptStore({directory,baseURL,apiKey,generation});store.write(writer.claimID);
 const p=planWriterRetirement(options()),result=applyWriterRetirement(options(),approval(p.approval_digest));
 assert.equal(result.archived,true);assert.equal(store.read(writer.claimID).native_cleanup_confirmed,true);
 assert.throws(()=>writer.assertOwned());
});

for(const stage of ['before_prepared','before_move','before_completed','before_unlock'])test('recovery resumes safely from '+stage,()=>{
 const stopped=interruptWriter(stage),before=tree(directory),plan=planWriterRecovery(options(),stopped.archiveID);
 assert.equal(plan.stage,stage);assert.deepEqual(tree(directory),before);assert.equal(plan.read_only,true);
 const result=resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest));
 assert.equal(result.resumed,true);assert.equal(fs.existsSync(claim),false);assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),false);
 assert.equal(fs.readFileSync(path.join(directory,'z-runtime-writer-archive',stopped.archiveID,'writer','owner.json'),'utf8'),ownerBytes);
 assert.equal(resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)).action,'already_archived');
});
test('recovery requires runtime and maintenance isolation plus unchanged approval',()=>{
 const stopped=interruptWriter('before_completed'),before=tree(directory),a=recoveryApproval(stopped.archiveID,stopped.digest);
 for(const change of [{confirmRuntimeStopped:false},{confirmMaintenanceStopped:false},{digest:'f'.repeat(64)},{fenceReference:''},{archiveID:'../other'}])assert.throws(()=>resumeWriterRetirement(options(),{...a,...change}));
 assert.deepEqual(tree(directory),before);
});
test('signed operation and audit tampering never authorize recovery',()=>{
 const stopped=interruptWriter('before_completed'),file=path.join(directory,'.task-maintenance.lock','writer-retirement.json');
 const data=JSON.parse(fs.readFileSync(file,'utf8'));data.operation.reason='tampered';fs.writeFileSync(file,JSON.stringify(data));
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'INVALID_WRITER_OPERATION'});
 assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)));
 assert.ok(fs.existsSync(path.join(directory,'z-runtime-writer-archive',stopped.archiveID,'writer','owner.json')));
});
test('conflicting prepared and completed audits are retained',()=>{
 const stopped=interruptWriter('before_unlock'),file=path.join(directory,'z-runtime-writer-archive',stopped.archiveID,'completed.json');
 const data=JSON.parse(fs.readFileSync(file,'utf8'));data.operation.reason='different';fs.writeFileSync(file,JSON.stringify(data));
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'CONFLICTING_WRITER_AUDIT'});
});
test('new writer beside archive and newly pending tasks block recovery',()=>{
 const stopped=interruptWriter('before_completed');fs.mkdirSync(claim);fs.writeFileSync(owner,'another writer');
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'AMBIGUOUS_WRITER_LOCATION'});
 fs.rmSync(claim,{recursive:true});const journal=path.join(directory,'z-task-executions');fs.mkdirSync(journal);fs.writeFileSync(path.join(journal,'unknown'),'pending');
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'EXECUTION_RECONCILIATION_REQUIRED'});
});
test('a replaced maintenance directory cannot borrow the old signature',()=>{
 const stopped=interruptWriter('before_completed'),lock=path.join(directory,'.task-maintenance.lock');fs.renameSync(lock,path.join(directory,'old-maintenance'));fs.mkdirSync(lock);
 fs.copyFileSync(path.join(directory,'old-maintenance','writer-retirement.json'),path.join(lock,'writer-retirement.json'));
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'MAINTENANCE_LOCK_CHANGED'});
});
test('concurrent or orphan recovery guard is not stolen',()=>{
 const stopped=interruptWriter('before_completed'),guard=path.join(directory,'.task-maintenance.lock','writer-resuming');fs.mkdirSync(guard);fs.writeFileSync(path.join(guard,'unknown'),'retained');
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'WRITER_RECOVERY_BUSY'});
 assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)),{code:'WRITER_RECOVERY_BUSY'});
 assert.equal(fs.readFileSync(path.join(guard,'unknown'),'utf8'),'retained');
});
test('failed recovery releases only its own guard and remains retryable',()=>{
 const stopped=interruptWriter('before_completed'),open=fs.openSync;
 fs.openSync=(file,...args)=>{if(path.basename(file)==='completed.json')throw Object.assign(new Error('injected'),{code:'EIO'});return open(file,...args);};
 try{assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)));}finally{fs.openSync=open;}
 assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock','writer-resuming')),false);
 assert.equal(fs.existsSync(path.join(directory,'.task-maintenance.lock')),true);
 assert.equal(resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)).resumed,true);
});
test('real child process exit after rename resumes using signed disk evidence',()=>{
 const p=planWriterRetirement(options()),script=new URL('./reconcile-openclaw-writer.mjs',import.meta.url).href;
 const code=`import fs from 'node:fs';import {applyWriterRetirement} from ${JSON.stringify(script)};const rename=fs.renameSync;fs.renameSync=(...a)=>{rename(...a);process.exit(72);};applyWriterRetirement(JSON.parse(process.env.WRITER_TEST_OPTIONS),JSON.parse(process.env.WRITER_TEST_APPROVAL));`;
 const child=spawnSync(process.execPath,['--input-type=module','-e',code],{env:{...process.env,WRITER_TEST_OPTIONS:JSON.stringify(options()),WRITER_TEST_APPROVAL:JSON.stringify(approval(p.approval_digest))},encoding:'utf8'});
 assert.equal(child.status,72,child.stderr);
 const root=path.join(directory,'z-runtime-writer-archive'),id=fs.readdirSync(root)[0];
 assert.equal(planWriterRecovery(options(),id).stage,'before_completed');
 assert.equal(resumeWriterRetirement(options(),recoveryApproval(id,p.approval_digest)).resumed,true);
});
test('recovery CLI defaults to read-only and requires a separate maintenance confirmation',()=>{
 const stopped=interruptWriter('before_completed'),env={CLAWMANAGER_TASK_RECEIPT_DIR:directory,CLAWMANAGER_LLM_BASE_URL:baseURL,CLAWMANAGER_LLM_API_KEY:apiKey};
 let out='';runWriterRetirementCLI(['--source-generation','7','--resume',stopped.archiveID],env,v=>out=v);assert.equal(JSON.parse(out).read_only,true);
 assert.throws(()=>runWriterRetirementCLI(['--source-generation','7','--resume',stopped.archiveID,'--apply','--approval',stopped.digest,'--confirm-runtime-stopped','--fence-ref','FENCE','--reason','resume'],env,()=>{}));
 assert.ok(fs.existsSync(path.join(directory,'.task-maintenance.lock')));
});

test('partial signed operation remains blocked and unmodified',()=>{
 const stopped=interruptWriter('before_move'),file=path.join(directory,'.task-maintenance.lock','writer-retirement.json');fs.writeFileSync(file,'{"operation":');
 const before=tree(directory);
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'INVALID_WRITER_EVIDENCE'});
 assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)));
 assert.deepEqual(tree(directory),before);
});
test('recovery guard replacement before recheck prevents moving the writer',()=>{
 const stopped=interruptWriter('before_move'),open=fs.openSync;
 fs.openSync=(file,...args)=>{if(path.basename(file).startsWith('recovery-')){const guard=path.join(directory,'.task-maintenance.lock','writer-resuming');fs.writeFileSync(path.join(guard,'owner.json'),'foreign');}return open(file,...args);};
 try{assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)));}finally{fs.openSync=open;}
 assert.ok(fs.existsSync(owner));
 assert.equal(fs.readFileSync(path.join(directory,'.task-maintenance.lock','writer-resuming','owner.json'),'utf8'),'foreign');
});
test('bounded signed recovery history never grows beyond eight attempts',()=>{
 const stopped=interruptWriter('before_completed'),open=fs.openSync;
 for(let i=0;i<8;i++){
  fs.openSync=(file,...args)=>{if(path.basename(file)==='completed.json')throw Object.assign(new Error('injected'),{code:'EIO'});return open(file,...args);};
  try{assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)));}finally{fs.openSync=open;}
 }
 const dest=path.join(directory,'z-runtime-writer-archive',stopped.archiveID),before=tree(directory);
 assert.equal(fs.readdirSync(dest).filter(n=>n.startsWith('recovery-')).length,8);
 assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)),{code:'WRITER_RECOVERY_LIMIT'});
 assert.deepEqual(tree(directory),before);
});
test('recovery audit history must remain authenticated',()=>{
 const stopped=interruptWriter('before_completed'),open=fs.openSync;
 fs.openSync=(file,...args)=>{if(path.basename(file)==='completed.json')throw Object.assign(new Error('injected'),{code:'EIO'});return open(file,...args);};
 try{assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)));}finally{fs.openSync=open;}
 const dest=path.join(directory,'z-runtime-writer-archive',stopped.archiveID),file=path.join(dest,fs.readdirSync(dest).find(n=>n.startsWith('recovery-')));
 const data=JSON.parse(fs.readFileSync(file,'utf8'));data.attempt.reason='edited';fs.writeFileSync(file,JSON.stringify(data));
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'UNVERIFIABLE_WRITER_AUDIT'});
});
test('legacy unbound operation is not silently adopted',()=>{
 const stopped=interruptWriter('before_move'),lock=path.join(directory,'.task-maintenance.lock','writer-retirement.json');
 const value=JSON.parse(fs.readFileSync(lock,'utf8'));value.operation.version=1;delete value.operation.maintenance_identity;value.mac=mac(['writer-retirement-operation-v1',value.operation]);
 fs.writeFileSync(lock,JSON.stringify(value));
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'UNBOUND_WRITER_MAINTENANCE'});
});
test('empty resume cannot fall back to a fresh apply operation',()=>{
 const p=planWriterRetirement(options()),env={CLAWMANAGER_TASK_RECEIPT_DIR:directory,CLAWMANAGER_LLM_BASE_URL:baseURL,CLAWMANAGER_LLM_API_KEY:apiKey},before=tree(directory);
 assert.throws(()=>runWriterRetirementCLI(['--source-generation','7','--resume','','--apply','--approval',p.approval_digest,'--confirm-runtime-stopped','--fence-ref','FENCE','--reason','test'],env,()=>{}),{code:'INVALID_ARGUMENTS'});
 assert.deepEqual(tree(directory),before);
});
test('a killed recovery process leaves a guard that cannot be silently stolen',()=>{
 const stopped=interruptWriter('before_move'),script=new URL('./reconcile-openclaw-writer.mjs',import.meta.url).href;
 const code=`import fs from 'node:fs';import {resumeWriterRetirement} from ${JSON.stringify(script)};const rename=fs.renameSync;fs.renameSync=(...a)=>{rename(...a);process.exit(73);};resumeWriterRetirement(JSON.parse(process.env.WRITER_TEST_OPTIONS),JSON.parse(process.env.WRITER_TEST_APPROVAL));`;
 const child=spawnSync(process.execPath,['--input-type=module','-e',code],{env:{...process.env,WRITER_TEST_OPTIONS:JSON.stringify(options()),WRITER_TEST_APPROVAL:JSON.stringify(recoveryApproval(stopped.archiveID,stopped.digest))},encoding:'utf8'});
 assert.equal(child.status,73,child.stderr);const before=tree(directory);
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'WRITER_RECOVERY_BUSY'});
 assert.throws(()=>resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)),{code:'WRITER_RECOVERY_BUSY'});
 assert.deepEqual(tree(directory),before);
});

test('new recovery owner records signed pid and scope without enabling automatic takeover',()=>{
 const stopped=crashWriterRecovery(),value=JSON.parse(fs.readFileSync(path.join(stopped.guard,'owner.json'),'utf8'));
 assert.equal(value.owner.version,2);assert.ok(Number.isInteger(value.owner.pid));assert.ok(value.owner.pid>0);
 assert.equal(value.mac,mac(['writer-recovery-owner-v1',value.owner]));
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'WRITER_RECOVERY_BUSY'});
 if(process.platform!=='linux')assert.throws(()=>planWriterGuardRetirement(options(),stopped.archiveID),{code:'WRITER_OWNER_PROBE_UNSUPPORTED'});
});
test('corrupt recovery owner cannot be retired even with operator confirmation',()=>{
 const stopped=crashWriterRecovery(),file=path.join(stopped.guard,'owner.json'),value=JSON.parse(fs.readFileSync(file,'utf8'));
 value.owner.pid=process.pid;fs.writeFileSync(file,JSON.stringify(value));const before=tree(directory);
 assert.throws(()=>planWriterGuardRetirement(options(),stopped.archiveID),{code:'UNVERIFIABLE_WRITER_RECOVERY_OWNER'});
 assert.throws(()=>retireWriterRecoveryGuard(options(),recoveryApproval(stopped.archiveID,stopped.digest)));
 assert.deepEqual(tree(directory),before);
});
test('retirement CLI requires resume identity and a separate explicit execution mode',()=>{
 const env={CLAWMANAGER_TASK_RECEIPT_DIR:directory,CLAWMANAGER_LLM_BASE_URL:baseURL,CLAWMANAGER_LLM_API_KEY:apiKey},before=tree(directory);
 for(const args of [['--source-generation','7','--retire-recovery-guard'],['--source-generation','7','--owner-token',randomUUID()],['--source-generation','7','--resume',randomUUID(),'--retire-recovery-guard','--owner-token',''],['--source-generation','7','--force']])assert.throws(()=>runWriterRetirementCLI(args,env,()=>{}),{code:'INVALID_ARGUMENTS'});
 assert.deepEqual(tree(directory),before);
});
test('Linux dead owner can be archived, idempotently confirmed, then normal recovery resumed',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),before=tree(directory),plan=planWriterGuardRetirement(options(),stopped.archiveID);
 assert.equal(plan.owner_state,'absent_on_same_linux_boot_and_pid_namespace');assert.deepEqual(tree(directory),before);
 const a={...recoveryApproval(stopped.archiveID,plan.approval_digest),ownerToken:plan.owner_token};
 const result=retireWriterRecoveryGuard(options(),a);assert.equal(result.action,'recovery_guard_retired');assert.equal(result.maintenance_lock_retained,true);
 const retired=path.join(directory,'z-runtime-writer-archive',stopped.archiveID,'retired-guards',plan.owner_token);
 assert.ok(fs.existsSync(path.join(retired,'owner.json')));assert.ok(fs.existsSync(path.join(retired,'retirement.json')));
 assert.equal(retireWriterRecoveryGuard(options(),a).action,'recovery_guard_already_retired');
 assert.equal(resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)).resumed,true);
 assert.ok(fs.existsSync(retired));assert.ok(fs.existsSync(receipt));
});
test('Linux live or reused pid is never retired',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery();changeRecoveryOwner(stopped.guard,o=>o.pid=process.pid);const before=tree(directory);
 assert.throws(()=>planWriterGuardRetirement(options(),stopped.archiveID),{code:'WRITER_OWNER_STILL_RUNNING'});assert.deepEqual(tree(directory),before);
});
test('Linux other boot or pid namespace is not comparable',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery();changeRecoveryOwner(stopped.guard,o=>o.scope='0'.repeat(64));
 assert.throws(()=>planWriterGuardRetirement(options(),stopped.archiveID),{code:'WRITER_OWNER_SCOPE_MISMATCH'});
});
test('Linux permission failures cannot become an absent-owner proof',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),kill=process.kill;process.kill=()=>{throw Object.assign(new Error('denied'),{code:'EPERM'});};
 try{assert.throws(()=>planWriterGuardRetirement(options(),stopped.archiveID),{code:'WRITER_OWNER_STATE_UNKNOWN'});}finally{process.kill=kill;}
});
test('Linux retirement interrupted after move can complete audit with original binding',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),plan=planWriterGuardRetirement(options(),stopped.archiveID),open=fs.openSync;
 const a={...recoveryApproval(stopped.archiveID,plan.approval_digest),ownerToken:plan.owner_token};
 fs.openSync=(file,...args)=>{if(path.basename(file)==='retirement.json')throw Object.assign(new Error('injected'),{code:'EIO'});return open(file,...args);};
 try{assert.throws(()=>retireWriterRecoveryGuard(options(),a));}finally{fs.openSync=open;}
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'WRITER_GUARD_RETIREMENT_INCOMPLETE'});
 const retry=planWriterGuardRetirement(options(),stopped.archiveID,plan.owner_token);assert.equal(retry.approval_digest,plan.approval_digest);
 assert.equal(retireWriterRecoveryGuard(options(),a).action,'recovery_guard_retired');
 assert.equal(resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)).resumed,true);
});
test('Linux changed guard-retirement approval preserves all evidence',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),plan=planWriterGuardRetirement(options(),stopped.archiveID),before=tree(directory);
 const a={...recoveryApproval(stopped.archiveID,'f'.repeat(64)),ownerToken:plan.owner_token};
 assert.throws(()=>retireWriterRecoveryGuard(options(),a),{code:'APPROVED_WRITER_PLAN_CHANGED'});assert.deepEqual(tree(directory),before);
});
test('Linux guard retirement audit cannot be edited during later recovery',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),plan=planWriterGuardRetirement(options(),stopped.archiveID);
 retireWriterRecoveryGuard(options(),{...recoveryApproval(stopped.archiveID,plan.approval_digest),ownerToken:plan.owner_token});
 const file=path.join(directory,'z-runtime-writer-archive',stopped.archiveID,'retired-guards',plan.owner_token,'retirement.json');
 const value=JSON.parse(fs.readFileSync(file,'utf8'));value.record.reason='edited';fs.writeFileSync(file,JSON.stringify(value));
 assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'UNVERIFIABLE_WRITER_GUARD_HISTORY'});
});
test('legacy recovery owners without pid and scope cannot be automatically upgraded',()=>{
 const stopped=crashWriterRecovery();changeRecoveryOwner(stopped.guard,o=>{o.version=1;delete o.pid;delete o.scope;});
 assert.throws(()=>planWriterGuardRetirement(options(),stopped.archiveID),{code:'UNVERIFIABLE_WRITER_RECOVERY_OWNER'});
 assert.ok(fs.existsSync(path.join(stopped.guard,'owner.json')));
});
test('Linux owner reappearing during final checks prevents retirement',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),plan=planWriterGuardRetirement(options(),stopped.archiveID),kill=process.kill;let checks=0;
 process.kill=()=>{if(++checks===1)throw Object.assign(new Error('absent'),{code:'ESRCH'});};
 try{assert.throws(()=>retireWriterRecoveryGuard(options(),{...recoveryApproval(stopped.archiveID,plan.approval_digest),ownerToken:plan.owner_token}),{code:'WRITER_OWNER_STILL_RUNNING'});}finally{process.kill=kill;}
 assert.ok(fs.existsSync(path.join(stopped.guard,'owner.json')));
});
test('Linux real retirement process exit after guard move can finish audit',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),plan=planWriterGuardRetirement(options(),stopped.archiveID),script=new URL('./reconcile-openclaw-writer.mjs',import.meta.url).href;
 const a={...recoveryApproval(stopped.archiveID,plan.approval_digest),ownerToken:plan.owner_token};
 const code=`import fs from 'node:fs';import {retireWriterRecoveryGuard} from ${JSON.stringify(script)};const rename=fs.renameSync;fs.renameSync=(...a)=>{rename(...a);process.exit(75);};retireWriterRecoveryGuard(JSON.parse(process.env.WRITER_TEST_OPTIONS),JSON.parse(process.env.WRITER_TEST_APPROVAL));`;
 const child=spawnSync(process.execPath,['--input-type=module','-e',code],{env:{...process.env,WRITER_TEST_OPTIONS:JSON.stringify(options()),WRITER_TEST_APPROVAL:JSON.stringify(a)},encoding:'utf8'});
 assert.equal(child.status,75,child.stderr);assert.throws(()=>planWriterRecovery(options(),stopped.archiveID),{code:'WRITER_GUARD_RETIREMENT_INCOMPLETE'});
 assert.equal(retireWriterRecoveryGuard(options(),a).action,'recovery_guard_retired');
 assert.equal(resumeWriterRetirement(options(),recoveryApproval(stopped.archiveID,stopped.digest)).resumed,true);
});
test('Linux competing retirement processes never replace the first archived owner',{skip:process.platform!=='linux'},async()=>{
 const stopped=crashWriterRecovery(),plan=planWriterGuardRetirement(options(),stopped.archiveID),script=new URL('./reconcile-openclaw-writer.mjs',import.meta.url).href;
 const a={...recoveryApproval(stopped.archiveID,plan.approval_digest),ownerToken:plan.owner_token};
 const code=`import {retireWriterRecoveryGuard} from ${JSON.stringify(script)};try{const result=retireWriterRecoveryGuard(JSON.parse(process.env.WRITER_TEST_OPTIONS),JSON.parse(process.env.WRITER_TEST_APPROVAL));console.log(JSON.stringify({ok:true,result}));}catch(e){console.log(JSON.stringify({ok:false,error:e.code}));}`;
 const run=()=>new Promise((resolve,reject)=>{const child=spawn(process.execPath,['--input-type=module','-e',code],{env:{...process.env,WRITER_TEST_OPTIONS:JSON.stringify(options()),WRITER_TEST_APPROVAL:JSON.stringify(a)}});let out='';child.stdout.on('data',b=>out+=b);child.on('error',reject);child.on('close',status=>{try{assert.equal(status,0);resolve(JSON.parse(out));}catch(e){reject(e);}});});
 const results=await Promise.all([run(),run()]);assert.ok(results.some(r=>r.ok));
 const retiredRoot=path.join(directory,'z-runtime-writer-archive',stopped.archiveID,'retired-guards');assert.deepEqual(fs.readdirSync(retiredRoot),[plan.owner_token]);
 assert.equal(planWriterRecovery(options(),stopped.archiveID).read_only,true);
});
test('Linux retired owner history is bounded before append',{skip:process.platform!=='linux'},()=>{
 const stopped=crashWriterRecovery(),plan=planWriterGuardRetirement(options(),stopped.archiveID),old=JSON.parse(fs.readFileSync(path.join(stopped.guard,'owner.json'),'utf8'));
 const root=path.join(directory,'z-runtime-writer-archive',stopped.archiveID,'retired-guards');fs.mkdirSync(root);
 for(let i=0;i<32;i++){
  const token=randomUUID(),dir=path.join(root,token);fs.mkdirSync(dir);const stat=fs.lstatSync(dir,{bigint:true});
  const owner={...old.owner,token,guard_identity:{device:String(stat.dev),inode:String(stat.ino),birthtime:String(stat.birthtimeNs)}};
  const ownerMAC=mac(['writer-recovery-owner-v1',owner]);fs.writeFileSync(path.join(dir,'owner.json'),JSON.stringify({owner,mac:ownerMAC}));
  const record={version:1,owner_mac:ownerMAC,operation_mac:old.owner.operation_mac,reason:'history',fence_reference:'HISTORY-FENCE',proof:'absent_on_same_linux_boot_and_pid_namespace'};
  fs.writeFileSync(path.join(dir,'retirement.json'),JSON.stringify({record,mac:mac(['writer-guard-retirement-audit-v1',record])}));
 }
 const before=tree(directory);
 assert.throws(()=>retireWriterRecoveryGuard(options(),{...recoveryApproval(stopped.archiveID,plan.approval_digest),ownerToken:plan.owner_token}),{code:'WRITER_GUARD_HISTORY_LIMIT'});
 assert.equal(fs.readdirSync(root).length,32);assert.deepEqual(tree(directory),before);
});
