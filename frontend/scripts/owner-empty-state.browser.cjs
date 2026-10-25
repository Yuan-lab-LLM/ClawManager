const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{
 const b=await chromium.launch({channel:'chrome',headless:true}),c=await b.newContext({viewport:{width:1440,height:900}}),p=await c.newPage();
 const errors=[];p.on('pageerror',e=>errors.push(e.message));let statistics=0,visits=0,failed=false;
 await c.route('**/api/**',async r=>{
  const path=new URL(r.request().url()).pathname;
  if(/dashboard|growth|report/.test(path))statistics++;
  if(path.endsWith('/visit')){visits++;await r.fulfill({status:204});return}
  if(path.endsWith('/instances')&&failed){await r.fulfill({status:503,json:{error:'Temporarily unavailable'}});return}
  await r.fulfill({json:{data:path.endsWith('/session')?{owner:'new-owner@example.com',expires_at:'2030-01-01T00:00:00Z'}:{instances:[],total:0}}});
 });
 const base=process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38479';
 for(const suffix of ['','?view=overview&dashboard_tab=growth','?view=instance&selected_instance_id=999']){
  await p.goto(base+'/ieisystem/list-instances'+suffix);await p.getByRole('heading',{name:'工作空间尚未分配'}).waitFor();
  assert.equal(await p.locator('.owner-dashboard').count(),0);assert.equal(await p.getByRole('button',{name:'我的总览',exact:true}).count(),0);
  assert.equal(await p.getByText('查看我的总览与累计用量').count(),0);
 }
 assert.equal(statistics,0);assert.equal(visits,3);failed=true;await p.reload();await p.getByRole('heading',{name:'访问验证失败'}).waitFor();assert.equal(await p.getByRole('heading',{name:'工作空间尚未分配'}).count(),0);
 assert.deepEqual(errors,[]);await b.close();console.log('PASS: zero instances always unassigned, stale URL safe, no analytics reads, list failure not empty');
})().catch(e=>{console.error(e);process.exit(1)});
