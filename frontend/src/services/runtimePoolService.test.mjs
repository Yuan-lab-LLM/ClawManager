import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";
import axios, { AxiosError } from "axios";
import ts from "typescript";

const endpoint = "/admin/runtime-scheduling/preflight";
const request = { instance_ids: [21], target_pod_id: 34, target_slot: 1 };
const result = { allowed: true, blockers: [] };
const compiled = ts.transpileModule(
  readFileSync(new URL("./runtimePoolService.ts", import.meta.url), "utf8"),
  { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } },
).outputText;

function setup(failures = []) {
  const calls = [];
  const api = { post: async (url, body) => {
    calls.push({ url, body });
    const failure = failures[calls.length - 1];
    if (failure) throw failure;
    return { data: { data: result } };
  } };
  return { service: loadService(api), calls };
}

function loadService(api) {
  const exports = {};
  runInNewContext(compiled, {
    exports,
    require: (name) => {
      if (name === "./api") return { default: api };
      if (name === "axios") return { default: axios };
      throw new Error(`Unexpected dependency: ${name}`);
    },
  });
  return exports.runtimePoolService;
}

function networkError(config = { url: endpoint }) {
  return new AxiosError("Network Error", "ERR_NETWORK", config);
}

test("successful preflight sends one request", async () => {
  const { service, calls } = setup();
  assert.equal(await service.preflightScheduling(request), result);
  assert.deepEqual(calls, [{ url: endpoint, body: request }]);
});

test("paged inventory sends server filters and cancellation while preserving the full-inventory API", async () => {
  const calls = [];
  const page = { pods: [], pagination: { page: 2, page_size: 4, total: 9, total_pages: 3 }, summary: { pods: 30 }, nodes: ['node-a'], updated_at: '2026-10-27T00:00:00Z' };
  const api = { get: async (...args) => { calls.push(args); return { data: { data: page } }; } };
  const service = loadService(api);
  const controller = new AbortController();
  const params = { page: 2, page_size: 4, runtime_type: 'hermes', search: 'off-page owner', node: 'node-a', scheduling: 'enabled' };
  assert.equal(await service.listPodsPage(params, controller.signal), page);
  assert.equal(calls[0][0], '/admin/runtime-pods');
  assert.equal(calls[0][1].params, params);
  assert.equal(calls[0][1].signal, controller.signal);
  assert.deepEqual(await service.listPods('hermes', controller.signal), []);
  assert.equal(calls[1][0], '/admin/runtime-pods');
  assert.equal(calls[1][1].params.runtime_type, 'hermes');
  assert.equal(calls[1][1].signal, controller.signal);
});

test("maintenance and balance services preserve exact admin API payloads", async () => {
  const calls = [];
  const api = Object.fromEntries(['get', 'post', 'delete'].map(method => [method, async (...args) => { calls.push([method, ...args]); return { data: { data: result } }; }]));
  const service = loadService(api);
  const signal = new AbortController().signal;
  assert.equal(await service.listManagedPools(signal), result);
  assert.equal(calls[0][1], "/admin/runtime-pod-pools");
  assert.equal(calls[0][2].signal, signal);
  assert.equal(await service.listScaleOptions(signal), result);
  assert.equal(calls[1][1], '/admin/runtime-pods/scale-options');
  assert.equal(calls[1][2].signal, signal);
  const create = { runtime_type: 'hermes', pod_count: 3 };
  assert.equal(await service.createPod(create), result);
  await service.deletePod(11, 'pod-uid');
  await service.deletePodPool('new-pool', 'runtime', 'deployment-uid');
  await service.planBalance('all');
  await service.executeBalance('hermes', 'fingerprint');
  assert.deepEqual(JSON.parse(JSON.stringify(calls.slice(2))), [
    ['post', '/admin/runtime-pods', create],
    ['delete', '/admin/runtime-pods/11', { data: { pod_uid: 'pod-uid' } }],
    ['delete', '/admin/runtime-pod-pools/new-pool', { data: { namespace: 'runtime', deployment_uid: 'deployment-uid' } }],
    ['post', '/admin/runtime-balance/plan', { runtime_type: 'all' }],
    ['post', '/admin/runtime-balance', { runtime_type: 'hermes', plan_fingerprint: 'fingerprint' }],
  ]);
});

test("create, delete and balance execution network errors never retry mutations", async () => {
  for (const invoke of [
    service => service.createPod({ runtime_type: 'hermes', pod_count: 3 }),
    service => service.scaleIn({ runtime_type: 'hermes', pod_count: 2, plan_fingerprint: 'plan' }),
    service => service.deletePod(11, 'pod-uid'),
    service => service.deletePodPool('new-pool', 'runtime', 'deployment-uid'),
    service => service.executeBalance('all', 'fingerprint'),
  ]) {
    let calls = 0;
    const failure = networkError();
    const api = { post: async () => { calls++; throw failure; }, delete: async () => { calls++; throw failure; } };
    await assert.rejects(invoke(loadService(api)), error => error === failure);
    assert.equal(calls, 1);
  }
});

