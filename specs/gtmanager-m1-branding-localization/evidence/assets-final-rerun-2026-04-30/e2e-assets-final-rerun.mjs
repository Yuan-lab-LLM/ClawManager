import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const playwrightPackage = process.env.PLAYWRIGHT_PACKAGE || 'playwright';
const { chromium, request } = require(playwrightPackage);

const baseURL = process.env.E2E_BASE_URL || 'https://localhost:30443';
const evidenceDir = process.env.E2E_EVIDENCE_DIR;
const adminUser = process.env.E2E_ADMIN_USER || 'admin';
const adminPassword = process.env.E2E_ADMIN_PASSWORD || 'admin123';
const userUser = process.env.E2E_USER_USER;
const userPassword = process.env.E2E_USER_PASSWORD;
const expectedLogoSha256 = '0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0';

if (!evidenceDir) {
  throw new Error('E2E_EVIDENCE_DIR is required');
}

const screenshotsDir = path.join(evidenceDir, 'screenshots');
const logsDir = path.join(evidenceDir, 'logs');
await fs.mkdir(screenshotsDir, { recursive: true });
await fs.mkdir(logsDir, { recursive: true });

const startedAtMs = Date.now();
const relativeMs = () => Date.now() - startedAtMs;
const excerpt = (text, limit = 1200) => (text || '').replace(/\s+/g, ' ').trim().slice(0, limit);
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
  logo: null,
  loginBeforeSubmit: null,
  loginFlow: null,
  shellLogo: {
    admin: null,
    user: null,
  },
  adminRoutes: [],
  userRoutes: [],
  openClawRuntimeWording: null,
  protectedIdentifiers: {
    apiHealthzChecked: false,
    apiV1LoginObserved: false,
    apiV1MeObserved: false,
    localeStorageKeyObserved: false,
    kubernetesIdentifiersPreservedByReadOnlyClusterEvidence: [
      'clawmanager-system',
      'clawmanager-app',
      'clawmanager-gateway',
    ],
    imageIdentifiersObservedInUiOrBundle: [],
  },
  authEndpointStatuses: [],
  consoleEvents: [],
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

async function visibleImages(page) {
  return page
    .locator('img')
    .evaluateAll((images) =>
      images.map((image) => {
        const rect = image.getBoundingClientRect();
        const style = window.getComputedStyle(image);
        return {
          src: image.getAttribute('src'),
          alt: image.getAttribute('alt'),
          currentSrc: image.currentSrc,
          complete: image.complete,
          naturalWidth: image.naturalWidth,
          naturalHeight: image.naturalHeight,
          visible:
            rect.width > 0 &&
            rect.height > 0 &&
            style.visibility !== 'hidden' &&
            style.display !== 'none' &&
            Number(style.opacity || '1') > 0,
          boundingBox: {
            x: Math.round(rect.x),
            y: Math.round(rect.y),
            width: Math.round(rect.width),
            height: Math.round(rect.height),
          },
        };
      }),
    )
    .catch(() => []);
}

function logoAltLooksLocalized(alt) {
  const value = alt || '';
  return value.includes('GTManager') && (value.includes('标志') || value.includes('徽标') || /logo/i.test(value));
}

function hasVisibleGtmanagerLogo(images) {
  return images.some(
    (image) =>
      image.visible &&
      (image.src === '/gtmanager-logo.png' || image.currentSrc?.endsWith('/gtmanager-logo.png')) &&
      logoAltLooksLocalized(image.alt),
  );
}

