// Login and start only restored samples on the identified trial deployment.
const help = `Verify one restored ordinary user's exact instance inventory and start selected samples.
Set BACKUP_TRIAL_ORIGIN, BACKUP_TRIAL_ISOLATED_TARGET=confirmed, BACKUP_TRIAL_BACKUP_ID,
BACKUP_TRIAL_NAMESPACE, BACKUP_TRIAL_USER, BACKUP_TRIAL_PASSWORD, and BACKUP_TRIAL_EXPECTED_FILE.
Expected JSON keys: user_id, instances, forbidden_ids, and start_ids.
Each instances entry has id, type, runtime_type, instance_mode and optional runtime_variant.
Optional hermes_history has instance_id, session_id, title and an ordered messages array
with role and content. Optional forbidden_hermes_history has instance_id and session_id.
Optional workspace_files entries have instance_id, path, size and sha256.
Optional forbidden_workspace_file has instance_id and path.
Set start_ids to [] for a login and visibility check with no mutations.
Optional BACKUP_TRIAL_TIMEOUT_MS, PLAYWRIGHT_CHANNEL and PLAYWRIGHT_EXECUTABLE_PATH.
No automatic start retries. Runtime status is an application observation. Verify the Ready Pod and
the current gateway binding separately on the isolated cluster.`;
if (process.argv.includes('--help')) {
  console.log(help);
  process.exit(0);
}

import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';

const id = value => Number.isSafeInteger(value) && value > 0;
const code = value => typeof value === 'string' && /^[a-z][a-z0-9_-]{0,31}$/.test(value);
const sessionID = value => typeof value === 'string' && /^[A-Za-z0-9_.-]{1,128}$/.test(value);
const workspacePath = value => typeof value === 'string' && value.length <= 1024 &&
  !/[\\:\x00-\x1f]/.test(value) && value.split('/').every(part => part && part !== '.' && part !== '..');
const workspaceDownload = file => `/instances/${file.instance_id}/workspace/download?${new URLSearchParams({ path: file.path })}`;
const receipt = { outcome: 'preflight', samples: [] };
let browser;
let step = 'preflight';

function requireThat(condition) {
  if (!condition) throw new Error('Trial runtime assertion failed');
}

function validateInventory(input) {
  requireThat(id(input.user_id) && Array.isArray(input.instances) &&
    Array.isArray(input.forbidden_ids) && Array.isArray(input.start_ids));
  const owned = new Set();
  for (const row of input.instances) {
    requireThat(id(row.id) && !owned.has(row.id) && code(row.type) && code(row.runtime_type) &&
      code(row.instance_mode) && (!row.runtime_variant || code(row.runtime_variant)));
    owned.add(row.id);
  }
  const forbidden = new Set();
  for (const value of input.forbidden_ids) {
    requireThat(id(value) && !owned.has(value) && !forbidden.has(value));
    forbidden.add(value);
  }
  const samples = new Set();
  for (const value of input.start_ids) {
    requireThat(owned.has(value) && !samples.has(value));
    samples.add(value);
  }
  if (input.hermes_history) {
    const history = input.hermes_history;
    const instance = input.instances.find(row => row.id === history.instance_id);
    requireThat(samples.has(history.instance_id) && instance?.type === 'hermes' && instance.instance_mode === 'lite' &&
      sessionID(history.session_id) && typeof history.title === 'string' && history.title.trim().length > 0 &&
      Array.isArray(history.messages) && history.messages.length > 0 && history.messages.length <= 500 &&
      history.messages.every(message => code(message.role) && typeof message.content === 'string' && message.content.trim().length > 0));
  }
  if (input.forbidden_hermes_history) {
    const history = input.forbidden_hermes_history;
    requireThat(forbidden.has(history.instance_id) && sessionID(history.session_id));
  }
  if (input.workspace_files !== undefined) {
    requireThat(Array.isArray(input.workspace_files) && input.workspace_files.length <= 20 &&
      input.workspace_files.every(file => samples.has(file.instance_id) && workspacePath(file.path) &&
        Number.isSafeInteger(file.size) && file.size >= 0 && file.size <= 32 * 1024 * 1024 &&
        /^[0-9a-f]{64}$/.test(file.sha256)));
  }
  if (input.forbidden_workspace_file) {
    const file = input.forbidden_workspace_file;
    requireThat(forbidden.has(file.instance_id) && workspacePath(file.path));
  }
}

function checkInstance(actual, expected, userID) {
  requireThat(actual?.id === expected.id && actual.user_id === userID &&
    actual.type === expected.type && actual.runtime_type === expected.runtime_type &&
    actual.instance_mode === expected.instance_mode &&
    (actual.runtime_variant || '') === (expected.runtime_variant || '') &&
    actual.status === 'stopped');
}

