import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {createHash,createHmac,randomUUID} from 'node:crypto';
import {pathToFileURL} from 'node:url';
import {readHistoricalReceipts,inspectHistoricalReceipts,inspectExecutionBarriers,runReceiptInspectionCLI} from './inspect-openclaw-receipts.mjs';

const sourceBase='http://old.invalid/api/gateway/llm',sourceKey='old-test-secret',currentBase='http://new.invalid/api/gateway/llm',currentKey='new-test-secret';
const hash=value=>createHash('sha256').update(value).digest('hex');
let directory,root;
test.beforeEach(()=>{directory=fs.mkdtempSync(path.join(os.tmpdir(),'cm-inspect-receipts-'));root=path.join(directory,hash(JSON.stringify([sourceBase,sourceKey,5])));fs.mkdirSync(root);});
test.afterEach(()=>fs.rmSync(directory,{recursive:true,force:true}));
const input=()=>({directory,sourceBaseURL:sourceBase,sourceApiKey:sourceKey,sourceGeneration:5,baseURL:currentBase,apiKey:currentKey,generation:6});
const readInput=()=>({directory,baseURL:sourceBase,apiKey:sourceKey,generation:5});
const receipt=(id='t',cancelled=true)=>({generation:5,session_id:'s',task_id:id,cancelled});
function put(r,{pending=false}={}){
  const namespace=path.basename(root),mac=createHmac('sha256',sourceKey).update(JSON.stringify([namespace,r.generation,r.session_id,r.task_id,r.cancelled])).digest('hex');
  const file=path.join(root,pending?'.pending-'+randomUUID():hash(JSON.stringify([r.generation,r.session_id,r.task_id]))+'.json');
  fs.writeFileSync(file,JSON.stringify({receipt:r,mac}));return file;
}
const snapshot=()=>fs.readdirSync(root).sort().map(name=>[name,hash(fs.readFileSync(path.join(root,name))),fs.statSync(path.join(root,name)).mtimeMs]);
const state=(revoked=false,finished=false,runtime_acknowledged=false)=>({revoked,finished,runtime_acknowledged});
function responder(getState=()=>state(true,true,true)){
  const calls=[];
  const fetcher=async(url,options)=>{
    assert.equal(url,currentBase+'/runtime/tasks/inspect');assert.equal(options.method,'POST');assert.equal(options.redirect,'error');
    assert.equal(options.headers.Authorization,'Bearer '+currentKey);
    assert.ok(!JSON.stringify(options.body).includes(sourceKey));
    const body=JSON.parse(options.body);calls.push(body);assert.equal(body.generation,6);assert.ok(body.tasks.length<=32);
    return Response.json({generation:6,read_only:true,receipt_replay_allowed:false,tasks:body.tasks.map(task=>({...task,state:getState(task)}))});
  };return {calls,fetcher};
}

test('authenticates published and complete pending copies without recovery or mutation',async()=>{
  put(receipt());put(receipt(),{pending:true});const before=snapshot();
  const local=readHistoricalReceipts(readInput());assert.equal(local.receipts.length,1);assert.equal(local.pendingFiles,1);
  const f=responder(),result=await inspectHistoricalReceipts({...input(),fetcher:f.fetcher});
  assert.equal(f.calls.length,1);assert.equal(result.tasks[0].status,'cancellation_recorded');assert.equal(result.tasks[0].action,'retain');
  assert.equal(result.archive_allowed,false);assert.equal(result.receipt_replay_allowed,false);assert.deepEqual(snapshot(),before);
  assert.ok(!JSON.stringify(result).includes(sourceKey));assert.ok(!JSON.stringify(result).includes(currentKey));
});

