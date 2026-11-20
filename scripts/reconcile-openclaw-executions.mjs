// Narrow offline maintenance: archive ONLY a complete journal whose every task
// has both an authenticated settled receipt and a matching server completion.
// Multiple original namespaces must be explicitly supplied, never guessed.
// Never finish tasks, alter credentials/generation, or delete receipt evidence.
import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {createHash,createHmac,randomUUID,timingSafeEqual} from 'node:crypto';
import {ReceiptInspectionError,canonicalBase,checkDirectory,sameFile,readHistoricalReceipts,inspectHistoricalReceipts} from './inspect-openclaw-receipts.mjs';

const hash=value=>createHash('sha256').update(value).digest('hex');
const fail=code=>{throw new ReceiptInspectionError(code);};
const identity=value=>JSON.stringify([value.generation,value.session_id,value.task_id]);
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}\.json$/;
const metadata=stat=>({device:String(stat.dev),inode:String(stat.ino),size:String(stat.size),mtime_ns:String(stat.mtimeNs)});
const journalName='z-task-executions',lockName='.task-maintenance.lock';
const archiveName='z-task-execution-archive';
const lockIdentity=stat=>({device:String(stat.dev),inode:String(stat.ino),birthtime_ns:String(stat.birthtimeNs)});
const equal=(a,b)=>JSON.stringify(a)===JSON.stringify(b);
const exists=file=>{try{fs.lstatSync(file);return true;}catch(error){if(error.code==='ENOENT')return false;throw error;}};
const assertNoRuntimeWriter=directory=>{checkDirectory(directory);if(exists(path.join(directory,'.task-writer.lock')))fail('RUNTIME_WRITER_CLAIM_PRESENT');};
const operationMAC=(operation,key)=>createHmac('sha256',key).update(JSON.stringify(['execution-maintenance-v1',operation])).digest('hex');
const ownerMAC=(owner,key)=>createHmac('sha256',key).update(JSON.stringify(['execution-recovery-owner-v1',owner])).digest('hex');

function localRecoveryScope(){
  if(process.platform!=='linux')return null;
  try{
    const machine=fs.readFileSync('/etc/machine-id','utf8').trim(),boot=fs.readFileSync('/proc/sys/kernel/random/boot_id','utf8').trim(),pidNS=fs.readlinkSync('/proc/self/ns/pid');
    if(!/^[a-f0-9]{32}$/.test(machine)||!uuid.test(boot+'.json')||!/^pid:\[\d+\]$/.test(pidNS))return null;
    return hash(JSON.stringify([machine,boot,pidNS]));
  }catch{return null;}
}
function readRecoveryOwner(guard,operation,options,retired=false){
  const stat=checkDirectory(guard);
  const names=fs.readdirSync(guard).sort();
  if(!equal(names,['owner.json'])&&!(retired&&equal(names,['owner.json','retirement.json'])))fail('UNVERIFIABLE_RECOVERY_OWNER');
  const envelope=readJSON(path.join(guard,'owner.json'),8192),owner=envelope?.owner;
  if(!owner||Object.keys(envelope).sort().join(',')!=='mac,owner'||owner.version!==1||!Number.isSafeInteger(owner.pid)||owner.pid<1||owner.pid>2147483647||!uuid.test((owner.token??'')+'.json')||owner.operation_mac!==operation.mac||!equal(owner.guard_identity,lockIdentity(stat))||owner.scope!==null&&!/^[a-f0-9]{64}$/.test(owner.scope??'')||!/^[a-f0-9]{64}$/.test(envelope.mac??''))fail('UNVERIFIABLE_RECOVERY_OWNER');
  if(!timingSafeEqual(Buffer.from(ownerMAC(owner,options.apiKey),'hex'),Buffer.from(envelope.mac,'hex')))fail('RECOVERY_OWNER_AUTHENTICATION_FAILED');
  if(retired&&names.includes('retirement.json')){
    const audit=readJSON(path.join(guard,'retirement.json'),8192),record=audit?.record;
    if(!record||record.owner_mac!==envelope.mac||record.operation_mac!==operation.mac||!/^[a-f0-9]{64}$/.test(audit.mac??''))fail('INVALID_MAINTENANCE_RECORD');
    const mac=createHmac('sha256',options.apiKey).update(JSON.stringify(['recovery-retirement-audit-v1',record])).digest('hex');
    if(!timingSafeEqual(Buffer.from(mac,'hex'),Buffer.from(audit.mac,'hex')))fail('INVALID_MAINTENANCE_RECORD');
  }
  return {envelope,stat};
}
function requireDeadLocalOwner(owner){
  const scope=localRecoveryScope();
  if(!scope||!owner.scope)fail('RECOVERY_OWNER_PROBE_UNSUPPORTED');
  if(scope!==owner.scope)fail('RECOVERY_OWNER_SCOPE_MISMATCH');
  try{process.kill(owner.pid,0);}catch(error){if(error.code==='ESRCH')return;fail('RECOVERY_OWNER_STATE_UNKNOWN');}
  fail('RECOVERY_OWNER_STILL_RUNNING');
}

