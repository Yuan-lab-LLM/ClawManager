// Run against the Vite dev server. Set PLAYWRIGHT_MODULE / CHROMIUM_PATH when
// reusing a local browser installation. API fixtures never contact a live Team.
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright-core');
(async () => {
  const browser = await chromium.launch({ headless: true, ...(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}) });
  const page = await browser.newPage({ viewport: { width: 1760, height: 950 } });
  const errors = []; page.on('pageerror', error => errors.push(String(error)));
  const member = i => ({memberId: 'member-'+i, displayName: i ? '论文精读成员'+i : '论文精读负责人', role: i ? 'worker' : 'leader', isLeader: !i, summary: '核对论文方法与原文依据。', mission: '形成可复核的精读笔记。', responsibilities: ['核对关键假设','逐项标明原文依据'], deliverables: ['精读笔记'], acceptanceCriteria: ['引用正确'], expectedInputs: ['论文原文'], collaborationNotes: ['正式提交给负责人'], boundaries: ['不编造实验结果'], capabilityTags: ['research']});
  let template = {id: 1, name: 'Attention Is All You Need 论文精读与实验依据核验团队（六人成员目录）', intent: '精读论文并交叉核对方法与实验', resolved_member_count: 6, revision: 1, spec: {schemaVersion: 1, name: '论文精读团队', summary: '论文精读', members: Array.from({length: 6}, (_, i) => member(i))}};
  let conflict = false;
  await page.route('**/api/v1/ieisystem/**', async route => {
    const req=route.request(),path=new URL(req.url()).pathname,method=req.method();
    let data;
    if(path.endsWith('/custom-team-templates') && method==='GET')data={templates:[template]};
    else if(path.endsWith('/custom-team-templates/1') && method==='PATCH') {
      if(conflict)return route.fulfill({status:409,json:{message:'模板版本已变化，请读取最新版本'}});
      const body=req.postDataJSON(); assert.equal(body.expected_revision,template.revision);template={...template,name:body.name,revision:template.revision+1};data=template;
    } else if(path.endsWith('/custom-team-templates/1') && method==='GET')data=template;
    else if(path.endsWith('/teams/99'))data={team:{id:99,name:'论文精读验收团队',status:'running'},leader_member_id:'member-0',members:template.spec.members.map((m,i)=>({id:i+1,member_key:m.memberId,display_name:m.displayName,role:m.role,status:'idle',availability:'idle',runtime_type:'openclaw',instance_mode:'lite'})),tasks:[],events:[],work_items:[]};
    else if(path.endsWith('/team-templates'))data={templates:[{key:'fixture',name:'论文精读',revision:1,members:template.spec.members.map(m=>({member_id:m.memberId,name:m.displayName,role:m.role,is_leader:m.isLeader,runtime_type:'openclaw',instance_mode:'lite'}))}]};
    else if(path.includes('/members/') && path.endsWith('/adjust')) {
      template={...template,revision:template.revision+1};data={operation_id:'adjust-fixture',status:'succeeded',template_id:1};
    } else if(path.endsWith('/team-operations/adjust-fixture'))data={operation_id:'adjust-fixture',status:'succeeded',template_id:1};
    else throw Error('Unexpected fixture request '+method+' '+path);
    return route.fulfill({json:{success:true,data}});
  });
  await page.goto((process.env.UI_BASE_URL || 'http://127.0.0.1:9059')+'/scripts/fixtures/owner-template-workbench.html');
  const goal=page.getByRole('textbox',{name:'团队目标与分工'});
  await goal.waitFor();
  const toolbar=page.locator('.ot-template-selection-toolbar');
  assert(await toolbar.evaluate(n=>{const nodes=[n.querySelector('h3'),...n.querySelectorAll('button')],centers=nodes.map(x=>{const r=x.getBoundingClientRect();return r.top+r.height/2});return Math.max(...centers)-Math.min(...centers)<1&&n.scrollWidth<=n.clientWidth}),'template title/actions must share one row');
  assert.equal(await page.getByRole('combobox',{name:'模板成员数量'}).evaluate(n=>n.form?.id),'ot-template-form');
  assert.equal(await page.locator('.ot-template-list button').first().evaluate(n=>getComputedStyle(n).boxShadow),'none');
  if(process.env.UI_SCREENSHOT_PATH)await page.screenshot({path:process.env.UI_SCREENSHOT_PATH});
  await goal.fill('尚未应用的目标草稿');
  await page.getByRole('button',{name:'重命名模板'}).click();
  await page.getByRole('textbox',{name:'模板名称',exact:true}).fill('论文精读新名称');
  await page.getByRole('button',{name:'保存名称',exact:true}).click();
  await page.getByRole('heading',{name:'论文精读新名称'}).waitFor();
  assert.equal(await goal.inputValue(),'尚未应用的目标草稿');
  assert.equal(await page.getByRole('button',{name:'重新生成整套',exact:true}).isDisabled(),true);
  await page.getByRole('tab',{name:'论文精读成员5',exact:true}).click();
  await page.getByRole('button',{name:'调整职责',exact:true}).click();
  await page.getByRole('textbox',{name:'调整说明'}).fill('增加原文依据核验');
  await page.getByRole('button',{name:'应用调整',exact:true}).click();
  await page.getByRole('dialog',{name:'调整成员职责'}).waitFor({state:'hidden'});
  assert.equal(await goal.inputValue(),'尚未应用的目标草稿');
  assert.equal(await page.getByRole('tab',{name:'论文精读成员5',exact:true}).getAttribute('aria-selected'),'true');
  const aligned=await page.locator('.ot-editor-section-heading').evaluateAll(nodes=>Math.abs(nodes[0].getBoundingClientRect().top-nodes[1].getBoundingClientRect().top));assert(aligned<1);
  await page.locator('.ot-template-list button').first().focus();
  assert(await page.locator('.ot-template-list button').first().evaluate(b=>{const p=b.parentElement.getBoundingClientRect(),r=b.getBoundingClientRect();return r.left-p.left>=5&&r.top-p.top>=5}),'template focus ring clipped');
  conflict=true;await page.getByRole('button',{name:'重命名模板'}).click();await page.getByRole('textbox',{name:'模板名称',exact:true}).fill('冲突名称');await page.getByRole('button',{name:'保存名称',exact:true}).click();await page.getByRole('alert').filter({hasText:'模板版本已变化'}).first().waitFor();assert.equal(await goal.inputValue(),'尚未应用的目标草稿');
  page.once('dialog',d=>d.accept());await page.getByRole('dialog',{name:'重命名模板'}).getByRole('button',{name:'取消',exact:true}).click();
  await page.getByRole('button',{name:'关闭通知',exact:true}).click();
  for(const width of [1366,1024,390]) {
    await page.setViewportSize({width,height:900});const paneTab=page.getByRole('tab',{name:'成员与分工',exact:true});if(await paneTab.isVisible())await paneTab.click();
    if(width===390)await page.getByRole('combobox',{name:'当前模板成员'}).selectOption('member-4');
    const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);assert.equal(overflow,false,'horizontal overflow at '+width);
    if(width>700)assert(await toolbar.evaluate(n=>{const a=n.querySelector('.ot-template-name').getBoundingClientRect(),b=n.querySelector('.ot-actions').getBoundingClientRect();return Math.abs(a.top+a.height/2-b.top-b.height/2)<1}),'toolbar wrapped at '+width);
  }
  await page.setViewportSize({width:1366,height:850});
  await page.goto((process.env.UI_BASE_URL || 'http://127.0.0.1:9059')+'/scripts/fixtures/owner-template-workbench.html?dashboard');
  await page.getByRole('button',{name:'管理',exact:true}).click();
  const manage=page.getByRole('dialog',{name:'管理 Team'});await manage.waitFor();
  assert.equal(await manage.getByRole('button',{name:'移除成员'}).count(),0);
  await manage.getByRole('button',{name:'关闭',exact:true}).click();
  await page.getByRole('button',{name:'新建 Team',exact:true}).click();
  const create=page.getByRole('dialog',{name:'新建 Team',exact:true});await create.waitFor();
  const name=create.getByRole('textbox',{name:'团队名称',exact:true});await name.focus();
  assert(await name.evaluate(b=>{const p=b.closest('.ot-form-scroll').getBoundingClientRect(),r=b.getBoundingClientRect();return r.left-p.left>=5&&r.right<=p.right-5}),'dialog field focus clipped');
  await create.getByRole('button',{name:'取消',exact:true}).click();
  await page.getByRole('button',{name:'操作记录',exact:true}).click();await page.getByRole('dialog',{name:'操作记录',exact:true}).waitFor();await page.getByRole('dialog',{name:'操作记录',exact:true}).getByRole('button',{name:'关闭',exact:true}).click();
  assert.deepEqual(errors,[]);console.log('PASS: single-row toolbar, six-member directory, draft/conflict safety, responsive layout and shared dialogs/focus boundaries');await browser.close();
})().catch(error=>{console.error(error);process.exit(1)});
