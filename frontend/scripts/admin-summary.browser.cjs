const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});const page=await browser.newPage({ignoreHTTPSErrors:true});
 await page.addInitScript(()=>localStorage.setItem('access_token','fixture-only'));
 let summaryCalls=0,listCalls=0;
 await page.route('**/api/**',async route=>{
  const p=new URL(route.request().url()).pathname;let data={};
  if(p.endsWith('/auth/me')||p.endsWith('/users/me'))data={id:1,username:'admin',email:'test@example.com',role:'admin'};
  else if(p.endsWith('/admin/instances/summary')){summaryCalls++;data={total:2501,running:2005,allocated_storage_gb:12505}};
  if(p.endsWith('/admin/instances'))listCalls++;
  if(p.endsWith('/users'))data={users:[],total:1};
  if(p.includes('cluster-resources'))data={nodes:[],node_count:0,ready_nodes:0,cpu:{requested:0,allocatable:0,capacity:0},memory:{requested:0,allocatable:0,capacity:0},disk:{requested:0,allocatable:0,capacity:0}};
  await route.fulfill({json:{success:true,data}});
 });
 await page.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38463')+'/admin');
 await page.getByText('2005',{exact:true}).filter({visible:true}).waitFor({timeout:15000});assert(summaryCalls>=1);assert.equal(listCalls,0);
 await browser.close();console.log('PASS admin running instances displays 2005, no paginated list used');
})().catch(e=>{console.error(e);process.exit(1)});
