// Runs against the real Vite app, Router and Auth. All API traffic is stubbed.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';
const require = createRequire(process.env.WORKSPACE_E2E_PACKAGE || '/root/cm-uiux08-20261111/source-e737ca5040e637f271ea5f394ca35aeac985ab98/e2e/package.json');
const { chromium } = require('playwright');
const server = process.env.WORKSPACE_URL ? null : await createServer({
 root:fileURLToPath(new URL('../../../..', import.meta.url)), server:{host:'127.0.0.1', port:0},
});
const out = process.env.WORKSPACE_SCREENSHOTS || '/root/cm-workspace-20261112/frontend-task/screenshots';
await fs.mkdir(out, {recursive:true});
const states=['busy','idle','sleeping','offline','running','starting','unknown','failed'];
const agents=states.map((state,i)=>({id:i+41,name:`Fixture Agent ${i+1}`,type:'hermes',instance_mode:'lite',status:state==='offline'?'stopped':'running',state,progress:null,cpu_cores:2,memory_gb:4,storage_gb:10,created_at:'2026-11-01T00:00:00Z',updated_at:'2026-11-12T00:00:00Z'}));
const task=(id,status)=>({key:`runtime:${id}`,source:'runtime',id:String(id),instance_id:41,title:`Fixture ${status} task`,agent_name:agents[0].name,status,progress:null,created_at:'2026-11-12T00:00:00Z',updated_at:'2026-11-12T01:00:00Z',finished_at:status==='running'?undefined:'2026-11-12T01:00:00Z',result_excerpt:'Recorded fixture result',error_message:status==='failed'?'Fixture failure diagnostic':undefined});
function payload(range='7d') {
 const coverage=Object.fromEntries(['recent','instances','tasks','usage','costs','capacity','observations','activity'].map(key=>[key,{status:key==='observations'?'partial':'ready'}]));
 return {range,timezone:'Asia/Shanghai',as_of:'2026-11-12T03:00:00Z',window:{start:'2026-11-06T00:00:00Z',end:'2026-11-12T03:00:00Z',previous_start:'2026-10-30T00:00:00Z',previous_end:'2026-11-05T03:00:00Z'},instances:agents,instances_total:8,instances_truncated:false,tasks:[task(1,'failed'),task(2,'succeeded'),task(3,'running')],tasks_truncated:false,state_counts:Object.fromEntries(states.map(state=>[state,1])),usage:{total_tokens:range==='today'?77:1200,prompt_tokens:900,completion_tokens:300,invocation_count:12,previous_tokens:1000,currencies:[{currency:'USD',amount:.000001,previous_amount:null,estimated:true,complete:false,recorded_calls:2},{currency:'private-credit',amount:2,previous_amount:1,estimated:false,complete:true,recorded_calls:10}]},usage_history:[0,100,null,300,0,500,300].map((tokens,i)=>({at:`2026-11-${String(i+6).padStart(2,'0')}T00:00:00Z`,tokens,calls:tokens===null?null:i,costs:{USD:tokens===null?null:.0000001*i,'private-credit':tokens===null?null:i*.1}})),agent_usage:[{instance_id:41,tokens:800,costs:{USD:.000001}},{instance_id:0,tokens:400,costs:{'private-credit':2}}],capacity:{eligible_slots:null,runtimes:[{type:'hermes',available:null}],allocated_cpu:16,allocated_memory_gb:32,allocated_storage_gb:80,max_instances:10,max_cpu:32,max_memory_gb:64,max_storage_gb:100},observations:[null,0,null,1].map((n,i)=>({at:`2026-11-${String(i+8).padStart(2, '0')}T00:00:00Z`,...Object.fromEntries(states.map(state=>[state,n])),total:n===null?null:8,allocated_cpu:n===null?null:16,allocated_memory_gb:n===null?null:32,max_cpu:n===null?null:32,max_memory_gb:n===null?null:64,eligible_slots:null})),activity:Array.from({length:365},(_,i)=>({date:new Date(Date.UTC(2025,10,13+i)).toISOString().slice(0,10),events:i%8===0?3:0,calls:i%8===0?2:0,tasks:i%8===0?1:0,tokens:0,observed:i>200})),coverage};
}
let browser;
try {
 await server?.listen();
 const base=process.env.WORKSPACE_URL || `http://127.0.0.1:${server.httpServer.address().port}`;
 browser=await chromium.launch({executablePath:process.env.WORKSPACE_BROWSER_EXECUTABLE || '/opt/google/chrome/chrome',headless:true,args:['--no-sandbox']});
 const page=await browser.newPage({viewport:{width:1440,height:900}});
 page.setDefaultTimeout(12000);
 const errors=[];const writes=[];let mode='normal';let delay=0;let userID=71;let userRole='admin';let workspaceCalls=0;
 let pendingWorkspaceResponse;
 const taskToggle=page.getByRole('button',{name:'View tasks',exact:true});
 const picker=page.locator('.ws-view-picker').getByRole('combobox');
 const chooseView=async view=>{
  await picker.click();
  const options=page.getByRole('listbox').getByRole('option');
  await (typeof view==='number'?options.nth(view):options.filter({hasText:view})).click();
 };
 const fitsWorkspace=async label=>{
  const issues=await page.locator('main').evaluate(main=>{
   const regions=[main,...main.querySelectorAll('.ws-panel,.ws-resume-list,.ws-recent-list,.ws-heat-frame')];
   const overflow=regions.filter(el=>el.getClientRects().length&&(el.scrollHeight>el.clientHeight+1||el.scrollWidth>el.clientWidth+1))
    .map(el=>`${el.className} ${el.scrollWidth}×${el.scrollHeight}>${el.clientWidth}×${el.clientHeight}`);
   const chart=main.querySelector('.ws-usage-panel .ws-chart');
   const footer=main.querySelector('.ws-usage-panel .ws-operating-footer');
   if(chart?.getClientRects().length && footer) {
    const plot=chart.getBoundingClientRect();
    if(plot.bottom>footer.getBoundingClientRect().top+1) overflow.push('The usage chart overlaps its footer');
    if(plot.height<48) overflow.push(`The usage chart is only ${plot.height}px high`);
   }
   return overflow;
  });
  if (issues.length) await page.screenshot({path:`${out}/workspace-overflow.png`});
  assert.deepEqual(issues,[],label);
 };
 const taskList=page.getByRole('dialog',{name:'View tasks',exact:true});
 const closeTasks=()=>taskList.getByRole('button',{name:'Close',exact:true}).click();
 const currency=page.getByRole('combobox',{name:'Cost currency',exact:true});
 const chooseCurrency=async code=>{
  await currency.click();
  await page.getByRole('option',{name:code,exact:true}).click();
 };
 page.on('pageerror',e=>{errors.push(e.message);console.error(e.message)});
 await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');if(!localStorage.getItem('clawmanager_locale'))localStorage.setItem('clawmanager_locale','en');});
 await page.route('**/api/**',async route=>{
  const url=new URL(route.request().url());
  if(route.request().method()!=='GET') writes.push(url.pathname);
  if(url.pathname.endsWith('/auth/me')) return route.fulfill({json:{data:{id:userID,username:`Fixture user ${userID}`,role:userRole,is_active:true}}});
  if(url.pathname.endsWith('/version')) return route.fulfill({json:{data:{version:'fixture-workspace',commit:'fixture',build_time:'fixture'}}});
  if(url.pathname.endsWith('/instances/summary')) {
   if(mode==='missingInstanceCount') return route.fulfill({status:503,json:{error:'Instance count unavailable'}});
   const total=mode==='empty'||mode==='zeroInstanceQuota'?0:mode==='aggregate'?11:userID===72?1:5;
   return route.fulfill({json:{success:true,data:{total}}});
  }
  if(url.pathname.startsWith('/api/v1/instances/44')) {
   const instance={...agents[3],user_id:71,runtime_type:'gateway',disk_gb:10,gpu_enabled:false,gpu_count:0,os_type:'linux',os_version:'ubuntu',storage_class:'standard',mount_path:'/workspace'};
   const data=url.pathname.endsWith('/44') ? {instance} : url.pathname.endsWith('/status') ? {instance_status:{instance_id:44,status:'stopped',availability:'unavailable',created_at:instance.created_at}}
    : url.pathname.endsWith('/runtime') ? {commands:[],skills:[]} : url.pathname.includes('/skills') ? [] : url.pathname.endsWith('/external-access') ? {external_access:null} : null;
   return data===null ? route.fulfill({status:404,json:{error:'Optional fixture endpoint unavailable'}}) : route.fulfill({json:{success:true,data}});
  }
  if(url.pathname.endsWith('/dashboard/workspace') || url.pathname.endsWith('/dashboard/workspace/usage')) {
   workspaceCalls++;
   const range=url.searchParams.get('range');const value=payload(range); const snapshot=mode;
   value.recent_agents=[agents[3],agents[0],agents[2]].map((a,i)=>({...a,last_used_at:`2026-11-12T0${3-i}:00:00Z`,activity_kind:i===0?'chat':'model'}));value.recent_truncated=false;
   if(userID===72) {value.tasks=[{...task(99,'failed'),title:'Account 72 private task'}];value.instances=[{...agents[0],id:99,name:'Account 72 private Agent'}];value.recent_agents=[{...value.instances[0],last_used_at:'2026-11-12T03:00:00Z',activity_kind:'chat'}];}
   if(pendingWorkspaceResponse) await pendingWorkspaceResponse;
   if(delay) await new Promise(r=>setTimeout(r,delay));
   if(snapshot==='error') return route.fulfill({status:503,json:{error:'unavailable'}});
   if(snapshot==='empty') {value.instances=[];value.instances_total=0;value.recent_agents=[];value.tasks=[];value.agent_usage=[];value.usage.total_tokens=0;value.activity=[];}
   if(snapshot==='partial') {value.instances=agents.slice(0,1);value.instances_truncated=true;value.coverage.instances.status='partial';}
   if(snapshot==='unavailable') {value.recent_agents=[];value.coverage.recent.status='unavailable';value.tasks=[];value.coverage.tasks.status='unavailable';value.coverage.usage.status='unavailable';value.coverage.costs.status='unavailable';value.coverage.activity.status='unavailable';value.usage.total_tokens=null;value.activity=value.activity.map(day=>({...day,observed:false,events:0,calls:0,tasks:0,tokens:0}));}
   if(snapshot==='boundary') {value.window.start='2026-11-05T16:00:00Z';value.window.end='2026-11-11T16:00:00Z';value.coverage.usage.since='2026-11-05T16:00:00Z';}
   if(snapshot==='aggregate') {value.state_counts.busy=300;value.instances_total=307;value.instances_truncated=true;value.capacity.allocated_cpu=48;}
   if(snapshot==='zeroInstanceQuota') value.capacity.max_instances=0;
   if(snapshot==='zeroquota') value.capacity.max_cpu=0;
   if(snapshot==='nolimit') value.capacity.max_cpu=null;
   if(snapshot==='zeroassigned') value.capacity.allocated_cpu=0;
   if(snapshot==='fractionalquota') {value.capacity.allocated_cpu=27;value.capacity.max_cpu=200;}
   if(snapshot==='norecent') value.recent_agents=[];
   if(snapshot==='sparsework') {value.tasks=[];value.recent_agents=value.recent_agents.slice(0,1);}
   if(snapshot==='busy-work') value.tasks.push(task(4,'running'),task(5,'running'));
   if(snapshot==='nocurrency') {value.usage.currencies=[];value.usage_history=value.usage_history.map(point=>({...point,costs:{}}));}
   if(snapshot==='singlecost') value.usage_history=value.usage_history.map((point,index,history)=>({...point,costs:index===history.length-1?{USD:.000001}:{}}));
   if(snapshot==='long') {value.instances=value.instances.map(agent=>({...agent,name:agent.name+' — '+('Very long authorized resource name '.repeat(6))}));value.recent_agents=value.recent_agents.map(a=>({...a,name:a.name+' — '+('Very long authorized resource name '.repeat(6))}));}
   return route.fulfill({json:{data:value}});
  }
  return route.fulfill({json:{success:true,data:[]}});
 });
 await page.goto(`${base}/dashboard`);
 await page.locator('.ws-grid[aria-busy="false"]').waitFor();
 assert.equal(await page.locator('.ws-dashboard > header').count(),0,'Workspace should begin with its overview without a welcome banner');
 assert.equal(await page.locator('.ws-dashboard').getByText('Welcome back,',{exact:true}).count(),0);
 const overviewTop=(await page.locator('.ws-grid').boundingBox()).y;
 assert.equal(overviewTop,(await page.locator('.ws-dashboard').boundingBox()).y,'Removed heading must leave no vertical gap');
 assert.equal(await page.locator('.ws-usage-panel:visible, .ws-capacity-panel:visible, .ws-agents-panel:visible').count(),3,'three independent metric panels must be present simultaneously');
 assert.equal(await page.locator('.ws-capacity-panel .ws-operation-body svg').count(),0,'Resource capacity is displayed as numbers');
 assert.equal(await page.getByText('Eligible slots',{exact:true}).count(),0,'Personal Workspace has no scheduling slot reading');
 const instanceQuota=page.locator('.ws-capacity-values [data-resource=instances]');
 const instanceReading=instanceQuota.locator('strong');
 assert.equal(await instanceQuota.count(),1,'Resource capacity includes the instance quota');
 await instanceReading.filter({hasText:'5 / 10'}).waitFor();
 assert.equal(await instanceQuota.getAttribute('data-quota-state'),'normal');
 const axisY=side=>page.locator(`.ws-usage-panel .ws-chart-axis-${side}`).evaluateAll(ticks=>ticks.map(tick=>Number(tick.getAttribute('y'))));
 assert.deepEqual(await axisY('left'),await axisY('right'),'Both series share a plotting area and grid');
 const stateOverview=page.getByRole('table',{name:'Agent states',exact:true});
 assert.equal(await stateOverview.count(),1,'Agent state counts are displayed as a table');
 assert.deepEqual(await stateOverview.getByRole('columnheader').allTextContents(),['State','Count']);
 assert.deepEqual(await stateOverview.locator('tbody tr').evaluateAll(rows=>rows.map(row=>[...row.cells].map(cell=>cell.dataset.state??null))),
  ['busy','idle','running','sleeping','starting','failed','offline','unknown'].map(state=>[null,state]),'Each row contains one state and its count');
 assert.ok(await stateOverview.getByRole('columnheader',{name:'Count',exact:true}).evaluateAll(headers=>headers.every(header=>header.scrollWidth<=header.clientWidth)),'Quantity headers must be readable without ellipsis');
 const stateRow=stateOverview.getByRole('row').nth(1);
 assert.ok((await stateRow.boundingBox()).height>=36,'Agent state table uses the Fluent dense row height');
 assert.equal(await stateRow.evaluate(row=>getComputedStyle(row.cells[1]).borderInlineStartWidth),'0px','Only horizontal rules separate table cells');
 await page.setViewportSize({width:1280,height:800});
 await page.locator('.ws-dashboard[data-layout="dashboard"]').waitFor();
 const desktopPanels=await page.evaluate(()=>{
  const rect=selector=>document.querySelector(selector).getBoundingClientRect().toJSON();
  return { capacity:rect('.ws-capacity-panel'), trend:rect('.ws-usage-panel'), agents:rect('.ws-agents-panel'), work:rect('.ws-work') };
 });
 assert.ok(desktopPanels.capacity.bottom<=desktopPanels.trend.top && desktopPanels.capacity.height<=112,'Capacity is a compact strip above the trend');
 assert.ok(Math.abs(desktopPanels.trend.top-desktopPanels.agents.top)<1 && Math.abs(desktopPanels.trend.bottom-desktopPanels.agents.bottom)<1,'Trend and status have aligned edges');
 assert.ok(desktopPanels.agents.bottom<=desktopPanels.work.top,'The status table ends above the work area');
 await fitsWorkspace('Workspace fits a 1280×800 viewport without scrolling');
 await page.setViewportSize({width:1440,height:900});
 assert.equal(await page.locator('.ws-recent-list a').first().getAttribute('href'),'/instances/44','Continue working must show recent Agents rather than task rows');
 if(process.env.WORKSPACE_USAGE_REVIEW_ONLY==='1') {
  assert.equal(await page.locator('.ws-costs>span').innerText(),'Recorded subtotal');
  assert.equal(await page.locator('.ws-usage-panel select[aria-label="Chart metric"]').count(),0,'Tokens and cost no longer require metric switching');
  assert.equal(await page.locator('.ws-usage-panel path.ws-chart-line').count(),4,'two smooth series retain their missing-data gaps');
  assert.equal(await page.locator('.ws-usage-panel .ws-chart-axis-right').count()>0,true);
  assert.equal(await page.locator('.ws-grid .ws-coverage').count(),0);
  await taskToggle.click();
  await taskList.locator('.ws-counters').getByRole('button',{name:/In progress/}).click();
  await closeTasks();
  const retained=await page.locator('.ws-resume').elementHandle(),stateTable=await page.locator('.ws-states-table').elementHandle(),heat=await page.locator('.ws-heat').elementHandle();
  const calls=[];page.on('request',r=>{if(new URL(r.url()).pathname.includes('/dashboard/workspace'))calls.push(new URL(r.url()).pathname);});
  delay=500;await page.getByRole('button',{name:'Today',exact:true}).click();
  await page.locator('.ws-usage-panel[aria-busy="true"]').waitFor();
  assert.equal(await page.locator('.ws-grid').getAttribute('aria-busy'),'false');
  assert.equal(await page.locator('.ws-usage-panel [role="status"]').isVisible(),true);
  assert.equal(await page.locator('.ws-resume').evaluate((e,old)=>e===old,retained),true);
  assert.equal(await page.locator('.ws-states-table').evaluate((e,old)=>e===old,stateTable),true);
  assert.equal(await page.locator('.ws-heat').evaluate((e,old)=>e===old,heat),true);
  await page.locator('.ws-usage-panel[aria-busy="false"]').waitFor();
  assert.equal(await page.locator('.ws-usage-panel .ws-metrics>div:first-child>strong').innerText(),'77');
  assert.deepEqual([...new Set(calls)],['/api/v1/dashboard/workspace/usage']);
  await taskToggle.click();
  assert.equal(await taskList.locator('.ws-counters').getByRole('button',{name:/In progress/}).getAttribute('aria-pressed'),'true');
  await closeTasks();
  await page.getByRole('button',{name:'30 days',exact:true}).click();await page.getByRole('button',{name:'7 days',exact:true}).click();
  await page.locator('.ws-usage-panel[aria-busy="false"]').waitFor();assert.equal(await page.getByRole('button',{name:'7 days',exact:true}).getAttribute('aria-pressed'),'true');
  delay=0;mode='error';await page.getByRole('button',{name:'30 days',exact:true}).click();await page.locator('.ws-usage-panel [role="alert"]').waitFor();
  assert.equal(await page.locator('.ws-resume').evaluate((e,old)=>e===old,retained),true);
  mode='normal';await page.locator('.ws-usage-panel').getByRole('button',{name:'Try again',exact:true}).click();await page.locator('.ws-usage-panel [role="alert"]').waitFor({state:'hidden'});
  assert.equal(await page.locator('.ws-usage-panel [role="alert"]').count(),0);
  await page.emulateMedia({reducedMotion:'reduce'});
  assert.equal(await page.locator('.ws-usage-stage').evaluate(e=>getComputedStyle(e).transitionDuration),'0s');
  assert.deepEqual(writes,[]);assert.deepEqual(errors,[]);
  console.log('PASS usage-only query/loading/error/rapid switching, retained dashboard DOM/task filter, dual axes/smooth gap-preserving series and reduced motion');
 } else if(process.env.WORKSPACE_REVIEW_ONLY==='1') {
  const failures=[];
  const verify=async(name,run)=>{try{mode='normal';delay=0;await page.goto(`${base}/dashboard`);await page.locator('.ws-grid[aria-busy="false"]').waitFor();await run();console.log('PASS '+name);}catch(error){failures.push(`${name}: ${error.message}`);console.error('FAIL '+name+': '+error.message);}finally{delay=0;}};
  await verify('four resource readings and focused period control survive loading and another range choice',async()=>{
   assert.equal(await page.locator('.ws-capacity-values [data-resource]').count(),4);delay=300;
   await page.getByRole('button',{name:'Today',exact:true}).click();await page.waitForTimeout(50);
   assert.equal(await page.getByRole('button',{name:'30 days',exact:true}).isVisible(),true,'range controls remain usable while loading');
   await page.getByRole('button',{name:'30 days',exact:true}).click();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
   assert.equal(await page.locator('.ws-capacity-values [data-resource]').count(),4);assert.equal(await page.locator('.ws-resource-controls').count(),0);
   assert.equal(await page.getByRole('button',{name:'30 days',exact:true}).evaluate(el=>el===document.activeElement),true);
  });
  await verify('Agent state aggregate and recents stay independent of token periods',async()=>{
   const recent=await page.locator('.ws-recent-list').innerText();await page.getByRole('button',{name:'Today',exact:true}).click();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
   assert.equal(await page.locator('.ws-state-summary strong').innerText(),'3 / 8');assert.equal(await page.locator('.ws-states-table td[data-state]').count(),8);
   assert.deepEqual(await page.locator('.ws-states-table td[data-state]').allTextContents(),Array(8).fill('1'));assert.equal(await page.locator('.ws-recent-list').innerText(),recent);
  });
  await verify('running tasks do not displace failed and new results from the work preview',async()=>{
   mode='busy-work';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
   assert.equal(await page.locator('.ws-resume-list button').count(),3);
   for(const status of ['running','failed','succeeded']) {
    assert.equal(await page.locator('.ws-resume-list').getByRole('button',{name:new RegExp(`Fixture ${status} task`)}).isVisible(),true);
   }
  });
  await verify('currency selection survives period changes',async()=>{
   await chooseCurrency('USD');await page.getByRole('button',{name:'30 days',exact:true}).click();await page.locator('.ws-usage-panel[aria-busy="false"]').waitFor();assert.equal(await currency.innerText(),'USD');
  });
  await verify('independent details retain state and quota observations and restore focus',async()=>{
   for(const panel of ['.ws-capacity-panel','.ws-agents-panel']) {
    const trigger=page.locator(panel).getByRole('button',{name:'Details',exact:true});await trigger.click();
    const dialog=page.getByRole('dialog');assert.equal(await dialog.locator('.ws-observation-details .ws-chart').count(),1);
    await dialog.getByRole('button',{name:'Close',exact:true}).click();assert.equal(await trigger.evaluate(el=>el===document.activeElement),true);
   }
  });
  await verify('state table uses full totals and capacity numbers show oversubscription, unknown ratios and zero',async()=>{
   mode='aggregate';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
   assert.equal(await page.locator('.ws-state-summary strong').innerText(),'302 / 307');assert.equal(await page.locator('[data-resource=cpu] .ws-capacity-percent').textContent(),'150%');assert.equal(await page.locator('[data-resource=cpu] .ws-capacity-quota').textContent(),'48 / 32');
   for(const edge of ['zeroquota','nolimit','zeroassigned']) {mode=edge;await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();assert.equal(await page.locator('[data-resource=cpu] .ws-capacity-percent').textContent(),edge==='zeroassigned'?'0%':'—');}
  });
  await verify('instance quota uses the account inventory total and handles zero and unavailable counts',async()=>{
   for (const [snapshot,display,status] of [
    ['aggregate','11 / 10','quotaOver'], ['empty','0 / 10','normal'],
    ['zeroInstanceQuota','0 / 0','quotaReached'], ['missingInstanceCount','— / 10','unknown'],
   ]) {
    mode=snapshot;await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
    await instanceReading.filter({hasText:display}).waitFor();
    assert.equal(await instanceQuota.getAttribute('data-quota-state'),status);
    await page.locator('.ws-capacity-panel').getByRole('button',{name:'Details',exact:true}).click();
    const dialog=page.getByRole('dialog',{name:'Capacity',exact:true});
    await dialog.getByRole('row',{name:`Instances ${display}`,exact:true}).waitFor();
    await dialog.getByRole('button',{name:'Close',exact:true}).click();
   }
  });
  await verify('personal quota colors distinguish normal, near limit, and over limit for a regular user',async()=>{
   userRole='user';mode='aggregate';
   try {
    await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
    const readings=page.locator('.ws-capacity-values .ws-capacity-percent');
    assert.deepEqual(await readings.allTextContents(),['150%','50%','80%']);
    assert.equal((await readings.evaluateAll(nodes=>new Set(nodes.map(node=>getComputedStyle(node).color)).size)),3);
    await page.locator('.ws-capacity-panel').getByRole('button',{name:'Details',exact:true}).click();
    const dialog=page.getByRole('dialog',{name:'Capacity',exact:true});
    assert.equal(await dialog.getByText(/eligible slots/i).count(),0);
    await dialog.getByRole('combobox',{name:'Chart metric'}).click();
    assert.deepEqual(await page.getByRole('listbox').getByRole('option').allTextContents(),['Assigned CPU','Assigned memory']);
   } finally { userRole='admin'; }
  });
  await verify('fractional capacity percentages fit in one line on desktop and mobile',async()=>{
   mode='fractionalquota';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
   const reading=page.locator('[data-resource=cpu] .ws-capacity-percent');
   for (const width of [1440,1280,390,320]) {
    await page.setViewportSize({width,height:900});
    await page.locator(`.ws-dashboard[data-layout="${width >= 1280 ? 'dashboard' : 'groups'}"]`).waitFor();
    await reading.waitFor();
    assert.equal(await reading.innerText(),'13.5%');
    assert.equal(await reading.evaluate(element=>{
     const range=document.createRange();
     range.selectNodeContents(element);
     const lines=new Set([...range.getClientRects()].map(rect=>rect.top));
     return lines.size===1 && range.getBoundingClientRect().width<=element.clientWidth;
    }),true,`percentage must fit at ${width}px`);
   }
   await page.setViewportSize({width:1440,height:900});
  });
  await verify('empty recent history never substitutes heartbeat or task results',async()=>{
   mode='norecent';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();assert.equal(await page.locator('.ws-recent-list a').getAttribute('href'),'/instances');assert.match(await page.locator('.ws-recent-list').innerText(),/No recently used Agents/);await taskToggle.click();assert.match(await page.locator('.ws-counters').innerText(),/Failed\s+1/);
  });
  await verify('sparse work uses compact rows without dropping pinned Agents',async()=>{
   mode='sparsework';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
   for(const [width,height] of [[1440,900],[1280,800],[1366,768]]) {
    await page.setViewportSize({width,height});
    await page.locator('.ws-work').waitFor();
    assert.equal(await page.locator('.ws-pins .ws-pin a:visible').count(),6);
    assert.ok((await page.locator('.ws-resume').boundingBox()).height<210);
    await fitsWorkspace(`Compact work fits at ${width}×${height}`);
   }
   await page.setViewportSize({width:1440,height:900});
  });
  await verify('selected currency survives a period with no recorded prices',async()=>{
   await chooseCurrency('USD');mode='nocurrency';await page.getByRole('button',{name:'Today',exact:true}).click();await page.locator('.ws-usage-panel[aria-busy="false"]').waitFor();assert.match(await page.locator('.ws-chart-cost-label').innerText(),/USD/);assert.equal(await page.locator('.ws-chart-cost circle').count(),0);
  });
  await verify('one reported cost stays a single point aligned to its date',async()=>{
   mode='singlecost';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
   const chart=page.locator('.ws-usage-panel .ws-chart');
   const cost=chart.locator('.ws-chart-cost circle');
   const token=chart.locator('.ws-chart-token circle').last();
   assert.equal(await cost.count(),1);
   assert.equal(await chart.locator('.ws-chart-cost .ws-chart-line').count(),0);
   assert.equal(await cost.getAttribute('cx'),await token.getAttribute('cx'));
   const costRadius=Number(await cost.getAttribute('r'));
   assert.ok(costRadius>Number(await token.getAttribute('r')),'The cost ring leaves room for the Token marker at a coincident date');
   assert.equal(await cost.evaluate(point=>getComputedStyle(point).fill),'rgba(0, 0, 0, 0)');
  });
  await verify('365 unknown activity days never announce zero records',async()=>{
   mode='unavailable';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();assert.equal(await page.locator('.ws-heat button').count(),365);assert.equal(await page.locator('.ws-heat [data-level="unknown"]').count(),365);
   const labels=await page.locator('.ws-heat button').evaluateAll(cells=>cells.map(cell=>[cell.getAttribute('aria-label'),cell.textContent].join(' ')).join('\n'));
   assert.doesNotMatch(labels,/\b0 (Model calls|Task events|Events)/);assert.match(labels,/Unavailable/);
   assert.doesNotMatch(await page.locator('.ws-heat-readout').innerText(),/\b0 (Model calls|Task events)/);assert.match(await page.locator('.ws-heat-readout').innerText(),/Unavailable/);
  });
  await verify('window and collection-start dates follow Shanghai boundary',async()=>{
   mode='boundary';await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();assert.equal(await page.locator('.ws-operations .ws-coverage').count(),0);
   await page.locator('.ws-usage-panel').getByRole('button',{name:'Details',exact:true}).click();const body=await page.getByRole('dialog',{name:'Usage by Agent',exact:true}).innerText();assert.match(body,/2026-11-06/);assert.match(body,/2026-11-12/);assert.doesNotMatch(body,/2026-11-05/);assert.match(body,/End not included/);assert.match(body,/Collected since 2026-11-06/);
  });
 await verify('calendar horizontal arrows follow the visible number of rows',async()=>{
   const position = cell => cell.evaluate(el=>({x:el.offsetLeft,y:el.offsetTop}));
   for(const [width,height,rows] of [[1440,960,7],[1280,844,14],[390,844,14]]) {
    await page.setViewportSize({width,height});
    if(width===390) await chooseView('Annual activity');
    else await page.locator(`.ws-dashboard[data-compact="${rows===14}"]`).waitFor();
    const cells=page.locator('.ws-heat button');
    await cells.first().focus();
    const before=await position(cells.first());
    await page.keyboard.press('ArrowRight');
    const target=cells.nth(rows);
    assert.equal(await target.evaluate(el=>el===document.activeElement),true);
    const after=await position(target);
    assert.ok(after.x>before.x);
    assert.equal(after.y,before.y);
    await page.keyboard.press('ArrowLeft');
    assert.equal(await cells.first().evaluate(el=>el===document.activeElement),true);
   }
  });
  assert.deepEqual(errors,[]);assert.deepEqual(writes,[]);assert.deepEqual(failures,[]);console.log('PASS all review regressions');
 } else {
 await page.getByRole('heading',{name:'Continue working',exact:true}).waitFor().catch(async error=>{console.error(await page.locator('body').innerText());await page.screenshot({path:out+'/failure.png'});throw error;});
 assert.equal(await page.getByText('Fixture failed task',{exact:true}).count(),1);
 await page.screenshot({path:`${out}/workspace-default-1440.png`,fullPage:true});
 await page.setViewportSize({width:1280,height:800});await page.screenshot({path:`${out}/workspace-default-1280.png`,fullPage:true});
 assert.ok((await page.locator('.ws-chart text').first().boundingBox()).height>=8,'Chart labels must retain readable rendered size');
 for(const panel of ['.ws-usage-panel','.ws-capacity-panel','.ws-agents-panel']) {assert.equal(await page.locator(panel).isVisible(),true);const bounds=await page.locator(panel).boundingBox();assert.ok(bounds.y+bounds.height<=800,'All metric panels fit in the desktop viewport');}
 assert.equal(await page.locator('.ws-state-summary strong').innerText(),'3 / 8');assert.equal(await page.locator('[data-resource=cpu] .ws-capacity-percent').textContent(),'50%');
 const allocations = await page.locator('.ws-capacity-quota').allTextContents();
 assert.deepEqual(allocations,['16 / 32','32 / 64 GB','80 / 100 GB']);
 assert.equal(await page.locator('.ws-capacity-values [data-resource]').count(),4);
 const geometry=await page.evaluate(()=>{const r=s=>document.querySelector(s).getBoundingClientRect();const frame=s=>{const el=document.querySelector(s),box=r(s),style=getComputedStyle(el);return {top:box.top,bottom:box.bottom,border:style.borderTopColor,background:style.backgroundColor};};return {stats:r('.ws-usage-panel').bottom,work:r('.ws-resume').top,resume:r('.ws-resume h2').top,pins:r('.ws-pins h2').top,left:frame('.ws-resume'),right:frame('.ws-pins')};});
 assert.ok(geometry.stats<=geometry.work,'Metrics must precede work');assert.ok(Math.abs(geometry.resume-geometry.pins)<1,'Work headings align');
 assert.deepEqual(geometry.right,geometry.left,'Work panels must have matching visible frames and equal top/bottom edges');
 await page.setViewportSize({width:390,height:844});
 await picker.waitFor();
 await page.screenshot({path:`${out}/workspace-default-390.png`,fullPage:true});
 for(const panel of ['.ws-usage-panel','.ws-capacity-panel']) assert.equal(await page.locator(panel).isVisible(),true);
 await fitsWorkspace('Mobile operations fits');
 await chooseView('Agent states');
 assert.equal(await stateOverview.locator('tbody tr').count(),8,'The narrow Agent states view shows all eight states');
 await fitsWorkspace('Mobile Agent states fits');
 await page.setViewportSize({width:390,height:375});
 const nextStatePage=page.locator('.ws-agents-panel').getByRole('button',{name:'Next',exact:true});
 const previousStatePage=page.locator('.ws-agents-panel').getByRole('button',{name:'Previous',exact:true});
 await nextStatePage.waitFor();
 const visitedStates=[];
 for(let pageNumber=0;pageNumber<states.length;pageNumber++) {
  visitedStates.push(...await stateOverview.locator('[data-state]').evaluateAll(cells=>cells.map(cell=>cell.dataset.state)));
  await fitsWorkspace('Short Agent states page fits');
  if(await nextStatePage.isDisabled()) break;
  await nextStatePage.press('Enter');
 }
 assert.deepEqual(visitedStates,['busy','idle','running','sleeping','starting','failed','offline','unknown']);
 assert.equal(await nextStatePage.evaluate(button=>button===document.activeElement),true,'Paging retains keyboard focus');
 await previousStatePage.press('Enter');
 assert.equal(await nextStatePage.isEnabled(),true,'The previous control returns to the earlier states');
 await page.setViewportSize({width:390,height:844});
 await page.locator('.ws-dashboard[data-layout="groups"]').waitFor();
 await chooseView('Continue working');
 await page.locator('.ws-work').waitFor();
 await fitsWorkspace('Mobile work fits');
 await page.screenshot({path:`${out}/workspace-work-390.png`,fullPage:true});
 await chooseView('Annual activity');
 await page.locator('.ws-activity').waitFor();
 await fitsWorkspace('Mobile activity fits');
 await page.locator('.ws-activity').screenshot({path:`${out}/workspace-activity-390.png`});
 await page.setViewportSize({width:390,height:740});
 await page.locator('.ws-dashboard[data-layout="section"]').waitFor();
 assert.equal(await page.locator('.ws-activity').isVisible(),true,'Activity remains selected when resizing');
 await page.setViewportSize({width:1440,height:900});
 await page.getByRole('button',{name:'Customize pins',exact:true}).click();
 let dialog=page.getByRole('dialog',{name:'Customize pins',exact:true});
 assert.equal(await dialog.getByRole('checkbox').count(),8);
 assert.equal(await dialog.getByRole('checkbox').nth(6).isDisabled(),true);
 await dialog.getByRole('checkbox').nth(0).uncheck();
 assert.equal(await dialog.getByRole('checkbox').nth(6).isDisabled(),false);
 await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
 assert.equal(await page.getByRole('button',{name:'Customize pins',exact:true}).evaluate(el=>el===document.activeElement),true);
 await page.getByRole('button',{name:'Customize pins',exact:true}).click();
 await dialog.getByRole('button',{name:'Clear selection',exact:true}).click();
 await dialog.getByRole('button',{name:'Save',exact:true}).click();
 await page.getByText('No pinned Agents. Choose Agents to keep close at hand.').waitFor();
 await page.reload();await page.getByText('No pinned Agents. Choose Agents to keep close at hand.').waitFor();
 await page.getByRole('button',{name:'Customize pins',exact:true}).click();
 await dialog.getByRole('checkbox').nth(3).check();await dialog.getByRole('button',{name:'Save',exact:true}).click();
 assert.equal(await page.locator('.ws-pins a').getAttribute('href'),'/instances/44');
 await page.locator('.ws-pins a').click();await page.getByRole('heading',{name:'Fixture Agent 4',exact:true}).waitFor();
 assert.equal(new URL(page.url()).pathname,'/instances/44');assert.deepEqual(writes,[]);
 await page.goto(`${base}/dashboard`);await page.getByRole('heading',{name:'Continue working',exact:true}).waitFor();
 await taskToggle.click();
 await taskList.getByRole('button',{name:/Fixture succeeded task/}).click();
 await page.getByRole('button',{name:'Mark as read',exact:true}).click();
 await page.getByRole('dialog',{name:'Task details',exact:true}).getByRole('button',{name:'Close',exact:true}).click();
 await taskList.waitFor();
 assert.equal(await taskList.getByRole('button',{name:/Fixture succeeded task/}).count(),0);
 await closeTasks();
 assert.equal(await taskToggle.evaluate(el=>el===document.activeElement),true);
 assert.equal(await page.locator('.ws-resume').getByText('Fixture succeeded task',{exact:true}).count(),0);
 await page.reload();await page.getByRole('heading',{name:'Continue working',exact:true}).waitFor();
 assert.equal(await page.locator('.ws-resume').getByText('Fixture succeeded task',{exact:true}).count(),0);
 await taskToggle.click();await taskList.getByRole('button',{name:'In progress',exact:false}).first().click();
 await taskList.getByText('Progress not reported',{exact:true}).waitFor();
 await closeTasks();
 await page.locator('.ws-capacity-panel').getByRole('button',{name:'Details',exact:true}).click();
 const capacityDialog=page.getByRole('dialog',{name:'Capacity',exact:true});
 await capacityDialog.waitFor();
 assert.equal(await capacityDialog.getByText(/eligible slots/i).count(),0);
 await page.getByRole('button',{name:'Close',exact:true}).click();
 await page.locator('.ws-agents-panel').getByRole('button',{name:'Details',exact:true}).click();
 const stateDetails=page.getByRole('dialog',{name:'Agent states',exact:true});await stateDetails.waitFor();
 assert.equal(await stateDetails.getByRole('row').count(),9);await stateDetails.getByRole('button',{name:'Close',exact:true}).click();
 await currency.focus();
 await page.keyboard.press('ArrowDown');
 await page.getByRole('option',{name:'USD',exact:true}).waitFor();
 await page.keyboard.press('Escape');
 assert.equal(await currency.evaluate(el=>el===document.activeElement),true);
 await chooseCurrency('USD');
 await page.locator('.ws-usage-panel').getByText('+20% vs previous period',{exact:true}).waitFor();
 assert.match(await page.locator('.ws-costs').innerText(),/0\.000001/);
 const costY=await page.locator('.ws-usage-panel .ws-chart-cost circle').evaluateAll(points=>points.map(point=>Number(point.getAttribute('cy'))));
 assert.ok(Math.max(...costY)-Math.min(...costY)>10,'Tiny recorded costs use their own meaningful chart scale');
 await chooseCurrency('private-credit');
 await page.locator('.ws-chart-cost-label').filter({hasText:'private-credit'}).waitFor();
 await chooseCurrency('USD');
 const points=page.locator('.ws-usage-panel .ws-chart [role="button"]');await points.first().focus();await page.keyboard.press('ArrowRight');
 assert.equal(await points.nth(1).evaluate(el=>el===document.activeElement),true);
 assert.equal(await page.locator('.ws-usage-panel .ws-chart path.ws-chart-line').count(),4);
 await page.getByRole('button',{name:'Today',exact:true}).click();await page.getByText('77',{exact:true}).waitFor();
 assert.equal(await page.locator('.ws-heat button').count(),365);
 delay=400;await page.getByRole('button',{name:'30 days',exact:true}).click();await page.getByRole('button',{name:'Today',exact:true}).click();await page.waitForTimeout(700);assert.equal(await page.locator('.ws-operations').getByText('77',{exact:true}).count(),1);delay=0;
 mode='error';await page.evaluate(()=>{window.dispatchEvent(new Event('offline'));window.dispatchEvent(new Event('online'));});await page.getByText('Connection failed. Showing the last successful data for this period.').first().waitFor();
 assert.equal(await page.locator('.ws-operations').getByText('77',{exact:true}).count(),1);
 await fitsWorkspace('Stale workspace stays on one screen');
 mode='normal';for(const button of await page.getByRole('button',{name:'Try again',exact:true}).all())await button.click();
 await page.getByLabel('Account',{exact:true}).click();assert.equal(await page.getByRole('link',{name:'Portal',exact:true}).count(),0);
 assert.equal(await page.getByRole('link',{name:'Console',exact:true}).count(),1);
 const language=page.getByRole('button',{name:'Language: English',exact:true});await language.click();await page.keyboard.press('Escape');assert.equal(await language.evaluate(el=>el===document.activeElement),true);await page.keyboard.press('Escape');
 for(const [width,height] of [[1440,900],[1280,800],[1366,768],[768,1024],[390,844],[320,720],[667,375],[390,375],[320,400],[1240,752]]) {
  await page.setViewportSize({width,height});
  const layout=width>=1280?'dashboard':height>=844?'groups':'section';
  await page.locator(`.ws-dashboard[data-layout="${layout}"]`).waitFor();
  if(layout==='dashboard') {
   assert.equal(await page.locator('.ws-grid .ws-panel:visible').count(),6);
   assert.equal(await stateOverview.locator('tbody tr').count(),8,`All states fit at ${width}×${height}`);
  }
  else if(layout==='groups') await chooseView('Operations');
  else for(const view of ['Tokens / cost','Capacity','Agent states','Continue working','Pinned Agents','Annual activity']) {
   await chooseView(view);
   assert.equal(await page.locator('.ws-grid .ws-panel:visible').count(),1);
   await fitsWorkspace(`${view} fits at ${width}×${height}`);
  }
  await fitsWorkspace(`Overview fits at ${width}×${height}`);
  await page.screenshot({path:`${out}/workspace-${width}x${height}.png`,fullPage:true});
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`horizontal overflow at ${width}`);
  assert.ok(await page.evaluate(()=>document.documentElement.scrollHeight<=innerHeight+1),`viewport overflow at ${width}`);
  assert.ok(await page.locator('main').evaluate(el=>el.scrollWidth<=el.clientWidth+1),`Workspace content overflows at ${width}`);
 }
 await page.setViewportSize({width:390,height:844});
 for(const locale of ['zh','ja','ko','de','en']) {
  mode='long';
  await page.evaluate(locale=>localStorage.setItem('clawmanager_locale',locale),locale);await page.reload();await page.locator('.ws-grid[aria-busy="false"]').waitFor();
  assert.equal(await page.locator('html').getAttribute('lang'),locale);
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
  await page.screenshot({path:`${out}/workspace-${locale}-390.png`,fullPage:true});
  assert.equal(await page.locator('.ws-capacity-values [data-resource]').count(),4);
  if(locale==='zh') {
   await chooseView(3);
   const status=page.getByRole('table',{name:'Agent 状态',exact:true}).getByRole('cell',{name:'失败待处理',exact:true});
   assert.ok(await status.evaluate(cell=>cell.scrollWidth<=cell.clientWidth),'The full Chinese failure status is readable on mobile');
   await chooseView(0);
  }
  await fitsWorkspace(`${locale} overview fits`);
  await chooseView(1);
  await fitsWorkspace(`${locale} work fits`);
  await page.locator('.ws-grid').screenshot({path:`${out}/workspace-content-${locale}-390.png`});
 }
 mode='normal';
 await taskToggle.click();await taskList.getByRole('button',{name:'In progress',exact:false}).first().click();
 await page.emulateMedia({reducedMotion:'reduce'});
 assert.equal(await taskList.locator('.ws-busy').first().evaluate(el=>getComputedStyle(el).animationName),'none');
 await closeTasks();
 await page.setViewportSize({width:1440,height:900});
 await page.getByRole('button',{name:'Customize pins',exact:true}).click();
 await dialog.getByRole('checkbox').nth(0).check();
 await page.evaluate(()=>{window.originalWorkspaceSetItem=Storage.prototype.setItem;Storage.prototype.setItem=function(key,value){if(key.startsWith('cm:workspace:'))throw new Error('fixture storage full');return window.originalWorkspaceSetItem.call(this,key,value);};});
 await dialog.getByRole('button',{name:'Save',exact:true}).click();
 await dialog.getByText('Browser storage is unavailable. Your changes have not been saved.').waitFor();
 assert.equal(await dialog.isVisible(),true);
 await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
 await page.evaluate(()=>{Storage.prototype.setItem=window.originalWorkspaceSetItem;});
 mode='partial';await page.reload();await page.locator('.ws-pins').getByText('Some saved pins are outside this response. They are retained.').waitFor();
 assert.deepEqual(await page.evaluate(()=>JSON.parse(localStorage.getItem('cm:workspace:v1:71')).pins),[44]);
 mode='normal';await page.reload();await page.locator('.ws-pins a[href="/instances/44"]').waitFor();
 // A delayed former-account response must never enter the new account's tree.
 await page.locator('.ws-grid[aria-busy="false"]').waitFor();
 delay=400;
 const formerAccountRequest=page.waitForRequest(request=>new URL(request.url()).pathname.endsWith('/dashboard/workspace'));
 await page.evaluate(()=>{window.dispatchEvent(new Event('offline'));window.dispatchEvent(new Event('online'));});
 await formerAccountRequest;
 userID=72;
 await page.evaluate(async()=>{const {useAuthStore}=await import('/src/stores/authStore.ts');useAuthStore.setState({user:{id:72,username:'Fixture user 72',role:'admin'},isAuthenticated:true});});
 await page.locator('.ws-recent-list').getByText('Account 72 private Agent',{exact:true}).waitFor();
 await page.waitForTimeout(500);delay=0;
 assert.equal(await page.locator('.ws-resume').getByText('Fixture failed task',{exact:true}).count(),0);
 assert.equal(await page.locator('.ws-pins').getByText('Fixture Agent 4',{exact:true}).count(),0);
 assert.equal(await page.evaluate(()=>localStorage.getItem('cm:workspace:v1:72')),null);
 await page.setViewportSize({width:1440,height:900});mode='unavailable';await page.reload();await page.locator('.ws-recent-list .ws-empty').waitFor();assert.match(await page.locator('.ws-recent-list .ws-empty').innerText(),/Unavailable/);
 await taskToggle.click();
 assert.equal(await taskList.locator('.ws-counters strong').first().innerText(),'—');
 await closeTasks();
 await page.locator('.ws-metrics').getByText('Input — · Output —',{exact:true}).waitFor();
 await page.locator('.ws-usage-panel').getByRole('button',{name:'Details',exact:true}).click();
 assert.equal(await page.getByRole('dialog').getByRole('cell',{name:'800',exact:true}).count(),0);
 await page.getByRole('button',{name:'Close',exact:true}).click();
 assert.equal(await page.locator('.ws-usage-panel .ws-chart circle').count(),0);
 await page.screenshot({path:`${out}/workspace-unavailable.png`});
 mode='empty';await page.reload();await page.locator('.ws-pins').getByText(/^No Agents yet\./).waitFor();await page.screenshot({path:`${out}/workspace-empty.png`});
 // Initial errors offer retry without inventing empty/zero data.
 mode='error';await page.reload();await page.getByRole('alert').filter({hasText:'Workspace could not be loaded.'}).waitFor();
 await page.screenshot({path:`${out}/workspace-error.png`});
 mode='normal';await page.getByRole('button',{name:'Try again',exact:true}).click();await page.getByRole('heading',{name:'Continue working',exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('.ws-grid')?.getAttribute('aria-busy')==='false');
 await page.clock.install();
 await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});Object.defineProperty(document,'visibilityState',{configurable:true,value:'hidden'});document.dispatchEvent(new Event('visibilitychange'));});
 await page.locator('.ws-paused').waitFor();const hiddenCalls=workspaceCalls;
 await page.clock.fastForward(31_000);await page.waitForTimeout(100);assert.equal(workspaceCalls,hiddenCalls);
 await page.evaluate(()=>{delete document.hidden;delete document.visibilityState;document.dispatchEvent(new Event('visibilitychange'));});
 const pending = Promise.withResolvers();
 pendingWorkspaceResponse = pending.promise;
 try {
  await page.reload({waitUntil:'domcontentloaded'});
  await page.locator('.ws-metric-skeleton').first().waitFor();
  await page.screenshot({path:`${out}/workspace-loading.png`});
 } finally {
  pendingWorkspaceResponse = undefined;
  pending.resolve();
 }
 await page.locator('.ws-grid[aria-busy="false"]').waitFor();
 assert.deepEqual(writes,[]);assert.deepEqual(errors,[]);
 console.log('PASS real /dashboard Router/Auth; recent Agents; three panels; pins max/cancel/empty/reload/stopped href; acknowledgement; gaps/keyboard; latest range; stale/retry; Portal absent/Console/language; 10 viewports without Workspace scrollbars; 5 locales; reduced motion; empty; zero API writes');
 }
} finally {
 await browser?.close();
 await server?.close();
}
