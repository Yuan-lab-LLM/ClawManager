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
const pods = Array.from({ length: 18 }, (_, i) => ({ id: i + 1, runtime_type: types[i % 4], namespace: 'runtime', deployment_name: 'default', pod_name: `runtime-default-long-name-${i + 1}`, node_name: 'worker-01', pod_ip: `10.42.0.${i + 1}`, image_ref: 'image@sha256:fixture', state: 'ready', capacity: 16, used_slots: 12, draining: false, agent_reported: true, scheduling_enabled: i % 3 !== 0, cpu_millis_used: 1000, cpu_millis_limit: 4000, memory_bytes_used: 1024 ** 3, memory_bytes_limit: 8 * 1024 ** 3, slots: Array.from({ length: 16 }, (_, n) => ({ index: n + 1, container_name: `agent-${n + 1}`, ready: n % 6 !== 5, restart_count: 0, state: 'running', ...(n % 6 < 4 ? { instance_id: i * 100 + n + 1, instance_state: states[n % 6], owner: i === 17 ? 'off-page-owner' : 'owner' } : {}) })) }));
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
      return send({ pods: matches.slice((current - 1) * 16, current * 16), pagination: { page: current, page_size: 16, total: matches.length, total_pages: Math.ceil(matches.length / 16) }, summary, nodes: ['worker-01'], updated_at: '2026-10-28T09:00:00Z' });
    }
    return send({});
  });
  for (const viewport of [{ width: 1440, height: 900 }, { width: 1920, height: 1080 }, { width: 1280, height: 900 }]) {
    await page.setViewportSize(viewport);
    await page.goto(`http://127.0.0.1:${server.address().port}/admin/runtime-pods`);
    await page.locator('article').nth(15).waitFor();
    const dimensions = await page.evaluate(() => {
      const grid = document.querySelector('.runtime-card-grid');
      const cards = [...grid.querySelectorAll('article')];
      return { firstCard: { height: cards[0].clientHeight, scroll: cards[0].scrollHeight, children: [...cards[0].children].map(child => ({ text: child.textContent, height: child.getBoundingClientRect().height })) }, body: document.documentElement.scrollHeight, viewport: innerHeight, gridHeight: grid.clientHeight, gridScroll: grid.scrollHeight, columns: getComputedStyle(grid).gridTemplateColumns.split(' ').length, rows: new Set(cards.map(card => card.getBoundingClientRect().top)).size, cardOverflow: cards.map(card => card.scrollHeight > card.clientHeight + 1), minFont: Math.min(...cards.flatMap(card => [...card.querySelectorAll("*")].filter(element => element.getClientRects().length && element.textContent.trim()).map(element => parseFloat(getComputedStyle(element).fontSize)))), overlappingRows: cards.some(card => [...card.children].some((child, index, children) => index > 0 && child.getBoundingClientRect().top < children[index - 1].getBoundingClientRect().bottom - .5)), collapsedContent: cards.some(card => [...card.children].some(child => child.getBoundingClientRect().height === 0)), actionRows: cards.map(card => new Set([...card.querySelectorAll(".runtime-card-actions button")].map(button => button.getBoundingClientRect().top)).size), slots: cards.map(card => { const slots = card.querySelector('.grid'); return { count: slots?.querySelectorAll('button').length, height: slots?.clientHeight, scroll: slots?.scrollHeight }; }) };
    });
    assert.equal(dimensions.columns, 4); assert.equal(dimensions.rows, 4);
    assert.ok(dimensions.body <= dimensions.viewport + 1, JSON.stringify({ locale, viewport, dimensions }));
    assert.ok(dimensions.gridScroll <= dimensions.gridHeight + 1, JSON.stringify({ locale, viewport, dimensions }));
    assert.ok(dimensions.minFont >= (viewport.width >= 1600 && viewport.height >= 950 ? 14 : 12), JSON.stringify({ locale, viewport, dimensions }));
    assert.equal(dimensions.overlappingRows, false, JSON.stringify({ locale, viewport, dimensions }));
    assert.equal(dimensions.collapsedContent, false, JSON.stringify({ locale, viewport, dimensions }));
    assert.ok(dimensions.actionRows.every(rows => rows <= 2), JSON.stringify({ locale, viewport, dimensions }));
    assert.ok(dimensions.cardOverflow.every(value => !value), JSON.stringify({ locale, viewport, dimensions }));
    assert.ok(dimensions.slots.every(slot => slot.count === 16 && slot.scroll <= slot.height), JSON.stringify({ locale, viewport, dimensions }));
    if (viewport.width === 1440) await page.screenshot({ path: `/tmp/cm-runtime-grid-${locale}.png` });
    console.log('PASS sixteen cards and all sixteen slots visible without scrolling', locale, viewport);
  }
  const firstCard = page.locator('article').first();
  const actions = firstCard.getByRole('button', { name: locale === 'en' ? 'Actions' : '操作', exact: true });
  await actions.focus();
  await page.keyboard.press('Enter');
  const popup = firstCard.locator('[popover]');
  await popup.waitFor({ state: 'visible' });
  const popupBox = await popup.boundingBox();
  const popupViewport = page.viewportSize();
  assert.ok(popupBox.x >= 0 && popupBox.y >= 0 && popupBox.x + popupBox.width <= popupViewport.width && popupBox.y + popupBox.height <= popupViewport.height);
  assert.equal(await popup.getByRole('button').count(), 4);
  await page.keyboard.press('Tab');
  assert.equal(await popup.getByRole('button').first().evaluate(element => element === document.activeElement), true);
  await page.keyboard.press('Escape');
  await popup.waitFor({ state: 'hidden' });
  assert.equal(await actions.evaluate(element => element === document.activeElement), true);
  assert.equal(await page.locator('article').count(), 16, 'opening/closing Actions must not select the parent card');
  await actions.click();
  await page.mouse.click(400, 80);
  await popup.waitFor({ state: 'hidden' });
  assert.equal(await page.locator('article').count(), 16);
  await actions.click();
  await popup.getByRole('button', { name: locale === 'en' ? 'Event log' : '事件日志', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.waitFor({ state: 'visible' });
  assert.equal(await page.locator('article').count(), 16, 'menu action must not select the parent card');
  await page.keyboard.press('Escape');
  await dialog.waitFor({ state: 'detached' });
  const secondCard = page.locator('article').nth(1);
  await secondCard.getByRole('button', { name: locale === 'en' ? 'Actions' : '操作', exact: true }).click();
  const previousMutations = mutations.length;
  const schedulingResponse = page.waitForResponse(response => response.url().endsWith("/runtime-pods/2/scheduling") && response.request().method() === "POST");
  await secondCard.locator('[popover]').getByRole('button', { name: locale === 'en' ? 'Pause scheduling' : '暂停调度', exact: true }).click();
  await page.waitForFunction(() => document.querySelectorAll('article').length === 16);
  await schedulingResponse;
  assert.deepEqual(mutations.slice(previousMutations), [{ path: '/api/v1/admin/runtime-pods/2/scheduling', body: { enabled: false } }]);
  assert.equal(await page.locator('article').count(), 16);
  await actions.click();
  await page.setViewportSize({ width: 1280, height: 899 });
  await popup.waitFor({ state: 'hidden' });
  await page.setViewportSize({ width: 1280, height: 900 });
  console.log('PASS Actions keyboard, viewport containment, dismiss, actual Events/Pause actions and article propagation', locale);
  for (const viewport of [{ width: 1440, height: 800 }, { width: 900, height: 900 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({ body: document.documentElement.scrollHeight, width: document.documentElement.scrollWidth, viewport: innerHeight, viewportWidth: innerWidth, columns: getComputedStyle(document.querySelector('.runtime-card-grid')).gridTemplateColumns.split(' ').length, overflowingCards: [...document.querySelectorAll('article')].some(card => card.scrollHeight > card.clientHeight + 1) }));
    assert.equal(dimensions.columns, viewport.width >= 1280 ? 4 : viewport.width >= 768 ? 2 : 1);
    assert.ok(dimensions.width <= dimensions.viewportWidth + 1, JSON.stringify({ locale, viewport, dimensions }));
    if (viewport.width >= 768) assert.ok(dimensions.body <= dimensions.viewport + 1, JSON.stringify({ locale, viewport, dimensions }));
    assert.equal(dimensions.overflowingCards, false, JSON.stringify({ locale, viewport, dimensions }));
    console.log('PASS responsive inventory without clipped cards', locale, viewport);
  }
  await page.setViewportSize({ width: 1280, height: 900 });
  assert.ok(requests.filter(request => request.page).every(request => request.page_size === '16'), JSON.stringify(requests));
  await page.getByRole('button', { name: locale === 'en' ? 'Next page' : '下一页', exact: true }).click();
  await page.waitForFunction(() => document.querySelectorAll('article').length === 2);
  await page.getByRole('textbox', { name: locale === 'en' ? 'Search Pods, instances or owners' : '搜索 Pod、实例或使用者' }).fill('off-page-owner');
  await page.waitForFunction(() => document.querySelectorAll('article').length === 1);
  assert.ok(requests.some(request => request.search === 'off-page-owner' && request.page === '1'));
  assert.deepEqual(errors, []);
  console.log('PASS server pagination and off-page search', locale);
  await page.close();
  }
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
