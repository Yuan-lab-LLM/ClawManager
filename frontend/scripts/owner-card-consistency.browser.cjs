// Synthetic fixtures only; lifecycle actions must remain cancelled.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});const context=await browser.newContext({viewport:{width:1440,height:900}});const page=await context.newPage();page.setDefaultTimeout(10000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const types=['openclaw','hermes','opencode','deepseek-harness'];
 const instances=types.map((type,i)=>({id:i+1,name:'长名称实例 · '+type+' · 协作工作空间',type,status:i===3?'error':'running',instance_mode:'lite',created_at:'2026-09-01T00:00:00Z',updated_at:'2026-09-21T04:00:00Z'}));
 const tasks=[['login',1,10],['questions',3,20],['questions',5,30],['tokens',5000,10],['tokens',20000,10],['tokens',50000,20]].map(([metric,target,xp],i)=>({code:'task-'+i,name:['今日启航','深入交流','持续推进','轻量协作','深度协作','充分协作'][i],metric,target,xp,period:'day',progress:i===0?1:0,awarded:false}));
 const growth={xp:27,level:1,level_start:0,next_level:120,started_at:'2026-09-01T00:00:00Z',as_of:'2026-09-21T04:00:00Z',tasks,ledger:[],days:[],rule:'growth-v3',note:'规则',daily_limit:100};
 let writes=0;
 await context.route('**/api/**',async route=>{const p=new URL(route.request().url()).pathname;let data={};if(p.endsWith('/session'))data={owner:'accept123@example.com',expires_at:'2030-01-01T00:00:00Z'};else if(p.endsWith('/instances'))data={instances,total:5};else if(p.includes('/growth'))data=growth;else if(p.endsWith('/dashboard'))data={summary:{questions:5,total_tokens:50000,sessions:2,workday_streak:1},trend:[],last_activity:{},coverage:{questions_supported:true,partial:true},insights:{as_of:growth.as_of,days:[],comparison:{tokens:{status:'new',current:5,previous:0,delta:5,percent:null}},window:{kind:'day'}}};else if(p.includes('lifecycle-operation')){await route.fulfill({status:404,json:{error:'none'}});return}else if(route.request().method()==='POST'){writes++}await route.fulfill({json:{data}})});
 await page.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38479')+'/ieisystem/list-instances?view=overview');await page.locator('.portal-runtime-card').first().waitFor();
 await page.getByRole('tab',{name:'我的成长',exact:true}).click();await page.getByText('已达标，奖励待入账',{exact:true}).waitFor();assert.match(await page.getByTestId('task-stat').innerText(),/1 \/ 6/);assert.match(await page.locator('.owner-task-tabs').innerText(),/已入账 0 \/ 100 XP/);
 assert.equal(await page.locator('.owner-daily-grid .owner-task').count(),6);
 growth.upcoming_tasks=[{...tasks[0],name:'未来任务预告',target:9}];await page.getByRole('button',{name:'更新进度',exact:true}).click();assert.equal(await page.getByText('未来任务预告',{exact:true}).count(),0);
 await page.getByRole('tab',{name:'总览',exact:true}).click();
 assert.match(await page.getByTestId('metric-tokens').innerText(),/上期 0/);
 const intersects=(a,b)=>a.x<b.x+b.width&&a.x+a.width>b.x&&a.y<b.y+b.height&&a.y+a.height>b.y;
 for(const width of [1920,1440,1366,390]){
  console.log('Checking cards at',width);
  await page.setViewportSize({width,height:width===1366?768:900});
  for(const type of types){
   const card=page.locator(`.portal-runtime-card[data-runtime="${type}"]`);await card.scrollIntoViewIfNeeded();await card.hover();await page.waitForTimeout(280);
   const entry=await card.locator('.instance-hover-entry').boundingBox();
   for(const selector of ['.instance-runtime-label','.instance-heading strong','.instance-meta']) assert(!intersects(entry,await card.locator(selector).boundingBox()),type+' collapsed overlap '+width+' '+selector);
   const rightBefore=await page.locator('.owner-dashboard').count();await card.locator('.instance-summary').click();await page.waitForTimeout(300);assert.equal(await card.getAttribute('data-expanded'),'true');assert.equal(await page.locator('.owner-dashboard').count(),rightBefore);
   assert.equal(await card.locator('.instance-watermark').evaluate(e=>getComputedStyle(e).opacity),'0.25');
   assert(await card.locator('.instance-watermark').evaluate(e=>e.complete&&e.naturalWidth>0));
   assert.equal(await card.locator('.instance-description .instance-entry').getAttribute('href'),'/ieisystem/instances/'+(types.indexOf(type)+1));
   await card.locator('.instance-summary').click();await page.waitForTimeout(300);assert.equal(await card.getAttribute('data-expanded'),'false');
  }
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'horizontal overflow '+width);
 }
 await page.setViewportSize({width:1440,height:900});const card=page.locator('.portal-runtime-card[data-runtime="deepseek-harness"]');await card.locator('.instance-more').click();
 let confirmations=0;page.on('dialog',async d=>{confirmations++;await d.dismiss()});await card.getByRole('menuitem',{name:'重启实例',exact:true}).click();assert.equal(confirmations,1);await card.getByRole('menuitem',{name:'重置实例',exact:true}).click();assert.equal(confirmations,2);assert.equal(writes,0);
 await card.getByRole('menuitem',{name:'实例详情',exact:true}).click();await page.getByRole('heading',{name:'运行时说明',exact:true}).waitFor();
 await page.getByRole('button',{name:'我的总览',exact:true}).click();await page.locator('.portal-runtime-card[data-runtime="opencode"] .instance-summary').click();await page.waitForTimeout(300);await page.screenshot({path:'.tmp/r14-cards.png',fullPage:true});
 await context.route('**/instances/*/access',route=>route.fulfill({status:409,json:{error:'Runtime temporarily unavailable'}}));
 await page.locator('.portal-runtime-card[data-runtime="opencode"] .instance-description .instance-entry').click();
 await page.getByRole('link',{name:'返回我的总览',exact:true}).first().click();await page.locator('.owner-dashboard').waitFor();
 assert.equal(new URL(page.url()).searchParams.get('view'),'overview');
 assert.equal(await page.locator('.portal-runtime-card[data-runtime="opencode"]').getAttribute('data-expanded'),'true');
 await page.reload();await page.locator('.owner-dashboard').waitFor();assert.equal(await page.locator('.portal-runtime-card[data-runtime="opencode"]').getAttribute('data-expanded'),'true');
 for(const [width,height] of [[1920,1080],[1920,944],[1440,900],[1366,768]]){
  await page.setViewportSize({width,height});await page.waitForTimeout(200);
  assert(await page.evaluate(()=>document.documentElement.scrollHeight<=innerHeight+1),'vertical overflow '+width+'x'+height);
  const gap=await page.evaluate(()=>{const dashboard=document.querySelector('.owner-dashboard').getBoundingClientRect();const overview=document.querySelector('.owner-overview').getBoundingClientRect();return dashboard.bottom-overview.bottom});
  assert(gap<35,'unfilled overview area '+width+'x'+height+': '+gap);
  assert(await page.locator('.owner-summary .owner-welcome-line').evaluate(e=>parseFloat(getComputedStyle(e).fontSize)>=19));
 }
 await page.getByRole('tab',{name:'我的成长',exact:true}).click();assert.doesNotMatch(await page.locator('.owner-dashboard').innerText(),/Lv\.100|100\s*级/);
 await page.getByRole('tab',{name:'总览',exact:true}).click();await page.screenshot({path:'.tmp/r15-overview-layout.png',fullPage:true});
 assert.deepEqual(errors,[]);await browser.close();console.log('PASS: five card types; 4 widths; no overlaps; overview return restores expanded target; reload persists; no future level announcement; overview fills height; pending awards and top count');
})().catch(e=>{console.error(e);process.exit(1)});
