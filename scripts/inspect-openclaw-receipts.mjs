// Operator-only, read-only reconciliation. Never import the writable runtime
// store here: opening that store creates namespaces and may trigger recovery.
import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {createHash,createHmac,timingSafeEqual} from 'node:crypto';

export class ReceiptInspectionError extends Error {
  constructor(code){super(code);this.name='ReceiptInspectionError';this.code=code;}
}
const fail=code=>{throw new ReceiptInspectionError(code);};
const hash=value=>createHash('sha256').update(value).digest('hex');
const validGeneration=value=>Number.isSafeInteger(value)&&value>=0&&value<=2147483647;
const validID=value=>typeof value==='string'&&/^[\x21-\x7e]{1,191}$/.test(value);
const identity=value=>JSON.stringify([value.generation,value.session_id,value.task_id]);
const validKey=value=>typeof value==='string'&&value.trim()!==''&&!/[\r\n]/.test(value);
export function canonicalBase(value){
  let url;try{url=new URL(value);}catch{fail('INVALID_CONFIGURATION');}
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password||url.search||url.hash)fail('INVALID_CONFIGURATION');
  return url.href.replace(/\/+$/,'');
}
export function checkDirectory(directory){
  const resolved=path.resolve(directory),root=path.parse(resolved).root;
  let current=root;
  for(const part of [null,...resolved.slice(root.length).split(path.sep).filter(Boolean)]){
    if(part!==null)current=path.join(current,part);
    const stat=fs.lstatSync(current);
    if(!stat.isDirectory()||stat.isSymbolicLink())fail('UNSAFE_SOURCE_DIRECTORY');
  }
  return fs.lstatSync(resolved,{bigint:true});
}
export const sameFile=(a,b,crossAPI=false)=>(a.dev===b.dev||crossAPI&&process.platform==='win32'&&a.dev===0n)&&a.ino===b.ino&&a.size===b.size&&a.mtimeNs===b.mtimeNs;

// Instance-wide barriers deliberately survive key and generation changes.
// Count every entry as uncertainty; do not read it as an authenticated receipt.
export function inspectExecutionBarriers(directory,{maxEntries=1024}={}){
  if(typeof directory!=='string'||!path.isAbsolute(directory)||!Number.isSafeInteger(maxEntries)||maxEntries<1||maxEntries>1024)fail('INVALID_CONFIGURATION');
  let dir;
  try{
    checkDirectory(directory);
    let maintenanceLocked=false;
    try{fs.lstatSync(path.join(directory,'.task-maintenance.lock'));maintenanceLocked=true;}catch(error){if(error.code!=='ENOENT')throw error;}
    // Presence is uncertainty, not proof of a live/dead process. Never read,
    // follow, adopt or remove a writer claim in this read-only inspector.
    let writerClaimPresent=false;
    try{fs.lstatSync(path.join(directory,'.task-writer.lock'));writerClaimPresent=true;}catch(error){if(error.code!=='ENOENT')throw error;}
    const writerFields=writerClaimPresent?{writer_claim_present:true}:{};
    const root=path.join(directory,'z-task-executions');
    let before;try{before=checkDirectory(root);}catch(error){if(error.code==='ENOENT')return Object.freeze({pending:0,maintenance_locked:maintenanceLocked,...writerFields,requires_manual_reconciliation:maintenanceLocked||writerClaimPresent});throw error;}
    dir=fs.opendirSync(root);let pending=0;
    while(dir.readSync()!==null)if(++pending>maxEntries)fail('SOURCE_SCAN_LIMIT_EXCEEDED');
    if(!sameFile(before,checkDirectory(root)))fail('SOURCE_CHANGED_DURING_READ');
    return Object.freeze({pending,maintenance_locked:maintenanceLocked,...writerFields,requires_manual_reconciliation:pending>0||maintenanceLocked||writerClaimPresent});
  }catch(error){if(error instanceof ReceiptInspectionError)throw error;fail('SOURCE_READ_FAILED');}
  finally{try{dir?.closeSync();}catch{}}
}

