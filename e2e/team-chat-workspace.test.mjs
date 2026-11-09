// Local browser acceptance of the real production bundle. All APIs are fixtures;
// no test mutates a live Team, workspace or model conversation.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { chromium } from 'playwright';

const dist = path.resolve(process.env.TEAM_WORKSPACE_DIST || path.resolve(import.meta.dirname, '../frontend/dist'));
const server = http.createServer((req, res) => {
  let file = path.join(dist, decodeURIComponent(new URL(req.url, 'http://local').pathname));
  if (!file.startsWith(dist + path.sep)) { res.writeHead(403).end(); return; }
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
  res.setHeader('Content-Type', ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html', '.png': 'image/png', '.svg': 'image/svg+xml' })[path.extname(file)] || 'application/octet-stream');
  fs.createReadStream(file).pipe(res);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
const errors = [];
page.on('pageerror', error => errors.push(String(error)));
await page.addInitScript(() => { localStorage.setItem('access_token', 'local-fixture-only'); localStorage.setItem('language', 'zh-CN'); });
const now = new Date().toISOString();
const members = ['leader', 'developer', 'reviewer'].map((key, i) => ({ id: i + 1, team_id: 144, user_id: 1, instance_id: 477 + i, member_key: key, display_name: key, role: key, runtime_type: i ? 'hermes' : 'openclaw', instance_mode: 'lite', status: i === 1 ? 'busy' : 'idle', availability: i === 1 ? 'busy' : 'idle', progress: 0, description: '成员职责与完整说明', last_seen_at: now, created_at: now, updated_at: now }));
const tasks = Array.from({ length: 8 }, (_, i) => ({ id: 300 + i, team_id: 144, target_member_id: 1, created_by: 1, message_id: `team-144-task-${300 + i}`, status: i === 7 ? 'running' : 'succeeded', workflow_state: i === 7 ? 'executing' : 'completed', payload: { title: `测试问题 ${i + 1}`, prompt: `测试问题 ${i + 1}：请协作开发并验收。\n${'长消息用于验证滚动定位和自动换行。'.repeat(20)}` }, result: i === 7 ? {} : { result: `已完成第 ${i + 1} 次任务。 /team/report.md` }, created_at: new Date(Date.now() - (8 - i) * 60000).toISOString(), updated_at: now }));
const data = { team: { id: 144, user_id: 1, name: 'workspace-acceptance', status: 'running', communication_mode: 'leader_mediated', shared_mount_path: '/team', created_at: now, updated_at: now }, members, leader_member_id: 'leader', tasks, events: [], work_items: [{ id: 1, team_id: 144, root_task_id: 307, work_id: 'build', owner_member_id: 2, title: '实现项目看板', status: 'running', required_for_root: true, created_at: now, updated_at: now, result: { summary: '详细交付说明 /team/report.md' } }] };
data.team.name = 'delivery-team';
tasks[6].payload = { title: '确认团队分工与交付标准', prompt: '请先明确成员职责、协作顺序和验收标准。开发与独立评审分开执行，最终交付由 Leader 汇总。' };
tasks[7].payload = { title: '个人项目看板 · 设计与交付', prompt: '请团队制作一个个人项目看板，参考 Trello 的操作体验。\n支持卡片拖拽、优先级标签与本地保存。请先制定计划，再由开发和评审分别完成实现与验收，最后交付页面和说明。' };
tasks[6].result = { result: '## 实施方案已确认\n本轮由 Developer 负责页面实现，Reviewer 负责独立验证。\n### 协作分工\n- Developer：实现卡片移动、标签管理和本地保存，提交自测结果。\n- Reviewer：核对产物、验证交互，记录测试边界与遗留项。\n### 交付约定\n1. 开发结果提交后，再开始独立评审。\n2. 静态检查和浏览器验证分别记录，不混为全部通过。\n3. 最终由 Leader 汇总交付与风险。\n方案说明：/team/report.md' };
const rosterTable = '\n\n| 成员 ID | 显示名称 | 角色 | Runtime | Mode | 状态 | 职责 |\n| --- | --- | --- | --- | --- | --- | --- |\n' +
  members.map(m => `| ${m.member_key} | 团队${m.role} | ${m.role} | ${m.runtime_type} | lite | idle/idle | ${'职责说明：负责协作分工、数据分析与交付验收，保留原始记录以及安全边界。'.repeat(16)} /team/report.md |`).join('\n');
tasks[6].result.result += rosterTable + '\n\n| 项目 | 说明 |\n| --- | --- |\n| 简短表格 | 不应折叠 |\n\n' +
  '| 单行长表格 | 内容 |\n| --- | --- |\n| 详细报告 | ' + '这是只有一行但内容非常长的表格，应该按实际高度折叠。'.repeat(100) + ' |';
data.work_items.push(
  { ...data.work_items[0], id: 2, work_id: 'requirements-review', owner_member_id: 3, title: '核对需求与验收清单', status: 'succeeded', result: { summary: '需求与测试清单已确认。 /team/report.md' } },
  { ...data.work_items[0], id: 3, work_id: 'leader-final-synthesis', owner_member_id: 1, title: '汇总交付与风险', status: 'pending', required_for_root: false, result: {} },
);
data.events.push({ id: 1, team_id: 144, task_id: 307, member_id: 1, event_type: 'outbound', sequence_no: 1, created_at: now, payload: { from: 'leader', to: 'developer', message: '开始实现项目看板', sent: true } });
const calls = [];
let failRefresh = false;
await page.route('**/api/v1/**', async route => {
  const request = route.request(), url = new URL(request.url()); calls.push([request.method(), url.pathname, url.search]);
  const send = value => route.fulfill({ json: { data: value } });
  if (url.pathname.endsWith('/auth/me')) return send({ id: 1, username: 'admin', role: 'user', status: 'active' });
  if (url.pathname.endsWith('/version')) return send({ version: 'ui-acceptance' });
  if (url.pathname.endsWith('/workspace/preview')) return send({ path: 'report.md', name: 'report.md', content: '# 验收报告\n可点击链接仍可打开预览。\n' + '## 验收章节\n预览长文档正文，确保标题与操作按钮始终可见。\n'.repeat(35) });
  if (url.pathname.endsWith('/workspace/download')) return route.fulfill({ body: 'test download', contentType: 'application/octet-stream' });
  if (url.pathname.endsWith('/workspace/files') && request.method() === 'GET') return send({ entries: url.searchParams.get('path') ? [{ name: 'report.md', path: 'artifacts/report.md', type: 'file', previewable: true, size: 123, modified_at: now }] : [{ name: 'artifacts', path: 'artifacts', type: 'directory', size: 0, modified_at: now }, { name: 'report.md', path: 'report.md', type: 'file', previewable: true, size: 123, modified_at: now }] });
  if (url.pathname === '/api/v1/teams/144') return failRefresh ? route.fulfill({ status: 503, json: { error: 'fixture transient failure' } }) : send(data);
  return send({});
});
const workspace = () => page.locator('.team-chat-workspace:visible');
const shot = async name => { await page.screenshot({ path: path.resolve(import.meta.dirname, `reports/team-workspace-${name}.png`), fullPage: true }); };
try {
  await page.goto(`${origin}/teams/144`);
  await workspace().waitFor();
  await page.waitForFunction(() => document.querySelector('.team-chat-workspace[data-mode=dual]'));
  assert.equal(await workspace().getAttribute('data-right'), 'closed');
  assert.equal(calls.filter(c => c[1].endsWith('/workspace/files')).length, 0, 'collapsed files must not fetch');
  // Member entry is available in both states, without reparenting/remounting chat.
  assert.equal(await workspace().locator('.team-roster-mini').count(), 3);
  assert.equal(await workspace().locator('.team-roster-quick-entry').count(), 3);
  const firstEntry = workspace().locator('.team-roster-quick-entry').first();
  assert.equal(await firstEntry.getAttribute('href'), '/instances/477');
  assert.equal(await firstEntry.getAttribute('target'), '_blank');
  assert.ok(await workspace().locator('.team-member-rail').evaluate(rail => {
    const cards = [...rail.querySelectorAll('.team-roster-mini')];
    if (getComputedStyle(rail).backgroundColor !== 'rgb(255, 255, 255)') return false;
    return cards.every((card, index) => {
      const r = card.getBoundingClientRect(), entry = card.querySelector('.team-roster-quick-entry').getBoundingClientRect();
      const nextAvatar = cards[index + 1]?.querySelector('.team-roster-avatar').getBoundingClientRect();
      const style = getComputedStyle(card);
      return card.querySelector('.team-roster-avatar small')?.textContent === String(index + 1)
        && card.querySelector('.team-roster-avatar').getBoundingClientRect().width >= 36
        && style.backgroundColor === 'rgb(255, 255, 255)' && style.borderLeftWidth === '0px'
        && (index === 0 || style.borderTopWidth === '1px')
        && entry.top > r.top && entry.bottom < r.bottom && entry.right < r.right
        && (!nextAvatar || nextAvatar.top - entry.bottom >= 36);
    });
  }), 'original avatars and ordinals stay on a white rail; gray separators and spacing distinguish entries from the next member');
  await workspace().getByRole('button', { name: '展开成员与团队配置' }).click();
  const memberPanel = workspace().locator('.team-members-panel');
  assert.equal(await memberPanel.locator('.team-roster-card').count(), 3);
  assert.equal(await memberPanel.locator('.team-roster-entry').count(), 3);
  assert.ok(!(await memberPanel.innerText()).includes('共享目录'), 'redundant introduction stays out of roster');
  assert.ok(await memberPanel.evaluate(el => el.scrollHeight <= el.clientHeight + 1), 'expanded roster is not a scrollbox');
  await memberPanel.getByRole('button', { name: '查看leader完整详情' }).click();
  const memberDialog = page.locator('[data-team-member-dialog]');
  assert.equal(await memberDialog.count(), 1);
  assert.ok(await memberDialog.evaluate(el => el.matches(':modal')));
  assert.ok((await memberDialog.innerText()).includes('成员职责与完整说明'));
  await page.keyboard.press('Escape');
  assert.equal(await memberDialog.count(), 0);
  assert.equal(await workspace().getAttribute('data-left'), 'true');
  assert.match(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), /查看leader完整详情/);
  await memberPanel.getByRole('button', { name: '团队配置', exact: true }).click();
  assert.match(await memberDialog.innerText(), /leader_mediated/);
  await page.keyboard.press('Escape');
  await workspace().getByRole('button', { name: '收起成员', exact: true }).click();
  // Names sharing a prefix, long profiles, unknown and blocked runtime, Pro and
  // missing-instance members are real presentation inputs, not hard-coded roles.
  members[0].display_name = '团队负责人';
  members[1].display_name = '论文检索专员';
  members[2].display_name = '论文精读专员';
  await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.waitForTimeout(200);
  await shot('member-rail-grouped');
  await workspace().locator('.team-member-rail').screenshot({ path: path.resolve(import.meta.dirname, 'reports/team-member-rail-white.png') });
  assert.ok(await workspace().locator('.team-roster-mini-name').evaluateAll(names => names.every(name => name.scrollHeight <= name.clientHeight + 1)), 'shared-prefix names fit without clipping the distinguishing suffix');
  members[1].availability = 'unknown';
  members[2].availability = 'blocked';
  members[2].blocked_reason = 'runtime_unavailable: preserved failure details';
  members[2].description = '非常长的职责说明。'.repeat(150);
  members[2].instance_mode = 'pro';
  const savedInstance = members[1].instance_id;
  delete members[1].instance_id;
  await page.getByRole('button', { name: '刷新', exact: true }).click();
  await page.waitForTimeout(200);
  assert.match(await workspace().locator('.team-member-rail').innerText(), /论文检索专员/);
  assert.match(await workspace().locator('.team-member-rail').innerText(), /论文精读专员/);
  assert.match(await workspace().locator('.team-member-rail').innerText(), /待确认/);
  assert.equal(await workspace().locator('.team-roster-quick-entry').count(), 2);
  await workspace().getByRole('button', { name: '展开成员与团队配置' }).click();
  assert.match(await memberPanel.innerText(), /pro/);
  assert.match(await memberPanel.innerText(), /runtime_unavailable/);
  assert.ok((await memberPanel.boundingBox()).height < 500);
  await shot('member-cards');
  await memberPanel.getByRole('button', { name: '查看论文精读专员完整详情' }).click();
  assert.match(await memberDialog.innerText(), /runtime_unavailable: preserved failure details/);
  await page.keyboard.press('Escape');
  for (const size of [{ width: 1920, height: 700 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(size);
    // Wait for ResizeObserver-driven pagination instead of racing a fixed 200ms delay.
    await page.waitForFunction(() => [...document.querySelectorAll('.team-chat-workspace')].some(root => {
      const bounds = root.getBoundingClientRect();
      if (bounds.width === 0) return false;
      const panel = root.querySelector('.team-members-panel');
      const mode = bounds.width - 2 >= 1540 ? 'dual' : bounds.width - 2 >= 1248 ? 'single' : 'drawer';
      return root.dataset.mode === mode && (!panel || panel.hidden || panel.getBoundingClientRect().bottom <= bounds.bottom);
    }));
    if (await workspace().getAttribute('data-left') !== 'true') await workspace().getByRole('button', { name: '展开成员与团队配置' }).click();
    assert.ok(await memberPanel.evaluate(el => el.scrollHeight <= el.clientHeight + 1));
    assert.ok(await memberPanel.evaluate(el => el.getBoundingClientRect().bottom <= el.closest('.team-chat-workspace').getBoundingClientRect().bottom));
    await memberPanel.getByRole('button', { name: /查看.*完整详情/ }).first().click();
    await memberDialog.getByRole('button', { name: '关闭成员详情' }).focus();
    await page.keyboard.press('Tab');
    assert.ok(await memberDialog.evaluate(el => el.contains(document.activeElement)));
    await page.keyboard.press('Escape');
    assert.equal(await workspace().getAttribute('data-left'), 'true', 'closing member modal keeps drawer open');
    await shot('members-' + size.width);
    if (size.width > 640) {
      await workspace().getByRole('button', { name: '收起成员', exact: true }).click();
      assert.ok(await workspace().locator('.team-member-rail').evaluate(rail =>
        rail.scrollHeight <= rail.clientHeight + 1 && rail.getBoundingClientRect().bottom <= rail.closest('.team-chat-workspace').getBoundingClientRect().bottom
      ), 'generous collapsed spacing fits short windows through pagination, not clipping or scrolling');
    }
  }
  await page.setViewportSize({ width: 1920, height: 1080 }); await page.waitForTimeout(200);
  const extraMembers = Array.from({ length: 5 }, (_, i) => ({ ...members[0], id: 20 + i, member_key: 'extra-' + i, display_name: 'Additional specialist ' + i }));
  members.push(...extraMembers);
  await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.waitForTimeout(200);
  if (await workspace().getAttribute('data-left') !== 'true') await workspace().getByRole('button', { name: '展开成员与团队配置' }).click();
  const found = new Set();
  for (let i = 0; i < 3; i++) {
    (await memberPanel.locator('.team-roster-card').evaluateAll(cards => cards.map(c => c.dataset.memberId))).forEach(id => found.add(id));
    assert.ok(await memberPanel.evaluate(el => el.scrollHeight <= el.clientHeight + 1));
    const next = memberPanel.getByRole('button', { name: '下一页成员' });
    if (await next.isEnabled()) await next.click();
  }
  assert.equal(found.size, 8, 'all members accessible through pagination');
  members.splice(3);
  members.forEach((m, i) => { m.display_name = m.member_key; m.availability = i === 1 ? 'busy' : 'idle'; m.instance_mode = 'lite'; delete m.blocked_reason; });
  members[1].instance_id = savedInstance;
  await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.waitForTimeout(200);
  assert.equal(await memberPanel.locator('.team-roster-card').count(), 3, 'shrinking roster clamps page safely');
  await workspace().getByRole('button', { name: '收起成员', exact: true }).click();
  await shot('collapsed');
  assert.equal(await workspace().locator('.team-history-popover').isVisible(), false);
  await workspace().locator('.team-history-menu summary').click();
  await workspace().locator('.team-history-options button').first().waitFor();
  assert.ok(await workspace().locator('.team-history-popover').evaluate(el => {
    const r = el.getBoundingClientRect();
    return el.contains(document.elementFromPoint(r.right - 8, r.bottom - 8));
  }), 'history popover must remain above floating workflow');
  await shot('history');
  await workspace().locator('.team-history-options button').nth(1).click();
  assert.equal(await workspace().locator('.team-history-popover').isVisible(), false);
  const roster = workspace().locator('.team-collapsible-table').filter({ hasText: '成员 ID' });
  await roster.getByRole('button', { name: '显示全部', exact: true }).waitFor();
  assert.equal(await roster.getAttribute('data-collapsed'), 'true');
  assert.ok((await roster.boundingBox()).height < 390, 'long table should not occupy the whole screen');
  assert.ok(await roster.locator('th').evaluateAll(cells => cells.every(cell => cell.getBoundingClientRect().width >= 95)), 'metadata columns must retain readable widths');
  await roster.scrollIntoViewIfNeeded();
  await shot('table-collapsed');
  await roster.getByRole('button', { name: '显示全部', exact: true }).click();
  assert.equal(await roster.getAttribute('data-collapsed'), 'false');
  assert.ok((await roster.boundingBox()).height > 500);
  await shot('table-expanded');
  await roster.getByRole('button', { name: '收起', exact: true }).click();
  await page.waitForTimeout(100);
  assert.equal(await roster.getAttribute('data-collapsed'), 'true');
  assert.ok(await roster.evaluate(el => {
    const feed = el.closest('.team-chat-feed').getBoundingClientRect(), rect = el.getBoundingClientRect();
    return rect.bottom > feed.top && rect.top < feed.bottom;
  }), 'collapse keeps the table in the reader viewport');
  const shortTable = workspace().locator('.team-collapsible-table').filter({ hasText: '不应折叠' });
  assert.equal(await shortTable.getByRole('button').count(), 0, 'short table must not gain a redundant toggle');
  const singleRowTable = workspace().locator('.team-collapsible-table').filter({ hasText: '单行长表格' });
  assert.equal(await singleRowTable.getAttribute('data-collapsed'), 'true', 'even one tall row must collapse');
  await roster.getByRole('button', { name: '显示全部', exact: true }).click();
  await roster.getByRole('button', { name: '/team/report.md', exact: true }).first().click();
  await page.locator('[data-workspace-preview]:visible').waitFor();
  assert.equal(await page.locator('[data-workspace-preview]:visible').count(), 1, 'responsive layout must not create duplicate body portals');
  await page.keyboard.press('Escape');
  await roster.getByRole('button', { name: '收起', exact: true }).click();
  await workspace().locator('.team-history-menu summary').click();
  await workspace().locator('.team-history-options button').first().click();
  await workspace().locator('.team-history-menu summary').click();
  await page.keyboard.press('Escape');
  assert.equal(await workspace().locator('.team-history-popover').isVisible(), false);
  const workflowNode = workspace().locator('.team-workflow-node').filter({ hasText: 'developer' }).first();
  await workflowNode.hover();
  await page.getByRole('tooltip').waitFor();
  assert.match(await page.getByRole('tooltip').innerText(), /实现项目看板/);
  await shot('workflow-hover');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  assert.equal(await workspace().locator('li[data-active=true] .team-workflow-marker svg').first().evaluate(el => getComputedStyle(el).animationName), 'none');
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await workflowNode.click();
  assert.equal(await workspace().getAttribute('data-right'), 'kanban');
  await workspace().getByRole('button', { name: '收起右侧面板' }).click();
  await workspace().locator('.team-tools-toolbar').getByRole('button', { name: '文件', exact: true }).click();
  assert.equal(await workspace().getAttribute('data-right'), 'files');
  await workspace().getByRole('button', { name: '收起右侧面板' }).click();
  const feed = workspace().locator('.team-chat-feed');
  await feed.evaluate(el => { el.scrollTop = el.scrollHeight / 2; el.dispatchEvent(new Event('scroll')); });
  await page.waitForTimeout(150);
  const anchor = await feed.evaluate(el => { const top = el.getBoundingClientRect().top; const n = [...el.querySelectorAll('[data-chat-message]')].find(n => n.getBoundingClientRect().bottom > top + 1); return { id: n.dataset.chatMessage, offset: n.getBoundingClientRect().top - top }; });
  await workspace().getByRole('button', { name: '展开成员与团队配置' }).click();
  await workspace().getByRole('button', { name: '展开任务看板', exact: true }).click();
  assert.equal(await workspace().getAttribute('data-left'), 'true');
  assert.equal(await workspace().getAttribute('data-right'), 'kanban');
  assert.ok(await workspace().locator('.team-chat-main').evaluate(el => el.clientWidth) >= 560);
  await page.waitForTimeout(200);
  const offset = await feed.evaluate((el, id) => [...el.querySelectorAll('[data-chat-message]')].find(n => n.dataset.chatMessage === id).getBoundingClientRect().top - el.getBoundingClientRect().top, anchor.id);
  assert.ok(Math.abs(offset - anchor.offset) < 8, `reading anchor moved ${offset - anchor.offset}px`);
  await feed.evaluate(el => { el.scrollTop = el.scrollHeight; el.dispatchEvent(new Event('scroll')); });
  await shot('both');
  await workspace().locator('.team-board-columns').getByRole('button', { name: /实现项目看板/ }).click();
  await workspace().getByRole('button', { name: '← 返回看板' }).waitFor();
  await workspace().locator('.team-kanban-host').getByRole('button', { name: '/team/report.md', exact: true }).click();
  await page.locator('[data-workspace-preview]:visible').waitFor();
  await page.keyboard.press('Escape');
  await shot('card-detail');
  await workspace().getByRole('button', { name: '← 返回看板' }).click();
  await workspace().locator('textarea').fill('保留未发送的草稿');
  await workspace().locator('.team-tools-toolbar').getByRole('button', { name: '文件', exact: true }).click();
  assert.equal(await workspace().locator('.team-kanban-host').isVisible(), false);
  await workspace().getByRole('button', { name: 'artifacts', exact: true }).click();
  await workspace().getByRole('button', { name: 'report.md', exact: true }).waitFor();
  await workspace().locator('.team-tools-toolbar').getByRole('button', { name: '看板', exact: true }).click();
  await workspace().locator('.team-tools-toolbar').getByRole('button', { name: '文件', exact: true }).click();
  assert.ok((await workspace().locator('.team-files-host').innerText()).includes('artifacts'), 'folder must survive switching');
  assert.equal(await workspace().locator('textarea').inputValue(), '保留未发送的草稿');
  await workspace().getByRole('button', { name: 'report.md', exact: true }).click();
  await page.locator('[data-workspace-preview]:visible').waitFor();
  assert.ok(await page.locator('[data-workspace-preview]:visible').evaluate(modal => {
    const topbar = [...document.querySelectorAll('.team-chat-topbar')].find(el => el.getClientRects().length);
    const r = topbar.getBoundingClientRect();
    return modal.contains(document.elementFromPoint(innerWidth / 2, r.top + r.height / 2));
  }), 'file preview must cover the chat header, not sit behind its stacking context');
  const previewDialog = page.locator('[data-workspace-preview]:visible');
  assert.ok(await previewDialog.evaluate(el => el.matches(':modal')), 'preview must use the browser modal layer');
  await previewDialog.locator('.team-preview-body').evaluate(el => { el.scrollTop = el.scrollHeight; });
  assert.ok(await previewDialog.locator('.team-preview-header').evaluate(el => {
    const r = el.getBoundingClientRect();
    return r.top >= 0 && el.contains(document.elementFromPoint(r.left + 20, r.top + 20));
  }), 'preview title remains visible and hit-testable after scrolling');
  await shot('preview-layer');
  await previewDialog.getByRole('button', { name: '关闭', exact: true }).focus();
  await page.keyboard.press('Tab');
  assert.ok(await previewDialog.evaluate(el => el.contains(document.activeElement)), 'Tab stays within preview');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('[data-workspace-preview]:visible').count(), 0);
  assert.equal(await workspace().getAttribute('data-right'), 'files');
  await shot('files');
  for (const width of [1600, 1440, 1366, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(200);
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `page overflows at ${width}`);
    if (await workspace().getAttribute('data-mode') === 'drawer') {
      await page.keyboard.press('Escape');
      await workspace().getByRole('button', { name: '展开任务看板', exact: true }).click();
      await workspace().getByRole('dialog').waitFor();
      await page.keyboard.press('Escape');
      assert.equal(await workspace().getAttribute('data-right'), 'closed');
    }
    await shot(String(width));
    if (width === 390) {
      await workspace().locator('.team-tools-toolbar').getByRole('button', { name: '文件', exact: true }).click();
      await workspace().getByRole('button', { name: 'report.md', exact: true }).click();
      const mobilePreview = page.locator('[data-workspace-preview]:visible');
      await mobilePreview.waitFor();
      assert.equal(await page.locator('[data-workspace-preview]:visible').count(), 1);
      assert.ok(await mobilePreview.evaluate(el => el.matches(':modal')));
      await mobilePreview.locator('.team-preview-body').evaluate(el => { el.scrollTop = el.scrollHeight; });
      await mobilePreview.getByRole('button', { name: '关闭', exact: true }).waitFor();
      await shot('preview-mobile');
      await page.keyboard.press('Escape');
      assert.equal(await workspace().getAttribute('data-right'), 'files', 'closing preview must not close underlying drawer');
      await page.keyboard.press('Escape');
    }
  }
  await page.setViewportSize({ width: 1920, height: 1080 });
  // New, successful transfer animates once; re-polling the same event does not
  // reset the effect. Terminal tasks must stop execution animation.
  data.events.push({ ...data.events[0], id: 2, sequence_no: 2, created_at: new Date().toISOString(), payload: { from: 'developer', to: 'reviewer', sent: true, messageDelivered: true } });
  await page.getByRole('button', { name: '刷新', exact: true }).filter({ visible: true }).first().click();
  await workspace().locator('.team-workflow-handoff[data-fresh=true]').waitFor();
  assert.match(await workspace().locator('.team-workflow-handoff').innerText(), /developer[\s\S]*reviewer/);
  const handoff=workspace().locator('.team-workflow-handoff');
  assert.ok(await handoff.locator('.team-workflow-handoff-caption').evaluate(el=>parseFloat(getComputedStyle(el).fontSize)>=12));
  assert.ok(await handoff.locator('.team-workflow-handoff-row strong').evaluateAll(nodes=>nodes.every(el=>parseFloat(getComputedStyle(el).fontSize)>=13)));
  await handoff.focus();
  assert.match(await handoff.getAttribute('aria-label'),/developer → reviewer/);
  assert.ok(await handoff.evaluate(el=>el===document.activeElement));
  tasks[7].status = 'succeeded'; tasks[7].workflow_state = 'completed'; tasks[7].updated_at = new Date(Date.now() + 1000).toISOString();
  await page.getByRole('button', { name: '刷新', exact: true }).filter({ visible: true }).first().click();
  await page.waitForFunction(() => [...document.querySelectorAll('.team-workflow-nodes li')].every(el => el.dataset.active !== 'true'));
  assert.match(await workspace().locator('.team-workflow-status').innerText(), /已完成/);
  // Actual product transcript: consecutive-worker grouping keeps every event
  // node, timestamp and file action, with an independent human-user badge.
  members[0].display_name='主编'; members[1].display_name='资讯采集员'; members[2].display_name='内容筛选员';
  members[1].icon_key='news'; members[2].icon_key='filter';
  const narrative = (id, member, content) => ({ id, team_id:144,task_id:307,member_id:member.id,event_type:'progress',sequence_no:id,created_at:new Date(Date.now()+id*1000).toISOString(),payload:{from:member.member_key,memberId:member.member_key,eventKind:'agent_narrative',messageId:`narrative-${id}`,text:content,phaseId:'phase-news',revision:1}});
  data.events.push(narrative(10,members[1],'已完成资讯来源核对，正在整理主题与发布时间。'),narrative(11,members[1],'继续筛选重复报道，保留可追溯的一手来源。'),narrative(12,members[2],'已收到采集结果，正在独立核对日报内容。'),narrative(13,members[1],'采集结果已经整理，可供内容筛选员查看。'));
  data.events.at(-1).payload.artifactRefs=['/team/report.md','/team/index.html'];
  await page.getByRole('button',{name:'刷新',exact:true}).filter({visible:true}).first().click();
  const eventNode=id=>workspace().locator(`[data-chat-message="event-${id}"]`);
  await eventNode(13).waitFor();
  assert.equal(await eventNode(10).getAttribute('data-chat-segment'),'start');
  assert.equal(await eventNode(11).getAttribute('data-chat-segment'),'continuation');
  assert.equal(await eventNode(11).locator('.team-avatar').count(),0);
  assert.equal(await eventNode(12).getAttribute('data-chat-segment'),'start');
  assert.equal(await eventNode(13).getAttribute('data-chat-segment'),'start','A→B→A does not merge non-adjacent authors');
  assert.equal(await eventNode(10).locator('[data-icon-key=news]').count(),1);
  assert.equal(await eventNode(12).locator('[data-icon-key=filter]').count(),1);
  assert.ok(await workspace().locator('[data-chat-author=user] [data-icon-key=user]').count()>0,'human User has a functional icon');
  assert.ok(await eventNode(10).evaluate(node=>parseFloat(getComputedStyle(node.querySelector('.team-chat-author-heading strong')).fontSize)>parseFloat(getComputedStyle(node.querySelector('.team-plain-message')).fontSize)),'worker name is larger than body');
  assert.ok(await workspace().locator('.team-chat-subtitle').evaluate(el=>parseFloat(getComputedStyle(el).fontSize)>=13));
  assert.ok(await eventNode(13).locator('.team-chat-attachments').evaluate(el=>getComputedStyle(el).borderTopWidth==='0px'),'attachments do not introduce an unrelated message divider');
  await feed.evaluate(el=>{el.scrollTop=el.scrollHeight;el.dispatchEvent(new Event('scroll'));});
  await shot('continuous-badges');
  await eventNode(13).getByRole('button',{name:'预览 /team/report.md',exact:true}).click();
  await page.locator('[data-workspace-preview]:visible').waitFor();
  await page.keyboard.press('Escape');
  const downloadEvent=page.waitForEvent('download');
  await eventNode(13).getByRole('button',{name:'下载 /team/index.html',exact:true}).click();
  await downloadEvent;
  // Real mixed-schema coordination: recipient work context must not split the
  // author block or alter existing message/attachment identity and ordering.
  const coordTime=Date.now()+60000;
  const coordEvent=(id,content,extra={})=>({id,team_id:144,task_id:307,member_id:1,event_type:'reply',sequence_no:id,created_at:new Date(coordTime+id*100).toISOString(),payload:{memberId:'leader',rootTaskId:'team-144-task-307',revision:1,chatPolicy:'visible',text:content,collaborationStep:{actor:'leader',target:'leader',type:'progress',content,summary:content},...extra}});
  data.events.push(
    coordEvent(20,'已更新团队产物：/team/report.md',{event:'artifact_changed',eventKind:'artifact_changed',artifactRefs:['/team/report.md']}),
    coordEvent(21,'派发采集任务',{assignmentId:'work-gather',phaseId:'phase-gather',revision:7,collaborationStep:{actor:'leader',target:'developer',type:'assignment',summary:'采集任务',content:'派发采集任务'}}),
    coordEvent(22,'三阶段协作计划已制定。',{eventKind:'leader_plan',artifactRefs:['/team/report.md']}),
    coordEvent(23,'开始阶段一。',{event:'task_progress',eventKind:'leader_plan',phaseId:'phase-one'}),
    coordEvent(24,'进入阶段二。',{event:'task_progress',eventKind:'leader_plan',phaseId:'phase-two'}),
    coordEvent(25,'重试版本二。',{event:'task_progress',eventKind:'leader_plan',phaseId:'phase-two',revision:2}),
  );
  await page.getByRole('button',{name:'刷新',exact:true}).filter({visible:true}).first().click();
  await eventNode(25).waitFor();
  for(const id of [21,22,23,24]) assert.equal(await eventNode(id).getAttribute('data-chat-segment'),'continuation',`coordination ${id}`);
  assert.equal(await eventNode(20).locator('.team-avatar').count(),1);
  assert.equal(await eventNode(21).locator('.team-avatar').count(),0);
  assert.match(await eventNode(21).locator('.team-chat-event-label').innerText(),/任务派发 → 资讯采集员/);
  assert.match(await eventNode(24).locator('.team-chat-event-label').innerText(),/阶段 · phase-two/);
  assert.equal(await eventNode(25).getAttribute('data-chat-segment'),'start');
  assert.match(await eventNode(25).locator('.team-chat-event-label').innerText(),/工作版本 · 2/);
  assert.ok(await workspace().locator('[data-chat-author=user]').evaluateAll(nodes=>nodes.every(node=>node.dataset.chatSegment==='start' && !!node.querySelector('.team-avatar'))),'all User submissions remain independent');
  await eventNode(22).getByRole('button',{name:'预览 /team/report.md',exact:true}).click();
  await page.locator('[data-workspace-preview]:visible').waitFor(); await page.keyboard.press('Escape');
  await feed.evaluate(el=>{el.scrollTop=el.scrollHeight;el.dispatchEvent(new Event('scroll'));});
  await shot('leader-coordination');
  await handoff.screenshot({path:path.resolve(import.meta.dirname,'reports/team-workflow-handoff-readable.png')});
  const longName='内容筛选与资讯来源独立核验及每日简报编辑专员';
  members[1].display_name=longName;
  await page.getByRole('button',{name:'刷新',exact:true}).filter({visible:true}).first().click();
  await page.waitForFunction(name=>document.querySelector('.team-workflow-handoff')?.textContent.includes(name),longName);
  assert.ok(await handoff.locator('.team-workflow-handoff-row strong').evaluateAll(nodes=>nodes.every(el=>el.scrollWidth<=el.clientWidth+1 && el.scrollHeight<=el.clientHeight+1)),'full names wrap without clipping');
  assert.match(await handoff.innerText(),new RegExp(longName));
  await handoff.screenshot({path:path.resolve(import.meta.dirname,'reports/team-workflow-handoff-long-name.png')});
  for(const width of [1440,1024,768,960,390]) {
    await page.setViewportSize({width,height:900}); await page.waitForTimeout(200);
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`context/handoff overflow at ${width}`);
    if(width<=640) assert.equal(await handoff.isVisible(),false,'mobile keeps existing compact controls');
    else assert.ok(await handoff.locator('.team-workflow-handoff-row strong').evaluateAll(nodes=>nodes.every(el=>parseFloat(getComputedStyle(el).fontSize)>=13 && el.scrollWidth<=el.clientWidth+1)));
  }
  await page.setViewportSize({width:960,height:540}); await page.waitForTimeout(200);
  await shot('coordination-200-percent-effective');
  await page.setViewportSize({width:1920,height:1080});
  await page.evaluate(()=>{document.body.style.zoom='2';}); await page.waitForTimeout(250);
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'200% CSS zoom must not overflow the page');
  assert.ok(await handoff.locator('.team-workflow-handoff-row strong').evaluateAll(nodes=>nodes.every(el=>el.scrollWidth<=el.clientWidth+1)),'200% rendered names remain readable');
  await shot('coordination-200-percent-rendered');
  await page.evaluate(()=>{document.body.style.zoom='';}); await page.waitForTimeout(250);
  members[1].display_name='资讯采集员';
  const ids=await workspace().locator('[data-chat-message]').evaluateAll(nodes=>nodes.map(n=>n.dataset.chatMessage));
  await page.getByRole('button',{name:'刷新',exact:true}).filter({visible:true}).first().click();
  assert.deepEqual(await workspace().locator('[data-chat-message]').evaluateAll(nodes=>nodes.map(n=>n.dataset.chatMessage)),ids,'polling does not duplicate or drop presented messages');
  await workspace().getByRole('button', { name: '展开任务看板', exact: true }).click();
  failRefresh = true;
  await page.getByRole('button', { name: '刷新', exact: true }).filter({ visible: true }).first().click();
  await page.getByRole('alert').filter({ visible: true }).waitFor();
  assert.equal(await workspace().locator('textarea').inputValue(), '保留未发送的草稿');
  assert.deepEqual(errors, []);
  console.log('PASS: floating workspace, table widths/collapse/links, native preview layering and mobile controls, reading anchor, panel state, transfer/terminal animation, responsive widths and transient refresh.');
} finally { await browser.close(); server.close(); }
