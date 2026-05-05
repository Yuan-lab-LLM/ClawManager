import { chromium, request } from 'playwright';
import fs from 'node:fs/promises';
import path from 'node:path';

const baseURL = process.env.E2E_BASE_URL || 'https://localhost:30443';
const evidenceDir = process.env.E2E_EVIDENCE_DIR;
const adminUser = process.env.E2E_ADMIN_USER;
const adminPassword = process.env.E2E_ADMIN_PASSWORD;
const userUser = process.env.E2E_USER_USER;
const userPassword = process.env.E2E_USER_PASSWORD;

if (!evidenceDir) {
  throw new Error('E2E_EVIDENCE_DIR is required');
}

const screenshotsDir = path.join(evidenceDir, 'screenshots');
const logsDir = path.join(evidenceDir, 'logs');
await fs.mkdir(screenshotsDir, { recursive: true });
await fs.mkdir(logsDir, { recursive: true });

const result = {
  baseURL,
  startedAt: new Date().toISOString(),
  tool: 'playwright',
  browser: 'chromium',
  credentials: {
    adminProvided: Boolean(adminUser && adminPassword),
    userProvided: Boolean(userUser && userPassword),
    routeCoverageCredentialMode: userUser && userPassword ? 'separate-user' : 'admin-authenticated-context',
  },
  healthz: null,
  login: null,
  auth: null,
  adminRoutes: [],
  userRoutes: [],
  openClawRuntimeWording: null,
  protectedIdentifiers: {
    apiHealthzChecked: false,
    apiV1LoginChecked: false,
    localeStorageKeyObservedByPrereqGrep: true,
  },
  assetStatus: {
    assetsWorkerC: 'BLOCKED',
    reason: 'Exact logo/favicon/loading source paths were not provided.',
    faviconRequestRecorded: false,
    faviconStillLobster: false,
  },
  issues: [],
  failures: [],
  blockers: [],
};

const consoleEvents = [];
const networkEvents = [];
const excerpt = (text) => (text || '').replace(/\s+/g, ' ').trim().slice(0, 900);
const hasChinese = (text) => /[\u4e00-\u9fff]/.test(text || '');
const hasLoadingOnly = (text) => {
  const trimmed = (text || '').replace(/\s+/g, ' ').trim();
  return /^(Loading|Loading\.{0,3}|加载中[.。…]*|正在加载[.。…]*|Loading dashboard\.{0,3})$/i.test(trimmed);
};

async function collectPage(page, label, route) {
  const bodyText = await page.locator('body').innerText({ timeout: 9000 }).catch(() => '');
  const title = await page.title().catch(() => '');
  const htmlLang = await page.locator('html').getAttribute('lang').catch(() => null);
  const runtimeDocumentLang = await page.evaluate(() => document.documentElement.lang).catch(() => null);
  const screenshot = path.join(screenshotsDir, `${label}.png`);
  await page.screenshot({ path: screenshot, fullPage: true }).catch(() => undefined);
  return {
    route,
    url: page.url(),
    title,
    htmlLang,
    runtimeDocumentLang,
    screenshot: path.relative(evidenceDir, screenshot),
    bodyExcerpt: excerpt(bodyText),
    bodyHasChinese: hasChinese(bodyText),
    bodyHasGTManager: bodyText.includes('GTManager') || title.includes('GTManager'),
    bodyHasProductClawManager: bodyText.includes('ClawManager') || title.includes('ClawManager'),
    bodyHasOpenClaw: bodyText.includes('OpenClaw'),
    bodyHasLoadingOnly: hasLoadingOnly(bodyText),
    redirectedToLogin: page.url().includes('/login') && route !== '/login',
  };
}

async function apiLogin(api, username, password) {
  const response = await api.post(`${baseURL}/api/v1/auth/login`, {
    data: { username, password },
  }).catch((error) => ({ error }));

  if (response.error) {
    return { ok: false, error: response.error.message };
  }

  const status = response.status();
  const ok = response.ok();
  const payload = await response.json().catch(() => null);
  return {
    ok,
    status,
    hasAccessToken: Boolean(payload?.data?.access_token),
    hasRefreshToken: Boolean(payload?.data?.refresh_token),
    accessToken: payload?.data?.access_token,
    refreshToken: payload?.data?.refresh_token,
  };
}

