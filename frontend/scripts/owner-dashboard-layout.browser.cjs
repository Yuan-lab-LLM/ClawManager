// Run from repository root with Vite preview on 38451 and Playwright available.
// All application requests are mocked; no real lifecycle operations are sent.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});const context=await browser.newContext({viewport:{width:1440,height:1000}});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const days=Array.from({length:90},(_,i)=>({date:new Date(Date.UTC(2026,5,20+i)).toISOString().slice(0,10),tokens:i>82?(i-82)*1000:0,prompt_tokens:i*100,completion_tokens:i*10,questions:i>82?2:0,sessions:i>82?1:0,observed:i>82}));
 const tasks=[{code:'login-day',name:'今日首次访问',period:'day',metric:'login',target:1,progress:1,xp:5,awarded:true},{code:'work-15',name:'前台活跃使用 15 分钟',period:'day',metric:'seconds',target:900,progress:30,xp:5,awarded:false},{code:'login-3',name:'累计访问 3 天',period:'all',metric:'login',target:3,progress:1,xp:10,awarded:false}];
 const growth={xp:5,level:1,level_start:0,next_level:100,started_at:'2026-09-17T01:00:00Z',tasks,ledger:[{name:'今日首次访问',xp:5,at:'2026-09-17T01:00:00Z'}],days:[{date:'2026-09-17',xp:5}],rule:'growth-v1',note:'测试成长规则'};
 const instances=[{id:1,name:'test',alias:'测试实例',type:'openclaw',status:'running',runtime_type:'gateway',instance_mode:'lite',created_at:'2026-09-01T00:00:00Z',updated_at:'2026-09-17T00:00:00Z'}];
 const report={kind:'week',start:'2026-09-07T00:00:00+08:00',end:'2026-09-14T00:00:00+08:00',as_of:'2026-09-17T00:00:00+08:00',ongoing:false,counts:{tokens:7000,questions:10,sessions:3},previous:{tokens:5000,questions:5,sessions:2},days:days.slice(-7),runtime:[{runtime:'openclaw',tokens:7000,questions:10}],active_days:3,observed_days:4,instances:1,used_sessions:3,revision:0,saved_revisions:[],note:'按已采集记录'};
 await context.route('**/api/v1/ieisystem/**',async route=>{const u=new URL(route.request().url());let data={};if(u.pathname.endsWith('/session'))data={owner:'owner@example.com',expires_at:'2030-01-01T00:00:00Z'};else if(u.pathname.endsWith('/instances'))data={instances,total:1,page:1,limit:100};else if(u.pathname.includes('/growth'))data=growth;else if(u.pathname.endsWith('/reports'))data=route.request().method()==='POST'?{...report,revision:1,saved_revisions:[1]}:report;else if(u.pathname.endsWith('/dashboard'))data={summary:{prompt_tokens:25000,completion_tokens:3000,total_tokens:28000,sessions:3,questions:14,workday_streak:3},trend:days.slice(u.searchParams.get('range')==='30d'?-30:-7).map(d=>({date:d.date,tokens:d.tokens})),last_activity:{1:'2026-09-17T00:00:00Z'},coverage:{questions_supported:true,partial:true,description:'已确认部分',ready_histories:1,unavailable_histories:0},insights:{days,window:{kind:'day',start:'2026-09-17T00:00:00+08:00',end:'2026-09-17T14:00:00+08:00',previous_start:'2026-09-16T00:00:00+08:00',previous_end:'2026-09-16T14:00:00+08:00'},comparison:{tokens:{current:7000,previous:5000,delta:2000,percent:40,status:'change'},questions:{current:2,previous:1,delta:1,percent:100,status:'change'},sessions:{current:1,previous:0,delta:1,percent:null,status:'insufficient'}},as_of:'2026-09-17T14:00:00+08:00',coverage_note:'等长时段'},timezone:'Asia/Shanghai',range:'7d'};else if(u.pathname.includes('lifecycle-operation')){await route.fulfill({status:404,json:{error:'none'}});return}await route.fulfill({json:{data}})});
 await page.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38451')+'/ieisystem/list-instances?view=overview');await page.getByRole('tab',{name:'总览',exact:true}).waitFor();await page.getByText('40.0% · 较昨日同期',{exact:false}).waitFor();
 assert.deepEqual(await page.getByRole('tab').allTextContents(),['总览','我的成长','周期报告']);
 const chart=page.getByTestId('usage-chart').first();const bounds=await chart.boundingBox();assert(bounds.height<=220);await chart.locator('circle').last().hover();await page.getByRole('tooltip').first().waitFor();await chart.locator('circle').last().click();await page.getByRole('dialog').waitFor();await page.keyboard.press('Escape');assert.equal(await page.getByRole('dialog').count(),0);

 const summary=page.getByTestId('owner-overview-summary');assert.equal(await summary.count(),1);
 assert(await summary.innerText().then(t=>t.includes('累计成长')&&t.includes('有效提问')));
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
 await page.setViewportSize({width:1440,height:1000});await page.screenshot({path:'.tmp/owner-v4-overview.png',fullPage:true});

 await page.getByRole('tab',{name:'我的成长',exact:true}).click();await page.getByRole('heading',{name:'每日任务',exact:false}).waitFor();await page.getByRole('heading',{name:'累计任务',exact:false}).waitFor();await page.screenshot({path:'.tmp/owner-v4-growth.png',fullPage:true});await page.reload();assert.equal(await page.getByRole('tab',{name:'我的成长',exact:true}).getAttribute('aria-selected'),'true');
 await page.getByRole('tab',{name:'周期报告',exact:true}).click();await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();await page.getByRole('button',{name:'保存 / 更新快照',exact:true}).click();await page.locator('span').filter({hasText:/^快照 v1$/}).waitFor();await page.getByRole('button',{name:'月报',exact:true}).click();await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();await page.reload();await page.getByRole('heading',{name:'周期摘要',exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'月报',exact:true}).getAttribute('aria-pressed'),'true');await page.screenshot({path:'.tmp/owner-v4-reports.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});for(const tab of ['总览','我的成长','周期报告']){await page.getByRole('tab',{name:tab,exact:true}).click();await page.waitForTimeout(150);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),tab+' overflow')};await page.screenshot({path:'.tmp/owner-v4-mobile.png',fullPage:true});assert.deepEqual(errors,[]);
 const touch=await browser.newContext({viewport:{width:390,height:844},hasTouch:true,isMobile:true});
 await touch.route('**/api/v1/ieisystem/**',async route=>{
  const p=new URL(route.request().url()).pathname;
  await route.fulfill({json:{data:p.endsWith('/session')?{owner:'owner@example.com'}:p.endsWith('/instances')?{instances,total:1}:null}});
 });
 const touchPage=await touch.newPage();await touchPage.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38451')+'/ieisystem/list-instances?view=overview');
 const touchActions=touchPage.getByTestId('instance-quick-actions').first();await touchActions.waitFor();
 assert.equal(await touchActions.evaluate(e=>getComputedStyle(e).opacity),'1');
 await browser.close();console.log('PASS tabs/order/persistence, equal-period labels, fixed chart height, tooltip/dialog, growth/tasks, report snapshots/month selection, mobile layout');
})().catch(e=>{console.error(e);process.exit(1)});