function readJSON(file,maxSize=2097152){
  const stat=fs.lstatSync(file,{bigint:true});
  if(!stat.isFile()||stat.isSymbolicLink()||stat.size>BigInt(maxSize))fail('INVALID_MAINTENANCE_RECORD');
  const fd=fs.openSync(file,fs.constants.O_RDONLY|(fs.constants.O_NOFOLLOW??0));
  try{
    const opened=fs.fstatSync(fd,{bigint:true});
    if(!opened.isFile()||!sameFile(stat,opened,true))fail('SOURCE_CHANGED_DURING_READ');
    const buffer=Buffer.alloc(maxSize+1),length=fs.readSync(fd,buffer,0,buffer.length,0);
    if(length>maxSize)fail('INVALID_MAINTENANCE_RECORD');
    let value;try{value=JSON.parse(buffer.subarray(0,length).toString('utf8'));}catch{fail('INVALID_MAINTENANCE_RECORD');}
    if(!sameFile(opened,fs.fstatSync(fd,{bigint:true}))||!sameFile(stat,fs.lstatSync(file,{bigint:true})))fail('SOURCE_CHANGED_DURING_READ');
    return value;
  }finally{fs.closeSync(fd);}
}
function checkOperation(envelope,options,archiveID){
  const op=envelope?.operation;
  if(!op||Object.keys(envelope).sort().join(',')!=='mac,operation'||op.version!==1||op.archive_id!==archiveID||op.directory!==path.resolve(options.directory)||op.base!==canonicalBase(options.baseURL)||op.current_generation!==options.generation||op.source_generation!==options.sourceGeneration||!/^[a-f0-9]{64}$/.test(op.approval_digest??'')||!/^[a-f0-9]{64}$/.test(envelope.mac??''))fail('MAINTENANCE_OPERATION_MISMATCH');
  const expected=operationMAC(op,options.apiKey);
  if(!timingSafeEqual(Buffer.from(expected,'hex'),Buffer.from(envelope.mac,'hex')))fail('MAINTENANCE_OPERATION_AUTHENTICATION_FAILED');
  return envelope;
}
function assertLockIdentity(lock,operation){
  const stat=checkDirectory(lock);
  if(!equal(lockIdentity(stat),operation.operation.lock_identity))fail('MAINTENANCE_LOCK_CHANGED');
}
function releaseBoundLock(lock,operation,options){
  assertLockIdentity(lock,operation);
  const names=fs.readdirSync(lock);
  if(names.some(name=>name!=='operation.json'))fail('MAINTENANCE_LOCK_BUSY');
  const record=path.join(lock,'operation.json');
  if(exists(record)){
    if(!equal(readJSON(record,8192),operation))fail('MAINTENANCE_OPERATION_MISMATCH');
    fs.unlinkSync(record);
  }
  // A completed archive embeds the signed binding, so a crash between unlink
  // and rmdir can be resumed without adopting a different empty lock.
  fs.rmdirSync(lock);syncDirectory(options.directory);
}

function readJournal(directory,namespace,generation,maxEntries=256){
  const root=path.join(directory,journalName),before=checkDirectory(root),entries=[];
  const dir=fs.opendirSync(root);
  try{
    for(let entry;(entry=dir.readSync())!==null;){
      if(entries.length>=maxEntries)fail('SOURCE_SCAN_LIMIT_EXCEEDED');
      if(!uuid.test(entry.name))fail('UNVERIFIABLE_EXECUTION_JOURNAL');
      const file=path.join(root,entry.name),stat=fs.lstatSync(file,{bigint:true});
      if(!stat.isFile()||stat.isSymbolicLink()||stat.size>2048n)fail('UNVERIFIABLE_EXECUTION_JOURNAL');
      const fd=fs.openSync(file,fs.constants.O_RDONLY|(fs.constants.O_NOFOLLOW??0));let bytes,record;
      try{
        const opened=fs.fstatSync(fd,{bigint:true});
        if(!sameFile(stat,opened,true)||!opened.isFile())fail('SOURCE_CHANGED_DURING_READ');
        const buffer=Buffer.alloc(2049),length=fs.readSync(fd,buffer,0,buffer.length,0);
        if(length>2048)fail('UNVERIFIABLE_EXECUTION_JOURNAL');
        bytes=buffer.subarray(0,length);
        try{record=JSON.parse(bytes.toString('utf8'));}catch{fail('UNVERIFIABLE_EXECUTION_JOURNAL');}
        if(!sameFile(opened,fs.fstatSync(fd,{bigint:true})))fail('SOURCE_CHANGED_DURING_READ');
      }finally{fs.closeSync(fd);}
      const scoped=namespace instanceof Map;
      if(!record||Object.keys(record).sort().join(',')!=='generation,namespace,session_id,task_id,version'||record.version!==1||(scoped?namespace.get(record.namespace)!==record.generation:record.namespace!==namespace||record.generation!==generation)||typeof record.session_id!=='string'||typeof record.task_id!=='string'||!record.session_id||!record.task_id)fail('UNVERIFIABLE_EXECUTION_JOURNAL');
      if(!sameFile(stat,fs.lstatSync(file,{bigint:true})))fail('SOURCE_CHANGED_DURING_READ');
      entries.push({name:entry.name,sha256:hash(bytes),file:metadata(stat),task:{generation:record.generation,session_id:record.session_id,task_id:record.task_id},...(scoped?{source_namespace:record.namespace}:{})});
    }
  }finally{dir.closeSync();}
  if(!sameFile(before,checkDirectory(root)))fail('SOURCE_CHANGED_DURING_READ');
  entries.sort((a,b)=>a.name.localeCompare(b.name));
  if(!entries.length)fail('NO_EXECUTION_BARRIERS');
  if(new Set(entries.map(entry=>identity(entry.task))).size!==entries.length)fail('DUPLICATE_EXECUTION_IDENTITY');
  return {root:metadata(before),entries};
}