test('instance-wide execution barriers remain visible without interpreting partial contents',async()=>{
  const journal=path.join(directory,'z-task-executions');
  assert.deepEqual(inspectExecutionBarriers(directory),{pending:0,maintenance_locked:false,requires_manual_reconciliation:false});
  assert.equal(fs.existsSync(journal),false);
  fs.mkdirSync(journal);fs.writeFileSync(path.join(journal,'partial'),'not a receipt');fs.mkdirSync(path.join(journal,'unknown'));
  const result=await inspectHistoricalReceipts({...input(),fetcher:()=>{throw new Error('empty receipts must not query');}});
  assert.deepEqual(result.execution_barriers,{pending:2,maintenance_locked:false,requires_manual_reconciliation:true});
  assert.equal(result.archive_allowed,false);assert.equal(result.tasks.length,0);
  assert.equal(fs.readFileSync(path.join(journal,'partial'),'utf8'),'not a receipt');
  assert.throws(()=>inspectExecutionBarriers(directory,{maxEntries:1}),{code:'SOURCE_SCAN_LIMIT_EXCEEDED'});
});

test('unsafe execution journal fails before any server request',async()=>{
  put(receipt());fs.writeFileSync(path.join(directory,'z-task-executions'),'invalid directory');let calls=0;
  await assert.rejects(inspectHistoricalReceipts({...input(),fetcher:()=>{calls++;}}),{code:'UNSAFE_SOURCE_DIRECTORY'});
  assert.equal(calls,0);
});

test('an orphan maintenance lock requires reconciliation even without a journal',()=>{
  fs.writeFileSync(path.join(directory,'.task-maintenance.lock'),'partial');
  assert.deepEqual(inspectExecutionBarriers(directory),{pending:0,maintenance_locked:true,requires_manual_reconciliation:true});
  assert.equal(fs.readFileSync(path.join(directory,'.task-maintenance.lock'),'utf8'),'partial');
});

test('writer claim presence is reported even without execution barriers',async()=>{
 const claim=path.join(directory,'.task-writer.lock');fs.mkdirSync(claim);
 fs.writeFileSync(path.join(claim,'owner.json'),'not trusted stop evidence');
 const before=fs.readFileSync(path.join(claim,'owner.json'),'utf8');
 const result=await inspectHistoricalReceipts({...input(),fetcher:()=>assert.fail('empty namespace must not query')});
 assert.equal(result.execution_barriers.writer_claim_present,true);
 assert.equal(result.execution_barriers.requires_manual_reconciliation,true);
 assert.equal(result.execution_barriers.pending,0);assert.equal(result.archive_allowed,false);
 assert.equal(fs.readFileSync(path.join(claim,'owner.json'),'utf8'),before);
 assert.ok(!JSON.stringify(result).includes('not trusted stop evidence'));
});

test('partial writer entry is retained and reported, never treated as stopped',()=>{
 const claim=path.join(directory,'.task-writer.lock');fs.writeFileSync(claim,'partial writer');
 const result=inspectExecutionBarriers(directory);
 assert.equal(result.writer_claim_present,true);assert.equal(result.requires_manual_reconciliation,true);
 assert.equal(fs.readFileSync(claim,'utf8'),'partial writer');
});

test('writer presence is preserved alongside execution and maintenance barriers',()=>{
 fs.mkdirSync(path.join(directory,'.task-writer.lock'));fs.mkdirSync(path.join(directory,'.task-maintenance.lock'));
 const journal=path.join(directory,'z-task-executions');fs.mkdirSync(journal);fs.writeFileSync(path.join(journal,'unknown'),'unverified');
 const result=inspectExecutionBarriers(directory);
 assert.equal(result.pending,1);assert.equal(result.maintenance_locked,true);assert.equal(result.writer_claim_present,true);
 assert.equal(result.requires_manual_reconciliation,true);
});

test('original key and endpoint verify source while only the current credential is sent',async()=>{
  put(receipt());const f=responder();await inspectHistoricalReceipts({...input(),fetcher:f.fetcher});
  assert.equal(f.calls[0].tasks[0].generation,5);assert.equal(f.calls[0].tasks[0].cancelled,undefined);
  const names=fs.readdirSync(directory);
  assert.throws(()=>readHistoricalReceipts({...readInput(),apiKey:'wrong-key'}),{code:'SOURCE_NOT_FOUND'});
  assert.deepEqual(fs.readdirSync(directory),names);
});

