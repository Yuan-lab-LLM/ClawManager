import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { chromium } from '../../e2e/node_modules/playwright/index.mjs';
import { createServer } from '../node_modules/vite/dist/node/index.js';
const root = path.resolve(import.meta.dirname, '..');
process.chdir(root);
const artifacts = process.env.GIT_UI_ARTIFACTS || '/tmp/cm-git-ui-browser-artifacts';
fs.mkdirSync(artifacts,{recursive:true});
const fixture = fs.mkdtempSync(path.join(root,'tests/.git-browser-'));
fs.writeFileSync(path.join(fixture,'index.html'),'<div id="root"></div><script type="module" src="./main.tsx"></script>');
fs.writeFileSync(path.join(fixture,'main.tsx'),`import React,{useState} from 'react'; import {createRoot} from 'react-dom/client'; import {QueryClient,QueryClientProvider} from '@tanstack/react-query'; import {I18nProvider} from '/src/contexts/I18nContext'; import {GitProjects} from '/src/components/workbench/GitProjects'; import {initialWorkbench} from '/src/components/workbench/state'; import '/src/index.css'; import '/src/components/workbench/workbench.css'; const client=new QueryClient({defaultOptions:{queries:{retry:false}}});function App(){const[view,setView]=useState(initialWorkbench(3).git);return <QueryClientProvider client={client}><I18nProvider><div className="wb-root" style={{width:'min(900px,100%)',height:'calc(100vh - 32px)',margin:'16px auto'}}><GitProjects instanceId={42} generation={3} enabled visible view={view} onChange={patch=>setView(v=>({...v,...patch}))}/></div></I18nProvider></QueryClientProvider>}createRoot(document.getElementById('root')).render(<App/>);`);
let browser, vite;
const operations=[], errors=[], previews=[];
let configured=false, large=false, rejectImage=false, identityUnsupported=true;
const base = {index_status:' ',worktree_status:'M',staged:false,untracked:false,additions:2,deletions:1,stats_known:true};
const initialChanges = [
 {...base,path:'src/a <b>.ts',index_status:'M',staged:true,staged_additions:4,staged_deletions:1,worktree_additions:2,worktree_deletions:1},
 {...base,path:'assets/photo.png',untracked:true,binary:true,additions:0,deletions:0},
 {...base,path:'data/archive.bin',binary:true,additions:0,deletions:0},
 {...base,path:'docs/new name.md',previous_path:'docs/old name.md',worktree_status:'R'},
 {...base,path:'large.txt',untracked:true,additions:12000,deletions:0},
 {...base,path:'unknown.txt',untracked:true,stats_known:false,additions:0,deletions:0},
];
let changes=structuredClone(initialChanges);
try {
 vite=await createServer({root,server:{port:0,host:'127.0.0.1'},logLevel:'error'}); await vite.listen();
 browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});
 const page=await browser.newPage({viewport:{width:1100,height:1000}});
 page.setDefaultTimeout(10000);
 page.on('pageerror', e=>errors.push(e.message));
 await page.addInitScript(()=>{
  localStorage.setItem('clawmanager_locale','en');
  window.__gitObjectURLs={created:[],revoked:[]};
  const create=URL.createObjectURL.bind(URL), revoke=URL.revokeObjectURL.bind(URL);
  URL.createObjectURL=blob=>{const url=create(blob);window.__gitObjectURLs.created.push(url);return url;};
  URL.revokeObjectURL=url=>{window.__gitObjectURLs.revoked.push(url);revoke(url);};
 });
 await page.route('**/api/v1/instances/42/workbench/git*',async route=>{
  const r=route.request().postDataJSON();operations.push(r);
  let data={};
  if(r.operation==='projects') data={projects:[{name:'Example',path:'repo'}]};
  if(r.operation==='status') data={status:{branch:'main',ahead:0,behind:0,detached:false,changes:large?Array.from({length:1001},(_,i)=>({...base,path:`file${i}`})):changes}};
  if(r.operation==='branches') data={branches:[{name:'main',current:true,remote:false}]};
  if(r.operation==='history') data={history:[]};
  if(r.operation==='diff') data=r.paths[0].match(/\.(png|bin)$/)?{diff:'',binary:true,preview_path:`repo/${r.paths[0]}`}:{diff:'diff --git "a/src/a <b>.ts" "b/src/a <b>.ts"\n--- "a/src/a <b>.ts"\n+++ "b/src/a <b>.ts"\n@@ -8,2 +8,2 @@\n context\n-old\n+new\n\\ No newline at end of file\n',truncated:true};
  if(r.operation==='identity') {if(identityUnsupported){await route.fulfill({status:400,json:{success:false,error:'unsupported operation'}});return;} data={identity:{name:'Existing',email:'existing@example.test',scope:'repository'}};}
  if(r.operation==='set-identity') {configured=true;data={identity:{name:r.name,email:r.email,scope:'repository'}};}
  if(r.operation==='commit'&&!configured){await route.fulfill({status:400,json:{success:false,error:'Configure identity',error_code:'missing_identity'}});return;}
  if(r.operation==='stage')changes=changes.map(c=>r.paths.includes(c.path)?{...c,staged:true,index_status:c.worktree_status.trim()||'A',worktree_status:' ',untracked:false}:c);
  if(r.operation==='unstage')changes=changes.map(c=>r.paths.includes(c.path)?{...c,staged:false,index_status:' ',worktree_status:'M'}:c);
  await route.fulfill({json:{success:true,data}});
 });
 await page.route('**/api/v1/instances/42/workspace/preview*',async route=>{
  previews.push(new URL(route.request().url()).searchParams.get('path'));
  await route.fulfill({contentType:rejectImage?'image/svg+xml':'image/png',body:rejectImage?'<svg xmlns="http://www.w3.org/2000/svg"/>':Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a5xkAAAAASUVORK5CYII=','base64')});
 });
 await page.goto(`${vite.resolvedUrls.local[0]}${path.relative(root,fixture)}/index.html`);
 await page.getByRole('button',{name:'Stage all',exact:true}).waitFor();
 assert.equal(previews.length,0,'images load only on selection');
 assert.match(await page.locator('.wb-change-row').filter({hasText:'large.txt'}).innerText(),/\+12000/);
 assert.doesNotMatch(await page.locator('.wb-change-row').filter({hasText:'unknown.txt'}).innerText(),/\+0/);
 assert.match(await page.locator('.wb-change-group').first().innerText(),/\+4/);
 await page.locator('.wb-file-choice').filter({hasText:'a <b>.ts'}).last().click();
 await page.locator('.wb-diff-line.addition').waitFor();
 assert.equal(await page.locator('.wb-diff-line.addition .wb-diff-number').last().innerText(),'9');
 assert.equal(await page.locator('.wb-diff').evaluate(e=>getComputedStyle(e).fontSize),'13px');
 assert.doesNotMatch(await page.locator('.wb-diff').innerText(),/^diff --git|^index |^--- |^\+\+\+ /m,'file header already identifies the diff');
 assert.match(await page.locator('.wb-diff').innerText(),/No newline at end of file/);
 assert.match(await page.locator('.wb-warning').innerText(),/truncated/);
 assert.equal(await page.locator('.wb-diff-heading b').count(),0,'filename is React text');
 await page.screenshot({path:path.join(artifacts,'git-diff-desktop.png'),fullPage:true});
 await page.locator('.wb-file-choice').filter({hasText:'photo.png'}).click();
 await page.locator('.wb-image-preview img').waitFor();
 await page.waitForFunction(()=>document.querySelector('.wb-image-preview img')?.naturalWidth>0);
 assert.deepEqual(previews,['repo/assets/photo.png']);
 assert.match(await page.locator('.wb-image-preview').innerText(),/Working-copy preview/);
 await page.screenshot({path:path.join(artifacts,'git-image-desktop.png'),fullPage:true});
 await page.locator('.wb-file-choice').filter({hasText:'archive.bin'}).click();
 await page.getByText('Binary file. No supported preview is available.',{exact:true}).waitFor();
 assert.equal(await page.evaluate(()=>window.__gitObjectURLs.created.every(url=>window.__gitObjectURLs.revoked.includes(url))),true,'switching files revokes image URLs');
 rejectImage=true;
 await page.locator('.wb-file-choice').filter({hasText:'photo.png'}).click();
 await page.locator('.wb-image-preview .wb-error').waitFor();
 assert.equal(await page.locator('.wb-image-preview img').count(),0,'SVG MIME never embeds');
 await page.getByRole('textbox',{name:'Commit message',exact:true}).fill('Keep this typed commit message');
 await page.getByRole('button',{name:'Commit identity',exact:true}).click();
 await page.getByRole('textbox',{name:'Name',exact:true}).waitFor();
 await page.getByRole('textbox',{name:'Name',exact:true}).fill('Example Person');
 await page.getByRole('textbox',{name:'Email',exact:true}).fill('person@example.test');
 await page.screenshot({path:path.join(artifacts,'git-identity-desktop.png'),fullPage:true});
 await page.getByRole('button',{name:'Cancel',exact:true}).click();
 await page.getByRole('button',{name:/^Commit \(/}).click();
 await page.getByText('Set your name and email for this repository, then click Commit again.',{exact:true}).last().waitFor();
 await page.getByRole('textbox',{name:'Name',exact:true}).fill('Example Person');
 await page.getByRole('textbox',{name:'Email',exact:true}).fill('person@example.test');
 const commitsBefore=operations.filter(o=>o.operation==='commit').length;
 await page.getByRole('button',{name:'Save identity',exact:true}).click();
 await page.locator('.wb-identity-form').waitFor({state:'hidden'});
 assert.equal(operations.filter(o=>o.operation==='commit').length,commitsBefore,'saving never auto commits');
 assert.equal(await page.getByRole('textbox',{name:'Commit message',exact:true}).inputValue(),'Keep this typed commit message');
 assert.equal(await page.locator('.wb-change-group').first().locator('.wb-change-row').count(),1,'saving preserves staged state');
 assert.deepEqual(operations.find(o=>o.operation==='set-identity'),{operation:'set-identity',path:'repo',name:'Example Person',email:'person@example.test'});
 await page.getByRole('button',{name:/^Commit \(/}).click();
 await page.waitForFunction(()=>document.querySelector('.wb-commit-form textarea')?.value==='');
 assert.equal(operations.filter(o=>o.operation==='commit').at(-1).message,'Keep this typed commit message');
 await page.getByRole('button',{name:'Stage all',exact:true}).click();
 await page.getByRole('button',{name:'Stage all',exact:true}).waitFor({state:'hidden'});
 assert.deepEqual(operations.find(o=>o.operation==='stage').paths,['src/a <b>.ts','assets/photo.png','data/archive.bin','docs/old name.md','docs/new name.md','large.txt','unknown.txt']);
 await page.locator('.wb-file-choice').filter({hasText:'photo.png'}).click();
 await page.getByText('Working-copy preview · may differ from staged content',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Unstage all',exact:true}).click();
 await page.getByRole('button',{name:'Unstage all',exact:true}).waitFor({state:'hidden'});
 assert(operations.find(o=>o.operation==='unstage').paths.includes('docs/old name.md'));
 large=true; await page.getByRole('button',{name:'Refresh',exact:true}).click();
 await page.getByText('This selection exceeds 1,000 paths. Stage or unstage files individually.',{exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'Stage all',exact:true}).isDisabled(),true);
 large=false; await page.getByRole('button',{name:'Refresh',exact:true}).click();
 await page.getByText('This selection exceeds 1,000 paths. Stage or unstage files individually.',{exact:true}).waitFor({state:'hidden'});
 identityUnsupported=false;
 await page.setViewportSize({width:390,height:844});
 await page.getByRole('button',{name:'Commit identity',exact:true}).click();
 await page.getByRole('textbox',{name:'Name',exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('.wb-identity-form input')?.value==='Existing');
 await page.screenshot({path:path.join(artifacts,'git-identity-mobile.png'),fullPage:true});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'mobile has no page overflow');
 assert.deepEqual(errors,[]);
 console.log('PASS Git UI browser: counts, numbered diff, truncation, image MIME/lazy preview, repository identity recovery, draft preservation, explicit rename bulk paths, >1000 guard, mobile');
 console.log(`Screenshots: ${artifacts}`);
} finally {await browser?.close();await vite?.close();fs.rmSync(fixture,{recursive:true,force:true});}