function settings(options){
  assertNoRuntimeWriter(options.directory);
  const base=canonicalBase(options.baseURL),sourceBase=canonicalBase(options.sourceBaseURL??base);
  const key=options.sourceApiKey??options.apiKey;
  const additional=options.additionalSources??[];
  if(!Array.isArray(additional)||additional.length>7)fail('INVALID_CONFIGURATION');
  if(!Number.isSafeInteger(options.generation)||options.generation<0||options.generation>2147483647||typeof options.apiKey!=='string'||!options.apiKey.trim()||/[\r\n]/.test(options.apiKey))fail('INVALID_CONFIGURATION');
  // readHistoricalReceipts is also the strict configuration and HMAC validator.
  const source={directory:options.directory,baseURL:sourceBase,apiKey:key,generation:options.sourceGeneration,maxEntries:options.maxEntries??256};
  const sources=[source];
  for(const item of additional){
    if(!item||Object.keys(item).sort().join(',')!=='apiKey,baseURL,generation')fail('INVALID_CONFIGURATION');
    sources.push({...source,baseURL:canonicalBase(item.baseURL),apiKey:item.apiKey,generation:item.generation});
  }
  const scopes=new Map();let scanned=0;
  const loaded=sources.map(item=>{
    if(!Number.isSafeInteger(item.generation)||item.generation<0||item.generation>options.generation)fail('INVALID_CONFIGURATION');
    const namespace=hash(JSON.stringify([item.baseURL,item.apiKey,item.generation]));
    if(scopes.has(namespace))fail('DUPLICATE_RECEIPT_SOURCE');
    scopes.set(namespace,item.generation);
    const receipts=readHistoricalReceipts(item);
    scanned+=receipts.scanned;if(scanned>source.maxEntries)fail('SOURCE_SCAN_LIMIT_EXCEEDED');
    return {source:item,namespace,receipts};
  }).sort((a,b)=>a.namespace.localeCompare(b.namespace));
  return {base,source,sources:loaded,mixed:additional.length>0,namespace:additional.length?scopes:loaded[0].namespace};
}

const scopedIdentity=(namespace,task)=>JSON.stringify([namespace,identity(task)]);
function receiptIndex(sources){
  return new Map(sources.flatMap(item=>item.receipts.receipts.map(receipt=>[scopedIdentity(item.namespace,receipt),receipt])));
}

async function prepare(options,journalDirectory=options.directory){
  const config=settings(options),before=readJournal(journalDirectory,config.namespace,options.sourceGeneration,config.source.maxEntries);
  const byIdentity=receiptIndex(config.sources),entryIdentity=entry=>scopedIdentity(entry.source_namespace??config.namespace,entry.task);
  for(const entry of before.entries)if(!byIdentity.has(entryIdentity(entry)))fail('SETTLED_RECEIPT_REQUIRED');
  const results=new Map(),totalTimeout=options.totalTimeoutMs??30000;
  if(!Number.isSafeInteger(totalTimeout)||totalTimeout<1||totalTimeout>60000)fail('INVALID_CONFIGURATION');
  const deadline=Date.now()+totalTimeout;
  for(const item of config.sources){
    const remaining=deadline-Date.now();if(remaining<1)fail('SERVER_INSPECTION_UNAVAILABLE');
    const inspected=await inspectHistoricalReceipts({...options,sourceBaseURL:item.source.baseURL,sourceApiKey:item.source.apiKey,sourceGeneration:item.source.generation,totalTimeoutMs:remaining});
    for(const task of inspected.tasks)results.set(scopedIdentity(item.namespace,task),task);
  }
  const evidence=before.entries.map(entry=>{
    const result=results.get(entryIdentity(entry));
    if(!result||!['completion_recorded','cancellation_recorded'].includes(result.status))fail('MATCHING_SERVER_COMPLETION_REQUIRED');
    return {...entry,receipt_sha256:hash(JSON.stringify(byIdentity.get(entryIdentity(entry)))),state:result.state,status:result.status};
  });
  if(JSON.stringify(before)!==JSON.stringify(readJournal(journalDirectory,config.namespace,options.sourceGeneration,config.source.maxEntries)))fail('SOURCE_CHANGED_DURING_READ');
  const current=receiptIndex(config.sources.map(item=>({...item,receipts:readHistoricalReceipts(item.source)})));
  for(const entry of evidence){const receipt=current.get(entryIdentity(entry));if(!receipt||hash(JSON.stringify(receipt))!==entry.receipt_sha256)fail('SOURCE_CHANGED_DURING_READ');}
  const plan={version:1,directory:path.resolve(options.directory),base:config.base,current_generation:options.generation,source_generation:options.sourceGeneration,journal_root:before.root,entries:evidence,...(config.mixed?{source_namespaces:config.sources.map(item=>item.namespace)}:{})};
  // Approval binds to this exact location, scope, credentials, files and states.
  const digest=createHmac('sha256',options.apiKey).update(JSON.stringify(plan)).digest('hex');
  return {plan,digest,config,snapshot:before};
}

