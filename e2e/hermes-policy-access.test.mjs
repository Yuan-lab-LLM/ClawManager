// Real built ClawManager + real ENTRYPOINT/Agent/Hermes, isolated local data only.
// Kubernetes scheduling and external/paid models are deliberately not simulated.
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { createHmac, randomBytes } from 'node:crypto';
import { chromium } from 'playwright';
import fs from 'node:fs';

const tag = process.env.HERMES_ACCESS_TAG || 'team-hermes-policy-access-20261009-v1';
const appImage = `10.130.14.23:5000/clawmanager-hxc-app:${tag}`;
const runtimeImage = `10.130.14.23:5000/agentsruntime/hermes-lite:${process.env.HERMES_ACCESS_RUNTIME_TAG || tag}`;
const prefix = `hermes-access-${process.pid}`;
const namespace = 'hermes-access-isolated';
const names = { mysql: `${prefix}-mysql`, redis: `${prefix}-redis`, app: `${prefix}-app`, runtime: `${prefix}-runtime` };
const containers = [];
const secrets = Object.fromEntries(['control', 'report', 'jwt', 'instance', 'model'].map(key => [key, randomBytes(32).toString('hex')]));
const label = `clawmanager.hermes-access=${prefix}`;
// Local candidate identity is its inspected config hash, not a claim about a
// published Registry manifest. Registry identity is verified separately at push.
const localRuntimeIdentity = `fixture/hermes-lite@${spawnSync('docker', ['image', 'inspect', '-f', '{{.Id}}', runtimeImage], { encoding: 'utf8' }).stdout.trim()}`;
let browser, renewTimer, stubProcess, activePage, renewalFailure;
let volumeCreated = false;
let allowedPeers = [];
let renewQueue = Promise.resolve();
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

function docker(args, input) {
  const result = spawnSync('docker', args, { input, encoding: 'utf8', timeout: 120000 });
  if (result.status !== 0) {
    // Never print Docker argv/env or credentials on a failure.
    throw new Error(`Local Docker operation ${args[0]} failed (${result.status})`);
  }
  return result.stdout.trim();
}
function start(name, image, args = []) {
  docker(['run', '-d', '--name', name, '--label', label, '--network', prefix, ...args, image]);
  containers.push(name);
}
function sql(query) {
  return docker(['exec', '-i', names.mysql, 'mysql', '-uroot', '-pdisposable-local-only', '-N', '-B', 'clawmanager'], query);
}
function ip(name) {
  return docker(['inspect', '-f', '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}', name]);
}
function publishedPort(name, port) {
  const bindings = JSON.parse(docker(['inspect', '-f', '{{json .HostConfig.PortBindings}}', name]));
  const binding = bindings[`${port}/tcp`]?.[0];
  assert.ok(binding, 'missing local-only port binding');
  const actual = JSON.parse(docker(['inspect', '-f', '{{json .NetworkSettings.Ports}}', name]));
  if (!actual[`${port}/tcp`]?.[0]?.HostPort) {
    const logs = docker(['logs', name]);
    // Config errors are allowlisted; do not emit arbitrary logs or credentials.
    const safe = logs.split('\n').filter(line => /load runtime agent config:/.test(line) && !/token|password|secret/i.test(line));
    if (safe.length) console.log(safe.join('\n'));
  }
  assert.ok(actual[`${port}/tcp`]?.[0]?.HostPort, `local fixture ${name} exited or has not published its port`);
  return actual[`${port}/tcp`][0].HostPort;
}
async function until(check, description, timeout = 90000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await check()) return;
    await delay(500);
  }
  throw new Error(`${description} timed out`);
}
const envArgs = env => Object.entries(env).flatMap(([key, value]) => ['-e', `${key}=${value}`]);
let controlBase;
async function control(path, body, method) {
  const response = await fetch(controlBase + path, {
    method: method || (body ? 'POST' : 'GET'),
    headers: { 'X-ClawManager-Control-Token': secrets.control, 'Content-Type': 'application/json' },
    ...(body ? { body: JSON.stringify(body) } : {}), signal: AbortSignal.timeout(30000),
  });
  assert.equal(response.status, 200, `Agent ${method || 'GET'} ${path}`);
  return response.status === 204 ? null : response.json();
}
function renewPolicy() {
  const peers = [...allowedPeers];
  const next = renewQueue.then(async () => {
    const previous = await control('/v1/proxy-policy');
    await control('/v1/proxy-policy', { namespace, expected_revision: previous.revision, proxies: peers }, 'PUT');
  });
  renewQueue = next.catch(() => {});
  return next;
}
function jwt() {
  const encode = value => Buffer.from(JSON.stringify(value)).toString('base64url');
  const unsigned = `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode({ user_id: 77, token_type: 'access', iat: Math.floor(Date.now() / 1000), exp: Math.floor(Date.now() / 1000) + 3600 })}`;
  return `${unsigned}.${createHmac('sha256', secrets.jwt).update(unsigned).digest('base64url')}`;
}

