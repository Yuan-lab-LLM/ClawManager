// Current source UI against real CM APIs; only the four recorded singles may wake.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { createServer } from '../node_modules/vite/dist/node/index.js';
const require = createRequire(
  new URL('../../e2e/package.json', import.meta.url),
);
const { chromium } = require('playwright');
const out = process.env.SLEEP_LIVE_OUTPUT;
assert.ok(out, 'Set SLEEP_LIVE_OUTPUT to the local evidence directory');
const ready = JSON.parse(
  await fs.readFile(path.join(out, 'sleep-ready.json'), 'utf8'),
);
const targets = ready.instances.filter(
  (instance) => instance.purpose === 'single',
);
const token = execFileSync(
  'ssh',
  [
    'root@10.130.81.54',
    'cat /root/cm-uiux2-sleep-20261116/admin-token.private',
  ],
  { encoding: 'utf8' },
).trim();
const base = 'https://10.130.81.54:30443';
const starts = [],
  results = [];
let vite, browser, browserServer;
try {
  vite = await createServer({
    root: path.resolve(import.meta.dirname, '..'),
    server: { port: 0, host: '127.0.0.1', hmr: false },
    logLevel: 'error',
  });
  await vite.listen();
  browserServer = await chromium.launchServer({
    headless: true,
    ...(process.env.CHROME_PATH
      ? { executablePath: process.env.CHROME_PATH }
      : {}),
    args: ['--no-sandbox'],
  });
  browser = await chromium.connect(browserServer.wsEndpoint());
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    ignoreHTTPSErrors: true,
    reducedMotion: 'reduce',
  });
  await context.addInitScript((value) => {
    localStorage.setItem('access_token', value);
    localStorage.setItem('clawmanager_locale', 'zh');
  }, token);
  await context.routeWebSocket('**/api/**', (socket) => socket.close());
  await context.route('**/api/**', async (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      method = req.method();
    if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
      const match = u.pathname.match(/^\/api\/v1\/instances\/(\d+)\/start$/);
      if (
        method !== 'POST' ||
        !match ||
        !targets.some((target) => target.id === Number(match[1]))
      )
        return route.abort('blockedbyclient');
      starts.push({ id: Number(match[1]), at: new Date().toISOString() });
    }
    const response = await context.request.fetch(base + u.pathname + u.search, {
      method,
      headers: {
        Authorization: 'Bearer ' + token,
        'Content-Type': 'application/json',
        Origin: base,
        'Sec-Fetch-Site': 'same-origin',
      },
      ...(req.postDataBuffer() ? { data: req.postDataBuffer() } : {}),
      timeout: 120000,
    });
    await route.fulfill({ response });
  });
  for (const target of targets) {
    const page = await context.newPage();
    page.setDefaultTimeout(30000);
    const tick = Date.now();
    try {
      await page.goto(vite.resolvedUrls.local[0] + 'instances/' + target.id);
      const pane = page.locator('.wb-instance-agent');
      const wake = pane.getByRole('button', { name: '唤醒', exact: true });
      await wake.waitFor();
      assert.equal(
        await pane.locator('[data-agent-state="sleeping"]').count(),
        1,
      );
      assert.equal(
        await pane.getByText('Unavailable', { exact: true }).count(),
        0,
      );
      await page.screenshot({
        path: path.join(out, `live-${target.type}-sleep.jpg`),
        animations: 'disabled',
        timeout: 15000,
      });
      await wake.focus();
      await page.keyboard.press('Enter');
      await page.waitForFunction(
        () =>
          document.querySelector('.wb-instance-agent button[aria-busy="true"]')
            ?.disabled,
      );
      await page.screenshot({
        path: path.join(out, `live-${target.type}-waking.jpg`),
        animations: 'disabled',
        timeout: 15000,
      });
      const end = Date.now() + 150000;
      let item;
      while (Date.now() < end) {
        const response = await context.request.get(
          `${base}/api/v1/instances/${target.id}`,
          { headers: { Authorization: 'Bearer ' + token } },
        );
        item = (await response.json()).data.instance;
        if (item.status === 'running' || item.status === 'error') break;
        await new Promise((resolve) => setTimeout(resolve, 1000));
      }
      assert.equal(item.status, 'running');
      assert.ok(item.runtime_generation > target.before_generation);
      assert.equal(starts.filter((start) => start.id === target.id).length, 1);
      results.push({
        type: target.type,
        id: target.id,
        result: 'PASS',
        seconds: (Date.now() - tick) / 1000,
        generation: item.runtime_generation,
        keyboard: true,
        pendingDisabled: true,
        startRequests: 1,
      });
    } catch (error) {
      results.push({
        type: target.type,
        id: target.id,
        result: 'FAIL',
        error: error.message,
      });
    } finally {
      await page.close();
    }
    console.log(JSON.stringify(results.at(-1)));
    await fs.writeFile(
      path.join(out, 'live-browser.json'),
      JSON.stringify(
        {
          source: 'UIUX2 working tree',
          backend: base,
          tlsNote:
            'Browser test trusts the private cluster certificate; API runner verifies cluster TLS',
          results,
          starts,
        },
        null,
        2,
      ),
    );
  }
  assert.equal(
    results.filter((result) => result.result === 'PASS').length,
    4,
    'All four live UI wake tests must pass',
  );
} finally {
  await browserServer?.kill();
  vite?.httpServer?.closeAllConnections();
  await vite?.close();
}