const sanitized=error=>error instanceof ReceiptInspectionError?error:new ReceiptInspectionError(error.code==='ENOENT'?'SOURCE_NOT_FOUND':'EXECUTION_MAINTENANCE_FAILED');
export async function planExecutionReconciliation(options={}){
  try{
    const {plan,digest}=await prepare(options);
    return {read_only:true,action:'review',eligible:plan.entries.length,approval_digest:digest,source_generation:plan.source_generation,current_generation:plan.current_generation,...(plan.source_namespaces?{source_count:plan.source_namespaces.length,source_generations:[...new Set(plan.entries.map(entry=>entry.task.generation))].sort((a,b)=>a-b)}:{}),requires_stopped_runtime:true,receipt_replay_allowed:false,tasks:plan.entries.map(({task,status})=>({...task,status}))};
  }catch(error){throw sanitized(error);}
}

function syncDirectory(directory){
  if(process.platform==='win32')return; // Local tests are NOT power-loss acceptance.
  const fd=fs.openSync(directory,fs.constants.O_RDONLY|(fs.constants.O_DIRECTORY??0)|(fs.constants.O_NOFOLLOW??0));
  try{fs.fsyncSync(fd);}finally{fs.closeSync(fd);}
}
function writeExclusive(file,value){
  const fd=fs.openSync(file,'wx',0o600);
  try{fs.writeFileSync(fd,JSON.stringify(value,null,2));fs.fsyncSync(fd);}finally{fs.closeSync(fd);}
}

export async function applyExecutionReconciliation(options={},approval={}){
  if(approval.confirmRuntimeStopped!==true||!(/^[a-f0-9]{64}$/.test(approval.digest??''))||typeof approval.reason!=='string'||!approval.reason.trim()||approval.reason.length>200||/[\r\n]/.test(approval.reason))fail('EXPLICIT_MAINTENANCE_APPROVAL_REQUIRED');
  let lock,lockStat,changed=false,archiveID,existingLock=false,operation;
  const releaseLock=()=>{
    if(operation){releaseBoundLock(lock,operation,options);lock=undefined;return;}
    const current=fs.lstatSync(lock,{bigint:true});
    if(!current.isDirectory()||current.isSymbolicLink()||!sameFile(lockStat,current))fail('MAINTENANCE_LOCK_CHANGED');
    const removed=lock;fs.rmdirSync(lock);lock=undefined;
    try{syncDirectory(options.directory);}catch(error){
      // If durability of unlocking is uncertain, try to restore a barrier.
      // Report actual retained state, not a promise that an absent lock exists.
      try{fs.mkdirSync(removed,{mode:0o700});lock=removed;lockStat=fs.lstatSync(lock,{bigint:true});syncDirectory(options.directory);}catch{}
      throw error;
    }
  };
  try{
    // Validate source/configuration before acquiring a lock; never accept a root.
    settings(options);checkDirectory(options.directory);
    const candidate=path.join(options.directory,lockName);
    try{fs.mkdirSync(candidate,{mode:0o700});}catch(error){if(error.code==='EEXIST'){existingLock=true;fail('MAINTENANCE_ALREADY_LOCKED');}throw error;}
    lock=candidate;lockStat=fs.lstatSync(lock,{bigint:true});syncDirectory(options.directory);
    assertNoRuntimeWriter(options.directory);
    const prepared=await prepare(options),{plan,digest,config,snapshot}=prepared;
    if(digest!==approval.digest)fail('APPROVED_PLAN_CHANGED');
    archiveID=randomUUID();
    const binding={version:1,archive_id:archiveID,directory:path.resolve(options.directory),base:config.base,current_generation:options.generation,source_generation:options.sourceGeneration,approval_digest:digest,lock_identity:lockIdentity(lockStat),reason:approval.reason.trim(),created_at:new Date().toISOString()};
    operation={operation:binding,mac:operationMAC(binding,options.apiKey)};
    // From this point any failure retains the lock. A complete signed operation
    // is required for recovery; partial records are never overwritten.
    changed=true;writeExclusive(path.join(lock,'operation.json'),operation);syncDirectory(lock);
    const archiveRoot=path.join(options.directory,archiveName);
    try{fs.mkdirSync(archiveRoot,{mode:0o700});}catch(error){if(error.code!=='EEXIST')throw error;}
    checkDirectory(archiveRoot);syncDirectory(options.directory);
    const destination=path.join(archiveRoot,archiveID);
    fs.mkdirSync(destination,{mode:0o700});changed=true;syncDirectory(archiveRoot);
    // Audit is written before the atomic directory move. Preserve both audit
    // and lock on any later failure; never roll back or silently delete evidence.
    writeExclusive(path.join(destination,'prepared.json'),{version:1,approval_digest:digest,reason:approval.reason.trim(),operator_confirmed_runtime_stopped:true,created_at:new Date().toISOString(),source_generation:plan.source_generation,current_generation:plan.current_generation,operation,entries:plan.entries});
    syncDirectory(destination);
    if(JSON.stringify(snapshot)!==JSON.stringify(readJournal(options.directory,config.namespace,options.sourceGeneration,config.source.maxEntries)))fail('SOURCE_CHANGED_DURING_READ');
    fs.renameSync(path.join(options.directory,journalName),path.join(destination,journalName));
    syncDirectory(destination);syncDirectory(options.directory);
    if(JSON.stringify(snapshot)!==JSON.stringify(readJournal(destination,config.namespace,options.sourceGeneration,config.source.maxEntries)))fail('ARCHIVE_VERIFICATION_FAILED');
    writeExclusive(path.join(destination,'completed.json'),{version:1,approval_digest:digest,archived_at:new Date().toISOString(),count:plan.entries.length,receipts_retained:true,receipt_replay_allowed:false,operation});
    syncDirectory(destination);releaseLock();
    return {action:'archived',archive_id:archiveID,count:plan.entries.length,receipts_retained:true,receipt_replay_allowed:false,restart_required:true};
  }catch(error){
    // Before archive writes, our own empty lock can safely be released. A crash
    // or any uncertain archive outcome retains the maintenance barrier.
    if(lock&&!changed){try{releaseLock();}catch{}}
    const result=sanitized(error);result.maintenanceLockRetained=Boolean(lock&&exists(lock))||existingLock;if(archiveID)result.archiveID=archiveID;throw result;
  }
}

