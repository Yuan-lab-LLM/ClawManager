const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});const page=await browser.newPage({ignoreHTTPSErrors:true,viewport:{width:1440,height:900}});
 page.setDefaultTimeout(10000);
 const instance={id:1,name:'workspace-test',type:'hermes',status:'running',owner:'test@example.com',instance_mode:'lite'};
 const entries=[{name:'home',path:'home',is_dir:true,size:0,modified_at:'2026-09-18T10:59:32',downloadable:true},{name:'long-file-name-to-check-layout.txt',path:'test.txt',is_dir:false,size:1234,modified_at:'2026-01-02T03:04:00',previewable:true,downloadable:true}];
 await page.route('**/api/v1/ieisystem/**',async route=>{const p=new URL(route.request().url()).pathname;let data={};if(p.endsWith('/workspace/files'))data={entries};else if(p.endsWith('/instances/1'))data={instance};else if(p.endsWith('/access'))data={access_url:'/fixture-runtime',expires_at:'2030-01-01T00:00:00Z',workspace_available:true,workspace_root:'/config'};await route.fulfill({json:{data}})});
 await page.route('**/fixture-runtime',route=>route.fulfill({contentType:'text/html',body:'Runtime fixture'}));
 await page.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38462')+'/ieisystem/instances/1');await page.getByRole('table').waitFor();
 const container=page.locator('.workspace-files-container');
 for(const width of [500,440,340]){
  console.log('Checking workspace width',width);
  await container.evaluate((e,w)=>{e.style.width=w+'px';e.style.maxWidth='none'},width);
  await page.waitForTimeout(100);
  const stamp=page.locator('time:visible').first();assert.equal(await stamp.innerText(),'09/18 10:59');
  assert.match(await stamp.getAttribute('title'),/2026/);
  await stamp.hover();await page.getByRole('tooltip').filter({hasText:'2026/'}).waitFor();assert.match(await page.getByRole('tooltip').filter({hasText:'2026/'}).innerText(),/2026/);
  await page.mouse.move(5,5);await stamp.focus();await page.getByRole('tooltip').filter({hasText:'2026/'}).waitFor();await page.keyboard.press('Escape');assert.equal(await page.getByRole('tooltip').filter({hasText:'2026/'}).count(),0);
  const clipped=await stamp.evaluate(e=>e.scrollWidth>e.clientWidth);assert(!clipped,'Timestamp clipped at '+width);
  assert(await container.evaluate(e=>e.scrollWidth<=e.clientWidth+1),'Workspace overflow at '+width);
  const actionWidths=await page.locator('.workspace-files-table td:last-child button').evaluateAll(es=>es.map(e=>e.getBoundingClientRect().width));assert(actionWidths.every(w=>w>=31.5),'Workspace action buttons shrunk: '+actionWidths);
  await page.screenshot({path:'.tmp/workspace-time-r6-'+width+'.png',fullPage:true});
 }
 await browser.close();console.log('PASS workspace times: compact, full hover/focus tooltip, 500/440/340 widths, no writes');
})().catch(e=>{console.error(e);process.exit(1)});