async function setTokens(page, accessToken, refreshToken) {
  await page.evaluate(
    ({ accessTokenValue, refreshTokenValue }) => {
      window.localStorage.setItem('access_token', accessTokenValue);
      window.localStorage.setItem('refresh_token', refreshTokenValue);
    },
    { accessTokenValue: accessToken, refreshTokenValue: refreshToken },
  );
}

async function collectRouteGroup(page, routes, target) {
  for (const [route, label] of routes) {
    await page.goto(`${baseURL}${route}`, { waitUntil: 'domcontentloaded', timeout: 25000 }).catch((error) => {
      result.failures.push(`${route} navigation failed: ${error.message}`);
    });
    await page.waitForLoadState('networkidle', { timeout: 12000 }).catch(() => undefined);
    const info = await collectPage(page, label, route);
    const passed =
      !info.redirectedToLogin &&
      !info.bodyHasLoadingOnly &&
      info.bodyHasChinese &&
      info.bodyHasGTManager &&
      !info.bodyHasProductClawManager;
    target.push({ ...info, passed });

    if (info.redirectedToLogin) {
      result.failures.push(`${route} redirected to login.`);
    }
    if (info.bodyHasLoadingOnly) {
      result.failures.push(`${route} remained on a loading-only surface.`);
    }
    if (!info.bodyHasChinese) {
      result.failures.push(`${route} did not show observable Chinese UI text.`);
    }
    if (!info.bodyHasGTManager) {
      result.failures.push(`${route} did not show observable GTManager branding.`);
    }
    if (info.bodyHasProductClawManager) {
      result.failures.push(`${route} showed product-facing ClawManager text.`);
    }
  }
}