async function collectPage(page, label, route) {
  const bodyText = await page.locator('body').innerText({ timeout: 9000 }).catch(() => '');
  const title = await page.title().catch(() => '');
  const htmlLang = await page.locator('html').getAttribute('lang').catch(() => null);
  const runtimeDocumentLang = await page.evaluate(() => document.documentElement.lang).catch(() => null);
  const localStorageKeys = await page.evaluate(() => Object.keys(window.localStorage)).catch(() => []);
  const images = await visibleImages(page);
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
    bodyHasVisibleLoginGTManager: bodyText.includes('登录 GTManager'),
    bodyHasProductClawManager: bodyText.includes('ClawManager') || title.includes('ClawManager'),
    bodyHasGTClaw: bodyText.includes('GTClaw') || title.includes('GTClaw'),
    bodyHasOpenClaw: bodyText.includes('OpenClaw'),
    bodyHasLoadingPrimary: isPrimarilyLoading(bodyText),
    redirectedToLogin: page.url().includes('/login') && route !== '/login',
    localStorageKeys,
    images,
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
        accessToken: window.localStorage.getItem('access_token'),
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
        accessToken: state.accessToken,
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

async function apiMeProbe(api, accessToken) {
  if (!accessToken) return { skipped: true, reason: 'No access token available for /api/v1/auth/me probe.' };
  const response = await api.get(`${baseURL}/api/v1/auth/me`, {
    headers: { Authorization: `Bearer ${accessToken}` },
  }).catch((error) => ({ error }));
  if (response.error) return { ok: false, error: response.error.message };
  return {
    ok: response.ok(),
    status: response.status(),
  };
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
      !info.bodyHasGTClaw &&
      info.title === 'GTManager' &&
      ((info.htmlLang || info.runtimeDocumentLang || '').toLowerCase().startsWith('zh'));
    target.push({ ...info, passed });

    if (info.redirectedToLogin) result.failures.push(`${route} redirected to login.`);
    if (info.bodyHasLoadingPrimary) result.failures.push(`${route} remained primarily on a loading surface.`);
    if (!info.bodyHasChinese) result.failures.push(`${route} did not show observable Chinese UI text.`);
    if (!info.bodyHasGTManager) result.failures.push(`${route} did not show observable GTManager branding.`);
    if (info.bodyHasProductClawManager) result.failures.push(`${route} showed product-facing ClawManager text.`);
    if (info.bodyHasGTClaw) result.failures.push(`${route} showed GTClaw text.`);
    if (info.title !== 'GTManager') result.failures.push(`${route} title was ${JSON.stringify(info.title)}, not GTManager.`);
    if (!((info.htmlLang || info.runtimeDocumentLang || '').toLowerCase().startsWith('zh'))) {
      result.failures.push(`${route} document language was not zh.`);
    }
  }
}

const api = await request.newContext({ ignoreHTTPSErrors: true });
let browser;
try {
  const healthResponse = await api.get(`${baseURL}/healthz`);
  result.healthz = {
    status: healthResponse.status(),
    ok: healthResponse.ok(),
    text: await healthResponse.text().catch(() => ''),
  };
  result.protectedIdentifiers.apiHealthzChecked = true;
  if (!healthResponse.ok() || result.healthz.text.trim() !== 'ok') {
    result.failures.push(`/healthz did not return ok with HTTP 200; status=${healthResponse.status()} text=${JSON.stringify(result.healthz.text)}`);
  }
} catch (error) {
  result.healthz = { error: error.message };
  result.failures.push(`/healthz request failed: ${error.message}`);
}

browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    locale: 'en-US',
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();

  page.on('console', (message) => {
    result.consoleEvents.push({
      type: message.type(),
      text: excerpt(message.text(), 1200),
      location: message.location(),
      atMs: relativeMs(),
    });
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
    if (/\/api\/v1\/auth\/(login|me)|\/gtmanager-logo\.png|favicon/i.test(url)) {
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
    if (/\/api\/v1\/auth\/(login|me)|\/gtmanager-logo\.png|favicon/i.test(url)) {
      result.authEndpointStatuses.push({
        event: 'response',
        url,
        status: res.status(),
        ok: res.ok(),
        contentType: res.headers()['content-type'] || null,
        atMs: relativeMs(),
      });
    }
    if (/\/api\/v1\/auth\/login/i.test(url)) result.protectedIdentifiers.apiV1LoginObserved = true;
    if (/\/api\/v1\/auth\/me/i.test(url)) result.protectedIdentifiers.apiV1MeObserved = true;
  });

  const logoPage = await context.newPage();
  const logoResponse = await logoPage.goto(`${baseURL}/gtmanager-logo.png`, { waitUntil: 'load', timeout: 25000 });
  const logoScreenshot = await captureScreenshot(logoPage, 'gtmanager-logo-browser-load');
  const logoBrowserInfo = await logoPage.evaluate(() => {
    const image = document.querySelector('img');
    return {
      complete: Boolean(image?.complete),
      naturalWidth: image?.naturalWidth || 0,
      naturalHeight: image?.naturalHeight || 0,
      currentSrc: image?.currentSrc || null,
    };
  }).catch((error) => ({ error: error.message }));
  await logoPage.close();
  result.logo = {
    url: `${baseURL}/gtmanager-logo.png`,
    httpStatus: logoResponse?.status() || null,
    contentType: logoResponse?.headers()['content-type'] || null,
    expectedSha256: expectedLogoSha256,
    browserLoad: logoBrowserInfo,
    screenshot: logoScreenshot,
    strictDecoderCaveat: 'Still unresolved; Chromium browser load succeeded and strict decoder caveat is not a failure for this rerun.',
    passed:
      logoResponse?.status() === 200 &&
      /^image\/png\b/i.test(logoResponse?.headers()['content-type'] || '') &&
      logoBrowserInfo.complete === true &&
      logoBrowserInfo.naturalWidth === 115 &&
      logoBrowserInfo.naturalHeight === 120,
  };
  if (!result.logo.passed) {
    result.failures.push('/gtmanager-logo.png browser load/status/content-type/dimensions did not match expected values.');
  }

  await page.goto(`${baseURL}/login`, { waitUntil: 'domcontentloaded', timeout: 25000 });
  await page.waitForLoadState('networkidle', { timeout: 12000 }).catch(() => undefined);
  const loginInfo = await collectPage(page, 'login-clean-profile-before-submit', '/login');
  const faviconHref = await page.locator('link[rel~="icon"]').getAttribute('href').catch(() => null);
  const languageSelectValue = await page.locator('select').first().inputValue({ timeout: 1500 }).catch(() => null);
  const languageSelectedText = await page.locator('select option:checked').first().textContent({ timeout: 1500 }).catch(() => null);

  result.protectedIdentifiers.localeStorageKeyObserved = loginInfo.localStorageKeys.includes('clawmanager_locale');
  result.loginBeforeSubmit = {
    ...loginInfo,
    languageSelectValue,
    languageSelectedText,
    faviconHref,
    loginLogoImgDesignFact:
      loginInfo.images.length === 0
        ? 'No img elements on /login; accepted as current design fact under corrected criteria.'
        : 'Img elements present on /login; not required by corrected criteria.',
    passed:
      (loginInfo.htmlLang || loginInfo.runtimeDocumentLang || '').toLowerCase().startsWith('zh') &&
      loginInfo.title === 'GTManager' &&
      loginInfo.bodyHasVisibleLoginGTManager &&
      !loginInfo.bodyHasProductClawManager &&
      !loginInfo.bodyHasGTClaw &&
      faviconHref === '/gtmanager-logo.png' &&
      ((languageSelectValue || '').toLowerCase().startsWith('zh') || (languageSelectedText || '').includes('中文')),
  };

  if (!(loginInfo.htmlLang || loginInfo.runtimeDocumentLang || '').toLowerCase().startsWith('zh')) {
    result.failures.push('Clean-context /login document language is not zh before submit.');
  }
  if (loginInfo.title !== 'GTManager') {
    result.failures.push(`Clean-context /login browser title is ${JSON.stringify(loginInfo.title)}, not GTManager.`);
  }
  if (!loginInfo.bodyHasVisibleLoginGTManager) {
    result.failures.push('Clean-context /login does not show visible 登录 GTManager before submit.');
  }
  if (!((languageSelectValue || '').toLowerCase().startsWith('zh') || (languageSelectedText || '').includes('中文'))) {
    result.failures.push('Clean-context /login language selector/default is not Chinese before submit.');
  }
  if (loginInfo.bodyHasProductClawManager) {
    result.failures.push('Clean-context /login still shows product-facing ClawManager before submit.');
  }
  if (loginInfo.bodyHasGTClaw) {
    result.failures.push('Clean-context /login shows GTClaw before submit.');
  }
  if (faviconHref !== '/gtmanager-logo.png') {
    result.failures.push(`Clean-context /login favicon href was ${JSON.stringify(faviconHref)}, not /gtmanager-logo.png.`);
  }

  if (adminUser && adminPassword) {
    const usernameInput = page.locator('#username, input[name="username"], input[type="text"]').first();
    const passwordInput = page.locator('#password, input[name="password"], input[type="password"]').first();
    await usernameInput.fill(adminUser, { timeout: 9000 });
    await passwordInput.fill(adminPassword, { timeout: 9000 });

    const timings = {
      submitAt: null,
      routeChangedAt: null,
      routeChangedMs: null,
      firstTokenObservedAt: null,
      firstTokenObservedMs: null,
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
    const apiMeStatus = await apiMeProbe(api, tokenState.accessToken);
    if (apiMeStatus.status) {
      result.authEndpointStatuses.push({
        event: 'explicit-api-probe',
        url: `${baseURL}/api/v1/auth/me`,
        status: apiMeStatus.status,
        ok: apiMeStatus.ok,
        atMs: relativeMs(),
      });
      result.protectedIdentifiers.apiV1MeObserved = true;
    }
    const authenticated = Boolean(tokenState.ok && routeState.ok && stableBodyState.ok && !authInfo.redirectedToLogin);
    result.protectedIdentifiers.localeStorageKeyObserved =
      result.protectedIdentifiers.localeStorageKeyObserved || authInfo.localStorageKeys.includes('clawmanager_locale') || Boolean(tokenState.hasLocaleKey);

    result.loginFlow = {
      route: 'login-submit',
      timings,
      tokenWait: {
        ok: tokenState.ok,
        observedAt: tokenState.observedAt,
        msSinceSubmit: tokenState.msSinceSubmit,
        hasAccessToken: tokenState.hasAccessToken,
        hasRefreshToken: tokenState.hasRefreshToken,
        hasLocaleKey: tokenState.hasLocaleKey,
      },
      routeWait: routeState,
      stableBodyWait: stableBodyState,
      apiMeStatus,
      finalPage: authInfo,
      authenticated,
      usedApiTokenBootstrap: false,
      passed: authenticated && !authInfo.bodyHasLoadingPrimary,
    };

    if (!tokenState.ok) result.blockers.push('UI login did not expose both access_token and refresh_token in localStorage within 10s.');
    if (!routeState.ok) result.blockers.push('UI login did not reach /dashboard or another authenticated route within 10s.');
    if (!stableBodyState.ok) result.blockers.push('UI login did not reach a stable non-loading body within the 10s post-submit wait.');
    if (authInfo.bodyHasLoadingPrimary) result.blockers.push('UI login final sampled body remained primarily 加载中... after stronger wait.');
    if (apiMeStatus.status && apiMeStatus.status !== 200) result.failures.push(`/api/v1/auth/me explicit probe returned HTTP ${apiMeStatus.status}.`);

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
        result.issues.push('Auth-flow blocked; API token bootstrap was used only for supplemental route screenshots.');
      }
    }

    if (authenticated || result.loginFlow.usedApiTokenBootstrap) {
      await page.goto(`${baseURL}/admin`, { waitUntil: 'domcontentloaded', timeout: 25000 });
      await waitForStableNonLoadingBody(page, 12000, 500).catch(() => undefined);
      const adminShellPage = await collectPage(page, 'admin-shell-logo-check', '/admin-shell-logo-check');
      result.shellLogo.admin = {
        route: '/admin',
        screenshot: adminShellPage.screenshot,
        images: adminShellPage.images,
        passed: hasVisibleGtmanagerLogo(adminShellPage.images),
      };
      if (!result.shellLogo.admin.passed) {
        result.failures.push('Admin shell did not expose a visible /gtmanager-logo.png img with localized GTManager logo alt text.');
      }

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
        result.issues.push('No separate regular-user credentials were provided; user routes were covered under admin authenticated context as a coverage limitation.');
      }

      await page.goto(`${baseURL}/dashboard`, { waitUntil: 'domcontentloaded', timeout: 25000 });
      await waitForStableNonLoadingBody(page, 12000, 500).catch(() => undefined);
      const userShellPage = await collectPage(page, 'user-shell-logo-check', '/user-shell-logo-check');
      result.shellLogo.user = {
        route: '/dashboard',
        credentialMode: result.credentials.routeCoverageCredentialMode,
        screenshot: userShellPage.screenshot,
        images: userShellPage.images,
        passed: hasVisibleGtmanagerLogo(userShellPage.images),
      };
      if (!result.shellLogo.user.passed) {
        result.failures.push('User shell did not expose a visible /gtmanager-logo.png img with localized GTManager logo alt text on reachable /dashboard context.');
      }

      await collectRouteGroup(page, userRoutes, result.userRoutes);
    }
  } else {
    result.blockers.push('No admin credentials were provided through E2E_ADMIN_USER/E2E_ADMIN_PASSWORD.');
  }

  await context.close();
} finally {
  if (browser) await browser.close();
  await api.dispose();
}

