import fs from 'node:fs';
import assert from 'node:assert/strict';
import { chromium } from '@playwright/test';

const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const context = await browser.newContext();
  const manager = await context.newPage();
  try {
    await manager.goto(input.manager + '/login', { waitUntil: 'domcontentloaded' });
  } catch (error) {
    if (!String(error.message).includes('ERR_CERT')) throw error;
    await manager.locator('#details-button').click({ timeout: 10_000 });
    await manager.locator('#proceed-link').click({ timeout: 10_000 });
  }
  await manager.locator('#username').waitFor();

  for (const [kind, url] of Object.entries(input.runtimes)) {
    const page = await context.newPage();
    try {
      const doc = await page.goto(url, { waitUntil: 'domcontentloaded' });
      assert.equal(doc?.status(), 200, kind + ' runtime document');
      const probe = await page.evaluate(async () => {
        const response = await fetch('/__clawmanager_cert_check', { cache: 'no-store', credentials: 'omit' });
        return response.status;
      });
      assert.equal(probe, 204, kind + ' certificate probe');
      const title = kind === 'opencode' ? 'opencode' : 'deepseek';
      await page.waitForFunction((marker) => {
        return document.title.toLowerCase().includes(marker) &&
          [...document.querySelectorAll('button, textarea, [contenteditable="true"]')]
            .some((node) => node.getClientRects().length > 0);
      }, title, { timeout: 30_000 });
      console.log(kind + ': native Chrome page and certificate probe verified');
    } catch {
      // Playwright errors can include the short-lived access URL.
      throw new Error(kind + ': native Chrome page or certificate probe failed');
    } finally {
      await page.close();
    }
  }
} finally {
  await browser.close();
}
