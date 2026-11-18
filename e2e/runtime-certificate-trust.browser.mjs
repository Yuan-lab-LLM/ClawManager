import assert from 'node:assert/strict';
import path from 'node:path';
import { createRequire } from 'node:module';
import { chromium } from '@playwright/test';
const root = path.resolve(import.meta.dirname, '../frontend');
const require = createRequire(path.join(root, 'package.json'));
const { createServer } = require('vite');
process.chdir(root);
const fixture = `
import React from 'react';
import { createRoot } from 'react-dom/client';
import { useRuntimeCertificateTrust } from '/src/hooks/useRuntimeCertificateTrust.ts';
import { translate } from '/src/lib/i18n.ts';
function Fixture() {
  const state = useRuntimeCertificateTrust('https://opencode-fixture.invalid/?token=private-fixture-access', true);
  window.hookState = state;
  return React.createElement('main', null,
    state.frameUrl ? React.createElement('iframe', { title: 'Native runtime', src: state.frameUrl }) :
    React.createElement('section', null,
      React.createElement('p', null, translate('en', 'instances.certificateConfirmationDescription')),
      state.certificateConfirmationRequired && React.createElement('button', { onClick: state.confirmCertificate }, 'Confirm certificate')));
}
createRoot(document.getElementById('root')).render(React.createElement(Fixture));
`;
const server = await createServer({ root, server: { port: 0 }, plugins: [{
  name: 'runtime-certificate-fixture',
  resolveId(id) { if (id === '/certificate-fixture.js') return '\0certificate-fixture'; },
  load(id) { if (id === '\0certificate-fixture') return fixture; },
  configureServer(dev) {
    dev.middlewares.use(async (req, res, next) => {
      if (req.url?.split('?')[0] !== '/certificate-fixture') return next();
      res.setHeader('Content-Type', 'text/html');
      res.end(await dev.transformIndexHtml('/certificate-fixture', '<html><body><div id="root"></div><script type="module" src="/certificate-fixture.js"></script></body></html>'));
    });
  },
}] });
let browser;
try {
  await server.listen();
  const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
  browser = await chromium.launch({ channel: 'chrome', headless: true });
  for (const { event, storageBlocked = false } of [{ event: 'focus' }, { event: 'visibilitychange' }, { event: 'pageshow' }, { event: 'focus', storageBlocked: true }]) {
    const context = await browser.newContext();
    let trusted = false, probes = 0;
    await context.route('https://opencode-fixture.invalid/**', route => {
      if (new URL(route.request().url()).pathname === '/__clawmanager_cert_check') {
        probes++;
        return trusted ? route.fulfill({ status: 204, headers: { 'Access-Control-Allow-Origin': '*' } }) : route.abort('failed');
      }
      return route.fulfill({ contentType: 'text/html', body: '<p>Native runtime</p>' });
    });
    await context.addInitScript(({ storageBlocked }) => {
      if (storageBlocked) Object.defineProperty(window, 'sessionStorage', { value: { getItem() { throw Error('Storage unavailable'); }, setItem() { throw Error('Storage unavailable'); } } });
      window.openCalls = [];
      // A blocked popup must never send the current page away.
      window.open = (...args) => { window.openCalls.push(args); return null; };
    }, { storageBlocked });
    const page = await context.newPage();
    page.setDefaultTimeout(10000);
    const entry = `${origin}/certificate-fixture?event=${event}`;
    await page.goto(entry);
    await page.waitForFunction(() => window.hookState?.certificateConfirmationRequired || location.pathname === '/__clawmanager_cert_trust');
    assert.equal(page.url(), entry, 'A certificate probe failure must retain the current CM page');
    await page.getByRole('button', { name: 'Confirm certificate' }).waitFor();
    assert.deepEqual(await page.evaluate(() => window.openCalls), [], 'Probing must never open a tab automatically');
    assert.equal(await page.locator('iframe').count(), 0, 'Untrusted runtime remains gated');
    await page.getByRole('button', { name: 'Confirm certificate' }).click();
    assert.equal(page.url(), entry, 'Blocked popups must retain the current CM page');
    const calls = await page.evaluate(() => window.openCalls);
    assert.deepEqual(calls, [['https://opencode-fixture.invalid/__clawmanager_cert_trust', '_blank', 'noopener,noreferrer']]);
    assert(!JSON.stringify(calls).includes('private-fixture-access'), 'The certificate tab must not carry the runtime access token');
    assert.match(await page.locator('main').innerText(), /allow pop-ups/i, 'The current page explains how to recover from a blocked popup');
    assert.equal(await page.evaluate(() => window.hookState.certificateOrigin), 'https://opencode-fixture.invalid');
    const before = probes;
    trusted = true;
    await page.evaluate(event => {
      if (event === 'visibilitychange') document.dispatchEvent(new Event(event));
      else window.dispatchEvent(new Event(event));
    }, event);
    await page.locator('iframe').waitFor();
    assert(probes > before, `${event} must retry certificate verification`);
    assert.equal(page.url(), entry);
    assert.equal(await page.evaluate(() => window.hookState.certificateConfirmationRequired), false);
    if (!storageBlocked) assert.equal(await page.evaluate(() => sessionStorage.getItem('clawmanager.runtime-certificate-trust.pending.v1')), null);
    assert.equal((await page.evaluate(() => window.openCalls)).length, 1);
    console.log(`PASS certificate trust: no automatic navigation, explicit token-free secure tab, popup hint, ${event} recovery${storageBlocked ? ' without storage' : ''}`);
    await context.close();
  }
} finally { await browser?.close(); await server.close(); }
