import { chromium, request } from 'playwright';
import fs from 'node:fs/promises';
import path from 'node:path';

const baseURL = process.env.E2E_BASE_URL || 'https://localhost:30443';
const evidenceDir = process.env.E2E_EVIDENCE_DIR;
const adminUser = process.env.E2E_ADMIN_USER;
const adminPassword = process.env.E2E_ADMIN_PASSWORD;

if (!evidenceDir) {
  throw new Error('E2E_EVIDENCE_DIR is required');
}

const screenshotsDir = path.join(evidenceDir, 'screenshots');
await fs.mkdir(screenshotsDir, { recursive: true });

const result = {
  baseURL,
  startedAt: new Date().toISOString(),
  tool: 'playwright',
  credentials: {
    adminProvided: Boolean(adminUser && adminPassword),
    userProvided: false,
  },
  healthz: null,
  login: null,
  auth: null,
  adminRoutes: [],
  userRoutes: [],
  failures: [],
  blockers: [],
};

const excerpt = (text) => (text || '').replace(/\s+/g, ' ').trim().slice(0, 600);
const hasChinese = (text) => /[\u4e00-\u9fff]/.test(text || '');

async function collectPage(page, label, route) {
  const bodyText = await page.locator('body').innerText({ timeout: 7000 }).catch(() => '');
  const title = await page.title().catch(() => '');
  const htmlLang = await page.locator('html').getAttribute('lang').catch(() => null);
  const screenshot = path.join(screenshotsDir, `${label}.png`);
  await page.screenshot({ path: screenshot, fullPage: true }).catch(() => undefined);
  return {
    route,
    url: page.url(),
    title,
    htmlLang,
    screenshot: path.relative(evidenceDir, screenshot),
    bodyExcerpt: excerpt(bodyText),
    bodyHasChinese: hasChinese(bodyText),
    bodyHasGTManager: bodyText.includes('GTManager') || title.includes('GTManager'),
    bodyHasClawManager: bodyText.includes('ClawManager') || title.includes('ClawManager'),
    bodyHasOpenClaw: bodyText.includes('OpenClaw'),
    redirectedToLogin: page.url().includes('/login') && route !== '/login',
  };
}

const api = await request.newContext({ ignoreHTTPSErrors: true });
try {
  const healthResponse = await api.get(`${baseURL}/healthz`);
  result.healthz = {
    status: healthResponse.status(),
    ok: healthResponse.ok(),
    text: await healthResponse.text().catch(() => ''),
  };
} catch (error) {
  result.healthz = { error: error.message };
  result.failures.push(`/healthz request failed: ${error.message}`);
}