const api = await request.newContext({ ignoreHTTPSErrors: true });
try {
  const healthResponse = await api.get(`${baseURL}/healthz`);
  result.healthz = {
    status: healthResponse.status(),
    ok: healthResponse.ok(),
    text: await healthResponse.text().catch(() => ''),
  };
  result.protectedIdentifiers.apiHealthzChecked = true;
  if (!healthResponse.ok()) {
    result.failures.push(`/healthz returned HTTP ${healthResponse.status()}.`);
  }
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

  page.on('console', (message) => {
    consoleEvents.push({
      type: message.type(),
      text: excerpt(message.text()),
      location: message.location(),
    });
  });
  page.on('pageerror', (error) => {
    consoleEvents.push({ type: 'pageerror', text: excerpt(error.message) });
  });
  page.on('request', (req) => {
    const url = req.url();
    if (/favicon|lobster|openclaw|\/api\/v1|\/healthz/i.test(url)) {
      networkEvents.push({ type: 'request', method: req.method(), url });
    }
  });
  page.on('response', (res) => {
    const url = res.url();
    if (/favicon|lobster|openclaw|\/api\/v1|\/healthz/i.test(url)) {
      networkEvents.push({ type: 'response', url, status: res.status() });
    }
  });
  page.on('requestfailed', (req) => {
    const url = req.url();
    if (/favicon|lobster|openclaw|\/api\/v1|\/healthz/i.test(url)) {
      networkEvents.push({ type: 'requestfailed', url, failure: req.failure()?.errorText || null });
    }
  });

  await page.goto(`${baseURL}/login`, { waitUntil: 'domcontentloaded', timeout: 25000 });
  await page.waitForLoadState('networkidle', { timeout: 12000 }).catch(() => undefined);
  const loginInfo = await collectPage(page, 'login-clean-profile', '/login');
  const faviconHref = await page.locator('link[rel~="icon"]').getAttribute('href').catch(() => null);
  const languageSelectValue = await page.locator('select').first().inputValue({ timeout: 1500 }).catch(() => null);
  const languageSelectedText = await page.locator('select option:checked').first().textContent({ timeout: 1500 }).catch(() => null);
  let faviconStatus = null;
  if (faviconHref) {
    const faviconUrl = new URL(faviconHref, baseURL).toString();
    await page.evaluate(async (href) => {
      await fetch(href, { cache: 'no-store' }).catch(() => undefined);
    }, faviconUrl).catch(() => undefined);
    const favResponse = await api.get(faviconUrl).catch((error) => ({ error }));
    faviconStatus = favResponse.error
      ? { url: faviconUrl, error: favResponse.error.message }
      : { url: faviconUrl, status: favResponse.status(), ok: favResponse.ok() };
    result.assetStatus.faviconStillLobster = /lobster/i.test(faviconHref);
  }
  result.assetStatus.faviconRequestRecorded = networkEvents.some((event) => /favicon|lobster/i.test(event.url));
  result.login = {
    ...loginInfo,
    languageSelectValue,
    languageSelectedText,
    faviconHref,
    faviconStatus,
    requestEvents: networkEvents.filter((event) => /favicon|lobster/i.test(event.url)),
    passed:
      (loginInfo.htmlLang || loginInfo.runtimeDocumentLang || '').toLowerCase().startsWith('zh') &&
      loginInfo.title === 'GTManager' &&
      loginInfo.bodyHasGTManager &&
      !loginInfo.bodyHasProductClawManager &&
      ((languageSelectValue || '').toLowerCase().startsWith('zh') || (languageSelectedText || '').includes('中文')),
  };

  if (!(loginInfo.htmlLang || loginInfo.runtimeDocumentLang || '').toLowerCase().startsWith('zh')) {
    result.failures.push('Clean-profile /login document language is not zh.');
  }
  if (loginInfo.title !== 'GTManager') {
    result.failures.push(`Clean-profile /login browser title is ${JSON.stringify(loginInfo.title)}, not GTManager.`);
  }
  if (!loginInfo.bodyHasGTManager) {
    result.failures.push('Clean-profile /login does not show GTManager in visible body or browser title.');
  }
  if (!((languageSelectValue || '').toLowerCase().startsWith('zh') || (languageSelectedText || '').includes('中文'))) {
    result.failures.push('Clean-profile /login language selector/default is not Chinese.');
  }
  if (loginInfo.bodyHasProductClawManager) {
    result.failures.push('Clean-profile /login still shows product-facing ClawManager.');
  }
  if (!result.assetStatus.faviconRequestRecorded) {
    result.failures.push('Clean-profile /login did not record a favicon request.');
  }
  if (result.assetStatus.faviconStillLobster) {
    result.issues.push('Favicon still references lobster_transparent.png; recorded under Assets C blocked, not as an M1 text failure.');
  }

  if (adminUser && adminPassword) {
    const usernameInput = page.locator('#username, input[name="username"], input[type="text"]').first();
    const passwordInput = page.locator('#password, input[name="password"], input[type="password"]').first();
    await usernameInput.fill(adminUser, { timeout: 9000 });
    await passwordInput.fill(adminPassword, { timeout: 9000 });
    await Promise.all([
      page.waitForLoadState('networkidle', { timeout: 12000 }).catch(() => undefined),
      page.locator('button[type="submit"]').click({ timeout: 9000 }),
    ]);
    await page.waitForLoadState('networkidle', { timeout: 12000 }).catch(() => undefined);
    const authInfo = await collectPage(page, 'after-admin-login', 'login-submit');
    let authenticated = await page.evaluate(() => Boolean(window.localStorage.getItem('access_token'))).catch(() => false);
    let apiTokenBootstrap = false;
    let apiLoginStatus = null;
    let uiLoginIssue = null;

    if (!authenticated) {
      if (authInfo.bodyHasLoadingOnly || /loading/i.test(authInfo.bodyExcerpt)) {
        uiLoginIssue = 'UI login did not complete and remained on a Loading surface.';
      } else {
        uiLoginIssue = 'UI login did not leave an access token in localStorage.';
      }
      result.issues.push(uiLoginIssue);
      result.failures.push(`${uiLoginIssue} API token bootstrap was used to continue route coverage.`);

      const apiLoginResponse = await apiLogin(api, adminUser, adminPassword);
      apiLoginStatus = apiLoginResponse.status || null;
      result.protectedIdentifiers.apiV1LoginChecked = true;
      if (apiLoginResponse.ok && apiLoginResponse.accessToken && apiLoginResponse.refreshToken) {
        await setTokens(page, apiLoginResponse.accessToken, apiLoginResponse.refreshToken);
        authenticated = true;
        apiTokenBootstrap = true;
      } else {
        result.blockers.push(`Admin API token bootstrap failed or returned unusable tokens (status ${apiLoginStatus || 'n/a'}).`);
      }
    }

    result.auth = {
      ...authInfo,
      authenticated,
      apiTokenBootstrap,
      apiLoginStatus,
      uiLoginIssue,
      passed: authenticated,
    };

    if (!authenticated) {
      result.blockers.push('Authenticated route coverage could not continue.');
    } else {
      const adminRoutes = [
        ['/admin', 'admin-dashboard'],
        ['/admin/users', 'admin-users'],
        ['/admin/instances', 'admin-instances'],
        ['/admin/ai-gateway', 'admin-ai-gateway'],
        ['/admin/security', 'admin-security-center'],
        ['/admin/security/reports', 'admin-security-reports'],
        ['/admin/security/scanner', 'admin-security-scanner'],
        ['/admin/settings', 'admin-settings'],
      ];
      await collectRouteGroup(page, adminRoutes, result.adminRoutes);

      const userRoutes = [
        ['/dashboard', 'user-dashboard'],
        ['/instances', 'user-instances'],
        ['/openclaw-configs', 'user-openclaw-resource-management'],
        ['/settings', 'user-settings'],
        ['/portal', 'user-instance-access-surface'],
      ];

      if (userUser && userPassword) {
        const userApiLoginResponse = await apiLogin(api, userUser, userPassword);
        if (userApiLoginResponse.ok && userApiLoginResponse.accessToken && userApiLoginResponse.refreshToken) {
          await setTokens(page, userApiLoginResponse.accessToken, userApiLoginResponse.refreshToken);
        } else {
          result.issues.push(`Separate user credential login was provided but did not produce usable tokens (status ${userApiLoginResponse.status || 'n/a'}); using existing authenticated context.`);
        }
      } else {
        result.issues.push('No separate regular-user credentials were provided; user route surfaces were covered under the authenticated admin context.');
      }

      await collectRouteGroup(page, userRoutes, result.userRoutes);
    }
  } else {
    result.blockers.push('No admin credentials were provided through E2E_ADMIN_USER/E2E_ADMIN_PASSWORD.');
  }

  await context.close();
} finally {
  await browser.close();
  await api.dispose();
}