async function recoveryContext(options,archiveID,ownedGuard){
  if(typeof archiveID!=='string'||!uuid.test(archiveID+'.json'))fail('INVALID_ARCHIVE_ID');
  settings(options);checkDirectory(options.directory);
  const archiveRoot=path.join(options.directory,archiveName),destination=path.join(archiveRoot,archiveID),lock=path.join(options.directory,lockName);
  if(exists(archiveRoot))checkDirectory(archiveRoot);
  if(exists(destination))checkDirectory(destination);
  const optional=file=>exists(file)?readJSON(file):undefined;
  const prepared=optional(path.join(destination,'prepared.json')),completed=optional(path.join(destination,'completed.json'));
  const locked=exists(lock);
  if(locked){
    checkDirectory(lock);
    const names=fs.readdirSync(lock);
    if(names.some(name=>!['operation.json','resuming'].includes(name)))fail('MAINTENANCE_LOCK_BUSY');
    if(names.includes('resuming')){
      const actual=checkDirectory(path.join(lock,'resuming'));
      if(!ownedGuard||!equal(lockIdentity(actual),lockIdentity(ownedGuard)))fail('MAINTENANCE_RECOVERY_BUSY');
    }else if(ownedGuard)fail('MAINTENANCE_LOCK_CHANGED');
  }
  const descriptor=locked?optional(path.join(lock,'operation.json')):undefined;
  const operation=checkOperation(descriptor??prepared?.operation??completed?.operation,options,archiveID);
  for(const record of [descriptor,prepared?.operation,completed?.operation])if(record&&!equal(record,operation))fail('MAINTENANCE_OPERATION_MISMATCH');
  if(locked)assertLockIdentity(lock,operation);
  const sourceExists=exists(path.join(options.directory,journalName)),archived=exists(path.join(destination,journalName));
  if(sourceExists===archived)fail('AMBIGUOUS_ARCHIVE_STATE');
  if(!locked&&(!archived||!completed))fail('MAINTENANCE_LOCK_REQUIRED');
  if(exists(destination)&&fs.readdirSync(destination).some(name=>!['prepared.json','completed.json',journalName,'recovery-guards'].includes(name)))fail('UNEXPECTED_ARCHIVE_CONTENTS');
  const retiredRoot=path.join(destination,'recovery-guards');
  if(exists(retiredRoot)){
    checkDirectory(retiredRoot);const dir=fs.opendirSync(retiredRoot);let count=0;
    try{for(let item;(item=dir.readSync())!==null;){
      if(++count>256||!uuid.test(item.name+'.json'))fail('UNEXPECTED_ARCHIVE_CONTENTS');
      const retired=readRecoveryOwner(path.join(retiredRoot,item.name),operation,options,true);
      if(retired.envelope.owner.token!==item.name)fail('UNVERIFIABLE_RECOVERY_OWNER');
    }}finally{dir.closeSync();}
  }
  const fresh=await prepare(options,archived?destination:options.directory);
  if(fresh.digest!==operation.operation.approval_digest)fail('APPROVED_PLAN_CHANGED');
  if(prepared&&(prepared.version!==1||prepared.approval_digest!==fresh.digest||prepared.operator_confirmed_runtime_stopped!==true||prepared.reason!==operation.operation.reason||prepared.source_generation!==options.sourceGeneration||prepared.current_generation!==options.generation||!equal(prepared.entries,fresh.plan.entries)))fail('INVALID_MAINTENANCE_RECORD');
  if(completed&&(!archived||!prepared||completed.version!==1||completed.approval_digest!==fresh.digest||completed.count!==fresh.plan.entries.length||completed.receipts_retained!==true||completed.receipt_replay_allowed!==false))fail('INVALID_MAINTENANCE_RECORD');
  // Network awaits must not turn a stale lock/audit snapshot into permission
  // to mutate a replacement operation or newly created journal.
  if(locked!==exists(lock)||sourceExists!==exists(path.join(options.directory,journalName))||archived!==exists(path.join(destination,journalName)))fail('SOURCE_CHANGED_DURING_READ');
  if(locked){
    assertLockIdentity(lock,operation);
    if(!equal(descriptor,optional(path.join(lock,'operation.json'))))fail('MAINTENANCE_OPERATION_MISMATCH');
    const guardPath=path.join(lock,'resuming');
    if(ownedGuard){if(!equal(lockIdentity(checkDirectory(guardPath)),lockIdentity(ownedGuard)))fail('MAINTENANCE_LOCK_CHANGED');}
    else if(exists(guardPath))fail('MAINTENANCE_RECOVERY_BUSY');
  }
  if(!equal(prepared,optional(path.join(destination,'prepared.json')))||!equal(completed,optional(path.join(destination,'completed.json'))))fail('SOURCE_CHANGED_DURING_READ');
  return {archiveID,archiveRoot,destination,lock,locked,operation,prepared,completed,archived,fresh,stage:!locked?'already_archived':archived?'after_move':'before_move'};
}

