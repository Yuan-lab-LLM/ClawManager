import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { createServer } from '../node_modules/vite/dist/node/index.js';

const require = createRequire(process.env.WORKSPACE_E2E_PACKAGE || '/root/cm-uiux08-20261111/source-e737ca5040e637f271ea5f394ca35aeac985ab98/e2e/package.json');
const { chromium } = require('playwright');
const root = path.resolve(import.meta.dirname, '..');
const out = process.env.INSTANCE_CREATE_SCREENSHOTS || '/tmp/cm-instance-create-browser';
await fs.mkdir(out, { recursive: true });

const types = ['openclaw', 'hermes', 'opencode', 'deepseek-harness'];
const typeNames = ['OpenClaw', 'Hermes', 'OpenCode', 'DeepSeek Harness'];
const images = types.flatMap((type, index) => ['gateway', 'desktop'].map((runtime, mode) => ({
  id: index * 2 + mode + 1, instance_type: type, runtime_type: runtime,
  display_name: `${type} ${runtime}`, image: `fixture/${type}:${runtime}`, is_enabled: true,
})));
const skill = (id, name, extra = {}) => ({ id, user_id: 71, skill_key: `fixture-${id}`, name,
  description: 'Fixture library skill', status: 'active', source_type: 'upload', risk_level: 'low',
  instance_count: 0, created_at: '2026-11-12T00:00:00Z', updated_at: '2026-11-12T00:00:00Z', ...extra });
const instance = (id, name, extra = {}) => ({ id, name, user_id: 71, type: 'openclaw',
  runtime_type: 'gateway', instance_mode: 'lite', status: 'running', cpu_cores: 2,
  memory_gb: 4, disk_gb: 10, gpu_enabled: false, gpu_count: 0, os_type: 'openclaw',
  os_version: 'latest', storage_class: '', mount_path: '/workspace',
  created_at: '2026-11-12T00:00:00Z', updated_at: '2026-11-12T00:00:00Z', ...extra });

