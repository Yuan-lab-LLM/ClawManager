import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {runInNewContext} from 'node:vm';
import test from 'node:test';
import ts from 'typescript';
const require=createRequire(import.meta.url);
function load(name,stubs){const output=ts.transpileModule(readFileSync(new URL('../src/services/'+name,import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,esModuleInterop:true}}).outputText.replaceAll('import.meta.env.VITE_API_URL','undefined');const exports={};runInNewContext(output,{exports,require:n=>stubs[n]??require(n),encodeURIComponent,FormData:class{append(){}}});return exports;}
function client(calls){return Object.fromEntries(['get','post','delete','patch'].map(method=>[method,async(...args)=>{calls.push({method,args});return {data:{data:{generation:7,sessions:[],processes:[]}}};}]))}
test('share workbench uses anonymous scoped endpoints and share CSRF for all mutations',async()=>{
 const owner=[],shared=[];const ownerAPI=client(owner),shareAPI=client(shared);let options;
 const base=load('instanceWorkbenchService.ts',{'./api':{__esModule:true,default:ownerAPI}});
 const api=load('sharedInstanceService.ts',{'axios':{__esModule:true,default:{create:v=>{options=v;return shareAPI}}},'./instanceWorkbenchService':base});
 assert.equal(typeof api.createSharedWorkbenchService,'function','share page needs an authenticated-share workbench transport');
 const svc=api.createSharedWorkbenchService('link/id','csrf-proof');
 await svc.capabilities(999);await svc.git(999,7,{operation:'projects'});await svc.createTerminal(999,7);await svc.output(999,7,'a/b',3);await svc.input(999,7,'session','echo hi\r');await svc.closeTerminal(999,7,'session');await svc.preview(999,7,'http://localhost:8080');await svc.processAction(999,7,'process','stop');
 assert.equal(owner.length,0,'guest must never use owner bearer transport');assert.equal(options.withCredentials,true);
 for(const c of shared){assert(c.args[0].startsWith('/link%2Fid/workbench/'));assert(!c.args[0].includes('999'));
  if(c.method!=='get'){const cfg=c.args[c.args.length-1];assert.equal(cfg.headers['X-ClawManager-Share-CSRF'],'csrf-proof');assert.equal(cfg.params.generation,7);}
 }
 assert(shared.some(c=>c.args[0].includes('/terminals/a%2Fb/output')));
});
test('default workbench transport preserves owner routes',async()=>{
 const calls=[];const api=load('instanceWorkbenchService.ts',{'./api':{__esModule:true,default:client(calls)}}).instanceWorkbenchService;
 await api.preview(42,3,'http://localhost:3000');assert.equal(calls[0].args[0],'/instances/42/workbench/previews');assert.equal(calls[0].args[2].params.generation,3);
});