export async function planExecutionRecovery(options={},archiveID){
  try{
    const ctx=await recoveryContext(options,archiveID);
    return {read_only:true,action:'review_recovery',archive_id:archiveID,stage:ctx.stage,eligible:ctx.fresh.plan.entries.length,approval_digest:ctx.fresh.digest,requires_stopped_runtime:true,requires_stopped_maintenance:true,receipt_replay_allowed:false};
  }catch(error){throw sanitized(error);}
}

export async function resumeExecutionReconciliation(options={},approval={}){
  if(approval.confirmRuntimeStopped!==true||approval.confirmMaintenanceStopped!==true||!/^[a-f0-9]{64}$/.test(approval.digest??'')||typeof approval.reason!=='string'||!approval.reason.trim()||approval.reason.length>200||/[\r\n]/.test(approval.reason))fail('EXPLICIT_RECOVERY_APPROVAL_REQUIRED');
  let guard,guardStat,guardOwner;
  const releaseGuard=()=>{
    if(!guard)return;
    const current=checkDirectory(guard);
    if(!equal(lockIdentity(current),lockIdentity(guardStat)))fail('MAINTENANCE_LOCK_CHANGED');
    if(guardOwner){
      if(!equal(fs.readdirSync(guard).sort(),['owner.json'])||!equal(readJSON(path.join(guard,'owner.json'),8192),guardOwner))fail('UNVERIFIABLE_RECOVERY_OWNER');
      fs.unlinkSync(path.join(guard,'owner.json'));
    }
    fs.rmdirSync(guard);guard=undefined;syncDirectory(path.join(options.directory,lockName));
  };
  try{
    let ctx=await recoveryContext(options,approval.archiveID);
    if(ctx.fresh.digest!==approval.digest)fail('APPROVED_PLAN_CHANGED');
    if(ctx.locked){
      const candidate=path.join(ctx.lock,'resuming');
      try{fs.mkdirSync(candidate,{mode:0o700});}catch(error){if(error.code==='EEXIST')fail('MAINTENANCE_RECOVERY_BUSY');throw error;}
      guard=candidate;guardStat=checkDirectory(guard);syncDirectory(ctx.lock);
      const owner={version:1,token:randomUUID(),pid:process.pid,scope:localRecoveryScope(),operation_mac:ctx.operation.mac,guard_identity:lockIdentity(guardStat)};
      const signedOwner={owner,mac:ownerMAC(owner,options.apiKey)};
      writeExclusive(path.join(guard,'owner.json'),signedOwner);syncDirectory(guard);guardOwner=signedOwner;
      // Recheck after acquiring the exclusive recovery guard. Another process
      // cannot join this recovery, and a crashed guard is never stolen.
      ctx=await recoveryContext(options,approval.archiveID,guardStat);
      if(ctx.fresh.digest!==approval.digest)fail('APPROVED_PLAN_CHANGED');
      for(const dir of [ctx.archiveRoot,ctx.destination]){try{fs.mkdirSync(dir,{mode:0o700});}catch(error){if(error.code!=='EEXIST')throw error;}checkDirectory(dir);syncDirectory(path.dirname(dir));}
      if(!ctx.prepared){
        writeExclusive(path.join(ctx.destination,'prepared.json'),{version:1,approval_digest:ctx.fresh.digest,reason:ctx.operation.operation.reason,operator_confirmed_runtime_stopped:true,created_at:new Date().toISOString(),source_generation:options.sourceGeneration,current_generation:options.generation,operation:ctx.operation,entries:ctx.fresh.plan.entries});syncDirectory(ctx.destination);
      }
      if(!ctx.archived){
        const {config,snapshot}=ctx.fresh;
        if(!equal(snapshot,readJournal(options.directory,config.namespace,options.sourceGeneration,config.source.maxEntries)))fail('SOURCE_CHANGED_DURING_READ');
        fs.renameSync(path.join(options.directory,journalName),path.join(ctx.destination,journalName));syncDirectory(ctx.destination);syncDirectory(options.directory);
      }
      const {config,snapshot}=ctx.fresh;
      if(!equal(snapshot,readJournal(ctx.destination,config.namespace,options.sourceGeneration,config.source.maxEntries)))fail('ARCHIVE_VERIFICATION_FAILED');
      if(!ctx.completed){
        writeExclusive(path.join(ctx.destination,'completed.json'),{version:1,approval_digest:ctx.fresh.digest,archived_at:new Date().toISOString(),count:ctx.fresh.plan.entries.length,receipts_retained:true,receipt_replay_allowed:false,operation:ctx.operation,recovery_reason:approval.reason.trim(),operator_confirmed_maintenance_stopped:true});syncDirectory(ctx.destination);
      }
      releaseGuard();releaseBoundLock(ctx.lock,ctx.operation,options);
    }
    return {action:ctx.locked?'archived':'already_archived',resumed:true,archive_id:approval.archiveID,count:ctx.fresh.plan.entries.length,receipts_retained:true,receipt_replay_allowed:false,restart_required:true};
  }catch(error){
    // Only our live process's empty recovery guard is released on failure.
    // The outer maintenance lock and all records remain for the next inspection.
    try{releaseGuard();}catch{}
    const result=sanitized(error);result.archiveID=approval.archiveID;
    try{result.maintenanceLockRetained=exists(path.join(options.directory,lockName));}catch{result.maintenanceLockRetained=true;}
    throw result;
  }
}

