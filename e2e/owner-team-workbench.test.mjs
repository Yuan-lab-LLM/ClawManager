// Real React/Vite/browser; API fixtures exercise UI races and never call a model.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { chromium } from 'playwright';
import { createServer } from '../frontend/node_modules/vite/dist/node/index.js';

const frontend = path.resolve('../frontend'), temp = fs.mkdtempSync(path.join(os.tmpdir(), 'team-workbench-'));
let server, browser;
const spec = { members: Array.from({ length: 6 }, (_, i) => ({ memberId: `m${i}`, displayName: i ? `调研与资料分析成员${i}` : '团队负责人', role: i ? 'worker' : 'leader', isLeader: i === 0, summary: '负责可追溯的数据搜集与核对。', mission: '保持原始来源，按任务要求交付。', responsibilities: Array(8).fill('核实来源、时间、内容与任务目标的一致性。'), boundaries: ['保留原始引用'], expectedInputs: ['用户目标'], deliverables: ['报告'], acceptanceCriteria: ['可核对'], collaborationNotes: ['遵循 Leader 分工'] })) };
let templates = [6,2,3,4,5].map((count,index) => ({ id:index+1, name:`模板${index+1}`, intent:`目标${index+1}`, resolved_member_count:count, revision:1, spec:{...spec,members:spec.members.slice(0,count)} }));
let mutations = 0, generationRequests = 0, failGeneration = false, failTemplateRead = false, holdRefresh = null;
try {
  const component = '/@fs/' + path.join(frontend, 'src/components/teams/OwnerTeamTemplates.tsx').replaceAll('\\', '/');
  const notices = '/@fs/' + path.join(frontend, 'src/components/teams/OwnerTeamNotifications.tsx').replaceAll('\\', '/');
  const styles = '/@fs/' + path.join(frontend, 'src/components/teams/owner-team-workspace.css').replaceAll('\\', '/');
  fs.writeFileSync(path.join(temp, 'fixture.tsx'), `import React,{useState}from'react';import{createRoot}from'react-dom/client';import{OwnerTeamTemplates}from${JSON.stringify(component)};import${JSON.stringify(styles)};import{OwnerTeamNotifications,useOwnerTeamFeedback}from${JSON.stringify(notices)};
function Notices(){const f=useOwnerTeamFeedback();return <><button onClick={f.openHistory}>操作记录</button><button onClick={()=>f.notify({id:'active-fixture',operationID:'op-ui-fixture',kind:'pending',title:'通知进行中',message:'关闭通知不取消操作'})}>显示进行中</button><button onClick={()=>f.notify({id:'active-fixture',operationID:'op-ui-fixture',attemptCount:1,kind:'pending',title:'新尝试进行中',message:'显式重试进入新尝试'})}>下一次尝试</button><button onClick={()=>f.notify({id:'active-fixture',operationID:'op-ui-fixture',attemptCount:0,terminal:true,kind:'success',title:'迟到的旧完成',message:'不能覆盖新尝试'})}>旧完成</button><button onClick={()=>f.notify({id:'error-fixture',kind:'error',title:'持久错误提示',message:'错误保留可检查'})}>显示失败</button><button onClick={()=>f.notify({id:'success-fixture',kind:'success',title:'短暂成功提示',message:'自动收起'})}>显示成功</button></>}
function App(){const[visible,setVisible]=useState(true),[owner,setOwner]=useState('alice@example.com');return <><button onClick={()=>setVisible(v=>!v)}>切换标签</button><button onClick={()=>setOwner(v=>v==='alice@example.com'?'bob@example.com':'alice@example.com')}>切换所有者</button><OwnerTeamNotifications key={owner} owner={owner}><Notices/><main style={{height:650,maxWidth:1050,margin:'12px auto',display:'flex',minHeight:0}}><section className="ot-workspace">{visible&&<OwnerTeamTemplates key={owner} owner={owner} canGenerate onOperation={()=>{}}/>}</section></main></OwnerTeamNotifications></>};createRoot(document.getElementById('root')).render(<App/>);`);
  server = await createServer({ root: frontend, configFile: path.join(frontend, 'vite.config.ts'), server: { host: '127.0.0.1', port: 0, fs: { allow: [frontend, temp] } }, plugins: [{ name: 'owner-team-layout-test', configureServer(s) { s.middlewares.use('/__team_workbench__', async (req, res) => { res.setHeader('Content-Type', 'text/html'); res.end(await s.transformIndexHtml('/__team_workbench__', `<html><body style="margin:0"><div id="root"></div><script type="module" src="/@fs/${path.join(temp, 'fixture.tsx').replaceAll('\\', '/')}"></script></body></html>`)); }); } }] });
  await server.listen(); const address = server.httpServer.address();
  browser = await chromium.launch({ channel: 'chrome', headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } }), errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/ieisystem/**', async route => {
    const request = route.request(), url = new URL(request.url()), method = request.method(), body = request.postDataJSON();
    const send = (data, status = 200) => route.fulfill({ status, json: { success: status < 400, data, message: status === 409 ? '模板版本冲突，请重新读取' : undefined } });
    if (url.pathname.endsWith('/custom-team-templates') && method === 'GET') {
      if (holdRefresh) await new Promise(resolve => { holdRefresh.release = resolve; });
      return send({ templates });
    }
    if (/\/team-operations\//.test(url.pathname)) return send({ operation_id: 'op-ui-fixture', status: failGeneration ? 'failed' : 'succeeded', template_id: 1, error_message: failGeneration ? '生成暂不可用，已保留模板' : undefined });
    const match = url.pathname.match(/\/custom-team-templates\/(\d+)/), t = templates.find(t => t.id === Number(match?.[1]));
    if (match && method === 'GET') return failTemplateRead ? route.fulfill({status:503,json:{message:'模板读取暂不可用'}}) : send(t);
    if (match && method === 'PATCH') { mutations++; if (body.expected_revision !== t.revision) return send(null, 409); Object.assign(t, { name: body.name, revision: t.revision + 1 }); return send(t); }
    if (match && method === 'POST') { generationRequests++; return send({ operation_id: 'op-ui-fixture', status: 'queued' }, 202); }
    if (match && method === 'PUT') { if (body.expected_revision !== t.revision) return send(null, 409); t.revision++; return send(t); }
    return send(null, 404);
  });
  holdRefresh = {};
  await page.goto(`http://127.0.0.1:${address.port}/__team_workbench__`);
  for(let i=0;i<40&&!holdRefresh.release;i++) await page.waitForTimeout(50);
  assert.equal(await page.getByRole('button',{name:'自定义团队模板',exact:true}).isDisabled(),true);
  assert.equal(await page.getByLabel('团队目标与分工',{exact:true}).isVisible(),false,'empty editor exposed before draft restore');
  const releaseInitial=holdRefresh.release; holdRefresh=null; releaseInitial();
  const name = page.getByLabel('模板名称', { exact: true }), intent = page.getByLabel('团队目标与分工', { exact: true });
  await name.waitFor(); await name.fill('保留名称草稿'); await intent.fill('保留目标草稿');
  await page.getByRole('button', { name: '刷新模板', exact: true }).click(); await page.getByText('模板列表已刷新，编辑内容已保留。').waitFor(); assert.equal(await name.inputValue(), '保留名称草稿');
  await page.getByRole('button', { name: '切换标签', exact: true }).click(); await page.getByRole('button', { name: '切换标签', exact: true }).click(); await name.waitFor(); assert.equal(await intent.inputValue(), '保留目标草稿');
  await page.getByRole('button', { name: '模板2', exact: false }).first().click(); await name.fill('另一个模板的草稿'); await page.getByRole('button', { name: /模板1.*6 人/ }).click(); assert.equal(await name.inputValue(), '保留名称草稿');
  assert.equal(await page.evaluate(() => window.dispatchEvent(new Event('clawmanager:before-page-refresh', { cancelable: true }))), false);
  await page.getByRole('button', { name: '仅保存名称', exact: true }).click(); await page.getByText('模板名称已保存。').waitFor(); assert.equal(mutations, 1); assert.equal(generationRequests, 0, 'rename must never generate');
  templates[0].revision++; await name.fill('冲突时保留的名称'); await page.getByRole('button', { name: '仅保存名称', exact: true }).click(); await page.getByRole('alert').waitFor(); assert.equal(await name.inputValue(), '冲突时保留的名称');
  await page.getByRole('button', { name: '读取最新版本并保留输入' }).click(); await page.getByText(/已读取最新版本/).waitFor(); assert.equal(await intent.inputValue(), '保留目标草稿');
  await page.getByRole('tab',{name:/团队负责人/}).click(); await page.getByRole('button', { name: '调整职责', exact: true }).first().click(); const dialog = page.getByRole('dialog', { name: '调整成员职责' });
  await dialog.getByLabel('调整说明').fill('核实每条引用'); page.once('dialog', d => d.dismiss()); await page.keyboard.press('Escape'); assert.equal(await dialog.count(), 1); page.once('dialog', d => d.accept()); await page.keyboard.press('Escape'); await dialog.waitFor({ state: 'detached' });
  assert.equal(await page.locator('.ot-member-tab').count(),6);
  const geometry=await page.locator('.ot-member-tab').evaluateAll(es=>{const ys=es.map(e=>Math.round(e.getBoundingClientRect().y));return[...new Set(ys)].map(y=>ys.filter(v=>v===y).length)});assert.deepEqual(geometry,[3,3]);
  const requestCount=generationRequests;
  for(let i=0;i<6;i++){await page.locator('.ot-member-tab').nth(i).click();assert.equal(await page.locator('.ot-member-tab[aria-selected=true]').count(),1);assert.equal(generationRequests,requestCount)}
  await page.getByRole('tab',{name:'协作与边界',exact:true}).click();await page.getByText('所需输入',{exact:true}).waitFor();await page.getByRole('tab',{name:'职责与交付',exact:true}).click();
  const layout=await page.locator('.ot-template-area').boundingBox();await page.getByRole('button',{name:'关闭通知',exact:true}).click();assert.deepEqual(await page.locator('.ot-template-area').boundingBox(),layout,'toast dismissal reflowed editor');
  assert.ok(await page.evaluate(()=>Object.keys(sessionStorage).some(k=>k.startsWith('owner-team-notices:alice@example.com'))),'dismissal lost notice history');
  for (const width of [1920, 1440, 1024, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    const result = await page.locator('.ot-member-detail-scroll').evaluate(e => { const body = e.closest('.ot-template-editor').getBoundingClientRect(), footer = e.closest('.ot-template-area').querySelector('.ot-template-footer').getBoundingClientRect(), work = e.closest('.ot-workspace').getBoundingClientRect(); return { overflow: document.documentElement.scrollWidth > innerWidth + 1, scroll: e.scrollHeight > e.clientHeight, body: body.bottom, footer: footer.top, end: footer.bottom, outer: work.bottom }; });
    if(width<=700){const select=page.getByLabel('当前模板成员',{exact:true});assert.equal(await select.locator('option').count(),6);await select.selectOption('m0');await select.selectOption('m5');}
    assert.equal(result.overflow, false, `overflow ${width}`); assert.equal(result.scroll, true, `missing internal scroll ${width}`); assert.ok(result.body <= result.footer + 1, `footer overlap ${width}`); assert.ok(result.end <= result.outer + 1, `footer escaped ${width}`);
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByLabel('成员数量',{exact:true}).fill('4');assert.equal(await page.locator('.ot-member-tab').count(),6,'requested count must not invent members');await page.getByText('当前模板 6 人；本次修订期望 4 人，生成成功后更新。').waitFor();await page.getByLabel('成员数量',{exact:true}).fill('6');
  failGeneration = true;
  await page.getByRole('button', { name: '按说明修订模板', exact: true }).dblclick(); await page.getByRole('alert').waitFor(); assert.equal(generationRequests, 1, 'double click created duplicate generation'); assert.equal(await name.inputValue(), '冲突时保留的名称');
  failGeneration = false; await page.getByRole('button', { name: '按说明修订模板', exact: true }).click(); await page.getByText('模板已更新，已创建的团队保持不变。').waitFor(); assert.equal(generationRequests, 2); assert.equal(await name.inputValue(), templates[0].name);
  await page.getByRole('button', { name: '重新生成成员', exact: true }).click(); await page.getByText('模板已更新，已创建的团队保持不变。').waitFor(); assert.equal(generationRequests, 3);
  failTemplateRead=true;await page.getByRole('button',{name:'按说明修订模板',exact:true}).click();await page.getByText('操作已完成，模板暂未刷新',{exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>JSON.parse(sessionStorage.getItem('owner-team-notices:alice@example.com')).find(x=>x.id==='op-ui-fixture').kind),'success','post-success read failure overwrote operation success');
  const beforeReadRetry=generationRequests;failTemplateRead=false;await page.getByRole('button',{name:'读取最新版本并保留输入',exact:true}).click();await page.getByText(/已读取最新版本/).waitFor();assert.equal(generationRequests,beforeReadRetry,'reading after completion repeated generation');
  const presentationRequests=generationRequests;
  for(const [index,count] of [6,2,3,4,5].entries()){
    await page.locator('.ot-template-list .ot-master-item').nth(index).click();assert.equal(await page.locator('.ot-member-tab').count(),count);
    const height=(await page.locator('.ot-template-area').boundingBox()).height;
    for(let member=0;member<count;member++){await page.locator('.ot-member-tab').nth(member).click();assert.equal(await page.locator('.ot-member-tab[aria-selected=true]').count(),1);assert.equal((await page.locator('.ot-template-area').boundingBox()).height,height)}
  }
  assert.equal(generationRequests,presentationRequests,'member selection dispatched work');
  // Old responses must not replace a different owner's editor after unmount.
  holdRefresh = {}; await page.getByRole('button', { name: '刷新模板', exact: true }).click();
  await page.waitForTimeout(50); const release = holdRefresh.release; holdRefresh = null;
  await page.getByRole('button', { name: '切换所有者', exact: true }).click(); await name.waitFor(); if (release) release(); await page.waitForTimeout(80); assert.equal(await name.inputValue(), templates[0].name, 'foreign-owner draft leaked');
  await page.clock.install();
  await page.getByRole('button',{name:'显示进行中',exact:true}).click();await page.getByText('通知进行中',{exact:true}).waitFor();await page.getByRole('button',{name:'关闭通知',exact:true}).click();
  await page.getByRole('button',{name:'操作记录',exact:true}).click();const history=page.getByRole('dialog',{name:'操作记录',exact:true});await history.waitFor();await history.getByRole('button',{name:'检查最新进度',exact:true}).first().click();await page.getByText('操作已完成',{exact:true}).waitFor();
  await page.getByRole('button',{name:'显示进行中',exact:true}).click();assert.equal(await page.getByText('通知进行中',{exact:true}).count(),0,'late progress revived completed notice');
  await page.getByRole('button',{name:'下一次尝试',exact:true}).click();await page.getByText('新尝试进行中',{exact:true}).waitFor();await page.getByRole('button',{name:'旧完成',exact:true}).click();assert.equal(await page.getByText('迟到的旧完成',{exact:true}).count(),0);await page.getByText('新尝试进行中',{exact:true}).waitFor();
  await page.getByRole('button',{name:'显示失败',exact:true}).click();await page.clock.fastForward(7000);await page.getByText('持久错误提示',{exact:true}).waitFor();
  await page.getByRole('button',{name:'显示成功',exact:true}).click();await page.getByText('短暂成功提示',{exact:true}).waitFor();await page.clock.fastForward(7000);assert.equal(await page.locator('.ot-floating-notice').count(),0,'successful notice did not dismiss');
  assert.deepEqual(errors, []); console.log('PASS compact Team UI / notifications: actual member counts2-6,3x2 grid,mobile selection,toast auto/persistent/history/terminal guard, scoped drafts, rename without model, CAS conflict/input retention, cancel guard, six widths/internal scroll, duplicate-submit fence, failed generation retention, owner/unmount fencing. API/model boundary simulated.');
} finally { await browser?.close(); await server?.close(); assert.equal(path.dirname(path.resolve(temp)), path.resolve(os.tmpdir())); assert.ok(path.basename(temp).startsWith('team-workbench-')); fs.rmSync(temp, { recursive: true, force: true }); }
