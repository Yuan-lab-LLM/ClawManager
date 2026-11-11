// Local UI regression: all services are stubbed, no real lifecycle mutations.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import http from 'node:http';
import path from 'node:path';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const here = path.dirname(fileURLToPath(import.meta.url));
const requireE2E = createRequire(process.env.CM_BROWSER_TOOL_PACKAGE || path.resolve(here, '../../e2e/package.json'));
const { build } = requireE2E('esbuild');
const { chromium } = requireE2E('playwright');
const bundle = await build({ stdin: {resolveDir:path.resolve(here,'..'),loader:'tsx',contents:`
 import React,{useState} from 'react';import {createRoot} from 'react-dom/client';
 import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
 import {I18nProvider} from './src/contexts/I18nContext';
 import RuntimeRecoveryDialog from './src/pages/admin/RuntimeRecoveryDialog';
 import RuntimeActions from './src/pages/admin/RuntimeActions';
 import {runtimePoolService as service} from './src/services/runtimePoolService';
 const pod={id:9,pod_name:'source-pod',pod_uid:'exact-source-uid',runtime_type:'openclaw',namespace:'local',capacity:8,used_slots:1,slots:[]};
 let generation=3; window.calls=[];window.rejectRecovery=true;
 service.inspectInstanceRecovery=async id=>({instance_id:id,generation,status:'error',recovery:{attempts:3},binding:{state:'error',runtime_pod_id:9},pod,blocker_code:'budget',blocker:'Automatic budget exhausted; inspect source before retry.'});
 service.retryInstanceRecovery=async(id,gen)=>{window.calls.push(['retry',id,gen]);if(window.rejectRecovery)throw Error('Source stop unconfirmed; writer retained');generation++;return {};};
 service.recheckPod=async(id,uid)=>{window.calls.push(['recheck',id,uid]);return pod;};
 service.schedulingHistory=async()=>({items:[{id:'op-1',kind:'scale_in',status:'needs_attention',phase:'resizing',error_message:'Replica write requires observation',items:[]}],supported_modes:[]});
 service.retryOperation=async id=>{window.calls.push(['continue',id]);return {};};
 function App(){const[view,setView]=useState('recovery');return <><button onClick={()=>setView('history')}>Show history</button>{view==='recovery'?<RuntimeRecoveryDialog pods={[pod]} onClose={()=>setView('closed')} onChanged={()=>{}}/>:view==='history'?<RuntimeActions kind='history' pods={[]} onClose={()=>setView('closed')} onSubmitted={()=>{}} onRetry={()=>{}}/>:null}</>}
 createRoot(document.getElementById('root')).render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><I18nProvider><App/></I18nProvider></QueryClientProvider>);
 `},bundle:true,write:false,format:'iife',jsx:'automatic',define:{'import.meta.env':'{}','process.env.NODE_ENV':'"development"'},logLevel:'silent'});
const assets=path.resolve(here,'../dist/assets');
const css=readFileSync(path.join(assets,readdirSync(assets).find(x=>x.startsWith('index-')&&x.endsWith('.css'))),'utf8');
const server=http.createServer((req,res)=>{if(req.url==='/app.js'){res.setHeader('Content-Type','text/javascript');res.end(bundle.outputFiles[0].text);}else if(req.url==='/style.css'){res.setHeader('Content-Type','text/css');res.end(css);}else {res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/style.css"></head><body><div id="root"></div><script src="/app.js"></script></body></html>');}});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({channel:'chrome',headless:true});
try {
 for(const locale of ['en','zh'])for(const width of [1280,390]){
  const context=await browser.newContext({viewport:{width,height:900}});await context.addInitScript(locale=>localStorage.setItem('clawmanager_locale',locale),locale);
  const page=await context.newPage();const errors=[];page.on('pageerror',error=>errors.push(error.message));
  await page.goto('http://127.0.0.1:'+server.address().port);const dialog=page.getByRole('dialog');await dialog.waitFor();
  const label=locale==='en'?{id:'Instance ID',inspect:'Inspect instance',retry:'Retry instance recovery',check:'Recheck physical slots',cont:'Recheck and continue'}:{id:'实例 ID',inspect:'检查实例',retry:'重试实例恢复',check:'重新核验现场槽位',cont:'重新核验并继续'};
  await page.getByRole('spinbutton',{name:label.id}).fill('7');await page.getByRole('button',{name:label.inspect,exact:true}).click();
  await page.getByRole('button',{name:label.retry}).click();await page.getByRole('alert').filter({hasText:'Source stop unconfirmed'}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls),[['retry',7,3]],'uncertain mutation must not retry automatically');
  await page.evaluate(()=>{window.rejectRecovery=false});await page.getByRole('button',{name:label.retry}).click();await page.getByText(/G4/).waitFor();
  await page.getByRole('button',{name:label.check}).click();await page.getByRole('button',{name:label.inspect,exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls),[['retry',7,3],['retry',7,3],['recheck',9,'exact-source-uid']]);
  assert(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth+1),'dialog content must fit mobile width');
  await page.screenshot({path:'/tmp/cm-recovery-'+locale+'-'+width+'.png',fullPage:true});
  await page.getByRole('spinbutton',{name:label.id}).fill('8');assert.equal(await page.getByRole('button',{name:label.retry}).count(),0,'changing ID invalidates inspected authority');
  await page.keyboard.press('Escape');await page.getByRole('button',{name:'Show history'}).click();await page.getByRole('button',{name:label.cont}).click();
  assert.deepEqual((await page.evaluate(()=>window.calls)).at(-1),['continue','op-1']);assert.deepEqual(errors,[]);
  await context.close();console.log('PASS recovery inspect/stale invalidation, uncertain source retained, exact UID/generation, operation continuation, layout',locale,width);
 }
} finally {await browser.close();await new Promise(resolve=>server.close(resolve));}
