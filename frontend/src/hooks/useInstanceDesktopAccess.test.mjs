import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";
import ts from "typescript";

const HOUR_MS = 60 * 60_000;

function loadModule(path, globals, dependencies = {}) {
  const source = readFileSync(new URL(path, import.meta.url), "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  });
  const exports = {};
  runInNewContext(compiled.outputText, {
    exports,
    ...globals,
    require: (name) => {
      assert.ok(name in dependencies, `Unexpected dependency: ${name}`);
      return dependencies[name];
    },
  });
  return exports;
}

// Exercise the production hook with a small state/effect facade and a controlled
// browser clock. No server, credentials, real-hour wait, or test dependency.
function setup({
  dedicatedOrigin = true,
  cookieFailures = 0,
  reloadOnAccessRefresh = false,
} = {}) {
  let now = Date.parse("2026-10-14T00:00:00Z");
  let timerId = 0;
  const timers = new Map();
  const listeners = new Map();
  const calls = [];
  const cookieRequests = [];
  let runtimeCookie;
  let managementCookie;
  const window = {
    location: {
      href: "https://manager.example/instances/42",
      origin: "https://manager.example",
    },
    setTimeout: (callback, delay) => {
      const id = ++timerId;
      timers.set(id, { callback, at: now + Math.max(0, delay) });
      return id;
    },
    clearTimeout: (id) => timers.delete(id),
    addEventListener: (name, callback) => {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(callback);
    },
    removeEventListener: (name, callback) => listeners.get(name)?.delete(callback),
    fetch: async (url, options) => {
      cookieRequests.push({ url, options });
      if (cookieFailures-- > 0) throw new Error("Temporary runtime-origin outage");
      runtimeCookie = {
        token: new URL(url).searchParams.get("token"),
        expiresAt: now + HOUR_MS,
      };
      return { type: "opaque", ok: false };
    },
  };
  const document = {
    hidden: false,
    addEventListener() {},
    removeEventListener() {},
  };
  class BrowserDate extends Date {
    static now() {
      return now;
    }
  }
  const globals = {
    window, document, URL, AbortSignal, Date: BrowserDate, queueMicrotask, console,
  };
  const { refreshDedicatedRuntimeCookie } = loadModule("../lib/instanceAccess.ts", globals);
  const instanceService = {
    generateAccessToken: async (instanceId) => {
      calls.push(instanceId);
      const token = `instance-capability-${calls.length}`;
      const expiresAt = now + HOUR_MS;
      managementCookie = { token, expiresAt };
      return {
        token,
        access_url: dedicatedOrigin
          ? `https://opencode-42.example/?token=${token}`
          : "/api/v1/instances/42/proxy/",
        proxy_url: `/api/v1/instances/42/proxy/?token=${token}`,
        expires_at: new Date(expiresAt).toISOString(),
      };
    },
  };

  const slots = [];
  let cursor = 0;
  let effects = [];
  let renderQueued = false;
  let mounted = true;
  let result;
  const dependenciesChanged = (previous, next) =>
    !previous || previous.length !== next.length ||
    next.some((value, i) => !Object.is(value, previous[i]));
  const queueRender = () => {
    if (renderQueued || !mounted) return;
    renderQueued = true;
    queueMicrotask(() => {
      renderQueued = false;
      if (mounted) render();
    });
  };
  const react = {
    useRef: (initial) => slots[cursor++] ??= { current: initial },
    useState: (initial) => {
      const slot = slots[cursor++] ??= { value: initial };
      return [slot.value, (value) => {
        const next = typeof value === "function" ? value(slot.value) : value;
        if (Object.is(next, slot.value)) return;
        slot.value = next;
        queueRender();
      }];
    },
    useCallback: (callback, deps) => {
      const index = cursor++;
      if (dependenciesChanged(slots[index]?.deps, deps)) {
        slots[index] = { callback, deps };
      }
      return slots[index].callback;
    },
    useEffect: (effect, deps) => {
      const index = cursor++;
      if (!dependenciesChanged(slots[index]?.deps, deps)) return;
      const cleanup = slots[index]?.cleanup;
      const slot = slots[index] = { deps };
      effects.push({
        cleanup,
        run: () => { slot.cleanup = effect(); },
      });
    },
  };
  const { useInstanceDesktopAccess } = loadModule("./useInstanceDesktopAccess.ts", globals, {
    react,
    axios: { isAxiosError: (error) => Boolean(error.response) },
    "../services/instanceService": { instanceService },
    "../lib/instanceAccess": { refreshDedicatedRuntimeCookie },
  });
  const options = {
    instanceId: 42,
    isRunning: true,
    retainSessionOnStop: true,
    reloadOnAccessRefresh,
    resolveEmbedUrl: (url) => url,
    failedMessage: "Failed to renew access",
  };
  function render() {
    cursor = 0;
    effects = [];
    result = useInstanceDesktopAccess(options);
    const pending = effects;
    for (const effect of pending) effect.cleanup?.();
    for (const effect of pending) effect.run();
  }
  async function settle() {
    for (let i = 0; i < 30; i++) await Promise.resolve();
  }
  render();
  return {
    calls, cookieRequests, refreshDedicatedRuntimeCookie,
    get result() { return result; },
    get runtimeCookie() { return runtimeCookie; },
    get managementCookie() { return managementCookie; },
    get now() { return now; },
    async advance(milliseconds) {
      await settle();
      const target = now + milliseconds;
      let count = 0;
      while (true) {
        const next = [...timers]
          .filter(([, timer]) => timer.at <= target)
          .sort((a, b) => a[1].at - b[1].at)[0];
        if (!next) break;
        assert.ok(count++ < 100, "Unexpected renewal loop");
        now = next[1].at;
        timers.delete(next[0]);
        next[1].callback();
        await settle();
      }
      now = target;
    },
    async resumeAfter(milliseconds) {
      now += milliseconds;
      for (const callback of listeners.get("focus") ?? []) callback();
      await settle();
    },
    stop() {
      options.isRunning = false;
      render();
    },
    unmount() {
      mounted = false;
      for (const slot of slots) slot?.cleanup?.();
    },
  };
}

test("dedicated-origin cookies renew for multiple hours while the iframe URL stays stable", async () => {
  const browser = setup();
  await browser.advance(0);
  const initialURL = browser.result.embedUrl;
  assert.match(initialURL, /[?]token=instance-capability-1$/);
  assert.equal(browser.cookieRequests.length, 0, "Initial navigation installs its own cookie");

  await browser.advance(2 * HOUR_MS);
  assert.equal(browser.calls.length, 3);
  assert.equal(browser.cookieRequests.length, 2);
  assert.equal(browser.result.embedUrl, initialURL, "Silent renewal must preserve the active iframe");
  assert.equal(browser.runtimeCookie.token, "instance-capability-3");
  assert.ok(browser.runtimeCookie.expiresAt > browser.now);
  assert.equal(browser.result.expiresAt.getTime(), browser.managementCookie.expiresAt);
  const { url, options } = browser.cookieRequests[1];
  assert.equal(url, "https://opencode-42.example/__clawmanager_access_refresh?token=instance-capability-3");
  assert.equal(options.credentials, "include");
  assert.equal(options.mode, "no-cors");
  assert.equal(options.cache, "no-store");
  assert.equal(options.referrerPolicy, "no-referrer");
  assert.ok(options.signal instanceof AbortSignal);
  browser.unmount();
});

test("same-origin proxy access renews using the existing management cookie", async () => {
  const browser = setup({ dedicatedOrigin: false });
  await browser.advance(HOUR_MS);
  assert.equal(browser.calls.length, 2);
  assert.equal(browser.cookieRequests.length, 0);
  assert.equal(browser.result.embedUrl, "/api/v1/instances/42/proxy/");
  assert.ok(browser.managementCookie.expiresAt > browser.now);
  browser.unmount();
});

test("failed cookie renewal keeps the old expiry and retries an established retained session", async () => {
  const browser = setup({ cookieFailures: 1 });
  await browser.advance(0);
  const initialExpiry = browser.result.expiresAt.getTime();
  await browser.advance(55 * 60_000);
  assert.equal(browser.cookieRequests.length, 1);
  assert.equal(browser.result.expiresAt.getTime(), initialExpiry);
  assert.equal(browser.result.error, "Failed to renew access");

  await browser.advance(10_000);
  assert.equal(browser.cookieRequests.length, 2);
  assert.equal(browser.runtimeCookie.token, "instance-capability-3");
  assert.ok(browser.result.expiresAt.getTime() > initialExpiry);
  assert.equal(browser.result.error, null);
  browser.unmount();
});

test("returning to a suspended tab renews an already-expired runtime cookie", async () => {
  const browser = setup();
  await browser.advance(0);
  const initialExpiry = browser.result.expiresAt.getTime();
  await browser.resumeAfter(65 * 60_000);
  assert.ok(browser.now > initialExpiry);
  assert.equal(browser.cookieRequests.length, 1);
  assert.equal(browser.runtimeCookie.token, "instance-capability-2");
  assert.ok(browser.result.expiresAt.getTime() > browser.now);
  browser.unmount();
});

test("stopped retained sessions keep their frame without renewing", async () => {
  const browser = setup();
  await browser.advance(0);
  const initialURL = browser.result.embedUrl;
  browser.stop();
  await browser.advance(2 * HOUR_MS);
  assert.equal(browser.calls.length, 1);
  assert.equal(browser.result.embedUrl, initialURL);
  browser.unmount();
});

test("explicit reload renewal still navigates the iframe with the fresh token", async () => {
  const browser = setup({ reloadOnAccessRefresh: true });
  await browser.advance(HOUR_MS);
  assert.match(browser.result.embedUrl, /[?]token=instance-capability-2$/);
  assert.equal(browser.cookieRequests.length, 0);
  browser.unmount();
});

test("cookie refresh forwards only the token query parameter to the runtime endpoint", async () => {
  const browser = setup();
  await browser.refreshDedicatedRuntimeCookie("https://opencode-42.example/session?directory=private&token=new%2Btoken#chat");
  assert.equal(browser.cookieRequests[0].url, "https://opencode-42.example/__clawmanager_access_refresh?token=new%2Btoken");
  await browser.refreshDedicatedRuntimeCookie(null);
  await browser.refreshDedicatedRuntimeCookie("https://opencode-42.example/session");
  await browser.refreshDedicatedRuntimeCookie("/api/v1/instances/42/proxy/?token=same-origin");
  assert.equal(browser.cookieRequests.length, 1);
  browser.unmount();
});
