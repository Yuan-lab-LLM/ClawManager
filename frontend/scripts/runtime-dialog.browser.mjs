// Local component regression: no backend, credentials, or application build required.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const here = path.dirname(fileURLToPath(import.meta.url));
const requireE2E = createRequire(path.resolve(here, '../../e2e/package.json'));
const { build } = requireE2E('esbuild');
const { chromium } = requireE2E('playwright');
const resource = { cpu_request: '500m', memory_request: '512Mi', cpu_limit: '1', memory_limit: '1Gi' };
const option = { runtime_type: 'opencode', namespace: 'test', deployment_name: 'code', deployment_uid: 'code-uid', replicas: 2, slot_count: 8, image_ref: 'code@sha256:fixture', pod_resources: resource, coordinator_resources: resource, slot_resources: resource };
const fixture = { items: [option, { ...option, runtime_type: 'hermes', deployment_name: 'hermes', deployment_uid: 'hermes-uid', slot_count: 4 }], limits: { min_pods: 1, max_pods: 10 } };
const bundle = await build({
  stdin: { resolveDir: path.resolve(here, '..'), loader: 'tsx', contents: `
    import React, { useState } from 'react';
    import { createRoot } from 'react-dom/client';
    import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
    import { I18nProvider } from './src/contexts/I18nContext';
    import { RuntimeDialog } from './src/pages/admin/RuntimeManagementUI';
    import RuntimeMaintenanceDialog from './src/pages/admin/RuntimeMaintenanceDialog';
    import { runtimePoolService } from './src/services/runtimePoolService';
    runtimePoolService.listScaleOptions = async () => (${JSON.stringify(fixture)});
    runtimePoolService.listManagedPools = async () => { window.templateFetches = (window.templateFetches || 0) + 1; return { items: [] }; };
    runtimePoolService.listPods = async () => { window.inventoryFetches = (window.inventoryFetches || 0) + 1; return []; };
    runtimePoolService.createPod = async request => { window.createCalls = (window.createCalls || 0) + 1; window.createRequest = request; return { added_pods: request.pod_count, replicas: 2 + request.pod_count }; };
    function App() {
      const [kind, setKind] = useState('shared');
      const [, setTick] = useState(0);
      window.refreshParent = () => setTick(value => value + 1);
      return <><button onClick={() => setKind('maintenance')}>Open maintenance</button><button onClick={() => setKind('shared')}>Open shared</button>
        {kind === 'shared' && <RuntimeDialog title="Shared" onClose={() => setKind('')}><input aria-label="Shared input" defaultValue="Preserve" /></RuntimeDialog>}
        {kind === 'maintenance' && <RuntimeMaintenanceDialog onClose={() => setKind('')} onChanged={() => {}} onViewScheduling={() => {}} />}</>;
    }
    localStorage.setItem('clawmanager_locale', 'en');
    createRoot(document.getElementById('root')).render(<React.StrictMode><QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><I18nProvider><App /></I18nProvider></QueryClientProvider></React.StrictMode>);
  ` }, bundle: true, write: false, format: 'iife', jsx: 'automatic', define: { 'import.meta.env': '{}', 'process.env.NODE_ENV': '"development"' }, logLevel: 'silent',
});
const server = http.createServer((request, response) => {
  if (request.url === '/app.js') { response.setHeader('Content-Type', 'text/javascript'); response.end(bundle.outputFiles[0].text); return; }
  response.setHeader('Content-Type', 'text/html');
  response.end('<!doctype html><style>dialog{width:620px;max-width:94vw;max-height:90vh;padding:0;border:1px solid #ddd}dialog>div{padding:20px}dialog::backdrop{background:#0005}label{display:block}input,select{margin:3px}</style><div id="root"></div><script src="/app.js"></script>');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : { channel: 'chrome' }) });
try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`http://127.0.0.1:${server.address().port}`);
  const dialog = page.getByRole('dialog');
  await dialog.waitFor();
  // A native dialog's own border/background is inside its bounds, not its backdrop.
  await dialog.evaluate(element => { const r = element.getBoundingClientRect(); element.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX: r.x + 1, clientY: r.y + 1 })); });
  assert.equal(await dialog.count(), 1, 'clicking dialog background inside its bounds must preserve the dialog');
  const sharedBox = await dialog.boundingBox();
  await page.mouse.move(sharedBox.x + 30, sharedBox.y + 30); await page.mouse.down(); await page.mouse.move(5, 5); await page.mouse.up();
  assert.equal(await dialog.count(), 1, 'dragging from dialog to backdrop must not dismiss it');
  await page.mouse.click(5, 5);
  await dialog.waitFor({ state: 'detached' });
  await page.getByRole('button', { name: 'Open maintenance', exact: true }).click();
  await dialog.waitFor();
  const form = dialog.getByRole('form', { name: 'Add Pods', exact: true });
  const type = form.getByRole('combobox', { name: 'Runtime type', exact: true });
  const count = form.getByRole('spinbutton', { name: 'Additional Pods', exact: true });
  const submit = dialog.locator('button[type=submit][form=runtime-create-pod]');
  assert.equal(await form.locator('input,select,textarea').count(), 2, 'only runtime type and additional Pod count are editable');
  assert.equal(await count.inputValue(), '1');
  assert.equal(await submit.isDisabled(), true, 'runtime choice is required');
  assert.equal(await page.evaluate(() => window.inventoryFetches || 0), 0, 'scale flow must not request legacy inventory');
  assert.equal(await page.evaluate(() => window.templateFetches || 0), 0, 'scale flow must not request legacy templates');
  await type.selectOption('opencode');
  await count.fill('3');
  assert.match(await form.innerText(), /8 slots per Pod × 3 added Pods = 24 additional slots/);
  await type.selectOption('hermes');
  assert.match(await form.innerText(), /4 slots per Pod × 3 added Pods = 12 additional slots/);
  await type.selectOption('opencode');
  await count.fill('11');
  assert.equal(await submit.isDisabled(), true, 'count above the server maximum is blocked');
  await count.fill('3');
  await dialog.getByRole('navigation').getByRole('button', { name: /Existing managed pools/ }).click();
  await page.waitForFunction(() => window.inventoryFetches > 0);
  const fetches = await page.evaluate(() => window.inventoryFetches);
  await page.evaluate(() => window.refreshParent());
  await page.waitForFunction(previous => window.inventoryFetches > previous, fetches);
  await dialog.getByRole('navigation').getByRole('button', { name: 'Add Pods', exact: true }).click();
  assert.equal(await count.inputValue(), '3', 'section switches, parent updates, and inventory polling retain the draft');
  assert.equal(await type.inputValue(), 'opencode');
  await page.mouse.click(5, 5);
  assert.equal(await dialog.count(), 1, 'maintenance backdrop click must preserve the draft');
  assert.equal(await count.inputValue(), '3');
  await submit.click();
  await page.waitForFunction(() => !!window.createRequest);
  assert.deepEqual(await page.evaluate(() => window.createRequest), { runtime_type: 'opencode', pod_count: 3 });
  assert.equal(await page.evaluate(() => window.createCalls), 1);
  await page.keyboard.press('Escape'); await dialog.waitFor({ state: 'detached' });
  await page.getByRole('button', { name: 'Open maintenance', exact: true }).click();
  await dialog.getByRole('button', { name: 'Close', exact: true }).first().click(); await dialog.waitFor({ state: 'detached' });
  assert.deepEqual(errors, []);
  console.log('PASS native-dialog interior, drag, backdrop policy, parent/poll refresh, two scale controls, inherited slots, exact incremental payload and lazy legacy loading, Escape, explicit Close');
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
