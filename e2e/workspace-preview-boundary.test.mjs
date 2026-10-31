import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {chromium} from 'playwright';
import {createServer} from '../frontend/node_modules/vite/dist/node/index.js';
const root=path.resolve('../frontend'),temp=fs.mkdtempSync(path.join(os.tmpdir(),'preview-boundary-'));
let server,browser;
try{
 const fixture=path.join(temp,'fixture.tsx');
 const component='/@fs/'+path.join(root,'src/components/WorkspacePreviewBoundary.tsx').replaceAll('\\','/');
 fs.writeFileSync(fixture,`import React,{useState} from 'react';import{createRoot}from'react-dom/client';import Boundary from ${JSON.stringify(component)};
 function Renderer({fail}){if(fail)throw Error('fixture renderer unavailable');return <article>正常渲染</article>}
 function App(){const[fail,setFail]=useState(false);return <main><textarea aria-label="任务草稿" defaultValue="保留未发送内容"/><button onClick={()=>setFail(true)}>模拟预览异常</button><button>关闭预览</button><Boundary content="# 保留原文件"><Renderer fail={fail}/></Boundary></main>};createRoot(document.getElementById('root')).render(<App/>);`);
 server=await createServer({root,configFile:path.join(root,'vite.config.ts'),server:{host:'127.0.0.1',port:0,fs:{allow:[root,temp]}},plugins:[{name:'preview-test-page',configureServer(s){s.middlewares.use('/__preview_boundary__',async(req,res)=>{res.setHeader('Content-Type','text/html');res.end(await s.transformIndexHtml('/__preview_boundary__',`<html><body><div id="root"></div><script type="module" src="/@fs/${fixture.replaceAll('\\','/')}"></script></body></html>`))})}}]});
 await server.listen();const address=server.httpServer.address();
 browser=await chromium.launch({channel:'chrome',headless:true});const page=await browser.newPage();
 await page.goto(`http://127.0.0.1:${address.port}/__preview_boundary__`);await page.getByText('正常渲染',{exact:true}).waitFor();
 await page.getByRole('button',{name:'模拟预览异常'}).click();await page.getByText('# 保留原文件',{exact:true}).waitFor();
 assert.equal(await page.getByRole('textbox',{name:'任务草稿'}).inputValue(),'保留未发送内容');
 await page.getByRole('button',{name:'关闭预览'}).click();assert.equal(await page.locator('main').count(),1);
 console.log('PASS actual React preview boundary isolates renderer exception and retains original file/draft/controls');
}finally{await browser?.close();await server?.close();fs.rmSync(temp,{recursive:true,force:true})}
