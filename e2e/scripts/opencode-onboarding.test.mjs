const {chromium} = await import(process.env.CLAW_MANAGER_PLAYWRIGHT_MODULE || '@playwright/test');
import fs from 'node:fs/promises';
import http from 'node:http';
import assert from 'node:assert/strict';

const storageTemplate = await fs.readFile(
  new URL('../../backend/internal/services/opencode_browser_storage.js', import.meta.url),
  'utf8',
);
const onboarding = await fs.readFile(
  new URL('../../backend/internal/services/opencode_onboarding.js', import.meta.url),
  'utf8',
);
const createdProjects = [];

const server = http.createServer((req, res) => {
  const pathname = new URL(req.url, 'http://127.0.0.1').pathname;
  if (req.method === 'POST' && pathname === '/__clawmanager/onboarding/project-directory') {
    if (req.headers['x-clawmanager-onboarding'] !== '1' || !String(req.headers['content-type']).startsWith('application/json')) {
      res.writeHead(403, {'Content-Type': 'application/json'});
      res.end(JSON.stringify({error: 'invalid onboarding request'}));
      return;
    }
    let body = '';
    req.setEncoding('utf8');
    req.on('data', (chunk) => { body += chunk; });
    req.on('end', () => {
      const name = JSON.parse(body).name;
      if (name === 'temporarily-unavailable') {
        res.writeHead(409, {'Content-Type': 'application/json'});
        res.end(JSON.stringify({code: 'WORKSPACE_UPGRADING', error: 'workspace is temporarily unavailable'}));
        return;
      }
      if (createdProjects.includes(name)) {
        res.writeHead(409, {'Content-Type': 'application/json'});
        res.end(JSON.stringify({code: 'PROJECT_EXISTS', error: 'project folder already exists'}));
        return;
      }
      createdProjects.push(name);
      res.writeHead(201, {'Content-Type': 'application/json'});
      res.end(JSON.stringify({data: {name, path: name}}));
    });
    return;
  }
  const scope = pathname.slice(1) || 'instance-a';
  const storage = storageTemplate.replace('__CLAWMANAGER_OPENCODE_SCOPE__', JSON.stringify(scope));
  const onboardingScript = onboarding.replace(
    '__CLAWMANAGER_OPENCODE_CAN_CREATE__',
    scope === 'read-only' ? 'false' : 'true',
  );
  res.setHeader('Content-Type', 'text/html');
  res.setHeader('Cache-Control', 'no-store');
  res.end(
    '<!doctype html><html><head><script>' + storage + '</script><script>' + onboardingScript +
    '</script></head><body><main id="opencode-app">OpenCode</main>' +
    '<button id="existing-focus" type="button">Existing action</button>' +
    '<button id="add-project" type="button" aria-label="Add project">+</button>' +
    '<button id="new-session" type="button" aria-label="New session">+</button>' +
    '<script>document.querySelector("#existing-focus").focus()</script></body></html>',
  );
});

await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
const port = server.address().port;
const browser = await chromium.launch({
  headless: true,
  args: ['--no-proxy-server'],
  ...(process.env.CLAW_MANAGER_CHROME_EXECUTABLE
    ? {executablePath: process.env.CLAW_MANAGER_CHROME_EXECUTABLE}
    : {}),
});

try {
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));

  const go = (scope) => page.goto('http://127.0.0.1:' + port + '/' + scope);
  const panel = () => page.locator('#clawmanager-opencode-onboarding-root .cm-oc-guide');
  const help = () => page.locator('#clawmanager-opencode-onboarding-root .cm-oc-help');

  await go('instance-a');
  await panel().waitFor({state: 'visible'});
  assert.equal(await panel().getAttribute('role'), 'dialog');
  assert.match(await panel().innerText(), /创建或选择文件夹/);
  assert.match(await panel().innerText(), /添加并选中项目/);
  assert.match(await panel().innerText(), /创建新会话/);
  assert.equal(await help().isVisible(), false);
  assert.equal(
    await page.evaluate(() => document.activeElement?.id),
    'existing-focus',
  );

  await panel().locator('[data-target="project"]').click();
  assert.equal(await page.locator('#clawmanager-opencode-onboarding-root .cm-oc-spotlight').isVisible(), true);
  await page.locator('#existing-focus').click();
  assert.equal(await page.locator('#clawmanager-opencode-onboarding-root .cm-oc-spotlight').isVisible(), true);
  await page.locator('#add-project').click();
  assert.equal(await page.locator('#clawmanager-opencode-onboarding-root .cm-oc-spotlight').isVisible(), false);
  await panel().locator('[data-target="session"]').click();
  assert.equal(await page.locator('#clawmanager-opencode-onboarding-root .cm-oc-spotlight').isVisible(), true);
  await page.evaluate(() => document.querySelector('#new-session').remove());
  await panel().locator('[data-target="session"]').click();
  assert.equal(await page.locator('#clawmanager-opencode-onboarding-root .cm-oc-spotlight').isVisible(), false);
  assert.notEqual(await panel().locator('[data-status="session"]').innerText(), '');

  await panel().locator('.cm-oc-input').fill('temporarily-unavailable');
  await panel().locator('.cm-oc-create-button').click();
  await panel().locator('.cm-oc-create-status').filter({hasText: '暂不可用'}).waitFor({state: 'visible'});
  assert.equal(await panel().locator('.cm-oc-create-button').isEnabled(), true);
  assert.deepEqual(createdProjects, []);

  await panel().locator('.cm-oc-input').fill('my-project');
  await panel().locator('.cm-oc-create-button').click();
  await panel().locator('.cm-oc-create-status').filter({hasText: '已创建'}).waitFor({state: 'visible'});
  assert.deepEqual(createdProjects, ['my-project']);

  await panel().locator('.cm-oc-primary').click();
  assert.equal(await panel().isVisible(), false);
  assert.equal(await help().isVisible(), true);
  assert.equal(
    await page.evaluate(() => localStorage.getItem('clawmanager-opencode-onboarding-v1-dismissed')),
    'true',
  );

  await page.reload();
  await help().waitFor({state: 'visible'});
  assert.equal(await panel().isVisible(), false);
  await help().click();
  await panel().waitFor({state: 'visible'});
  await page.waitForFunction(() => document.querySelector('#clawmanager-opencode-onboarding-root').shadowRoot.activeElement?.className === 'cm-oc-close');
  assert.equal(
    await page.evaluate(() => document.querySelector('#clawmanager-opencode-onboarding-root').shadowRoot.activeElement?.className),
    'cm-oc-close',
  );

  await go('instance-b');
  await panel().waitFor({state: 'visible'});
  assert.equal(
    await page.evaluate(() => localStorage.getItem('clawmanager-opencode-onboarding-v1-dismissed')),
    null,
  );

  await go('instance-a');
  await help().waitFor({state: 'visible'});
  assert.equal(await panel().isVisible(), false);

  await go('read-only');
  await panel().waitFor({state: 'visible'});
  assert.equal(await panel().locator('.cm-oc-create').isVisible(), false);
  assert.match(await panel().locator('.cm-oc-create-copy').innerText(), /没有创建文件夹的权限/);
  assert.equal(errors.length, 0, errors.join('\n'));
  console.log('PASS: onboarding creates projects explicitly, preserves focus, reopens on demand, and isolates state and permissions.');
} finally {
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}
