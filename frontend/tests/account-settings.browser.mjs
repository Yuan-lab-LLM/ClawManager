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
  process.env.ACCOUNT_SETTINGS_SCREENSHOTS || '/tmp/cm-account-settings';
await fs.mkdir(out, { recursive: true });
let vite,
  server,
  role = 'user';
const errors = [],
  writes = [];
const version = 'v2026.11.13-coordinator-finalization';
try {
  vite = await createServer({
    root: path.resolve(import.meta.dirname, '..'),
    server: { port: 0, host: '127.0.0.1', hmr: false },
    logLevel: 'error',
  });
  await vite.listen();
  server = await chromium.launchServer({
    headless: true,
    ...(process.env.CHROME_PATH
      ? { executablePath: process.env.CHROME_PATH }
      : {}),
    args: ['--no-sandbox'],
  });
  const browser = await chromium.connect(server.wsEndpoint());
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
    reducedMotion: 'reduce',
  });
  page.setDefaultTimeout(20000);
  page.on('pageerror', (error) => {
    if (errors.length < 10) errors.push(error.message);
  });
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'fixture');
    if (!localStorage.getItem('clawmanager_locale'))
      localStorage.setItem('clawmanager_locale', 'en');
  });
  await page.routeWebSocket('**/api/**', (socket) => socket.close());
  await page.route('**/api/**', (route) => {
    const request = route.request(),
      p = new URL(request.url()).pathname;
    if (request.method() !== 'GET') {
      writes.push(p);
      return route.fulfill({
        status: 400,
        json: { error: 'Unexpected write in navigation test' },
      });
    }
    const data = p.endsWith('/auth/me')
      ? { id: 701, username: 'account-fixture', role, is_active: true }
      : p.endsWith('/version')
        ? {
            version,
            commit: 'fixture-commit',
            build_time: '2026-11-13T09:12:22Z',
          }
        : p.endsWith('/instances')
          ? { instances: [], total: 0 }
          : p.endsWith('/admin/runtime-pods')
            ? {
                pods: [],
                summary: {
                  pods: 0,
                  online: 0,
                  capacity: 0,
                  ready: 0,
                  available: 0,
                  bound_instances: 0,
                },
                pagination: { total: 0, total_pages: 1 },
                nodes: [],
              }
            : [];
    return route.fulfill({ json: { success: true, data } });
  });
  const visit = async (route = '/settings') => {
    await page.goto(vite.resolvedUrls.local[0] + route.replace(/^\//, ''), {
      waitUntil: 'domcontentloaded',
      timeout: 60000,
    });
    await page
      .locator('.cm-account-name')
      .getByText('account-fixture', { exact: true })
      .waitFor({ state: 'attached' });
  };
  const account = page.locator('.cm-account'),
    trigger = account.locator('summary'),
    settings = account.locator('a[href="/settings"]');
  const checkBrandVersion = async (scope) => {
    const brand = scope.locator('.cm-shell-brand'),
      name = brand.locator('.cm-shell-brand-name'),
      badge = brand.getByTestId('clawmanager-version');
    await badge.getByText(version, { exact: true }).waitFor();
    assert.match(await badge.getAttribute('title'), /fixture-commit/);
    const b = await brand.boundingBox(),
      n = await name.boundingBox(),
      v = await badge.boundingBox();
    assert.ok(v.y >= n.y + n.height - 1, 'version is below the brand name');
    assert.ok(
      v.y + v.height <= b.y + b.height + 1,
      'version stays inside the existing brand header',
    );
    assert.ok(
      Math.abs(v.x - n.x) < 1,
      'version and name share the same left edge',
    );
  };

  await visit();
  await checkBrandVersion(page.locator('aside'));
  assert.equal(
    await page.locator('.cm-shell-navigation a[href="/settings"]').count(),
    0,
    'personal settings must leave desktop and mobile navigation',
  );
  for (role of ['user', 'admin'])
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await visit();
      assert.equal(
        await page.locator('.cm-shell-navigation a[href="/settings"]').count(),
        0,
      );
      assert.equal(
        await page.locator('.cm-sidebar-inner > .cm-shell-version').count(),
        0,
        'no footer version remains',
      );
      if (width >= 768) await checkBrandVersion(page.locator('aside'));
      await trigger.focus();
      await page.keyboard.press('Enter');
      await settings.waitFor();
      assert.equal(await settings.innerText(), 'Settings');
      const bounds = await account.locator('.cm-account-popup').boundingBox();
      assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= width);
      await page.screenshot({
        path: path.join(out, `${role}-en-${width}.jpg`),
        animations: 'disabled',
      });
      await page.keyboard.press('Escape');
      assert.equal(
        await trigger.evaluate((element) => element === document.activeElement),
        true,
      );
      assert.equal(await account.evaluate((element) => element.open), false);
      if (width === 390) {
        await page.locator('.cm-mobile-nav-trigger').click();
        assert.equal(
          await page.locator('dialog[open] a[href="/settings"]').count(),
          0,
        );
        await checkBrandVersion(page.locator('dialog[open]'));
        await page.screenshot({
          path: path.join(out, `${role}-drawer-${width}.jpg`),
          animations: 'disabled',
        });
        await page.keyboard.press('Escape');
      }
    }
  role = 'user';
  await page.setViewportSize({ width: 1440, height: 900 });
  await visit('/instances');
  await page.locator('.cm-sidebar-edge').click();
  assert.equal(
    await page.locator('aside').getByTestId('clawmanager-version').isVisible(),
    false,
  );
  assert.equal(
    await page.locator('aside .cm-shell-brand img').isVisible(),
    true,
  );
  await page.locator('.cm-sidebar-edge').click();
  await checkBrandVersion(page.locator('aside'));
  await trigger.click();
  await settings.focus();
  await page.keyboard.press('Enter');
  await page
    .getByRole('button', { name: 'Update Password', exact: true })
    .waitFor();
  assert.ok(page.url().endsWith('/settings'));
  assert.equal(await account.evaluate((element) => element.open), false);
  await page
    .getByRole('button', { name: 'Update Password', exact: true })
    .click();
  await page.locator('input[type="password"]').first().waitFor();
  assert.equal(await page.locator('input[type="password"]').count(), 3);
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal(await page.locator('input[type="password"]').count(), 0);
  await trigger.click();
  const language = account.locator('.cm-shell-language button');
  await language.click();
  await page.getByRole('listbox').waitFor();
  await page.keyboard.press('Escape');
  assert.equal(
    await language.evaluate((element) => element === document.activeElement),
    true,
  );
  assert.equal(await account.evaluate((element) => element.open), true);
  await page.keyboard.press('Escape');
  assert.equal(
    await trigger.evaluate((element) => element === document.activeElement),
    true,
  );
  role = 'admin';
  await visit('/admin/runtime-pods');
  await checkBrandVersion(page.locator('aside'));
  await trigger.click();
  await settings.waitFor();
  assert.equal(
    await page.locator('aside a[href="/admin/settings"]').count(),
    1,
  );
  await settings.click();
  await page
    .getByRole('button', { name: 'Update Password', exact: true })
    .waitFor();
  for (const locale of ['zh', 'ja', 'ko', 'de']) {
    await page.evaluate(
      (value) => localStorage.setItem('clawmanager_locale', value),
      locale,
    );
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await visit();
      await trigger.click();
      await settings.waitFor();
      assert.equal(
        await settings.innerText(),
        await page
          .locator('.cm-current-page [aria-current="page"]')
          .innerText(),
      );
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        true,
        `${locale} ${width} has no page overflow`,
      );
      await page.screenshot({
        path: path.join(out, `admin-${locale}-${width}.jpg`),
        animations: 'disabled',
      });
    }
  }
  assert.deepEqual(writes, []);
  assert.deepEqual(errors, []);
  console.log(
    'PASS brand version placement, mobile drawer/collapsed sidebar, account Settings entry, desktop/mobile navigation, users/admins, keyboard and Escape, language focus, password dialog; 5 languages / 4 widths; no writes',
  );
} finally {
  await server?.kill();
  vite?.httpServer?.closeAllConnections();
  await vite?.close();
}