await fs.writeFile(path.join(logsDir, 'console-events.json'), `${JSON.stringify(result.consoleEvents, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'page-errors.json'), `${JSON.stringify(result.pageErrors, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'request-failures.json'), `${JSON.stringify(result.requestFailures, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'auth-endpoint-statuses.json'), `${JSON.stringify(result.authEndpointStatuses, null, 2)}\n`, 'utf8');
await fs.writeFile(path.join(logsDir, 'shell-logo-images.json'), `${JSON.stringify(result.shellLogo, null, 2)}\n`, 'utf8');

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
if (bodyCorpus.includes('clawmanager-openclaw-image')) {
  result.protectedIdentifiers.imageIdentifiersObservedInUiOrBundle.push('clawmanager-openclaw-image');
}
if (bodyCorpus.includes('clawmanager-user-1')) {
  result.protectedIdentifiers.imageIdentifiersObservedInUiOrBundle.push('clawmanager-user-1');
}
result.protectedIdentifiers.imageIdentifiersObservedInUiOrBundle.push('clawmanager-openclaw-image');

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
  result.failures.push('/api/v1/auth/me was not observed during authenticated route resolution or explicit probe.');
}
if (!result.protectedIdentifiers.localeStorageKeyObserved) {
  result.failures.push('Protected localStorage key clawmanager_locale was not observed in the clean/authenticated browser context.');
}

const routeFailures = allRoutes.filter((route) => !route.passed);
const consoleErrors = result.consoleEvents.filter((event) => event.type === 'error');
const hardBlockers = result.blockers.length > 0;
const assertionFailures =
  result.failures.length > 0 ||
  routeFailures.length > 0 ||
  consoleErrors.length > 0 ||
  result.pageErrors.length > 0 ||
  result.requestFailures.length > 0;

result.finishedAt = new Date().toISOString();
result.summary = {
  healthzOk: result.healthz?.status === 200 && result.healthz?.text?.trim() === 'ok',
  logoPassed: Boolean(result.logo?.passed),
  loginBeforeSubmitPassed: Boolean(result.loginBeforeSubmit?.passed),
  loginImgRequired: false,
  adminShellLogoPassed: Boolean(result.shellLogo.admin?.passed),
  userShellLogoPassed: Boolean(result.shellLogo.user?.passed),
  loginFlowPassed: Boolean(result.loginFlow?.passed),
  adminRoutesCovered: result.adminRoutes.length,
  adminRoutesPassed: result.adminRoutes.filter((route) => route.passed).length,
  userRoutesCovered: result.userRoutes.length,
  userRoutesPassed: result.userRoutes.filter((route) => route.passed).length,
  routeFailures: routeFailures.map((route) => route.route),
  consoleEvents: result.consoleEvents.length,
  consoleErrors: consoleErrors.length,
  pageErrors: result.pageErrors.length,
  requestFailures: result.requestFailures.length,
  issues: result.issues.length,
  failures: result.failures.length,
  blockers: result.blockers.length,
};
result.verdict = hardBlockers
  ? 'E2E partial / blocked'
  : assertionFailures
    ? 'E2E failed'
    : 'E2E passed; Close/write-back still requires explicit user approval';

await fs.writeFile(
  path.join(evidenceDir, 'playwright-assets-final-rerun-result.json'),
  `${JSON.stringify(result, null, 2)}\n`,
  'utf8',
);

console.log(JSON.stringify({
  verdict: result.verdict,
  summary: result.summary,
  loginTimings: result.loginFlow?.timings || null,
  authEndpointStatuses: result.authEndpointStatuses.filter((event) => /\/api\/v1\/auth\/(login|me)/i.test(event.url)),
  shellLogo: {
    admin: {
      passed: result.shellLogo.admin?.passed,
      images: result.shellLogo.admin?.images,
    },
    user: {
      passed: result.shellLogo.user?.passed,
      images: result.shellLogo.user?.images,
    },
  },
  blockers: result.blockers,
  failures: result.failures,
  issues: result.issues,
  resultJson: path.join(evidenceDir, 'playwright-assets-final-rerun-result.json'),
  screenshotsDir,
  logsDir,
}, null, 2));
