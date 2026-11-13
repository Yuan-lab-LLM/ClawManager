// Exercise the actual owner hook/dashboard against isolated APIs, without Go or containers.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { chromium } from '@playwright/test';
import { createServer } from '../frontend/node_modules/vite/dist/node/index.js';
const frontend = path.resolve(import.meta.dirname, '../frontend');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'owner-refresh-'));
let server, browser;
const owner = 'refresh-fixture@example.com';
const fsURL = file => `/@fs/${path.join(frontend, file).replaceAll('\\', '/')}`;
fs.writeFileSync(path.join(temp, 'fixture.tsx'), `import React,{useState}from'react';import{createRoot}from'react-dom/client';import{BrowserRouter}from'react-router-dom';import{useOwnerTeams}from${JSON.stringify(fsURL('src/hooks/useOwnerTeams.ts'))};import{OwnerTeamDashboard}from${JSON.stringify(fsURL('src/components/teams/OwnerTeamWorkspace.tsx'))};function App(){const state=useOwnerTeams(${JSON.stringify(owner)});const[draft,setDraft]=useState('');return <><input aria-label="Unsent draft" value={draft} onChange={e=>setDraft(e.target.value)}/><OwnerTeamDashboard state={state}/></>};const root=globalThis.__ownerRefreshFixtureRoot??=createRoot(document.getElementById('root'));root.render(<BrowserRouter><App/></BrowserRouter>);`);
let listCalls = 0, terminal = false;
const now = new Date().toISOString();
const team = { id: 144, name: 'Owner refresh fixture', status: 'running', member_count: 0, available_count: 0, members: [], communication_mode: 'leader_mediated', shared_mount_path: '/team', created_at: now, updated_at: now };
try {
  server = await createServer({ root: frontend, configFile: path.join(frontend, 'vite.config.ts'), server: { host: '127.0.0.1', port: 0, fs: { allow: [frontend, temp] } }, plugins: [{ name: 'owner-refresh-fixture', configureServer(vite) { vite.middlewares.use('/__owner_refresh__', async (_req, res) => { res.setHeader('Content-Type', 'text/html'); res.end(await vite.transformIndexHtml('/__owner_refresh__', `<html><body><div id="root"></div><script type="module" src="/@fs/${path.join(temp, 'fixture.tsx')}"></script></body></html>`)); }); } }] });
  await server.listen();
  browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH });
  const page = await browser.newPage(); page.setDefaultTimeout(15000);
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(({ owner }) => { sessionStorage.setItem(`owner-team-operation:${owner}`, JSON.stringify({ operation_id: 'fixture-op', status: 'queued' })); }, { owner });
  await page.route('**/api/v1/ieisystem/**', async route => {
    const url = new URL(route.request().url());
    assert.equal(route.request().headers().authorization, undefined, 'owner API must retain cookie auth rather than a CM bearer token');
    if (url.pathname.endsWith('/teams')) listCalls++;
    const data = url.pathname.endsWith('/team-capabilities') ? { enabled: true, can_create: false }
      : url.pathname.endsWith('/teams') ? { teams: [team], total: 1 }
      : url.pathname.endsWith('/teams/144') ? { team, members: [], tasks: [], events: [], work_items: [] }
      : url.pathname.endsWith('/team-operations/fixture-op') ? { operation_id: 'fixture-op', status: terminal ? 'succeeded' : 'running' } : {};
    await route.fulfill({ json: { data } });
  });
  await page.clock.install();
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__owner_refresh__?selected_team_id=144&team_q=fixture`);
  await page.getByRole('link', { name: '进入团队对话' }).waitFor();
  assert.equal(await page.getByRole('button', { name: '刷新团队', exact: true }).count(), 0);
  const draft = page.getByRole('textbox', { name: 'Unsent draft' }); await draft.fill('retained owner draft');
  for (const type of ['focus', 'online']) {
    const before = listCalls;
    await Promise.all([page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/ieisystem/teams')), page.evaluate(type => window.dispatchEvent(new Event(type)), type)]);
    assert(listCalls > before, `${type} updates original owner list`);
    assert.equal(await draft.inputValue(), 'retained owner draft');
  }
  const before = listCalls; await page.clock.fastForward(3200);
  await page.waitForFunction(() => document.querySelector('.ot-master-item'));
  assert(listCalls > before, 'ongoing operation updates the owner list before its normal 10s interval');
  terminal = true; await page.clock.fastForward(1600);
  await page.getByText('操作已完成', { exact: true }).waitFor();
  assert.equal(await draft.inputValue(), 'retained owner draft');
  const search = new URL(page.url()).searchParams;
  assert.equal(search.get('selected_team_id'), '144'); assert.equal(search.get('team_q'), 'fixture');
  assert.deepEqual(errors, []);
  console.log('PASS owner focus/online/ongoing-operation refresh, existing query/selection/draft and owner auth namespace');
} finally { await browser?.close(); await server?.close(); fs.rmSync(temp, { recursive: true, force: true }); }
