// Actual candidate App, MySQL migrations/audits, shared volume and browser.
// Team acceptance uses real APIs; adjacent IEI UI regression uses local fixtures.
// No live tenant writes, Runtime restarts or model calls.
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHmac, randomBytes } from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { chromium } from 'playwright';

const tag = process.env.TEAM_EDITOR_TAG || 'team-workspace-editor-20261010-v1';
const image = `10.130.14.23:5000/clawmanager-hxc-app:${tag}`;
const prefix = `team-editor-${process.pid}`;
const names = { mysql: `${prefix}-mysql`, redis: `${prefix}-redis`, app: `${prefix}-app` };
const containers = [], jwtKey = randomBytes(32).toString('hex');
let browser, networkCreated = false, volumeCreated = false;
const root = '/workspaces/teams/user-77/team-1-shared';
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
function docker(args, input) {
  const result = spawnSync('docker', args, { input, encoding: 'utf8', timeout: 120000 });
  if (result.status !== 0) throw new Error(`isolated Docker ${args[0]} failed (${result.status})`);
  return result.stdout.trim();
}
function start(name, img, args = []) {
  docker(['run', '-d', '--name', name, '--label', `clawmanager.team-editor=${prefix}`, '--network', prefix, ...args, img]); containers.push(name);
}
const envArgs = env => Object.entries(env).flatMap(([key, value]) => ['-e', `${key}=${value}`]);
function sql(query) { return docker(['exec', '-i', names.mysql, 'mysql', '--default-character-set=utf8mb4', '-uroot', '-pdisposable-local-only', '-N', '-B', 'clawmanager'], query); }
function token(userID = 77) {
  const encode = value => Buffer.from(JSON.stringify(value)).toString('base64url');
  const data = `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode({ user_id: userID, token_type: 'access', iat: Math.floor(Date.now() / 1000), exp: Math.floor(Date.now() / 1000) + 3600 })}`;
  return `${data}.${createHmac('sha256', jwtKey).update(data).digest('base64url')}`;
}
async function until(check, description) {
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) { if (await check()) return; await wait(500); }
  throw new Error(`${description} timed out`);
}
// Every target is a newly created local test container / isolated volume.
function write(name, content) { docker(['exec', '-i', names.app, 'tee', `${root}/${name}`], content); }
function read(name) { return readBytes(name).trimEnd(); }
function readBytes(name) { return Buffer.from(docker(['exec', names.app, 'base64', `${root}/${name}`]), 'base64').toString('utf8'); }

