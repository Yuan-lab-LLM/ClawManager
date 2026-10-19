// Run from repository root with Vite preview on 38451 and Playwright available.
// All application requests are mocked; no real lifecycle operations are sent.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:1000}});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const days=Array.from({length:90},(_,i)=>({date:new Date(Date.UTC(2026,5,20+i)).toISOString().slice(0,10),tokens:i>82?(i-82)*1000:0,prompt_tokens:i*100,completion_tokens:i*10,questions:i>82?2:0,sessions:i>82?1:0,observed:i>82}));
 const tasks=[{code:'login-day',name:'今日首次访问',period:'day',metric:'login',target:1,progress:1,xp:5,awarded:true},{code:'work-15',name:'前台活跃使用 15 分钟',period:'day',metric:'seconds',target:900,progress:30,xp:5,awarded:false},{code:'login-3',name:'累计访问 3 天',period:'all',metric:'login',target:3,progress:1,xp:10,awarded:false}];
 const growth={xp:5,level:1,level_start:0,next_level:100,started_at:'2026-09-17T01:00:00Z',tasks,ledger:[{name:'今日首次访问',xp:5,at:'2026-09-17T01:00:00Z'}],days:[{date:'2026-09-17',xp:5}],rule:'growth-v1',note:'测试成长规则'};
 growth.ranking={rank:2,participants:8,tokens:28000,start:'2026-09-10T00:00:00Z',end:'2026-09-17T00:00:00Z'};
 days[87]={...days[87],observed:false,tokens:0,questions:0};
 const instances=[{id:1,name:'test',alias:'测试实例',type:'openclaw',status:'running',runtime_type:'gateway',instance_mode:'lite',created_at:'2026-09-01T00:00:00Z',updated_at:'2026-09-17T00:00:00Z'}];
 for(let i=0;i<4;i++)tasks.push({code:'daily-'+i,name:'前台活跃任务 '+i,period:'day',metric:'seconds',target:900,progress:30,xp:5,awarded:false});
 for(let i=0;i<8;i++)tasks.push({code:'total-'+i,name:'累计任务 '+i,period:'all',metric:'login',target:3,progress:1,xp:10,awarded:false});
 for(let i=2;i<=40;i++)instances.push({...instances[0],id:i,name:'test-'+i,alias:'长名称用于检查布局的实例 '+i,type:['openclaw','hermes','opencode','deepseek-harness'][(i-1)%4]});
 const report={kind:'week',start:'2026-09-07T00:00:00+08:00',end:'2026-09-14T00:00:00+08:00',as_of:'2026-09-17T00:00:00+08:00',ongoing:false,counts:{tokens:7000,questions:10,sessions:3},previous:{tokens:5000,questions:5,sessions:2},days:days.slice(-7),runtime:[{runtime:'openclaw',tokens:7000,questions:10}],active_days:3,observed_days:4,instances:1,used_sessions:3,revision:0,saved_revisions:[],note:'按已采集记录'};
 let shownInstances=instances;let failRefresh=false;const requests=[];
 await context.route('**/api/v1/ieisystem/**',async route=>{const u=new URL(route.request().url());requests.push({path:u.pathname,method:route.request().method()});if(failRefresh&&u.pathname.endsWith('/dashboard')){await route.fulfill({status:500,json:{error:'fixture'}});return}let data={};if(u.pathname.endsWith('/session'))data={owner:'zhangsan123@example.com',expires_at:'2030-01-01T00:00:00Z'};else if(u.pathname.endsWith('/instances'))data={instances:shownInstances,total:shownInstances.length,page:1,limit:100};else if(u.pathname.includes('/growth'))data=growth;else if(u.pathname.endsWith('/reports'))data=route.request().method()==='POST'?{...report,revision:1,saved_revisions:[1]}:report;else if(u.pathname.endsWith('/dashboard'))data={summary:{prompt_tokens:25000,completion_tokens:3000,total_tokens:28000,sessions:3,questions:14,workday_streak:3},trend:days.slice(u.searchParams.get('range')==='30d'?-30:-7).map(d=>({date:d.date,tokens:d.tokens})),last_activity:{1:'2026-09-17T00:00:00Z'},recent_questions:{1:{text:'请帮我检查这段代码并保存测试文件，确认长提问能够正常换行而不是撑开卡片。',at:'2026-09-17T00:00:00Z',checked_at:'2026-09-17T00:00:00Z'}},coverage:{questions_supported:true,partial:true,description:'已确认部分',ready_histories:1,unavailable_histories:0},insights:{days,window:{kind:'day',start:'2026-09-17T00:00:00+08:00',end:'2026-09-17T14:00:00+08:00',previous_start:'2026-09-16T00:00:00+08:00',previous_end:'2026-09-16T14:00:00+08:00'},comparison:{tokens:{current:7000,previous:5000,delta:2000,percent:40,status:'change'},questions:{current:2,previous:1,delta:1,percent:100,status:'change'},sessions:{current:1,previous:0,delta:1,percent:null,status:'insufficient'}},as_of:'2026-09-17T14:00:00+08:00',coverage_note:'等长时段'},timezone:'Asia/Shanghai',range:'7d'};else if(u.pathname.includes('lifecycle-operation')){await route.fulfill({status:404,json:{error:'none'}});return}await route.fulfill({json:{data}})});
 await page.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38451')+'/ieisystem/list-instances?view=overview');await page.getByRole('tab',{name:'总览',exact:true}).waitFor();await page.getByText('40.0% · 较昨日同期',{exact:false}).waitFor();
 assert.equal(await page.getByRole('button',{name:'刷新统计',exact:true}).count(),0);
 assert.deepEqual(await page.getByRole('tab').allTextContents(),['总览','我的成长','周期报告']);
 assert.equal(await page.getByText('统计口径与覆盖范围',{exact:true}).count(),0);
 assert.equal(await page.getByRole('button',{name:'统计说明',exact:true}).count(),0);
 const refresh=page.locator('main > header').getByRole('button',{name:'刷新',exact:true});
 const marker=await page.locator('.owner-panel').evaluate(e=>{e.setAttribute('data-refresh-marker','kept');return true});assert(marker);
 const start=requests.length;await refresh.click();await page.waitForFunction(()=>!document.querySelector('main > header button:disabled'));await page.waitForTimeout(100);
 for(const path of ['/session','/instances','/dashboard','/growth'])assert(requests.slice(start).some(r=>r.path.endsWith(path)),path+' not refreshed');
 assert(!requests.slice(start).some(r=>r.path.endsWith('/growth/sync')),'ordinary refresh must not reconcile tasks');
 assert.equal(await page.locator('.owner-panel').getAttribute('data-refresh-marker'),'kept');
 const chart=page.getByTestId('usage-chart').first();const bounds=await chart.boundingBox();assert(bounds.height>=100);await chart.locator('circle').last().hover();await page.getByRole('tooltip').first().waitFor();await chart.locator('circle').last().click();await page.getByRole('dialog').waitFor();await page.keyboard.press('Escape');assert.equal(await page.getByRole('dialog').count(),0);

 const summary=page.getByTestId('owner-overview-summary');assert.equal(await summary.count(),1);
 assert(await summary.innerText().then(t=>t.includes('近七天 Token 排名')&&t.includes('有效提问')));
 assert.match(await page.getByTestId('growth-stat').innerText(),/第 2 名/);
 const missing=page.locator(`.owner-heatmap button[aria-label^="${days[87].date}"]`);
 assert.match(await missing.getAttribute('class'),/bg-slate-100/);
 assert.match(await page.locator(`.owner-heatmap button[aria-label^="${days[0].date}"]`).getAttribute('class'),/bg-white/);
 await missing.hover();await page.getByRole('tooltip').filter({hasText:'暂无提问记录'}).waitFor();await page.mouse.move(0,0);
 assert.equal(await page.getByRole('tab',{name:'总览',exact:true}).evaluate(e=>getComputedStyle(e).fontSize),'14px');
 const quick=page.getByTestId('instance-quick-actions').first();await page.mouse.move(1400,10);
 assert.equal(await quick.evaluate(e=>getComputedStyle(e).opacity),'0');
 const card=quick.locator('..');const before=await card.locator('h3').boundingBox();await card.hover();
 assert.equal(await quick.evaluate(e=>getComputedStyle(e).opacity),'1');assert.deepEqual(await card.locator('h3').boundingBox(),before);
 assert.equal(await quick.getByRole('link',{name:/进入实例/}).getAttribute('href'),'/ieisystem/instances/1');
 await quick.getByRole('button',{name:/的操作/}).click();
 for(const name of ['进入实例','重启实例','重置实例'])assert.equal(await page.getByRole('menuitem',{name,exact:true}).count(),1);
 let dialogs=0;page.on('dialog',async d=>{dialogs++;await d.dismiss()});
 await page.getByRole('menuitem',{name:'重启实例',exact:true}).click();assert.equal(dialogs,1);
 await page.getByRole('menuitem',{name:'重置实例',exact:true}).click();assert.equal(dialogs,2);
 await page.keyboard.press('Escape');assert.equal(await page.getByRole('menu').count(),0);
 for(const width of [1920,1440,1280]){await page.setViewportSize({width,height:1000});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));}
 await page.setViewportSize({width:1440,height:1000});await page.screenshot({path:'.tmp/owner-r7-overview.png',fullPage:true});

 await page.getByRole('tab',{name:'我的成长',exact:true}).click();await page.getByRole('button',{name:'每日任务',exact:true}).waitFor();await page.getByRole('button',{name:'累计任务',exact:true}).waitFor();await page.screenshot({path:'.tmp/owner-v5-growth.png',fullPage:true});await page.reload();assert.equal(await page.getByRole('tab',{name:'我的成长',exact:true}).getAttribute('aria-selected'),'true');
 await page.getByRole('tab',{name:'周期报告',exact:true}).click();await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();await page.getByRole('button',{name:'保存 / 更新快照',exact:true}).click();await page.locator('span').filter({hasText:/^快照 v1$/}).waitFor();await page.getByRole('button',{name:'月报',exact:true}).click();await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();await page.reload();await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'月报',exact:true}).getAttribute('aria-pressed'),'true');await page.screenshot({path:'.tmp/owner-v5-reports.png',fullPage:true});

 for(const viewport of [{width:1920,height:1080},{width:1440,height:900},{width:1366,height:768}]){
  await page.setViewportSize(viewport);
  for(const tab of ['总览','我的成长','周期报告']){
   await page.getByRole('tab',{name:tab,exact:true}).click();await page.waitForTimeout(180);
   if(tab==='周期报告')await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();
   await page.screenshot({path:'.tmp/owner-v5-'+viewport.width+'-'+tab+'.png',fullPage:true});
   const measure=await page.evaluate(()=>({w:innerWidth,h:innerHeight,sw:document.documentElement.scrollWidth,sh:document.documentElement.scrollHeight,panel:[document.querySelector('.owner-panel').clientHeight,document.querySelector('.owner-panel').scrollHeight]}));
   assert(measure.sw<=measure.w&&measure.sh<=measure.h+1,JSON.stringify({tab,...measure}));
   assert(measure.panel[1]<=measure.panel[0]+2,JSON.stringify({tab,...measure}));
   const clipped=await page.locator('.owner-summary,.owner-continue,.owner-chart-card,.owner-task-panel,.owner-report-summary,.owner-report-runtime').evaluateAll(es=>es.filter(e=>e.scrollHeight>e.clientHeight+2).map(e=>({className:e.className,client:e.clientHeight,scroll:e.scrollHeight})));
   assert.deepEqual(clipped,[],JSON.stringify({tab,clipped}));
   const bottoms=await page.locator('.owner-panel button,.owner-panel svg,.owner-panel footer').evaluateAll(es=>es.filter(e=>e.getBoundingClientRect().width>0).map(e=>({text:e.textContent?.slice(0,30),bottom:e.getBoundingClientRect().bottom})));
   assert(bottoms.every(e=>e.bottom<=viewport.height),JSON.stringify({tab,bottoms}));
   if(tab==='总览'){
    const identity=await page.locator('.owner-welcome').boundingBox(),medal=await page.locator('.owner-welcome>.owner-medal').boundingBox();
    assert.equal(medal.height,72);assert(Math.abs(medal.y+medal.height/2-identity.y-identity.height/2)<2,'Medal must be vertically centered');
    const left=await page.locator('.portal-instance-sidebar').boundingBox(),right=await page.locator('.owner-panel').boundingBox();
    assert(Math.abs(left.y-right.y)<2,'Panels top edges differ');
    const plot=await page.locator('.owner-charts').boundingBox();assert(plot.height<=282,'Charts too tall');
    assert.equal(await page.getByText('欢迎回来，zhangsan',{exact:true}).count(),1);
    const visual=await page.locator('.owner-welcome').evaluate(e=>{const box=e.getBoundingClientRect(),badge=e.querySelector('svg').getBoundingClientRect(),greeting=e.querySelector('.owner-welcome-line span').getBoundingClientRect();return {top:Math.min(badge.top,greeting.top)-box.top,level:parseFloat(getComputedStyle(e.querySelector('.owner-level')).fontSize)}});
    assert(visual.top>=16&&visual.level>=40,'Personal grade emphasis/spacing regressed: '+JSON.stringify(visual));
    const grow=await page.getByTestId('growth-stat').boundingBox(),sessions=await page.getByTestId('metric-sessions').boundingBox();
    const task=await page.getByTestId('task-stat').boundingBox(),streak=await page.getByTestId('metric-streak').boundingBox();
    assert(Math.abs(grow.x-sessions.x)<2&&Math.abs(task.x-streak.x)<2,'Grid columns not aligned');
   }
  }
 }
 await page.getByRole('tab',{name:'我的成长',exact:true}).click();
 await page.getByRole('button',{name:'每日任务',exact:true}).click();
 assert.equal(await page.locator('.owner-task').count(),6);
 assert.equal(await page.locator('.owner-pager').count(),0);
 await page.getByRole('button',{name:'累计任务',exact:true}).click();assert(await page.locator('.owner-pager').innerText().then(t=>t.includes('1 /')));
 await page.getByRole('button',{name:'下一页',exact:true}).click();assert(await page.locator('.owner-pager').innerText().then(t=>t.includes('2 /')));
 await page.getByRole('button',{name:'查看经验明细',exact:true}).click();await page.getByRole('dialog',{name:'经验明细'}).waitFor();await page.keyboard.press('Escape');assert.equal(await page.getByRole('dialog').count(),0);
 await page.getByRole('tab',{name:'周期报告',exact:true}).click();await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();
 const reportStart=requests.length;await refresh.click();await page.waitForTimeout(300);
 assert(requests.slice(reportStart).some(r=>r.path.endsWith('/reports')&&r.method==='GET'));
 assert(!requests.slice(reportStart).some(r=>r.path.endsWith('/reports')&&r.method==='POST'),'Refresh saved report');
 await page.getByRole('tab',{name:'总览',exact:true}).click();
 for(const width of [1366,1920]){
  await page.setViewportSize({width,height:width===1366?768:1080});
  for(let count=1;count<=5;count++){
   shownInstances=instances.slice(0,count);await refresh.click();await page.waitForTimeout(200);
   const shown=await page.locator('.owner-continue-list button').count();
   assert.equal(shown,width===1366?Math.min(3,count):count);
   if(count>shown){await page.getByRole('button',{name:'下一组实例'}).click();assert.equal(await page.locator('.owner-continue-list button').count(),count-shown);await page.getByRole('button',{name:'上一组实例'}).click()}
   assert(await page.evaluate(()=>document.documentElement.scrollHeight<=innerHeight+1));
   await page.screenshot({path:'.tmp/owner-r7-'+width+'-'+count+'-types.png',fullPage:true});
  }
 }
 shownInstances=instances;await refresh.click();await page.waitForTimeout(250);
 failRefresh=true;await refresh.click();await page.getByRole('alert').filter({hasText:'统计刷新失败'}).waitFor();
 assert.equal(await page.getByTestId('metric-questions').innerText().then(t=>t.includes('14')),true);
 failRefresh=false;await refresh.click();await page.waitForTimeout(250);
 await page.setViewportSize({width:390,height:844});for(const tab of ['总览','我的成长','周期报告']){await page.getByRole('tab',{name:tab,exact:true}).click();await page.waitForTimeout(150);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),tab+' overflow')};await page.screenshot({path:'.tmp/owner-v5-mobile.png',fullPage:true});assert.deepEqual(errors,[]);
 const touch=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:390,height:844},hasTouch:true,isMobile:true});
 await touch.route('**/api/v1/ieisystem/**',async route=>{
  const p=new URL(route.request().url()).pathname;
  await route.fulfill({json:{data:p.endsWith('/session')?{owner:'zhangsan123@example.com'}:p.endsWith('/instances')?{instances,total:1}:null}});
 });
 const touchPage=await touch.newPage();await touchPage.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38451')+'/ieisystem/list-instances?view=overview');
 const touchActions=touchPage.getByTestId('instance-quick-actions').first();await touchActions.waitFor();
 assert.equal(await touchActions.evaluate(e=>getComputedStyle(e).opacity),'1');
 await browser.close();console.log('PASS fixed viewport all 3 tabs at 1920/1440/1366, no clipping, aligned columns, numeric suffix removed, pagination/ledger modal, original lifecycle cancel/menu, mobile/touch');
})().catch(e=>{console.error(e);process.exit(1)});
