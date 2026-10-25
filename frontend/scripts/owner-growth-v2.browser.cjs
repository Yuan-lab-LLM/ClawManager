// All API responses are synthetic. No real owner usage or lifecycle calls.
const {chromium}=require('playwright');const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});const context=await browser.newContext({viewport:{width:1440,height:900},ignoreHTTPSErrors:true});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const threshold=l=>120*(l-1)+5*(l-1)*(l-2);
 let level=23,legacy=false;
 const tasks=[['login',1,10],['questions',3,20],['questions',5,30],['tokens',5000,10],['tokens',20000,10],['tokens',50000,20]].map(([metric,target,xp],i)=>({code:'task-'+i,name:['今日启航','深入交流','持续推进','轻量协作','深度协作','充分协作'][i],metric,target,xp,period:'day',progress:metric==='login'?1:metric==='questions'?4:21000,awarded:metric==='login'||metric==='questions'&&target<=4||metric==='tokens'&&target<=21000}));
 const all=[7,30,60,90,180].map((target,i)=>({code:'milestone-'+i,name:'累计访问',metric:'login',period:'all',target,progress:8,xp:0,reward:'足迹收藏章 · '+(i+1)+' 阶',awarded:target<=8}));
 const growth=()=>({xp:level===50?20000:threshold(level)+27,level,level_start:threshold(level),next_level:threshold(Math.min(50,level+1)),started_at:'2026-09-01T00:00:00Z',tasks:[...tasks,...all],ledger:[],days:[],rule:legacy?'growth-v1':'growth-v2',note:'测试规则',open_level:50,planned_level:100,daily_limit:legacy?49:100,today_xp:55,effective_at:'2026-09-22T00:00:00+08:00',upcoming_tasks:legacy?tasks:undefined});
 await context.route('**/api/**',async route=>{const p=new URL(route.request().url()).pathname;let data={};if(p.endsWith('/session'))data={owner:'growth123@example.com',expires_at:'2030-01-01T00:00:00Z'};else if(p.endsWith('/instances'))data={instances:[{id:1,name:'验收实例',type:'hermes',status:'running',instance_mode:'lite',created_at:'2026-09-01T00:00:00Z'}],total:1};else if(p.includes('/growth'))data=growth();else if(p.endsWith('/dashboard'))data={summary:{questions:5,total_tokens:50000,sessions:2,workday_streak:1},trend:[],last_activity:{},coverage:{questions_supported:true,partial:false},insights:{days:[],comparison:{},window:{kind:'day'}}};else if(p.includes('lifecycle-operation')){await route.fulfill({status:404,json:{error:'none'}});return}await route.fulfill({json:{data}})});
 const url=(process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38471')+'/ieisystem/list-instances?view=overview&dashboard_tab=growth';
 await page.goto(url);await page.locator('.owner-growth-path').waitFor();
 assert.equal(await page.locator('.owner-daily-grid .owner-task').count(),6);assert.equal(await page.locator('.owner-pager').count(),0);
 assert.equal(await page.locator('.owner-daily-grid').evaluate(e=>getComputedStyle(e).gridTemplateColumns.split(' ').length),2);
 assert(await page.locator('.owner-daily-grid .my-2').evaluateAll(es=>es.every(e=>e.getBoundingClientRect().height>=4)));
 assert.equal(await page.locator('.owner-path-nodes button').count(),50);
 assert.equal(await page.locator('.owner-path-axis').count(),1);assert.deepEqual(await page.locator('.owner-axis-label').allTextContents(),['10','20','30','40','50']);
 assert.match(await page.locator('[aria-current=step]').getAttribute('aria-label'),/Lv.23/);
 await page.locator('[aria-current=step]').hover();assert.equal(await page.locator('[role=tooltip]:visible').count(),1);
 await page.locator('.owner-path-nodes button').last().click();await page.getByRole('dialog').waitFor();assert.match(await page.getByRole('dialog').innerText(),/17,640/);await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'累计任务',exact:true}).click();assert.equal(await page.getByText('足迹收藏章 · 1 阶',{exact:true}).count(),1);await page.getByRole('button',{name:'下一页',exact:true}).click();await page.getByRole('button',{name:'每日任务',exact:true}).click();
 for(const viewport of [{width:1920,height:1080},{width:1440,height:900},{width:1366,height:768}]){
  await page.setViewportSize(viewport);await page.mouse.move(0,0);await page.waitForTimeout(100);
  await page.screenshot({path:'.tmp/growth-r10-'+viewport.width+'.png',fullPage:true});
  const m=await page.evaluate(()=>({width:innerWidth,height:innerHeight,sw:document.documentElement.scrollWidth,sh:document.documentElement.scrollHeight,clipped:[...document.querySelectorAll('.owner-panel,.owner-task-panel,.owner-growth-path')].filter(e=>e.scrollHeight>e.clientHeight+2).map(e=>[e.className,e.clientHeight,e.scrollHeight])}));
  assert(m.sw<=m.width&&m.sh<=m.height+1,JSON.stringify(m));assert.deepEqual(m.clipped,[],JSON.stringify(m));
  const medal=await page.locator('.owner-welcome .owner-medal').boundingBox();const identity=await page.locator('.owner-identity-copy').boundingBox();
  assert(medal.height===72&&medal.x+medal.width<=identity.x,'Medal must be larger and always on the left');
  assert.equal(await page.getByText(/欢迎回来/).count(),0);assert.equal(await page.locator('.owner-path-locate').count(),1);
  assert.equal(await page.locator('.owner-path-nodes button>span').allTextContents().then(a=>a.join('')),'');
  await page.getByRole('button',{name:'累计任务',exact:true}).click();
  assert.equal(await page.locator('.owner-cumulative-grid .owner-task').count(),4);
  const cumulative=await page.locator('.owner-task-panel,.owner-cumulative-grid,.owner-cumulative-grid .owner-task').evaluateAll(es=>es.filter(e=>e.scrollHeight>e.clientHeight+2).map(e=>[e.className,e.clientHeight,e.scrollHeight]));
  assert.deepEqual(cumulative,[],'Cumulative four-card overflow '+JSON.stringify(cumulative));
  await page.screenshot({path:'.tmp/growth-four-tasks-'+viewport.width+'.png',fullPage:true});
  await page.getByRole('button',{name:'每日任务',exact:true}).click();
 }
 level=50;await page.reload();await page.getByText('已达当前开放等级 · XP 继续累计',{exact:true}).waitFor();assert.equal(await page.getByText(/距下级 -/).count(),0);assert.match(await page.locator('[aria-current=step]').getAttribute('aria-label'),/Lv.50/);
 legacy=true;await page.reload();await page.getByRole('button',{name:/新任务于/}).click();await page.getByRole('dialog').waitFor();assert.match(await page.getByRole('dialog').innerText(),/50,000/);await page.keyboard.press('Escape');
 legacy=false;level=1;await page.setViewportSize({width:390,height:844});await page.reload();await page.locator('.owner-growth-path').waitFor();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:'.tmp/growth-r10-mobile.png',fullPage:true});
 assert.deepEqual(errors,[]);await browser.close();console.log('PASS: six daily cards in two columns, collectible pagination, 50-level path, locate/tooltip/detail, 50 cap, scheduled transition, enlarged medals, desktop single-screen and mobile width');
})().catch(e=>{console.error(e);process.exit(1)});
