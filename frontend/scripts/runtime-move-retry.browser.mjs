// Actual dialog regression with mocked APIs: no cluster lifecycle mutations.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import http from 'node:http';
import path from 'node:path';
const requireE2E = createRequire(process.env.CM_BROWSER_TOOL_PACKAGE || new URL('../../e2e/package.json', import.meta.url));
const { build } = requireE2E('esbuild');
const { chromium } = requireE2E('playwright');
const bundle = await build({stdin:{resolveDir:path.resolve(import.meta.dirname,'..'),loader:'tsx',contents:`
import React,{useState} from 'react';import{createRoot}from'react-dom/client';
import{QueryClient,QueryClientProvider}from'@tanstack/react-query';import{I18nProvider}from'./src/contexts/I18nContext';
import RuntimeActions from'./src/pages/admin/RuntimeActions';import RuntimePodDetails from'./src/pages/admin/RuntimePodDetails';import{runtimePoolService as service}from'./src/services/runtimePoolService';
const source={id:1,pod_uid:'source-uid',pod_name:'source',namespace:'runtime',deployment_name:'hermes',runtime_type:'hermes',image_ref:'native@sha256:abc',image_digest:'sha256:abc',state:'ready',agent_reported:true,bindings_reported:true,capacity:1,used_slots:1,bound_instances:1,slots:[{index:1,ready:true,reconciliation_state:'unknown',instance_id:101,expected_instance_id:101,generation:1,gateway_id:'gw-101-1',instance_state:'stopped',binding_confirmed:false,observation_message:'SQL binding still reserves runtime slot'}]};
const target={...source,id:2,pod_name:'target',pod_uid:'target-uid',used_slots:0,bound_instances:0,slots:[{index:1,ready:true,reconciliation_state:'consistent'}]};
const pending={...target,id:3,pod_name:'pending-release',pod_uid:'pending-uid',reconciliation_state:'releasing',reconciliation_message:'runtime slot quarantine release pending',slots:[{index:1,container_name:'agent-1',ready:true,reconciliation_state:'releasing',binding_confirmed:false,observation_message:'runtime slot quarantine release pending'}]};
const operation={id:'original-failed-move',kind:'migration',status:'failed',items:[{instance_id:101,source_pod_id:1,source_pod_uid:'source-uid',source_generation:1,source_gateway_id:'gw-101-1',target_pod_id:2,state:'failed'}]};
window.calls=[];service.schedulingHistory=async()=>({items:[operation],supported_modes:[]});
service.preflightScheduling=async request=>{window.calls.push(['preflight',request]);return {allowed:false,blockers:['Source identity must be revalidated']};};
service.schedule=async request=>{window.calls.push(['submit',request]);return {};};
function App(){const[inspect,setInspect]=useState(false);const[dialog,setDialog]=useState({kind:'history'});return inspect?<RuntimePodDetails pod={pending} onBack={()=>setInspect(false)} onSchedule={()=>{throw Error('pending release cannot be scheduled')}} onPause={()=>{}} onEvents={()=>{}} busy={false}/>:<RuntimeActions key={dialog.kind} {...dialog} pods={[source,target,pending]} onClose={()=>setInspect(true)} onSubmitted={()=>{}} onRetry={(ids,retryOperationId)=>setDialog({kind:'schedule',initialIds:ids,retryOperationId})}/>;}
createRoot(document.getElementById('root')).render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><I18nProvider><App/></I18nProvider></QueryClientProvider>);
`},bundle:true,write:false,format:'iife',jsx:'automatic',define:{'import.meta.env':'{}','process.env.NODE_ENV':'"production"'},logLevel:'silent'});
const server=http.createServer((req,res)=>{res.setHeader('Content-Type',req.url==='/app.js'?'text/javascript':'text/html');res.end(req.url==='/app.js'?bundle.outputFiles[0].text:'<div id="root"></div><script src="/app.js"></script>');});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=process.env.CM_BROWSER_CDP_URL
  ? await chromium.connectOverCDP(process.env.CM_BROWSER_CDP_URL)
  : await chromium.launch({channel:process.env.CM_BROWSER_CHANNEL || 'chrome',headless:true});
try{
const page=await browser.newPage();page.setDefaultTimeout(5000);await page.addInitScript(()=>localStorage.setItem('clawmanager_locale','en'));
await page.goto('http://127.0.0.1:'+server.address().port);
await page.getByRole('button',{name:'Retry',exact:true}).click();
assert.equal(await page.getByRole('combobox',{name:'Choose a compatible Pod'}).locator('option[value="3"]').count(),0,'pending release is not a target');
await page.getByRole('combobox',{name:'Choose a compatible Pod'}).selectOption('2');
const check=page.getByRole('button',{name:'Validate target',exact:true});await check.click();
await page.getByText('Source identity must be revalidated',{exact:true}).waitFor();
const calls=await page.evaluate(()=>window.calls);assert.equal(calls.length,1);assert.equal(calls[0][0],'preflight');assert.equal(calls[0][1].retry_operation_id,'original-failed-move');assert.deepEqual(calls[0][1].instance_ids,[101]);
assert.equal(await page.getByRole('button',{name:'Confirm scheduling',exact:true}).isDisabled(),true);
await page.getByRole('button',{name:'Close',exact:true}).click();
await page.getByRole('button',{name:'Slot 1 · Pending release',exact:true}).click();
assert.equal(await page.getByText('runtime slot quarantine release pending',{exact:false}).count(),0,'release details use localized product language');
assert.equal(await page.getByRole('checkbox').count()>0,true);
const checkboxes=await page.getByRole('checkbox').all();for(const checkbox of checkboxes)assert.equal(await checkbox.isDisabled(),true);
assert.equal(await page.getByRole('button',{name:'Manual scheduling',exact:true}).count(),0);
await page.screenshot({path:process.env.CM_RUNTIME_RETRY_SCREENSHOT || '/tmp/cm-runtime-release-pending.png',fullPage:true});
console.log('PASS pending release displays separately and offers no lifecycle actions');
console.log('PASS exact stopped SQL reservation retains operation-scoped Retry and denied preflight prevents submission');
}finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await browser.close();}
