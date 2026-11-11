// Mocked production-page regression; run npm run build before this script.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';
const { chromium } = createRequire(new URL('../../e2e/package.json', import.meta.url))('playwright');
const dist = path.resolve(import.meta.dirname, '../dist');
const server = http.createServer((request, response) => {
  let file = path.resolve(dist, '.' + new URL(request.url, 'http://local').pathname);
  if (!file.startsWith(dist + path.sep) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
  response.setHeader('Content-Type', ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[path.extname(file)] || 'application/octet-stream');
  fs.createReadStream(file).pipe(response);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1500, height: 900 } });
  let state = 'error', reads = 0;
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'fixture'); localStorage.setItem('clawmanager_locale', 'en');
    window.sockets = [];
    window.WebSocket = class {
      static OPEN = 1; readyState = 1;
      constructor(url) { this.url = url; window.sockets.push(this); setTimeout(() => { if (!window.holdSocketOpen) this.onopen?.(); }, 0); }
      close() {} send() {}
    };
  });
  await page.route('**/api/v1/**', async route => {
    const p = new URL(route.request().url()).pathname;
    let data = {};
    if (p.endsWith('/auth/me')) data = { id: 1, username: 'admin', role: 'admin' };
    else if (p.endsWith('/admin/runtime-pods')) {
      reads++;
      const pod = { id: 198, runtime_type: 'openclaw', namespace: 'runtime', pod_name: 'openclaw-test', state: 'ready', capacity: 8, used_slots: 1, bound_instances: 1, agent_reported: true, bindings_reported: true, unknown_slots: 0, reconciliation_state: 'consistent', scheduling_enabled: true,
        slots: Array.from({ length: 8 }, (_, i) => ({ index: i + 1, container_name: 'slot-' + i, ready: true, reconciliation_state: 'consistent', ...(i === 0 ? { instance_id: 199, instance_name: 'openclaw-030', instance_state: state, binding_confirmed: true } : {}) })) };
      data = { pods: [pod], summary: { pods: 1, online: 1, schedulable: 1, capacity: 8, ready: 8, occupied: 1, available: 7, running: state === 'running' ? 1 : 0, starting: 0, migrating: 0, error: state === 'error' ? 1 : 0, idle: 7, unknown: 0, unavailable: 0 }, pagination: { page: 1, page_size: 16, total: 1, total_pages: 1 }, nodes: [], updated_at: new Date().toISOString() };
    }
    await route.fulfill({ json: { data } });
  });
  const visibility = hidden => page.evaluate(hidden => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => hidden ? 'hidden' : 'visible' });
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden });
    document.dispatchEvent(new Event('visibilitychange'));
  }, hidden);
  await page.goto(`http://127.0.0.1:${server.address().port}/admin/runtime-pods`);
  const red = page.getByRole('button', { name: 'Slot 1 · Instance error', exact: true });
  const green = page.getByRole('button', { name: 'Slot 1 · Running', exact: true });
  await red.waitFor(); state = 'running'; await green.waitFor({ timeout: 7000 });
  state = 'error'; await red.waitFor({ timeout: 7000 });
  await visibility(true); const before = reads; state = 'running'; await page.waitForTimeout(6500);
  assert.ok(reads > before, 'background Runtime inventory stopped polling');
  assert.equal(await red.count(), 0, 'recovered slot stayed red');
  await visibility(false); state = 'error'; await red.waitFor({ timeout: 7000 }); state = 'running';
  await page.evaluate(() => window.sockets.find(s => s.url.includes('topic=runtime_admin')).onmessage({ data: JSON.stringify({ type: 'instance_status', data: { instance_id: 199, status: 'running' }, timestamp: new Date().toISOString() }) }));
  await green.waitFor({ timeout: 1400 });
  state = 'error'; await red.waitFor({ timeout: 7000 });
  await page.evaluate(() => { window.holdSocketOpen = true; window.sockets.at(-1).onclose(); });
  await page.waitForFunction(() => window.sockets.length === 2);
  state = 'running'; await page.evaluate(() => window.sockets.at(-1).onopen());
  await green.waitFor({ timeout: 1400 });
  await green.click(); const detailsBefore = reads; await visibility(true); await page.waitForTimeout(6500);
  assert.ok(reads >= detailsBefore + 2, 'background Runtime detail inventory stopped polling');
  console.log('PASS Runtime overview/detail background refresh, lifecycle events and WebSocket reconnect without reload');
} finally { await browser.close(); await new Promise(resolve => { server.close(resolve); server.closeAllConnections(); }); }