let vite, browser, page, holdCreate = false, releaseCreate;
let skillsState = 'normal', settingsState = 'normal';
let singleSetupFailure = false, batchRecovery = false, malformedBatch = false, runtimeRunning = false;
let planFixtures = false, releaseCompile, batchAllSuccess = false;
const errors = [], writes = [], reads = [], singlePayloads = [], batchPayloads = [], imports = [], compiles = [];
try {
  vite = await createServer({ root, server: { port: 0, host: '127.0.0.1', hmr: false }, logLevel: 'error' });
  await vite.listen();
  browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH || '/opt/google/chrome/chrome', args: ['--no-sandbox'] });
  page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  page.setDefaultTimeout(10000);
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    window.creationNavigation = [];
    for (const method of ['pushState', 'replaceState']) {
      const original = history[method];
      history[method] = function (...args) {
        window.creationNavigation.push(new URL(args[2], location.href).pathname);
        return original.apply(this, args);
      };
    }
    localStorage.setItem('access_token', 'fixture');
    if (!localStorage.getItem('clawmanager_locale')) localStorage.setItem('clawmanager_locale', 'en');
  });
  await page.routeWebSocket('**/*', socket => socket.close());
  // Every API call is intercepted; no test mutation can reach a real backend.
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url()), pathname = url.pathname;
    const respond = (data, status = 200) => route.fulfill({ status, json: { success: true, data } });
    if (request.method() === 'POST' && pathname === '/api/v1/instances') {
      const payload = request.postDataJSON(); singlePayloads.push(payload);
      if (holdCreate) await new Promise(resolve => { releaseCreate = resolve; });
      if (singleSetupFailure) return route.fulfill({ status: 403, headers: { 'X-ClawManager-Created-Instance': '901', 'Access-Control-Expose-Headers': 'X-ClawManager-Created-Instance' }, json: { error: 'Fixture post-create skill setup failed' } });
      return respond(instance(901, payload.name, { ...payload, status: 'creating' }));
    }
    if (request.method() === 'POST' && pathname === '/api/v1/instances/batch/lite') {
      const payload = request.postDataJSON(); batchPayloads.push(payload);
      if (holdCreate) await new Promise(resolve => { releaseCreate = resolve; });
      const results = Array.from({ length: payload.count }, (_, offset) => {
        const name = `${payload.name_prefix}-${String((payload.start_index || 1) + offset).padStart(3, '0')}`;
        if (batchRecovery && offset === 1) return { name, status: 'failed', instance: instance(911, name), error: 'Fixture existing instance setup failed' };
        if (batchRecovery && offset === 2) return { name, status: 'failed', error: 'Fixture capacity unavailable' };
        return !batchAllSuccess && !batchRecovery && offset === 1 ? { name, status: 'failed', error: 'Fixture capacity unavailable' }
          : { name, status: 'created', instance: instance(910 + offset, name) };
      });
      return respond({ requested: results.length, created: results.filter(row => row.status === 'created').length, failed: results.filter(row => row.status === 'failed').length, results: malformedBatch ? results.slice(0, -1) : results }, 207);
    }
    if (request.method() === 'POST' && pathname === '/api/v1/instances/901/openclaw/import') {
      imports.push({ pathname, body: request.postData(), contentType: request.headers()['content-type'] });
      if (imports.length === 1) return route.fulfill({ status: 503, json: { error: 'Fixture archive upload unavailable' } });
      return respond(null);
    }
    if (request.method() === 'POST' && pathname === '/api/v1/openclaw-configs/compile-preview') {
      compiles.push(request.postDataJSON());
      await new Promise(resolve => { releaseCompile = resolve; });
      return respond({ mode: 'manual', selected_resources: [], resolved_resources: [], auto_included: [], warnings: [], env_names: [], payload_sizes: {}, total_payload_bytes: 0, manifest: {} });
    }
    if (request.method() === 'POST' && /^\/api\/v1\/instances\/\d+\/access$/.test(pathname)) return respond({ token: 'fixture', access_url: '/api/v1/create-fixture-frame', proxy_url: '/api/v1/create-fixture-frame', expires_at: '2099-01-01T00:00:00Z' });
    if (pathname === '/api/v1/create-fixture-frame') return route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>Created fixture</title>' });
    if (request.method() !== 'GET') {
      writes.push(`${request.method()} ${pathname}`);
      return route.fulfill({ status: 400, json: { error: 'Unexpected fixture mutation' } });
    }
    reads.push(pathname);
    if (pathname === '/api/v1/auth/me') return respond({ id: 71, username: 'Create fixture', role: 'admin', is_active: true });
    if (pathname === '/api/v1/system-settings/images') {
      if (settingsState === 'error') return route.fulfill({ status: 503, json: { error: 'Fixture images unavailable' } });
      return respond({ items: settingsState === 'empty' ? [] : settingsState === 'hermes' ? images.filter(item => item.instance_type === 'hermes') : settingsState === 'pro' ? images.filter(item => item.runtime_type === 'desktop') : settingsState === 'custom-gateway' ? [{ id: 90, instance_type: 'custom', runtime_type: 'gateway', display_name: 'Invalid Custom Lite', image: 'fixture/custom:gateway', is_enabled: true }] : [...images,
        { id: 99, instance_type: 'custom', runtime_type: 'desktop', display_name: 'Disabled image', image: 'fixture/disabled:latest', is_enabled: false }] });
    }
    if (pathname === '/api/v1/skill-hub/attachable') {
      if (skillsState === 'error') return route.fulfill({ status: 503, json: { error: 'Fixture skills unavailable' } });
      return respond(skillsState === 'empty' ? [] : [skill(501, 'Fixture safe skill'),
        skill(502, 'Fixture second skill'), skill(503, 'Fixture high risk', { risk_level: 'high' }),
        skill(504, 'Fixture inactive skill', { status: 'inactive' })]);
    }
    if (pathname === '/api/v1/users/71/quota') return respond({ id: 1, user_id: 71, max_instances: 200,
      max_cpu_cores: 128, max_memory_gb: 512, max_storage_gb: 2048, max_gpu_count: 4 });
    if (pathname === '/api/v1/instances') return respond({ instances: [], total: 0, page: 1, limit: 1000 });
    if (/^\/api\/v1\/instances\/\d+$/.test(pathname)) return respond({ instance: instance(Number(pathname.split('/').at(-1)), 'Created fixture', { status: runtimeRunning ? 'running' : 'creating' }) });
    if (pathname.endsWith('/status')) return respond({ instance_status: { instance_id: 901, status: 'creating', availability: 'unavailable' } });
    if (pathname.endsWith('/runtime')) return respond({ commands: [], skills: [] });
    if (pathname.endsWith('/external-access')) return respond({ external_access: null });
    if (pathname.endsWith('/workbench/capabilities')) return respond({ generation: 1, git: true, terminal: true, preview: true, processes: false });
    if (pathname.endsWith('/workspace/files')) return respond({ entries: [] });
    if (pathname.endsWith('/environment-overrides')) return respond({ names: [] });
    if (pathname === '/api/v1/openclaw-configs/resources') return respond(planFixtures ? [{ id: 601, user_id: 71, resource_type: 'channel', resource_key: 'fixture-channel', name: 'Fixture compile resource', enabled: true, version: 1, tags: [], content: {} }] : []);
    if (pathname === '/api/v1/version') return respond({ version: 'fixture', commit: 'fixture', build_time: 'fixture' });
    return respond([]);
  });
  const url = vite.resolvedUrls.local[0] + 'instances/new';
  const batchSwitch = () => page.getByRole('switch', { name: /Batch create|批量创建/i });
  const summary = () => page.getByRole('complementary', { name: /Creation preview|创建预览/ });
  const env = () => page.getByLabel(/Environment Variables|环境变量/i, { exact: true });
  const create = () => page.locator('form button[type="submit"]');
  const ready = async (query = '') => {
    const settingsReady = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system-settings/images');
    await page.goto(url + query); await settingsReady; await page.locator('form').waitFor();
  };
  const advanced = async () => {
    if (!await env().isVisible()) await page.getByText(/^(Advanced configuration|高级配置)$/i).click();
    await env().waitFor();
  };
  const chooseType = name => page.getByRole('radio', { name, exact: true }).click();
  const noOverflow = async label => assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, label);
  const results = () => page.getByRole('region', { name: 'Creation results', exact: true });
  const resultRow = name => results().getByRole('listitem').filter({ has: page.getByText(name, { exact: true }) });

  await ready();
  // The old wizard fails here before any create request is made.
  await batchSwitch().waitFor();
  assert.match(await create().innerText(), /^Create Instance$/i, 'idle submit has an accessible action label');
  assert.equal(await batchSwitch().getAttribute('aria-checked'), 'false');
  await batchSwitch().focus(); await page.keyboard.press('Space');
  assert.equal(await batchSwitch().getAttribute('aria-checked'), 'true', 'batch switch works from keyboard');
  await page.keyboard.press('Space');
  assert.equal(await batchSwitch().getAttribute('aria-checked'), 'false');
  for (const name of typeNames) assert.equal(await page.getByRole('radio', { name, exact: true }).isVisible(), true);
  assert.equal(await page.getByRole('radio', { name: /custom|workbuddy|codex|claude/i }).count(), 0);
  assert.equal(await page.getByRole('button', { name: /^(Next|下一步)$/ }).count(), 0, 'all fields belong to one form');
  const owner = page.getByLabel(/Owner|负责人/i);
  assert.equal(await owner.evaluate(input => input.required), false, 'owner is optional');
  assert.equal(await page.getByRole('spinbutton', { name: /CPU|Memory|Storage|内存|存储/ }).count(), 0, 'Lite hides resource controls');
  assert.equal(await page.getByRole('heading', { name: /Initial configuration|初始配置/ }).count(), 0, 'Lite hides initial configuration');
  assert.equal(await page.getByRole('combobox', { name: /Image|镜像/i }).count(), 0, 'default enabled image is automatic');
  await page.getByLabel(/Instance Name|实例名称/i).fill('Fixture single');
  await advanced();
  await env().fill('# comment\nPLAIN=left=right\n\nAPI_TOKEN=secret-value\nEMPTY=');
  const envTable = summary().getByRole('table', { name: /Environment preview|环境变量预览/ });
  await envTable.getByRole('row', { name: /PLAIN left=right/ }).waitFor();
  assert.equal((await envTable.innerText()).includes('secret-value'), false, 'summary masks sensitive values');
  assert.equal((await envTable.innerHTML()).includes('secret-value'), false, 'masked preview also excludes secret tooltip and accessible text');
  assert.match(await envTable.getByRole('row', { name: /API_TOKEN/ }).innerText(), /[•●*]{3,}/);
  await env().fill('DUPLICATE=one\nDUPLICATE=two');
  assert.equal(await create().isDisabled(), true, 'duplicate environment names block submission');
  assert.equal(await env().getAttribute('aria-invalid'), 'true');
  await env().fill('INVALID-NAME=value');
  assert.equal(await create().isDisabled(), true, 'invalid environment names block submission');
  await env().fill('# comment\nPLAIN=left=right\nAPI_TOKEN=secret-value\nEMPTY=');
  const safeSkill = page.getByRole('checkbox', { name: /Fixture safe skill/ });
  await safeSkill.check();
  await summary().getByText('Fixture safe skill', { exact: true }).waitFor();
  const skillSearch = page.locator('form').getByRole('searchbox');
  await skillSearch.fill('second');
  assert.equal(await safeSkill.count(), 0, 'skill search filters the real inventory');
  assert.equal(await summary().getByText('Fixture safe skill', { exact: true }).isVisible(), true, 'filtering preserves the selected summary');
  await skillSearch.fill('');
  assert.equal(await page.getByRole('checkbox', { name: /Fixture high risk|Fixture inactive skill/ }).count(), 0);
  await safeSkill.uncheck();
  assert.equal(await summary().getByText('Fixture safe skill', { exact: true }).count(), 0);
  await safeSkill.check();

  const importButton = page.getByRole('button', { name: /^(Import configuration|导入配置)$/i });
  assert.equal(await importButton.isEnabled(), true);
  const importBounds = await importButton.boundingBox(), envBounds = await env().boundingBox();
  assert.ok(importBounds.y < envBounds.y, 'configuration import precedes advanced environment fields');
  const chooserEvent = page.waitForEvent('filechooser'); await importButton.click();
  const chooser = await chooserEvent;
  await chooser.setFiles({ name: 'fixture-config.tar.gz', mimeType: 'application/gzip', buffer: Buffer.from('fixture archive') });
  await page.getByText('fixture-config.tar.gz', { exact: true }).waitFor();
  await page.getByRole('button', { name: /Remove (?:configuration|file)|移除(?:配置|文件)/i }).click();
  assert.equal(await page.getByText('fixture-config.tar.gz', { exact: true }).count(), 0);
  await chooseType('Hermes'); assert.equal(await importButton.isEnabled(), true);
  await chooseType('OpenCode'); assert.equal(await importButton.isDisabled(), true);
  await chooseType('DeepSeek Harness'); assert.equal(await importButton.isDisabled(), true);
  await chooseType('OpenClaw'); await safeSkill.check();

  holdCreate = true; await create().click();
  while (!releaseCreate) await page.waitForTimeout(10);
  // The held request allows checking the in-flight guard without racing navigation.
  assert.equal(await create().isDisabled(), true);
  assert.equal(singlePayloads.length, 1);
  assert.equal(Object.hasOwn(singlePayloads[0], 'owner'), false, 'blank owner is omitted from JSON');
  assert.deepEqual(singlePayloads[0].environment_overrides, { PLAIN: 'left=right', API_TOKEN: 'secret-value', EMPTY: '' });
  assert.deepEqual(singlePayloads[0].skill_ids, [501]);
  assert.equal(singlePayloads[0].image_registry, 'fixture/openclaw:gateway');
  holdCreate = false; releaseCreate(); releaseCreate = undefined;
  await page.waitForURL('**/instances/901');
  assert.ok(await page.evaluate(() => window.creationNavigation.includes('/instances')), 'successful creation returns through My Instances');

  await ready(); await page.getByRole('radio', { name: 'Pro', exact: true }).click();
  assert.equal(await batchSwitch().isDisabled(), true, 'batch supports Lite only');
  await page.getByLabel(/Instance Name|实例名称/i).fill('Fixture Pro');
  await page.getByRole('spinbutton', { name: /CPU/i }).fill('3');
  await page.getByRole('spinbutton', { name: /Memory|内存/i }).fill('6');
  await page.getByRole('spinbutton', { name: /Storage|存储/i }).fill('50');
  await create().click(); await page.waitForURL('**/instances/901');
  assert.equal(singlePayloads.length, 2);
  for (const [key, value] of Object.entries({ cpu_cores: 3, memory_gb: 6, disk_gb: 50 })) assert.equal(singlePayloads[1][key], value, `${key} remains numeric`);
  assert.equal(singlePayloads[1].image_registry, 'fixture/openclaw:desktop');

  await ready('?batch=1');
  assert.equal(await batchSwitch().getAttribute('aria-checked'), 'true', 'legacy batch entry initializes the switch');
  await page.getByLabel(/Name prefix|名称前缀/i).fill('fixture-team');
  await page.getByLabel(/^(Count|数量)$/i).fill('3');
  await page.getByLabel(/Start index|起始序号/i).fill('7');
  await owner.fill('Shared owner');
  await summary().getByText('Shared owner', { exact: true }).waitFor();
  await page.getByLabel(/Description|描述/i).fill('Shared description');
  for (const name of ['fixture-team-007', 'fixture-team-008', 'fixture-team-009']) await summary().getByText(name, { exact: true }).waitFor();
  await page.getByLabel(/^(Count|数量)$/i).fill('1'); assert.equal(await create().isDisabled(), true);
  await page.getByLabel(/^(Count|数量)$/i).fill('101'); assert.equal(await create().isDisabled(), true);
  await page.getByLabel(/^(Count|数量)$/i).fill('3');
  await advanced(); await env().fill('SHARED=value=with=equals\nAPI_KEY=batch-secret'); await safeSkill.check();
  await create().click(); await page.getByText('Fixture capacity unavailable', { exact: true }).waitFor();
  assert.equal(batchPayloads.length, 1);
  const batch = batchPayloads[0];
  assert.deepEqual({ name_prefix: batch.name_prefix, count: batch.count, start_index: batch.start_index }, { name_prefix: 'fixture-team', count: 3, start_index: 7 });
  assert.equal(batch.template.owner, 'Shared owner'); assert.equal(batch.template.description, 'Shared description');
  assert.deepEqual(batch.template.environment_overrides, { SHARED: 'value=with=equals', API_KEY: 'batch-secret' });
  assert.deepEqual(batch.template.skill_ids, [501]);
  assert.equal(batch.template.image_registry, 'fixture/openclaw:gateway');
  for (const name of ['fixture-team-007', 'fixture-team-008', 'fixture-team-009']) assert.equal(await page.getByText(name, { exact: true }).count() > 0, true, 'per-item batch results stay visible');
  assert.equal(await create().isDisabled(), true, 'completed partial batch cannot resubmit successful instances');

  for (const locale of ['en', 'zh']) {
    await page.evaluate(value => localStorage.setItem('clawmanager_locale', value), locale);
    await ready('?batch=1'); await page.getByLabel(/Name prefix|名称前缀/i).fill('long-fixture-name-for-responsive-layout');
    await advanced(); await env().fill('LONG_VALUE=' + 'value'.repeat(80)); await safeSkill.check();
    for (const width of [2048, 1440, 1280, 768, 390, 320]) {
      await page.setViewportSize({ width, height: width >= 1280 ? 900 : 844 });
      await page.evaluate(() => { window.scrollTo(0, 0); });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      await noOverflow(`${locale} creation at ${width} has no document overflow`);
      assert.equal(await batchSwitch().isVisible(), true);
      assert.equal(await create().isVisible(), true);
      await page.screenshot({ path: path.join(out, `create-${locale}-${width}.png`), fullPage: true });
    }
  }
  for (const locale of ['de', 'ja', 'ko']) {
    await page.evaluate(value => localStorage.setItem('clawmanager_locale', value), locale); await ready('?batch=1');
    for (const width of [390, 320]) { await page.setViewportSize({ width, height: 844 }); await page.evaluate(() => { window.scrollTo(0, 0); }); await noOverflow(`${locale} at ${width}`); }
    await page.screenshot({ path: path.join(out, `create-${locale}-320.png`), fullPage: true });
  }
  await page.evaluate(() => localStorage.setItem('clawmanager_locale', 'en'));
  settingsState = 'hermes'; await ready();
  await page.getByRole('radio', { name: 'Hermes', exact: true }).waitFor();
  await page.getByRole('radio', { name: 'OpenClaw', exact: true }).waitFor({ state: 'hidden' });
  for (const name of ['OpenClaw', 'OpenCode', 'DeepSeek Harness']) assert.equal(await page.getByRole('radio', { name, exact: true }).count(), 0, 'types follow enabled image settings');
  settingsState = 'normal'; skillsState = 'empty'; await ready(); await advanced();
  assert.equal(await page.getByRole('checkbox', { name: /Fixture .* skill/ }).count(), 0, 'empty skill inventory has no sample choices');
  await page.screenshot({ path: path.join(out, 'create-empty-skills.png'), fullPage: true });
  assert.equal(singlePayloads.length, 2); assert.equal(batchPayloads.length, 1);

  await page.setViewportSize({ width: 1440, height: 900 });
  skillsState = 'normal'; singleSetupFailure = true; await ready();
  await page.getByLabel(/Instance Name/i).fill('Fixture setup recovery'); await advanced(); await safeSkill.check();
  await create().click(); await results().getByText('Fixture post-create skill setup failed', { exact: true }).waitFor();
  assert.equal(singlePayloads.length, 3);
  assert.equal(await resultRow('Fixture setup recovery').getByRole('link', { name: 'Fixture setup recovery', exact: true }).getAttribute('href'), '/instances/901');
  assert.equal(await results().getByRole('button', { name: 'Retry creation', exact: true }).count(), 0, 'persisted instance is never offered creation retry');
  assert.equal(await create().isDisabled(), true);
  assert.equal(new URL(page.url()).pathname, '/instances/new');
  await page.screenshot({ path: path.join(out, 'create-existing-setup-error.png'), fullPage: true });

  singleSetupFailure = false; batchRecovery = true; await ready('?batch=1');
  await page.getByLabel(/Name prefix/i).fill('recovery'); await page.getByLabel(/^Count$/i).fill('3');
  await page.getByLabel(/Start index/i).fill('4'); await owner.fill('Original owner');
  await page.getByLabel(/Description/i).fill('Original retry description');
  await advanced(); await env().fill('RETRY_VALUE=original=value'); await safeSkill.check();
  await create().click(); await results().getByText('Fixture existing instance setup failed', { exact: true }).waitFor();
  assert.equal(batchPayloads.length, 2);
  assert.equal(await resultRow('recovery-004').getByRole('button', { name: 'Retry creation', exact: true }).count(), 0);
  assert.equal(await resultRow('recovery-005').getByRole('link', { name: 'recovery-005', exact: true }).getAttribute('href'), '/instances/911');
  assert.equal(await resultRow('recovery-005').getByRole('button', { name: 'Retry creation', exact: true }).count(), 0);
  await resultRow('recovery-006').getByRole('button', { name: 'Retry creation', exact: true }).click();
  await resultRow('recovery-006').getByRole('link', { name: 'recovery-006', exact: true }).waitFor();
  assert.equal(singlePayloads.length, 4); assert.equal(batchPayloads.length, 2, 'retry never resubmits the batch');
  assert.equal(singlePayloads[3].name, 'recovery-006', 'only the uncreated original name is retried');
  assert.equal(singlePayloads[3].owner, 'Original owner'); assert.equal(singlePayloads[3].description, 'Original retry description');
  assert.deepEqual(singlePayloads[3].environment_overrides, { RETRY_VALUE: 'original=value' }); assert.deepEqual(singlePayloads[3].skill_ids, [501]);
  assert.equal(await create().isDisabled(), true);

  batchRecovery = false; runtimeRunning = true; await ready();
  await page.getByLabel(/Instance Name/i).fill('Fixture import recovery'); await advanced();
  const archiveChooser = page.waitForEvent('filechooser'); await importButton.click();
  await (await archiveChooser).setFiles({ name: 'retry-import.tar.gz', mimeType: 'application/gzip', buffer: Buffer.from('fixture recovery archive') });
  await create().click(); await results().getByText(/Fixture archive upload unavailable/).waitFor();
  assert.equal(singlePayloads.length, 5); assert.equal(imports.length, 1);
  assert.equal(await resultRow('Fixture import recovery').getByRole('link', { name: 'Fixture import recovery', exact: true }).getAttribute('href'), '/instances/901');
  assert.equal(await results().getByRole('button', { name: 'Retry creation', exact: true }).count(), 0);
  await results().getByRole('button', { name: 'Retry import', exact: true }).click();
  await results().getByRole('button', { name: 'Retry import', exact: true }).waitFor({ state: 'hidden' });
  assert.equal(imports.length, 2); assert.equal(singlePayloads.length, 5, 'retry import reuses the existing instance without creating again');
  assert.ok(imports.every(upload => upload.pathname === '/api/v1/instances/901/openclaw/import' && upload.contentType.includes('multipart/form-data') && upload.body.includes('retry-import.tar.gz')));
  await page.waitForURL('**/instances/901');
  assert.ok(await page.evaluate(() => window.creationNavigation.includes('/instances')), 'successful final import retry returns to My Instances');
  assert.equal(await results().count(), 0);
  await page.screenshot({ path: path.join(out, 'create-import-retry-completed.png'), fullPage: true });

  runtimeRunning = false; settingsState = 'normal'; await ready();
  await page.getByLabel(/Instance Name/i).fill('Fixture pinned settings'); await advanced();
  const pinnedChooser = page.waitForEvent('filechooser'); await importButton.click();
  await (await pinnedChooser).setFiles({ name: 'openclaw-only.tar.gz', mimeType: 'application/gzip', buffer: Buffer.from('fixture pinned archive') });
  settingsState = 'hermes';
  const refreshedImages = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system-settings/images');
  await page.clock.install(); await page.clock.fastForward(61000);
  await page.evaluate(() => window.dispatchEvent(new Event('focus'))); await refreshedImages;
  await page.getByRole('radio', { name: 'OpenClaw', exact: true }).waitFor({ state: 'hidden' });
  assert.equal(await create().isDisabled(), true, 'removed selected type cannot silently submit using another type');
  assert.equal(await summary().getByText('Hermes', { exact: true }).count(), 0, 'settings refresh preserves explicit type intent');
  assert.equal(await page.getByText('openclaw-only.tar.gz', { exact: true }).isVisible(), true);
  await chooseType('Hermes');
  assert.equal(await page.getByText('openclaw-only.tar.gz', { exact: true }).count(), 0, 'explicit type change clears the old archive');

  settingsState = 'pro'; await ready('?batch=1');
  assert.equal(await page.getByRole('radio', { name: 'Pro', exact: true }).getAttribute('aria-checked'), 'true');
  assert.equal(await batchSwitch().getAttribute('aria-checked'), 'false'); assert.equal(await batchSwitch().isDisabled(), true);
  assert.equal(await page.getByLabel(/Name prefix/i).count(), 0);
  await page.getByLabel(/Instance Name/i).fill('Fixture Pro-only'); await create().click(); await page.waitForURL('**/instances/901');
  assert.equal(singlePayloads.length, 6); assert.equal(singlePayloads[5].mode, 'pro'); assert.equal(batchPayloads.length, 2, 'desktop-only batch URL never posts a Lite batch');

  settingsState = 'custom-gateway'; await ready('?batch=1');
  assert.equal(await page.getByRole('radio', { name: /Custom/i }).count(), 0, 'custom gateway image cannot offer Custom Lite');
  assert.equal(await batchSwitch().getAttribute('aria-checked'), 'false'); assert.equal(await create().isDisabled(), true);

  settingsState = 'normal'; malformedBatch = true; await ready('?batch=1');
  await page.getByLabel(/Name prefix/i).fill('uncertain'); await page.getByLabel(/^Count$/i).fill('3');
  await create().click(); await resultRow('uncertain-001').getByRole('link', { name: 'uncertain-001', exact: true }).waitFor();
  assert.equal(await resultRow('uncertain-001').getByRole('link', { name: 'uncertain-001', exact: true }).getAttribute('href'), '/instances/910');
  assert.equal(await resultRow('uncertain-003').getByRole('button', { name: 'Retry creation', exact: true }).count(), 0, 'unknown batch outcome cannot retry a possibly created name');
  assert.equal(await create().isDisabled(), true); assert.equal(batchPayloads.length, 3); assert.equal(singlePayloads.length, 6);
  await page.screenshot({ path: path.join(out, 'create-malformed-batch.png'), fullPage: true });

  malformedBatch = false; planFixtures = true; await ready();
  await page.getByLabel(/Instance Name/i).fill('Fixture compile collapse'); await page.getByRole('radio', { name: 'Pro', exact: true }).click();
  const initialHeading = page.getByRole('heading', { name: 'Initial configuration', exact: true });
  await initialHeading.click(); await page.getByRole('button', { name: 'Manual resources', exact: true }).click();
  await page.getByRole('checkbox', { name: /Fixture compile resource/ }).check();
  while (!releaseCompile) await page.waitForTimeout(10);
  assert.equal(await create().isDisabled(), true, 'pending config compile blocks creation');
  await initialHeading.click(); releaseCompile();
  await page.waitForFunction(() => !document.querySelector('form button[type="submit"]').disabled);
  assert.equal(await create().isEnabled(), true, 'collapsing config panel preserves the pending compile result');
  assert.deepEqual(compiles, [{ mode: 'manual', resource_ids: [601] }]);
  batchAllSuccess = true; await ready('?batch=1');
  await page.getByLabel(/Name prefix/i).fill('complete'); await page.getByLabel(/^Count$/i).fill('9');
  await create().click(); await page.waitForURL('**/instances/910');
  assert.ok(await page.evaluate(() => window.creationNavigation.includes('/instances')), 'all-success batch returns through My Instances');
  assert.equal(await results().count(), 0, 'successful batch leaves no creation records below the form');
  assert.equal(await page.locator('form.cm-create').count(), 0);

  batchAllSuccess = false; await ready('?batch=1');
  await page.getByLabel(/Name prefix/i).fill('final-retry'); await page.getByLabel(/^Count$/i).fill('2');
  await create().click(); await results().getByText('Fixture capacity unavailable', { exact: true }).waitFor();
  await resultRow('final-retry-002').getByRole('button', { name: 'Retry creation', exact: true }).click();
  await page.waitForURL('**/instances/910');
  assert.ok(await page.evaluate(() => window.creationNavigation.includes('/instances')), 'final successful creation retry returns to My Instances');
  assert.equal(await results().count(), 0);

  assert.ok(reads.includes('/api/v1/system-settings/images')); assert.ok(reads.includes('/api/v1/skill-hub/attachable'));
  assert.deepEqual(writes, []); assert.deepEqual(errors, []);
  console.log('PASS unified creation: four enabled types, same-page Lite/Pro, optional owner, import chooser/capabilities, environment validation/masking, real skills, numeric Pro resources, single/batch payloads, setup/import recovery without duplicate creation, settings refresh/archive isolation, Pro-only and Custom mode boundaries, malformed batch safety, pending compile collapse, six widths and five locales; all APIs intercepted');
} catch (error) {
  if (page) { console.log('Create route:', page.url(), 'Page errors:', errors, 'Body:', (await page.locator('body').innerText()).slice(0, 5000)); await page.screenshot({ path: path.join(out, 'failure.png'), fullPage: true }); }
  throw error;
} finally { await browser?.close(); await vite?.close(); }
