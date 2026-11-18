// Operator-controlled retirement of a CLEANLY STOPPED writer only. Never infer
// that a crashed process or its tools stopped from a timestamp or absent PID.
import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {createHash,createHmac,timingSafeEqual,randomUUID} from 'node:crypto';
import {checkDirectory,sameFile,canonicalBase,inspectExecutionBarriers,ReceiptInspectionError} from './inspect-openclaw-receipts.mjs';
const fail=code=>{throw new ReceiptInspectionError(code);};
const hash=value=>createHash('sha256').update(value).digest('hex');
const mac=(key,value)=>createHmac('sha256',key).update(JSON.stringify(value)).digest('hex');
const metadata=s=>({device:String(s.dev),inode:String(s.ino),size:String(s.size),mtime:String(s.mtimeNs)});
const sameDirectoryIdentity=(a,b)=>b.isDirectory()&&!b.isSymbolicLink()&&a.dev===b.dev&&a.ino===b.ino&&a.birthtimeNs===b.birthtimeNs;
const lockIdentity=s=>({device:String(s.dev),inode:String(s.ino),birthtime:String(s.birthtimeNs)});
const equal=(a,b)=>JSON.stringify(a)===JSON.stringify(b);
const validUUID=value=>typeof value==='string'&&/^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/.test(value);
function localWriterRecoveryScope(){
 if(process.platform!=='linux')return null;
 try{const machine=fs.readFileSync('/etc/machine-id','utf8').trim(),boot=fs.readFileSync('/proc/sys/kernel/random/boot_id','utf8').trim(),ns=fs.readlinkSync('/proc/self/ns/pid');
  if(!/^[a-f0-9]{32}$/.test(machine)||!validUUID(boot)||!/^pid:\[\d+\]$/.test(ns))return null;
  return hash(JSON.stringify([machine,boot,ns]));
 }catch{return null;}
}
function requireDeadWriterRecovery(owner){
 const scope=localWriterRecoveryScope();
 if(!scope||!owner.scope)fail('WRITER_OWNER_PROBE_UNSUPPORTED');
 if(scope!==owner.scope)fail('WRITER_OWNER_SCOPE_MISMATCH');
 try{process.kill(owner.pid,0);}catch(e){if(e.code==='ESRCH')return;fail('WRITER_OWNER_STATE_UNKNOWN');}
 fail('WRITER_OWNER_STILL_RUNNING');
}
function verifyMAC(signature,key,value){return typeof signature==='string'&&/^[a-f0-9]{64}$/.test(signature)&&timingSafeEqual(Buffer.from(signature,'hex'),Buffer.from(mac(key,value),'hex'));}
const exists=file=>{try{fs.lstatSync(file);return true;}catch(e){if(e.code==='ENOENT')return false;throw e;}};
const safe=e=>e instanceof ReceiptInspectionError?e:new ReceiptInspectionError('WRITER_RECONCILIATION_FAILED');
function syncDirectory(dir){if(process.platform==='win32')return;const fd=fs.openSync(dir,fs.constants.O_RDONLY|(fs.constants.O_DIRECTORY??0)|(fs.constants.O_NOFOLLOW??0));try{fs.fsyncSync(fd);}finally{fs.closeSync(fd);}}
function read(file,limit=2048){
 const before=fs.lstatSync(file,{bigint:true});
 if(!before.isFile()||before.isSymbolicLink()||before.size>BigInt(limit))fail('UNSAFE_WRITER_EVIDENCE');
 const fd=fs.openSync(file,fs.constants.O_RDONLY|(fs.constants.O_NOFOLLOW??0));let bytes;
 try{const opened=fs.fstatSync(fd,{bigint:true});if(!sameFile(before,opened,true)||!opened.isFile())fail('WRITER_EVIDENCE_CHANGED');const b=Buffer.alloc(limit+1),n=fs.readSync(fd,b,0,b.length,0);if(n>limit)fail('UNSAFE_WRITER_EVIDENCE');bytes=b.subarray(0,n);if(!sameFile(opened,fs.fstatSync(fd,{bigint:true})))fail('WRITER_EVIDENCE_CHANGED');}finally{fs.closeSync(fd);}
 if(!sameFile(before,fs.lstatSync(file,{bigint:true})))fail('WRITER_EVIDENCE_CHANGED');
 let value;try{value=JSON.parse(bytes.toString('utf8'));}catch{fail('INVALID_WRITER_EVIDENCE');}
 return {value,hash:hash(bytes),stat:metadata(before)};
}
function configuration(options){
 const {directory,apiKey,generation}=options;
 if(typeof directory!=='string'||!path.isAbsolute(directory)||path.resolve(directory)===path.parse(directory).root||typeof apiKey!=='string'||!apiKey.trim()||/[\r\n\0]/.test(apiKey)||!Number.isSafeInteger(generation)||generation<0||generation>2147483647)fail('INVALID_CONFIGURATION');
 return {base:canonicalBase(options.baseURL),root:checkDirectory(directory)};
}
function prepare(options,ownedMaintenance=false,sourceClaim){
 const {directory,apiKey,generation}=options,{base,root}=configuration(options);
 if(!ownedMaintenance&&exists(path.join(directory,'.task-maintenance.lock')))fail('MAINTENANCE_ALREADY_LOCKED');
 const claimRoot=sourceClaim??path.join(directory,'.task-writer.lock'),claimStat=checkDirectory(claimRoot);
 if(fs.readdirSync(claimRoot).sort().join(',')!=='owner.json')fail('UNVERIFIABLE_WRITER_CLAIM');
 const owner=read(path.join(claimRoot,'owner.json')),value=owner.value;
 if(!value||Object.keys(value).sort().join(',')!=='generation,owner,version'||value.version!==1||value.generation!==generation||typeof value.owner!=='string'||!/^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/.test(value.owner))fail('UNVERIFIABLE_WRITER_CLAIM');
 const namespace=hash(JSON.stringify([base,apiKey,generation]));
 const receiptRoot=path.join(directory,'z-runtime-shutdowns');checkDirectory(receiptRoot);
 const evidence=read(path.join(receiptRoot,namespace+'-'+owner.hash+'.json'));
 const record=evidence.value?.record,signature=evidence.value?.mac;
 if(!evidence.value||Object.keys(evidence.value).sort().join(',')!=='mac,record'||!record||Object.keys(record).sort().join(',')!=='claim_id,generation,namespace,native_cleanup_confirmed,version'||record.version!==1||record.namespace!==namespace||record.generation!==generation||record.claim_id!==owner.hash||record.native_cleanup_confirmed!==true||typeof signature!=='string'||!/^[a-f0-9]{64}$/.test(signature)||!timingSafeEqual(Buffer.from(signature,'hex'),Buffer.from(mac(apiKey,['native-cleanup-v1',namespace,generation,owner.hash,true]),'hex')))fail('NATIVE_CLEANUP_NOT_CONFIRMED');
 const barriers=inspectExecutionBarriers(directory);
 if(barriers.pending!==0)fail('EXECUTION_RECONCILIATION_REQUIRED');
 const journal=path.join(directory,'z-task-executions'),journalStat=exists(journal)?metadata(checkDirectory(journal)):null;
 if(!sameFile(claimStat,checkDirectory(claimRoot))||owner.hash!==read(path.join(claimRoot,'owner.json')).hash)fail('WRITER_EVIDENCE_CHANGED');
 const snapshot={version:1,directory:path.resolve(directory),root:{device:String(root.dev),inode:String(root.ino)},generation,namespace,claim:metadata(claimStat),owner,receipt:evidence,journal:journalStat};
 return {snapshot,digest:mac(apiKey,['writer-retirement-v1',snapshot]),claimRoot};
}
export function planWriterRetirement(options={}){
 try{const p=prepare(options);return Object.freeze({read_only:true,action:'review',approval_digest:p.digest,generation:options.generation,native_cleanup_confirmed:true,release_confirmed:false,requires_runtime_fence:true});}catch(e){throw safe(e);}
}
function writeExclusive(file,value){const fd=fs.openSync(file,'wx',0o600);try{fs.writeFileSync(fd,JSON.stringify(value));fs.fsyncSync(fd);}finally{fs.closeSync(fd);}}
export function applyWriterRetirement(options={},approval={}){
 if(approval.confirmRuntimeStopped!==true||!(/^[a-f0-9]{64}$/.test(approval.digest??''))||![approval.fenceReference,approval.reason].every(v=>typeof v==='string'&&v.trim()===v&&v.length>0&&v.length<=200&&!/[\r\n\0]/.test(v)))fail('EXPLICIT_WRITER_FENCE_APPROVAL_REQUIRED');
 let maintenance,maintenanceStat,archiveID;
 try{
  const first=prepare(options);if(first.digest!==approval.digest)fail('APPROVED_WRITER_PLAN_CHANGED');
  maintenance=path.join(options.directory,'.task-maintenance.lock');
  try{fs.mkdirSync(maintenance,{mode:0o700});}catch(e){maintenance=undefined;if(e.code==='EEXIST')fail('MAINTENANCE_ALREADY_LOCKED');throw e;}
  maintenanceStat=fs.lstatSync(maintenance,{bigint:true});archiveID=randomUUID();
  const binding={version:2,archive_id:archiveID,approval_digest:approval.digest,fence_reference:approval.fenceReference,reason:approval.reason,generation:options.generation,snapshot:first.snapshot,maintenance_identity:lockIdentity(maintenanceStat)};
  const operation={operation:binding,mac:mac(options.apiKey,['writer-retirement-operation-v1',binding])};
  writeExclusive(path.join(maintenance,'writer-retirement.json'),operation);syncDirectory(maintenance);syncDirectory(options.directory);
  const current=prepare(options,true);if(current.digest!==approval.digest)fail('APPROVED_WRITER_PLAN_CHANGED');
  const archiveRoot=path.join(options.directory,'z-runtime-writer-archive');try{fs.mkdirSync(archiveRoot,{mode:0o700});}catch(e){if(e.code!=='EEXIST')throw e;}checkDirectory(archiveRoot);syncDirectory(options.directory);
  const destination=path.join(archiveRoot,archiveID);fs.mkdirSync(destination,{mode:0o700});syncDirectory(archiveRoot);
  writeExclusive(path.join(destination,'prepared.json'),operation);syncDirectory(destination);
  if(prepare(options,true).digest!==approval.digest)fail('APPROVED_WRITER_PLAN_CHANGED');
  fs.renameSync(current.claimRoot,path.join(destination,'writer'));syncDirectory(destination);syncDirectory(options.directory);
  const archived=read(path.join(destination,'writer','owner.json'));
  if(archived.hash!==current.snapshot.owner.hash||!sameDirectoryIdentity(maintenanceStat,fs.lstatSync(maintenance,{bigint:true})))fail('WRITER_EVIDENCE_CHANGED');
  writeExclusive(path.join(destination,'completed.json'),operation);syncDirectory(destination);
  if(fs.readdirSync(maintenance).sort().join(',')!=='writer-retirement.json'||!sameDirectoryIdentity(maintenanceStat,fs.lstatSync(maintenance,{bigint:true})))fail('MAINTENANCE_LOCK_CHANGED');
  if(JSON.stringify(read(path.join(maintenance,'writer-retirement.json'),8192).value)!==JSON.stringify(operation))fail('MAINTENANCE_LOCK_CHANGED');
  fs.unlinkSync(path.join(maintenance,'writer-retirement.json'));fs.rmdirSync(maintenance);syncDirectory(options.directory);maintenance=undefined;
  return Object.freeze({archived:true,archive_id:archiveID,action:'restart_required',receipt_replay_allowed:false});
 }catch(e){const result=safe(e);result.maintenanceLockRetained=Boolean(maintenance&&exists(maintenance));if(archiveID)result.archiveID=archiveID;throw result;}
}
function recoveryContext(options,archiveID,ownedGuard,retiringToken){
 configuration(options);if(!validUUID(archiveID))fail('INVALID_ARCHIVE_ID');
 const directory=options.directory,maintenance=path.join(directory,'.task-maintenance.lock'),locked=exists(maintenance);
 const archiveRoot=path.join(directory,'z-runtime-writer-archive'),destination=path.join(archiveRoot,archiveID);
 if(exists(archiveRoot))checkDirectory(archiveRoot);if(exists(destination))checkDirectory(destination);
 const optional=file=>exists(file)?read(file,8192).value:undefined;
 const prepared=optional(path.join(destination,'prepared.json')),completed=optional(path.join(destination,'completed.json'));
 let maintenanceStat,operation;
 if(locked){
  maintenanceStat=checkDirectory(maintenance);
  const entries=fs.readdirSync(maintenance);
  if(entries.some(n=>!['writer-retirement.json','writer-resuming'].includes(n)))fail('UNVERIFIABLE_WRITER_MAINTENANCE');
  if(entries.includes('writer-resuming')){
   if(!ownedGuard||!sameDirectoryIdentity(ownedGuard.stat,checkDirectory(path.join(maintenance,'writer-resuming'))))fail('WRITER_RECOVERY_BUSY');
   const guardRoot=path.join(maintenance,'writer-resuming');
   if(fs.readdirSync(guardRoot).sort().join(',')!=='owner.json'||!equal(read(path.join(guardRoot,'owner.json'),8192).value,ownedGuard.owner))fail('WRITER_RECOVERY_GUARD_CHANGED');
  }
  operation=optional(path.join(maintenance,'writer-retirement.json'));
 }
 operation??=prepared??completed;
 const binding=operation?.operation;
 if(!binding||binding.version!==2||!binding.maintenance_identity)fail('UNBOUND_WRITER_MAINTENANCE');
 if(Object.keys(operation).sort().join(',')!=='mac,operation'||binding.archive_id!==archiveID||binding.generation!==options.generation||
 !verifyMAC(operation.mac,options.apiKey,['writer-retirement-operation-v1',binding])||
 !verifyMAC(binding.approval_digest,options.apiKey,['writer-retirement-v1',binding.snapshot]))fail('INVALID_WRITER_OPERATION');
 if(locked&&!equal(binding.maintenance_identity,lockIdentity(maintenanceStat)))fail('MAINTENANCE_LOCK_CHANGED');
 for(const record of [prepared,completed])if(record&&!equal(record,operation))fail('CONFLICTING_WRITER_AUDIT');
 const source=path.join(directory,'.task-writer.lock'),archived=path.join(destination,'writer'),hasSource=exists(source),hasArchive=exists(archived);
 if(hasSource===hasArchive)fail('AMBIGUOUS_WRITER_LOCATION');
 if(completed&&!hasArchive)fail('CONFLICTING_WRITER_AUDIT');
 if(!locked&&(!prepared||!completed||!hasArchive))fail('WRITER_MAINTENANCE_PROOF_REQUIRED');
 let attempts=0;
 if(exists(destination))for(const name of fs.readdirSync(destination)){
  if(['writer','prepared.json','completed.json'].includes(name))continue;
  if(name==='retired-guards'){
   const root=path.join(destination,name);checkDirectory(root);const names=fs.readdirSync(root);if(names.length>32)fail('WRITER_GUARD_HISTORY_LIMIT');
   for(const token of names){if(!validUUID(token))fail('UNVERIFIABLE_WRITER_GUARD_HISTORY');readWriterRecoveryOwner(path.join(root,token),operation,options,true,retiringToken===token);}
   continue;
  }
  const match=/^recovery-([a-f0-9-]+)\.json$/.exec(name);
  if(!match||!validUUID(match[1])||++attempts>8)fail('UNVERIFIABLE_WRITER_AUDIT');
  const audit=read(path.join(destination,name),8192).value;
  if(!audit?.attempt||Object.keys(audit).sort().join(',')!=='attempt,mac'||Object.keys(audit.attempt).sort().join(',')!=='archive_id,fence_reference,id,operation_mac,operator_confirmed_maintenance_stopped,reason,version'||audit.attempt.version!==1||audit.attempt.operator_confirmed_maintenance_stopped!==true||![audit.attempt.fence_reference,audit.attempt.reason].every(v=>typeof v==='string'&&v.trim()===v&&v.length>0&&v.length<=200&&!/[\r\n\0]/.test(v))||audit.attempt.id!==match[1]||audit.attempt.archive_id!==archiveID||audit.attempt.operation_mac!==operation.mac||
    !verifyMAC(audit.mac,options.apiKey,['writer-recovery-attempt-v1',audit.attempt]))fail('UNVERIFIABLE_WRITER_AUDIT');
 }
 const fresh=prepare(options,locked,hasSource?source:archived);
 if(fresh.digest!==binding.approval_digest||!equal(fresh.snapshot,binding.snapshot))fail('APPROVED_WRITER_PLAN_CHANGED');
 return {operation,binding,maintenance,maintenanceStat,locked,archiveRoot,destination,source,archived,hasArchive,prepared,completed,attempts,fresh,
  stage:!hasArchive?prepared?'before_move':'before_prepared':completed?locked?'before_unlock':'completed':'before_completed'};
}
export function planWriterRecovery(options={},archiveID){
 try{const ctx=recoveryContext(options,archiveID);return Object.freeze({read_only:true,action:'review_recovery',archive_id:archiveID,stage:ctx.stage,approval_digest:ctx.binding.approval_digest,requires_runtime_fence:true,requires_stopped_maintenance:true,receipt_replay_allowed:false});}catch(e){throw safe(e);}
}
function validRecoveryApproval(approval){return approval.confirmRuntimeStopped===true&&approval.confirmMaintenanceStopped===true&&
 /^[a-f0-9]{64}$/.test(approval.digest??'')&&[approval.fenceReference,approval.reason].every(v=>typeof v==='string'&&v.trim()===v&&v.length>0&&v.length<=200&&!/[\r\n\0]/.test(v));}