test('stored states never imply stopped tools or authorize archive',async()=>{
  const cases=[['missing',true,null,'server_record_missing'],['pending',false,state(),'server_task_unfinished'],['done',false,state(false,true),'completion_recorded'],['revoked',true,state(true),'cancellation_unconfirmed'],['normal-revoked',false,state(true,true),'cancellation_unconfirmed'],['conflict',false,state(true,true,true),'receipt_state_conflict']];
  for(const [id,cancelled] of cases)put(receipt(id,cancelled));
  const f=responder(task=>cases.find(x=>x[0]===task.task_id)[2]);const result=await inspectHistoricalReceipts({...input(),fetcher:f.fetcher});
  for(const [id,,,expected] of cases){const entry=result.tasks.find(x=>x.task_id===id);assert.equal(entry.status,expected);assert.equal(entry.action,'retain');}
  assert.equal(result.archive_allowed,false);
});

test('HMAC tampering and partial or unknown files stop before any request',async()=>{
  for(const mode of ['mac','flag','partial','unknown','conflict']){
    for(const name of fs.readdirSync(root))fs.unlinkSync(path.join(root,name));
    const file=put(receipt());const envelope=JSON.parse(fs.readFileSync(file));
    if(mode==='mac'){envelope.mac='0'.repeat(64);fs.writeFileSync(file,JSON.stringify(envelope));}
    if(mode==='flag'){envelope.receipt.cancelled=false;fs.writeFileSync(file,JSON.stringify(envelope));}
    if(mode==='partial')fs.writeFileSync(path.join(root,'.pending-'+randomUUID()),'{');
    if(mode==='unknown')fs.writeFileSync(path.join(root,'unknown'),'untrusted');
    if(mode==='conflict')put(receipt('t',false),{pending:true});
    const before=snapshot();let calls=0;
    await assert.rejects(inspectHistoricalReceipts({...input(),fetcher:async()=>{calls++;}}));
    assert.equal(calls,0);assert.deepEqual(snapshot(),before);
  }
});

test('identity, source generation and scan limits are checked',()=>{
  const file=put(receipt());fs.renameSync(file,path.join(root,'0'.repeat(64)+'.json'));
  assert.throws(()=>readHistoricalReceipts(readInput()),{code:'RECEIPT_IDENTITY_MISMATCH'});
  fs.unlinkSync(path.join(root,'0'.repeat(64)+'.json'));put({...receipt(),generation:4});
  assert.throws(()=>readHistoricalReceipts(readInput()),{code:'INVALID_RECEIPT_ENVELOPE'});
  for(const name of fs.readdirSync(root))fs.unlinkSync(path.join(root,name));put(receipt('a'));put(receipt('b'));
  assert.throws(()=>readHistoricalReceipts({...readInput(),maxEntries:1}),{code:'SOURCE_SCAN_LIMIT_EXCEEDED'});
});

test('missing source is not created and unsafe configuration is rejected',async()=>{
  const missing=path.join(directory,'missing');assert.throws(()=>readHistoricalReceipts({...readInput(),directory:missing}),{code:'SOURCE_NOT_FOUND'});assert.equal(fs.existsSync(missing),false);
  for(const extra of [{directory:'relative'},{directory:path.parse(directory).root},{baseURL:'http://user:password@host/'},{baseURL:'file:///secret'},{apiKey:''},{generation:-1},{maxEntries:1025}])assert.throws(()=>readHistoricalReceipts({...readInput(),...extra}));
  await assert.rejects(inspectHistoricalReceipts({...input(),sourceGeneration:7}),{code:'INVALID_CONFIGURATION'});
});