// Runs in the actual browser through CM's authenticated same-origin BFF.
async function exerciseBrowser(page, iid) {
  return page.evaluate(async iid => {
    const base = `/api/v1/instances/${iid}/hermes-desktop`;
    const token = localStorage.getItem('access_token');
    const probe = await fetch(base + '/bootstrap', { headers: { Authorization: 'Bearer ' + token } }).then(r => r.json());
    if (!probe.data?.available) throw new Error('real capability probe rejected: ' + probe.data?.reason);
    const activate = await fetch(base + '/bootstrap', { method: 'POST', headers: { Authorization: 'Bearer ' + token } }).then(r => r.json());
    if (!activate.data?.available) throw new Error('real upstream login rejected: ' + activate.data?.reason);
    for (const path of ['/api/status', '/api/model/info', '/api/sessions']) {
      const response = await fetch(base + path);
      if (!response.ok) throw new Error('actual BFF HTTP read failed: ' + path + '/' + response.status);
    }
    const ticket = await fetch(base + '/ws-ticket', { method: 'POST' }).then(r => r.json());
    const socketURL = new URL(ticket.data.url, location.origin);
    socketURL.protocol = 'wss:';
    const ws = new WebSocket(socketURL);
    const pending = new Map(), events = [];
    let seq = 0;
    ws.onmessage = message => {
      for (const line of String(message.data).trim().split('\n')) {
        const packet = JSON.parse(line);
        if (packet.method === 'event') events.push(packet.params);
        else if (pending.has(packet.id)) {
          const { resolve, reject, timer } = pending.get(packet.id);
          clearTimeout(timer); pending.delete(packet.id);
          packet.error ? reject(new Error(JSON.stringify(packet.error))) : resolve(packet.result);
        }
      }
    };
    const wait = async predicate => {
      const end = Date.now() + 60000;
      while (Date.now() < end) {
        const value = predicate(); if (value) return value;
        await new Promise(resolve => setTimeout(resolve, 100));
      }
      throw new Error('actual BFF WebSocket event timeout');
    };
    const call = (method, params = {}) => new Promise((resolve, reject) => {
      const id = ++seq;
      const timer = setTimeout(() => { pending.delete(id); reject(new Error('RPC timeout: ' + method)); }, 45000);
      pending.set(id, { resolve, reject, timer });
      ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
    });
    try {
      await wait(() => ws.readyState === WebSocket.OPEN);
      await wait(() => events.find(e => e.type === 'gateway.ready'));
      await call('ping');
      const setup = await call('setup.status');
      if (!setup.provider_configured) throw new Error('actual provider not configured');
      const catalog = await call('model.options', { explicit_only: true });
      const provider = catalog.providers.find(row => row.models?.includes('smoke-model') && (row.is_current || catalog.provider === row.slug || row.aliases?.includes(catalog.provider)))?.slug;
      if (!provider) throw new Error('actual model catalogue unavailable');
      const created = await call('session.create', { source: 'web', cols: 96, model: 'smoke-model', provider });
      const sid = created.session_id, stored = created.stored_session_id;
      if (!sid || !stored || sid === stored) throw new Error('actual durable/live session identity invalid');
      const submitted = await call('prompt.submit', { session_id: sid, text: 'SMOKE_TEXT local real ClawManager BFF acceptance' });
      if (submitted.status !== 'streaming') throw new Error('actual prompt not accepted');
      const done = await wait(() => events.find(e => e.type === 'message.complete' && e.session_id === sid));
      if (done.payload?.status === 'error') throw new Error('actual model turn failed');
      const history = await call('session.history', { session_id: sid });
      if (history.count !== 2 || !JSON.stringify(history).includes('SMOKE_TEXT')) throw new Error('actual history lost/duplicated');
      const httpHistory = await fetch(`${base}/api/sessions/${stored}/messages`);
      if (!httpHistory.ok || !(await httpHistory.text()).includes('SMOKE_TEXT')) throw new Error('actual BFF durable history unavailable');
      // CM must deny Core-disallowed operations even with an authenticated socket.
      let denied = false;
      try { await call('shell.exec', { command: 'must-not-execute' }); } catch { denied = true; }
      if (!denied) throw new Error('forbidden RPC accepted');
      return { stored, count: history.count, streaming: events.some(e => e.type === 'message.delta' && e.session_id === sid) };
    } finally { ws.close(); }
  }, iid);
}

