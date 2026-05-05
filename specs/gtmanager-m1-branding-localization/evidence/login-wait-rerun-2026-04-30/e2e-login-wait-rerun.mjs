import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const playwrightPackage =
  process.env.PLAYWRIGHT_PACKAGE ||
  '/Users/eduardogan/.npm/_npx/e41f203b7505f1fb/node_modules/playwright';
const { chromium, request } = require(playwrightPackage);

const baseURL = process.env.E2E_BASE_URL || 'https://localhost:30443';
const evidenceDir = process.env.E2E_EVIDENCE_DIR;
const adminUser = process.env.E2E_ADMIN_USER || 'admin';
const adminPassword = process.env.E2E_ADMIN_PASSWORD || 'admin123';
const userUser = process.env.E2E_USER_USER;
const userPassword = process.env.E2E_USER_PASSWORD;

if (!evidenceDir) {
  throw new Error('E2E_EVIDENCE_DIR is required');
}

const screenshotsDir = path.join(evidenceDir, 'screenshots');
const logsDir = path.join(evidenceDir, 'logs');
await fs.mkdir(screenshotsDir, { recursive: true });
await fs.mkdir(logsDir, { recursive: true });

const startedAtMs = Date.now();
const relativeMs = () => Date.now() - startedAtMs;
const excerpt = (text, limit = 900) => (text || '').replace(/\s+/g, ' ').trim().slice(0, limit);
const hasChinese = (text) => /[\u4e00-\u9fff]/.test(text || '');
const normalizeBody = (text) => (text || '').replace(/\s+/g, ' ').trim();
const isPrimarilyLoading = (text) => {
  const normalized = normalizeBody(text);
  if (!normalized) return true;
  if (/^(Loading|Loading\.{0,3}|加载中[.。…]*|正在加载[.。…]*|Loading dashboard\.{0,3})$/i.test(normalized)) {
    return true;
  }
  return normalized.length <= 30 && /(加载中|正在加载|Loading)/i.test(normalized);
};
const isAuthenticatedURL = (urlLike) => {
  const url = new URL(String(urlLike));
  return [
    '/dashboard',
    '/instances',
    '/openclaw-configs',
    '/settings',
    '/portal',
    '/admin',
    '/admin/users',
    '/admin/instances',
    '/admin/ai-gateway',
    '/admin/security',
    '/admin/settings',
  ].some((route) => url.pathname === route || url.pathname.startsWith(`${route}/`));
};

const result = {
  baseURL,
  startedAt: new Date(startedAtMs).toISOString(),
  tool: 'playwright',
  playwrightPackage,
  browser: 'chromium',
  context: {
    cleanContext: true,
    locale: 'en-US',
    ignoreHTTPSErrors: true,
  },
  credentials: {
    adminProvided: Boolean(adminUser && adminPassword),
    userProvided: Boolean(userUser && userPassword),
    routeCoverageCredentialMode: userUser && userPassword ? 'separate-user' : 'admin-authenticated-context',
  },
  healthz: null,
  loginBeforeSubmit: null,
  loginFlow: null,
  adminRoutes: [],
  userRoutes: [],
  openClawRuntimeWording: null,
  protectedIdentifiers: {
    apiHealthzChecked: false,
    apiV1LoginObserved: false,
    apiV1MeObserved: false,
    localeStorageKeyObserved: false,
    kubernetesIdentifiersObservedInUi: [],
    imageIdentifiersObservedInUi: [],
  },
  assetStatus: {
    assetsWorkerC: 'BLOCKED',
    reason: 'Exact logo, favicon, and loading source paths were not provided.',
    faviconRequestRecorded: false,
    faviconStillLobster: false,
  },
  authEndpointStatuses: [],
  consoleErrors: [],
  pageErrors: [],
  requestFailures: [],
  issues: [],
  failures: [],
  blockers: [],
};

async function captureScreenshot(page, label) {
  const screenshot = path.join(screenshotsDir, `${label}.png`);
  await page.screenshot({ path: screenshot, fullPage: true }).catch(() => undefined);
  return path.relative(evidenceDir, screenshot);
}