try {
  docker(['network', 'create', prefix]); networkCreated = true;
  docker(['volume', 'create', `${prefix}-workspaces`]); volumeCreated = true;
  start(names.mysql, 'mysql:8.4', ['--tmpfs', '/var/lib/mysql:rw,size=768m', ...envArgs({ MYSQL_ROOT_PASSWORD: 'disposable-local-only', MYSQL_DATABASE: 'clawmanager' })]);
  start(names.redis, 'redis:7-alpine');
  await until(() => { try { return sql('SELECT 1') === '1'; } catch { return false; } }, 'isolated database');
  const appArgs = ['-v', `${prefix}-workspaces:/workspaces`, '-p', '127.0.0.1::8443', ...envArgs({ DB_HOST: names.mysql, DB_USER: 'root', DB_PASSWORD: 'disposable-local-only', DB_NAME: 'clawmanager', JWT_SECRET: jwtKey, RUNTIME_NAMESPACE: 'team-editor-isolated', RUNTIME_SCHEDULER_ENABLED: 'false', PLATFORM_REDIS_URL: `redis://${names.redis}:6379/0` })];
  start(names.app, image, appArgs);
  await until(() => { try { return sql("SELECT COUNT(*) FROM schema_migrations WHERE filename='072_add_team_workspace_file_audits.sql'") === '1'; } catch { return false; } }, 'actual App migration072');
  console.log('PASS candidate App starts and applies real migration072');
  sql("INSERT INTO users(id,username,email,password_hash,role,is_active) VALUES(77,'editor-owner','editor@local.invalid','no-login','user',1),(78,'other-owner','other@local.invalid','no-login','user',1)");
  sql("INSERT INTO teams(id,user_id,name,status,communication_mode) VALUES(1,77,'文件编辑隔离验收','running','leader_mediated')");
  sql("INSERT INTO team_members(team_id,user_id,member_key,display_name,role,runtime_type,instance_mode,status,availability) VALUES(1,77,'leader','团队负责人','leader','openclaw','lite','idle','idle'),(1,77,'editor','编辑员','worker','hermes','lite','idle','idle')");
  docker(['exec', names.app, 'mkdir', '-p', root]);
  write('config.yaml', '\ufeffname: 初始\r\n'); write('report.md', '# 初始报告\n'); write('state.db', 'not editable');
  docker(['exec', names.app, 'chmod', '0640', `${root}/config.yaml`]);
  const port = JSON.parse(docker(['inspect', '-f', '{{json .NetworkSettings.Ports}}', names.app]))['8443/tcp'][0].HostPort;
  const origin = `https://127.0.0.1:${port}`;
  browser = await chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome', headless: true });
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(token => { localStorage.setItem('access_token', token); localStorage.setItem('clawmanager_locale', 'zh'); }, token());
  const page = await context.newPage(); page.setDefaultTimeout(15000);
  const errors = [], calls = [];
  page.on('pageerror', error => errors.push(String(error)));
  page.on('request', request => { if (request.url().includes('/workspace/')) calls.push(new URL(request.url()).pathname); });
  await page.goto(`${origin}/teams/1`); const workspace = page.locator('.team-chat-workspace:visible'); await workspace.waitFor();
  await workspace.locator('.team-tools-toolbar').getByRole('button', { name: '文件', exact: true }).click();
  const files = page.locator('.team-files-host:visible');
  const row = name => files.locator('.team-file-row').filter({ has: page.getByRole('button', { name, exact: true }) });
  await row('config.yaml').getByRole('button', { name: '编辑内容', exact: true }).click();
  const modal = page.locator('[data-workspace-editor]'), editor = modal.locator('.cm-content'); await editor.waitFor();
  assert.ok(await modal.evaluate(el => el.matches(':modal')));
  fs.mkdirSync('reports', { recursive: true });
  await page.screenshot({ path: 'reports/team-workspace-editor-desktop.png' });
  await editor.fill('name: 修改完成\n'); await page.keyboard.press('Control+s');
  await modal.getByText('已保存。不会自动重启实例或执行脚本。').waitFor();
  assert.equal(read('config.yaml'), '\ufeffname: 修改完成');
  assert.equal(readBytes('config.yaml'), '\ufeffname: 修改完成\r\n', 'BOM and CRLF survive actual browser save');
  assert.equal(docker(['exec', names.app, 'stat', '-c', '%a', `${root}/config.yaml`]), '640');
  assert.equal(sql("SELECT CONCAT(team_id,':',user_id,':',action) FROM team_workspace_file_audits ORDER BY id DESC LIMIT 1"), '1:77:edit_attempt');
  await editor.fill('name: [\n'); await modal.getByRole('button', { name: '保存', exact: true }).click();
  await modal.getByRole('alert').filter({ hasText: '语法检查未通过' }).waitFor(); assert.equal(read('config.yaml'), '\ufeffname: 修改完成');
  await editor.fill('name: 本地草稿\n'); write('config.yaml', 'name: Worker 新内容\n');
  await modal.getByText(/远端文件已更新/).waitFor(); assert.match(await editor.innerText(), /本地草稿/);
  await modal.getByRole('button', { name: '保存', exact: true }).click(); await modal.getByRole('alert').filter({ hasText: '文件已被修改' }).waitFor();
  assert.equal(read('config.yaml'), 'name: Worker 新内容'); assert.match(await editor.innerText(), /本地草稿/);
  const download = page.waitForEvent('download'); await modal.getByRole('button', { name: '下载草稿' }).click(); assert.equal((await download).suggestedFilename(), 'config.yaml');
  page.once('dialog', dialog => dialog.dismiss()); await page.keyboard.press('Escape'); assert.equal(await modal.count(), 1);
  page.once('dialog', dialog => dialog.accept()); await modal.getByRole('button', { name: '重新载入', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('.cm-content')?.textContent.includes('Worker 新内容'));
  assert.equal(await modal.getByRole('button', { name: '保存', exact: true }).isDisabled(), true);
  page.once('dialog', dialog => dialog.accept('renamed.yaml')); await modal.getByRole('button', { name: '重命名', exact: true }).click();
  await modal.getByText('renamed.yaml', { exact: true }).waitFor(); await modal.getByRole('button', { name: '关闭编辑器' }).click();
  await row('renamed.yaml').waitFor(); assert.equal(await row('state.db').getByRole('button', { name: '编辑内容', exact: true }).count(), 0);
  write('new.txt', '实时新增'); await row('new.txt').waitFor();
  await row('report.md').getByRole('button', { name: '预览', exact: true }).click();
  const preview = page.locator('[data-workspace-preview]'); await preview.getByRole('heading', { name: '初始报告' }).waitFor();
  write('report.md', '# 远端新报告\n'); await preview.getByRole('heading', { name: '远端新报告' }).waitFor();
  await preview.getByRole('button', { name: '编辑内容', exact: true }).click(); await editor.waitFor();
  assert.match(await editor.innerText(), /远端新报告/); await modal.getByRole('button', { name: '关闭编辑器' }).click();
  // Mobile drawer must not consume Escape intended for the native editor.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForFunction(() => [...document.querySelectorAll('.team-chat-workspace')].some(el => el.getBoundingClientRect().width > 0 && el.dataset.mode === 'drawer'));
  if (await workspace.getAttribute('data-right') !== 'files') await workspace.locator('.team-tools-toolbar').getByRole('button', { name: '文件', exact: true }).click();
  await row('report.md').getByRole('button', { name: '编辑内容', exact: true }).click(); await editor.waitFor();
  await page.screenshot({ path: 'reports/team-workspace-editor-mobile.png' });
  assert.ok(await modal.getByRole('button', { name: '保存', exact: true }).evaluate(button => getComputedStyle(button).whiteSpace === 'nowrap' && button.getBoundingClientRect().width >= 72), 'mobile Save must remain readable');
  await page.keyboard.press('Escape'); assert.equal(await modal.count(), 0); assert.equal(await workspace.getAttribute('data-right'), 'files');
  await workspace.getByRole('button', { name: '收起右侧面板' }).click();
  const count = calls.filter(path => path.endsWith('/files')).length; await wait(5500);
  assert.equal(calls.filter(path => path.endsWith('/files')).length, count, 'closed file drawer must not keep polling');
  const result = await page.evaluate(async ({ foreign, owner }) => {
    const headers = { Authorization: `Bearer ${foreign}` };
    const denied = await fetch('/api/v1/teams/1/workspace/text?path=report.md', { headers });
    const traversal = await fetch('/api/v1/teams/1/workspace/text?path=..%2Freport.md', { headers: { Authorization: `Bearer ${owner}` } });
    const missing = await fetch('/api/v1/teams/1/workspace/text?path=missing.md', { headers: { Authorization: `Bearer ${owner}` } });
    return [denied.status, traversal.status, missing.status];
  }, { foreign: token(78), owner: token() });
  assert.deepEqual(result, [403, 403, 404]); assert.deepEqual(errors, []);
  assert.equal(sql('SELECT COUNT(*) FROM team_tasks'), '0'); assert.equal(sql('SELECT COUNT(*) FROM instance_commands'), '0');
  const appB = `${prefix}-app-b`;
  start(appB, image, appArgs);
  const portB = JSON.parse(docker(['inspect', '-f', '{{json .NetworkSettings.Ports}}', appB]))['8443/tcp'][0].HostPort;
  const originB = `https://127.0.0.1:${portB}`;
  await until(async () => { try { return (await context.request.get(`${originB}/api/v1/version`)).ok(); } catch { return false; } }, 'second actual App');
  const authorization = { Authorization: `Bearer ${token()}` };
  const opened = await context.request.get(`${origin}/api/v1/teams/1/workspace/text?path=renamed.yaml`, { headers: authorization });
  const openedDoc = (await opened.json()).data;
  const race = await Promise.all([origin, originB].map((base, i) => context.request.put(`${base}/api/v1/teams/1/workspace/text`, { headers: authorization, data: { path: 'renamed.yaml', text: `name: concurrent-${i}\n`, version: openedDoc.version } })));
  assert.deepEqual(race.map(response => response.status()).sort(), [200, 409], 'two actual Linux App processes must not both save one version');
  docker(['exec', names.app, 'ln', '-s', '/etc/passwd', `${root}/link.md`]);
  docker(['exec', names.app, 'ln', `${root}/new.txt`, `${root}/hard.txt`]);
  for (const name of ['link.md', 'hard.txt']) {
    const response = await context.request.get(`${origin}/api/v1/teams/1/workspace/text?path=${name}`, { headers: authorization });
    assert.equal(response.status(), 422, 'actual image must reject symlink/hardlink editing');
  }
  fs.mkdirSync('.tmp', { recursive: true });
  const testModules = path.dirname(path.dirname(createRequire(import.meta.url).resolve('playwright')));
  const ownerRegression = spawnSync(process.execPath, ['frontend/scripts/workspace-maintenance.browser.cjs'], { env: { ...process.env, NODE_PATH: [testModules, process.env.NODE_PATH].filter(Boolean).join(path.delimiter), OWNER_PREVIEW_URL: origin }, stdio: 'inherit', timeout: 90000 });
  assert.equal(ownerRegression.status, 0, 'existing northbound owner editor browser regression');
  // Restart the local fixture App to exercise idempotent embedded migration startup.
  docker(['restart', names.app]);
  // Docker may allocate a different port when restarting a binding with HostPort=0.
  const restartedPort = JSON.parse(docker(['inspect', '-f', '{{json .NetworkSettings.Ports}}', names.app]))['8443/tcp'][0].HostPort;
  console.log(`Fixture restart binding: ${port} -> ${restartedPort}`);
  const restartedOrigin = `https://127.0.0.1:${restartedPort}`;
  await until(async () => { try { return (await context.request.get(`${restartedOrigin}/api/v1/version`, { timeout: 5000 })).ok(); } catch { return false; } }, 'fixture restart');
  const restartedDoc = await context.request.get(`${restartedOrigin}/api/v1/teams/1/workspace/text?path=renamed.yaml`, { headers: authorization });
  assert.equal(restartedDoc.status(), 200, 'owner text API survives actual App restart');
  assert.match((await restartedDoc.json()).data.text, /concurrent-[01]/);
  assert.equal(sql("SELECT COUNT(*) FROM schema_migrations WHERE filename='072_add_team_workspace_file_audits.sql'"), '1');
  assert.ok(Number(sql('SELECT COUNT(*) FROM team_workspace_file_audits')) >= 1);
  console.log('PASS actual candidate App: migrated audit, Team owner HTTP/CAS, UTF-8/BOM/permissions, browser edit/validate/conflict/draft/reload/rename, live list/preview, mobile drawer, no task/lifecycle writes, restart-safe migration');
} catch (error) {
  if (containers.includes(names.mysql)) {
    try { console.log('fixture MySQL state:', docker(['inspect', '-f', '{{.State.Status}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}}', names.mysql])); } catch {}
    const logs = spawnSync('docker', ['logs', names.mysql], { encoding: 'utf8', timeout: 10000 });
    const safe = (logs.stdout + logs.stderr).split('\n').filter(line => /error|fatal|disk|InnoDB/i.test(line) && !/password|secret|token|bearer|authorization|@tcp/i.test(line));
    if (safe.length) console.log(safe.slice(-8).join('\n'));
  }
  if (containers.includes(names.app)) {
    try { console.log('fixture App state:', docker(['inspect', '-f', '{{.State.Status}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}}', names.app])); } catch {}
    const logs = spawnSync('docker', ['logs', names.app], { encoding: 'utf8', timeout: 10000 });
    const safe = (logs.stdout + logs.stderr).split('\n').filter(line => /migration|initialize database|fatal|panic|error/i.test(line) && !/password|secret|token|bearer|authorization|@tcp/i.test(line));
    if (safe.length) console.log(safe.slice(-12).join('\n'));
  }
  throw error;
} finally {
  await browser?.close();
  for (const name of containers.reverse()) { try { docker(['rm', '-f', name]); } catch {} }
  if (volumeCreated) { try { docker(['volume', 'rm', `${prefix}-workspaces`]); } catch {} }
  if (networkCreated) { try { docker(['network', 'rm', prefix]); } catch {} }
}