async function staleRecoveryContext(options,archiveID){
  if(typeof archiveID!=='string'||!uuid.test(archiveID+'.json'))fail('INVALID_ARCHIVE_ID');
  settings(options);const lock=path.join(options.directory,lockName),guard=path.join(lock,'resuming');
  checkDirectory(lock);const stat=checkDirectory(guard);
  // Reuse all existing archive, receipt, authorization and lock checks while
  // inspecting the retained guard. This does not confer ownership of it.
  const ctx=await recoveryContext(options,archiveID,stat);
  const loaded=readRecoveryOwner(guard,ctx.operation,options);requireDeadLocalOwner(loaded.envelope.owner);
  const digest=createHmac('sha256',options.apiKey).update(JSON.stringify(['retire-recovery-guard-v1',archiveID,ctx.fresh.digest,loaded.envelope])).digest('hex');
  return {ctx,guard,...loaded,digest};
}
export async function planStaleRecoveryRetirement(options={},archiveID){
  try{const result=await staleRecoveryContext(options,archiveID);return {read_only:true,action:'review_guard_retirement',archive_id:archiveID,approval_digest:result.digest,owner_state:'absent_on_same_linux_boot_and_pid_namespace',receipt_replay_allowed:false,maintenance_lock_retained:true};}
  catch(error){throw sanitized(error);}
}
export async function retireStaleRecoveryGuard(options={},approval={}){
  if(approval.confirmRuntimeStopped!==true||approval.confirmMaintenanceStopped!==true||!/^[a-f0-9]{64}$/.test(approval.digest??'')||typeof approval.reason!=='string'||!approval.reason.trim()||approval.reason.length>200||/[\r\n]/.test(approval.reason))fail('EXPLICIT_RECOVERY_APPROVAL_REQUIRED');
  try{
    const result=await staleRecoveryContext(options,approval.archiveID),{ctx,guard,envelope}=result;
    if(result.digest!==approval.digest)fail('APPROVED_PLAN_CHANGED');
    const retiredRoot=path.join(ctx.destination,'recovery-guards');
    for(const dir of [ctx.archiveRoot,ctx.destination,retiredRoot]){try{fs.mkdirSync(dir,{mode:0o700});}catch(error){if(error.code!=='EEXIST')throw error;}checkDirectory(dir);syncDirectory(path.dirname(dir));}
    // Match recoveryContext's bounded history reader. Never append entry 257
    // and make a previously readable archive impossible to recover.
    const history=fs.opendirSync(retiredRoot);let historyCount=0;
    try{while(history.readSync()!==null)if(++historyCount>=256)fail('RECOVERY_HISTORY_FULL');}finally{history.closeSync();}
    const target=path.join(retiredRoot,envelope.owner.token);
    if(exists(target))fail('RECOVERY_GUARD_ALREADY_RETIRED');
    assertLockIdentity(ctx.lock,ctx.operation);
    const current=readRecoveryOwner(guard,ctx.operation,options);
    if(!equal(current.envelope,envelope))fail('RECOVERY_OWNER_CHANGED');
    requireDeadLocalOwner(envelope.owner);
    // Deterministic NONEMPTY destination prevents competing retirements from
    // replacing the first archived guard with a later process's guard.
    fs.renameSync(guard,target);syncDirectory(retiredRoot);syncDirectory(ctx.lock);
    if(!equal(readRecoveryOwner(target,ctx.operation,options).envelope,envelope))fail('ARCHIVE_VERIFICATION_FAILED');
    const audit={version:1,owner_mac:envelope.mac,operation_mac:ctx.operation.mac,reason:approval.reason.trim(),retired_at:new Date().toISOString(),proof:'absent_on_same_linux_boot_and_pid_namespace'};
    writeExclusive(path.join(target,'retirement.json'),{record:audit,mac:createHmac('sha256',options.apiKey).update(JSON.stringify(['recovery-retirement-audit-v1',audit])).digest('hex')});syncDirectory(target);
    assertLockIdentity(ctx.lock,ctx.operation);
    return {action:'recovery_guard_retired',archive_id:approval.archiveID,maintenance_lock_retained:true,receipt_replay_allowed:false,next_step:'review_recovery'};
  }catch(error){const result=sanitized(error);result.archiveID=approval.archiveID;try{result.maintenanceLockRetained=exists(path.join(options.directory,lockName));}catch{result.maintenanceLockRetained=true;}throw result;}
}

