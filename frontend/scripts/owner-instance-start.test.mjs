import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {test} from 'node:test';
import ts from 'typescript';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
const require=createRequire(import.meta.url);
function load(file,mocks={}) {
 const source=readFileSync(new URL(file,import.meta.url),'utf8');
 const {outputText}=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,jsx:ts.JsxEmit.ReactJSX}});
 const exports={};new Function('require','exports',outputText)(id=>Object.hasOwn(mocks,id)?mocks[id]:require(id),exports);return exports;
}
const {connectOwnerInstance}=load('../src/lib/ownerInstanceStart.ts');
function instance(status='stopped'){return {id:42,type:'openclaw',name:'saved-instance',status};}
function fixture(states=['creating','running']) {
 let time=0,starts=0,gets=0,accesses=0;
 const observed=[];
 const service={startInstance:async()=>{starts++},getInstance:async()=>instance(states[Math.min(gets++,states.length-1)]),generateAccess:async()=>{accesses++;return {access_url:'/original-instance'}}};
 return {service,run:(initial,requested=true,valid=()=>true)=>connectOwnerInstance(initial,requested,service,i=>observed.push(i.status),valid,async ms=>{time+=ms},()=>time,5000),observed,counts:()=>({starts,gets,accesses})};
}
test('stopped click starts once, waits for readiness and returns the original entry',async()=>{
 const f=fixture();const result=await f.run(instance());assert.equal(result.access_url,'/original-instance');assert.deepEqual(f.counts(),{starts:1,gets:2,accesses:1});
});
test('normal visits and errors never dispatch Start; creating visits only observe',async()=>{
 for(const state of ['stopped','error','deleting']) {const f=fixture();await assert.rejects(f.run(instance(state),state==='error'));assert.equal(f.counts().starts,0);}
 const f=fixture(['running']);await f.run(instance('creating'),false);assert.equal(f.counts().starts,0);
 const ready=fixture();await ready.run(instance('running'));assert.equal(ready.counts().starts,0);
});
test('lost Start response is reconciled without resending; late access readiness is retried',async()=>{
 const f=fixture(['creating','creating','running','running']);let attempts=0,access=0;
 f.service.startInstance=async()=>{attempts++;throw new Error('response lost')};
 f.service.generateAccess=async()=>{if(!access++)throw {response:{status:503}};return {access_url:'/original-instance'}};
 await f.run(instance());assert.equal(attempts,1);assert.equal(access,2);
});
test('explicit Stop, navigation cancellation and timeout never replay lifecycle actions',async()=>{
 const stopped=fixture(['creating','stopped']);await assert.rejects(stopped.run(instance()),/已停止/);assert.equal(stopped.counts().starts,1);
 const waiting=fixture(['creating']);await assert.rejects(waiting.run(instance()),/后台任务会继续/);assert.equal(waiting.counts().starts,1);
 const left=fixture();await assert.rejects(left.run(instance(),true,()=>false));assert.equal(left.counts().starts,0);
});
const {OwnerInstanceCard}=load('../src/components/OwnerInstanceCard.tsx',{
 'react-router-dom':{useLocation:()=>({search:'?view=overview'}),Link:({to,state,children,...props})=>React.createElement('a',{...props,href:to,'data-start':String(state?.startStopped)},children)},
 '../lib/ieiRuntimeCatalog':{ownerRuntimeLogos:{},getIEIRuntimePresentation:type=>({id:type,name:type,category:'runtime',capabilities:[]})},
 '../lib/ownerPortalNavigation':load('../src/lib/ownerPortalNavigation.ts'),
 '../lib/compactDisplayTime':{compactDisplayTime:()=>'-'},'../styles/owner-instance-card.css':{},
});
test('all Runtime cards label only stopped entries as Start, including the menu',()=>{
 for(const type of ['openclaw','hermes','opencode','deepseek-harness','workbuddy'])for(const status of ['stopped','running','error','deleting']){
  const html=renderToStaticMarkup(React.createElement(OwnerInstanceCard,{instance:{...instance(status),type,updated_at:'2026-11-05T00:00:00Z'},selected:false,menuOpen:true,pending:false,status,onMenu(){},onCloseMenu(){},onRestart(){},onReset(){}}));
  if(status==='stopped'){assert.match(html,/启动实例/);assert.match(html,/data-start="true"/);assert.match(html,/aria-label="打开实例工作区 saved-instance"[^>]*data-start="undefined"/)}
  else {assert.doesNotMatch(html,/data-start="true"/);if(status!=='deleting')assert.match(html,/进入实例/)}
 }
});
