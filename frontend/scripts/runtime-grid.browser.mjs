// Local production-layout regression. Every API request is fulfilled by fixtures.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';
const requireE2E = createRequire(new URL('../../e2e/package.json', import.meta.url));
const { chromium } = requireE2E('playwright');
const dist = path.resolve(import.meta.dirname, '../dist');
const server = http.createServer((request, response) => {
  let file = path.resolve(dist, '.' + new URL(request.url, 'http://local').pathname);
  if (!file.startsWith(dist + path.sep)) file = path.join(dist, 'index.html');
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
  response.setHeader('Content-Type', ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[path.extname(file)] || 'application/octet-stream');
  fs.createReadStream(file).pipe(response);
});
const types = ['openclaw', 'hermes', 'opencode', 'deepseek-harness'];
const states = ['running', 'starting', 'migrating', 'error', 'idle', 'unavailable'];
const pods = Array.from({ length: 18 }, (_, i) => ({ id: i + 1, runtime_type: types[i % 4], namespace: 'runtime', deployment_name: 'default', pod_uid: `uid-${i + 1}`, bindings_reported: true, bound_instances: 12, pod_name: `runtime-default-long-name-${i + 1}`, node_name: 'worker-01', pod_ip: `10.42.0.${i + 1}`, image_ref: 'image@sha256:fixture', state: 'ready', capacity: 16, used_slots: 12, draining: false, agent_reported: true, scheduling_enabled: i % 3 !== 0, cpu_millis_used: 1000, cpu_millis_limit: 4000, memory_bytes_used: 1024 ** 3, memory_bytes_limit: 8 * 1024 ** 3, slots: Array.from({ length: 16 }, (_, n) => ({ index: n + 1, container_name: `agent-${n + 1}`, ready: n % 6 !== 5, restart_count: 0, state: 'running', ...(n % 6 < 4 ? { instance_id: i * 100 + n + 1, instance_state: states[n % 6], owner: i === 17 ? 'off-page-owner' : 'owner' } : {}) })) }));
const summary = { pods: 18, online: 18, schedulable: 18, capacity: 288, ready: 252, occupied: 216, available: 36, running: 54, starting: 54, migrating: 54, error: 54, idle: 36, unavailable: 36 };
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : { channel: 'chrome' }) });
try {
  for (const locale of ['en', 'zh']) {
  const page = await browser.newPage();
  const errors = []; const requests = []; const mutations = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(locale => { localStorage.setItem('access_token', 'fixture'); localStorage.setItem('clawmanager_locale', locale); }, locale);
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    const send = data => route.fulfill({ json: { data } });
    if (url.pathname.endsWith('/auth/me')) return send({ id: 1, username: 'fixture-admin', role: 'admin', status: 'active' });
    if (url.pathname.endsWith('/version')) return send({ version: 'fixture' });
    if (url.pathname.endsWith('/scheduling') && route.request().method() === 'POST') { mutations.push({ path: url.pathname, body: route.request().postDataJSON() }); return send({}); }
    if (url.pathname.endsWith('/runtime-pods')) {
      requests.push(Object.fromEntries(url.searchParams));
      const matches = url.searchParams.get('search') ? pods.slice(17) : pods;
      const current = Number(url.searchParams.get('page') || 1);
      return send({ pods: url.searchParams.has('page') ? matches.slice((current - 1) * 16, current * 16) : pods, pagination: { page: current, page_size: 16, total: matches.length, total_pages: Math.ceil(matches.length / 16) }, summary, nodes: ['worker-01'], updated_at: '2026-10-28T09:00:00Z' });
    }
    if (url.pathname.endsWith('/events')) return send({ events: [] });
    if (url.pathname.endsWith('/runtime-scheduling')) return send({ items: [], supported_modes: [] });
    return send({});
  });
  for (const viewport of [{ width: 1440, height: 900 }, { width: 1920, height: 1080 }, { width: 1280, height: 900 }]) {
    await page.setViewportSize(viewport);
    await page.goto(`${process.env.FRONTEND_URL || `http://127.0.0.1:${server.address().port}`}/admin/runtime-pods`);
    await page.getByRole('table').locator('tbody tr').nth(15).waitFor();
    const dimensions = await page.getByRole('table').evaluate(table => {
      const rows = [...table.tBodies[0].rows];
      return { pageWidth: document.documentElement.scrollWidth, viewportWidth: innerWidth,
        rows: rows.map(row => { const rect = row.getBoundingClientRect(); return { top: rect.top, bottom: rect.bottom }; }),
        minimumFont: Math.min(...rows.flatMap(row => [...row.querySelectorAll('td, button, span')].filter(el => el.checkVisibility() && el.textContent.trim()).map(el => parseFloat(getComputedStyle(el).fontSize)))),
        distributions: rows.map(row => { const image = row.querySelector('[role="img"]'); const bounds = image?.getBoundingClientRect(); return { count: (image?.getAttribute('aria-label')?.match(/\d+/g) || []).map(Number).reduce((sum, n) => sum + n, 0), marks: image?.children.length, visible: image && [...image.children].every(mark => { const r = mark.getBoundingClientRect(); return r.width > 0 && r.height > 0 && r.left >= bounds.left - 1 && r.right <= bounds.right + 1; }) }; }),
        actionRows: rows.map(row => { const centers = [...row.querySelectorAll('td:last-child button')].filter(el => el.checkVisibility()).map(el => { const r = el.getBoundingClientRect(); return r.top + r.height / 2; }); return Math.max(...centers) - Math.min(...centers); }) };
    });
    assert.equal(dimensions.rows.length, 16, 'the first server page retains sixteen Pods');
    assert.ok(dimensions.pageWidth <= dimensions.viewportWidth + 1, JSON.stringify({ locale, viewport, dimensions }));
    assert.ok(dimensions.minimumFont >= 12);
    assert.ok(dimensions.rows.every((row, i, rows) => row.bottom > row.top && (!i || row.top >= rows[i - 1].bottom - 1)), 'table rows do not overlap or collapse');
    assert.ok(dimensions.actionRows.every(spread => spread <= 1), 'row actions stay on one line');
    assert.ok(dimensions.distributions.every(d => d.count === 16 && d.marks === 16 && d.visible), 'all sixteen slots of each Pod remain represented, visible and accessible');
    if (viewport.width === 1440) await page.screenshot({ path: `/tmp/cm-runtime-table-${locale}.png` });
    console.log('PASS sixteen table rows, readable geometry and slot distributions', locale, viewport);
  }
  const firstCard = page.getByRole('table').locator('tbody tr').first();
  const actions = firstCard.getByRole('button', { name: new RegExp(`^${locale === 'en' ? 'Actions' : '操作'}:`) });
  await actions.focus();
  await page.keyboard.press('Enter');
  const popup = page.getByRole('menu');
  await popup.waitFor({ state: 'visible' });
  const popupBox = await popup.boundingBox();
  const popupViewport = page.viewportSize();
  assert.ok(popupBox.x >= 0 && popupBox.y >= 0 && popupBox.x + popupBox.width <= popupViewport.width && popupBox.y + popupBox.height <= popupViewport.height);
  assert.equal(await popup.getByRole('menuitem').count(), 5);
  assert.equal(await popup.getByRole('menuitem').first().evaluate(element => element === document.activeElement), true);
  await page.keyboard.press('Escape');
  await popup.waitFor({ state: 'hidden' });
  assert.equal(await actions.evaluate(element => element === document.activeElement), true);
  assert.equal(await page.getByRole('table').locator('tbody tr').count(), 16, 'opening/closing Actions must not enter Pod details');
  await actions.click();
  await page.mouse.click(400, 80);
  await popup.waitFor({ state: 'hidden' });
  assert.equal(await page.getByRole('table').locator('tbody tr').count(), 16);
  await actions.click();
  await popup.getByRole('menuitem', { name: locale === 'en' ? 'Event log' : '事件日志', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.waitFor({ state: 'visible' });
  assert.equal(await page.getByRole('table').locator('tbody tr').count(), 16, 'menu action must not enter Pod details');
  await page.keyboard.press('Escape');
  await dialog.waitFor({ state: 'hidden' });
  const secondCard = page.getByRole('table').locator('tbody tr').nth(1);
  await secondCard.getByRole('button', { name: new RegExp(`^${locale === 'en' ? 'Actions' : '操作'}:`) }).click();
  const previousMutations = mutations.length;
  const schedulingResponse = page.waitForResponse(response => response.url().endsWith("/runtime-pods/2/scheduling") && response.request().method() === "POST");
  await page.getByRole('menu').getByRole('menuitem', { name: locale === 'en' ? 'Pause scheduling' : '暂停调度', exact: true }).click();
  await page.waitForFunction(() => document.querySelectorAll('table tbody tr').length === 16);
  await schedulingResponse;
  assert.deepEqual(mutations.slice(previousMutations), [{ path: '/api/v1/admin/runtime-pods/2/scheduling', body: { enabled: false } }]);
  assert.equal(await page.getByRole('table').locator('tbody tr').count(), 16);
  await actions.click();
  await page.setViewportSize({ width: 1280, height: 899 });
  const resizedBox = await popup.boundingBox();
  assert.ok(resizedBox && resizedBox.x >= 0 && resizedBox.y >= 0 && resizedBox.x + resizedBox.width <= 1280 && resizedBox.y + resizedBox.height <= 899, 'resize keeps the shared menu bounded');
  await page.keyboard.press('Escape');
  await popup.waitFor({ state: 'hidden' });
  await page.setViewportSize({ width: 1280, height: 900 });
  console.log('PASS Actions keyboard, viewport containment, dismiss, actual Events/Pause actions and row action isolation', locale);
  for (const viewport of [{ width: 1440, height: 800 }, { width: 900, height: 900 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.getByRole('table').evaluate(table => {
      const scroll = table.parentElement, rect = scroll.getBoundingClientRect();
      return { pageWidth: document.documentElement.scrollWidth, viewportWidth: innerWidth,
        tableWidth: table.getBoundingClientRect().width, containerWidth: rect.width, rows: table.tBodies[0].rows.length,
        insideViewport: rect.left >= 0 && rect.right <= innerWidth + 1 };
    });
    assert.equal(dimensions.rows, 16);
    assert.ok(dimensions.pageWidth <= dimensions.viewportWidth + 1, JSON.stringify({ locale, viewport, dimensions }));
    assert.ok(dimensions.insideViewport, 'table overflow remains local');
    if (viewport.width < 1280) assert.ok(dimensions.tableWidth > dimensions.containerWidth, 'wide columns scroll locally');
    const scroller = page.getByRole('table').locator('..');
    await scroller.evaluate(el => { el.scrollLeft = el.scrollWidth; });
    await page.getByRole('table').locator('tbody tr').first().getByRole('button', { name: new RegExp(`^${locale === 'en' ? 'Actions' : '操作'}:`) }).click();
    const menu = page.getByRole('menu'); await menu.waitFor();
    const box = await menu.boundingBox(); assert.ok(box.x >= 0 && box.x + box.width <= viewport.width + 1, 'narrow action menu remains reachable');
    await page.keyboard.press('Escape');
    await scroller.evaluate(el => { el.scrollLeft = 0; });
    console.log('PASS responsive table with local horizontal scrolling and reachable actions', locale, viewport);
  }
  await page.setViewportSize({ width: 1280, height: 900 });
  assert.ok(requests.filter(request => request.page).every(request => request.page_size === '16'), JSON.stringify(requests));
  await page.getByRole('button', { name: locale === 'en' ? 'Next page' : '下一页', exact: true }).click();
  await page.waitForFunction(() => document.querySelectorAll('table tbody tr').length === 2);
  await page.getByRole('button', { name: locale === 'en' ? 'Search Pods, instances or owners' : '搜索 Pod、实例或使用者', exact: true }).click();
  await page.getByRole('textbox', { name: locale === 'en' ? 'Search Pods, instances or owners' : '搜索 Pod、实例或使用者' }).fill('off-page-owner');
  await page.waitForFunction(() => document.querySelectorAll('table tbody tr').length === 1);
  assert.ok(requests.some(request => request.search === 'off-page-owner' && request.page === '1'));
  assert.deepEqual(errors, []);
  await page.getByRole('button', { name: pods[17].pod_name, exact: true }).press('Enter');
  await page.getByRole('heading', { name: pods[17].pod_name, exact: true }).waitFor();
  console.log('PASS server pagination, off-page search and keyboard Pod detail entry', locale);
  await page.close();
  }
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