export async function runExecutionReconciliationCLI(argv,env,write=console.log){
  if(argv.length===1&&argv[0]==='--help'){write('Usage: node scripts/reconcile-openclaw-executions.mjs --source-generation N [--additional-sources] [--resume ARCHIVE_ID [--retire-stale-recovery]] [--approve DIGEST --confirm-runtime-stopped --reason TICKET [--confirm-maintenance-stopped]]\nDefault is read-only. --additional-sources reads CLAWMANAGER_RECEIPT_ADDITIONAL_SOURCES_JSON (up to 7 original baseURL/apiKey/generation objects). Stale retirement requires a signed owner absent on the same Linux boot/PID namespace. Credentials use the receipt-inspection environment.');return;}
  const args=new Map();
  for(let i=0;i<argv.length;i++){
    const name=argv[i];if(args.has(name)||!['--source-generation','--additional-sources','--resume','--retire-stale-recovery','--approve','--confirm-runtime-stopped','--confirm-maintenance-stopped','--reason'].includes(name))fail('INVALID_ARGUMENTS');
    if(['--confirm-runtime-stopped','--confirm-maintenance-stopped','--retire-stale-recovery','--additional-sources'].includes(name)){args.set(name,true);continue;}
    if(argv[i+1]===undefined||argv[i+1].startsWith('--'))fail('INVALID_ARGUMENTS');args.set(name,argv[++i]);
  }
  if(!/^(0|[1-9][0-9]*)$/.test(args.get('--source-generation')??'')||!/^(0|[1-9][0-9]*)$/.test(env.CLAWMANAGER_GATEWAY_GENERATION??''))fail('INVALID_CONFIGURATION');
  const options={directory:env.CLAWMANAGER_TASK_RECEIPT_DIR,baseURL:env.CLAWMANAGER_LLM_BASE_URL,apiKey:env.CLAWMANAGER_LLM_API_KEY,generation:Number(env.CLAWMANAGER_GATEWAY_GENERATION),sourceBaseURL:env.CLAWMANAGER_RECEIPT_SOURCE_BASE_URL,sourceApiKey:env.CLAWMANAGER_RECEIPT_SOURCE_API_KEY,sourceGeneration:Number(args.get('--source-generation'))};
  if(args.has('--additional-sources')){
    const raw=env.CLAWMANAGER_RECEIPT_ADDITIONAL_SOURCES_JSON;
    if(typeof raw!=='string'||Buffer.byteLength(raw,'utf8')>65536)fail('INVALID_CONFIGURATION');
    try{options.additionalSources=JSON.parse(raw);}catch{fail('INVALID_CONFIGURATION');}
    if(!Array.isArray(options.additionalSources)||options.additionalSources.length<1||options.additionalSources.length>7)fail('INVALID_CONFIGURATION');
  }
  if(!args.has('--approve')&&(args.has('--reason')||args.has('--confirm-runtime-stopped')||args.has('--confirm-maintenance-stopped'))||!args.has('--resume')&&args.has('--confirm-maintenance-stopped'))fail('INVALID_ARGUMENTS');
  if(args.has('--retire-stale-recovery')&&!args.has('--resume'))fail('INVALID_ARGUMENTS');
  const approval={archiveID:args.get('--resume'),digest:args.get('--approve'),confirmRuntimeStopped:args.get('--confirm-runtime-stopped')===true,confirmMaintenanceStopped:args.get('--confirm-maintenance-stopped')===true,reason:args.get('--reason')};
  const result=args.has('--retire-stale-recovery')?(args.has('--approve')?await retireStaleRecoveryGuard(options,approval):await planStaleRecoveryRetirement(options,args.get('--resume'))):args.has('--resume')?(args.has('--approve')?await resumeExecutionReconciliation(options,approval):await planExecutionRecovery(options,args.get('--resume'))):(args.has('--approve')?await applyExecutionReconciliation(options,approval):await planExecutionReconciliation(options));
  write(JSON.stringify(result,null,2));
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
  runExecutionReconciliationCLI(process.argv.slice(2),process.env).catch(error=>{const result=sanitized(error);console.error(JSON.stringify({error:result.code,maintenance_lock_retained:result.maintenanceLockRetained??false,archive_id:result.archiveID,action:'retain'}));process.exitCode=1;});
}