await fs.writeFile(path.join(logsDir, 'playwright-console-events.json'), `${JSON.stringify(consoleEvents, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'playwright-network-events.json'), `${JSON.stringify(networkEvents, null, 2)}\n`, 'utf8');

const openClawRoutes = [...result.adminRoutes, ...result.userRoutes].filter((route) => route.bodyHasOpenClaw);
result.openClawRuntimeWording = {
  observed: openClawRoutes.length > 0,
  routes: openClawRoutes.map((route) => route.route),
};
if (openClawRoutes.length === 0) {
  result.failures.push('OpenClaw runtime wording was not observed on covered runtime/resource surfaces.');
}

const routeFailures = [...result.adminRoutes, ...result.userRoutes].filter((route) => !route.passed);
const hardBlockers = result.blockers.length > 0;
const assertionFailures = result.failures.length > 0;
result.finishedAt = new Date().toISOString();
result.summary = {
  loginPassed: Boolean(result.login?.passed),
  adminRoutesCovered: result.adminRoutes.length,
  adminRoutesPassed: result.adminRoutes.filter((route) => route.passed).length,
  userRoutesCovered: result.userRoutes.length,
  userRoutesPassed: result.userRoutes.filter((route) => route.passed).length,
  routeFailures: routeFailures.map((route) => route.route),
  issues: result.issues.length,
  failures: result.failures.length,
  blockers: result.blockers.length,
};
result.verdict = hardBlockers ? 'E2E blocked' : assertionFailures ? 'E2E partial / blocked' : 'E2E passed';

await fs.writeFile(
  path.join(evidenceDir, 'playwright-rerun-result.json'),
  `${JSON.stringify(result, null, 2)}\n`,
  'utf8',
);

console.log(JSON.stringify({
  verdict: result.verdict,
  summary: result.summary,
  issues: result.issues,
  failures: result.failures,
  blockers: result.blockers,
  screenshotsDir: path.relative(process.cwd(), screenshotsDir),
  resultJson: path.join(evidenceDir, 'playwright-rerun-result.json'),
}, null, 2));