export function resumeWriterRetirement(options={},approval={}){
 if(!validRecoveryApproval(approval))fail('EXPLICIT_WRITER_RECOVERY_APPROVAL_REQUIRED');
 let guard,guardStat,guardOwner,ctx;
 const releaseGuard=()=>{
  if(!guard)return;
  if(!sameDirectoryIdentity(guardStat,checkDirectory(guard))||fs.readdirSync(guard).sort().join(',')!=='owner.json'||!equal(read(path.join(guard,'owner.json'),8192).value,guardOwner))fail('WRITER_RECOVERY_GUARD_CHANGED');
  fs.unlinkSync(path.join(guard,'owner.json'));fs.rmdirSync(guard);guard=undefined;syncDirectory(ctx.maintenance);
 };
 try{
  ctx=recoveryContext(options,approval.archiveID);
  if(ctx.binding.approval_digest!==approval.digest)fail('APPROVED_WRITER_PLAN_CHANGED');
  if(!ctx.locked)return Object.freeze({archived:true,action:'already_archived',archive_id:approval.archiveID,restart_required:true,receipt_replay_allowed:false});
  if(ctx.attempts>=8)fail('WRITER_RECOVERY_LIMIT');
  const candidate=path.join(ctx.maintenance,'writer-resuming');
  try{fs.mkdirSync(candidate,{mode:0o700});}catch(e){if(e.code==='EEXIST')fail('WRITER_RECOVERY_BUSY');throw e;}
  guard=candidate;guardStat=checkDirectory(guard);
  const owner={version:2,token:randomUUID(),pid:process.pid,scope:localWriterRecoveryScope(),operation_mac:ctx.operation.mac,guard_identity:lockIdentity(guardStat)};
  guardOwner={owner,mac:mac(options.apiKey,['writer-recovery-owner-v1',owner])};
  writeExclusive(path.join(guard,'owner.json'),guardOwner);syncDirectory(guard);syncDirectory(ctx.maintenance);
  ctx=recoveryContext(options,approval.archiveID,{stat:guardStat,owner:guardOwner});
  if(ctx.binding.approval_digest!==approval.digest)fail('APPROVED_WRITER_PLAN_CHANGED');
  for(const dir of [ctx.archiveRoot,ctx.destination]){try{fs.mkdirSync(dir,{mode:0o700});}catch(e){if(e.code!=='EEXIST')throw e;}checkDirectory(dir);syncDirectory(path.dirname(dir));}
  const attempt={version:1,id:randomUUID(),archive_id:approval.archiveID,operation_mac:ctx.operation.mac,fence_reference:approval.fenceReference,reason:approval.reason,operator_confirmed_maintenance_stopped:true};
  writeExclusive(path.join(ctx.destination,'recovery-'+attempt.id+'.json'),{attempt,mac:mac(options.apiKey,['writer-recovery-attempt-v1',attempt])});syncDirectory(ctx.destination);
  if(!ctx.prepared){writeExclusive(path.join(ctx.destination,'prepared.json'),ctx.operation);syncDirectory(ctx.destination);}
  ctx=recoveryContext(options,approval.archiveID,{stat:guardStat,owner:guardOwner});
  if(!ctx.hasArchive){fs.renameSync(ctx.source,ctx.archived);syncDirectory(ctx.destination);syncDirectory(options.directory);}
  ctx=recoveryContext(options,approval.archiveID,{stat:guardStat,owner:guardOwner});
  if(!ctx.completed){writeExclusive(path.join(ctx.destination,'completed.json'),ctx.operation);syncDirectory(ctx.destination);}
  ctx=recoveryContext(options,approval.archiveID,{stat:guardStat,owner:guardOwner});
  releaseGuard();
  // Validate again after dropping only our guard, while the outer barrier is
  // still present. Never unlink a replaced/foreign maintenance record.
  ctx=recoveryContext(options,approval.archiveID);
  const marker=path.join(ctx.maintenance,'writer-retirement.json');
  if(exists(marker)){if(!equal(read(marker,8192).value,ctx.operation))fail('MAINTENANCE_LOCK_CHANGED');fs.unlinkSync(marker);}
  if(fs.readdirSync(ctx.maintenance).length!==0)fail('MAINTENANCE_LOCK_CHANGED');
  fs.rmdirSync(ctx.maintenance);syncDirectory(options.directory);
  return Object.freeze({archived:true,resumed:true,action:'restart_required',archive_id:approval.archiveID,receipt_replay_allowed:false});
 }catch(e){try{releaseGuard();}catch{}const result=safe(e);result.archiveID=approval.archiveID;try{result.maintenanceLockRetained=exists(path.join(options.directory,'.task-maintenance.lock'));}catch{result.maintenanceLockRetained=true;}throw result;}
}
function readWriterRecoveryOwner(guard,operation,options,retired=false,allowMissingAudit=false){
 const stat=checkDirectory(guard),names=fs.readdirSync(guard).sort();
 if(!equal(names,['owner.json'])&&!(retired&&equal(names,['owner.json','retirement.json'])))fail('UNVERIFIABLE_WRITER_RECOVERY_OWNER');
 const envelope=read(path.join(guard,'owner.json'),8192).value,owner=envelope?.owner;
 if(!owner||Object.keys(envelope).sort().join(',')!=='mac,owner'||Object.keys(owner).sort().join(',')!=='guard_identity,operation_mac,pid,scope,token,version'||owner.version!==2||!validUUID(owner.token)||!Number.isSafeInteger(owner.pid)||owner.pid<1||owner.pid>2147483647||owner.scope!==null&&!/^[a-f0-9]{64}$/.test(owner.scope??'')||owner.operation_mac!==operation.mac||!equal(owner.guard_identity,lockIdentity(stat))||!verifyMAC(envelope.mac,options.apiKey,['writer-recovery-owner-v1',owner]))fail('UNVERIFIABLE_WRITER_RECOVERY_OWNER');
 if(retired&&path.basename(guard)!==owner.token)fail('UNVERIFIABLE_WRITER_RECOVERY_OWNER');
 let audited=false;
 if(retired&&names.includes('retirement.json')){
  const audit=read(path.join(guard,'retirement.json'),8192).value,record=audit?.record;
  if(!record||Object.keys(audit).sort().join(',')!=='mac,record'||Object.keys(record).sort().join(',')!=='fence_reference,operation_mac,owner_mac,proof,reason,version'||record.version!==1||record.owner_mac!==envelope.mac||record.operation_mac!==operation.mac||record.proof!=='absent_on_same_linux_boot_and_pid_namespace'||![record.reason,record.fence_reference].every(v=>typeof v==='string'&&v.trim()===v&&v.length>0&&v.length<=200&&!/[\r\n\0]/.test(v))||!verifyMAC(audit.mac,options.apiKey,['writer-guard-retirement-audit-v1',record]))fail('UNVERIFIABLE_WRITER_GUARD_HISTORY');
  audited=true;
 }
 if(retired&&!audited&&!allowMissingAudit)fail('WRITER_GUARD_RETIREMENT_INCOMPLETE');
 return {stat,envelope,audited};
}
function staleWriterRecoveryContext(options,archiveID,ownerToken){
 configuration(options);if(!validUUID(archiveID)||ownerToken!==undefined&&!validUUID(ownerToken))fail('INVALID_ARCHIVE_ID');
 const maintenance=path.join(options.directory,'.task-maintenance.lock'),active=path.join(maintenance,'writer-resuming');checkDirectory(maintenance);
 const root=path.join(options.directory,'z-runtime-writer-archive',archiveID,'retired-guards');
 let guard,isRetired=false;
 if(exists(active)){
  checkDirectory(active);const candidate=read(path.join(active,'owner.json'),8192).value;
  if(ownerToken!==undefined&&candidate?.owner?.token!==ownerToken)fail('WRITER_RECOVERY_OWNER_CHANGED');
  guard=active;
 }else{
  checkDirectory(root);const names=fs.readdirSync(root);if(names.length>32)fail('WRITER_GUARD_HISTORY_LIMIT');
  if(ownerToken===undefined){const unfinished=names.filter(n=>validUUID(n)&&!exists(path.join(root,n,'retirement.json')));if(unfinished.length!==1)fail('WRITER_RECOVERY_OWNER_REQUIRED');ownerToken=unfinished[0];}
  guard=path.join(root,ownerToken);checkDirectory(guard);isRetired=true;
 }
 const stat=checkDirectory(guard),envelope=read(path.join(guard,'owner.json'),8192).value;
 const ctx=recoveryContext(options,archiveID,isRetired?undefined:{stat,owner:envelope},isRetired?ownerToken:undefined);
 const loaded=readWriterRecoveryOwner(guard,ctx.operation,options,isRetired,isRetired);
 if(!loaded.audited)requireDeadWriterRecovery(loaded.envelope.owner);
 const digest=mac(options.apiKey,['writer-guard-retirement-v1',archiveID,ctx.binding.approval_digest,loaded.envelope,ctx.binding.maintenance_identity]);
 return {ctx,guard,root,isRetired,...loaded,digest};
}
export function planWriterGuardRetirement(options={},archiveID,ownerToken){
 try{const result=staleWriterRecoveryContext(options,archiveID,ownerToken);return Object.freeze({read_only:true,action:'review_recovery_guard_retirement',archive_id:archiveID,owner_token:result.envelope.owner.token,approval_digest:result.digest,owner_state:result.audited?'already_retired':'absent_on_same_linux_boot_and_pid_namespace',maintenance_lock_retained:true,receipt_replay_allowed:false});}catch(e){throw safe(e);}
}
export function retireWriterRecoveryGuard(options={},approval={}){
 if(!validRecoveryApproval(approval))fail('EXPLICIT_WRITER_RECOVERY_APPROVAL_REQUIRED');
 try{
  let result=staleWriterRecoveryContext(options,approval.archiveID,approval.ownerToken);
  if(result.digest!==approval.digest)fail('APPROVED_WRITER_PLAN_CHANGED');
  if(result.audited)return Object.freeze({action:'recovery_guard_already_retired',owner_token:result.envelope.owner.token,archive_id:approval.archiveID,maintenance_lock_retained:true,next_step:'review_recovery'});
  for(const dir of [result.ctx.archiveRoot,result.ctx.destination,result.root]){try{fs.mkdirSync(dir,{mode:0o700});}catch(e){if(e.code!=='EEXIST')throw e;}checkDirectory(dir);syncDirectory(path.dirname(dir));}
  if(!result.isRetired&&fs.readdirSync(result.root).length>=32)fail('WRITER_GUARD_HISTORY_LIMIT');
  result=staleWriterRecoveryContext(options,approval.archiveID,approval.ownerToken);
  if(result.digest!==approval.digest)fail('APPROVED_WRITER_PLAN_CHANGED');
  const token=result.envelope.owner.token,target=path.join(result.root,token);
  if(!result.isRetired){
   if(exists(target))fail('WRITER_RECOVERY_GUARD_ALREADY_RETIRED');
   requireDeadWriterRecovery(result.envelope.owner);
   fs.renameSync(result.guard,target);syncDirectory(result.root);syncDirectory(result.ctx.maintenance);
  }
  const archived=readWriterRecoveryOwner(target,result.ctx.operation,options,true,true);
  if(!equal(archived.envelope,result.envelope))fail('WRITER_RECOVERY_OWNER_CHANGED');
  requireDeadWriterRecovery(result.envelope.owner);
  const record={version:1,owner_mac:result.envelope.mac,operation_mac:result.ctx.operation.mac,reason:approval.reason,fence_reference:approval.fenceReference,proof:'absent_on_same_linux_boot_and_pid_namespace'};
  writeExclusive(path.join(target,'retirement.json'),{record,mac:mac(options.apiKey,['writer-guard-retirement-audit-v1',record])});syncDirectory(target);
  recoveryContext(options,approval.archiveID);
  return Object.freeze({action:'recovery_guard_retired',owner_token:token,archive_id:approval.archiveID,maintenance_lock_retained:true,receipt_replay_allowed:false,next_step:'review_recovery'});
 }catch(e){const result=safe(e);result.archiveID=approval.archiveID;try{result.maintenanceLockRetained=exists(path.join(options.directory,'.task-maintenance.lock'));}catch{result.maintenanceLockRetained=true;}throw result;}
}
// No network or credentials on command line. Offline authority/fencing is an
// explicit administrator assertion, never derived from a PID or stale timer.
export function runWriterRetirementCLI(argv,env,write=console.log){
 if(argv.length===0||argv.includes('--help')){write('Read-only default: --source-generation N [--resume ARCHIVE_ID] [--retire-recovery-guard [--owner-token TOKEN]]. Apply requires --apply --approval DIGEST --confirm-runtime-stopped --fence-ref REF --reason TEXT. Resume/guard apply additionally requires --confirm-maintenance-stopped. Credentials and directory use environment variables. No force or PID takeover.');return;}
 const values={},switches=new Set();for(let i=0;i<argv.length;i++){const a=argv[i];if(['--apply','--confirm-runtime-stopped','--confirm-maintenance-stopped','--retire-recovery-guard'].includes(a)){if(switches.has(a))fail('INVALID_ARGUMENTS');switches.add(a);}else if(['--source-generation','--approval','--fence-ref','--reason','--resume','--owner-token'].includes(a)){if(a in values||i+1>=argv.length)fail('INVALID_ARGUMENTS');values[a]=argv[++i];}else fail('INVALID_ARGUMENTS');}
 if(!/^(0|[1-9][0-9]*)$/.test(values['--source-generation']??''))fail('INVALID_ARGUMENTS');
 if('--resume' in values&&!validUUID(values['--resume']))fail('INVALID_ARGUMENTS');
 if('--owner-token' in values&&!validUUID(values['--owner-token'])||switches.has('--retire-recovery-guard')&&!values['--resume']||'--owner-token' in values&&!switches.has('--retire-recovery-guard'))fail('INVALID_ARGUMENTS');
 const options={directory:env.CLAWMANAGER_TASK_RECEIPT_DIR,baseURL:env.CLAWMANAGER_RECEIPT_SOURCE_BASE_URL??env.CLAWMANAGER_LLM_BASE_URL,apiKey:env.CLAWMANAGER_RECEIPT_SOURCE_API_KEY??env.CLAWMANAGER_LLM_API_KEY,generation:Number(values['--source-generation'])};
 if(!switches.has('--apply')&&([...switches].some(s=>s!=='--retire-recovery-guard')||Object.keys(values).some(k=>!['--source-generation','--resume','--owner-token'].includes(k)))||!values['--resume']&&switches.has('--confirm-maintenance-stopped'))fail('INVALID_ARGUMENTS');
 const approval={digest:values['--approval'],confirmRuntimeStopped:switches.has('--confirm-runtime-stopped'),confirmMaintenanceStopped:switches.has('--confirm-maintenance-stopped'),fenceReference:values['--fence-ref'],reason:values['--reason'],archiveID:values['--resume'],ownerToken:values['--owner-token']};
 const result=switches.has('--retire-recovery-guard')?(switches.has('--apply')?retireWriterRecoveryGuard(options,approval):planWriterGuardRetirement(options,values['--resume'],values['--owner-token'])):values['--resume']?(switches.has('--apply')?resumeWriterRetirement(options,approval):planWriterRecovery(options,values['--resume'])):(switches.has('--apply')?applyWriterRetirement(options,approval):planWriterRetirement(options));write(JSON.stringify(result));
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){try{runWriterRetirementCLI(process.argv.slice(2),process.env);}catch(e){const error=safe(e);console.error(JSON.stringify({error:error.code,action:'retain',maintenance_lock_retained:error.maintenanceLockRetained??false,archive_id:error.archiveID}));process.exitCode=1;}}
