import assert from 'node:assert/strict';
import path from 'node:path';
import fs from 'node:fs';
import os from 'node:os';
import readline from 'node:readline';
import { spawn } from 'node:child_process';
import { createRequire } from 'node:module';
import { chromium } from '@playwright/test';

const root = path.resolve(import.meta.dirname, '../frontend');
const presentationOnly = process.env.WORKBENCH_PRESENTATION_ONLY === '1';
const require = createRequire(path.join(root, 'package.json'));
const { createServer } = require('vite');
process.chdir(root);
const fixture = `
import React from 'react';import{createRoot}from'react-dom/client';import{BrowserRouter,Routes,Route,Link}from'react-router-dom';
import{QueryClient,QueryClientProvider}from'@tanstack/react-query';import{I18nProvider}from'/src/contexts/I18nContext.tsx';import{AuthProvider}from'/src/contexts/AuthContext.tsx';import{useAuthStore}from'/src/stores/authStore.ts';import Page from'/src/pages/instances/InstanceDetailPage.tsx';import'/src/index.css';
window.fixtureUser=id=>{sessionStorage.setItem('fixture-user',String(id));useAuthStore.setState({user:{id,username:'tester'+id,role:'user'},isAuthenticated:true,isLoading:false,fetchCurrentUser:async()=>{}})};window.fixtureUser(Number(sessionStorage.getItem('fixture-user')||7));
createRoot(document.getElementById('root')).render(React.createElement(React.StrictMode,null,React.createElement(QueryClientProvider,{client:new QueryClient({defaultOptions:{queries:{retry:false}}})},React.createElement(I18nProvider,null,React.createElement(AuthProvider,null,React.createElement(BrowserRouter,null,React.createElement('nav',null,...['/instances','/instances/42','/instances/43'].map(to=>React.createElement(Link,{key:to,to},to+' '))),React.createElement(Routes,null,React.createElement(Route,{path:'/instances',element:React.createElement('h1',null,'Instances fixture')}),React.createElement(Route,{path:'/instances/:id',element:React.createElement(Page)}))))))));
`;
const vite = await createServer({ root, server: { port: 0 }, plugins: [{
  name: 'workbench-persistence-fixture',
  resolveId(id) { if (id === '/persistence-fixture.js') return '\0persistence-fixture'; },
  load(id) { if (id === '\0persistence-fixture') return fixture; },
  configureServer(server) { server.middlewares.use(async (req, res, next) => {
    if (!/^\/instances(?:\/\d+)?$/.test(req.url?.split('?')[0] ?? '')) return next();
    res.setHeader('Content-Type', 'text/html');
    res.end(await server.transformIndexHtml(req.url, '<html><body><div id="root"></div><script type="module" src="/persistence-fixture.js"></script></body></html>'));
  }); },
}] });
let browser;
const workspace = fs.mkdtempSync(path.join(os.tmpdir(), 'cm-persist-pty-'));
const terminalSessions = new Map(), children = [], terminalReads = [];
let terminalCreates = 0;
function createTerminal(generation) {
  const child = spawn('python3', ['-u', path.resolve(import.meta.dirname, 'fixtures/workbench-pty.py')], { cwd: workspace, stdio: ['pipe', 'pipe', 'pipe'] });
  children.push(child);
  const info = { id: 'shell-' + ++terminalCreates, name: 'Original shell', generation, cwd: '', exited: false, created_at: new Date().toISOString() };
  const session = { info, child, output: Buffer.alloc(0) };
  terminalSessions.set(info.id, session);
  readline.createInterface({ input: child.stdout }).on('line', line => { const message = JSON.parse(line); if (message.data) session.output = Buffer.concat([session.output, Buffer.from(message.data, 'base64')]); if (message.exited) info.exited = true; });
  child.on('exit', () => { info.exited = true; });
  return info;
}
try {
  await vite.listen();
  const origin = 'http://127.0.0.1:' + vite.httpServer.address().port;
  browser = await chromium.launch({ channel: 'chrome', headless: true });
  const context = await browser.newContext({ viewport: { width: 1600, height: 1100 } });
  await context.addInitScript(() => { localStorage.setItem('clawmanager_locale', 'en'); localStorage.setItem('access_token', 'fixture-manager-credential'); });
  await context.addCookies([{ name: 'manager-session', value: 'fixture-manager-cookie', url: origin }]);
  const allocations = [], mutations = [];
  let sessionLists = 0, processLists = 0;
  let generation = 6, running = true;
  await context.route('https://native-persistence.invalid/**', route => route.fulfill({ headers: { 'Access-Control-Allow-Origin': '*' }, contentType: 'text/html', body: '<h1>Native service</h1>' }));
  await context.route('https://app-persistence.invalid/**', route => {
    assert.equal(route.request().headers().authorization, undefined);
    assert.equal(route.request().headers().cookie, undefined);
    return route.fulfill({ headers: { 'Access-Control-Allow-Origin': '*' }, contentType: 'text/html', body: '<h1>Fresh restored app</h1>' });
  });
  await context.route(/http:\/\/(?:localhost|127\.0\.0\.1):3000\//, () => assert.fail('Raw loopback address must never become an iframe navigation'));
  await context.route('**/api/v1/**', async route => {
    const request = route.request(), pathname = new URL(request.url()).pathname;
    const id = Number(pathname.match(/instances\/(\d+)/)?.[1] ?? 42);
    let data = [];
    if (request.method() !== 'GET') mutations.push(pathname);
    if (/\/instances\/\d+$/.test(pathname)) data = { instance: { id, user_id: 7, name: 'Persistence ' + id, type: 'opencode', instance_mode: 'lite', runtime_type: 'gateway', status: running ? 'running' : 'stopped', runtime_generation: generation, workspace_path: '/workspace', created_at: new Date().toISOString() } };
    else if (pathname.endsWith('/status')) data = { instance_status: { instance_id: id, status: running ? 'running' : 'stopped', availability: running ? 'available' : 'unavailable' } };
    else if (pathname.endsWith('/access')) data = { access_url: 'https://native-persistence.invalid/', expires_at: new Date(Date.now() + 3600000).toISOString() };
    else if (pathname.endsWith('/runtime')) data = { commands: [], skills: [] };
    else if (pathname.endsWith('/capabilities')) data = { generation, workspace: '/workspace', git: true, terminal: true, preview: true, processes: true };
    else if (pathname.endsWith('/workspace/files')) {
      const current = new URL(request.url()).searchParams.get('path') || '';
      data = { entries: [{ name: current === 'starter' ? 'nested' : 'leaf.txt', path: current === 'starter' ? 'starter/nested' : current + '/leaf.txt', is_dir: current === 'starter', size: 12, modified_at: new Date().toISOString(), previewable: true, downloadable: true }] };
    }
    else if (pathname.endsWith('/workbench/git')) {
      const operation = request.postDataJSON().operation;
      if (operation === 'projects') data = { projects: [{ path: 'first', name: 'First' }, { path: 'second', name: 'Second' }] };
      else if (operation === 'status') data = { status: { branch: 'main', detached: false, ahead: 0, behind: 0, changes: [{ path: 'main.ts', index_status: 'M', worktree_status: '', staged: true, untracked: false, binary: false, stats_known: true, staged_additions: 1, staged_deletions: 1 }] } };
      else if (operation === 'branches') data = { branches: [{ name: 'main', current: true }] };
      else if (operation === 'history') data = { history: [] };
      else if (operation === 'diff') data = { diff: '--- a/main.ts\n+++ b/main.ts\n@@ -1 +1 @@\n-old\n+restored diff body\n' };
    }
    else if (pathname.endsWith('/workbench/terminals')) { if (request.method() === 'GET') sessionLists++; data = request.method() === 'POST' ? createTerminal(generation) : { sessions: [...terminalSessions.values()].map(session => session.info) }; }
    else if (pathname.endsWith('/workbench/processes')) { processLists++; data = { processes: [{ id: 'retained-process', name: 'Retained process', command: 'fixture', state: 'running', can_stop: false, can_restart: false }] }; }
    else if (/\/terminals\/[^/]+\/(?:output|input|resize)$/.test(pathname)) {
      const [, sessionId, operation] = pathname.match(/\/terminals\/([^/]+)\/(output|input|resize)$/);
      const session = terminalSessions.get(sessionId);
      assert(session, 'Only an actual original session can receive terminal operations');
      if (operation === 'output') {
        const cursor = Number(new URL(request.url()).searchParams.get('cursor') || 0);
        terminalReads.push({ id: sessionId, cursor });
        data = { data: session.output.subarray(cursor).toString('base64'), cursor: session.output.length, exited: session.info.exited, truncated: false };
      } else { session.child.stdin.write(JSON.stringify(request.postDataJSON()) + '\n'); data = {}; }
    }
    else if (pathname.endsWith('/session-usage')) data = { summary: { session_count: 0, total_tokens: 0, total_prompt_tokens: 0, total_completion_tokens: 0, total_estimated_cost: 0, currency: 'USD' }, compliance: { fallback_session_count: 0, has_fallback_sessions: false, recent_fallback_audit_count: 0 }, items: [], total: 0, page: 1, limit: 20 };
    else if (pathname.endsWith('/previews')) {
      assert.equal(request.method(), 'POST');
      allocations.push({ id, ...request.postDataJSON() });
      data = { generation, url: 'https://app-persistence.invalid/bootstrap?capability=fresh-' + allocations.length, viewer_url: origin + '/viewer#private-viewer' };
    }
    return route.fulfill({ json: { success: true, data } });
  });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  await page.routeWebSocket(/\/api\/v1\/ws(?:\?|$)/, () => {});
  const nav = async target => {
    await page.locator('nav').first().getByRole('link', { name: target, exact: true }).click();
    const instance = target.match(/\/instances\/(\d+)$/)?.[1];
    await page.getByRole('heading', { name: instance ? 'Persistence ' + instance : 'Instances fixture', exact: true }).waitFor();
  };
  const newBrowser = async address => {
    const wb = page.locator('.wb-root');
    await wb.locator('.wb-panels').getByRole('tab', { name: 'Preview & terminals', exact: true }).click();
    await wb.getByRole('button', { name: 'New tab', exact: true }).click();
    await wb.getByRole('button', { name: 'Browser', exact: true }).click();
    const input = wb.locator('.wb-live-view:not([hidden]) .wb-address input');
    await input.fill(address); await input.press('Enter');
    await page.frameLocator('.wb-live-view:not([hidden]) .wb-preview-frame').getByRole('heading', { name: 'Fresh restored app' }).waitFor();
  };
  await page.goto(origin + '/instances/42');
  if (presentationOnly) {
    const wb = page.locator('.wb-root');
    await wb.locator('.wb-panels').getByRole('tab', { name: 'Preview & terminals', exact: true }).click();
    await wb.getByRole('button', { name: 'Sessions & processes', exact: true }).click();
    await wb.locator('.wb-session-drawer').getByText('Retained process', { exact: true }).waitFor();
    await wb.getByRole('button', { name: 'Expand workspace', exact: true }).click();
    await page.locator('.wb-root.wb-fullscreen').waitFor();
    const beforeSessions = sessionLists, beforeProcesses = processLists;
    await page.goto(origin + '/instances'); await nav('/instances/42');
    await page.locator('.wb-root.wb-fullscreen .wb-session-drawer').getByText('Retained process', { exact: true }).waitFor();
    assert(sessionLists > beforeSessions && processLists > beforeProcesses, 'A restored open drawer must fetch current backend rows');
    await page.reload();
    await page.locator('.wb-root.wb-fullscreen .wb-session-drawer').getByText('Retained process', { exact: true }).waitFor();
    await wb.getByRole('button', { name: 'Exit full screen', exact: true }).focus();
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => !document.querySelector('.wb-fullscreen') && !document.querySelector('.wb-session-drawer'));
    await page.goto(origin + '/instances'); await nav('/instances/42');
    assert.equal(await page.locator('.wb-fullscreen, .wb-session-drawer').count(), 0, 'Explicit dismissal must also persist');
    assert.equal(terminalCreates, 0);
    assert.equal(allocations.length, 0);
    console.log('PASS presentation: internal expanded workspace and Sessions/processes drawer persist on reentry/reload; restored drawer fetches fresh rows, dismissal persists, no terminal/service creation');
  } else {
  await newBrowser('http://localhost:3000/one?view=clock#/settings');
  await newBrowser('http://localhost:3000/two');
  const firstTab = page.locator('.wb-program-tab').first().getByRole('tab');
  await firstTab.click();
  assert.equal(allocations.length, 2);
  await nav('/instances'); await page.getByRole('heading', { name: 'Instances fixture' }).waitFor(); await nav('/instances/42');
  await page.frameLocator('.wb-live-view:not([hidden]) .wb-preview-frame').getByRole('heading', { name: 'Fresh restored app' }).waitFor();
  assert.equal(await page.locator('.wb-program-tab').count(), 2, 'Leaving and reentering must restore both browser tabs');
  assert.equal(await page.locator('.wb-live-view:not([hidden]) .wb-address input').inputValue(), 'http://localhost:3000/one?view=clock#/settings');
  assert.equal(allocations.length, 4, 'StrictMode restores each capability once');
  await page.reload();
  await page.frameLocator('.wb-live-view:not([hidden]) .wb-preview-frame').getByRole('heading', { name: 'Fresh restored app' }).waitFor();
  assert.equal(allocations.length, 6, 'Reload gets fresh capabilities instead of serialized URLs');
  assert.equal(await page.locator('.wb-program-tab').count(), 2);
  const durable = await page.evaluate(() => Object.entries(localStorage).filter(([key]) => key.startsWith('cm:workbench:')));
  assert.equal(durable.length, 1);
  assert(!JSON.stringify(durable).includes('fresh-'));
  assert(!JSON.stringify(durable).includes('private-viewer'));
  assert(!JSON.stringify(durable).includes('fixture-manager'));
  await page.locator('.wb-program-tab').nth(1).locator('.wb-close-tab').click();
  await page.reload(); await page.locator('.wb-preview-frame').first().waitFor();
  assert.equal(await page.locator('.wb-program-tab').count(), 1, 'A closed tab remains hidden after reload');
  assert.equal(allocations.length, 7, 'Hidden restored tabs must not allocate capabilities');
  await nav('/instances/43'); await page.getByRole('heading', { name: 'Persistence 43', exact: true }).waitFor(); await page.locator('.wb-root').waitFor();
  assert.equal(await page.locator('.wb-program-tab').count(), 0, 'Another instance must start with its own view');
  await newBrowser('http://localhost:3000/instance43');
  await nav('/instances/42'); await page.locator('.wb-preview-frame').first().waitFor();
  assert.equal(await page.locator('.wb-program-tab').count(), 1);
  const beforeUserSwitch = allocations.length;
  await page.evaluate(() => window.fixtureUser(8));
  await page.waitForFunction(() => !document.querySelector('.wb-program-tab'));
  assert.equal(allocations.length, beforeUserSwitch, 'An account change must not restore another account’s previews');
  await newBrowser('http://localhost:3000/user8');
  await page.evaluate(() => window.fixtureUser(7));
  await page.waitForFunction(() => document.querySelector('.wb-address input')?.value.includes('/one'));
  await page.locator('.wb-preview-frame').first().waitFor();
  const wb = page.locator('.wb-root');
  await wb.getByRole('button', { name: 'New tab', exact: true }).click();
  await wb.getByRole('button', { name: 'Terminal', exact: true }).click();
  const input = () => page.locator('.wb-live-view:not([hidden]) .xterm-helper-textarea');
  const send = async command => { await input().focus(); await page.keyboard.type(command); await page.keyboard.press('Enter'); };
  await input().waitFor();
  await send('CM_PERSIST=kept; printf "SHELL_ID:%s:%s\\nTTY_PRIVATE_MARKER\\n" "$$" "$CM_PERSIST"');
  await page.waitForFunction(() => document.querySelector('.wb-live-view:not([hidden]) .xterm-accessibility-tree')?.textContent.includes('TTY_PRIVATE_MARKER'));
  const original = terminalSessions.get('shell-1');
  const shellPid = original.output.toString().match(/SHELL_ID:(\d+):kept/)[1];
  await nav('/instances'); await nav('/instances/42');
  await input().waitFor();
  await page.waitForFunction(() => document.querySelector('.wb-live-view:not([hidden]) .xterm-accessibility-tree')?.textContent.includes('TTY_PRIVATE_MARKER'));
  await send('printf "REATTACHED:%s:%s\\n" "$$" "$CM_PERSIST"');
  await page.waitForFunction(() => document.querySelector('.wb-live-view:not([hidden]) .xterm-accessibility-tree')?.textContent.includes('REATTACHED:'));
  assert(original.output.toString().includes('REATTACHED:' + shellPid + ':kept'), 'Reentry must join the same shell PID and variables');
  assert.equal(terminalCreates, 1);
  await page.reload(); await input().waitFor();
  await page.waitForFunction(() => document.querySelector('.wb-live-view:not([hidden]) .xterm-accessibility-tree')?.textContent.includes('TTY_PRIVATE_MARKER'));
  await send('printf "RELOADED:%s:%s\\n" "$$" "$CM_PERSIST"');
  await page.waitForFunction(() => document.querySelector('.wb-live-view:not([hidden]) .xterm-accessibility-tree')?.textContent.includes('RELOADED:'));
  assert(original.output.toString().includes('RELOADED:' + shellPid + ':kept'));
  assert(terminalReads.filter(read => read.id === 'shell-1' && read.cursor === 0).length >= 3, 'Each fresh xterm must replay retained backend output from cursor zero');
  assert.equal(terminalCreates, 1);
  assert(!(await page.evaluate(() => localStorage.getItem('cm:workbench:v1:7:42'))).includes('TTY_PRIVATE_MARKER'), 'TTY contents must stay in backend replay, never browser storage');
  await wb.locator('.wb-panels').getByRole('tab', { name: 'Git projects', exact: true }).click();
  await wb.locator('.wb-project-bar select').selectOption('second');
  await wb.locator('.wb-file-choice').first().click();
  await wb.locator('.wb-diff').getByText('+restored diff body', { exact: true }).waitFor();
  await wb.getByRole('textbox', { name: 'Commit message', exact: true }).fill('Retained commit draft');
  await nav('/instances'); await nav('/instances/42');
  await wb.locator('.wb-diff').getByText('+restored diff body', { exact: true }).waitFor();
  assert.equal(await wb.locator('.wb-project-bar select').inputValue(), 'second');
  assert.equal(await wb.getByRole('textbox', { name: 'Commit message', exact: true }).inputValue(), 'Retained commit draft');
  await page.reload();
  await wb.locator('.wb-diff').getByText('+restored diff body', { exact: true }).waitFor();
  assert.equal(await wb.locator('.wb-project-bar select').inputValue(), 'second');
  assert.equal(await wb.getByRole('textbox', { name: 'Commit message', exact: true }).inputValue(), 'Retained commit draft');
  await wb.locator('.wb-panels').getByRole('tab', { name: 'Files', exact: true }).click();
  await wb.locator('.wb-files').getByRole('button', { name: 'nested', exact: true }).click();
  await wb.locator('.wb-files').getByRole('button', { name: 'leaf.txt', exact: true }).waitFor();
  await nav('/instances'); await nav('/instances/42');
  await wb.locator('.wb-files').getByRole('button', { name: 'leaf.txt', exact: true }).waitFor();
  assert.equal(await wb.locator('.wb-panels').getByRole('tab', { name: 'Files', exact: true }).getAttribute('aria-selected'), 'true');
  await page.reload();
  await wb.locator('.wb-files').getByRole('button', { name: 'leaf.txt', exact: true }).waitFor();
  assert.equal(await wb.locator('.wb-panels').getByRole('tab', { name: 'Files', exact: true }).getAttribute('aria-selected'), 'true');
  await wb.locator('.wb-panels').getByRole('tab', { name: 'Preview & terminals', exact: true }).click();
  await send('exit');
  await page.locator('.wb-live-view:not([hidden]) .wb-terminal-message').getByText('Session ended', { exact: true }).waitFor();
  await page.reload();
  await page.locator('.wb-live-view:not([hidden]) .wb-terminal-message').getByText('Session ended', { exact: true }).waitFor();
  await page.waitForFunction(() => document.querySelector('.wb-live-view:not([hidden]) .xterm-accessibility-tree')?.textContent.includes('TTY_PRIVATE_MARKER'));
  terminalSessions.delete('shell-1'); await page.reload();
  await page.locator('.wb-live-view:not([hidden])').getByText('Session ended', { exact: true }).waitFor();
  assert.equal(terminalCreates, 1, 'Ended or pruned shells must retain ended tabs without creating replacements');
  const beforeGeneration = allocations.length;
  generation = 7; await page.reload(); await page.locator('.wb-root').waitFor();
  assert.equal(await page.locator('.wb-address input').first().inputValue(), 'http://localhost:3000/one?view=clock#/settings');
  assert.equal(await page.locator('.wb-preview-frame').count(), 0, 'A new generation keeps the address but fences automatic replay');
  assert.equal(allocations.length, beforeGeneration);
  running = false; await page.reload(); await page.locator('.wb-root').waitFor();
  assert.equal(await page.locator('.wb-preview-frame').count(), 0);
  assert.equal(allocations.length, beforeGeneration);
  assert(!mutations.some(path => /\/(?:start|stop|restart)(?:\/|$)/.test(path)), 'Reentry must never start/restart services');
  assert.equal(mutations.filter(path => path.endsWith('/workbench/terminals')).length, 1, 'Only the explicit New terminal click may create a shell');
  assert.equal(context.pages().length, 1);
  console.log('PASS actual InstanceDetailPage: browser state/fresh capabilities; original real PTY PID, variables and replay after reentry/reload with one explicit creation; ended/pruned sessions stay ended; Files folder and Git repository/diff/draft restore on reentry/reload; scoped isolation and generation/stopped fences');
  }
} catch (error) { console.error(error); throw error; }
finally { await browser?.close(); for (const child of children) child.stdin.end(); vite.httpServer?.closeAllConnections(); await vite.close(); fs.rmSync(workspace, { recursive: true, force: true }); }
