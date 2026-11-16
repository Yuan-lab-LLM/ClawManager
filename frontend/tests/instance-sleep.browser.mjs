import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { createServer } from '../node_modules/vite/dist/node/index.js';
const require = createRequire(
  new URL('../../e2e/package.json', import.meta.url),
);
const { chromium } = require('playwright');
const out =
  process.env.INSTANCE_SLEEP_SCREENSHOTS || '/tmp/cm-instance-sleep-browser';
await fs.mkdir(out, { recursive: true });
let vite,
  browser,
  browserServer,
  type = 'openclaw',
  mode = 'lite',
  state = 'stopped',
  reason = 'idle_timeout';
let releaseWake,
  rejectWake = false,
  starts = 0,
  serviceRequests = 0,
  generation = 1;
const errors = [];
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
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
  });
  page.setDefaultTimeout(30000);
  page.on('pageerror', (error) => {
    if (errors.length < 10) errors.push(error.stack ?? error.message);
  });
  page.on('dialog', (dialog) => {
    errors.push(`Unexpected dialog: ${dialog.message()}`);
    void dialog.dismiss();
  });
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'fixture');
    if (!localStorage.getItem('clawmanager_locale'))
      localStorage.setItem('clawmanager_locale', 'en');
  });
  await page.routeWebSocket('**/api/**', (socket) => socket.close());
  await page.route('**/api/**', async (route) => {
    const request = route.request(),
      p = new URL(request.url()).pathname;
    if (p.endsWith('/start') && request.method() === 'POST') {
      starts++;
      await new Promise((resolve) => {
        releaseWake = resolve;
      });
      releaseWake = null;
      if (rejectWake)
        return route.fulfill({
          status: 409,
          json: { error: 'Runtime lifecycle operation is already in progress' },
        });
      state = 'creating';
      return route.fulfill({ json: { success: true, data: {} } });
    }
    if (
      /\/(access|desktop-access|desktop\/capability|desktop\/bootstrap)$/.test(
        p,
      )
    )
      serviceRequests++;
    const instance = {
      id: 37,
      user_id: 71,
      name: 'Sleep fixture',
      type,
      instance_mode: mode,
      runtime_type: 'gateway',
      status: state,
      stopped_reason: reason,
      runtime_generation: generation,
      cpu_cores: 2,
      memory_gb: 4,
      disk_gb: 10,
      gpu_enabled: false,
      gpu_count: 0,
      os_type: 'linux',
      os_version: 'ubuntu',
      storage_class: 'standard',
      mount_path: '/workspace',
      workspace_path: '/workspace',
      created_at: '2026-11-01T00:00:00Z',
      updated_at: '2026-11-16T00:00:00Z',
    };
    let data = [];
    if (p.endsWith('/auth/me'))
      data = { id: 71, username: 'Fixture', role: 'admin', is_active: true };
    else if (p.endsWith('/version')) data = { version: 'fixture' };
    else if (p === '/api/v1/instances/37') data = { instance };
    else if (p === '/api/v1/instances')
      data = { instances: [instance], total: 1, page: 1, limit: 50 };
    else if (p.endsWith('/status'))
      data = {
        instance_status: {
          instance_id: 37,
          status: state,
          availability:
            state === 'running'
              ? 'available'
              : state === 'creating'
                ? 'starting'
                : 'unavailable',
        },
      };
    else if (p.endsWith('/runtime')) data = { commands: [], skills: [] };
    else if (p.endsWith('/external-access')) data = { external_access: null };
    else if (p.endsWith('/workbench/capabilities'))
      data = {
        generation: 1,
        git: true,
        terminal: true,
        preview: true,
        processes: false,
      };
    else if (p.endsWith('/workspace/files')) data = { entries: [] };
    return route.fulfill({ json: { success: true, data } });
  });
  const ready = async () => {
    await page.goto(vite.resolvedUrls.local[0] + 'instances/37', {
      waitUntil: 'domcontentloaded',
      timeout: 60000,
    });
    await page
      .getByRole('heading', { name: 'Sleep fixture', exact: true })
      .waitFor();
  };
  const pane = page.locator('.wb-instance-agent');
  for (type of ['openclaw', 'hermes', 'opencode', 'deepseek-harness'])
    for (mode of ['lite', 'pro']) {
      state = 'stopped';
      reason = 'idle_timeout';
      rejectWake = false;
      await ready();
      const wake = pane.getByRole('button', { name: 'Wake up', exact: true });
      await wake.waitFor();
      assert.equal(
        await pane.locator('[data-agent-state="sleeping"]').count(),
        1,
        `${type} ${mode}: sleeping icon`,
      );
      assert.equal(
        await pane.getByText('Unavailable', { exact: true }).count(),
        0,
      );
      assert.equal(await pane.locator('iframe').count(), 0);
      assert.equal(
        serviceRequests,
        0,
        'sleeping agents do not bootstrap native services',
      );
      const before = starts;
      await wake.focus();
      await page.keyboard.press('Enter');
      await page.waitForFunction(
        () =>
          document.querySelector('.wb-instance-agent button[aria-busy="true"]')
            ?.disabled,
      );
      assert.equal(starts, before + 1);
      await page.screenshot({
        path: path.join(out, `${type}-${mode}-waking.jpg`),
        animations: 'disabled',
        timeout: 15000,
      });
      releaseWake();
      await pane
        .locator('[data-agent-state="sleeping"]')
        .waitFor({ state: 'hidden' });
      assert.equal(
        await pane
          .getByRole('button', { name: 'Waking up…', exact: true })
          .count(),
        0,
      );
      console.log(
        `PASS ${type} ${mode}: sleep icon, keyboard wake, pending guard and lifecycle transition`,
      );
    }
  type = 'hermes';
  mode = 'lite';
  state = 'stopped';
  rejectWake = true;
  await ready();
  await pane.getByRole('button', { name: 'Wake up', exact: true }).click();
  await page.waitForFunction(
    () =>
      document.querySelector('.wb-instance-agent button[aria-busy="true"]')
        ?.disabled,
  );
  releaseWake();
  await pane
    .getByRole('alert')
    .getByText('Runtime lifecycle operation is already in progress', {
      exact: true,
    })
    .waitFor();
  assert.equal(
    await pane
      .getByRole('button', { name: 'Wake up', exact: true })
      .isEnabled(),
    true,
  );
  console.log('PASS failed wake stays inline and permits retry');
  generation++;
  await pane.getByRole('alert').waitFor({ state: 'hidden' });
  assert.equal(
    await pane
      .getByRole('button', { name: 'Wake up', exact: true })
      .isVisible(),
    true,
  );
  console.log('PASS a newer generation does not display an older wake failure');
  for (const nextState of ['stopped', 'error', 'creating']) {
    state = nextState;
    reason = 'manual';
    await ready();
    assert.equal(
      await pane.getByRole('button', { name: 'Wake up', exact: true }).count(),
      0,
      `${nextState} is not sleep`,
    );
  }
  state = 'stopped';
  reason = 'idle_timeout';
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const locale of ['en', 'zh', 'ja', 'ko', 'de']) {
    await page.evaluate(
      (value) => localStorage.setItem('clawmanager_locale', value),
      locale,
    );
    await ready();
    await pane.locator('[data-agent-state="sleeping"]').waitFor();
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        true,
        `${locale} ${width}: no page overflow`,
      );
      const button = pane.locator('button[aria-busy]');
      assert.equal(await button.isVisible(), true);
      assert.ok((await button.innerText()).length > 0);
      await page.screenshot({
        path: path.join(out, `sleep-${locale}-${width}.jpg`),
        fullPage: true,
        animations: 'disabled',
        timeout: 15000,
      });
    }
    console.log(`PASS ${locale}: sleeping state at 1440 / 1280 / 768 / 390`);
  }
  console.log('Browser errors:', JSON.stringify(errors));
  assert.deepEqual(errors, []);
  console.log(
    `PASS sleeping states in 5 languages / 4 widths; screenshots: ${out}`,
  );
} finally {
  releaseWake?.();
  await browserServer?.kill();
  vite?.httpServer?.closeAllConnections();
  await vite?.close();
}
