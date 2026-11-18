import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { chromium } from '../../e2e/node_modules/playwright/index.mjs';
import { createServer } from '../node_modules/vite/dist/node/index.js';
const root=path.resolve(import.meta.dirname,'..');
process.chdir(root);
const fixture=fs.mkdtempSync(path.join(root,'tests/.recovery-browser-'));
fs.writeFileSync(path.join(fixture,'index.html'),'<div id="root"></div><script type="module" src="./main.tsx"></script>');
fs.writeFileSync(path.join(fixture,'main.tsx'),`
import React,{useReducer,useState} from 'react';import {createRoot} from 'react-dom/client';import {QueryClient,QueryClientProvider} from '@tanstack/react-query';import {I18nProvider} from '/src/contexts/I18nContext';import {InstanceWorkbench} from '/src/components/workbench/InstanceWorkbench';import {initialWorkbench,workbenchReducer} from '/src/components/workbench/state';import {instanceWorkbenchService as service} from '/src/services/instanceWorkbenchService';import '/src/index.css';
const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
function App(){const[state,dispatch]=useReducer(workbenchReducer,null,()=>{const s=initialWorkbench(9);s.panel='git';s.git.message='Keep my commit draft';s.git.selected='repo';s.tabs=[{id:'browser',kind:'browser',title:'Browser draft',address:'http://localhost:3000/my-draft',hidden:false,stale:false},{id:'terminal',kind:'terminal',title:'Retained terminal',hidden:true,stale:false,session:{id:'terminal',name:'Retained terminal',generation:9,exited:false,cwd:'',created_at:'now'}}];return s;});const[mounted,setMounted]=useState(true);return <QueryClientProvider client={client}><I18nProvider><button onClick={()=>setMounted(false)}>Unmount workbench</button><button onClick={()=>client.invalidateQueries({queryKey:['workbench-capabilities']})}>Refresh capabilities</button>{['commit','stage','input'].map(action=><button key={action} onClick={()=>void(action==='input'?service.input(118,9,'terminal','echo draft'):service.git(118,9,{operation:action,path:'repo',paths:['draft'],message:state.git.message})).catch(()=>{})}>{action}</button>)}<pre id="state">{JSON.stringify(state)}</pre>{mounted&&<div style={{width:900,height:700}}><InstanceWorkbench instanceId={118} runtimeGeneration={9} isRunning sharedState={state} onWorkbenchEvent={dispatch}/></div>}</I18nProvider></QueryClientProvider>};createRoot(document.getElementById('root')).render(<App/>);
`);
let browser,vite;
const errors=[];
try{
 vite=await createServer({root,server:{port:0,host:'127.0.0.1'},logLevel:'error'});await vite.listen();
 browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});
 async function scenario(code,{recover=true,unmount=false,wait=17000}={}){
  const page=await browser.newPage({viewport:{width:1100,height:1000}});page.setDefaultTimeout(10000);
  page.on('pageerror',e=>errors.push(e.message));await page.addInitScript(()=>localStorage.setItem('clawmanager_locale','en'));
  let calls=0;const mutations=[];const times=[];
  await page.route('**/api/v1/instances/118/**',async route=>{
   const request=route.request();const url=new URL(request.url());
   if(url.pathname.endsWith('/workbench/capabilities')){
    calls++;times.push(Date.now());
    if(calls===1||!recover){await route.fulfill({status:code==='timeout'?504:code==='instance_busy'||code==='stop_unconfirmed'?423:code==='upgrade_required'?501:code==='access_denied'?403:409,json:{success:false,error:'Temporary worker explanation',error_code:code}});return;}
    await route.fulfill({json:{success:true,data:{generation:9,git:true,terminal:true,preview:true,processes:false}}});return;
   }
   if(url.pathname.endsWith('/workbench/git')){
    const body=request.postDataJSON();let data={};
    if(['commit','stage'].includes(body.operation)){mutations.push(body.operation);await route.fulfill({status:423,json:{success:false,error:'Busy',error_code:'instance_busy'}});return;}
    if(body.operation==='projects')data={projects:[{name:'Repo',path:'repo'}]};
    if(body.operation==='status')data={status:{branch:'main',changes:[],ahead:0,behind:0}};
    if(body.operation==='branches')data={branches:[]};if(body.operation==='history')data={history:[]};
    await route.fulfill({json:{success:true,data}});return;
   }
   if(url.pathname.endsWith('/input')){mutations.push('input');await route.fulfill({status:423,json:{success:false,error:'Busy',error_code:'instance_busy'}});return;}
   await route.fulfill({json:{success:true,data:{entries:[],files:[],sessions:[],items:[]}}});
  });
  await page.goto(`${vite.resolvedUrls.local[0]}${path.relative(root,fixture)}/index.html`);
  await page.waitForFunction(()=>document.querySelector('#state'));
  const before=JSON.parse(await page.locator('#state').innerText());
  if(unmount){await page.getByRole('button',{name:'Unmount workbench',exact:true}).click();await page.waitForTimeout(3000);assert.equal(calls,1,'unmount cancels pending retries');}
  else if(recover){
   try{await page.getByRole('textbox',{name:'Commit message',exact:true}).waitFor({timeout:wait});}catch(error){throw new Error(`${code} did not recover automatically; calls=${calls}: ${error.message}`);}
   assert.equal(calls,2,`${code} retries capabilities once before success`);
   assert.equal(await page.getByRole('textbox',{name:'Commit message',exact:true}).inputValue(),'Keep my commit draft');
   assert.deepEqual(JSON.parse(await page.locator('#state').innerText()),before,'same generation preserves all drafts and tabs');
   for(const action of ['commit','stage','input'])await page.getByRole('button',{name:action,exact:true}).click();
   await page.waitForTimeout(2400);assert.deepEqual(mutations,['commit','stage','input'],'mutations are never replayed');
  }else{
   await page.waitForTimeout(wait);assert.equal(calls,['instance_busy','stop_unconfirmed','cancelled','timeout'].includes(code)?4:1,`${code} has bounded or no retries`);
   assert.deepEqual(JSON.parse(await page.locator('#state').innerText()),before,'failure does not reset drafts');
  }
  await page.close();return times;
 }
 await scenario('instance_busy');
 await scenario('cancelled');
 await scenario('instance_busy',{recover:false,unmount:true});
 const times=await scenario('instance_busy',{recover:false});assert(times[3]-times[0]>=13500&&times[3]-times[0]<17000,'backoff covers fourteen seconds');
 for(const code of ['stale_generation','upgrade_required','access_denied','unknown_failure'])await scenario(code,{recover:false,wait:2500});
 assert.deepEqual(errors,[]);console.log('PASS capabilities recovery: busy/cancelled recover; 3 bounded retries at 2/4/8 seconds; unmount cancels; permanent errors stop; drafts/tabs persist; commit/stage/input never replay.');
}finally{await browser?.close();await vite?.close();fs.rmSync(fixture,{recursive:true,force:true});}