test("one transient preflight network error retries the same body once", async () => {
  const { service, calls } = setup([networkError()]);
  assert.equal(await service.preflightScheduling(request), result);
  assert.deepEqual(calls, [{ url: endpoint, body: request }, { url: endpoint, body: request }]);
  assert.equal(calls[0].body, calls[1].body);
});

test("two preflight network errors stop after the second request and preserve its error", async () => {
  const second = networkError();
  const { service, calls } = setup([networkError(), second]);
  await assert.rejects(service.preflightScheduling(request), error => error === second);
  assert.equal(calls.length, 2);
});

test("HTTP, authentication, cancellation and timeout failures are never retried", async () => {
  const failures = [
    ...[401, 403, 500].map(status => new AxiosError("HTTP error", "ERR_BAD_RESPONSE", { url: endpoint }, undefined, { status })),
    new AxiosError("cancelled", "ERR_CANCELED", { url: endpoint }),
    new AxiosError("timeout", "ECONNABORTED", { url: endpoint }),
    new AxiosError("timeout", "ETIMEDOUT", { url: endpoint }),
    networkError({ url: "/api/v1/auth/refresh" }),
    networkError({ url: endpoint, _retry: true }),
    networkError({}),
    new AxiosError("Network Error", "ERR_NETWORK", undefined),
    new AxiosError("Network Error", "ERR_NETWORK", { url: endpoint }, undefined, { status: 500 }),
    Object.assign(new Error("Network Error"), { code: "ERR_NETWORK", config: { url: endpoint } }),
  ];
  for (const failure of failures) {
    const { service, calls } = setup([failure]);
    await assert.rejects(service.preflightScheduling(request), error => error === failure);
    assert.equal(calls.length, 1, `${failure.code}: ${failure.config?.url}`);
  }
});

test("mutation network errors are never retried", async () => {
  for (const invoke of [
    service => service.schedule(request),
    service => service.evict(32, { mode: "graceful", grace_period_seconds: 30, concurrency: 1 }),
    service => service.cancelScheduling("operation"),
    service => service.setScheduling(32, false),
    service => service.drainPod(32),
    service => service.startRollout({ runtime_type: "hermes", target_image_ref: "image", batch_size: 1, max_unavailable: 1 }),
  ]) {
    const failure = networkError();
    const { service, calls } = setup([failure]);
    await assert.rejects(invoke(service), error => error === failure);
    assert.equal(calls.length, 1);
  }
});

test("queued 401 authentication replay never triggers a third preflight after a network error", async () => {
  let releaseRefresh;
  const pendingRefresh = new Promise(resolve => { releaseRefresh = resolve; });
  let notifyRefreshStarted;
  const refreshStarted = new Promise(resolve => { notifyRefreshStarted = resolve; });
  let preflightCalls = 0;
  let refreshCalls = 0;
  const transport = async config => {
    if (config.url === endpoint) {
      preflightCalls++;
      if (preflightCalls > 1) throw networkError(config);
    } else if (config._retry) {
      return { data: {}, status: 200, statusText: "OK", headers: {}, config };
    }
    throw new AxiosError("Unauthorized", "ERR_BAD_REQUEST", config, undefined, { status: 401 });
  };
  const boundary = {
    create: config => axios.create({ ...config, adapter: transport }),
    post: async url => {
      assert.equal(url, "/api/v1/auth/refresh");
      refreshCalls++;
      notifyRefreshStarted();
      await pendingRefresh;
      return { data: { data: { access_token: "fixture-access", refresh_token: "fixture-refresh" } } };
    },
  };
  const apiSource = readFileSync(new URL("./api.ts", import.meta.url), "utf8")
    .replace("import.meta.env.VITE_API_URL", "undefined");
  const apiCompiled = ts.transpileModule(apiSource, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const exports = {};
  const window = { location: { href: "/runtime" } };
  runInNewContext(apiCompiled, {
    exports,
    localStorage: { getItem: () => "fixture-token", setItem() {}, removeItem() {} },
    window,
    require: name => {
      assert.equal(name, "axios");
      return { default: boundary };
    },
  });
  const api = exports.default;
  const first = api.get("/refresh-trigger");
  await refreshStarted;
  const rejectedPreflight = assert.rejects(
    loadService(api).preflightScheduling(request),
    error => error.code === "ERR_NETWORK",
  );
  // Drain the real Axios interceptor chain so the preflight joins the refresh queue.
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(preflightCalls, 1);
  releaseRefresh();
  await Promise.all([first, rejectedPreflight]);
  assert.equal(preflightCalls, 2, "The queued 401 replay is the final preflight attempt");
  assert.equal(refreshCalls, 1);
  assert.equal(window.location.href, "/runtime");
});


test("scale-in preflight and execution preserve the requested count and plan fingerprint", async () => {
  const calls = [];
  const api = { post: async (...args) => { calls.push(args); return { data: { data: result } }; } };
  const service = loadService(api);
  const request = { runtime_type: 'hermes', pod_count: 2 };
  assert.equal(await service.preflightScaleIn(request), result);
  assert.equal(await service.scaleIn({ ...request, plan_fingerprint: 'reviewed-plan' }), result);
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [
    ['/admin/runtime-pods/scale-in/preflight', request],
    ['/admin/runtime-pods/scale-in', { ...request, plan_fingerprint: 'reviewed-plan' }],
  ]);
});

