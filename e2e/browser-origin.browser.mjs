import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { createRequire } from 'node:module';
import { chromium } from '@playwright/test';
const root = path.resolve(import.meta.dirname, '../frontend');
const require = createRequire(path.join(root, 'package.json'));
const { createServer } = require('vite');
process.chdir(root);
const bridge = await fs.readFile(path.resolve(root, '../backend/internal/services/native_preview_bridge.js'), 'utf8');
const previewBody = '<h1>Isolated preview</h1><button onclick="window.popupResult=window.open(\'https://popup.invalid\')">Popup</button><script>window.stored=localStorage.getItem("private-native-draft");</script>';
const networkCookies=[];
const previewServer=http.createServer((req,res)=>{if(req.url==='/bootstrap')networkCookies.push(req.headers.cookie);res.setHeader('Content-Type','text/html');res.end(previewBody);});
await new Promise(resolve=>previewServer.listen(0,'127.0.0.1',resolve));
const networkOrigin='http://127.0.0.1:'+previewServer.address().port;
const fixture = `
import React from 'react'; import {createRoot} from 'react-dom/client';
import {BrowserRouter,Routes,Route,useNavigate} from 'react-router-dom';
import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {I18nProvider} from '/src/contexts/I18nContext.tsx';
import {AuthProvider} from '/src/contexts/AuthContext.tsx';
import {useAuthStore} from '/src/stores/authStore.ts';
import {WorkbenchBrowser} from '/src/components/workbench/WorkbenchBrowser.tsx';
import {InstanceWorkbench} from '/src/components/workbench/InstanceWorkbench.tsx';
import {initialWorkbench,workbenchReducer} from '/src/components/workbench/state.ts';
import Detail from '/src/pages/instances/InstanceDetailPage.tsx';
import IEI from '/src/pages/instances/IEISystemInstancePage.tsx';
import Shared from '/src/pages/instances/SharedInstancePage.tsx';
import '/src/index.css';
window.documentMarker=crypto.randomUUID();
function DetailRoute(){window.switchInstance=useNavigate();return React.createElement(params.get('surface')==='iei'?IEI:params.get('surface')==='shared'?Shared:Detail);}
useAuthStore.setState({user:{id:7,username:'tester',role:'user'},isAuthenticated:true,isLoading:false,fetchCurrentUser:async()=>{}});
const params = new URLSearchParams(location.search);
const metadata={browser_origin_mode:'chrome-single-host',manager_origin:location.origin};
function LegacyWorkbench(){
 const initial={...initialWorkbench(6),panel:'preview',active:'saved',tabs:[{id:'saved',kind:'browser',title:'Saved preview',address:'http://localhost:3000/draft',url:'https://p-legacy.invalid/bootstrap',stale:false,hidden:false}]};
 const [state,dispatch]=React.useReducer(workbenchReducer,initial);window.legacyState=state;
 return React.createElement(InstanceWorkbench,{instanceId:155,runtimeGeneration:6,isRunning:true,sharedState:state,onWorkbenchEvent:dispatch});
}
const body=params.has('legacy')?React.createElement(LegacyWorkbench):params.has('browser')?React.createElement(WorkbenchBrowser,{
 instanceId:155,generation:6,enabled:true,rendered:true,browserOrigin:metadata,
 tab:{id:'preview',kind:'browser',title:'',hidden:false,stale:false,address:'http://localhost:3000/hello',url:params.has('network')?${JSON.stringify(networkOrigin+'/bootstrap')}:'https://preview-origin.invalid/bootstrap?opaque=fixture',viewer_url:location.origin+'/__clawmanager_browser/view#opaque-fixture',credentialless:true},onNavigate:()=>{}
}):React.createElement(AuthProvider,null,React.createElement(BrowserRouter,null,React.createElement(Routes,null,React.createElement(Route,{path:params.get('surface')==='shared'?'/browser-origin-fixture/:code':'/browser-origin-fixture/:id',element:React.createElement(DetailRoute)}))));
createRoot(document.getElementById('root')).render(React.createElement(QueryClientProvider,{client:new QueryClient({defaultOptions:{queries:{retry:false}}})},React.createElement(I18nProvider,null,body)));
`;
const server = await createServer({ root, server: { port: 0 }, plugins: [{
 name: 'browser-origin-fixture',
 resolveId(id) { if (id === '/browser-origin-fixture.js') return '\0browser-origin-fixture'; },
 load(id) { if (id === '\0browser-origin-fixture') return fixture; },
 configureServer(dev) { dev.middlewares.use(async (req, res, next) => {
  if (!req.url?.startsWith('/browser-origin-fixture/')) return next();
  res.setHeader('Content-Type', 'text/html');
  res.end(await dev.transformIndexHtml('/browser-origin-fixture/', '<html><body><div id="root" style="height:900px"></div><script type="module" src="/browser-origin-fixture.js"></script></body></html>'));
 }); },
}] });
let browser;
try {
 await server.listen();
 const origin = 'http://127.0.0.1:' + server.httpServer.address().port;
 browser = await chromium.launch({channel:'chrome',headless:true});
 for (const unsupported of [false, true]) {
  const context=await browser.newContext();
  let certificateChecks=0, previewRequests=0;
  await context.addInitScript(({unsupported})=>{
   localStorage.setItem('clawmanager_locale','en');
   if(window.top===window)localStorage.setItem('private-native-draft','must-not-be-cleared');
   if(unsupported)delete HTMLIFrameElement.prototype.credentialless;
  },{unsupported});
  await context.route('https://preview-origin.invalid/**',route=>{
   if(route.request().url().includes('__clawmanager_cert')){certificateChecks++;return route.abort('failed');}
   previewRequests++;return route.fulfill({contentType:'text/html',body:previewBody});
  });
  await context.addCookies([{name:'manager-cookie',value:'fixture',domain:'preview-origin.invalid',path:'/',httpOnly:true,secure:true,sameSite:'None'}]);
  const page=await context.newPage();page.setDefaultTimeout(10000);
  if(!unsupported){await page.goto('https://preview-origin.invalid/seed');previewRequests=0;}
  await page.goto(origin+'/browser-origin-fixture/155?browser');
  if(unsupported){
   await page.getByText('This preview requires Chrome with credentialless iframe support.',{exact:true}).waitFor();
   assert.equal(await page.locator('iframe').count(),0);
   assert.equal(await page.getByRole('link',{name:'Open in new window'}).count(),0);
   assert.equal(previewRequests,0);
   console.log('PASS unsupported Chrome fails closed without preview or external link');
  }else{
   const frame=page.locator('.wb-preview-frame');await frame.waitFor();
   assert.equal(await frame.evaluate(el=>el.credentialless),true,'Preview must use a credentialless browsing context');
   const tokens=(await frame.getAttribute('sandbox')).split(' ');
   assert(!tokens.some(token=>token.startsWith('allow-popups')||token.startsWith('allow-top-navigation')));
   await page.frameLocator('.wb-preview-frame').getByRole('heading',{name:'Isolated preview'}).waitFor();
   assert.equal(await page.frameLocator('.wb-preview-frame').locator('body').evaluate(()=>window.stored),null,'Credentialless preview must not see ordinary origin storage');
   await page.frameLocator('.wb-preview-frame').getByRole('button',{name:'Popup'}).click();
   assert.equal(await page.frameLocator('.wb-preview-frame').locator('body').evaluate(()=>window.popupResult),null);
   assert.equal(context.pages().length,1);
   assert.equal(await page.getByRole('textbox').inputValue(),'http://localhost:3000/hello','Address remains the user URL');
   assert.equal(await page.getByRole('link',{name:'Open in new window'}).getAttribute('href'),origin+'/__clawmanager_browser/view#opaque-fixture');
   assert.equal(await page.evaluate(()=>localStorage.getItem('private-native-draft')),'must-not-be-cleared');
   assert.equal(certificateChecks,0,'Same-host mode must not ask for extra preview certificate confirmations');
   console.log('PASS credentialless preview, opaque URL, trusted external viewer, no popup or extra certificate check');
   await context.addCookies([{name:'manager-cookie',value:'fixture',domain:'127.0.0.1',path:'/',httpOnly:true}]);
   await page.goto(networkOrigin+'/seed');
   await page.goto(origin+'/browser-origin-fixture/155?browser&network');
   await page.frameLocator('.wb-preview-frame').getByRole('heading',{name:'Isolated preview'}).waitFor();
   assert.deepEqual(networkCookies,[undefined],'Real preview server must receive no manager HttpOnly cookie');
   assert.equal(await page.frameLocator('.wb-preview-frame').locator('body').evaluate(()=>window.stored),null);
   console.log('PASS actual HTTP preview server receives no manager cookie or ordinary origin storage');
   let legacyRequests=0;
   await context.route('https://p-legacy.invalid/**',route=>{legacyRequests++;return route.abort('failed');});
   await context.route('**/api/v1/**',route=>route.fulfill({json:{success:true,data:{generation:6,workspace:'/workspace',preview:true,browser_origin_mode:'chrome-single-host',manager_origin:origin,entries:[]}}}));
   await page.goto(origin+'/browser-origin-fixture/155?legacy');
   await page.waitForFunction(()=>window.legacyState?.tabs[0]?.stale===true);
   assert.equal(await page.locator('iframe').count(),0);
   assert.equal(legacyRequests,0,'Restored legacy preview must never load or probe its certificate');
   assert.equal(await page.getByRole('textbox',{name:'Service address'}).inputValue(),'http://localhost:3000/draft');
   assert.equal(await page.evaluate(()=>window.legacyState.tabs[0].url),undefined);
   console.log('PASS actual restored workbench expires legacy URL and retains its address without loading or certificate prompts');
  }
  await context.close();
 }
 const context=await browser.newContext({viewport:{width:1440,height:1000}});
 await context.addInitScript(()=>{localStorage.setItem('clawmanager_locale','en');localStorage.setItem('access_token','fixture');});
 let allocations=0,release,documentBoundary=false;
 let managerOrigin=origin;
 const nativeLoads=[];
 await context.route('https://native-origin.invalid/**',route=>{
  if(route.request().url().includes('__clawmanager_cert'))return route.fulfill({status:204,headers:{'Access-Control-Allow-Origin':'*'}});
  nativeLoads.push({url:route.request().url(),documentBoundary});
  return route.fulfill({contentType:'text/html',body:'<script>'+bridge+'</script><textarea aria-label="Native draft"></textarea><a target="_blank" href="http://localhost:3000/hello">Local app</a>'});
 });
 await context.route('https://preview-origin.invalid/**',route=>route.fulfill({contentType:'text/html',body:previewBody}));
 await context.route('**/api/v1/**',async route=>{
  const pathname=new URL(route.request().url()).pathname;
  const id=Number(pathname.match(/\/(?:instances|shared-instances)\/(\d+)/)?.[1]||155);
  const metadata={browser_origin_mode:'chrome-single-host',manager_origin:managerOrigin};
  let data=[];
  if(new RegExp('/instances/'+id+'$').test(pathname))data={instance:{id,user_id:7,name:'Scoped native',type:'opencode',instance_mode:'lite',runtime_type:'gateway',status:'running',runtime_generation:6,workspace_path:'/workspace',created_at:new Date().toISOString()}};
  else if(pathname.includes('/shared-instances/')&&pathname.endsWith('/session'))data={...metadata,instance:{id,name:'Shared scoped native',type:'opencode',status:'running'},access_url:'https://native-origin.invalid/?instance='+id,session_expires_at:new Date(Date.now()+3600000).toISOString(),workspace_access:'none',workspace_available:false,csrf_token:'fixture'};
  else if(pathname.endsWith('/status'))data={instance_status:{instance_id:id,status:'running',availability:'available'}};
  else if(pathname.endsWith('/runtime'))data={commands:[],skills:[]};
  else if(pathname.endsWith('/session-usage'))data={summary:{session_count:0,total_tokens:0,total_prompt_tokens:0,total_completion_tokens:0,total_estimated_cost:0,currency:'USD'},compliance:{fallback_session_count:0,has_fallback_sessions:false,recent_fallback_audit_count:0},items:[],total:0,page:1,limit:20};
  else if(pathname.endsWith('/access'))data={...metadata,access_url:'https://native-origin.invalid/?instance='+id,expires_at:new Date(Date.now()+3600000).toISOString()};
  else if(pathname.endsWith('/capabilities'))data={...metadata,generation:6,workspace:'/workspace',git:true,terminal:true,preview:true};
  else if(pathname.endsWith('/workspace/files'))data={entries:[]};
  else if(pathname.endsWith('/previews')){allocations++;if(allocations===1)await new Promise(resolve=>release=resolve);data={id:'opaque',url:'https://preview-origin.invalid/bootstrap?opaque=fixture',generation:6,viewer_url:origin+'/__clawmanager_browser/view#opaque-fixture',credentialless:true};}
  return route.fulfill({json:{success:true,data}});
 });
 const page=await context.newPage();page.setDefaultTimeout(15000);
 await page.routeWebSocket(/\/api\/v1\/ws(?:\?|$)/,()=>{});
 await page.goto(origin+'/browser-origin-fixture/155');
 const native=page.frameLocator('iframe[title$=" service"]');
 await native.getByRole('textbox',{name:'Native draft'}).fill('unsent draft');
 const nativeElement=await page.locator('iframe[title$=" service"]').elementHandle();
 assert.equal(await nativeElement.evaluate(el=>el.credentialless),false,'Native UI must retain its persistent browser storage');
 for(const width of [390,767,768,1440]){
  await page.setViewportSize({width,height:1000});
  assert(await nativeElement.evaluate(el=>el===document.querySelector('iframe[title$=" service"]')));
  assert.equal(await native.getByRole('textbox',{name:'Native draft'}).inputValue(),'unsent draft');
 }
 await native.getByRole('link',{name:'Local app',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('.wb-program-tab'));
 while(!release)await new Promise(resolve=>setTimeout(resolve,10));
 await native.getByRole('link',{name:'Local app',exact:true}).click();
 assert.equal(allocations,1,'Pending allocation must be reused');release();
 await page.locator('.wb-preview-frame').waitFor();
 assert.equal(await page.locator('.wb-preview-frame').evaluate(el=>el.credentialless),true,'Native-link preview metadata must survive reducer state');
 assert.equal(await page.getByRole('link',{name:'Open in new window'}).getAttribute('href'),origin+'/__clawmanager_browser/view#opaque-fixture');
 await native.getByRole('link',{name:'Local app',exact:true}).click();assert.equal(allocations,1);
 await page.getByRole('textbox',{name:'Service address'}).fill('http://localhost:3000/manual');
 const manualResponse=page.waitForResponse(response=>response.url().includes('/workbench/previews'));
 await page.getByRole('button',{name:'Open',exact:true}).click();
 await manualResponse;
 await page.locator('.wb-loading').waitFor({state:'hidden'});
 await page.waitForFunction(()=>document.querySelector('.wb-address input')?.value==='http://localhost:3000/manual' && document.querySelector('.wb-preview-frame')?.credentialless===true);
 assert.equal(allocations,2,'Manual navigation preserves isolation metadata too');
 assert.equal(await page.getByRole('link',{name:'Open in new window'}).getAttribute('href'),origin+'/__clawmanager_browser/view#opaque-fixture');
 const previousDocument=await page.evaluate(()=>window.documentMarker);
 page.on('request',request=>{if(request.isNavigationRequest()&&request.frame()===page.mainFrame()&&request.url().endsWith('/156'))documentBoundary=true;});
 await page.evaluate(()=>window.switchInstance('/browser-origin-fixture/156'));
 await page.waitForFunction(previous=>window.documentMarker!==previous,previousDocument);
 await page.frameLocator('iframe[title$=" service"]').getByRole('textbox',{name:'Native draft'}).waitFor();
 assert.equal(documentBoundary,true,'Changing instance must cross a full-document boundary');
 assert(nativeLoads.some(load=>load.url.includes('instance=156')));
 assert(nativeLoads.filter(load=>load.url.includes('instance=156')).every(load=>load.documentBoundary),'New native frame cannot load before a document navigation');
 console.log('PASS actual detail: persistent native frame/draft across resize, pending/existing preview reuse, metadata propagation, full-document instance boundary');
 await page.close();
 managerOrigin='https://canonical-manager.invalid';
 await context.route('https://canonical-manager.invalid/**',route=>route.fulfill({contentType:'text/html',body:'<h1>Canonical manager</h1>'}));
 const canonical=await context.newPage();
 await canonical.routeWebSocket(/\/api\/v1\/ws(?:\?|$)/,()=>{});
 const loadsBefore=nativeLoads.length;
 await canonical.goto(origin+'/browser-origin-fixture/155?panel=preview#draft');
 await canonical.getByRole('heading',{name:'Canonical manager'}).waitFor();
 assert.equal(canonical.url(),managerOrigin+'/browser-origin-fixture/155?panel=preview#draft');
 assert.equal(nativeLoads.length,loadsBefore,'Wrong manager origin must redirect before loading a native frame');
 console.log('PASS actual detail canonical manager redirect preserves path/query/hash before native load');
 await canonical.close();
 managerOrigin=origin;
 for(const surface of ['iei','shared']){
  const nativePage=await context.newPage();nativePage.setDefaultTimeout(15000);
  await nativePage.goto(origin+'/browser-origin-fixture/155?surface='+surface);
  await nativePage.frameLocator('iframe[title$=" service"]').getByRole('textbox',{name:'Native draft'}).waitFor();
  assert.equal(await nativePage.locator('iframe').evaluate(el=>el.credentialless),false);
  const oldMarker=await nativePage.evaluate(()=>window.documentMarker);
  await nativePage.evaluate(surface=>window.switchInstance('/browser-origin-fixture/156?surface='+surface),surface);
  await nativePage.waitForFunction(previous=>window.documentMarker!==previous,oldMarker);
  await nativePage.frameLocator('iframe[title$=" service"]').getByRole('textbox',{name:'Native draft'}).waitFor();
  console.log('PASS actual '+surface+' native frame uses shared instance document boundary');
  await nativePage.close();
 }
 await context.close();
}finally{await browser?.close();await server.close();await new Promise(resolve=>previewServer.close(resolve));}