test('symlink namespace is rejected without following it',t=>{
  fs.rmdirSync(root);const outside=fs.mkdtempSync(path.join(os.tmpdir(),'cm-inspect-outside-'));
  try{
    try{fs.symlinkSync(outside,root,process.platform==='win32'?'junction':'dir');}catch(error){if(['EPERM','EACCES'].includes(error.code)){t.skip('symlink creation unavailable');return;}throw error;}
    assert.throws(()=>readHistoricalReceipts(readInput()),{code:'UNSAFE_SOURCE_DIRECTORY'});
    assert.deepEqual(fs.readdirSync(outside),[]);
  }finally{if(fs.existsSync(root))fs.unlinkSync(root);fs.rmSync(outside,{recursive:true,force:true});}
});

test('inspection batches stay within server limits and never call finish',async()=>{
  for(let i=0;i<65;i++)put(receipt('t'+i));
  const before=snapshot(),f=responder();const result=await inspectHistoricalReceipts({...input(),fetcher:f.fetcher});
  assert.deepEqual(f.calls.map(c=>c.tasks.length),[32,32,1]);assert.equal(result.tasks.length,65);assert.deepEqual(snapshot(),before);
});

test('old server and authorization failures retain all files without fallback',async()=>{
  put(receipt());const before=snapshot();
  for(const status of [401,403,404,405,500]){
    let calls=0;await assert.rejects(inspectHistoricalReceipts({...input(),fetcher:async url=>{calls++;assert.ok(url.endsWith('/inspect'));return new Response(sourceKey,{status});}}),error=>!error.message.includes(sourceKey));assert.equal(calls,1);
  }
  assert.deepEqual(snapshot(),before);
});

test('invalid response identity, scope, state or size fails closed',async()=>{
  put(receipt());const before=snapshot();
  const good=()=>({generation:6,read_only:true,receipt_replay_allowed:false,tasks:[{generation:5,session_id:'s',task_id:'t',state:state(true,true,true)}]});
  const cases=[()=>({}),()=>({...good(),generation:5}),()=>({...good(),read_only:false}),()=>({...good(),receipt_replay_allowed:true}),()=>({...good(),tasks:[]}),()=>({...good(),tasks:[{...good().tasks[0],task_id:'other'}]}),()=>({...good(),tasks:[{...good().tasks[0],state:state(false,false,true)}]})];
  for(const value of cases)await assert.rejects(inspectHistoricalReceipts({...input(),fetcher:async()=>Response.json(value())}),{code:'INVALID_SERVER_CONTRACT'});
  await assert.rejects(inspectHistoricalReceipts({...input(),fetcher:async()=>new Response('x'.repeat(32769))}),{code:'INVALID_SERVER_CONTRACT'});
  assert.deepEqual(snapshot(),before);
});

test('timeout covers response body and sanitizes transport errors',{timeout:3000},async()=>{
  put(receipt());
  await assert.rejects(inspectHistoricalReceipts({...input(),timeoutMs:20,fetcher:async(_url,{signal})=>new Response(new ReadableStream({start(controller){signal.addEventListener('abort',()=>controller.error(new Error(currentKey)),{once:true});}}))}),{code:'SERVER_INSPECTION_UNAVAILABLE'});
});

test('CLI takes no credential arguments and help requires no environment',async()=>{
  const output=[];await runReceiptInspectionCLI(['--help'],{},value=>output.push(value));assert.ok(output[0].includes('--source-generation'));
  await assert.rejects(runReceiptInspectionCLI(['--api-key',sourceKey],{}),error=>error.code==='INVALID_ARGUMENTS'&&!error.message.includes(sourceKey));
});

test('reader matches the actual AgentsRuntime receipt writer',{skip:!process.env.OPENCLAW_RECEIPT_STORE_MODULE},async()=>{
  const {createTaskReceiptStore}=await import(pathToFileURL(path.resolve(process.env.OPENCLAW_RECEIPT_STORE_MODULE)).href);
  createTaskReceiptStore(readInput()).put(receipt());const before=snapshot();
  assert.deepEqual(readHistoricalReceipts(readInput()).receipts,[receipt()]);assert.deepEqual(snapshot(),before);
});
