// Actual Manager frame component, mocked renderer/API: no model calls or cluster mutations.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import http from 'node:http';
import path from 'node:path';
const requireE2E = createRequire(new URL('../../e2e/package.json', import.meta.url));
const { build } = requireE2E('esbuild');
const { chromium } = requireE2E('playwright');
const bundle = await build({ stdin: { resolveDir: path.resolve(import.meta.dirname, '..'), loader: 'tsx', contents: `
import React, {useState} from 'react';
import {createRoot} from 'react-dom/client';
import {QueryClient, QueryClientProvider} from '@tanstack/react-query';
import {I18nProvider} from './src/contexts/I18nContext';
import {HermesLiteServiceFrame} from './src/components/HermesLiteServiceFrame';
import {hermesDesktopService} from './src/services/hermesDesktopService';
import {useAuthStore} from './src/stores/authStore';
localStorage.setItem('clawmanager_locale','en');
useAuthStore.setState({isAuthenticated:true,user:{id:1,username:'one',role:'admin'}});
const descriptor=id=>({available:true,instance_id:id,renderer_url:'/hermes-desktop-web/?instance_id='+id,expires_at:new Date(Date.now()+540000).toISOString()});
hermesDesktopService.probe=async id=>descriptor(id);
hermesDesktopService.bootstrap=async id=>descriptor(id);
hermesDesktopService.recentSession=async id=>'recent-'+useAuthStore.getState().user.id+'-'+id;
function App(){const[open,setOpen]=useState(true);const[id,setID]=useState(229);return <>
<button onClick={()=>setOpen(!open)}>{open?'Leave instance':'Enter instance'}</button>
<button onClick={()=>useAuthStore.setState({user:{id:2,username:'two',role:'admin'}})}>User two</button>
<button onClick={()=>setID(228)}>Other instance</button>
{open&&<HermesLiteServiceFrame instanceId={id} instanceName={'Hermes-'+id} instanceMode='lite' availability='available'/>}</>}
createRoot(document.getElementById('root')).render(<QueryClientProvider client={new QueryClient()}><I18nProvider><App/></I18nProvider></QueryClientProvider>);
` }, bundle: true, write: false, format: 'esm', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"test"', 'import.meta.env': '{}'  } });
const server = http.createServer((request, response) => {
  if (request.url.startsWith('/hermes-desktop-web/')) {
    response.setHeader('Content-Type', 'text/html');
    response.end(`<p id="selected"></p><button onclick="move('#/older-chat')">Older session</button><button onclick="move('#/')">New chat</button><button onclick="move('#/settings')">Settings</button><script>
function render(){document.getElementById('selected').textContent=location.hash||'#/'}
function move(route){history.pushState({},'',route);render()}render();
setTimeout(()=>parent.postMessage({type:'clawmanager:hermes-desktop:ready',instanceId:Number(new URLSearchParams(location.search).get('instance_id'))},location.origin),30);
</script>`);return;
  }
  if (request.url==='/fixture.js') {response.setHeader('Content-Type','text/javascript');response.end(bundle.outputFiles[0].text);return;}
  response.setHeader('Content-Type','text/html');response.end('<div id="root"></div><script type="module" src="/fixture.js"></script>');
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({channel:'chrome',headless:true});
try{
 const page=await browser.newPage();const errors=[];page.on('pageerror',e=>{errors.push(e.message);console.error('page error:', e.message)});
 await page.goto('http://127.0.0.1:'+server.address().port);
 const selected=()=>page.frameLocator('iframe').locator('#selected');
 await selected().getByText('#/recent-1-229',{exact:true}).waitFor();
 await page.frameLocator('iframe').getByText('Older session',{exact:true}).click();
 await page.getByText('Leave instance',{exact:true}).click();await page.getByText('Enter instance',{exact:true}).click();
 await selected().getByText('#/older-chat',{exact:true}).waitFor();
 await page.reload();await selected().getByText('#/older-chat',{exact:true}).waitFor();
 await page.frameLocator('iframe').getByText('Settings',{exact:true}).click();
 await page.getByText('Leave instance',{exact:true}).click();await page.getByText('Enter instance',{exact:true}).click();
 await selected().getByText('#/older-chat',{exact:true}).waitFor();
 await page.frameLocator('iframe').getByText('New chat',{exact:true}).click();
 await page.getByText('Leave instance',{exact:true}).click();await page.getByText('Enter instance',{exact:true}).click();
 await selected().getByText('#/',{exact:true}).waitFor();
 await page.getByText('User two',{exact:true}).click();await selected().getByText('#/recent-2-229',{exact:true}).waitFor();
 await page.getByText('Other instance',{exact:true}).click();await selected().getByText('#/recent-2-228',{exact:true}).waitFor();
 assert.deepEqual(errors,[]);console.log('PASS exact session after exit/reentry and reload, recent-history fallback, new chat, settings, user/instance isolation');
}finally{await browser.close();await new Promise(resolve=>{server.close(resolve);server.closeAllConnections()});}
