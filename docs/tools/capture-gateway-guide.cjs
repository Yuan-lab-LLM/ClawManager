// Read-only documentation capture. No model calls, settings saves or instance actions.
const fs = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const root = path.resolve(__dirname, '../..');
const modulePath = process.env.GUIDE_PLAYWRIGHT_PATH || path.join(root, 'e2e/node_modules/playwright/package.json');
const { chromium } = createRequire(modulePath)('playwright');
const base = process.env.GUIDE_BASE_URL || 'https://localhost:30443';
if (!['localhost', '127.0.0.1'].includes(new URL(base).hostname)) throw new Error('This capture script is restricted to the local application.');
const out = path.join(root, 'docs/AIGateway/workflow-20261014');
const mode = process.argv[2] || 'inspect';
const routes = [
  ['01-gateway-overview', '/admin/ai-gateway'],
  ['02-model-catalog', '/admin/models'],
  ['04-routing-settings', '/admin/settings'],
  ['05-instances', '/instances'],
  ['06-create-instance', '/instances/new'],
  ['08-session-usage', '/admin/session-usage'],
  ['09-audit', '/admin/ai-audit'],
  ['10-costs', '/admin/costs'],
  ['11-risk-rules', '/admin/risk-rules'],
];
async function main() {
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  try {
    if (mode === 'diagrams') {
      const page = await browser.newPage({ viewport: { width: 1200, height: 1060 }, deviceScaleFactor: 1 });
      for (const name of ['flow-01-user-journey', 'flow-02-auto-routing', 'flow-03-governance']) {
        await page.goto(require('node:url').pathToFileURL(path.join(out, `${name}.svg`)).href);
        await page.locator('svg').screenshot({ path: path.join(out, `${name}.png`) });
        console.log(`Rendered ${name}`);
      }
      return;
    }
    const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1600, height: 1040 }, deviceScaleFactor: 1 });
    const fixture = fs.readFileSync(path.join(root, 'e2e/fixtures/users.ts'), 'utf8');
    const admin = fixture.match(/admin:\s*\{\s*username:\s*"([^"]+)"\s*,\s*password:\s*"([^"]+)"/);
    const username = process.env.GUIDE_USERNAME || admin?.[1];
    const password = process.env.GUIDE_PASSWORD || admin?.[2];
    if (!username || !password) throw new Error('Provide GUIDE_USERNAME and GUIDE_PASSWORD.');
    const login = await context.request.post(`${base}/api/v1/auth/login`, { data: { username, password } });
    if (!login.ok()) throw new Error(`Login failed (${login.status()}); no further attempts made.`);
    const auth = (await login.json()).data;
    if (!auth?.access_token) throw new Error('Login did not return a token.');
    await context.addInitScript(({ token, refresh }) => {
      localStorage.setItem('access_token', token);
      if (refresh) localStorage.setItem('refresh_token', refresh);
      localStorage.setItem('clawmanager_locale', 'zh');
    }, { token: auth.access_token, refresh: auth.refresh_token });
    await context.route('**/*', async route => {
      const request = route.request();
      if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) return route.abort('blockedbyclient');
      return route.continue();
    });
    const page = await context.newPage();
    for (const url of ['/api/v1/admin/models', '/api/v1/system-settings/routing-classifier', '/api/v1/instances']) {
      const response = await context.request.get(base + url, { headers: { Authorization: `Bearer ${auth.access_token}` } });
      let data;
      try { data = (await response.json()).data; } catch {}
      console.log(JSON.stringify({ endpoint: url, status: response.status(), count: Array.isArray(data) ? data.length : Array.isArray(data?.items) ? data.items.length : undefined }));
    }
    for (const [name, url] of routes.filter(([name]) => mode !== 'supplement' || ['04-routing-settings', '06-create-instance'].includes(name))) {
      await page.goto(base + url, { waitUntil: 'domcontentloaded', timeout: 30000 });
      await page.waitForTimeout(1600);
      console.log(JSON.stringify({ name, path: new URL(page.url()).pathname,
        headings: await page.locator('h1,h2,h3').allTextContents(),
        buttons: (await page.getByRole('button').allTextContents()).map(t => t.trim()).filter(Boolean).slice(0, 32),
        instancePaths: await page.locator('a[href^="/instances/"]').evaluateAll(nodes => nodes.map(n => n.getAttribute('href'))),
      }));
      if (mode === 'capture' || mode === 'supplement') await page.screenshot({ path: path.join(out, `${name}.png`), fullPage: false,
        mask: [page.locator('input[type="password"]'), page.locator('iframe')], maskColor: '#dbe3ec' });
      if (name === '04-routing-settings' && ['capture', 'supplement'].includes(mode)) {
        const section = page.locator('section[aria-labelledby="routing-classifier-title"]:visible');
        await section.screenshot({ path: path.join(out, '04-routing-settings.png'),
          mask: [section.locator('input[type="password"]')], maskColor: '#dbe3ec' });
        await section.locator('select').selectOption('hybrid');
        await section.screenshot({ path: path.join(out, '04b-hybrid-unsaved.png'),
          mask: [section.locator('input[type="password"]')], maskColor: '#dbe3ec' });
      }
      if (name === '02-model-catalog') {
        await page.getByRole('button', { name: /^(Add Model|添加模型|新增模型)$/ }).click();
        await page.waitForTimeout(400);
        console.log(JSON.stringify({ name: '03-model-form', headings: await page.locator('h1,h2,h3').allTextContents(), labels: await page.locator('label').allTextContents() }));
        if (mode === 'capture') await page.screenshot({ path: path.join(out, '03-model-form.png'), fullPage: true,
          mask: [page.locator('input[type="password"]')], maskColor: '#dbe3ec' });
      }
    }
  } finally { await browser.close(); }
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
