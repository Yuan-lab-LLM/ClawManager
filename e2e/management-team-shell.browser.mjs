// Real TeamDetailPage + shell regression; APIs are fixtures, no live mutations.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { chromium } from '@playwright/test';
const browser = await chromium.launch({ channel: 'chrome', headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH });
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
await page.addInitScript(() => { localStorage.setItem('access_token', 'fixture'); localStorage.setItem('clawmanager_locale', 'en'); });
page.setDefaultTimeout(15000);
let nativeDialogs = 0;
let failPreview = true;
page.on('dialog', async dialog => { nativeDialogs++; await dialog.dismiss(); });
const now = new Date().toISOString();
const members = ['leader', 'developer', 'reviewer'].map((key, i) => ({ id: i + 1, team_id: 144, user_id: 1, instance_id: 477 + i, member_key: key, display_name: key, role: key, runtime_type: i ? 'hermes' : 'openclaw', instance_mode: 'lite', status: i === 1 ? 'busy' : 'idle', availability: i === 1 ? 'busy' : 'idle', progress: 0, description: '成员职责与完整说明', last_seen_at: now, created_at: now, updated_at: now }));
const tasks = Array.from({ length: 8 }, (_, i) => ({ id: 300 + i, team_id: 144, target_member_id: 1, created_by: 1, message_id: `team-144-task-${300 + i}`, status: i === 7 ? 'running' : 'succeeded', workflow_state: i === 7 ? 'executing' : 'completed', payload: { title: `测试问题 ${i + 1}`, prompt: `测试问题 ${i + 1}：请协作开发并验收。\n${'长消息用于验证滚动定位和自动换行。'.repeat(20)}` }, result: i === 7 ? {} : { result: `已完成第 ${i + 1} 次任务。 /team/report.md` }, created_at: new Date(Date.now() - (8 - i) * 60000).toISOString(), updated_at: now }));
const data = { team: { id: 144, user_id: 1, name: 'workspace-acceptance', status: 'running', communication_mode: 'leader_mediated', shared_mount_path: '/team', created_at: now, updated_at: now }, members, leader_member_id: 'leader', tasks, events: [], work_items: [{ id: 1, team_id: 144, root_task_id: 307, work_id: 'build', owner_member_id: 2, title: '实现项目看板', status: 'running', required_for_root: true, created_at: now, updated_at: now, result: { summary: '详细交付说明 /team/report.md' } }] };