const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    locale: 'en-US',
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();
  const requestEvents = [];
  page.on('request', (req) => {
    const url = req.url();
    if (/favicon|lobster|openclaw|healthz/i.test(url)) {
      requestEvents.push({ type: 'request', url });
    }
  });
  page.on('response', (res) => {
    const url = res.url();
    if (/favicon|lobster|openclaw|healthz/i.test(url)) {
      requestEvents.push({ type: 'response', url, status: res.status() });
    }
  });
  page.on('requestfailed', (req) => {
    const url = req.url();
    if (/favicon|lobster|openclaw|healthz/i.test(url)) {
      requestEvents.push({ type: 'requestfailed', url, failure: req.failure()?.errorText || null });
    }
  });

  await page.goto(`${baseURL}/login`, { waitUntil: 'domcontentloaded', timeout: 20000 });
  await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => undefined);
  const loginInfo = await collectPage(page, 'login-clean-profile', '/login');
  const faviconHref = await page.locator('link[rel~="icon"]').getAttribute('href').catch(() => null);
  const languageSelectValue = await page.locator('select').first().inputValue({ timeout: 1000 }).catch(() => null);
  const languageSelectedText = await page.locator('select option:checked').first().textContent({ timeout: 1000 }).catch(() => null);
  let faviconStatus = null;
  if (faviconHref) {
    const faviconUrl = new URL(faviconHref, baseURL).toString();
    const favResponse = await api.get(faviconUrl).catch((error) => ({ error }));
    faviconStatus = favResponse.error
      ? { url: faviconUrl, error: favResponse.error.message }
      : { url: faviconUrl, status: favResponse.status(), ok: favResponse.ok() };
  }
  result.login = { ...loginInfo, languageSelectValue, languageSelectedText, faviconHref, faviconStatus, requestEvents };

  if (!loginInfo.bodyHasGTManager) {
    result.failures.push('Clean-profile /login does not show GTManager in visible body or browser title.');
  }
  const selectedChinese = (languageSelectValue || '').toLowerCase().startsWith('zh')
    || (languageSelectedText || '').includes('中文')
    || (loginInfo.htmlLang || '').toLowerCase().startsWith('zh');
  if (!selectedChinese) {
    result.failures.push('Clean-profile /login does not default to Chinese (html lang / selected language are not zh/中文).');
  }
  if (loginInfo.bodyHasClawManager) {
    result.failures.push('Clean-profile /login still shows ClawManager.');
  }

  if (adminUser && adminPassword) {
    const usernameInput = page.locator('#username, input[name="username"], input[type="text"]').first();
    const passwordInput = page.locator('#password, input[name="password"], input[type="password"]').first();
    await usernameInput.fill(adminUser, { timeout: 7000 });
    await passwordInput.fill(adminPassword, { timeout: 7000 });
    await Promise.all([
      page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => undefined),
      page.locator('button[type="submit"]').click({ timeout: 7000 }),
    ]);
    await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => undefined);
    const authInfo = await collectPage(page, 'after-admin-login', 'login-submit');
    let authenticated = await page.evaluate(() => Boolean(window.localStorage.getItem('access_token'))).catch(() => false);
    let apiTokenBootstrap = false;
    let apiLoginStatus = null;

    if (!authenticated) {
      const apiLoginResponse = await api.post(`${baseURL}/api/v1/auth/login`, {
        data: { username: adminUser, password: adminPassword },
      }).catch((error) => ({ error }));

      if (apiLoginResponse.error) {
        result.blockers.push(`Admin API token bootstrap failed: ${apiLoginResponse.error.message}`);
      } else {
        apiLoginStatus = apiLoginResponse.status();
        if (apiLoginResponse.ok()) {
          const payload = await apiLoginResponse.json().catch(() => null);
          const accessToken = payload?.data?.access_token;
          const refreshToken = payload?.data?.refresh_token;
          if (accessToken && refreshToken) {
            await page.evaluate(
              ({ accessTokenValue, refreshTokenValue }) => {
                window.localStorage.setItem('access_token', accessTokenValue);
                window.localStorage.setItem('refresh_token', refreshTokenValue);
              },
              { accessTokenValue: accessToken, refreshTokenValue: refreshToken },
            );
            authenticated = true;
            apiTokenBootstrap = true;
          } else {
            result.blockers.push('Admin API token bootstrap response did not include expected token fields.');
          }
        } else {
          result.blockers.push(`Admin API token bootstrap returned HTTP ${apiLoginStatus}.`);
        }
      }
    }

    result.auth = { ...authInfo, authenticated, apiTokenBootstrap, apiLoginStatus };

    if (!authenticated) {
      result.blockers.push('Admin UI login did not leave an access token in localStorage, and API token bootstrap did not recover authenticated route coverage.');
    } else {
      const adminRoutes = [
        ['/admin', 'admin-dashboard'],
        ['/admin/users', 'admin-users'],
        ['/admin/instances', 'admin-instances'],
        ['/admin/ai-gateway', 'admin-ai-gateway'],
        ['/admin/security', 'admin-security-center'],
        ['/admin/settings', 'admin-settings'],
      ];
      for (const [route, label] of adminRoutes) {
        await page.goto(`${baseURL}${route}`, { waitUntil: 'domcontentloaded', timeout: 20000 }).catch((error) => {
          result.failures.push(`${route} navigation failed: ${error.message}`);
        });
        await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => undefined);
        const info = await collectPage(page, label, route);
        result.adminRoutes.push(info);
        if (info.redirectedToLogin) {
          result.failures.push(`${route} redirected to login.`);
        }
      }

      const userRoutes = [
        ['/dashboard', 'user-dashboard'],
        ['/instances', 'user-instances'],
        ['/openclaw-configs', 'user-openclaw-resource-management'],
        ['/settings', 'user-settings'],
        ['/portal', 'user-instance-access-surface'],
      ];
      for (const [route, label] of userRoutes) {
        await page.goto(`${baseURL}${route}`, { waitUntil: 'domcontentloaded', timeout: 20000 }).catch((error) => {
          result.failures.push(`${route} navigation failed: ${error.message}`);
        });
        await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => undefined);
        const info = await collectPage(page, label, route);
        result.userRoutes.push(info);
        if (info.redirectedToLogin) {
          result.failures.push(`${route} redirected to login.`);
        }
      }
    }
  } else {
    result.blockers.push('No admin credentials were provided through E2E_ADMIN_USER/E2E_ADMIN_PASSWORD.');
  }

  await context.close();
} finally {
  await browser.close();
  await api.dispose();
}

result.finishedAt = new Date().toISOString();
result.verdict = result.failures.length === 0 && result.blockers.length === 0 ? 'E2E passed' : 'E2E blocked';
await fs.writeFile(
  path.join(evidenceDir, 'playwright-result.json'),
  `${JSON.stringify(result, null, 2)}\n`,
  'utf8',
);

console.log(JSON.stringify({
  verdict: result.verdict,
  failures: result.failures,
  blockers: result.blockers,
  screenshotsDir: path.relative(process.cwd(), screenshotsDir),
}, null, 2));