try {
  docker(['network', 'create', '--label', label, prefix]);
  docker(['volume', 'create', '--label', label, `${prefix}-workspaces`]);
  volumeCreated = true;
  start(names.mysql, 'mysql:8.4', ['--tmpfs', '/var/lib/mysql:rw,size=768m', ...envArgs({ MYSQL_ROOT_PASSWORD: 'disposable-local-only', MYSQL_DATABASE: 'clawmanager' })]);
  start(names.redis, 'redis:7-alpine');
  await until(() => { try { return sql('SELECT 1') === '1'; } catch { return false; } }, 'local database');
  const appDNS = `clawmanager-gateway.${namespace}.svc.cluster.local`;
  const modelDNS = `local-model.${namespace}.svc`;
  const origin = `http://${appDNS}:9001`;
  const common = { RUNTIME_AGENT_CONTROL_TOKEN: secrets.control, RUNTIME_AGENT_REPORT_TOKEN: secrets.report, CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED: 'true', CLAWMANAGER_CONTROL_UI_ORIGIN: origin };
  start(names.app, appImage, ['--network-alias', appDNS, '-v', `${prefix}-workspaces:/workspaces`, '-p', '127.0.0.1::8443', ...envArgs({ ...common, DB_HOST: names.mysql, DB_USER: 'root', DB_PASSWORD: 'disposable-local-only', DB_NAME: 'clawmanager', JWT_SECRET: secrets.jwt, RUNTIME_NAMESPACE: namespace, RUNTIME_SCHEDULER_ENABLED: 'false', PLATFORM_REDIS_URL: `redis://${names.redis}:6379/0` })]);
  const publicURL = `https://127.0.0.1:${publishedPort(names.app, 8443)}`;
  await until(() => { try { return sql("SELECT COUNT(*) FROM schema_migrations WHERE filename='071_add_team_member_icon.sql'") === '1'; } catch { return false; } }, 'real App migrations');
  // The invented smoke model has no public metadata. Make those optional
  // catalogue lookups fail immediately/offline instead of depending on WAN
  // reachability and paying its5s-connect/10s-read budget during this test.
  // This changes only fixture DNS, never the image, production config or API.
  start(names.runtime, runtimeImage, ['--add-host', `${modelDNS}:127.0.0.1`, '--add-host', 'models.dev:127.0.0.1', '--add-host', 'openrouter.ai:127.0.0.1', '-v', `${prefix}-workspaces:/workspaces`, '-p', '127.0.0.1::19090', ...envArgs({ ...common, POD_NAMESPACE: namespace, POD_NAME: names.runtime, POD_UID: `${prefix}-runtime-uid`, CLAWMANAGER_BACKEND_URL: origin, CLAWMANAGER_RUNTIME_IMAGE_REF: localRuntimeIdentity })]);
  controlBase = `http://127.0.0.1:${publishedPort(names.runtime, 19090)}`;
  await until(async () => { try { return (await control('/v1/health')).status === 'ready'; } catch { return false; } }, 'real Runtime entrypoint');
  await until(() => sql(`SELECT COUNT(*) FROM runtime_pods WHERE namespace='${namespace}' AND pod_name='${names.runtime}'`) === '1', 'actual Agent registration');
  const podID = sql(`SELECT id FROM runtime_pods WHERE namespace='${namespace}' AND pod_name='${names.runtime}'`);
  allowedPeers = [ip(names.app)];
  await renewPolicy();
  renewTimer = setInterval(() => renewPolicy().catch(error => { renewalFailure = error; }), 10000);
  // Reproduces an existing root0700 parent; only the real Agent renew may repair it.
  docker(['exec', names.runtime, 'chmod', '0700', '/run/clawmanager-proxy-policy']);
  await renewPolicy();
  assert.equal(docker(['exec', names.runtime, 'stat', '-c', '%a', '/run/clawmanager-proxy-policy']), '755');
  docker(['exec', '--user', '209101:1000', names.runtime, 'sh', '-c', 'test -r /run/clawmanager-proxy-policy/policy.json && test ! -w /run/clawmanager-proxy-policy/policy.json && test ! -w /run/clawmanager-proxy-policy']);
  console.log('PASS actual ENTRYPOINT umask077 / existing0700 repair / isolated read-only UID');
  sql("INSERT INTO users(id,username,email,password_hash,role,is_active) VALUES(77,'local-hermes-acceptance','fixture@local.invalid','no-password-login','user',1)");
  sql("INSERT INTO teams(id,user_id,name,status,communication_mode) VALUES(1,77,'isolated Hermes Team','running','leader_only')");
  docker(['exec', names.runtime, 'sh', '-c', 'mkdir -p /workspaces/teams/user-77/team-1-shared && chown 0:1000 /workspaces/teams/user-77/team-1-shared && chmod 2770 /workspaces/teams/user-77/team-1-shared']);
  const stubCode = "import sys,signal; from pathlib import Path; sys.path.insert(0,'/usr/local/share/hermes-lite/tests'); from smoke_lite_desktop_rpc import ModelStub; stub=ModelStub(Path('/workspaces/hermes/user-77/instance-9101'),sys.argv[1]); print(stub.url,flush=True); signal.pause()";
  stubProcess = spawn('docker', ['exec', names.runtime, 'python3', '-u', '-c', stubCode, secrets.model], { stdio: ['ignore', 'pipe', 'pipe'] });
  const stubURL = await new Promise((resolve, reject) => {
    let output = '';
    const timeout = setTimeout(() => reject(new Error('local model fixture failed to start')), 15000);
    stubProcess.stdout.on('data', chunk => { output += chunk; if (output.includes('\n')) { clearTimeout(timeout); resolve(output.trim()); } });
    stubProcess.on('exit', code => { if (code) { clearTimeout(timeout); reject(new Error('local model fixture exited')); } });
  });
  const ids = [9101, 9102, 9103];
  for (const [offset, iid] of ids.entries()) {
    const port = 20000 + offset, workspace = `/workspaces/hermes/user-77/instance-${iid}`;
    const token = secrets.instance + iid;
    sql(`INSERT INTO instances(id,user_id,name,type,runtime_type,instance_mode,status,cpu_cores,memory_gb,disk_gb,os_type,os_version,workspace_path,runtime_generation,access_token) VALUES(${iid},77,'fixture-${iid}','hermes','gateway','lite','creating',1,1,1,'hermes','lite','${workspace}',1,'${token}')`);
    sql(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(${iid},${podID},'hermes','hermes-${iid}-1',${port},'${workspace}','creating',1)`);
    const member = offset === 1 ? 'leader' : 'briefing-editor';
    if (offset) sql(`INSERT INTO team_members(team_id,instance_id,user_id,member_key,display_name,role,runtime_type,instance_mode,status) VALUES(1,${iid},77,'${member}','${member}','${offset === 1 ? 'leader' : 'worker'}','hermes','lite','idle')`);
    const environment = { CLAWMANAGER_INSTANCE_TOKEN: token, CLAWMANAGER_LLM_API_KEY: secrets.model, CLAWMANAGER_LLM_BASE_URL: stubURL.replace('127.0.0.1', modelDNS), CLAWMANAGER_LLM_MODEL: 'smoke-model', CLAWMANAGER_LLM_PROVIDER: 'custom' };
    if (offset) Object.assign(environment, { CLAWMANAGER_TEAM_ENABLED: 'true', CLAWMANAGER_TEAM_ID: '1', CLAWMANAGER_TEAM_MEMBER_ID: member, CLAWMANAGER_TEAM_ROLE: offset === 1 ? 'leader' : 'worker', CLAWMANAGER_TEAM_REDIS_URL: `redis://${names.redis}:6379/0`, CLAWMANAGER_TEAM_SHARED_DIR: '/workspaces/teams/user-77/team-1-shared', CLAWMANAGER_TEAM_SHARED_GID: '1000' });
    await control('/v1/gateways', { instance_id: iid, user_id: 77, agent_type: 'hermes', workspace_path: workspace, gateway_port: port, uid: 200000 + iid, gid: 1000, generation: 1, environment });
    await until(() => sql(`SELECT status FROM instances WHERE id=${iid}`) === 'running', `actual Agent instance ${iid} running`);
  }
  browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1600, height: 1000 } });
  await context.addInitScript(token => {
    localStorage.setItem('access_token', token); localStorage.setItem('clawmanager_locale', 'zh');
    window.hermesAcceptanceMessages = [];
    window.addEventListener('message', event => {
      if (event.origin === location.origin && event.data?.type?.startsWith('clawmanager:hermes-desktop:')) window.hermesAcceptanceMessages.push(event.data);
    });
  }, jwt());
  const page = await context.newPage();
  activePage = page;
  const pageErrors = [], expectedDeniedErrors = [];
  let deniedPeerPhase = false;
  const collectError = error => {
    if (deniedPeerPhase && /^Error: Hermes 服务请求失败（HTTP 502）/.test(String(error))) expectedDeniedErrors.push(String(error));
    else pageErrors.push(String(error));
  };
  page.on('pageerror', collectError);
  for (const iid of ids) {
    await page.goto(`${publicURL}/instances/${iid}`);
    await page.waitForSelector('iframe[title*="Hermes Desktop Web"]:visible', { timeout: 60000 });
    await page.waitForFunction(iid => window.hermesAcceptanceMessages.some(e => e.type === 'clawmanager:hermes-desktop:ready' && e.instanceId === iid), iid, { timeout: 60000 });
    const result = await exerciseBrowser(page, iid);
    assert.equal(result.count, 2); assert.equal(result.streaming, true);
    fs.mkdirSync('reports', { recursive: true });
    await page.screenshot({ path: `reports/${prefix}-${iid}-ready.png` });
    console.log(`PASS actual CM instance ${iid}: capability/login/cookie/iframe/BFF HTTP/WS/prompt/durable history`);
  }
  assert.deepEqual(pageErrors, [], 'normal actual renderer errors before negative tests');
  // New distinct peer is refused before renewal, works after renewal; old peer
  // is refused after removal, without restarting Runtime or any Gateway.
  const appB = `${prefix}-app-b`;
  start(appB, appImage, ['-v', `${prefix}-workspaces:/workspaces`, '-p', '127.0.0.1::8443', ...envArgs({ ...common, DB_HOST: names.mysql, DB_USER: 'root', DB_PASSWORD: 'disposable-local-only', DB_NAME: 'clawmanager', JWT_SECRET: secrets.jwt, RUNTIME_NAMESPACE: namespace, RUNTIME_SCHEDULER_ENABLED: 'false', PLATFORM_REDIS_URL: `redis://${names.redis}:6379/0` })]);
  const urlB = `https://127.0.0.1:${publishedPort(appB, 8443)}`;
  await delay(5000);
  const pageB = await context.newPage();
  pageB.on('pageerror', collectError);
  activePage = pageB;
  await pageB.goto(`${urlB}/instances/9103`);
  await pageB.waitForFunction(() => /安全连接|secure Desktop Web connection/.test(document.body.innerText), undefined, { timeout: 60000 });
  assert.ok(!await pageB.locator('iframe').count(), 'untrusted peer unexpectedly activated');
  deniedPeerPhase = true;
  allowedPeers = [ip(appB)]; await renewPolicy();
  await pageB.reload(); await pageB.waitForSelector('iframe[title*="Hermes Desktop Web"]', { state: 'attached', timeout: 60000 });
  await exerciseBrowser(pageB, 9103);
  // A may still have a five-minute upstream auth cookie cache. The trust
  // boundary must reject its actual request, regardless of cached activation.
  const removedPeerStatus = await page.evaluate(async () => (await fetch('/api/v1/instances/9103/hermes-desktop/api/status')).status);
  assert.notEqual(removedPeerStatus, 200, 'removed App peer retained actual upstream access');
  allowedPeers = [ip(names.app)]; await renewPolicy();
  await pageB.close();
  activePage = page;
  await page.reload(); await page.waitForSelector('iframe[title*="Hermes Desktop Web"]', { state: 'attached', timeout: 60000 });
  await page.waitForFunction(() => window.hermesAcceptanceMessages.some(e => e.type === 'clawmanager:hermes-desktop:ready' && e.instanceId === 9103), undefined, { timeout: 60000 });
  deniedPeerPhase = false;
  await exerciseBrowser(page, 9103);
  console.log('PASS actual App A/B/A policy rotation; precise activation error; no Gateway restart');
  assert.deepEqual(pageErrors, []);
  assert.equal(renewalFailure, undefined, 'managed policy renewal failed during acceptance');
  const actual = sql('SELECT COUNT(*) FROM instances WHERE id IN (9101,9102,9103) AND status="running" AND runtime_generation=1');
  assert.equal(actual, '3');
  console.log('PASS combined acceptance; no external model, no live tenant mutation');
} catch (error) {
  // Diagnose the real upstream rather than suppressing/retrying a failed check.
  if (containers.includes(names.runtime)) {
    const diagnostics = `import http.cookiejar,json,urllib.request,urllib.error,sys
origin=sys.argv[1]; password=sys.argv[2]
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
for path,body in [('/auth/password-login',{'provider':'basic','username':'clawmanager','password':password}),('/api/model/info',None)]:
 req=urllib.request.Request('http://127.0.0.1:20000'+path,data=json.dumps(body).encode() if body else None,headers={'Origin':origin,'Content-Type':'application/json'})
 try:
  with opener.open(req,timeout=20) as r: print('UPSTREAM_DIAGNOSTIC',path,r.status)
 except urllib.error.HTTPError as e: print('UPSTREAM_DIAGNOSTIC',path,e.code)
 except Exception as e: print('UPSTREAM_DIAGNOSTIC',path,type(e).__name__)
`;
    try { console.log(docker(['exec', names.runtime, 'python3', '-c', diagnostics, `http://clawmanager-gateway.${namespace}.svc.cluster.local:9001`, secrets.instance + '9101'])); } catch { /* No raw secret-bearing output. */ }
  }
  if (activePage) {
    fs.mkdirSync('reports', { recursive: true });
    await activePage.screenshot({ path: `reports/${prefix}-failed.png`, fullPage: true });
    console.log('Local browser failure screenshot captured');
  }
  throw error;
} finally {
  clearInterval(renewTimer);
  await browser?.close();
  stubProcess?.kill();
  if (process.env.HERMES_KEEP_LOCAL_FIXTURE === 'true') console.log(`Local diagnostic fixture retained: ${prefix}`);
  for (const name of process.env.HERMES_KEEP_LOCAL_FIXTURE === 'true' ? [] : containers.reverse()) {
    if (docker(['inspect', '-f', '{{index .Config.Labels "clawmanager.hermes-access"}}', name]) !== prefix) throw new Error('fixture cleanup label mismatch');
    docker(['rm', '-f', '-v', name]);
  }
  if (process.env.HERMES_KEEP_LOCAL_FIXTURE !== 'true') try { docker(['network', 'rm', prefix]); } catch { /* Preserve evidence if a process still owns it. */ }
  if (volumeCreated && process.env.HERMES_KEEP_LOCAL_FIXTURE !== 'true') {
    assert.equal(docker(['volume', 'inspect', '-f', '{{index .Labels "clawmanager.hermes-access"}}', `${prefix}-workspaces`]), prefix);
    docker(['volume', 'rm', `${prefix}-workspaces`]);
  }
}
