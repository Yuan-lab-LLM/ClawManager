// Actual application routes + explicit API fixtures. No pixel/CSS equality assertions.
// FRONTEND_URL=http://127.0.0.1:9002 node e2e/management-ui-visual.browser.mjs
// Or serve committed production output: FRONTEND_DIST=/absolute/frontend/dist node ...
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import http from 'node:http';
import { chromium } from '@playwright/test';
const output = path.resolve(process.env.MANAGEMENT_VISUAL_ARTIFACTS || 'docs/uiux/assets/2026-11-11-uiux08-implementation');
await fs.mkdir(output, { recursive: true });
let server;
let origin = process.env.FRONTEND_URL || 'http://127.0.0.1:9002';
if (process.env.FRONTEND_DIST) {
  const root = path.resolve(process.env.FRONTEND_DIST);
  server = http.createServer(async (req, res) => {
    try {
      const pathname = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
      let file = path.resolve(root, `.${pathname}`);
      if (!file.startsWith(`${root}${path.sep}`)) file = path.join(root, 'index.html');
      try { if (!(await fs.stat(file)).isFile()) file = path.join(root, 'index.html'); } catch { file = path.join(root, 'index.html'); }
      res.setHeader('Content-Type', ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png' })[path.extname(file)] || 'application/octet-stream');
      res.end(await fs.readFile(file));
    } catch { res.writeHead(500).end(); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
}
const now = '2026-11-11T08:00:00Z';
const makeInstance = (id, name, status, type = 'hermes', mode = 'lite') => ({ id, name, status, type, instance_mode: mode, user_id: 1, runtime_type: 'gateway', cpu_cores: 2, memory_gb: 4, disk_gb: 20, gpu_enabled: false, gpu_count: 0, os_type: 'linux', os_version: 'ubuntu', storage_class: 'standard', mount_path: '/workspace', created_at: now, updated_at: now, availability: status === 'running' ? 'available' : 'unavailable', workspace_usage_bytes: 346030080, last_online_at: now, ...(status === 'stopped' ? { stopped_reason: 'idle_timeout' } : {}), ...(status === 'error' ? { runtime_error_code: 'gateway_start_failed', runtime_error_message: 'Gateway initialization failed: workspace mount unavailable. Retry after storage recovers.' } : {}) });
const instances = [makeInstance(229, 'Research workspace', 'running'), makeInstance(230, 'Development Pro', 'stopped', 'openclaw', 'pro'), makeInstance(231, 'Release verification', 'error'), makeInstance(232, 'Source analysis', 'creating', 'opencode')];
const makePod = (id, capacity, extra = {}) => ({ id, runtime_type: id % 2 ? 'hermes' : 'openclaw', namespace: 'runtime', pod_name: `runtime-${id}`, pod_uid: `uid-${id}`, deployment_name: `pool-${id}`, node_name: 'test-node-54', pod_ip: '10.42.1.24', image_ref: 'runtime:2026.11', capabilities: [], state: 'ready', capacity, used_slots: capacity ? 2 : 0, bound_instances: capacity ? 2 : 0, draining: false, agent_reported: true, bindings_reported: true, reconciliation_state: 'consistent', scheduling_enabled: true, last_seen_at: now, slots: Array.from({ length: capacity }, (_, i) => ({ index: i + 1, container_name: `agent-${i + 1}`, state: 'ready', ready: true, restart_count: 0, reconciliation_state: 'consistent', ...(i < 2 ? { instance_id: 229 + i, instance_name: instances[i].name, instance_state: i ? 'creating' : 'running', binding_confirmed: true } : {}) })), ...extra });
const members = ['leader', 'developer', 'reviewer'].map((key, i) => ({ id: i + 1, team_id: 144, user_id: 1, instance_id: 229 + i, member_key: key, display_name: key, role: key, runtime_type: i ? 'hermes' : 'openclaw', instance_mode: 'lite', status: 'idle', availability: 'idle', progress: 0, last_seen_at: now, created_at: now, updated_at: now }));
const tasks = ['Review release scope and acceptance checks.', 'Verify source links and summarize findings.', 'Prepare the release note.'].map((prompt, i) => ({ id: 300 + i, team_id: 144, target_member_id: 1, created_by: 1, message_id: `team-144-task-${300 + i}`, status: 'succeeded', workflow_state: 'completed', payload: { title: prompt, prompt }, result: { result: ['Scope confirmed. Developer prepares changes; reviewer verifies behavior.', 'Source links verified. Two documentation corrections are ready.', 'Release note prepared in /team/report.md.'][i] }, created_at: `2026-11-11T07:0${i}:00Z`, updated_at: now }));
const regularTeam = { team: { id: 144, user_id: 1, name: 'delivery-team', status: 'running', communication_mode: 'leader_mediated', shared_mount_path: '/team', created_at: now, updated_at: now }, members, leader_member_id: 'leader', tasks, events: [], work_items: [{ id: 1, team_id: 144, root_task_id: 302, work_id: 'release-note', owner_member_id: 2, title: 'Prepare release note', status: 'succeeded', required_for_root: true, result: { summary: 'Release notes verified.' }, created_at: now, updated_at: now }] };
function teamData(mode) {
  const team = structuredClone(regularTeam);
  if (mode === 'empty') for (const key of ['members', 'tasks', 'events', 'work_items']) team[key] = [];
  if (mode === 'long') {
    team.team.name = 'Release verification and international source review '.repeat(5);
    team.members.forEach(m => { m.display_name = `${m.role} — international verification `.repeat(5); });
    team.tasks[1].result.result += '\n\n| Source | Verification |\n| --- | --- |\n' + Array.from({ length: 24 }, (_, i) => `| Source ${i + 1} | ${'Verified context and original source. '.repeat(10)} |`).join('\n');
    team.tasks[2].payload.prompt += ' Preserve source attribution, evidence and delivery details.'.repeat(30);
  }
  return team;
}
function runtimeData(mode) {
  let pods = mode === 'empty' ? [] : [makePod(198, 8), makePod(199, 16), makePod(200, 64)];
  if (mode === 'long') pods.forEach(p => { p.pod_name += '-international-runtime-inventory'.repeat(7); p.node_name += '-long-node-identifier'.repeat(5); });
  if (mode === 'zero') pods = [makePod(201, 0)];
  if (mode === 'offline') pods = [makePod(202, 8, { state: 'unhealthy', scheduling_enabled: false, last_seen_at: '2026-11-10T08:00:00Z' })];
  if (mode === 'unknown') pods = [makePod(203, 16, { agent_reported: false, bindings_reported: false, slots: [], reconciliation_state: 'unknown', bound_instances: undefined, used_slots: 0, unknown_slots: 16 })];
  const capacity = pods.reduce((n, p) => n + p.capacity, 0), occupied = pods.reduce((n, p) => n + p.used_slots, 0);
  const unknown = mode === 'unknown' ? capacity : 0, online = mode === 'offline' ? 0 : pods.length;
  return { pods, summary: { pods: pods.length, online, schedulable: ['offline', 'unknown'].includes(mode) ? 0 : pods.filter(p => p.capacity).length, capacity, ready: unknown ? 0 : capacity, occupied, ...(unknown ? {} : { bound_instances: occupied }), available: online && !unknown ? capacity - occupied : 0, running: unknown ? 0 : pods.filter(p => p.capacity).length, starting: unknown ? 0 : pods.filter(p => p.capacity).length, migrating: 0, error: 0, idle: unknown ? 0 : capacity - occupied, unknown, unavailable: mode === 'offline' ? capacity : 0 }, pagination: { page: 1, page_size: 16, total: pods.length, total_pages: 1 }, nodes: ['test-node-54'], updated_at: now };
}
const routes = { instances: '/instances', runtimes: '/admin/runtime-pods', teams: '/teams/144' };
const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH });
const results = [];
const apiPaths = new Set();
const unhandledPaths = new Set();
const contrastSamples = [];
async function contrast(page, selector, label) {
  const samples = await page.locator(selector).evaluateAll(elements => elements.filter(el => el.checkVisibility() && el.textContent.trim()).map(el => {
    let ancestor = el; let bg = "rgb(255, 255, 255)";
    while (ancestor) { const color = getComputedStyle(ancestor).backgroundColor; if (color !== "rgba(0, 0, 0, 0)") { bg = color; break; } ancestor = ancestor.parentElement; }
    return { text: el.textContent.trim().slice(0, 100), fg: getComputedStyle(el).color, bg };
  }));
  const lum = color => color.match(/[\d.]+/g).slice(0, 3).map(Number).map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((a, v, i) => a + v * [.2126, .7152, .0722][i], 0);
  for (const sample of samples) { const a = lum(sample.fg), b = lum(sample.bg); const ratio = (Math.max(a, b) + .05) / (Math.min(a, b) + .05); contrastSamples.push({ label, ...sample, ratio }); assert(ratio >= 4.5, `${label}: ${sample.text} contrast ${ratio}`); }
  assert(samples.length, `${label} rendered text sampled`);
}
async function scenePage(scene, mode, locale, width) {
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' });
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  page.setDefaultNavigationTimeout(20000);
  await page.addInitScript(({ locale }) => { localStorage.setItem('access_token', 'visual-fixture'); localStorage.setItem('clawmanager_locale', locale); localStorage.setItem('sidebar_collapsed', 'false'); window.WebSocket = class { static OPEN = 1; readyState = 1; close() {} send() {} }; }, { locale });
  const holds = []; const seen = [];
  const scenePath = `/api/v1${routes[scene]}`;
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), p = url.pathname; seen.push(p); apiPaths.add(p);
    // Hold only this scene's data. Auth/version always succeed; release on teardown.
    if (p === scenePath && mode === 'loading') await new Promise(resolve => holds.push(resolve));
    if (p === scenePath && mode === 'error') return route.fulfill({ status: 503, json: { error: 'Inventory temporarily unavailable' } });
    let data;
    if (p.endsWith('/auth/me')) data = { id: 1, username: 'visual-admin', role: 'admin', status: 'active' };
    else if (p.endsWith('/version')) data = { version: '2026.11', commit: 'isolated-fixture', build_time: now };
    else if (p.endsWith('/instances/batch/lifecycle')) data = [];
    else if (p === '/api/v1/instances') { const rows = mode === 'empty' && scene === 'instances' ? [] : structuredClone(instances); if (mode === 'long') rows[0].name = 'International research and source verification workspace '.repeat(6); data = { instances: rows, total: rows.length, page: 1, page_size: 100 }; }
    else if (p === '/api/v1/admin/runtime-pods') data = runtimeData(mode);
    else if (p === '/api/v1/teams/144') data = teamData(mode);
    else if (p.endsWith('/teams')) data = { teams: [regularTeam.team], total: 1 };
    else if (p.endsWith('/workspace/files')) data = { entries: [] };
    else if (p.endsWith('/runtime-auto-stop')) data = { enabled: false, check_interval_days: 1, idle_timeout_days: 7, version: 1, next_check_at: now };
    else if (p.endsWith('/scale-options')) data = { items: [], limits: { min_pods: 1, max_pods: 10 } };
    else if (p.endsWith('/runtime-scheduling')) data = { items: [], supported_modes: [] };
    else if (p.endsWith('/gateways')) data = { gateways: [] };
    else if (p.endsWith('/events')) data = { events: [] };
    else if (p.endsWith('/runtime-rollouts')) data = { items: [], total: 0 };
    else if (p.endsWith('/system-settings/images')) data = { images: [] };
    else { unhandledPaths.add(p); return route.fulfill({ status: 503, json: { error: `Unhandled visual fixture API: ${p}` } }); }
    return route.fulfill({ json: { success: true, data } }).catch(() => {});
  });
  await page.goto(origin + routes[scene]);
  await page.locator('.cm-management-shell').waitFor();
  await page.waitForFunction(() => document.documentElement.lang !== '');
  for (let i = 0; i < 60 && !seen.includes(scenePath); i++) await page.waitForTimeout(50);
  assert(seen.includes(scenePath), `${scene}/${mode} requested actual scene API`);
  if (mode === 'error') await page.getByText('Inventory temporarily unavailable', { exact: false }).first().waitFor();
  else if (mode !== 'loading') {
    if (scene === 'teams') await page.locator('[data-team-task-composer], .cm-team-composer, textarea').first().waitFor();
    else if (mode !== 'empty') await page.getByRole('table').waitFor();
    else await page.getByText(scene === 'instances' ? (locale === 'zh' ? '暂无实例' : 'No instances') : (locale === 'zh' ? '暂无活跃运行时 Pod' : 'No active runtime pods'), { exact: true }).waitFor();
  }
  await page.waitForTimeout(180);
  if (mode === 'loading') { assert.equal(await page.getByRole('table').count(), 0); assert.equal(await page.locator('textarea').count(), 0); }
  if (mode === 'empty' && scene === 'teams') assert.equal(await page.locator('textarea').count(), 1, 'empty is a loaded Team workspace');
  if (scene === 'runtimes' && locale !== 'zh') assert.equal(await page.locator('a[href="/admin/northbound"]').first().innerText(), ({ en: 'Northbound API', ja: '北向き API', ko: '노스바운드 API', de: 'Northbound-API' })[locale]);
  if (scene === 'runtimes') assert(await page.locator('.runtime-metric').evaluateAll(cells => cells.every(cell => { const bounds = cell.getBoundingClientRect(); return [...cell.querySelectorAll('div')].filter(el => el.checkVisibility()).every(el => { const r = el.getBoundingClientRect(); return r.left >= bounds.left - 1 && r.right <= bounds.right + 1; }); })), 'metric labels and unknown values stay inside their own cells');
  assert.equal(new URL(page.url()).pathname, routes[scene]);
  assert.equal(await page.locator('input[type="password"]').count(), 0, 'must not capture login');
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${scene}/${mode}/${locale}/${width} page overflow`);
  const header = await page.locator('.cm-management-header').boundingBox();
  if (header) assert(header.height <= 51, 'compact shared header');
  if (!['loading', 'error', 'empty'].includes(mode) && scene !== 'teams') assert(await page.getByRole('table').locator('tbody td').evaluateAll(cells => cells.every(c => getComputedStyle(c).whiteSpace === 'nowrap')), 'management cells remain single line');
  return { page, seen, close: async () => { holds.forEach(resolve => resolve()); await context.close(); } };
}
async function shot(page, name) { await page.screenshot({ path: path.join(output, `${name}.png`) }); results.push(name); console.log(name); }
try {
  if (!process.env.VISUAL_OVERLAYS_ONLY) for (const locale of (process.env.VISUAL_LOCALES || 'en,zh').split(',')) for (const scene of Object.keys(routes)) for (const mode of ['regular', 'empty', 'loading', 'error', 'long']) for (const width of [1440, 1280, 768, 390]) {
    const run = await scenePage(scene, mode, locale, width);
    try { await shot(run.page, `final-${scene}-${locale}-${mode}-${width}`); } finally { await run.close(); }
  }
  for (const mode of ['zero', 'offline', 'unknown']) for (const width of [1440, 390]) {
    const run = await scenePage('runtimes', mode, 'en', width);
    try { await shot(run.page, `final-runtimes-en-${mode}-${width}`); } finally { await run.close(); }
  }
  if (!process.env.VISUAL_OVERLAYS_ONLY) for (const locale of ['ja', 'ko', 'de']) for (const scene of Object.keys(routes)) for (const width of [1440, 1280, 768, 390]) {
    const run = await scenePage(scene, 'regular', locale, width);
    try {
      const page = run.page;
      if (width < 768) { await page.locator('.cm-management-header button').first().click(); await shot(page, `final-${scene}-${locale}-navigation-${width}`); await page.keyboard.press('Escape'); }
      // Actual account locale list exercises translated toolbars + bounded long labels.
      await page.locator('.cm-account > summary').click();
      await page.locator('.cm-shell-language button').click();
      const list = page.getByRole('listbox'); await list.waitFor();
      const box = await list.boundingBox(); assert(box.x >= 0 && box.x + box.width <= width + 1, 'locale popover bounded');
      await shot(page, `final-${scene}-${locale}-popover-${width}`);
    } finally { await run.close(); }
  }
  for (const locale of ['en', 'zh']) {
  const run = await scenePage('instances', 'regular', locale, 1440);
  try {
    const page = run.page;
    await page.getByRole('button', { name: locale === 'zh' ? '搜索实例' : 'Search instances', exact: true }).click();
    await shot(page, `final-instances-${locale}-search-1440`);
    await page.getByRole('textbox', { name: locale === 'zh' ? '搜索实例' : 'Search instances', exact: true }).press('Escape');
    await page.getByRole('button', { name: locale === 'zh' ? '按实例类型筛选: 全部类型' : 'Filter by instance type: All types', exact: true }).click();
    await shot(page, `final-instances-${locale}-filter-1440`); await page.keyboard.press('Escape');
    const row = page.getByRole('row').filter({ hasText: 'Research workspace' });
    await row.getByRole('checkbox').check();
    await contrast(page, '.cm-instance-row[aria-selected="true"] .cm-instance-id, .cm-instance-row[aria-selected="true"] .cm-management-identity, .cm-management-header .cm-current-page, .app-nav-link-active', 'selected row and shell');
    await row.focus(); await page.keyboard.press('Tab'); await page.keyboard.press('Shift+Tab');
    assert(await page.locator(':focus-visible').count(), 'keyboard focus is visible');
    assert(await page.locator(':focus-visible').evaluate(el => { const s = getComputedStyle(el); return parseFloat(s.outlineWidth) >= 2 || s.boxShadow !== 'none'; }), 'keyboard focus has a visible outline/ring');
    await page.getByRole('button', { name: `${locale === 'zh' ? '操作' : 'Actions'}: Research workspace`, exact: true }).click();
    await contrast(page, '[role="menuitem"]', 'row menu');
    await shot(page, `final-instances-${locale}-row-menu-1440`);
    await page.getByRole('menuitem', { name: locale === 'zh' ? /删除/ : /Delete/ }).click();
    await page.getByRole('dialog').waitFor(); await shot(page, `final-instances-${locale}-confirm-1440`);
    await page.keyboard.press('Escape');
    assert(await page.getByRole('button', { name: `${locale === 'zh' ? '操作' : 'Actions'}: Research workspace`, exact: true }).evaluate(el => el === document.activeElement), 'modal returns focus');
    await page.keyboard.press('Tab');
    await shot(page, `final-instances-${locale}-selected-focus-1440`);
    await page.getByRole('button', { name: locale === 'zh' ? '收起侧栏' : 'Collapse sidebar' }).click();
    await shot(page, `final-instances-${locale}-collapsed-1440`);
  } finally { await run.close(); }
  const runtime = await scenePage('runtimes', 'regular', locale, 1440);
  try {
    await runtime.page.getByRole('row').filter({ hasText: 'runtime-198' }).hover();
    await contrast(runtime.page, '.runtime-table tbody tr:first-child td, .runtime-table tbody tr:first-child .runtime-scheduling', 'hovered runtime node and counts');
    await runtime.page.getByText('runtime-198', { exact: true }).click();
    await runtime.page.getByRole('heading', { name: 'runtime-198', exact: true }).waitFor();
    await contrast(runtime.page, 'main h1, main th, main p', 'runtime detail secondary text');
    await shot(runtime.page, `final-runtimes-${locale}-details-1440`);
    await runtime.page.setViewportSize({ width: 390, height: 900 });
    assert(await runtime.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    await shot(runtime.page, `final-runtimes-${locale}-details-390`);
  } finally { await runtime.close(); }
  const team = await scenePage('teams', 'regular', locale, 1440);
  try {
    await contrast(team.page, '.team-chat-heading, .team-chat-subtitle>span', 'team object and auxiliary labels');
    const composer = team.page.getByRole('textbox', { name: locale === 'zh' ? '任务内容' : 'Task content', exact: true });
    await composer.fill('Draft survives viewport shrink'); await composer.focus();
    await team.page.setViewportSize({ width: 390, height: 560 });
    assert.equal(await composer.inputValue(), 'Draft survives viewport shrink');
    const send = team.page.getByRole('button', { name: locale === 'zh' ? '发送任务' : 'Send task', exact: true });
    const bounds = await send.boundingBox(); assert(bounds && bounds.y + bounds.height <= 560, 'send remains visible under simulated keyboard-height viewport');
    await shot(team.page, `final-teams-${locale}-shrunken-viewport-390`);
    await team.page.setViewportSize({ width: 1440, height: 900 });
    await team.page.goto(origin + '/teams');
    await team.page.getByRole('table').waitFor();
    await team.page.getByRole('row').filter({ hasText: 'delivery-team' }).hover();
    await contrast(team.page, '.cm-team-table tbody td, .cm-team-table .cm-team-identity>span', 'hovered team ID and date');
    await shot(team.page, `final-team-list-${locale}-hover-1440`);
  } finally { await team.close(); }
  }
  assert.deepEqual([...unhandledPaths], [], 'every consumed API has an explicit fixture contract');
  await fs.writeFile(path.join(output, 'rendered-contrast.json'), JSON.stringify(contrastSamples, null, 2) + '\n');
  const previous = process.env.VISUAL_OVERLAYS_ONLY ? JSON.parse(await fs.readFile(path.join(output, 'matrix.json'), 'utf8').catch(() => '{"screenshots":[]}')).screenshots : [];
  const screenshots = [...new Set([...previous, ...results])];
  await fs.writeFile(path.join(output, 'matrix.json'), JSON.stringify({ origin, source: 'actual routes; explicit isolated API fixtures', apiPaths: [...apiPaths].sort(), screenshots, count: screenshots.length }, null, 2) + '\n');
  console.log(`PASS ${results.length} captures: responsive routes/states, locale popovers, scene/auth isolation, single-line management tables, focus return, details`);
} finally { await browser.close(); if (server) await new Promise(resolve => server.close(resolve)); }