test("automatic stop services preserve settings version, disabled flag and response envelopes", async () => {
  const calls = [];
  const settings = { enabled: true, check_interval_days: 2, idle_timeout_days: 7, version: 5, next_check_at: '2026-11-03T20:00:00Z' };
  const activity = { instance_id: 21, auto_stop_disabled: true, monitoring_since: '2026-11-01T00:00:00Z' };
  const api = {
    get: async (...args) => { calls.push(['get', ...args]); return { data: { data: settings } }; },
    put: async (...args) => { calls.push(['put', ...args]); return { data: { data: args[0].includes('/instances/') ? activity : settings } }; },
  };
  const service = loadService(api);
  const signal = new AbortController().signal;
  assert.equal(await service.getAutoStopSettings(signal), settings);
  const update = { enabled: false, check_interval_days: 2, idle_timeout_days: 7, version: 5 };
  assert.equal(await service.updateAutoStopSettings(update), settings);
  assert.equal(await service.setInstanceAutoStopDisabled(21, true), activity);
  assert.equal(calls[0][1], '/admin/runtime-auto-stop');
  assert.equal(calls[0][2].signal, signal);
  assert.deepEqual(JSON.parse(JSON.stringify(calls.slice(1))), [
    ['put', '/admin/runtime-auto-stop', update],
    ['put', '/admin/instances/21/auto-stop', { auto_stop_disabled: true }],
  ]);
});

test("automatic stop conflicts and mutation network failures propagate without retry", async () => {
  for (const invoke of [
    service => service.updateAutoStopSettings({ enabled: true, check_interval_days: 1, idle_timeout_days: 1, version: 1 }),
    service => service.setInstanceAutoStopDisabled(21, true),
  ]) {
    for (const failure of [networkError(), new AxiosError('Refresh and retry', 'ERR_BAD_REQUEST', {}, undefined, { status: 409 })]) {
      let calls = 0;
      const api = { put: async () => { calls++; throw failure; } };
      await assert.rejects(invoke(loadService(api)), error => error === failure);
      assert.equal(calls, 1);
    }
  }
});

test('manual recovery preserves exact generation and Pod UID without retrying mutations', async () => {
  const calls = [];
  const api = Object.fromEntries(['get', 'post'].map(method => [method, async (...args) => { calls.push([method, ...args]); return { data: { data: result } }; }]));
  const service = loadService(api);
  await service.inspectInstanceRecovery(7);
  await service.retryInstanceRecovery(7, 3);
  await service.recheckPod(9, 'exact-uid');
  await service.retryOperation('operation/1');
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [
    ['get', '/admin/instances/7/runtime-recovery'],
    ['post', '/admin/instances/7/runtime-recovery', { generation: 3 }],
    ['post', '/admin/runtime-pods/9/recheck', { pod_uid: 'exact-uid' }],
    ['post', '/admin/runtime-scheduling/operation%2F1/retry'],
  ]);
  for (const invoke of [
    s => s.retryInstanceRecovery(7, 3), s => s.recheckPod(9, 'exact-uid'), s => s.retryOperation('op'),
  ]) {
    const { service: failing, calls: failed } = setup([new Error('network response lost')]);
    await assert.rejects(() => invoke(failing), /network response lost/);
    assert.equal(failed.length, 1);
  }
});


test('fault recovery keeps the existing eviction endpoint and exact instance scope', async () => {
  const calls = [];
  const service = loadService({ post: async (...args) => { calls.push(args); return { data: { data: result } }; } });
  for (const body of [{ mode: 'recovery', instance_ids: [17] }, { mode: 'recovery', instance_ids: [] }, { mode: 'recovery' }]) {
    assert.equal(await service.evict(41, body), result);
    assert.equal(calls.at(-1)[0], '/admin/runtime-pods/41/evict');
    assert.equal(calls.at(-1)[1], body);
  }
});