// Verify a single EXPLICIT source namespace with its original key. Do not scan
// other namespaces, guess old keys, recover pending files, or write source files.
// Filesystem access times may still change as a consequence of reading.
export function readHistoricalReceipts({directory,baseURL,apiKey,generation,maxEntries=256}={}){
  const base=canonicalBase(baseURL);
  if(typeof directory!=='string'||!path.isAbsolute(directory)||path.resolve(directory)===path.parse(path.resolve(directory)).root||!validKey(apiKey)||!validGeneration(generation)||!Number.isSafeInteger(maxEntries)||maxEntries<1||maxEntries>1024)fail('INVALID_CONFIGURATION');
  const namespace=hash(JSON.stringify([base,apiKey,generation]));
  const root=path.join(directory,namespace),records=new Map();
  let scanned=0,pendingFiles=0,dir;
  try{
    checkDirectory(directory);const before=checkDirectory(root);
    dir=fs.opendirSync(root);
    for(let entry;(entry=dir.readSync())!==null;){
      if(++scanned>maxEntries)fail('SOURCE_SCAN_LIMIT_EXCEEDED');
      const published=/^[a-f0-9]{64}\.json$/.test(entry.name);
      const pending=/^\.pending-[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/.test(entry.name);
      if(!published&&!pending)fail('SOURCE_CONTAINS_UNKNOWN_FILES');
      const file=path.join(root,entry.name),stat=fs.lstatSync(file,{bigint:true});
      if(!stat.isFile()||stat.isSymbolicLink()||stat.size>2048n)fail('UNSAFE_RECEIPT_FILE');
      const fd=fs.openSync(file,fs.constants.O_RDONLY|(fs.constants.O_NOFOLLOW??0));
      let envelope;
      try{
        const opened=fs.fstatSync(fd,{bigint:true});
        // Windows lstat may report dev=0 while fstat reports the volume number.
        // Keep inode/size/mtime checks; same-API comparisons still require dev.
        if(!opened.isFile()||!sameFile(stat,opened,true))fail('SOURCE_CHANGED_DURING_READ');
        const buffer=Buffer.alloc(2049),size=fs.readSync(fd,buffer,0,buffer.length,0);
        if(size>2048)fail('UNSAFE_RECEIPT_FILE');
        try{envelope=JSON.parse(buffer.subarray(0,size).toString('utf8'));}catch{fail('INVALID_RECEIPT_ENVELOPE');}
        if(!sameFile(opened,fs.fstatSync(fd,{bigint:true})))fail('SOURCE_CHANGED_DURING_READ');
      }finally{fs.closeSync(fd);}
      const receipt=envelope?.receipt;
      if(!envelope||Object.keys(envelope).sort().join(',')!=='mac,receipt'||!receipt||Object.keys(receipt).sort().join(',')!=='cancelled,generation,session_id,task_id'||receipt.generation!==generation||!validID(receipt.session_id)||!validID(receipt.task_id)||typeof receipt.cancelled!=='boolean')fail('INVALID_RECEIPT_ENVELOPE');
      const mac=createHmac('sha256',apiKey).update(JSON.stringify([namespace,generation,receipt.session_id,receipt.task_id,receipt.cancelled])).digest();
      if(typeof envelope.mac!=='string'||!/^[a-f0-9]{64}$/.test(envelope.mac)||!timingSafeEqual(mac,Buffer.from(envelope.mac,'hex')))fail('RECEIPT_AUTHENTICATION_FAILED');
      const id=identity(receipt);
      if(published&&entry.name!==hash(id)+'.json')fail('RECEIPT_IDENTITY_MISMATCH');
      if(records.has(id)&&records.get(id).cancelled!==receipt.cancelled)fail('CONFLICTING_RECEIPTS');
      records.set(id,Object.freeze(receipt));
      if(pending)pendingFiles++;
      if(!sameFile(stat,fs.lstatSync(file,{bigint:true})))fail('SOURCE_CHANGED_DURING_READ');
    }
    const after=checkDirectory(root);
    if(!sameFile(before,after))fail('SOURCE_CHANGED_DURING_READ');
    return Object.freeze({receipts:Object.freeze([...records.values()]),scanned,pendingFiles});
  }catch(error){
    if(error instanceof ReceiptInspectionError)throw error;
    fail(error.code==='ENOENT'?'SOURCE_NOT_FOUND':'SOURCE_READ_FAILED');
  }finally{try{dir?.closeSync();}catch{}}
}

function classify(receipt,state){
  if(state===null)return 'server_record_missing';
  if(state.runtime_acknowledged)return receipt.cancelled?'cancellation_recorded':'receipt_state_conflict';
  if(receipt.cancelled||state.revoked)return 'cancellation_unconfirmed';
  return state.finished?'completion_recorded':'server_task_unfinished';
}

export async function inspectHistoricalReceipts({directory,sourceBaseURL,sourceApiKey,sourceGeneration,baseURL,apiKey,generation,maxEntries=256,fetcher=globalThis.fetch,timeoutMs=2500,totalTimeoutMs=30000}={}){
  const base=canonicalBase(baseURL);
  if(!validKey(apiKey)||!validGeneration(generation)||!validGeneration(sourceGeneration)||sourceGeneration>generation||!Number.isSafeInteger(timeoutMs)||timeoutMs<1||!Number.isSafeInteger(totalTimeoutMs)||totalTimeoutMs<1||totalTimeoutMs>60000)fail('INVALID_CONFIGURATION');
  const local=readHistoricalReceipts({directory,baseURL:sourceBaseURL??base,apiKey:sourceApiKey??apiKey,generation:sourceGeneration,maxEntries});
  const executionBarriers=inspectExecutionBarriers(directory);
  const report={read_only:true,receipt_replay_allowed:false,archive_allowed:false,execution_barriers:executionBarriers,source_generation:sourceGeneration,current_generation:generation,scanned:local.scanned,pending_files:local.pendingFiles,tasks:[]};
  const overall=new AbortController(),overallTimer=setTimeout(()=>overall.abort(),totalTimeoutMs);
  try{
    for(let start=0;start<local.receipts.length;start+=32){
      const receipts=local.receipts.slice(start,start+32);
      const tasks=receipts.map(({generation,session_id,task_id})=>({generation,session_id,task_id}));
      const deadline=new AbortController(),timer=setTimeout(()=>deadline.abort(),timeoutMs);
      const signal=AbortSignal.any([overall.signal,deadline.signal]);let reader;
      try{
        signal.throwIfAborted();
        const response=await fetcher(base+'/runtime/tasks/inspect',{method:'POST',redirect:'error',signal,headers:{Authorization:'Bearer '+apiKey,'Content-Type':'application/json',Accept:'application/json'},body:JSON.stringify({generation,tasks})});
        if(response.status===404||response.status===405)fail('SERVER_INSPECTION_UNSUPPORTED');
        if(response.status===401||response.status===403)fail('CURRENT_INSTANCE_AUTHORIZATION_DENIED');
        if(!response.ok||!response.body)fail('SERVER_INSPECTION_UNAVAILABLE');
        reader=response.body.getReader();const chunks=[];let size=0;
        for(;;){const part=await reader.read();if(part.done)break;size+=part.value.byteLength;if(size>32768)fail('INVALID_SERVER_CONTRACT');chunks.push(Buffer.from(part.value));}
        signal.throwIfAborted();
        let value;try{value=JSON.parse(Buffer.concat(chunks).toString('utf8'));}catch{fail('INVALID_SERVER_CONTRACT');}
        if(value?.generation!==generation||value.read_only!==true||value.receipt_replay_allowed!==false||!Array.isArray(value.tasks)||value.tasks.length!==tasks.length)fail('INVALID_SERVER_CONTRACT');
        for(let i=0;i<tasks.length;i++){
          const item=value.tasks[i],state=item?.state;
          if(!item||identity(item)!==identity(tasks[i])||state!==null&&(!state||Object.keys(state).sort().join(',')!=='finished,revoked,runtime_acknowledged'||['finished','revoked','runtime_acknowledged'].some(key=>typeof state[key]!=='boolean')||state.runtime_acknowledged&&(!state.finished||!state.revoked)))fail('INVALID_SERVER_CONTRACT');
          report.tasks.push({...tasks[i],local_cancelled:receipts[i].cancelled,state,status:classify(receipts[i],state),action:'retain'});
        }
      }catch(error){if(error instanceof ReceiptInspectionError)throw error;fail('SERVER_INSPECTION_UNAVAILABLE');}
      finally{clearTimeout(timer);void reader?.cancel().catch(()=>{});}
    }
    return report;
  }finally{clearTimeout(overallTimer);}
}

function generationFrom(value){if(!/^(0|[1-9][0-9]*)$/.test(value??''))fail('INVALID_CONFIGURATION');return Number(value);}
export async function runReceiptInspectionCLI(argv,env,write=console.log){
  if(argv.length===1&&argv[0]==='--help'){
    write('Usage: node scripts/inspect-openclaw-receipts.mjs --source-generation N [--max-entries N]\nCredentials and directory are read from environment; no replay or file mutation is supported.');return;
  }
  const args=new Map();
  for(let i=0;i<argv.length;i+=2){if(!['--source-generation','--max-entries'].includes(argv[i])||args.has(argv[i])||argv[i+1]===undefined)fail('INVALID_ARGUMENTS');args.set(argv[i],argv[i+1]);}
  const report=await inspectHistoricalReceipts({directory:env.CLAWMANAGER_TASK_RECEIPT_DIR,sourceGeneration:generationFrom(args.get('--source-generation')),generation:generationFrom(env.CLAWMANAGER_GATEWAY_GENERATION),baseURL:env.CLAWMANAGER_LLM_BASE_URL,apiKey:env.CLAWMANAGER_LLM_API_KEY,sourceBaseURL:env.CLAWMANAGER_RECEIPT_SOURCE_BASE_URL,sourceApiKey:env.CLAWMANAGER_RECEIPT_SOURCE_API_KEY,maxEntries:args.has('--max-entries')?generationFrom(args.get('--max-entries')):256});
  write(JSON.stringify(report,null,2));
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
  runReceiptInspectionCLI(process.argv.slice(2),process.env).catch(error=>{console.error(JSON.stringify({error:error instanceof ReceiptInspectionError?error.code:'RECEIPT_INSPECTION_FAILED',action:'retain'}));process.exitCode=1;});
}
