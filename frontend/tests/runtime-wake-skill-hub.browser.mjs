import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { createServer } from '../node_modules/vite/dist/node/index.js';
const { chromium } = createRequire(
  new URL('../../e2e/package.json', import.meta.url),
)('playwright');
const root = path.resolve(import.meta.dirname, '..'),
  out = '/tmp/cm-wake-slot';
process.chdir(root);
await fs.mkdir(out, { recursive: true });
const vite = await createServer({
  root,
  server: { port: 0, host: '127.0.0.1', hmr: false },
  logLevel: 'error',
});
await vite.listen();
let server;
const errors = [],
  writes = [],
  results = [];
try {
  server = await chromium.launchServer({
    headless: true,
    ...(process.env.CHROME_PATH
      ? { executablePath: process.env.CHROME_PATH }
      : { channel: 'chrome' }),
  });
  const browser = await chromium.connect(server.wsEndpoint());
  for (const locale of ['en', 'zh', 'ja', 'ko', 'de']) {
    const page = await browser.newPage({ reducedMotion: 'reduce' });
    page.setDefaultTimeout(15000);
    let stage = 'pending',
      content = 'empty';
    page.on('pageerror', (e) => errors.push(e.message));
    await page.addInitScript((locale) => {
      localStorage.setItem('access_token', 'fixture');
      localStorage.setItem('clawmanager_locale', locale);
    }, locale);
    await page.routeWebSocket('**/api/**', (s) => s.close());
    await page.route('**/api/**', async (route) => {
      const req = route.request(),
        url = new URL(req.url()),
        p = url.pathname;
      let data = [];
      if (req.method() !== 'GET') {
        writes.push(p);
        return route.fulfill({
          status: 400,
          json: { error: 'Unexpected mutation' },
        });
      }
      if (p.endsWith('/auth/me'))
        data = {
          id: 1,
          username: 'fixture-admin',
          role: 'admin',
          is_active: true,
        };
      else if (p.endsWith('/version')) data = { version: 'fixture' };
      else if (p.endsWith('/instances')) data = { instances: [], total: 0 };
      else if (p.endsWith('/skill-hub/tags')) {
        if (content === 'loading')
          await new Promise((resolve) => setTimeout(resolve, 1500));
        if (content === 'error')
          return route.fulfill({
            status: 500,
            json: { error: 'Fixture tags unavailable' },
          });
      } else if (p.endsWith('/skill-hub/catalog'))
        data = {
          items:
            content === 'full'
              ? [
                  {
                    id: 701,
                    user_id: 2,
                    name: 'Readable long skill name for interface coverage',
                    description: 'Existing Skill Hub content stays available.',
                    skill_key: 'fixture/skill',
                    visibility: 'public',
                    risk_level: 'low',
                    instance_count: 0,
                    tags: [],
                  },
                ]
              : [],
          total: content === 'full' ? 1 : 0,
        };
      else if (p.endsWith('/admin/runtime-pods')) {
        const slot = {
          index: 1,
          container_name: 'slot-0',
          ready: true,
          ...(stage === 'pending'
            ? {
                expected_instance_id: 701,
                start_pending: true,
                binding_confirmed: false,
                reconciliation_state: 'conflict',
              }
            : {
                instance_id: 701,
                instance_state: stage,
                binding_confirmed: true,
                reconciliation_state:
                  stage === 'unknown' ? 'unknown' : 'consistent',
              }),
        };
        const pod = {
          id: 71,
          runtime_type: 'hermes',
          pod_name: 'hermes-fixture',
          namespace: 'runtime',
          state: 'ready',
          capacity: 8,
          used_slots: 1,
          bound_instances: 1,
          agent_reported: true,
          bindings_reported: true,
          reconciliation_state: stage === 'pending' ? 'conflict' : 'consistent',
          slots: [
            slot,
            ...Array.from({ length: 7 }, (_, i) => ({
              index: i + 2,
              container_name: 'slot-' + (i + 1),
              ready: true,
              reconciliation_state: 'consistent',
            })),
          ],
        };
        data = {
          pods: [pod],
          pagination: { page: 1, page_size: 16, total: 1, total_pages: 1 },
          summary: {
            pods: 1,
            online: 1,
            capacity: 8,
            ready: 8,
            occupied: 1,
            bound_instances: 1,
            available: 7,
          },
          nodes: [],
        };
      }
      return route.fulfill({ json: { data } });
    });
    const visit = async (route) => {
      await page.goto(vite.resolvedUrls.local[0] + route.slice(1), {
        waitUntil: 'domcontentloaded',
      });
      await page
        .locator('.cm-account-name')
        .getByText('fixture-admin', { exact: true })
        .waitFor({ state: 'attached' });
    };
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      stage = 'pending';
      await visit('/admin/runtime-pods');
      const graph = page.locator('.runtime-slot-overview[role="img"]');
      await graph.waitFor();
      assert.equal(await graph.locator('.runtime-slot-starting').count(), 1);
      assert.equal(await graph.locator('.runtime-slot-error').count(), 0);
      await page
        .getByRole('button', { name: 'hermes-fixture', exact: true })
        .click();
      const dialog = page.locator('main');
      const slot = dialog.locator('button[aria-pressed]').first();
      await slot.waitFor();
      assert.match(
        await slot.getAttribute('aria-label'),
        locale === 'zh' ? /启动/ : /Starting/,
      );
      assert.equal(
        await dialog.getByRole('checkbox').first().isDisabled(),
        true,
      );
      if (width === 1440 || width === 390)
        await page.screenshot({
          path: path.join(out, `pending-${locale}-${width}.jpg`),
        });
      content = 'empty';
      await visit('/skill-hub');
      const main = page.locator('main');
      await main.locator('.app-panel').waitFor();
      assert.equal(
        await main
          .getByRole('heading', { name: 'Skill Hub', exact: true })
          .count(),
        0,
      );
      assert.equal(await main.locator('section.app-panel').count(), 1);
      const tabs = main
        .locator('section.app-panel > div')
        .first()
        .getByRole('button');
      assert.equal(await tabs.count(), 3);
      await tabs.nth(1).click();
      await page.locator('#skill-hub-upload').waitFor({ state: 'attached' });
      await tabs.nth(2).click();
      await tabs.nth(0).click();
      await Promise.all([
        page.waitForResponse(
          (r) =>
            r.url().includes('/skill-hub/catalog') &&
            new URL(r.url()).searchParams.get('q') === 'needle',
        ),
        main.locator('input').first().fill('needle'),
      ]);
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
        true,
      );
      if (width === 1440 || width === 390)
        await page.screenshot({
          path: path.join(out, `skill-hub-${locale}-${width}.jpg`),
        });
      results.push({ locale, width, status: 'PASS' });
      console.log('PASS', locale, width);
    }
    for (stage of ['running', 'error', 'unknown']) {
      await visit('/admin/runtime-pods');
      await page
        .locator('.runtime-slot-' + stage)
        .first()
        .waitFor();
    }
    if (locale === 'en')
      for (content of ['full', 'loading', 'error']) {
        await visit('/skill-hub');
        if (content === 'full')
          await page
            .getByRole('heading', {
              name: 'Readable long skill name for interface coverage',
            })
            .waitFor();
        if (content === 'loading')
          await page.getByText('Loading...', { exact: true }).waitFor();
        if (content === 'error')
          await page
            .getByText('Request failed with status code 500', { exact: true })
            .waitFor();
      }
    await page.close();
  }
  assert.deepEqual(errors, []);
  assert.deepEqual(writes, []);
  await fs.writeFile(
    path.join(out, 'results.json'),
    JSON.stringify({ results, javascriptErrors: errors, writes }, null, 2),
  );
  console.log(
    'PASS all states, tabs/search and 20 locale/viewport combinations; no writes',
  );
} catch (error) {
  console.error(error);
  process.exitCode = 1;
} finally {
  await server?.kill();
  vite.httpServer?.closeAllConnections();
  await Promise.race([vite.close(), new Promise((r) => setTimeout(r, 3000))]);
  process.exit(process.exitCode || 0);
}