await page.route('**/api/v1/**', async route => {
  const path = new URL(route.request().url()).pathname;
  if (path.endsWith('/workspace/preview')) return failPreview ? route.fulfill({ status: 503, json: { error: 'fixture preview failure' } }) : route.fulfill({ json: { data: { path: 'report.md', name: 'report.md', content: '# Preserved report' } } });
  if (path.endsWith('/workspace/download')) return route.fulfill({ status: 503, json: { error: 'fixture download failure' } });
  const value = path.endsWith('/auth/me') ? { id: 1, username: 'admin', role: 'user', status: 'active' } : path === '/api/v1/teams/144' ? data : path.endsWith('/workspace/files') ? { entries: [{ name: 'report.md', path: 'report.md', type: 'file', previewable: true, size: 123, modified_at: now }] } : {};
  await route.fulfill({ json: { data: value } });
});
try {
  await page.goto(process.env.FRONTEND_URL || 'http://127.0.0.1:9002/teams/144');
  const composer = page.getByRole('textbox', { name: 'Task content' });
  await composer.waitFor();
  assert.equal(await page.getByRole('button', { name: 'Refresh', exact: true }).count(), 0);
  const bounds = await page.locator('.cm-management-header').evaluate(el => ({ header: el.getBoundingClientRect().toJSON(), actions: el.querySelector('.cm-header-actions').getBoundingClientRect().toJSON(), width: document.documentElement.scrollWidth, viewport: innerWidth }));
  assert(bounds.actions.top >= bounds.header.top && bounds.actions.bottom <= bounds.header.bottom, 'Team actions must fit inside the 50px header');
  assert(bounds.width <= bounds.viewport, 'Team shell must not overflow horizontally');
  const box = await composer.boundingBox();
  assert(box.y >= 50 && box.y + box.height <= 844, 'Real team composer stays inside viewport');
  await composer.fill('Unsent team draft');
  if (process.env.MANAGEMENT_TEAM_ARTIFACTS) { await fs.mkdir(process.env.MANAGEMENT_TEAM_ARTIFACTS, { recursive: true }); await page.screenshot({ path: path.join(process.env.MANAGEMENT_TEAM_ARTIFACTS, 'team-390.png') }); }
  const actions = page.getByRole('button', { name: 'Team actions' });
  await actions.click();
  const panel = page.getByRole('region', { name: 'Team actions' });
  if (process.env.MANAGEMENT_TEAM_ARTIFACTS) await page.screenshot({ path: path.join(process.env.MANAGEMENT_TEAM_ARTIFACTS, 'team-390-actions.png') });
  for (const text of ['Team #144', 'Shared directory: /team']) assert(await panel.getByText(text, { exact: true }).isVisible());
  for (const name of ['Delete team']) { const button = panel.getByRole('button', { name, exact: true }); assert(await button.isVisible()); const box = await button.boundingBox(); assert(box.x >= 0 && box.x + box.width <= 390, 'Team actions remain within the viewport'); }
  assert(await panel.getByRole('link', { name: 'Back to teams', exact: true }).isVisible());
  await Promise.all([page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/teams/144'), page.evaluate(() => window.dispatchEvent(new Event('online')))]);
  assert.equal(await composer.inputValue(), 'Unsent team draft', 'Background refresh retains draft');
  await page.keyboard.press('Escape');
  assert.equal(await panel.isVisible(), false);
  assert(await actions.evaluate(el => el === document.activeElement), 'Team action disclosure restores focus');
  assert.equal(await composer.inputValue(), 'Unsent team draft');
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    const composerBox = await composer.boundingBox();
    assert(composerBox.y + composerBox.height <= 900);
    if (process.env.MANAGEMENT_TEAM_ARTIFACTS) await page.screenshot({ path: path.join(process.env.MANAGEMENT_TEAM_ARTIFACTS, `team-${width}.png`) });
  }
  const messageIDs = await page.locator('[data-chat-message]').evaluateAll(nodes => nodes.map(node => node.dataset.chatMessage));
  await page.getByTitle('预览 /team/report.md', { exact: true }).first().click();
  await page.getByRole('alert').getByText('fixture preview failure', { exact: true }).waitFor();
  assert.equal(nativeDialogs, 0, 'File preview failures use in-workspace feedback');
  assert.equal(await composer.inputValue(), 'Unsent team draft');
  assert.deepEqual(await page.locator('[data-chat-message]').evaluateAll(nodes => nodes.map(node => node.dataset.chatMessage)), messageIDs);
  failPreview = false;
  await page.getByTitle('预览 /team/report.md', { exact: true }).first().click();
  const previewDialog = page.getByRole('dialog', { name: 'Preview report.md', exact: true });
  await previewDialog.waitFor();
  await previewDialog.getByRole('button', { name: 'Download', exact: true }).click();
  await previewDialog.getByRole('alert').waitFor();
  assert.match(await previewDialog.getByRole('alert').innerText(), /下载文件失败/);
  assert.equal(nativeDialogs, 0);
  await page.keyboard.press('Escape');
  assert.equal(await composer.inputValue(), 'Unsent team draft');
  await page.locator('.team-tools-toolbar').getByRole('button', { name: 'Files', exact: true }).click();
  await page.getByRole('button', { name: 'report.md', exact: true }).waitFor();
  assert(await page.getByRole('button', { name: 'Refresh', exact: true }).isVisible(), 'File-specific refresh remains available');
  await page.getByRole('button', { name: 'New folder', exact: true }).click();
  const fileDialog = page.getByRole('dialog', { name: 'New folder', exact: true });
  await fileDialog.waitFor();
  await fileDialog.getByRole('textbox', { name: 'Name', exact: true }).fill('Unsubmitted folder');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('.team-chat-workspace').getAttribute('data-right'), 'files', 'File action Escape preserves underlying drawer');
  assert(await page.getByRole('button', { name: 'New folder', exact: true }).evaluate(el => el === document.activeElement));
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await page.getByRole('dialog', { name: 'Delete', exact: true }).getByRole('button', { name: 'Cancel' }).click();
  assert.equal(await composer.inputValue(), 'Unsent team draft');
  await page.keyboard.press('Escape');
  console.log('PASS real TeamDetailPage 390: compact header, accessible info/actions, visible composer and retained draft');
} catch (error) { console.error(error); throw error; } finally { await browser.close(); }
