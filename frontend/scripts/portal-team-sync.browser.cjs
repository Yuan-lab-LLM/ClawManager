// Exercises the built portal with local HTTP fixtures; never contacts a tenant.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const http=require('node:http'),fs=require('node:fs'),path=require('node:path');
const dist=process.env.PORTAL_DIST||path.resolve(__dirname,'../dist');
const mime={'.js':'text/javascript','.css':'text/css','.png':'image/png','.svg':'image/svg+xml','.html':'text/html'};
const server=http.createServer((req,res)=>{
 const file=path.resolve(dist,'.'+decodeURIComponent(new URL(req.url,'http://localhost').pathname));
 if(!file.startsWith(dist+path.sep)&&file!==dist){res.writeHead(403).end();return}
 const actual=fs.existsSync(file)&&fs.statSync(file).isFile()?file:path.join(dist,'index.html');
 res.setHeader('Content-Type',mime[path.extname(actual)]||'application/octet-stream');fs.createReadStream(actual).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const base=`http://127.0.0.1:${server.address().port}`;
 const browser=await chromium.launch({channel:'chrome',headless:true});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:900}}),page=await context.newPage();
  page.setDefaultTimeout(8000);
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const owner='owner@example.test',at='2026-11-05T01:00:00Z';
  const instances=[{id:1,owner,name:'OpenClaw workspace',type:'openclaw',runtime_type:'gateway',instance_mode:'lite',status:'running',created_at:at,updated_at:at,workspace_available:true}];
  const members=Array.from({length:6},(_,i)=>({id:i+1,team_id:175,instance_id:i+1,member_key:i?'worker'+i:'leader',name:i?'Worker '+i:'Leader',display_name:i?'Worker '+i:'Leader',role:i?'developer':'leader',runtime_type:i===1?'opencode':'openclaw',instance_mode:'lite',is_leader:i===0,status:'idle',availability:'idle'}));
  const team={id:175,user_id:1,name:'Six member Team',display_name:'Six member Team',status:'running',member_count:6,available_count:6,members,communication_mode:'leader_mediated'};
  const days=Array.from({length:90},(_,i)=>({date:new Date(Date.UTC(2026,7,8+i)).toISOString().slice(0,10),tokens:i*10,prompt_tokens:i*7,completion_tokens:i*3,questions:i%3,sessions:1,observed:true}));
  const growth={as_of:at,xp:140,level:2,level_start:120,next_level:250,started_at:at,tasks:[],ledger:[],days:[],rule:'growth-v4',note:''};
  const dashboard={growth,summary:{questions:14,total_tokens:28000,prompt_tokens:25000,completion_tokens:3000,sessions:3,workday_streak:2},trend:days.slice(-7),last_activity:{1:at},recent_questions:{1:{text:'human prompt',at,checked_at:at}},coverage:{questions_supported:true,partial:true,description:'bounded',ready_histories:1,unavailable_histories:0},visits:{today:'2026-11-05',streak:7,longest:7,total:9,provisional:false,versions:{2026:1},days:days.map(d=>({date:d.date,visited:true,rest:false,name:'工作日',known:true}))},snapshot:{version:1,as_of:at,next_refresh_at:'2030-01-01T00:00:00Z',state:'ready',last_batch_at:at},insights:{days,comparison:{},window:{kind:'day'},as_of:at}};
  let emptyInstances=false,failStatistics=false,expireTeams=false;
  const writes=[];
  await context.route('**/api/**',async r=>{
   const u=new URL(r.request().url()),p=u.pathname,m=r.request().method();let data={};
   if(m!=='GET'&&m!=='HEAD')writes.push(p);
   if(p.includes('/teams')&&expireTeams){await r.fulfill({status:401,json:{error:'IEI system session is invalid or expired'}});return}
   if(p.endsWith('/dashboard')&&failStatistics){await r.fulfill({status:503,json:{error:'temporary'}});return}
   if(p.endsWith('/session')||p.endsWith('/session/refresh'))data={owner,expires_at:'2030-01-01T00:00:00Z'};
   else if(p.endsWith('/instances'))data={instances:emptyInstances?[]:instances,total:emptyInstances?0:1,page:1,limit:100};
   else if(p.endsWith('/dashboard'))data=dashboard;
   else if(p.endsWith('/growth')||p.endsWith('/growth/sync'))data=growth;
   else if(p.endsWith('/team-capabilities'))data={enabled:true,can_create:true,runtime_types:['openclaw','hermes','opencode'],instance_modes:['lite'],leader_runtime_types:['openclaw'],worker_runtime_types:['openclaw','hermes','opencode']};
   else if(p.endsWith('/teams'))data={teams:[team],total:1,page:1,limit:20};
   else if(p.endsWith('/teams/175'))data={team,members,tasks:[],events:[],work_items:[]};
   else if(/templates$/.test(p))data={templates:[]};
   else if(p.endsWith('/reports'))data={kind:'week',start:'2026-11-01',end:'2026-11-08',as_of:at,ongoing:true,counts:{},previous:{},days:[],runtime:[],active_days:0,observed_days:0,instances:1,used_sessions:0,revision:0,saved_revisions:[],note:''};
   else if(p.endsWith('/instances/1'))data={instance:instances[0]};
   else if(p.endsWith('/instances/1/access'))data={access_url:'/fixture-runtime',expires_at:'2030-01-01T00:00:00Z',workspace_available:true,workspace_root:'Workspace'};
   else if(p.endsWith('/workspace/files'))data={entries:[]};
   else if(p.includes('lifecycle-operation')){await r.fulfill({status:404,json:{error:'none'}});return}
   await r.fulfill({json:{data}});
  });
  await page.route('**/fixture-runtime',r=>r.fulfill({contentType:'text/html',body:'<p>Local Runtime fixture</p>'}));
  await page.goto(base+'/ieisystem/list-instances?view=overview');
  await page.getByTestId('metric-streak').getByText('7 天',{exact:true}).waitFor();
  assert.match(await page.locator('.owner-chart-caption').innerText(),/最近汇总批次/);
  for(const [width,height] of [[1920,1000],[1440,900],[1366,768],[390,844]]){
   await page.setViewportSize({width,height});
   for(const name of ['总览','我的成长','周期报告','Team']){
    await page.locator('.owner-bookmarks').getByRole('tab',{name,exact:true}).click();
    if(name==='Team')await page.getByRole('heading',{name:'团队空间',exact:true}).waitFor();
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),`${name} horizontal overflow at ${width}`);
   }
  }
  await page.setViewportSize({width:1440,height:900});
  await page.getByRole('tab',{name:'Team · 1',exact:true}).click();
  const card=page.locator('.ot-team-card');await card.getByRole('button',{name:/Six member Team/}).click();
  await card.locator('.ot-card-members a[target="_blank"]').last().waitFor();
  assert.equal(await card.locator('.ot-card-members a[target="_blank"]').count(),6,'all six member entries must open new tabs');
  const workspace=page.locator('.ot-workspace').first();assert(await workspace.evaluate(e=>e.getBoundingClientRect().bottom<=innerHeight+2),'Team workspace exceeds viewport');
  await page.locator('.owner-bookmarks').getByRole('tab',{name:'总览',exact:true}).click();failStatistics=true;
  await page.getByRole('button',{name:'刷新',exact:true}).click();
  assert.match(await page.getByTestId('metric-questions').innerText(),/14/);
  assert.equal(await page.getByRole('alert').count(),0,'one transient statistics failure should preserve snapshot silently');failStatistics=false;
  emptyInstances=true;await page.reload();await page.locator('.owner-bookmarks').getByRole('tab',{name:'Team',exact:true}).click();await page.getByRole('heading',{name:'团队空间',exact:true}).waitFor();
  assert.equal(await page.getByText('工作空间尚未分配',{exact:true}).count(),0,'Team-only owners must retain the portal');
  await page.goto(base+'/ieisystem/instances/1');
  await page.getByRole('button',{name:'收起文件区',exact:true}).click();
  await page.locator('#iei-instance-workspace').waitFor({state:'hidden'});
  assert.equal(await page.locator('#iei-instance-workspace').isVisible(),false);
  await page.getByRole('button',{name:'展开文件区',exact:true}).click();
  assert.equal(await page.locator('#iei-instance-workspace').isVisible(),true);
  assert.equal(await page.locator('iframe').count(),1,'workspace toggles re-created the Runtime UI');
  await page.goto(base+'/ieisystem/list-instances?view=overview&dashboard_tab=team');
  await page.getByRole('heading',{name:'团队空间',exact:true}).waitFor();
  expireTeams=true;
  // The existing periodic GET may observe expiry before the manual refresh.
  await page.getByRole('button',{name:'刷新团队',exact:true}).click({timeout:2000}).catch(async error=>{
   if(!await page.getByRole('heading',{name:'访问验证失败',exact:true}).isVisible())throw error;
  });
  await page.getByRole('heading',{name:'访问验证失败',exact:true}).waitFor();
  assert.equal(await page.locator('.ot-team-card').count(),0,'expired owner data remained visible');
  assert.deepEqual(errors,[]);
  assert(writes.every(p=>['/visit','/access','/activity','/session/refresh','/growth/sync'].some(suffix=>p.endsWith(suffix))),`presentation changes dispatched or modified business data: ${[...new Set(writes)].join(',')}`);
  await context.close();
  console.log('PASS compiled portal: visits, batch time, four tabs, Team-only owner, six members/new tabs, bounded viewport, transient retention and owner expiry');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>server.close());
