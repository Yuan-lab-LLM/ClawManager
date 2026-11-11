import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {runInNewContext} from 'node:vm';
import test from 'node:test';
import ts from 'typescript';
import axios, { AxiosError } from 'axios';
const require=createRequire(import.meta.url);
function load(name,stubs){const output=ts.transpileModule(readFileSync(new URL('../src/services/'+name,import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,esModuleInterop:true}}).outputText.replaceAll('import.meta.env.VITE_API_URL','undefined');const exports={};runInNewContext(output,{exports,require:n=>stubs[n]??require(n),encodeURIComponent,FormData:class{append(){}}});return exports;}
function client(calls,respond){return axios.create({adapter:async config=>{
 const method=config.method,url=config.url;
 calls.push({method,args:method==='get'||method==='delete'?[url,config]:[url,config.data,config]});
 const data=respond?await respond(config):{generation:7,sessions:[],processes:[]};
 return {data:{data},status:200,statusText:'OK',headers:{},config};
}})}
test('share workbench uses anonymous scoped endpoints and share CSRF for all mutations',async()=>{
 const owner=[],shared=[];const ownerAPI=client(owner),shareAPI=client(shared);let options;
 const base=load('instanceWorkbenchService.ts',{'./api':{__esModule:true,default:ownerAPI}});
 const api=load('sharedInstanceService.ts',{'axios':{__esModule:true,default:{create:v=>{options=v;return shareAPI},isAxiosError:axios.isAxiosError}},'./instanceWorkbenchService':base});
 assert.equal(typeof api.createSharedWorkbenchService,'function','share page needs an authenticated-share workbench transport');
 const svc=api.createSharedWorkbenchService('link/id','csrf-proof');
 await svc.capabilities(999);await svc.git(999,7,{operation:'projects'});await svc.createTerminal(999,7);await svc.output(999,7,'a/b',3);await svc.input(999,7,'session','echo hi\r');await svc.closeTerminal(999,7,'session');await svc.preview(999,7,'http://localhost:8080');await svc.processAction(999,7,'process','stop');
 assert.equal(owner.length,0,'guest must never use owner bearer transport');assert.equal(options.withCredentials,true);
 for(const c of shared){assert(c.args[0].startsWith('/link%2Fid/workbench/'));assert(!c.args[0].includes('999'));
  if(c.method!=='get'){const cfg=c.args[c.args.length-1];assert.equal(cfg.headers['X-ClawManager-Share-CSRF'],'csrf-proof');assert.equal(cfg.params.generation,7);}
 }
 assert(shared.some(c=>c.args[0].includes('/terminals/a%2Fb/output')));
});
test('another tab CSRF rotation renews once and concurrent requests share renewal',async()=>{
 const calls=[];let sessions=0;
 const clientAPI=client(calls,async config=>{
  if(config.url.endsWith('/session')){sessions++;await new Promise(r=>setTimeout(r,5));return {csrf_token:'fresh-csrf'};}
  if(config.headers.get('X-ClawManager-Share-CSRF')!=='fresh-csrf')throw new AxiosError('CSRF',AxiosError.ERR_BAD_REQUEST,config,undefined,{status:403,data:{error_code:'share_csrf_invalid'},config});
  return {projects:[]};
 });
 const base=load('instanceWorkbenchService.ts',{'./api':{__esModule:true,default:client([])}});
 const api=load('sharedInstanceService.ts',{'axios':{__esModule:true,default:{create:()=>clientAPI,isAxiosError:axios.isAxiosError}},'./instanceWorkbenchService':base});
 const svc=api.createSharedWorkbenchService('link/id','old-csrf');
 await Promise.all([svc.git(1,7,{operation:'projects'}),svc.preview(1,7,'http://localhost:3000')]);
 assert.equal(sessions,1);assert.equal(calls.length,5);
 await svc.git(1,7,{operation:'status'});assert.equal(sessions,1);assert.equal(calls.at(-1).args.at(-1).headers.get('X-ClawManager-Share-CSRF'),'fresh-csrf');
});
test('permission denials never replay mutations, and CSRF retries are bounded',async()=>{
 for(const code of ['read_only','share_csrf_invalid']){
  const calls=[];let sessions=0;
  const clientAPI=client(calls,config=>{
   if(config.url.endsWith('/session')){sessions++;return {csrf_token:'fresh'};}
   throw new AxiosError('denied',AxiosError.ERR_BAD_REQUEST,config,undefined,{status:403,data:{error_code:code},config});
  });
  const base=load('instanceWorkbenchService.ts',{'./api':{__esModule:true,default:client([])}});
  const api=load('sharedInstanceService.ts',{'axios':{__esModule:true,default:{create:()=>clientAPI,isAxiosError:axios.isAxiosError}},'./instanceWorkbenchService':base});
  await assert.rejects(api.createSharedWorkbenchService('link','old').git(1,7,{operation:'commit'}));
  assert.equal(sessions,code==='share_csrf_invalid'?1:0);assert.equal(calls.length,code==='share_csrf_invalid'?3:1);
 }
});
test('default workbench transport preserves owner routes',async()=>{
 const calls=[];const api=load('instanceWorkbenchService.ts',{'./api':{__esModule:true,default:client(calls)}}).instanceWorkbenchService;
 await api.preview(42,3,'http://localhost:3000');assert.equal(calls[0].args[0],'/instances/42/workbench/previews');assert.equal(calls[0].args[2].params.generation,3);
});
