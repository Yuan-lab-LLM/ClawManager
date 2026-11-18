import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { chromium } from '@playwright/test';
const root=path.resolve(import.meta.dirname,'../frontend'),require=createRequire(path.join(root,'package.json')),{createServer}=require('vite');
const nativeType=process.env.NATIVE_PREVIEW_AGENT||'opencode';
process.chdir(root);
const bridge=await fs.readFile(path.resolve(root,'../backend/internal/services/native_preview_bridge.js'),'utf8');
const fixture=`
import React from 'react';import{createRoot}from'react-dom/client';
import{attachNativePreviewBridge}from'/src/lib/nativePreviewBridge.ts';
import{WorkbenchBrowser}from'/src/components/workbench/WorkbenchBrowser.tsx';
import{I18nProvider}from'/src/contexts/I18nContext.tsx';import{AuthProvider}from'/src/contexts/AuthContext.tsx';import{useAuthStore}from'/src/stores/authStore.ts';import{MemoryRouter,Routes,Route}from'react-router-dom';import{QueryClient,QueryClientProvider}from'@tanstack/react-query';import Page from'/src/pages/instances/InstanceDetailPage.tsx';import'/src/index.css';
if(location.search.includes('detail')){
 useAuthStore.setState({user:{id:7,username:'tester',role:'user'},isAuthenticated:true,isLoading:false,fetchCurrentUser:async()=>{}});
 createRoot(document.getElementById('root')).render(React.createElement(QueryClientProvider,{client:new QueryClient({defaultOptions:{queries:{retry:false}}})},React.createElement(I18nProvider,null,React.createElement(AuthProvider,null,React.createElement(MemoryRouter,{initialEntries:['/instances/155']},React.createElement(Routes,null,React.createElement(Route,{path:'/instances/:id',element:React.createElement(Page)})))))));
}else if(location.search.includes('certificate'))createRoot(document.getElementById('root')).render(React.createElement(I18nProvider,null,React.createElement(WorkbenchBrowser,{instanceId:155,generation:6,enabled:true,rendered:true,tab:{id:'preview',kind:'browser',title:'',hidden:false,stale:false,address:'http://localhost:3000',url:'https://p-fixture.invalid/'},onNavigate:()=>{}})));
else{
 window.received=[];window.ignored=0;
 let frame,detach;
 window.replaceNative=()=>{
  detach?.();frame?.remove();frame=document.createElement('iframe');frame.title='Native app';frame.src='https://native-fixture.invalid/?generation='+window.received.length;document.getElementById('native').append(frame);
  detach=attachNativePreviewBridge(frame,frame.src,async(address,isCurrent)=>{
   if(!isCurrent())return;window.received.push(address);const result=await(await fetch('/allocate')).json();
   if(!isCurrent()){window.ignored++;return;}
   const preview=document.createElement('iframe');preview.title='Right preview';preview.src=result.url;document.getElementById('right').append(preview);
  });
 };window.replaceNative();
}
`;
const server=await createServer({root,server:{port:0},plugins:[{name:'native-preview-fixture',resolveId(id){if(id==='/native-preview-fixture.js')return'\0native-preview-fixture';},load(id){if(id==='\0native-preview-fixture')return fixture;},configureServer(dev){dev.middlewares.use(async(req,res,next)=>{if(req.url?.split('?')[0]!=='/native-preview-fixture')return next();res.setHeader('Content-Type','text/html');res.end(await dev.transformIndexHtml('/native-preview-fixture','<html><body><div id="root" style="height:700px"></div><div id="native"></div><section id="right"></section><script type="module" src="/native-preview-fixture.js"></script></body></html>'));});}}]});
let browser, releaseDetail;
try{
 await server.listen();const origin='http://127.0.0.1:'+server.httpServer.address().port;browser=await chromium.launch({channel:'chrome',headless:true});
 const context=await browser.newContext();let allocationCount=0,hold=false,release;
 await context.addInitScript(()=>{window.addEventListener('message',event=>{if(event.source===parent&&String(event.data?.type).startsWith('clawmanager:preview-bridge:activate:'))window.previewActivated=true;});});
 await context.route('https://native-fixture.invalid/**',route=>route.fulfill({contentType:'text/html; charset=utf-8',body:'<html><head><script>'+bridge+'</script></head><body><a target="_blank" href="http://localhost:3000/hello?from=chat">Local app</a><a target="_blank" href="http://127.0.0.1:3000（PID">Chat autolink</a><a target="_blank" href="https://external-fixture.invalid/">External site</a><script>if(location.search.includes("generation=0"))setTimeout(()=>parent.postMessage({type:"clawmanager:preview-bridge:open",url:"http://localhost:3000/automatic"},"*"),100)</script></body></html>'}));
 await context.route('https://external-fixture.invalid/**',route=>route.fulfill({body:'External site'}));
 await context.route('**/allocate',async route=>{allocationCount++;if(hold)await new Promise(resolve=>release=resolve);await route.fulfill({json:{url:origin+'/preview-content'}});});
 await context.route('**/preview-content',route=>route.fulfill({contentType:'text/html',body:'<h1>Instance preview</h1>'}));
 const page=await context.newPage();page.setDefaultTimeout(15000);await page.goto(origin+'/native-preview-fixture');await page.frameLocator('iframe[title="Native app"]').getByRole('link',{name:'Local app'}).waitFor();
 await page.frames().find(frame=>new URL(frame.url()).hostname==='native-fixture.invalid').waitForFunction(()=>window.previewActivated);
 console.log('PASS native bridge activation');
 await page.waitForTimeout(250);assert.equal(allocationCount,0,'Startup messages must not create previews');
 await page.frameLocator('iframe[title="Native app"]').getByRole('link',{name:'Local app'}).click();await page.frameLocator('iframe[title="Right preview"]').getByRole('heading',{name:'Instance preview'}).waitFor();
 assert.equal(allocationCount,1);assert.equal(context.pages().length,1,'A loopback anchor must not open a laptop tab');assert.deepEqual(await page.evaluate(()=>window.received),['http://localhost:3000/hello?from=chat']);
 await page.frameLocator('iframe[title="Native app"]').getByRole('link',{name:'Chat autolink',exact:true}).click();
 await page.waitForTimeout(250);
 assert.equal(context.pages().length,1,'A chat autolink containing Chinese punctuation must not open a laptop tab');
 assert.equal(allocationCount,2);assert.equal((await page.evaluate(()=>window.received)).at(-1),'http://127.0.0.1:3000/');
 for (const [href,expected] of [
  ['http://[::1]:3000（进程','http://[::1]:3000/'],
  ['http://0.0.0.0:3000，服务','http://localhost:3000/'],
  ['http://localhost:3000/文章（草稿）?标签=（中文）#节',new URL('http://localhost:3000/文章（草稿）?标签=（中文）#节').href],
 ]) {
  const link=page.frameLocator('iframe[title="Native app"]').getByRole('link',{name:'Chat autolink',exact:true});
  await link.evaluate((node,value)=>node.setAttribute('href',value),href);await link.click();
  await page.waitForFunction(expected=>window.received.at(-1)===expected,expected);
  assert.equal(context.pages().length,1,'Loopback variants must open in the right preview and preserve valid Chinese paths');
 }
 assert.equal(allocationCount,5);
 const externalPromise=context.waitForEvent('page');await page.frameLocator('iframe[title="Native app"]').getByRole('link',{name:'External site'}).click();const external=await externalPromise;await external.waitForLoadState();assert.equal(new URL(external.url()).hostname,'external-fixture.invalid');await external.close();
 hold=true;await page.frameLocator('iframe[title="Native app"]').getByRole('link',{name:'Local app'}).click();await page.waitForFunction(()=>window.received.length===6);await page.evaluate(()=>window.replaceNative());release();await page.waitForFunction(()=>window.ignored===1);assert.equal(await page.locator('iframe[title="Right preview"]').count(),5,'Stale instance/generation response must not open another tab');
 console.log('PASS native anchor: one right preview, no laptop popup, external link unchanged, stale response ignored');await context.close();
 const cert=await browser.newContext();let trusted=false;await cert.addInitScript(()=>{window.openCalls=[];window.open=(...args)=>{window.openCalls.push(args);return null;};});
 await cert.route('https://p-fixture.invalid/**',route=>new URL(route.request().url()).pathname==='/__clawmanager_cert_check'?(trusted?route.fulfill({status:404,headers:{'Access-Control-Allow-Origin':'*'}}):route.abort('failed')):route.fulfill({status:502,contentType:'text/html',body:'Service unavailable'}));
 const preview=await cert.newPage();preview.setDefaultTimeout(15000);const entry=origin+'/native-preview-fixture?certificate';await preview.goto(entry);const confirm=preview.getByRole('button',{name:'Confirm certificate in new tab',exact:true});await confirm.waitFor();assert.equal(preview.url(),entry);assert.equal(await preview.locator('iframe').count(),0);await preview.screenshot({path:'/tmp/cm-native-preview-certificate.png'});await confirm.click();assert.deepEqual(await preview.evaluate(()=>window.openCalls),[['https://p-fixture.invalid/__clawmanager_cert_trust','_blank','noopener,noreferrer']]);trusted=true;await preview.evaluate(()=>window.dispatchEvent(new Event('focus')));await preview.locator('iframe').waitFor();await preview.frameLocator('iframe').getByText('Service unavailable',{exact:true}).waitFor();assert.equal(preview.url(),entry);console.log('PASS workbench preview: certificate gate retains CM, secure tab, focus recovery; HTTP404 probe and app502 do not masquerade as TLS failure');await cert.close();
 const detail=await browser.newContext({viewport:{width:1440,height:1000}});let detailAllocations=0;
 await detail.addCookies([{name:'manager_session',value:'fixture-manager-cookie',url:origin}]);
 await detail.route(/http:\/\/(?:localhost|127\.0\.0\.1):3000\//,()=>assert.fail('Never fall back to the raw loopback URL'));
 await detail.addInitScript(()=>{localStorage.setItem('clawmanager_locale','zh');localStorage.setItem('access_token','fixture-only');});
 await detail.route('https://native-detail.invalid/**',route=>new URL(route.request().url()).pathname==='/__clawmanager_cert_check'?route.fulfill({status:204,headers:{'Access-Control-Allow-Origin':'*'}}):route.fulfill({contentType:'text/html',body:'<script>window.addEventListener("message",event=>{if(event.source===parent)window.activationTag=event.data.type})</script><script>'+bridge+'</script><a target="_blank" href="http://127.0.0.1:3000/hello">Local app</a><a target="_blank" href="http://localhost:3000/other">Other app</a>'}));
 await detail.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;let data=[];
  if(path==='/api/v1/instances/155')data={instance:{id:155,user_id:7,name:'Bridge integration',type:nativeType,instance_mode:'lite',runtime_type:'gateway',status:'running',runtime_generation:6,workspace_path:'/workspace',created_at:new Date().toISOString()}};
  else if(path.endsWith('/status'))data={instance_status:{instance_id:155,status:'running',availability:'available'}};
  else if(path.endsWith('/runtime'))data={commands:[],skills:[]};
  else if(path.endsWith('/session-usage'))data={summary:{session_count:0,total_tokens:0,total_prompt_tokens:0,total_completion_tokens:0,total_estimated_cost:0,currency:'USD'},compliance:{fallback_session_count:0,has_fallback_sessions:false,recent_fallback_audit_count:0},items:[],total:0,page:1,limit:20};
  else if(path.endsWith('/access'))data={access_url:'https://native-detail.invalid/',expires_at:new Date(Date.now()+3600000).toISOString()};
  else if(path.endsWith('/capabilities'))data={generation:6,workspace:'/workspace',git:true,terminal:true,preview:true};
  else if(path.endsWith('/workspace/files'))data={entries:[]};
  else if(path.endsWith('/previews')){detailAllocations++;assert.equal(route.request().method(),'POST');assert.equal(route.request().postDataJSON().url,'http://127.0.0.1:3000/hello');if(detailAllocations===1){await new Promise(resolve=>releaseDetail=resolve);return route.fulfill({status:423,json:{success:false,error_code:'instance_busy',error:'Instance is stopping or unavailable'}});}if(detailAllocations===2)return route.fulfill({status:423,json:{success:false,error_code:'instance_busy',error:'Instance is stopping or unavailable'}});data={id:'preview-'+detailAllocations,url:'https://app-detail.invalid/',generation:6};}
  return route.fulfill({json:{success:true,data}});
 });
 await detail.route('https://app-detail.invalid/**',route=>{assert.equal(route.request().headers().authorization,undefined,'Manager bearer token must not reach the app');assert.equal(route.request().headers().cookie,undefined,'Manager cookies must not reach the app');return route.fulfill({headers:{'Access-Control-Allow-Origin':'*'},contentType:'text/html',body:'<h1>Actual detail preview</h1>'});});
 const actual=await detail.newPage();actual.setDefaultTimeout(15000);await actual.routeWebSocket(/\/api\/v1\/ws(?:\?|$)/,()=>{});await actual.goto(origin+'/native-preview-fixture?detail');
 const native=actual.frameLocator('iframe[title$=" service"]');await native.getByRole('link',{name:'Local app',exact:true}).click();await actual.waitForFunction(()=>document.querySelector('.wb-program-tab'));const allocationDeadline=Date.now()+15000;while(!releaseDetail&&Date.now()<allocationDeadline)await new Promise(resolve=>setTimeout(resolve,10));assert.ok(releaseDetail,"The native click must issue a preview allocation");
 assert.equal(await actual.locator('.wb-program-tab').count(),1);assert.equal(await actual.locator('.wb-address input').inputValue(),'http://127.0.0.1:3000/hello');await actual.locator('.wb-browser').getByRole('status').waitFor();assert.equal(await actual.locator('.wb-preview-frame').count(),0,'Pending allocation must not navigate to the raw loopback address');
 await native.getByRole('link',{name:'Local app',exact:true}).click();await actual.waitForTimeout(250);assert.equal(detailAllocations,1,'Repeated same-URL clicks must reuse a pending capability allocation');releaseDetail();
 const busyReason='实例正在停止或暂不可用，预览尚未打开。请稍后重试。';await actual.locator('.wb-browser').getByRole('alert').getByText(busyReason,{exact:true}).waitFor();assert.equal(await actual.locator('.wb-program-tab').count(),1,'A rejected allocation must retain the browser tab');assert.equal(await actual.locator('.wb-address input').inputValue(),'http://127.0.0.1:3000/hello');assert.equal(await actual.locator('.wb-preview-frame').count(),0,'A busy rejection must not render an app iframe');assert.equal(detail.pages().length,1,'A busy rejection must not open a popup');
 await actual.screenshot({path:'/tmp/cm-native-preview-busy-desktop.png'});await actual.setViewportSize({width:390,height:844});await actual.locator('.wb-root').scrollIntoViewIfNeeded();await actual.screenshot({path:'/tmp/cm-native-preview-busy-mobile.png'});await actual.setViewportSize({width:1440,height:1000});
 await native.getByRole('link',{name:'Local app',exact:true}).click();await actual.waitForTimeout(250);assert.equal(detailAllocations,1,'Repeated failed URL must select its existing tab without automatic mutation retry');
 await actual.locator('.wb-browser').getByRole('button',{name:'重试',exact:true}).click();await actual.locator('.wb-browser').getByRole('alert').getByText(busyReason,{exact:true}).waitFor();assert.equal(detailAllocations,2,'One manual Retry must send exactly one mutation');assert.equal(await actual.locator('.wb-program-tab').count(),1);assert.equal(await actual.locator('.wb-address input').inputValue(),'http://127.0.0.1:3000/hello');
 await actual.locator('.wb-browser').getByRole('button',{name:'重试',exact:true}).click();await actual.frameLocator('.wb-preview-frame').getByRole('heading',{name:'Actual detail preview'}).waitFor();assert.equal(detailAllocations,3,'A subsequent manual Retry can recover');assert.notEqual(new URL(await actual.locator('.wb-preview-frame').getAttribute('src')).origin,origin,'App must retain a separate origin from the manager');assert.equal(await actual.locator('.wb-browser [role="alert"]').count(),0,'Recovery must clear the tab error');assert.equal(detail.pages().length,1);
 await native.getByRole('link',{name:'Local app',exact:true}).click();await actual.waitForTimeout(100);assert.equal(detailAllocations,3,'The existing same-URL preview must be reused');await actual.evaluate(()=>window.oldNativeFrame=document.querySelector('iframe[title$=" service"]'));await actual.locator('.wb-instance-agent').getByRole('button',{name:'刷新',exact:true}).click();await actual.waitForFunction(()=>document.querySelector('iframe[title$=" service"]')!==window.oldNativeFrame);await native.getByRole('link',{name:'Local app',exact:true}).waitFor();await actual.waitForTimeout(200);assert.match(await native.getByRole('link',{name:'Local app',exact:true}).evaluate(()=>window.activationTag)||'',/^clawmanager:preview-bridge:activate:/,'A forced same-URL refresh must activate the replacement frame');await native.getByRole('link',{name:'Local app',exact:true}).click();await actual.waitForTimeout(200);assert.equal(detail.pages().length,1,'A same-URL forced native refresh must reattach the click bridge');assert.equal(detailAllocations,3);
 const wb=actual.locator('.wb-root');for(let i=0;i<31;i++){await wb.getByRole('button',{name:'新建标签',exact:true}).click();await wb.getByRole('button',{name:'浏览器',exact:true}).click();}
 assert.equal(await wb.locator('.wb-program-tab').count(),32);await native.getByRole('link',{name:'Other app',exact:true}).click();await actual.waitForTimeout(250);assert.equal(detailAllocations,3,'Full workbench must not mint a capability that its reducer will reject');await actual.getByText('打开的标签过多，请先关闭浏览器标签或结束终端。',{exact:true}).waitFor();
 await actual.screenshot({path:'/tmp/cm-native-preview-detail-desktop.png'});await actual.setViewportSize({width:390,height:844});await actual.screenshot({path:'/tmp/cm-native-preview-detail-mobile.png'});console.log('PASS actual InstanceDetailPage: pending/423 keep address and tab; localized reason, explicit Retry fails once then recovers; repeated clicks reuse; separate app origin has no manager credentials; forced refresh and tab limit remain guarded');await detail.close();
}finally{releaseDetail?.();await browser?.close();await server.close();}
