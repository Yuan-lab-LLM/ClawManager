// Real routes with isolated APIs: list updates and owner embedding never mutate live data.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { chromium } from '@playwright/test';
const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
await page.addInitScript(() => { localStorage.setItem('access_token', 'fixture'); localStorage.setItem('clawmanager_locale', 'en'); });
page.setDefaultTimeout(15000);
const now = new Date().toISOString();
let teams = ['running', 'creating', 'failed'].map((status, i) => ({ id: 144 + i, name: i ? `Team ${i}` : 'Research and source verification '.repeat(10), status, communication_mode: 'leader_mediated', shared_mount_path: '/team', created_at: now, updated_at: now }));
let failList = false;
let deleteCalls = 0;
const requests = [];
await page.route('**/api/v1/**', async route => {
  const request = route.request(), url = new URL(request.url()); requests.push(url.pathname);
  if (request.method() === 'DELETE') { deleteCalls++; return route.fulfill({ status: 503, json: { error: 'fixture delete failure' } }); }
  if (url.pathname === '/api/v1/teams' && failList) return route.fulfill({ status: 503, json: { error: 'fixture list failure' } });
  const value = url.pathname.endsWith('/auth/me') ? { id: 1, username: 'fixture-user', role: 'user', status: 'active' }
    : url.pathname.endsWith('/session') ? { owner: 'owner@example.com' }
    : url.pathname === '/api/v1/teams' ? { teams, total: teams.length }
    : url.pathname.endsWith('/teams/144') ? { team: teams[0] || { id: 144, name: 'Owner team', status: 'running', shared_mount_path: '/team' }, members: [], tasks: [], events: [], work_items: [] } : {};
  return route.fulfill({ json: { data: value } });
});
const origin = process.env.FRONTEND_URL || 'http://127.0.0.1:9002';
const update = async event => { await Promise.all([page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/teams'), page.evaluate(type => window.dispatchEvent(new Event(type)), event)]); };
const shot = async name => { if (process.env.MANAGEMENT_TEAM_ARTIFACTS) { await fs.mkdir(process.env.MANAGEMENT_TEAM_ARTIFACTS, { recursive: true }); await page.screenshot({ path: path.join(process.env.MANAGEMENT_TEAM_ARTIFACTS, `${name}.png`) }); } };
try {
  await page.goto(`${origin}/teams`);
  const table = page.getByRole('table');
  await table.getByText('Creating', { exact: true }).waitFor();
  assert.equal(await page.getByRole('textbox', { name: 'Search teams' }).isVisible(), false);
  assert.equal(await page.getByRole('button', { name: 'Refresh', exact: true }).count(), 0);
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    assert(await page.getByRole('link', { name: 'Create team', exact: true }).isVisible());
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `list overflow ${width}`);
    assert(await table.locator('td').evaluateAll(cells => cells.every(cell => getComputedStyle(cell).whiteSpace === 'nowrap')));
    assert.equal(await table.getByRole('link', { name: teams[0].name, exact: true }).getAttribute('title'), teams[0].name);
    await shot(`list-${width}`);
  }
  await page.getByRole('button', { name: 'Search teams', exact: true }).click();
  const search = page.getByRole('textbox', { name: 'Search teams' });
  await search.fill('146');
  assert.equal(await table.locator('tbody tr').count(), 1);
  assert(await table.getByText('Failed', { exact: true }).isVisible());
  await search.fill('no-match');
  await page.getByText('No matching teams', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'Clear search', exact: true }).click();
  await search.press('Escape');
  assert(await page.getByRole('button', { name: 'Search teams', exact: true }).evaluate(el => document.activeElement === el));
  teams[1].status = 'running';
  await update('focus');
  assert.equal(await table.getByText('Creating', { exact: true }).count(), 0);
  failList = true; await update('online');
  await page.getByRole('alert').getByText('fixture list failure', { exact: false }).waitFor();
  assert.equal(await table.locator('tbody tr').count(), 3, 'list errors retain loaded team entries');
  await shot('list-error');
  failList = false; teams = []; await update('online');
  await page.getByText('No teams yet', { exact: true }).waitFor();
  await shot('list-empty');
  // Owner route reuses its own environment/Layout and authenticated API namespace.
  await page.goto(`${origin}/ieisystem/teams/144`);
  const composer = page.getByRole('textbox', { name: 'Task content' });
  await composer.waitFor();
  assert.equal(await page.locator('.cm-management-shell').count(), 0, 'owner embedding has no duplicate outer navigation');
  assert.equal(await page.locator('.ot-northbound-header').count(), 1);
  assert(requests.includes('/api/v1/ieisystem/session'));
  assert(requests.includes('/api/v1/ieisystem/teams/144'));
  await composer.fill('owner draft');
  const actions = page.getByRole('button', { name: 'Team actions', exact: true });
  await actions.click();
  const panel = page.getByRole('region', { name: 'Team actions' });
  const back = new URL(await panel.getByRole('link', { name: 'Back to teams' }).getAttribute('href'), origin);
  assert.equal(back.searchParams.get('selected_team_id'), '144');
  assert.equal(back.searchParams.get('workspace_tab'), 'teams');
  await panel.getByRole('button', { name: 'Delete team', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete team', exact: true });
  await dialog.waitFor();
  assert.match(await dialog.innerText(), /permanently/);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal(deleteCalls, 0);
  assert(await panel.getByRole('button', { name: 'Delete team' }).evaluate(el => el === document.activeElement));
  await panel.getByRole('button', { name: 'Delete team', exact: true }).click();
  await dialog.getByRole('button', { name: 'Delete team', exact: true }).click();
  await dialog.getByText('fixture delete failure', { exact: false }).waitFor();
  assert.equal(deleteCalls, 1);
  assert.equal(await composer.inputValue(), 'owner draft');
  await page.keyboard.press('Escape');
  await shot('owner-390');
  console.log('PASS team list search, responsive semantic table, focus/online updates, retained errors/empty, owner API/Layout/return path, styled delete failure and draft');
} finally { await browser.close(); }
