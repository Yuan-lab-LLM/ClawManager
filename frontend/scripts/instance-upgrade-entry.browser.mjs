// Production-page fixture: no live cluster, credentials or model calls.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';
const { chromium } = createRequire(new URL('../../e2e/package.json', import.meta.url))('playwright');
const dist = path.resolve(import.meta.dirname, '../dist');
const server = http.createServer((req, res) => {
  let file = path.resolve(dist, '.' + new URL(req.url, 'http://fixture').pathname);
  if (!file.startsWith(dist + path.sep) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(dist, 'index.html');
  res.setHeader('Content-Type', ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[path.extname(file)] || 'application/octet-stream');
  fs.createReadStream(file).pipe(res);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  for (const locale of ['zh', 'en']) {
    const page = await browser.newPage();
    const requests = [], errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(locale => {
      localStorage.setItem('access_token', 'fixture');
      localStorage.setItem('clawmanager_locale', locale);
    }, locale);
    const instance = { id: 42, user_id: 1, name: 'OpenCode fixture', type: 'opencode', instance_mode: 'lite', runtime_type: 'gateway', status: 'running', runtime_generation: 8, created_at: '2026-11-17T00:00:00Z' };
    await page.route('**/api/v1/**', async route => {
      const request = route.request(), pathname = new URL(request.url()).pathname;
      requests.push({ pathname, method: request.method() });
      let data = {};
      if (pathname.endsWith('/auth/me')) data = { id: 1, username: 'admin', role: 'admin' };
      else if (pathname.endsWith('/instances/42')) data = { instance };
      else if (pathname.endsWith('/instances')) data = { instances: [instance], total: 1, page: 1, page_size: 100 };
      else if (pathname.endsWith('/status')) data = { instance_status: { status: 'running', availability: 'available' } };
      else if (pathname.endsWith('/model-catalog-sync')) data = { state: 'unknown', reason: 'not_reported', generation: 8, model_count: 0 };
      else if (pathname.endsWith('/access')) data = { access_url: '/native-fixture', expires_at: new Date(Date.now() + 3600000).toISOString() };
      else if (pathname.endsWith('/runtime')) data = { runtime: {}, agent: {}, commands: [] };
      else if (pathname.endsWith('/workspace/files')) data = { entries: [] };
      else if (pathname.endsWith('/external-access')) data = { enabled: false };
      else data = { items: [], skills: [], sessions: [], teams: [], images: [], capabilities: {} };
      await route.fulfill({ json: { success: true, data } }).catch(() => {});
    });
    await page.route('**/native-fixture', route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><h1>Native session fixture</h1>' }));
    await page.goto(`http://127.0.0.1:${server.address().port}/instances/42`);
    await page.locator('.cm-instance-frame-identity').waitFor().catch(async error => {
      console.error({ url: page.url(), errors, requests, body: (await page.locator('body').innerText()).slice(0, 2500) });
      throw error;
    });
    const management = page.getByRole('button', { name: locale === 'zh' ? '运行管理' : 'Runtime management', exact: true });
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      assert.equal(await page.getByText(/升级前排空检查|Pre-upgrade drain check|治理：等待运行时确认/).count(), 0);
      assert.equal(await management.isVisible(), true);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `overflow at ${locale}/${width}`);
      if (process.env.UI_SCREENSHOT_DIR) {
        fs.mkdirSync(process.env.UI_SCREENSHOT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.UI_SCREENSHOT_DIR, `${locale}-${width}.png`) });
      }
    }
    await management.click();
    assert.equal(await page.getByRole('menuitem', { name: locale === 'zh' ? '重启' : 'Restart', exact: true }).isVisible(), true);
    assert.equal(requests.some(r => /runtime-(maintenance|governance)|runtime-rollouts/.test(r.pathname)), false, 'instance page must not poll or submit per-instance upgrade checks');
    assert.equal(requests.some(r => r.method !== 'GET' && !r.pathname.endsWith('/access')), false, 'opening the page must not mutate instance state');
    assert.deepEqual(errors, []);
    await page.close();
  }
  console.log('PASS: no per-instance upgrade entry or governance polling; normal runtime actions retained; zh/en at four viewport widths');
} finally {
  await browser.close();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
