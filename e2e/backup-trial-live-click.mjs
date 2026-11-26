// Real browser and trial HTTP routes against an explicitly selected drill target.
import { constants, createReadStream } from 'node:fs';
import { copyFile, lstat, readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { isAbsolute } from 'node:path';
import { isDeepStrictEqual } from 'node:util';
import { chooseBackupSelection, validateSelection } from './backup-trial-selection.mjs';

const help = `Run one trial backup followed by one isolated restore from the admin page.
Set BACKUP_TRIAL_ORIGIN and BACKUP_TRIAL_ISOLATED_TARGET=confirmed.
Authenticate with BACKUP_TRIAL_STORAGE_STATE or BACKUP_TRIAL_USER and BACKUP_TRIAL_PASSWORD.
Optional BACKUP_TRIAL_REPLAY_ID restores an existing artifact without creating another.
Optional BACKUP_TRIAL_CAPTURE_ONLY=1 creates an artifact without restoring it.
Optional BACKUP_TRIAL_PREPARE_PROFILE pins the approved environment.json for a capture-only or replay run.
Optional BACKUP_TRIAL_SOURCE_RECEIPT verifies the source evidence before submitting a replay.
Optional BACKUP_TRIAL_SELECTION_FILE selects accounts, instances, and optional Hermes sessions and a workspace directory in a source run. Its JSON contains selection and expected_counts and requires BACKUP_TRIAL_PREPARE_PROFILE.
Optional BACKUP_TRIAL_DELIVERY_FILE saves or imports an absolute .tar path. Set BACKUP_TRIAL_PREPARE_PROFILE on both sides and BACKUP_TRIAL_SOURCE_RECEIPT on replay.
Optional BACKUP_TRIAL_HERMES_HISTORY=required verifies the Hermes history receipt.
Optional BACKUP_TRIAL_WORKSPACE_FILES=required verifies the workspace file receipt.
Optional BACKUP_TRIAL_TIMEOUT_MS, PLAYWRIGHT_CHANNEL and PLAYWRIGHT_EXECUTABLE_PATH.
Task submissions happen once. Output contains environment and task identifiers, public status and aggregate evidence.`;
if (process.argv.includes('--help')) {
  console.log(help);
  process.exit(0);
}

const trialPath = '/api/v1/admin/system-backup/trial';
const idPattern = /^[0-9a-f]{32}$/;
const digestPattern = /^[0-9a-f]{64}$/;
const namePattern = /^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/;
const uidPattern = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/;
const terminal = new Set(['succeeded', 'failed']);
const preparingStates = new Set(['waiting', 'running', 'prepared']);
const tables = new Set([
  'users', 'user_quotas', 'instances', 'openclaw_injection_snapshots',
  'instance_desired_state', 'instance_config_revisions',
]);
const receipt = { outcome: 'preflight', backup: null, restore: null };
let browser;
let step = 'preflight';

function requireThat(condition) {
  if (!condition) throw new Error('Trial browser acceptance assertion failed');
}

function observe(response, origin, method, path) {
  const url = new URL(response.url());
  return url.origin === origin && url.pathname === path && response.request().method() === method;
}

function confirmCounts(counts) {
  requireThat(counts && Object.keys(counts).length === tables.size);
  for (const [table, count] of Object.entries(counts)) {
    requireThat(tables.has(table) && Number.isSafeInteger(count) && count >= 0);
  }
}

function confirmBackup(expected, actual) {
  for (const field of ['counts', 'hermes_history', 'workspace_files']) {
    requireThat(isDeepStrictEqual(expected[field], actual[field]));
  }
  if (expected.ciphertext_sha256 !== undefined) requireThat(actual.ciphertext_sha256 === expected.ciphertext_sha256);
}

function placement(profile) {
  const { source, target } = profile;
  return {
    source: { cluster: source.cluster, namespace: source.namespace },
    target: { cluster: target.cluster, namespace: target.namespace },
  };
}

async function readEvidenceFile(path) {
  const raw = await readFile(path, 'utf8');
  return JSON.parse(raw.replace(/^\uFEFF/, ''));
}

async function deliveryDigest(path) {
  const info = await lstat(path);
  requireThat(info.isFile() && Number.isSafeInteger(info.size) && info.size > 0);
  const hash = createHash('sha256');
  let bytes = 0;
  for await (const chunk of createReadStream(path)) {
    hash.update(chunk);
    bytes += chunk.length;
  }
  requireThat(bytes === info.size);
  return { bytes, sha256: hash.digest('hex') };
}

async function guardSubmission(page, origin, path, allow) {
  let used = false;
  await page.route(url => url.origin === origin && url.pathname === path, async route => {
    const request = route.request();
    if (request.method() !== 'POST') return route.continue();
    try {
      if (!used && allow(request)) {
        used = true;
        return route.continue();
      }
    } catch {
      // Malformed requests fail closed, before the operation reaches the server.
    }
    return route.abort();
  });
}

async function exportDelivery(page, panel, origin, path, backup, timeout) {
  requireThat(digestPattern.test(backup.ciphertext_sha256));
  const [download, response] = await Promise.all([
    page.waitForEvent('download', { timeout }),
    page.waitForResponse(item => observe(item, origin, 'GET', trialPath + '/backups/' + backup.backup_id + '/transfer'), { timeout }),
    panel.getByRole('button', { name: /^(下载备份交付包|Download backup delivery package)$/ }).click({ timeout }),
  ]);
  requireThat(response.ok() && new URL(response.url()).searchParams.get('expected_ciphertext_sha256') === backup.ciphertext_sha256);
  requireThat(download.suggestedFilename() === `backup-trial-${backup.backup_id}.tar`);
  await copyFile(await download.path(), path, constants.COPYFILE_EXCL);
  receipt.delivery = await deliveryDigest(path);
}

async function importDelivery(page, panel, origin, path, timeout) {
  const [response] = await Promise.all([
    page.waitForResponse(item => observe(item, origin, 'POST', trialPath + '/artifacts/import'), { timeout }),
    panel.getByLabel(/^(导入备份交付包|Import backup delivery package)$/).setInputFiles(path, { timeout }),
  ]);
  requireThat(response.ok());
  // The page verifies the handoff. Preflight below verifies the source digest
  // again from the target mount before the script allows any restore request.
  await panel.getByText(/^(原件已到目标导入卷，请检查此备份。|The original files are on the target import volume\. Run the backup check\.)$/).waitFor({ timeout });
}

async function expectedPreparation(file, side) {
  const expected = await readEvidenceFile(file);
  const { source, target } = expected;
  const names = [expected.revision, source.cluster, source.namespace, target.cluster, target.namespace];
  requireThat(expected.side === side && typeof expected.prepare_job?.uid === 'string' && uidPattern.test(expected.prepare_job.uid));
  requireThat(names.every((name) => typeof name === 'string' && namePattern.test(name)));
  requireThat(source.cluster !== target.cluster || source.namespace !== target.namespace);
  return expected;
}

async function prepareEnvironment(page, panel, initial, expected, origin, timeout) {
  const missing = (checks) => expected.side === 'source' ? checks.missing : checks.restore_missing;
  const verify = (environment) => {
    requireThat(isDeepStrictEqual(environment?.profile, expected));
    if (/^[a-z_]+$/.test(environment.state)) receipt.preparation.state = environment.state;
    requireThat(preparingStates.has(environment.state));
  };

  receipt.preparation = {
    side: expected.side, revision: expected.revision, job_uid: expected.prepare_job.uid,
    ...placement(expected), clicked: false,
  };
  verify(initial.environment);
  let ready = initial;
  if (initial.environment.state !== 'prepared' || missing(initial)?.length) {
    const after = page.waitForResponse(async (response) => {
      if (!observe(response, origin, 'GET', trialPath + '/readiness') || !response.ok()) return false;
      const checks = await response.json();
      verify(checks.environment);
      return checks.environment.state === 'prepared' && !missing(checks)?.length;
    }, { timeout });
    async function activate() {
      if (initial.environment.state !== 'waiting') return;
      const button = panel.getByRole('button', { name: /^(准备试行环境|Prepare trial environment)$/ });
      const [response] = await Promise.all([
        page.waitForResponse((item) => observe(item, origin, 'POST', trialPath + '/environment/prepare'), { timeout }),
        button.click({ timeout }),
      ]);
      receipt.preparation.clicked = true;
      requireThat(response.ok());
      const environment = await response.json();
      verify(environment);
      requireThat(environment.state === 'running' || environment.state === 'prepared');
    }
    const [complete] = await Promise.all([after, activate()]);
    ready = await complete.json();
  }
  receipt.preparation.state = 'prepared';
  await panel.getByText(/^(准备任务已完成|Preparation job completed)$/).waitFor({ timeout });
  return ready;
}

async function execute(page, control, path, origin, timeout, result, required) {
  const pending = page.waitForResponse(response => observe(response, origin, 'POST', path), { timeout });
  const [accepted] = await Promise.all([pending, control.click({ timeout })]);
  requireThat(accepted.status() === 202);
  const acceptedTask = await accepted.json();
  requireThat(idPattern.test(acceptedTask.id) && acceptedTask.status === 'running');
  Object.assign(result, { task_id: acceptedTask.id, http_status: 202 });
  const finished = await page.waitForResponse(async response => {
    if (!observe(response, origin, 'GET', trialPath + '/task') || !response.ok()) return false;
    const { task } = await response.json();
    return task?.id === acceptedTask.id && terminal.has(task.status);
  }, { timeout });
  const { task } = await finished.json();
  result.status = task.status;
  if (/^[a-z_]+$/.test(task.stage)) result.stage = task.stage;
  if (/^[a-z_]+$/.test(task.error)) result.error_code = task.error;
  requireThat(task.status === 'succeeded' && task.stage === 'done' && task.readback_verified === true);
  requireThat(idPattern.test(task.backup_id));
  confirmCounts(task.counts);
  Object.assign(result, { backup_id: task.backup_id, readback_verified: true, counts: task.counts });
  if (digestPattern.test(task.ciphertext_sha256)) result.ciphertext_sha256 = task.ciphertext_sha256;
  if (required.history || task.hermes_history) {
    const history = task.hermes_history;
    requireThat(Number.isSafeInteger(history?.sessions) && history.sessions > 0 &&
      Number.isSafeInteger(history.messages) && history.messages >= 0 && /^[0-9a-f]{64}$/.test(history.transcript_sha256));
    result.hermes_history = { sessions: history.sessions, messages: history.messages, transcript_sha256: history.transcript_sha256 };
  }
  if (required.files || task.workspace_files) {
    const files = task.workspace_files;
    requireThat(Number.isSafeInteger(files?.entries) && files.entries > 0 &&
      Number.isSafeInteger(files.file_bytes) && files.file_bytes >= 0 && /^[0-9a-f]{64}$/.test(files.archive_sha256));
    result.workspace_files = { entries: files.entries, file_bytes: files.file_bytes, archive_sha256: files.archive_sha256 };
  }
}

try {
  const origin = new URL(process.env.BACKUP_TRIAL_ORIGIN);
  requireThat(['http:', 'https:'].includes(origin.protocol) && origin.href === origin.origin + '/');
  requireThat(origin.protocol === 'https:' || ['127.0.0.1', 'localhost', '[::1]'].includes(origin.hostname));
  requireThat(process.env.BACKUP_TRIAL_ISOLATED_TARGET === 'confirmed');
  const storageState = process.env.BACKUP_TRIAL_STORAGE_STATE || undefined;
  const user = process.env.BACKUP_TRIAL_USER;
  const password = process.env.BACKUP_TRIAL_PASSWORD;
  requireThat(storageState || (user && password));
  const replay = process.env.BACKUP_TRIAL_REPLAY_ID;
  const captureOnly = process.env.BACKUP_TRIAL_CAPTURE_ONLY === '1';
  const required = {
    history: process.env.BACKUP_TRIAL_HERMES_HISTORY === 'required',
    files: process.env.BACKUP_TRIAL_WORKSPACE_FILES === 'required',
  };
  requireThat(!replay || idPattern.test(replay));
  requireThat(!captureOnly || !replay);
  const preparationFile = process.env.BACKUP_TRIAL_PREPARE_PROFILE;
  const sourceReceiptFile = process.env.BACKUP_TRIAL_SOURCE_RECEIPT;
  const selectionFile = process.env.BACKUP_TRIAL_SELECTION_FILE;
  const delivery = process.env.BACKUP_TRIAL_DELIVERY_FILE;
  requireThat(!preparationFile || captureOnly || replay);
  requireThat(!sourceReceiptFile || replay);
  requireThat(!selectionFile || captureOnly && preparationFile);
  requireThat(!delivery || (isAbsolute(delivery) && /\.tar$/i.test(delivery) && preparationFile && (captureOnly || replay && sourceReceiptFile)));
  const expectedProfile = preparationFile ? await expectedPreparation(preparationFile, captureOnly ? 'source' : 'target') : undefined;
  let chosen;
  if (selectionFile) {
    chosen = await readEvidenceFile(selectionFile);
    validateSelection(chosen, expectedProfile);
    confirmCounts(chosen.expected_counts);
    required.history ||= !!chosen.selection.hermes_sessions;
    required.files ||= !!chosen.selection.workspace_directory;
  }
  if (delivery && captureOnly) {
    const existing = await lstat(delivery).catch(err => {
      if (err.code !== 'ENOENT') throw err;
    });
    requireThat(!existing);
  }
  let expectedBackup;
  if (sourceReceiptFile) {
    const sourceReceipt = await readEvidenceFile(sourceReceiptFile);
    expectedBackup = sourceReceipt.backup;
    requireThat(sourceReceipt.outcome === 'verified' && sourceReceipt.restore === null &&
      expectedBackup?.backup_id === replay && expectedBackup?.task_id === replay && expectedBackup?.readback_verified === true);
    confirmCounts(expectedBackup.counts);
    if (expectedProfile && sourceReceipt.preparation) {
      const sourcePreparation = sourceReceipt.preparation;
      requireThat(sourcePreparation.side === 'source' && sourcePreparation.state === 'prepared' &&
        isDeepStrictEqual(placement(sourcePreparation), placement(expectedProfile)));
    }
    if (delivery) {
      requireThat(sourceReceipt.preparation && digestPattern.test(expectedBackup.ciphertext_sha256));
      receipt.delivery = await deliveryDigest(delivery);
      requireThat(isDeepStrictEqual(sourceReceipt.delivery, receipt.delivery));
    }
  }
  const timeout = Number(process.env.BACKUP_TRIAL_TIMEOUT_MS || 120000);
  requireThat(Number.isSafeInteger(timeout) && timeout >= 1000 && timeout <= 600000);

  step = 'launch_browser';
  const { chromium } = await import('playwright-core');
  const executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
  browser = await chromium.launch({
    headless: true,
    channel: executablePath ? undefined : process.env.PLAYWRIGHT_CHANNEL || 'chrome',
    executablePath,
  });
  const context = await browser.newContext({ storageState, serviceWorkers: 'block' });
  const page = await context.newPage();
  if (!storageState) {
    step = 'login';
    await page.goto(new URL('/login', origin).href, { timeout });
    await page.locator('#username').fill(user, { timeout });
    await page.locator('#password').fill(password, { timeout });
    const [login] = await Promise.all([
      page.waitForResponse(response => observe(response, origin.origin, 'POST', '/api/v1/auth/login'), { timeout }),
      page.locator('button[type=submit]').click({ timeout }),
    ]);
    requireThat(login.ok());
  }
  step = 'page_readiness';
  const [ready] = await Promise.all([
    page.waitForResponse(response => observe(response, origin.origin, 'GET', trialPath + '/readiness'), { timeout }),
    page.goto(new URL('/admin/backups/records', origin).href, { waitUntil: 'domcontentloaded', timeout }),
  ]);
  requireThat(ready.ok());
  let checks = await ready.json();
  const panel = page.getByRole('region', { name: /^(备份（试行版）|Backup \(trial\))$/ });
  if (expectedProfile) {
    step = 'prepare_environment';
    checks = await prepareEnvironment(page, panel, checks, expectedProfile, origin.origin, timeout);
  }
  step = 'page_readiness';
  requireThat((checks.missing === null || Array.isArray(checks.missing)) &&
    (checks.restore_missing === null || Array.isArray(checks.restore_missing)));
  requireThat((captureOnly || !checks.restore_missing?.length) && (replay || !checks.missing?.length));
  const input = panel.getByLabel(/^(试行备份 ID|Trial backup ID)$/);
  const success = panel.getByRole('status').getByText(/· (成功 · 已完成|succeeded · done)$/);
  if (!replay) {
    step = 'backup_or_readback';
    receipt.backup = {};
    let control = panel.getByRole('button', { name: /^(创建试行备份|Create trial backup)$/ });
    if (chosen) {
      step = 'select_backup_scope';
      control = await chooseBackupSelection(panel, chosen.selection, expectedProfile, timeout);
      await guardSubmission(page, origin.origin, trialPath + '/backups', request => isDeepStrictEqual(request.postDataJSON(), chosen.selection));
      step = 'backup_or_readback';
    }
    await execute(page, control,
      trialPath + '/backups', origin.origin, timeout, receipt.backup, required);
    requireThat(receipt.backup.task_id === receipt.backup.backup_id);
    if (chosen) requireThat(isDeepStrictEqual(receipt.backup.counts, chosen.expected_counts));
    await panel.getByText(/^(独立读回校验通过|Independent readback verified)$/).waitFor({ timeout });
    await success.waitFor({ timeout });
    requireThat((await input.inputValue({ timeout })) === receipt.backup.backup_id);
    if (delivery) {
      step = 'export_delivery';
      await exportDelivery(page, panel, origin.origin, delivery, receipt.backup, timeout);
    }
  } else if (delivery) {
    step = 'import_delivery';
    requireThat(checks.import_available === true);
    await importDelivery(page, panel, origin.origin, delivery, timeout);
    requireThat((await input.inputValue({ timeout })) === expectedBackup.backup_id);
  } else {
    await input.fill(replay, { timeout });
  }

  if (!captureOnly) {
    const id = replay || receipt.backup.backup_id;
    step = 'restore_preflight';
    const [checked] = await Promise.all([
      page.waitForResponse(response => observe(response, origin.origin, 'POST', trialPath + '/backups/' + id + '/preflight'), { timeout }),
      panel.getByRole('button', { name: /^(检查此备份|Check this backup)$/ }).click({ timeout }),
    ]);
    requireThat(checked.ok());
    const preflight = await checked.json();
    requireThat(preflight.backup_id === id && preflight.readback_verified === true && !preflight.missing);
    const reference = expectedBackup ?? receipt.backup;
    if (reference) confirmBackup(reference, preflight);
    await panel.getByText(/^(产物读回和恢复内容预检通过|Artifact readback and restore content checks passed)$/).waitFor({ timeout });
    step = 'restore_or_reconcile';
    receipt.restore = {};
    if (delivery) {
      await guardSubmission(page, origin.origin, trialPath + '/backups/' + id + '/restore', request => {
        const digests = new URL(request.url()).searchParams.getAll('expected_ciphertext_sha256');
        return digests.length === 1 && digests[0] === expectedBackup.ciphertext_sha256;
      });
    }
    await execute(page,
      panel.getByRole('button', { name: /^(恢复到隔离环境|Restore to isolated target)$/ }),
      trialPath + '/backups/' + id + '/restore', origin.origin, timeout, receipt.restore, required);
    requireThat(receipt.restore.backup_id === id && receipt.restore.task_id !== id);
    if (reference) confirmBackup(reference, receipt.restore);
    await panel.getByRole('status').getByText(receipt.restore.task_id, { exact: true }).waitFor({ timeout });
    await success.waitFor({ timeout });
    await panel.getByText(/^(独立读回校验通过|Independent readback verified)$/).waitFor({ timeout });
  }
  receipt.outcome = 'verified';
} catch {
  receipt.outcome = step;
  process.exitCode = 1;
} finally {
  await browser?.close();
  console.log(JSON.stringify(receipt));
}
