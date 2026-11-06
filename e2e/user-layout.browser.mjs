import assert from 'node:assert/strict';
import path from 'node:path';
import fs from 'node:fs/promises';
import { createRequire } from 'node:module';
import { chromium } from '@playwright/test';

const root = path.resolve(import.meta.dirname, '../frontend');
const require = createRequire(path.join(root, 'package.json'));
const { createServer } = require('vite');
process.chdir(root);
// Use the actual layout and providers; only the native service and API are fixtures.
const fixture = `
import React, { useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import UserLayout from '/src/components/UserLayout.tsx';
import { AuthProvider } from '/src/contexts/AuthContext.tsx';
import { I18nProvider } from '/src/contexts/I18nContext.tsx';
import '/src/index.css';
window.layoutCounts = { childMounts: 0, childUnmounts: 0, accessoryMounts: 0 };
function Child() {
  useEffect(() => { window.layoutCounts.childMounts++; return () => { window.layoutCounts.childUnmounts++; }; }, []);
  return React.createElement('iframe', { title: 'native instance', src: '/layout-native', style: { width: '100%', minHeight: 700 } });
}
function Accessory() {
  useEffect(() => { window.layoutCounts.accessoryMounts++; }, []);
  return React.createElement('button', { 'data-testid': 'title-action' }, 'Title action');
}
const mode = new URLSearchParams(location.search).get('mode');
createRoot(document.getElementById('root')).render(React.createElement(MemoryRouter, null,
  React.createElement(AuthProvider, null, React.createElement(I18nProvider, null,
    React.createElement(UserLayout, { title: 'Workspace title', titleAccessory: React.createElement(Accessory), fillHeight: mode !== 'normal', scrollableMain: mode === 'scroll' }, React.createElement(Child))))));
`;
const server = await createServer({ root, server: { port: 0 }, plugins: [{
  name: 'user-layout-regression-fixture',
  resolveId(id) { if (id === '/layout-fixture.js') return '\0layout-fixture'; },
  load(id) { if (id === '\0layout-fixture') return fixture; },
  configureServer(dev) {
    dev.middlewares.use(async (req, res, next) => {
      if (req.url?.split('?')[0] !== '/layout-fixture') return next();
      res.setHeader('Content-Type', 'text/html');
      res.end(await dev.transformIndexHtml('/layout-fixture', '<html><body><div id="root"></div><script type="module" src="/layout-fixture.js"></script></body></html>'));
    });
  },
}] });
let browser;
try {
  await server.listen();
  const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
  browser = await chromium.launch({ channel: 'chrome', headless: true });
  for (const mode of ['normal', 'fill', 'scroll']) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    await context.route('**/api/**', route => route.fulfill({ json: { data: {} } }));
    let nativeStarts = 0;
    await context.route('**/layout-native', route => route.fulfill({ contentType: 'text/html', body: '<textarea aria-label="Prompt"></textarea><script>fetch("/layout-startup")</script>' }));
    await context.route('**/layout-startup', route => { nativeStarts++; return route.fulfill({ json: { healthy: true } }); });
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
    await page.goto(`${origin}/layout-fixture?mode=${mode}`);
    await page.waitForFunction(() => window.layoutCounts?.childMounts > 0).catch(error => { console.error(errors); throw error; });
    assert.equal(await page.locator('iframe').count(), 1, 'Only one native instance may load');
    assert.equal(await page.locator('main').count(), 1);
    assert.equal(await page.getByTestId('title-action').count(), 1);
    const frameElement = await page.locator('iframe').elementHandle();
    const prompt = page.frameLocator('iframe').getByRole('textbox', { name: 'Prompt' });
    await prompt.fill('Unsent draft survives responsive layout');
    for (const width of [390, 767, 768, 1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      assert(await frameElement.evaluate(el => el === document.querySelector('iframe')), 'Resizing must retain the exact iframe DOM node');
      assert.equal(await prompt.inputValue(), 'Unsent draft survives responsive layout');
      assert.equal(await page.locator('.app-topbar').isVisible(), width < 768);
      assert.equal(await page.locator('aside').isVisible(), width >= 768);
      const layout = await page.locator('main').evaluate(el => ({ overflow: getComputedStyle(el).overflowY, display: getComputedStyle(el).display, bodyHeight: document.documentElement.scrollHeight, viewport: innerHeight, horizontalOverflow: document.documentElement.scrollWidth - innerWidth }));
      assert(layout.horizontalOverflow <= 1, `No horizontal overflow at ${width}px`);
      if (process.env.USER_LAYOUT_ARTIFACTS && mode === 'fill' && [390, 1440].includes(width)) {
        await fs.mkdir(process.env.USER_LAYOUT_ARTIFACTS, { recursive: true });
        await page.screenshot({ path: path.join(process.env.USER_LAYOUT_ARTIFACTS, `user-layout-${width}.png`), fullPage: true });
      }
      if (width < 768) {
        assert.equal(layout.overflow, 'visible', 'Mobile pages retain natural document scrolling');
        assert(layout.bodyHeight > layout.viewport);
      } else if (mode !== 'normal') {
        assert.equal(layout.display, 'flex');
        assert.equal(layout.overflow, mode === 'scroll' ? 'auto' : 'hidden');
      }
    }
    assert.deepEqual(await page.evaluate(() => window.layoutCounts), { childMounts: 1, childUnmounts: 0, accessoryMounts: 1 });
    assert.equal(nativeStarts, 1, 'One native startup request batch across all breakpoints');
    assert.deepEqual(errors, []);
    console.log(`PASS UserLayout ${mode}: one mount, one native startup, stable iframe/draft at 390/767/768/1440px`);
    await context.close();
  }
} finally { await browser?.close(); await server.close(); }