async function collectPage(page, label, route) {
  const bodyText = await page.locator('body').innerText({ timeout: 9000 }).catch(() => '');
  const title = await page.title().catch(() => '');
  const htmlLang = await page.locator('html').getAttribute('lang').catch(() => null);
  const runtimeDocumentLang = await page.evaluate(() => document.documentElement.lang).catch(() => null);
  const localStorageKeys = await page.evaluate(() => Object.keys(window.localStorage)).catch(() => []);
  const screenshot = await captureScreenshot(page, label);
  return {
    route,
    url: page.url(),
    title,
    htmlLang,
    runtimeDocumentLang,
    screenshot,
    bodyExcerpt: excerpt(bodyText),
    bodyHasChinese: hasChinese(bodyText),
    bodyHasGTManager: bodyText.includes('GTManager') || title.includes('GTManager'),
    bodyHasProductClawManager: bodyText.includes('ClawManager') || title.includes('ClawManager'),
    bodyHasOpenClaw: bodyText.includes('OpenClaw'),
    bodyHasLoadingPrimary: isPrimarilyLoading(bodyText),
    redirectedToLogin: page.url().includes('/login') && route !== '/login',
    localStorageKeys,
  };
}

async function waitForTokens(page, timeoutMs = 10000) {
  const started = Date.now();
  while (Date.now() - started <= timeoutMs) {
    const state = await page
      .evaluate(() => ({
        hasAccessToken: Boolean(window.localStorage.getItem('access_token')),
        hasRefreshToken: Boolean(window.localStorage.getItem('refresh_token')),
        hasLocaleKey: window.localStorage.getItem('clawmanager_locale') !== null,
      }))
      .catch(() => null);
    if (state?.hasAccessToken && state?.hasRefreshToken) {
      return {
        ok: true,
        observedAt: new Date().toISOString(),
        msSinceSubmit: null,
        hasAccessToken: true,
        hasRefreshToken: true,
        hasLocaleKey: state.hasLocaleKey,
      };
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  const finalState = await page
    .evaluate(() => ({
      hasAccessToken: Boolean(window.localStorage.getItem('access_token')),
      hasRefreshToken: Boolean(window.localStorage.getItem('refresh_token')),
      hasLocaleKey: window.localStorage.getItem('clawmanager_locale') !== null,
    }))
    .catch(() => null);
  return {
    ok: false,
    observedAt: null,
    msSinceSubmit: null,
    ...finalState,
  };
}

async function waitForStableNonLoadingBody(page, timeoutMs = 10000, stableMs = 500) {
  const started = Date.now();
  let candidateStartedAt = null;
  let candidateText = '';
  while (Date.now() - started <= timeoutMs) {
    const bodyText = await page.locator('body').innerText({ timeout: 1000 }).catch(() => '');
    const normalized = normalizeBody(bodyText);
    if (normalized && !isPrimarilyLoading(normalized)) {
      if (candidateStartedAt === null || normalized !== candidateText) {
        candidateStartedAt = Date.now();
        candidateText = normalized;
      } else if (Date.now() - candidateStartedAt >= stableMs) {
        return {
          ok: true,
          observedAt: new Date().toISOString(),
          stableForMs: Date.now() - candidateStartedAt,
          textExcerpt: excerpt(normalized),
        };
      }
    } else {
      candidateStartedAt = null;
      candidateText = '';
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  const finalText = await page.locator('body').innerText({ timeout: 1000 }).catch(() => '');
  return {
    ok: false,
    observedAt: null,
    stableForMs: 0,
    textExcerpt: excerpt(finalText),
    bodyHasLoadingPrimary: isPrimarilyLoading(finalText),
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
    await waitForStableNonLoadingBody(page, 12000, 500).catch(() => undefined);
    await page.waitForLoadState('networkidle', { timeout: 7000 }).catch(() => undefined);
    const info = await collectPage(page, label, route);
    const passed =
      !info.redirectedToLogin &&
      !info.bodyHasLoadingPrimary &&
      info.bodyHasChinese &&
      info.bodyHasGTManager &&
      !info.bodyHasProductClawManager &&
      info.title === 'GTManager' &&
      ((info.htmlLang || info.runtimeDocumentLang || '').toLowerCase().startsWith('zh'));
    target.push({ ...info, passed });

    if (info.redirectedToLogin) {
      result.failures.push(`${route} redirected to login.`);
    }
    if (info.bodyHasLoadingPrimary) {
      result.failures.push(`${route} remained primarily on a loading surface.`);
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
    if (info.title !== 'GTManager') {
      result.failures.push(`${route} title was ${JSON.stringify(info.title)}, not GTManager.`);
    }
    if (!((info.htmlLang || info.runtimeDocumentLang || '').toLowerCase().startsWith('zh'))) {
      result.failures.push(`${route} document language was not zh.`);
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
    if (message.type() === 'error') {
      result.consoleErrors.push({
        type: message.type(),
        text: excerpt(message.text(), 1200),
        location: message.location(),
        atMs: relativeMs(),
      });
    }
  });
  page.on('pageerror', (error) => {
    result.pageErrors.push({ text: excerpt(error.message, 1200), atMs: relativeMs() });
  });
  page.on('requestfailed', (req) => {
    result.requestFailures.push({
      method: req.method(),
      url: req.url(),
      failure: req.failure()?.errorText || null,
      atMs: relativeMs(),
    });
  });
  page.on('request', (req) => {
    const url = req.url();
    if (/\/api\/v1\/auth\/(login|me)|favicon|lobster/i.test(url)) {
      result.authEndpointStatuses.push({
        event: 'request',
        method: req.method(),
        url,
        atMs: relativeMs(),
      });
    }
  });
  page.on('response', (res) => {
    const url = res.url();
    if (/\/api\/v1\/auth\/(login|me)|favicon|lobster/i.test(url)) {
      result.authEndpointStatuses.push({
        event: 'response',
        url,
        status: res.status(),
        ok: res.ok(),
        atMs: relativeMs(),
      });
    }
    if (/\/api\/v1\/auth\/login/i.test(url)) result.protectedIdentifiers.apiV1LoginObserved = true;
    if (/\/api\/v1\/auth\/me/i.test(url)) result.protectedIdentifiers.apiV1MeObserved = true;
  });

  await page.goto(`${baseURL}/login`, { waitUntil: 'domcontentloaded', timeout: 25000 });
  await page.waitForLoadState('networkidle', { timeout: 12000 }).catch(() => undefined);
  const loginInfo = await collectPage(page, 'login-clean-profile-before-submit', '/login');
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
  result.assetStatus.faviconRequestRecorded = result.authEndpointStatuses.some((event) =>
    /favicon|lobster/i.test(event.url),
  );
  result.protectedIdentifiers.localeStorageKeyObserved = loginInfo.localStorageKeys.includes('clawmanager_locale');

  result.loginBeforeSubmit = {
    ...loginInfo,
    languageSelectValue,
    languageSelectedText,
    faviconHref,
    faviconStatus,
    passed:
      (loginInfo.htmlLang || loginInfo.runtimeDocumentLang || '').toLowerCase().startsWith('zh') &&
      loginInfo.title === 'GTManager' &&
      loginInfo.bodyExcerpt.includes('登录 GTManager') &&
      !loginInfo.bodyHasProductClawManager &&
      ((languageSelectValue || '').toLowerCase().startsWith('zh') || (languageSelectedText || '').includes('中文')),
  };

  if (!(loginInfo.htmlLang || loginInfo.runtimeDocumentLang || '').toLowerCase().startsWith('zh')) {
    result.failures.push('Clean-context /login document language is not zh before submit.');
  }
  if (loginInfo.title !== 'GTManager') {
    result.failures.push(`Clean-context /login browser title is ${JSON.stringify(loginInfo.title)}, not GTManager.`);
  }
  if (!loginInfo.bodyExcerpt.includes('登录 GTManager')) {
    result.failures.push('Clean-context /login does not show visible 登录 GTManager before submit.');
  }
  if (!((languageSelectValue || '').toLowerCase().startsWith('zh') || (languageSelectedText || '').includes('中文'))) {
    result.failures.push('Clean-context /login language selector/default is not Chinese before submit.');
  }
  if (loginInfo.bodyHasProductClawManager) {
    result.failures.push('Clean-context /login still shows product-facing ClawManager before submit.');
  }
  if (result.assetStatus.faviconStillLobster) {
    result.issues.push('Favicon still references lobster_transparent.png; Assets C remains blocked and this is not counted as text/localization pass.');
  }

  if (adminUser && adminPassword) {
    const usernameInput = page.locator('#username, input[name="username"], input[type="text"]').first();
    const passwordInput = page.locator('#password, input[name="password"], input[type="password"]').first();
    await usernameInput.fill(adminUser, { timeout: 9000 });
    await passwordInput.fill(adminPassword, { timeout: 9000 });

    const timings = {
      submitAt: null,
      firstTokenObservedAt: null,
      firstTokenObservedMs: null,
      routeChangedAt: null,
      routeChangedMs: null,
      finalStableBodyAt: null,
      finalStableBodyMs: null,
    };
    const submitEpoch = Date.now();
    timings.submitAt = new Date(submitEpoch).toISOString();

    const tokenPromise = waitForTokens(page, 10000).then((tokenState) => {
      if (tokenState.ok) {
        timings.firstTokenObservedAt = tokenState.observedAt;
        timings.firstTokenObservedMs = Date.parse(tokenState.observedAt) - submitEpoch;
        tokenState.msSinceSubmit = timings.firstTokenObservedMs;
      }
      return tokenState;
    });
    const routePromise = page
      .waitForURL((url) => isAuthenticatedURL(url), { timeout: 10000 })
      .then(() => {
        timings.routeChangedAt = new Date().toISOString();
        timings.routeChangedMs = Date.now() - submitEpoch;
        return { ok: true, url: page.url() };
      })
      .catch((error) => ({ ok: false, error: error.message, url: page.url() }));

    await page.locator('button[type="submit"]').click({ timeout: 9000 });
    const [tokenState, routeState] = await Promise.all([tokenPromise, routePromise]);
    const stableBodyState = await waitForStableNonLoadingBody(page, 10000, 500);
    if (stableBodyState.ok) {
      timings.finalStableBodyAt = stableBodyState.observedAt;
      timings.finalStableBodyMs = Date.parse(stableBodyState.observedAt) - submitEpoch;
    }
    await page.waitForLoadState('networkidle', { timeout: 7000 }).catch(() => undefined);

    const authInfo = await collectPage(page, 'after-admin-login-strong-wait', 'login-submit');
    const authenticated = Boolean(tokenState.ok && routeState.ok && stableBodyState.ok && !authInfo.redirectedToLogin);
    result.protectedIdentifiers.localeStorageKeyObserved =
      result.protectedIdentifiers.localeStorageKeyObserved || authInfo.localStorageKeys.includes('clawmanager_locale');

    result.loginFlow = {
      route: 'login-submit',
      timings,
      tokenWait: tokenState,
      routeWait: routeState,
      stableBodyWait: stableBodyState,
      finalPage: authInfo,
      authenticated,
      usedApiTokenBootstrap: false,
      passed: authenticated && !authInfo.bodyHasLoadingPrimary,
    };

    if (!tokenState.ok) {
      result.blockers.push('UI login did not expose both access_token and refresh_token in localStorage within 10s.');
    }
    if (!routeState.ok) {
      result.blockers.push('UI login did not reach /dashboard or another authenticated route within 10s.');
    }
    if (!stableBodyState.ok) {
      result.blockers.push('UI login did not reach a stable non-loading body within the 10s post-submit wait.');
    }
    if (authInfo.bodyHasLoadingPrimary) {
      result.blockers.push('UI login final sampled body remained primarily 加载中... after stronger wait.');
    }

    if (!authenticated) {
      const apiLoginResponse = await apiLogin(api, adminUser, adminPassword);
      result.loginFlow.apiFallbackProbe = {
        status: apiLoginResponse.status || null,
        ok: Boolean(apiLoginResponse.ok),
        hasAccessToken: Boolean(apiLoginResponse.hasAccessToken),
        hasRefreshToken: Boolean(apiLoginResponse.hasRefreshToken),
      };
      if (apiLoginResponse.ok && apiLoginResponse.accessToken && apiLoginResponse.refreshToken) {
        await setTokens(page, apiLoginResponse.accessToken, apiLoginResponse.refreshToken);
        result.loginFlow.usedApiTokenBootstrap = true;
        result.issues.push('API token bootstrap was available but route coverage is not counted as hidden pass for the UI login blocker.');
      }
    }

    if (authenticated || result.loginFlow.usedApiTokenBootstrap) {
      const adminRoutes = [
        ['/admin', 'admin-dashboard'],
        ['/admin/users', 'admin-users'],
        ['/admin/instances', 'admin-instances'],
        ['/admin/ai-gateway', 'admin-ai-gateway'],
        ['/admin/security', 'admin-security-center'],
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
          result.issues.push(`Separate user credentials were provided but did not produce usable tokens (status ${userApiLoginResponse.status || 'n/a'}); using existing authenticated context.`);
        }
      } else {
        result.issues.push('No separate regular-user credentials were provided; user route surfaces were covered under admin authenticated context as a coverage limitation.');
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

await fs.writeFile(path.join(logsDir, 'console-errors.json'), `${JSON.stringify(result.consoleErrors, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'page-errors.json'), `${JSON.stringify(result.pageErrors, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'request-failures.json'), `${JSON.stringify(result.requestFailures, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'auth-endpoint-statuses.json'), `${JSON.stringify(result.authEndpointStatuses, null, 2)}\n`, 'utf8');

const allRoutes = [...result.adminRoutes, ...result.userRoutes];
const openClawRoutes = allRoutes.filter((route) => route.bodyHasOpenClaw);
result.openClawRuntimeWording = {
  observed: openClawRoutes.length > 0,
  routes: openClawRoutes.map((route) => route.route),
};
if (openClawRoutes.length === 0) {
  result.failures.push('OpenClaw runtime wording was not observed on covered runtime/resource surfaces.');
}

const bodyCorpus = allRoutes.map((route) => route.bodyExcerpt).join('\n');
for (const identifier of ['clawmanager-system', 'clawmanager-app', 'clawmanager-gateway']) {
  if (bodyCorpus.includes(identifier)) {
    result.protectedIdentifiers.kubernetesIdentifiersObservedInUi.push(identifier);
  }
}
for (const identifier of ['clawmanager-openclaw-image', 'clawmanager-user-1']) {
  if (bodyCorpus.includes(identifier)) {
    result.protectedIdentifiers.imageIdentifiersObservedInUi.push(identifier);
  }
}
result.protectedIdentifiers.apiV1LoginObserved =
  result.protectedIdentifiers.apiV1LoginObserved ||
  result.authEndpointStatuses.some((event) => /\/api\/v1\/auth\/login/i.test(event.url));
result.protectedIdentifiers.apiV1MeObserved =
  result.protectedIdentifiers.apiV1MeObserved ||
  result.authEndpointStatuses.some((event) => /\/api\/v1\/auth\/me/i.test(event.url));

if (!result.protectedIdentifiers.apiV1LoginObserved) {
  result.failures.push('/api/v1/auth/login was not observed during UI login.');
}
if (!result.protectedIdentifiers.apiV1MeObserved) {
  result.failures.push('/api/v1/auth/me was not observed during authenticated route resolution.');
}

const routeFailures = allRoutes.filter((route) => !route.passed);
const hasBlockingLoginFailure = result.blockers.some((blocker) => /UI login|route|token|loading/i.test(blocker));
const hardBlockers = result.blockers.length > 0;
const assertionFailures = result.failures.length > 0 || routeFailures.length > 0;
result.finishedAt = new Date().toISOString();
result.summary = {
  loginBeforeSubmitPassed: Boolean(result.loginBeforeSubmit?.passed),
  loginFlowPassed: Boolean(result.loginFlow?.passed),
  adminRoutesCovered: result.adminRoutes.length,
  adminRoutesPassed: result.adminRoutes.filter((route) => route.passed).length,
  userRoutesCovered: result.userRoutes.length,
  userRoutesPassed: result.userRoutes.filter((route) => route.passed).length,
  routeFailures: routeFailures.map((route) => route.route),
  consoleErrors: result.consoleErrors.length,
  pageErrors: result.pageErrors.length,
  requestFailures: result.requestFailures.length,
  issues: result.issues.length,
  failures: result.failures.length,
  blockers: result.blockers.length,
};
result.verdict = hasBlockingLoginFailure || hardBlockers
  ? 'E2E blocked'
  : assertionFailures
    ? 'E2E partial / blocked'
    : 'E2E text/localization/auth-flow passed; final M1 close remains blocked by Assets C unless user explicitly re-scopes assets.';

await fs.writeFile(
  path.join(evidenceDir, 'playwright-login-wait-rerun-result.json'),
  `${JSON.stringify(result, null, 2)}\n`,
  'utf8',
);

console.log(JSON.stringify({
  verdict: result.verdict,
  summary: result.summary,
  loginTimings: result.loginFlow?.timings || null,
  authEndpointStatuses: result.authEndpointStatuses.filter((event) => /\/api\/v1\/auth\/(login|me)/i.test(event.url)),
  blockers: result.blockers,
  failures: result.failures,
  issues: result.issues,
  resultJson: path.join(evidenceDir, 'playwright-login-wait-rerun-result.json'),
  screenshotsDir,
  logsDir,
}, null, 2));
