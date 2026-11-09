// Actual production pages with local APIs only; no live model/Team mutation.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { chromium } from 'playwright';

const dist=path.resolve(process.env.TEAM_WORKSPACE_DIST||path.resolve(import.meta.dirname,'../frontend/dist'));
const server=http.createServer((req,res)=>{
  let file=path.join(dist,decodeURIComponent(new URL(req.url,'http://local').pathname));
  if(!file.startsWith(dist+path.sep)){res.writeHead(403).end();return;}
  if(!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.png':'image/png','.svg':'image/svg+xml'})[path.extname(file)]||'application/octet-stream');
  fs.createReadStream(file).pipe(res);
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const origin=`http://127.0.0.1:${server.address().port}`;
const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||'chrome'});
const page=await browser.newPage({viewport:{width:1920,height:1080}});
const errors=[];page.on('pageerror',error=>errors.push(String(error)));
await page.addInitScript(()=>{localStorage.setItem('access_token','local-fixture-only');localStorage.setItem('language','zh-CN');});
const member=(id,name,role,icon,isLeader=false)=>({memberId:id,displayName:name,role,isLeader,summary:`${name}的职责`,mission:'保持业务使命',responsibilities:['核对证据'],boundaries:['不替代业务验收'],expectedInputs:['来源'],deliverables:['报告'],acceptanceCriteria:['可追溯'],collaborationNotes:['汇报给主编'],capabilityTags:[],...(icon?{iconKey:icon,autoIconKey:icon}:{})});
const fixture={id:9,name:'每日资讯',intent:'追踪资讯',resolved_member_count:3,revision:1,created_at:new Date().toISOString(),updated_at:new Date().toISOString(),spec:{schemaVersion:1,name:'每日资讯',summary:'独立采集与筛选',resolvedMemberCount:3,members:[member('leader','主编','leader','leader',true),member('news-collector','资讯采集员','news-collector','news'),member('content-filter','内容筛选员','content-filter','filter')]}};
const templates=[fixture];const mutations=[];let created=null,failNextIcon=false;
await page.route('**/api/v1/**',async route=>{
  const request=route.request(),url=new URL(request.url()),send=data=>route.fulfill({json:{data}});
  if(url.pathname.endsWith('/auth/me'))return send({id:1,username:'admin',role:'user',status:'active'});
  if(url.pathname.endsWith('/version'))return send({version:'badge-browser-acceptance'});
  if(['/api/v1/openclaw-configs/resources','/api/v1/openclaw-configs/bundles'].includes(url.pathname))return send([]);
  if(url.pathname.endsWith('/system-settings/images'))return send({items:['openclaw','hermes'].map((type,i)=>({id:i+1,instance_type:type,runtime_type:'gateway',image:`fixture/${type}:latest`,is_enabled:true,is_default:true,name:type}))});
  if(url.pathname==='/api/v1/custom-team-templates'&&request.method()==='GET')return send({templates});
  if(url.pathname.endsWith('/members/news-collector/icon')){
    const body=request.postDataJSON();mutations.push([url.pathname,body]);
    assert.equal(body.expected_revision,fixture.revision);
    if(failNextIcon){failNextIcon=false;return route.fulfill({status:409,json:{error:'revision conflict'}});}
    fixture.spec.members[1].iconOverride=body.icon_override;
    fixture.spec.members[1].iconKey=body.icon_override||fixture.spec.members[1].autoIconKey;
    fixture.revision++;return send(fixture);
  }
  if(url.pathname==='/api/v1/teams'&&request.method()==='POST'){
    created=request.postDataJSON();
    // Leave the actual create form mounted so failure handling can be checked
    // without entering an unrelated fixture Team.
    return route.fulfill({status:503,json:{error:'fixture captured creation request'}});
  }
  return send({});
});
const visiblePage=()=>page.locator('main:visible').last();
const picker=(scope,label)=>scope.getByRole('button',{name:label,exact:true});
const options=label=>page.getByRole('dialog',{name:`${label}选项`,exact:true});
const choose=async(scope,label,key)=>{
  await picker(scope,label).click();
  await options(label).waitFor();
  await options(label).locator(`[data-icon-choice="${key}"]`).click();
  await options(label).waitFor({state:'hidden'});
  await page.waitForFunction(()=>![...document.querySelectorAll('.team-avatar-picker-trigger')].some(b=>b.disabled));
};
try{
  await page.goto(`${origin}/teams/custom-templates`);
  const article=()=>visiblePage().locator('article').filter({hasText:'资讯采集员'});
  const header=()=>article().locator('button[aria-expanded]').filter({hasText:'资讯采集员'});
  const label='资讯采集员职能图标';
  await picker(article(),label).waitFor();
  assert.equal(await article().getByRole('combobox').count(),0,'removed the separate selection region');
  assert.equal(await header().getAttribute('aria-expanded'),'false');
  await picker(article(),label).click();
  await options(label).waitFor();
  assert.equal(await header().getAttribute('aria-expanded'),'false','avatar does not expand member details');
  assert.equal(await options(label).innerText(),'','choices are icon-only');
  assert.equal(await options(label).getByRole('button').count(),21);
  assert.equal(await options(label).locator('[data-icon-choice=auto]').getAttribute('aria-pressed'),'true');
  for(const identity of ['user','leader','system','unknown'])assert.equal(await options(label).locator(`[data-icon-choice=${identity}]`).count(),0);
  await page.keyboard.press('ArrowRight');
  assert.equal(await page.evaluate(()=>document.activeElement.dataset.iconChoice),'developer');
  await page.keyboard.press('End');
  assert.equal(await page.evaluate(()=>document.activeElement.dataset.iconChoice),'general');
  await page.keyboard.press('Home');
  assert.equal(await page.evaluate(()=>document.activeElement.dataset.iconChoice),'auto');
  await page.evaluate(()=>window.scrollBy(0,16));
  await page.waitForFunction(()=>[...document.querySelectorAll('.team-avatar-picker-options')].some(p=>p.matches(':popover-open')));
  await page.keyboard.press('Escape');
  await options(label).waitFor({state:'hidden'});
  await page.waitForFunction(()=>document.activeElement?.getAttribute('aria-expanded')==='false');
  assert.equal(await picker(article(),label).getAttribute('aria-expanded'),'false');
  assert.equal(await visiblePage().locator('article').filter({hasText:'主编'}).locator('[data-icon-key=leader]').count(),1);
  assert.equal(await visiblePage().locator('article').filter({hasText:'主编'}).locator('.team-avatar-picker-trigger').count(),0,'canonical Leader stays fixed');
  await choose(article(),label,'data');
  await page.waitForFunction(()=>[...document.querySelectorAll('article')].some(a=>a.innerText.includes('资讯采集员')&&a.querySelector('[data-icon-key=data]')));
  assert.equal(mutations.length,1);
  assert.equal(fixture.spec.members[1].mission,'保持业务使命');
  await header().click();
  assert.equal(await header().getAttribute('aria-expanded'),'true');
  assert.equal(await article().getByText('仅修改显示徽章',{exact:false}).count(),0,'removed expanded explanatory block');
  await page.reload();
  await picker(article(),label).waitFor();
  assert.equal(await picker(article(),label).locator('[data-icon-key=data]').count(),1,'manual icon survives reload');
  failNextIcon=true;
  await choose(article(),label,'security');
  await page.locator('.border-red-200:visible').filter({hasText:'这个模板刚刚发生了变化，请重新选择后再操作。'}).waitFor();
  assert.equal(await picker(article(),label).locator('[data-icon-key=data]').count(),1,'failed save preserves confirmed icon');
  await choose(article(),label,'auto');
  await page.waitForFunction(()=>[...document.querySelectorAll('article')].some(a=>a.innerText.includes('资讯采集员')&&a.querySelector('[data-icon-key=news]')));
  // Native top-layer popup is never clipped by the member card overflow.
  await picker(article(),label).click();
  const box=await options(label).boundingBox();
  assert.ok(box&&box.x>=12&&box.y>=12&&box.x+box.width<=1920-12);
  await picker(visiblePage().locator('article').filter({hasText:'内容筛选员'}),'内容筛选员职能图标').click();
  await options('内容筛选员职能图标').waitFor();
  assert.equal(await options(label).isVisible(),false,'only one icon popup is open');
  await page.keyboard.press('Escape');
  await choose(article(),label,'editor');
  await page.waitForFunction(()=>[...document.querySelectorAll('article')].some(a=>a.innerText.includes('资讯采集员')&&a.querySelector('[data-icon-key=editor]')));
  fs.mkdirSync(path.resolve(import.meta.dirname,'reports'),{recursive:true});
  await picker(article(),label).click();
  await options(label).waitFor();
  await page.screenshot({path:path.resolve(import.meta.dirname,'reports/team-avatar-picker-editor.png'),fullPage:true});
  await visiblePage().getByRole('heading',{name:'成员职责',exact:true}).click();
  await options(label).waitFor({state:'hidden'});
  await page.goto(`${origin}/teams/new?template=custom-9`);
  const form=page.locator('form:visible');
  await picker(form,label).waitFor();
  assert.equal(await form.getByRole('combobox',{name:label}).count(),0);
  assert.equal(await picker(form,label).locator('[data-icon-key=editor]').count(),1);
  assert.equal(await form.locator('[data-icon-key=editor]').count(),1,'one avatar, not duplicated preview');
  await choose(form,label,'data');
  await form.getByRole('button',{name:'创建',exact:true}).click();
  await page.waitForFunction(()=>document.body.innerText.includes('fixture captured creation request'));
  assert.ok(created,'actual create request captured');
  assert.deepEqual(created.members.map(m=>m.icon_key),['leader','data','filter']);
  assert.equal(created.members[1].role,'news-collector');
  assert.equal(created.members[1].role_profile.mission,'保持业务使命');
  assert.ok(!JSON.stringify(created.members[1].environment_overrides||{}).includes('icon'),'icon is not a Runtime environment field');
  // Old saved custom specs have no icon fields. Resolve from known roles/names,
  // while unfamiliar members remain neutral and never become fake Leaders.
  delete fixture.spec.members[1].iconKey;delete fixture.spec.members[1].autoIconKey;delete fixture.spec.members[1].iconOverride;
  fixture.spec.members[2].displayName='自定义专家';fixture.spec.members[2].role='unfamiliar-specialist';
  delete fixture.spec.members[2].iconKey;delete fixture.spec.members[2].autoIconKey;
  await page.goto(`${origin}/teams/new?template=custom-9`);
  await picker(form,label).waitFor();
  assert.equal(await form.locator('[data-icon-key=news]').count(),1,'old custom role/name gets one automatic avatar');
  assert.equal(await form.locator('[data-icon-key=general]').count(),1);
  assert.equal(await form.locator('[data-icon-key=leader]').count(),1);
  for(const size of [{width:1440,height:900},{width:390,height:844}]){
    await page.setViewportSize(size);
    assert.ok(await form.evaluate(el=>el.scrollWidth<=el.clientWidth+1),'creation page has no new icon-driven horizontal overflow');
    await picker(form,label).click();
    const rect=await options(label).boundingBox();
    assert.ok(rect&&rect.x>=12&&rect.y>=12&&rect.x+rect.width<=size.width-12&&rect.y+rect.height<=size.height-12,'popup stays inside viewport');
    assert.equal(await options(label).innerText(),'');
    if(size.width===390)await page.screenshot({path:path.resolve(import.meta.dirname,'reports/team-avatar-picker-mobile.png'),fullPage:true});
    await page.keyboard.press('Escape');
  }
  assert.deepEqual(errors,[]);
  console.log('PASS: avatar-only popup on actual editor/create pages, no redundant controls, separate expand, keyboard/dismiss/single popup/mobile bounds, choice/clear/reload/failure, canonical Leader, legacy fallback, role/env isolation.');
}catch(error){
  console.error(await page.evaluate(()=>({scroll:window.scrollY,popups:[...document.querySelectorAll('.team-avatar-picker-options')].map(p=>({name:p.getAttribute('aria-label'),open:p.matches(':popover-open'),rect:p.getBoundingClientRect().toJSON(),scrollHeight:p.scrollHeight,clientHeight:p.clientHeight})),focused:{tag:document.activeElement?.tagName,label:document.activeElement?.getAttribute('aria-label')}})));
  fs.mkdirSync(path.resolve(import.meta.dirname,'reports'),{recursive:true});
  await page.screenshot({path:path.resolve(import.meta.dirname,'reports/team-avatar-picker-failure.png'),fullPage:true});
  throw error;
}finally{await browser.close();server.close();}