try {
  const origin = new URL(process.env.BACKUP_TRIAL_ORIGIN);
  const namespace = process.env.BACKUP_TRIAL_NAMESPACE;
  const backupID = process.env.BACKUP_TRIAL_BACKUP_ID;
  const user = process.env.BACKUP_TRIAL_USER;
  const password = process.env.BACKUP_TRIAL_PASSWORD;
  const timeout = Number(process.env.BACKUP_TRIAL_TIMEOUT_MS || 120000);
  requireThat(origin.href === origin.origin + '/' &&
    (origin.protocol === 'https:' || (origin.protocol === 'http:' &&
      ['127.0.0.1', '[::1]', 'localhost'].includes(origin.hostname))));
  requireThat(process.env.BACKUP_TRIAL_ISOLATED_TARGET === 'confirmed' &&
    /^[a-f0-9]{32}$/.test(backupID) && /^cm-drill-[a-z0-9][a-z0-9-]*$/.test(namespace) &&
    user && password && Number.isSafeInteger(timeout) && timeout >= 1000 && timeout <= 600000);
  const expected = JSON.parse(await readFile(process.env.BACKUP_TRIAL_EXPECTED_FILE, 'utf8'));
  validateInventory(expected);

  step = 'launch_browser';
  const { chromium } = await import('playwright-core');
  const executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
  browser = await chromium.launch({
    headless: true,
    channel: executablePath ? undefined : process.env.PLAYWRIGHT_CHANNEL || 'chrome',
    executablePath,
  });
  const context = await browser.newContext({ serviceWorkers: 'block' });
  const page = await context.newPage();
  const url = path => new URL(path, origin).href;
  const arrived = path => page.url() === url(path);
  const observe = (response, method, path) => response.url() === url(path) && response.request().method() === method;

  step = 'login';
  const loginFlow = '/login';
  await page.goto(url(loginFlow), { timeout });
  requireThat(arrived(loginFlow));
  await page.locator('#username').fill(user, { timeout });
  await page.locator('#password').fill(password, { timeout });
  const [login] = await Promise.all([
    page.waitForResponse(response => observe(response, 'POST', '/api/v1/auth/login'), { timeout }),
    page.locator('button[type=submit]').click({ timeout }),
  ]);
  requireThat(login.ok());
  const token = (await login.json())?.data?.access_token;
  requireThat(typeof token === 'string' && token.length > 0);

  async function get(path, wait = Math.min(timeout, 15000)) {
    return context.request.get(url('/api/v1' + path), {
      headers: { Authorization: 'Bearer ' + token }, maxRedirects: 0, timeout: wait,
    });
  }

  async function read(path, wait) {
    const response = await get(path, wait);
    requireThat(response.ok());
    const body = await response.json();
    requireThat(body.success === true);
    return body.data;
  }

  async function verifyTarget() {
    const response = await get('/trial-runtime');
    requireThat(response.ok());
    const runtime = await response.json();
    requireThat(runtime.backup_id === backupID && runtime.namespace === namespace);
  }

  step = 'trial_identity';
  await verifyTarget();
  receipt.backup_id = backupID;
  step = 'owner_visibility';
  const who = await read('/auth/me');
  requireThat(who.id === expected.user_id && who.role === 'user');
  const actual = new Map();
  const pageLimit = 100;
  const pages = Math.max(1, Math.ceil(expected.instances.length / pageLimit));
  for (let pageNumber = 1; pageNumber <= pages; pageNumber++) {
    const listed = await read('/instances?page=' + pageNumber + '&limit=' + pageLimit);
    requireThat(listed.total === expected.instances.length && listed.page === pageNumber &&
      listed.limit === pageLimit && Array.isArray(listed.instances ?? []));
    for (const row of listed.instances ?? []) {
      requireThat(id(row.id) && !actual.has(row.id));
      actual.set(row.id, row);
    }
  }
  requireThat(actual.size === expected.instances.length);
  for (const row of expected.instances) checkInstance(actual.get(row.id), row, who.id);
  for (const denied of expected.forbidden_ids) {
    const response = await get('/instances/' + denied);
    requireThat(response.status() === 403);
  }
  receipt.owner_id = who.id;
  receipt.visible_ids = [...actual.keys()].sort((a, b) => a - b);
  receipt.forbidden_ids = expected.forbidden_ids;
  if (expected.forbidden_hermes_history) {
    step = 'hermes_history_denied';
    const { instance_id, session_id } = expected.forbidden_hermes_history;
    const desktopPath = `/instances/${instance_id}/hermes-desktop`;
    const bootstrap = await get(`${desktopPath}/bootstrap`);
    requireThat(bootstrap.status() === 403);
    receipt.hermes_bootstrap_denied_status = bootstrap.status();
    const forbidden = await get(`${desktopPath}/api/sessions/${encodeURIComponent(session_id)}/messages`);
    requireThat([401, 403].includes(forbidden.status()));
    receipt.hermes_history_denied_status = forbidden.status();
  }
  if (expected.forbidden_workspace_file) {
    step = 'workspace_file_denied';
    const forbidden = await get(workspaceDownload(expected.forbidden_workspace_file));
    requireThat(forbidden.status() === 403);
    receipt.workspace_file_denied_status = forbidden.status();
  }

  for (const sample of expected.start_ids) {
    step = 'sample_start';
    await verifyTarget();
    const instancePath = '/instances/' + sample;
    const info = expected.instances.find(row => row.id === sample);
    const evidence = { instance_id: sample, type: info.type, runtime_type: info.runtime_type, instance_mode: info.instance_mode };
    receipt.samples.push(evidence);
    await page.goto(url(instancePath), { waitUntil: 'domcontentloaded', timeout });
    requireThat(arrived(instancePath));
    checkInstance((await read(instancePath)).instance, info, who.id);
    const [started] = await Promise.all([
      page.waitForResponse(response => observe(response, 'POST', '/api/v1' + instancePath + '/start'), { timeout }),
      page.getByRole('button', { name: /^(Start|启动|啟動|開始)$/i }).click({ timeout }),
    ]);
    evidence.start_status = started.status();
    requireThat(started.ok() && (await started.json()).success === true);
    step = 'sample_availability';
    const deadline = performance.now() + timeout;
    while (true) {
      const remaining = Math.ceil(deadline - performance.now());
      requireThat(remaining > 0);
      const data = await read(instancePath + '/status', Math.min(remaining, 15000));
      const status = data.instance_status;
      requireThat(status?.instance_id === sample && status.status !== 'error');
      if (status.status === 'running' && status.availability === 'available') {
        evidence.status = 'running';
        evidence.availability = 'available';
        break;
      }
      await delay(Math.min(2000, remaining));
    }
    if (expected.hermes_history?.instance_id === sample) {
      step = 'hermes_history_readback';
      const history = expected.hermes_history;
      await page.reload({ waitUntil: 'domcontentloaded', timeout });
      await page.getByRole('button', { name: /^(Enter fullscreen|进入全屏|全画面表示|전체 화면으로 전환|Vollbild öffnen)$/i }).click({ timeout });
      const frame = page.frameLocator('iframe[title*="Hermes Desktop Web"]:visible');
      const openSidebar = frame.getByRole('button', { name: 'Show sidebar', exact: true });
      if (await openSidebar.isVisible()) await openSidebar.click({ timeout });
      await frame.getByText(history.title, { exact: true }).first().click({ timeout });
      for (const message of history.messages) {
        await frame.getByText(message.content, { exact: true }).first().waitFor({ timeout });
      }
      const sameHistory = await page.evaluate(async ({ path, messages }) => {
        const response = await fetch(path, { credentials: 'same-origin', cache: 'no-store' });
        if (!response.ok) return false;
        const actual = (await response.json()).messages;
        return Array.isArray(actual) && actual.length === messages.length &&
          actual.every((message, i) => message.role === messages[i].role && message.content === messages[i].content);
      }, {
        path: `/api/v1/instances/${sample}/hermes-desktop/api/sessions/${encodeURIComponent(history.session_id)}/messages?limit=500&order=oldest`,
        messages: history.messages,
      });
      requireThat(sameHistory);
      evidence.hermes_history_messages = history.messages.length;
    }
    const files = expected.workspace_files?.filter(file => file.instance_id === sample) ?? [];
    if (files.length) {
      step = 'workspace_file_readback';
      for (const file of files) {
        const response = await get(workspaceDownload(file));
        requireThat(response.ok() && response.headers()['content-length'] === String(file.size));
        const downloaded = await response.body();
        requireThat(downloaded.length === file.size && createHash('sha256').update(downloaded).digest('hex') === file.sha256);
      }
      evidence.workspace_files_verified = files.length;
    }
  }
  receipt.outcome = 'verified';
} catch {
  receipt.outcome = step;
  process.exitCode = 1;
} finally {
  await browser?.close();
  console.log(JSON.stringify(receipt));
}
