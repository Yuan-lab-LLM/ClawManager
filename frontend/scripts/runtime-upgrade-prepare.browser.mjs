// Isolated browser fixture: all lifecycle calls are intercepted in memory.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import http from 'node:http';
import path from 'node:path';
import { readFileSync, readdirSync } from 'node:fs';
const requireE2E = createRequire(process.env.CM_BROWSER_TOOL_PACKAGE || new URL('../../e2e/package.json', import.meta.url));
const { build } = requireE2E('esbuild');
const { chromium } = requireE2E('playwright');
const bundle = await build({stdin:{resolveDir:path.resolve(import.meta.dirname,'..'),loader:'tsx',contents:`
import React,{useState} from 'react';import{createRoot}from'react-dom/client';
import {I18nProvider} from './src/contexts/I18nContext';
import Settings from './src/pages/admin/SystemSettingsPage';
import {runtimePoolService as service}from'./src/services/runtimePoolService';
import {systemSettingsService}from'./src/services/systemSettingsService';
import {enterpriseAuthService}from'./src/services/enterpriseAuthService';
import {useAuthStore} from './src/stores/authStore';
window.calls=[];window.mode='pending';window.release=()=>{};
useAuthStore.setState({user:{id:1,role:'admin'}});
systemSettingsService.getImageSettings=async()=>[];
enterpriseAuthService.getConfig=async()=>({});
service.listPods=async()=>[];service.listRollouts=async()=>window.historyItems||[];service.schedulingHistory=async()=>{if(window.historyHold){window.historyHold=false;await new Promise(resolve=>window.releaseHistory=resolve);}return {items:window.historyItems||[]};};
service.prepareRollout=async(type,signal,observer)=>{window.calls.push(['prepare',type]);if(window.mode==='lost-prepare')throw Error('prepare response lost');const id=window.calls.filter(c=>c[0]==='prepare').length===1?'wake-ready':'wake-ready-'+window.calls.filter(c=>c[0]==='prepare').length;const observation={id,kind:'rollout_wake',runtime_type:type,status:window.mode==='protected'?'needs_attention':window.mode==='failed'?'failed':'pending',phase:'verify_sleep_proof',error_message:'proof drift'};observer?.onOperation(id,observation);observer?.onStatus?.(observation);if(window.mode==='protected')throw Error('Preparation #wake-ready: needs_attention: proof drift');if(window.mode==='pending')await new Promise(resolve=>window.release=resolve);if(window.mode==='poll-error')throw Error('Runtime preparation #wake-ready: offline');if(window.mode==='failed')throw Error('Preparation #wake-failed: failed');if(signal?.aborted){await observer?.cancelOperation?.(id);signal.throwIfAborted();}return id;};
service.preflightOpenClawRollout=async request=>{window.calls.push(['preflight',request]);if(window.mode==='lost-preflight')throw Error('preflight response lost');return {passed:true,strategy:'openclaw_8plus_data_safe',target_image_ref:request.target_image_ref,blockers:[],warnings:[],required_capabilities:[],rollout:{id:77,preflight_id:'native-proof',plan_fingerprint:'native-fingerprint'}};};
service.cancelScheduling=async id=>{window.calls.push(['cancel',id]);if(window.cancelHold||window.mode==='cancel-pending')await new Promise(resolve=>window.releaseCancel=resolve);if(window.mode==='cancel-failed')throw Error('cancel unavailable');};
service.startRollout=async request=>{window.calls.push(['rollout',request]);if(window.mode==='lost-rollout')throw Error('rollout response lost');if(window.mode==='expired')throw Object.assign(Error('Preparation #wake-ready expired; prepare again'),{response:{status:409}});if(request.runtime_type==='opencode')throw Object.assign(Error('OpenCode safe upgrade unsupported'),{response:{status:409}});return {id:7,runtime_type:request.runtime_type,status:'pending',phase:'queued'};};
service.getRollout=async id=>({rollout:(window.historyItems||[]).find(item=>item.id===id)||{id:7,status:'pending',runtime_type:'hermes'},items:[],audits:[]});
function App(){const[show,setShow]=useState(true);return <><button onClick={()=>setShow(!show)}>Toggle fixture</button><button onClick={()=>useAuthStore.setState({user:{id:2,role:'admin'}})}>Change account</button><button onClick={()=>useAuthStore.setState({user:{id:1,role:'admin'}})}>Restore account</button>{show&&<Settings/>}</>;}
createRoot(document.getElementById('root')).render(<I18nProvider><App/></I18nProvider>);
`},plugins:[{name:'existing-layout',setup(b){b.onLoad({filter:/\/(AdminLayout|PasswordSettingsSection|RoutingClassifierSettings)\.tsx$/},()=>({loader:'tsx',contents:'export default function Component({children}){return <div>{children}</div>}'}));}}],bundle:true,write:false,format:'iife',jsx:'automatic',define:{'import.meta.env':'{}','process.env.NODE_ENV':'"production"'},logLevel:'silent'});
const assets = new URL('../dist/assets/', import.meta.url);
const stylesheet = readdirSync(assets).filter(name=>name.endsWith('.css')).map(name=>readFileSync(new URL(name,assets),'utf8')).join('\n');
const server=http.createServer((req,res)=>{res.setHeader('Content-Type',req.url==='/app.js'?'text/javascript':'text/html');res.end(req.url==='/app.js'?bundle.outputFiles[0].text:'<style>'+stylesheet+'</style><div id="root"></div><script src="/app.js"></script>');});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({executablePath:process.env.CM_BROWSER_EXECUTABLE || '/opt/google/chrome/chrome',headless:true,args:['--no-sandbox']});
try {
  const page=await browser.newPage();page.setDefaultTimeout(5000);
  await page.route('**/api/**',route=>route.fulfill({json:{data:{}}}));
  const url='http://127.0.0.1:'+server.address().port;
  async function open(type='openclaw',locale='zh') {
    await page.goto(url);await page.evaluate(locale=>{sessionStorage.clear();localStorage.setItem('clawmanager_locale',locale);},locale);await page.reload();await page.locator('[data-upgrade-target-image]').waitFor();
    if(type!=='openclaw')await page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).locator('select').selectOption(type);
    await page.locator('[data-upgrade-target-image]').fill('registry/runtime@sha256:target');
  }
  const upgrade=()=>page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).locator('button').first();
  await open();await upgrade().click();
  await page.waitForFunction(()=>window.calls.some(c=>c[0]==='prepare'));
  assert.equal(await upgrade().isDisabled(),true);
  assert.match(await upgrade().innerText(),/准备|Prepar/);
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['prepare']);
  await page.evaluate(()=>window.release());await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['prepare','preflight']);
  await upgrade().click();await page.waitForFunction(()=>window.calls.some(c=>c[0]==='rollout'));
  let calls=await page.evaluate(()=>window.calls);assert.deepEqual(calls.map(c=>c[0]),['prepare','preflight','rollout']);assert.equal(calls[2][1].preflight_id,'native-proof');assert.equal(calls[2][1].max_unavailable,0);assert.equal(calls[1][1].preparation_id,'wake-ready');assert.equal(calls[2][1].preparation_id,'wake-ready');
  await page.getByText('Toggle fixture',{exact:true}).click();
  assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='cancel').length),0,'consumed reservation is not cancelled');
  for(const change of ['image','batch','unavailable','runtime','account','unmount']) {
    await open();await upgrade().click();await page.waitForFunction(()=>window.calls.some(c=>c[0]==='prepare'));
    if(change==='image')await page.locator('[data-upgrade-target-image]').fill('registry/changed@sha256:other');
    if(change==='batch')await page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).locator('input[type=number]').first().fill('3');
    if(change==='unavailable')await page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).locator('input[type=number]').last().fill('2');
    if(change==='runtime')await page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).locator('select').selectOption('hermes');
    if(change==='account')await page.getByText('Change account',{exact:true}).click();
    if(change==='unmount')await page.getByText('Toggle fixture',{exact:true}).click();
    await page.evaluate(()=>window.release());await page.waitForTimeout(100);
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(c=>c[0]!=='cancel').map(c=>c[0])),['prepare'],change+' stops stale continuation');
  }
  for(const change of ['image','batch','runtime','account','unmount']) {
    await open();await page.evaluate(()=>window.mode='complete');await upgrade().click();
    await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
    if(change==='image')await page.locator('[data-upgrade-target-image]').fill('registry/changed@sha256:other');
    if(change==='batch')await page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).locator('input[type=number]').first().fill('3');
    if(change==='runtime')await page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).locator('select').selectOption('hermes');
    if(change==='account')await page.getByText('Change account',{exact:true}).click();
    if(change==='unmount')await page.getByText('Toggle fixture',{exact:true}).click();
    await page.waitForFunction(()=>window.calls.some(c=>c[0]==='cancel'));
    assert.deepEqual(await page.evaluate(()=>window.calls.filter(c=>c[0]==='cancel')), [['cancel','wake-ready']], change+' releases exact ready reservation once');
  }
  await open();await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  await page.evaluate(()=>window.mode='expired');await upgrade().click();await page.getByText('Preparation #wake-ready expired; prepare again',{exact:true}).waitFor();
  assert.equal(await page.getByText('预检通过，可确认执行',{exact:true}).count(),0,'expired reservation requires a fresh reviewed preflight');
  await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='prepare').length),2,'explicit retry prepares again after expiry');
  await page.evaluate(()=>window.mode='cancel-failed');await page.locator('[data-upgrade-target-image]').fill('registry/changed@sha256:other');
  await page.waitForTimeout(100);assert.equal(await page.getByText('cancel unavailable',{exact:true}).count(),0,'best-effort cancellation does not replace action error');
  await open();await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  await page.evaluate(()=>window.mode='cancel-pending');await page.locator('[data-upgrade-target-image]').fill('registry/changed@sha256:other');await upgrade().click();
  await page.waitForTimeout(100);assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='prepare').length),1,'next prepare waits for previous reservation cleanup');
  await page.evaluate(()=>{window.mode='complete';window.releaseCancel();});await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='prepare').length),2);
  await open('hermes');await page.evaluate(()=>window.mode='failed');await upgrade().click();await page.getByText('Preparation #wake-failed: failed',{exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['prepare']);
  await open('hermes');await page.evaluate(()=>window.mode='protected');await upgrade().click();await page.getByText('Preparation #wake-ready: needs_attention: proof drift',{exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['prepare'],'needs_attention never auto-cancels native proof protection');
  await page.locator('[data-upgrade-target-image]').fill('registry/changed@sha256:other');await page.getByText('Toggle fixture',{exact:true}).click();await page.getByText('Toggle fixture',{exact:true}).click();
  assert.equal(await upgrade().isDisabled(),true,'protected operation remains held across parameter discard and remount');
  assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='cancel').length),0);
  await page.evaluate(()=>window.historyItems=[{id:'foreign',kind:'rollout_wake',runtime_type:'hermes',status:'completed',phase:'ready'}, {id:'wake-ready',kind:'rollout_wake',runtime_type:'hermes',status:'needs_attention',phase:'verify_sleep_proof',error_message:'proof drift'}]);
  await page.getByRole('button',{name:'核对操作记录',exact:true}).click();assert.equal(await page.getByRole('button',{name:'使用此操作',exact:true}).count(),1,'protected history remains scoped to exact known ID');await page.getByRole('button',{name:'使用此操作',exact:true}).click();
  assert.equal(await upgrade().isDisabled(),true,'read-only adoption cannot release a needs_attention hold');assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='cancel').length),0);
  await page.evaluate(()=>{window.historyItems[1].status='completed';window.historyItems[1].phase='ready';});await page.getByRole('button',{name:'核对操作记录',exact:true}).click();await page.getByRole('button',{name:'使用此操作',exact:true}).click();
  assert.equal(await upgrade().isDisabled(),false,'only repaired native ready state can be explicitly adopted');await page.locator('[data-upgrade-target-image]').fill('registry/after-repair@sha256:other');await page.waitForFunction(()=>window.calls.some(c=>c[0]==='cancel'));assert.deepEqual(await page.evaluate(()=>window.calls.filter(c=>c[0]==='cancel')), [['cancel','wake-ready']]);
  await open('hermes');await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.waitForFunction(()=>window.calls.some(c=>c[0]==='rollout'));
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['prepare','rollout']);
  await open('opencode');await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.getByText('OpenCode safe upgrade unsupported',{exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['rollout'],'OpenCode rejection never wakes pools');
  await open();await page.evaluate(()=>{window.mode='poll-error';window.cancelHold=true;});await upgrade().click();await page.getByText('Runtime preparation #wake-ready: offline',{exact:true}).waitFor();
  await upgrade().click();await page.waitForTimeout(100);assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='prepare').length),1,'poll error retains ID and next submission awaits its cancellation');
  await page.evaluate(()=>{window.mode='complete';window.cancelHold=false;window.releaseCancel();});await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls.filter(c=>c[0]==='cancel')), [['cancel','wake-ready']]);
  for(const stage of ['prepare','preflight','rollout']) {
    await open();await page.evaluate(stage=>window.mode='lost-'+stage,stage);
    if(stage==='rollout'){await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();await page.evaluate(()=>window.mode='lost-rollout');}
    await upgrade().click();await page.getByText(stage+' response lost',{exact:true}).waitFor();
    const count=await page.evaluate(()=>window.calls.filter(c=>['prepare','preflight','rollout'].includes(c[0])).length);
    assert.equal(await upgrade().isDisabled(),true,'unknown outcome blocks repeated '+stage+' POST');
    await page.locator('[data-upgrade-target-image]').fill('registry/changed@sha256:other');assert.equal(await upgrade().isDisabled(),true,'editing does not clear uncertainty');
    await page.getByText('Toggle fixture',{exact:true}).click();await page.getByText('Toggle fixture',{exact:true}).click();
    assert.equal(await upgrade().isDisabled(),true,'remount retains uncertainty');
    await page.getByRole('button',{name:'核对操作记录',exact:true}).click();
    assert.equal(await upgrade().isDisabled(),true,'empty history never permits retry');
    assert.equal(await page.evaluate(()=>window.calls.filter(c=>['prepare','preflight','rollout'].includes(c[0])).length),count);
  }
  await open();await page.evaluate(()=>window.mode='lost-prepare');await upgrade().click();await page.getByText('prepare response lost',{exact:true}).waitFor();
  await page.evaluate(()=>window.historyHold=true);await page.getByRole('button',{name:'核对操作记录',exact:true}).click();await page.waitForFunction(()=>Boolean(window.releaseHistory));
  await page.locator('[data-upgrade-target-image]').fill('registry/changed@sha256:other');assert.equal(await page.getByRole('button',{name:'核对操作记录',exact:true}).isDisabled(),false,'discarded history reads release their busy state');await page.evaluate(()=>window.releaseHistory());
  await page.getByText('Change account',{exact:true}).click();await page.waitForFunction(()=>!document.querySelector('[data-upgrade-target-image]').closest('section').querySelector('button').disabled);assert.equal(await upgrade().isDisabled(),false,'another account has an independent guard');
  await page.getByText('Restore account',{exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-upgrade-target-image]').closest('section').querySelector('button').disabled);assert.equal(await upgrade().isDisabled(),true,'original account retains its guard');
  await page.reload();assert.equal(await upgrade().isDisabled(),true,'document reload retains persisted intent');
  assert.deepEqual(await page.evaluate(()=>window.calls),[],'reload performs no mutating lifecycle calls');
  await page.evaluate(()=>window.historyItems=[{id:'recovered-wake',kind:'rollout_wake',runtime_type:'openclaw',status:'completed',phase:'ready',created_at:new Date(Date.now()-15*60*1000).toISOString()}]);
  await page.getByRole('button',{name:'核对操作记录',exact:true}).click();await page.getByRole('button',{name:'使用此操作',exact:true}).click();
  assert.deepEqual(await page.evaluate(()=>window.calls),[],'history adoption sends no POST');
  await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['preflight'],'known ready preparation is adopted without re-POST');
  assert.equal(await page.evaluate(()=>window.calls[0][1].preparation_id),'recovered-wake');
  await open();await page.locator('[data-upgrade-target-image]').fill('registry/runtime:release');await page.evaluate(()=>window.mode='lost-preflight');await upgrade().click();await page.getByText('preflight response lost',{exact:true}).waitFor();
  await page.evaluate(()=>{const intent=JSON.parse(sessionStorage.getItem('cm-runtime-upgrade-intent:1'));window.historyItems=[{id:77,runtime_type:'openclaw',target_image_ref:'registry/runtime@sha256:target',status:'preflight_passed',phase:'preflight',preflight_id:'native-proof',plan_fingerprint:'native-fingerprint',created_at:new Date(Math.floor(Date.parse(intent.submittedAt)/1000)*1000).toISOString()}];intent.submittedAt=new Date(Math.floor(Date.parse(intent.submittedAt)/1000)*1000+250).toISOString();sessionStorage.setItem('cm-runtime-upgrade-intent:1',JSON.stringify(intent));});
  await page.getByRole('button',{name:'核对操作记录',exact:true}).click();await page.getByRole('button',{name:'使用此操作',exact:true}).click();
  await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.waitForFunction(()=>window.calls.some(c=>c[0]==='rollout'));
  assert.deepEqual(await page.evaluate(()=>window.calls.map(c=>c[0])),['prepare','preflight','rollout'],'recovered native preflight is confirmed without new preparation or preflight');
  assert.equal(await page.evaluate(()=>window.calls.at(-1)[1].preflight_id),'native-proof');
  assert.equal(await page.evaluate(()=>window.calls.at(-1)[1].target_image_ref),'registry/runtime@sha256:target','tag intent can adopt the server-pinned digest');
  await open();await page.evaluate(()=>window.mode='complete');await upgrade().click();await page.getByText('预检通过，可确认执行',{exact:true}).waitFor();
  await page.evaluate(()=>window.mode='lost-rollout');await upgrade().click();await page.getByText('rollout response lost',{exact:true}).waitFor();
  await page.evaluate(()=>window.historyItems=[{id:77,runtime_type:'openclaw',target_image_ref:'registry/runtime@sha256:target',status:'preflight_passed',phase:'preflight',preflight_id:'native-proof',plan_fingerprint:'native-fingerprint',created_at:new Date().toISOString()}]);
  await page.getByRole('button',{name:'核对操作记录',exact:true}).click();assert.equal(await page.getByRole('button',{name:'使用此操作',exact:true}).count(),0,'unchanged preflight does not resolve an unknown confirmation');
  await page.evaluate(()=>window.historyItems[0].status='running');await page.getByRole('button',{name:'核对操作记录',exact:true}).click();await page.getByRole('button',{name:'使用此操作',exact:true}).click();
  assert.equal(await page.evaluate(()=>window.calls.filter(c=>['prepare','preflight','rollout'].includes(c[0])).length),3,'adopting running confirmation never resubmits');
  await page.getByText('Toggle fixture',{exact:true}).click();assert.equal(await page.evaluate(()=>window.calls.filter(c=>c[0]==='cancel').length),0,'unknown consumed confirmation is never auto-cancelled');
  for(const locale of ['zh','en','ja','ko','de']) for(const width of [1440,1280,768,390]) {
    await open('openclaw',locale);await page.setViewportSize({width,height:900});await upgrade().click();
    await page.waitForFunction(()=>window.calls.some(c=>c[0]==='prepare'));
    assert.equal(await upgrade().isDisabled(),true);assert.ok(!(await upgrade().innerText()).includes('systemSettingsPage.'));
    if(locale==='zh'||locale==='en')await page.screenshot({path:'/tmp/cm-upgrade-preparing-'+locale+'-'+width+'.png',fullPage:true});
    await page.evaluate(()=>{window.mode='lost-preflight';window.release();});await page.getByText('preflight response lost',{exact:true}).waitFor();
    assert.equal(await upgrade().isDisabled(),true);
    assert.equal(await page.evaluate(()=>document.body.scrollWidth<=window.innerWidth),true,'uncertain state has no page overflow at '+locale+'/'+width);
    if(locale==='zh'||locale==='en')await page.locator('section').filter({has:page.locator('[data-upgrade-target-image]')}).screenshot({path:'/tmp/cm-upgrade-uncertain-'+locale+'-'+width+'.png'});
  }
  console.log('PASS isolated upgrade preparation, two-click confirmation, pending guards, parameter/account/unmount isolation, terminal errors, OpenCode rejection and four viewport renders');
} finally {server.closeAllConnections();await new Promise(resolve=>server.close(resolve));await browser.close();}
