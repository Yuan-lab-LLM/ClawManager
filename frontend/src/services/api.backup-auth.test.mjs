import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';
import {runInNewContext} from 'node:vm';
import ts from 'typescript';

function setup() {
  let rejectResponse;
  let refreshCalls=0;
  const values=new Map([['access_token','session-access'],['refresh_token','session-refresh']]);
  const location={href:'/admin/backups'};
  const api=Object.assign(async()=>({data:'retried'}),{interceptors:{request:{use(){}},response:{use(_ok,reject){rejectResponse=reject;}}}});
  const axios={create:()=>api,post:async()=>{refreshCalls++;return {data:{data:{access_token:'renewed-access',refresh_token:'renewed-refresh'}}};}};
  const source=readFileSync(new URL('./api.ts',import.meta.url),'utf8').replace('import.meta.env.VITE_API_URL','undefined');
  const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,esModuleInterop:true}});
  runInNewContext(compiled.outputText,{exports:{},require:(name)=>{assert.equal(name,'axios');return axios;},localStorage:{getItem:k=>values.get(k),setItem:(k,v)=>values.set(k,v),removeItem:k=>values.delete(k)},window:{location}});
  return {reject:e=>rejectResponse(e),refreshCalls:()=>refreshCalls,values,location};
}

test('backup strong-auth challenge does not refresh or destroy the ordinary login',async()=>{
  for(const code of ['strong_auth_required','strong_auth_expired','strong_auth_invalid','strong_auth_invalid_issuer','strong_auth_replayed']){
    const s=setup();const error={response:{status:401,data:{code}},config:{url:'/admin/system-backup/config',headers:{}}};
    await assert.rejects(s.reject(error),e=>e===error);
    assert.equal(s.refreshCalls(),0);assert.equal(s.values.get('access_token'),'session-access');assert.equal(s.location.href,'/admin/backups');
  }
});
test('ordinary expired authentication still refreshes and retries',async()=>{
  const s=setup();const result=await s.reject({response:{status:401,data:{code:'authentication_required'}},config:{url:'/admin/system-backup/config',headers:{}}});
  assert.equal(s.refreshCalls(),1);assert.equal(s.values.get('access_token'),'renewed-access');assert.equal(result.data,'retried');
});
