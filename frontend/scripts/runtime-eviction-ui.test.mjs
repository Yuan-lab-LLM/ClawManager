import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';
import * as React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { AxiosError } from 'axios';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const compiled = new Map();
const source = {
  id: 41, runtime_type: 'openclaw', namespace: 'runtime', deployment_name: 'oc-u5',
  pod_name: 'oc-source', image_ref: 'image@sha256:abc', state: 'ready',
  agent_reported: true, capacity: 3, used_slots: 2, scheduling_enabled: true,
  slots: [{ index: 1, ready: true, instance_id: 1 }, { index: 2, ready: true, instance_id: 2 }],
};
const target = { ...source, id: 42, used_slots: 0, slots: [{ index: 1, ready: true }] };

// Execute production render/event logic with in-memory hook state and service boundaries.
function setup({ pod = source, pods = [pod], failure, forceSupported = true, component = 'RuntimeActions', realLayout = false, kind = 'evict', initialIds, preflight, templates, scaleOptions, scaleState, scaleRefetch, create, balancePlan, historyItems = [] } = {}) {
  const states = [];
  const effects = [];
  const timers = new Map();
  const queries = [];
  const invalidations = [];
  const inventoryCalls = [];
  const preflightRequests = [];
  const scheduleRequests = [];
  const operationCalls = [];
  const refetches = [];
  let changes = 0;
  let submitted = 0;
  let fullPods = pods;
  let fullState = { isSuccess: true, isFetching: false };
  let websocketRefresh;
  let timerId = 0;
  let cursor = 0;
  let calls = 0;
  const modules = new Map();
  const react = { ...React, useCallback: fn => fn, useRef: initial => {
    const index = cursor++;
    return states[index] ??= { current: initial };
  }, useEffect: (effect, deps = []) => {
    const index = cursor++;
    const previous = states[index];
    if (!previous || deps.some((value, i) => !Object.is(value, previous.deps[i]))) effects.push(() => {
      previous?.cleanup?.();
      states[index] = { deps, cleanup: effect() };
    });
  }, useState: initial => {
    const index = cursor++;
    if (!(index in states)) states[index] = typeof initial === 'function' ? initial() : initial;
    return [states[index], value => { states[index] = typeof value === 'function' ? value(states[index]) : value; }];
  } };
  const wrapper = ({ children, footer }) => React.createElement('div', null, children, footer);
  const empty = () => null;
  const ui = { RuntimeDialog: wrapper, ErrorMessage: wrapper, Metric: empty, ResourceBar: empty, SlotGrid: empty, Legend: empty };
  const summary = { pods: 30, online: 29, schedulable: 28, capacity: 100, ready: 99, occupied: 20, available: 79, running: 20, starting: 0, migrating: 0, error: 0, idle: 79, unavailable: 1 };
  const inventory = { pods, summary, nodes: ['off-page-node'], pagination: { page: 1, page_size: 4, total: 30, total_pages: 8 }, updated_at: '2026-10-27T00:00:00Z' };
  const service = {
    evict: async () => { calls++; if (failure) throw failure; },
    preflightScheduling: async request => { preflightRequests.push(request); return preflight ? preflight(request) : { allowed: true, blockers: [] }; },
    schedule: async request => { scheduleRequests.push(request); },
    listPodsPage: async (...args) => { inventoryCalls.push(args); return inventory; },
    listPods: async () => pods,
    createPod: async request => { operationCalls.push(['createPod', request]); if (failure) throw failure; if (create) return create(request); return { runtime_type: request.runtime_type, namespace: 'runtime', deployment_name: 'default-pool', deployment_uid: 'default-uid', previous_replicas: 2, added_pods: request.pod_count, replicas: 2 + request.pod_count, slot_count: 3 }; },
    deletePod: async (...args) => { operationCalls.push(['deletePod', ...args]); if (failure) throw failure; },
    deletePodPool: async (...args) => { operationCalls.push(['deletePodPool', ...args]); if (failure) throw failure; },
    planBalance: async type => { operationCalls.push(['planBalance', type]); return typeof balancePlan === 'function' ? balancePlan(type) : balancePlan; },
    executeBalance: async (...args) => { operationCalls.push(['executeBalance', ...args]); if (failure) throw failure; return { id: 'balance-operation', kind: 'balance', items: [] }; },
  };
  const client = { invalidateQueries: (...args) => { invalidations.push(args); return Promise.resolve(); } };
  let translations;
  const t = (key, values) => translations.interpolate(translations.translate('en', key) || key, values);
  function load(url) {
    if (modules.has(url.href)) return modules.get(url.href);
    if (!compiled.has(url.href)) compiled.set(url.href, ts.transpileModule(readFileSync(url, 'utf8'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
    }).outputText);
    const exports = {};
    runInNewContext(compiled.get(url.href), { exports,
      setTimeout: (callback, delay) => { const id = ++timerId; timers.set(id, { callback, delay }); return id; },
      clearTimeout: id => timers.delete(id),
      require: name => {
      if (name.endsWith('.css')) return {};
      if (name === 'react') return react;
      if (name.endsWith('/I18nContext')) return { useI18n: () => ({ t }) };
      if (name.endsWith('/runtimePoolService')) return { runtimePoolService: service };
      if (name.endsWith('/RuntimeManagementUI')) return ui;
      if (name.endsWith('/AdminLayout') && !realLayout) return { default: wrapper };
      if (name.endsWith('/AuthContext')) return { useAuth: () => ({ user: { username: 'fixture-admin', role: 'admin' }, logout() {} }) };
      if (name.endsWith('/uiStore')) return { useUiStore: () => ({ sidebarCollapsed: false, toggleSidebarCollapsed() {} }) };
      if (name.endsWith('/AppVersion') || name.endsWith('/LanguageSwitcher')) return { default: empty };
      if (name.endsWith('/protection/_data')) return { CATEGORIES: [] };
      if (name === 'react-router-dom') return { Link: ({ to, children, ...props }) => React.createElement('a', { ...props, href: to }, children), useLocation: () => ({ pathname: '/admin/runtime-pods' }), useNavigate: () => () => {} };
      if (name.endsWith('/useWebSocket')) return { useRuntimeAdminWebSocket: refresh => { websocketRefresh = refresh; return { isConnected: true }; } };
      if (name === '@tanstack/react-query') return {
        useQuery: options => {
          queries.push(options);
          return { refetch: async () => { refetches.push(options.queryKey); if (options.queryKey[0] === 'runtime-pod-scale-options' && scaleRefetch) return scaleRefetch(); }, ...(options.queryKey[1] === 'all' ? fullState : options.queryKey[0] === 'runtime-pod-scale-options' ? scaleState : {}), data: options.enabled === false ? undefined : options.queryKey[0] === 'runtime-pod-pools' ? { items: templates.managed_pools ?? [] } : options.queryKey[0] === 'runtime-pod-scale-options' ? scaleOptions : options.queryKey[0] === 'runtime-pods'
            ? options.queryKey[1] === 'page' ? inventory : fullPods
            : options.queryKey[0] === 'runtime-pod-events' ? [] : { supported_modes: forceSupported ? ['force'] : [], items: historyItems } };
        },
        keepPreviousData: data => data,
        useQueryClient: () => client,
      };
      if (!name.startsWith('.')) return require(name);
      const extension = ['.ts', '.tsx', '/index.ts'].find(ext => existsSync(new URL(name + ext, url)));
      return load(new URL(name + extension, url));
    } });
    modules.set(url.href, exports);
    return exports;
  }
  translations = load(new URL('../src/lib/i18n.ts', import.meta.url));
  const Component = load(new URL(`../src/pages/admin/${component}.tsx`, import.meta.url)).default;
  const props = { kind, pod: pod ?? undefined, pods, initialIds, onClose() {}, onChanged() { changes++; }, onSubmitted() { submitted++; }, onRetry() {}, onBack() {}, onSchedule() {}, onPause() {}, onEvents() {}, busy: false };
  const render = () => { cursor = 0; const tree = Component(props); effects.splice(0).forEach(effect => effect()); return tree; };
  return { render, calls: () => calls, queries, inventoryCalls, invalidations, timers, preflightRequests, scheduleRequests, operationCalls, refetches, changed: () => changes, submitted: () => submitted,
    updateProps: values => Object.assign(props, values),
    scaleOptions: (value, state) => { scaleOptions = value; scaleState = state; },
    refresh: () => websocketRefresh(),
    runTimers: () => { const pending = [...timers.values()]; timers.clear(); pending.forEach(timer => timer.callback()); },
    cleanup: () => states.forEach(state => state?.cleanup?.()),
    fullInventory: (value, state = { isSuccess: true, isFetching: false }) => { fullPods = value; fullState = state; },
  };
}

function find(node, match) {
  if (Array.isArray(node)) return node.flatMap(item => find(item, match));
  if (!node || typeof node !== 'object') return [];
  return [...(match(node) ? [node] : []), ...find(node.props?.children, match), ...find(node.props?.footer, match)];
}
const button = (tree, label) => find(tree, node => node.type === 'button' && renderToStaticMarkup(node).includes(label))[0];
const input = (tree, type, value) => find(tree, node => node.type === 'input' && node.props.type === type && (!value || node.props.value === value))[0];
const acknowledge = view => input(view.render(), 'checkbox').props.onChange({ target: { checked: true } });

test('graceful eviction requires enough compatible empty slots for every bound instance', () => {
  for (const [targets, available] of [
    [[], 0], [[target], 1], [[target, { ...target, id: 43 }], 2],
    [[{ ...target, deployment_name: 'other' }], 1],
    [[{ ...target, image_ref: 'different@sha256:def' }], 0],
  ]) {
    const view = setup({ pods: [source, ...targets] });
    acknowledge(view);
    const tree = view.render();
    assert.equal(button(tree, 'Start eviction').props.disabled, available < 2);
    if (available < 2) {
      const markup = renderToStaticMarkup(tree);
      assert.match(markup, new RegExp(`Requires 2 compatible empty slots; ${available} available`));
      assert.match(markup, /will not stop instances/);
      assert.match(markup, /explicitly choose force eviction/);
    }
  }
});

test('force eviction remains an explicit supported choice and resets acknowledgement', () => {
  const view = setup();
  acknowledge(view);
  assert.equal(input(view.render(), 'radio', 'force').props.checked, false);
  input(view.render(), 'radio', 'force').props.onChange();
  assert.equal(button(view.render(), 'Start eviction').props.disabled, true);
  acknowledge(view);
  assert.equal(button(view.render(), 'Start eviction').props.disabled, false);
  assert.equal(input(setup({ forceSupported: false }).render(), 'radio', 'force').props.disabled, true);
});

test('eviction transport failure stays uncertain, never retries, and clears on acknowledgement or mode change', async () => {
  for (const code of ['ERR_NETWORK', 'ECONNABORTED', 'ETIMEDOUT']) {
    const view = setup({ failure: new AxiosError('Network Error', code) });
    input(view.render(), 'radio', 'force').props.onChange();
    acknowledge(view);
    button(view.render(), 'Start eviction').props.onClick();
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(view.calls(), 1);
    assert.match(renderToStaticMarkup(view.render()), /No server response was received; the outcome is unconfirmed\. Check scheduling records before retrying/);
    input(view.render(), 'checkbox').props.onChange({ target: { checked: false } });
    assert.doesNotMatch(renderToStaticMarkup(view.render()), /outcome is unconfirmed/);
    acknowledge(view);
    button(view.render(), 'Start eviction').props.onClick();
    await new Promise(resolve => setImmediate(resolve));
    input(view.render(), 'radio', 'graceful').props.onChange();
    assert.doesNotMatch(renderToStaticMarkup(view.render()), /outcome is unconfirmed/);
    assert.equal(input(view.render(), 'checkbox').props.checked, false);
  }
});

test('a real eviction 409 preserves its domain error', async () => {
  const reason = 'no ready compatible destination capacity; Pod remains scheduling paused';
  const view = setup({ pods: [source, target, { ...target, id: 43 }], failure: new AxiosError('Conflict', 'ERR_BAD_REQUEST', {}, undefined, { status: 409, data: { error: reason } }) });
  acknowledge(view);
  button(view.render(), 'Start eviction').props.onClick();
  await new Promise(resolve => setImmediate(resolve));
  const markup = renderToStaticMarkup(view.render());
  assert.match(markup, new RegExp(reason));
  assert.doesNotMatch(markup, /outcome is unconfirmed/);
  assert.equal(view.calls(), 1);
});

test('both card and detail allow enabled or paused upgrade targets without activating reserved pools', () => {
  for (const component of ['RuntimePodsPage', 'RuntimePodDetails']) {
    for (const [extra, allowed] of [
      [{}, true],
      [{ pool_role: 'upgrade-target', scheduling_enabled: true }, true],
      [{ pool_role: 'upgrade-target', scheduling_enabled: false, scheduling_paused: true }, true],
      [{ pool_role: 'upgrade-target', scheduling_enabled: false }, false],
      [{ pool_role: 'upgrade-lab', scheduling_enabled: true, scheduling_paused: true }, false],
      [{ pool_role: 'unknown', scheduling_enabled: true, scheduling_paused: true }, false],
      [{ pool_purpose: 'openclaw-upgrade-lab' }, false],
      [{ agent_reported: false }, false], [{ draining: true }, false], [{ id: 0 }, false],
    ]) {
      const pod = { ...source, ...extra };
      const label = pod.scheduling_enabled === false || pod.scheduling_paused ? 'Resume scheduling' : 'Pause scheduling';
      assert.equal(Boolean(button(setup({ component, pod }).render(), label).props.disabled), !allowed, `${component}: ${JSON.stringify(extra)}`);
    }
  }
});

test('overview requests only a server page, retains previous data and searches the whole inventory after debounce', async () => {
  const pods = Array.from({ length: 16 }, (_, i) => ({ ...source, id: i + 1, pod_name: `pod-${i}` }));
  const view = setup({ component: 'RuntimePodsPage', pods });
  let tree = view.render();
  assert.doesNotMatch(renderToStaticMarkup(tree), /Runtime resources &amp; scheduling/);
  assert.equal(find(tree, node => node.type === 'article').length, 16);
  const pageQuery = () => view.queries.findLast(query => query.queryKey[1] === 'page');
  assert.equal(pageQuery().queryKey[2].page_size, 16);
  assert.equal(pageQuery().refetchInterval, 3000);
  assert.equal(pageQuery().refetchOnWindowFocus, 'always');
  assert.equal(pageQuery().refetchOnReconnect, 'always');
  assert.equal(pageQuery().placeholderData('previous'), 'previous');
  assert.equal(view.queries.findLast(query => query.queryKey[1] === 'all').enabled, false);
  const signal = new AbortController().signal;
  await pageQuery().queryFn({ signal });
  assert.equal(view.inventoryCalls[0][1], signal);
  button(tree, 'Next page').props.onClick();
  tree = view.render();
  assert.equal(pageQuery().queryKey[2].page, 2);
  find(tree, node => node.type === 'input' && node.props['aria-label'] === 'Search Pods, instances or owners')[0].props.onChange({ target: { value: 'off-page owner' } });
  view.render();
  assert.equal(pageQuery().queryKey[2].search, '');
  assert.ok([...view.timers.values()].some(timer => timer.delay >= 200 && timer.delay <= 300));
  view.runTimers();
  tree = view.render();
  assert.equal(pageQuery().queryKey[2].search, 'off-page owner');
  assert.equal(pageQuery().queryKey[2].page, 1);
  assert.match(renderToStaticMarkup(tree), /off-page-node/);
  button(tree, 'Batch scheduling').props.onClick();
  view.render();
  assert.equal(view.queries.findLast(query => query.queryKey[1] === 'all').enabled, true);
});

test('WebSocket bursts coalesce without cancelling inventory fetches and timers clean up', () => {
  const view = setup({ component: 'RuntimePodsPage' });
  view.render();
  for (let i = 0; i < 20; i++) view.refresh();
  assert.equal(view.timers.size, 1);
  assert.equal(view.invalidations.length, 0);
  view.runTimers();
  assert.equal(view.invalidations.length, 2);
  for (const [, options] of view.invalidations) assert.equal(options.cancelRefetch, false);
  view.refresh();
  assert.equal(view.timers.size, 1);
  view.cleanup();
  assert.equal(view.timers.size, 0);
});

test('production AdminLayout mounts one shared main and sixteen Runtime cards', () => {
  const pods = Array.from({ length: 16 }, (_, i) => ({ ...source, id: i + 1, pod_name: `pod-${i}` }));
  const view = setup({ component: 'RuntimePodsPage', pods, realLayout: true });
  const markup = renderToStaticMarkup(view.render());
  assert.equal((markup.match(/<main(?:\s|>)/g) || []).length, 1);
  assert.equal((markup.match(/<article(?:\s|>)/g) || []).length, 16);
});

test('detail uses its snapshot while loading or failed, but a successful deletion returns to the overview', () => {
  const view = setup({ component: 'RuntimePodsPage' });
  find(view.render(), node => node.type === 'article')[0].props.onClick();
  view.fullInventory(undefined, { isSuccess: false, isFetching: true });
  assert.match(renderToStaticMarkup(view.render()), /Back to overview/);
  view.fullInventory([], { isSuccess: false, isFetching: false, isError: true, error: new Error('fixture unavailable') });
  assert.match(renderToStaticMarkup(view.render()), /Back to overview/);
  view.fullInventory([]);
  view.render();
  const tree = view.render();
  assert.doesNotMatch(renderToStaticMarkup(tree), /Back to overview/);
  assert.equal(find(tree, node => node.type === 'article').length, 1);
  assert.equal(view.queries.findLast(query => query.queryKey[1] === 'page').enabled, true);
});

test('Pod dialogs await fresh inventory and close only on a confirmed source deletion', () => {
  for (const label of ['Evict instances', 'Event log']) {
    const view = setup({ component: 'RuntimePodsPage' });
    button(view.render(), label).props.onClick({ currentTarget: { closest: () => ({ hidePopover() {} }) } });
    view.fullInventory(undefined, { isSuccess: false, isFetching: true });
    const modal = tree => find(tree, node => typeof node.props?.onClose === 'function');
    assert.equal(modal(view.render()).length, 1);
    assert.equal(view.queries.findLast(query => query.queryKey[1] === 'all').enabled, true);
    view.fullInventory(undefined, { isSuccess: false, isFetching: false, isError: true, error: new Error('fixture unavailable') });
    assert.equal(modal(view.render()).length, 1);
    view.fullInventory([]);
    view.render();
    assert.equal(modal(view.render()).length, 0);
    assert.equal(view.queries.findLast(query => query.queryKey[1] === 'all').enabled, false);
  }
});

const hermesSource = { ...source, id: 51, runtime_type: 'hermes', deployment_name: 'hermes-v1', slots: [
  { index: 1, ready: true, instance_id: 3, instance_name: 'hermes-one' },
  { index: 2, ready: true, instance_id: 4, instance_name: 'hermes-two' },
] };
const schedulePods = [
  { ...source, slots: source.slots.map((slot, i) => ({ ...slot, instance_name: `oc-${i + 1}` })) },
  { ...target, slots: [{ index: 1, ready: true }, { index: 2, ready: true }] },
  hermesSource,
  { ...hermesSource, id: 52, used_slots: 0, slots: [{ index: 1, ready: true }, { index: 2, ready: true }] },
];
const select = (tree, label) => find(tree, node => node.type === 'select' && node.props['aria-label'] === label)[0];
const chooseType = (view, value) => select(view.render(), 'Choose runtime type').props.onChange({ target: { value } });
const chooseTarget = (view, value) => select(view.render(), 'Choose a compatible Pod').props.onChange({ target: { value: String(value) } });
const chooseInstance = (view, index, checked = true) => find(view.render(), node => node.type === 'input' && node.props.type === 'checkbox')[index].props.onChange({ target: { checked } });
const tick = () => new Promise(resolve => setImmediate(resolve));

test('cross-deployment manual candidates require successful server preflight before confirmation', async () => {
  for (const allowed of [false, true]) {
    const managed = { ...target, deployment_name: 'independent-managed-copy', pod_name: 'managed-copy-pod' };
    const view = setup({ kind: 'schedule', pod: source, pods: [source, managed], preflight: async () => ({ allowed, blockers: allowed ? [] : ['target workspace storage identity differs'] }) });
    chooseInstance(view, 0);
    const options = find(select(view.render(), 'Choose a compatible Pod'), node => node.type === 'option');
    assert.ok(options.some(option => String(option.props.value) === String(managed.id)), 'a different controller must not hide a compatible candidate');
    chooseTarget(view, managed.id);
    assert.equal(button(view.render(), 'Confirm scheduling').props.disabled, true);
    button(view.render(), 'Validate target').props.onClick(); await tick();
    assert.equal(view.preflightRequests.length, 1);
    assert.equal(button(view.render(), 'Confirm scheduling').props.disabled, !allowed);
    button(view.render(), 'Confirm scheduling').props.onClick(); await tick();
    assert.equal(view.scheduleRequests.length, allowed ? 1 : 0);
    if (!allowed) assert.match(renderToStaticMarkup(view.render()), /target workspace storage identity differs/);
  }
});

test('global manual scheduling requires type first and shows only that type with same-type multi-select', async () => {
  const view = setup({ kind: 'schedule', pod: null, pods: schedulePods });
  const initial = view.render();
  assert.equal(find(initial, node => node.type === 'input' && node.props.type === 'checkbox').length, 0);
  assert.match(renderToStaticMarkup(initial), /Choose a runtime type before selecting instances/);
  assert.doesNotMatch(renderToStaticMarkup(initial), /No compatible, schedulable target/);
  assert.equal(select(initial, 'Choose runtime type').props.value, '');
  chooseType(view, 'openclaw');
  const filtered = view.render();
  assert.equal(find(filtered, node => node.type === 'input' && node.props.type === 'checkbox').length, 2);
  assert.doesNotMatch(renderToStaticMarkup(filtered), /hermes-one|hermes-two/);
  chooseInstance(view, 0);
  chooseInstance(view, 1);
  chooseTarget(view, 42);
  assert.equal(button(view.render(), 'Validate target').props.disabled, false);
  button(view.render(), 'Validate target').props.onClick();
  await tick();
  assert.equal(view.preflightRequests.length, 1);
  assert.equal(JSON.stringify(view.preflightRequests[0].instance_ids), '[1,2]');
  const markup = renderToStaticMarkup(view.render());
  for (const step of ['1 · Select runtime type', '2 · Select instances', '3 · Select target', '4 · Check and confirm']) assert.ok(markup.includes(step));
});

test('changing runtime type clears selection, target, slot, validation, and stale submit callbacks', async () => {
  const view = setup({ kind: 'schedule', pod: null, pods: schedulePods });
  chooseType(view, 'openclaw'); chooseInstance(view, 0); chooseTarget(view, 42);
  select(view.render(), 'Target slot').props.onChange({ target: { value: '2' } });
  button(view.render(), 'Validate target').props.onClick();
  await tick();
  const oldConfirm = button(view.render(), 'Confirm scheduling');
  assert.equal(oldConfirm.props.disabled, false);
  chooseType(view, 'hermes');
  const tree = view.render();
  assert.equal(select(tree, 'Choose a compatible Pod').props.value, '');
  assert.ok(find(tree, node => node.type === 'input' && node.props.type === 'checkbox').every(node => !node.props.checked));
  assert.doesNotMatch(renderToStaticMarkup(tree), /Target checks passed|oc-1|oc-2/);
  oldConfirm.props.onClick();
  await tick();
  assert.equal(view.scheduleRequests.length, 0);
  chooseInstance(view, 0); chooseTarget(view, 52);
  assert.equal(select(view.render(), 'Target slot').props.value, '');
});

test('a preflight response arriving after a type change cannot restore validation or errors', async () => {
  let resolve;
  const pending = new Promise(done => { resolve = done; });
  const view = setup({ kind: 'schedule', pod: null, pods: schedulePods, preflight: () => pending });
  chooseType(view, 'openclaw'); chooseInstance(view, 0); chooseTarget(view, 42);
  const oldValidate = button(view.render(), 'Validate target');
  oldValidate.props.onClick();
  chooseType(view, 'hermes');
  resolve({ allowed: true, blockers: [] });
  await tick();
  oldValidate.props.onClick();
  await tick();
  assert.equal(view.preflightRequests.length, 1);
  assert.doesNotMatch(renderToStaticMarkup(view.render()), /Target checks passed/);
  assert.equal(button(view.render(), 'Confirm scheduling').props.disabled, true);
});

test('per-Pod scheduling prefills and locks type, preserves valid IDs and enforces its source context', () => {
  const offSource = { ...source, id: 61, slots: [{ index: 1, ready: true, instance_id: 8, instance_name: 'off-source-instance' }] };
  const view = setup({ kind: 'schedule', pod: schedulePods[0], pods: [...schedulePods, offSource], initialIds: [1, 2] });
  const tree = view.render();
  assert.equal(select(tree, 'Choose runtime type').props.value, 'openclaw');
  assert.equal(select(tree, 'Choose runtime type').props.disabled, true);
  assert.equal(find(tree, node => node.type === 'input' && node.props.type === 'checkbox' && node.props.checked).length, 2);
  chooseType(view, 'hermes');
  assert.equal(select(view.render(), 'Choose runtime type').props.value, 'openclaw');
  assert.doesNotMatch(renderToStaticMarkup(view.render()), /hermes-one|hermes-two|off-source-instance/);
  assert.equal(find(view.render(), node => node.type === 'input' && node.props.type === 'checkbox').length, 2);
});

test('mixed initial IDs fail closed and never send preflight or scheduling requests', async () => {
  for (const pod of [null, schedulePods[0]]) {
    const view = setup({ kind: 'schedule', pod, pods: schedulePods, initialIds: [1, 3] });
    assert.equal(button(view.render(), 'Validate target').props.disabled, true);
    const validate = button(view.render(), 'Validate target');
    const confirm = button(view.render(), 'Confirm scheduling');
    validate.props.onClick();
    confirm.props.onClick();
    await tick();
    assert.equal(view.preflightRequests.length, 0);
    assert.equal(view.scheduleRequests.length, 0);
    assert.ok(find(view.render(), node => node.type === 'input' && node.props.type === 'checkbox').every(node => !node.props.checked));
  }
});

test('late initial inventory infers type once and never overwrites a subsequent explicit choice', () => {
  const view = setup({ kind: 'schedule', pod: null, pods: [], initialIds: [3] });
  assert.equal(select(view.render(), 'Choose runtime type').props.value, '');
  view.updateProps({ pods: schedulePods });
  view.render();
  assert.equal(select(view.render(), 'Choose runtime type').props.value, 'hermes');
  assert.equal(find(view.render(), node => node.type === 'input' && node.props.type === 'checkbox' && node.props.checked).length, 1);
  chooseType(view, 'openclaw');
  view.updateProps({ pods: [...schedulePods] });
  assert.equal(select(view.render(), 'Choose runtime type').props.value, 'openclaw');
  const explicit = setup({ kind: 'schedule', pod: null, pods: [], initialIds: [3] });
  chooseType(explicit, 'openclaw');
  explicit.updateProps({ pods: schedulePods });
  assert.equal(select(explicit.render(), 'Choose runtime type').props.value, 'openclaw');
});

test('changing type clears a previous validation error and discards a late error from the old type', async () => {
  const failure = new AxiosError('Conflict', 'ERR_BAD_REQUEST', {}, undefined, { status: 409, data: { error: 'old-type-blocker' } });
  const view = setup({ kind: 'schedule', pod: null, pods: schedulePods, preflight: async () => { throw failure; } });
  chooseType(view, 'openclaw'); chooseInstance(view, 0); chooseTarget(view, 42);
  button(view.render(), 'Validate target').props.onClick();
  await tick();
  assert.match(renderToStaticMarkup(view.render()), /old-type-blocker/);
  chooseType(view, 'hermes');
  assert.doesNotMatch(renderToStaticMarkup(view.render()), /old-type-blocker/);
  let reject;
  const pending = setup({ kind: 'schedule', pod: null, pods: schedulePods, preflight: () => new Promise((_resolve, fail) => { reject = fail; }) });
  chooseType(pending, 'openclaw'); chooseInstance(pending, 0); chooseTarget(pending, 42);
  button(pending.render(), 'Validate target').props.onClick();
  chooseType(pending, 'hermes');
  reject(failure);
  await tick();
  assert.doesNotMatch(renderToStaticMarkup(pending.render()), /old-type-blocker/);
});

const budget = { cpu_request: '100m', memory_request: '128Mi', cpu_limit: '500m', memory_limit: '512Mi' };
const template = { template_pod_id: 41, runtime_type: 'openclaw', namespace: 'runtime', deployment_name: 'native-pool', image_ref: 'image@sha256:abc', image_digest: 'sha256:abc', slot_count: 3, pod_resources: { cpu_request: '1', memory_request: '2Gi', cpu_limit: '2', memory_limit: '4Gi' }, coordinator_resources: budget, slot_resources: budget };
const managed = { namespace: 'runtime', deployment_name: 'managed-pool', deployment_uid: 'deployment-uid', replicas: 1 };
const templateData = { items: [template, { ...template, template_pod_id: 51, runtime_type: 'hermes' }], managed_pools: [managed], limits: { min_slots: 1, max_slots: 256 } };
const field = (tree, label) => find(tree, node => node.type === 'input' && node.props['aria-label'] === label)[0];
const change = (view, label, value) => field(view.render(), label).props.onChange({ target: { value } });
const formSubmit = (view, label) => find(view.render(), node => node.type === 'form' && node.props['aria-label'] === label)[0].props.onSubmit({ preventDefault() {} });

const scaleData = { items: [{ ...template, deployment_uid: 'default-uid', replicas: 2 }, { ...template, runtime_type: 'hermes', deployment_name: 'hermes-default', deployment_uid: 'hermes-uid', replicas: 4, slot_count: 8 }], limits: { min_pods: 1, max_pods: 10 } };

test('maintenance adds Pods to a default deployment with exactly type and additional count editable', async () => {
  const view = setup({ component: 'RuntimeMaintenanceDialog', scaleOptions: scaleData, templates: templateData });
  let tree = view.render();
  assert.equal(find(tree, node => node.type === 'select' || node.type === 'input').length, 2);
  assert.equal(field(tree, 'Additional Pods').props.value, '1');
  assert.equal(find(tree, node => node.type === 'button' && node.props.form === 'runtime-create-pod')[0].props.disabled, true);
  assert.equal(view.queries.find(query => query.queryKey[0] === 'runtime-pod-pools').enabled, false);
  assert.equal(view.queries.find(query => query.queryKey[1] === 'all').enabled, false);
  select(tree, 'Runtime type').props.onChange({ target: { value: 'openclaw' } });
  tree = view.render();
  assert.match(renderToStaticMarkup(tree), /3 slots per Pod/);
  assert.doesNotMatch(renderToStaticMarkup(tree), /sha256|default-uid|Runtime template|budget/);
  for (const value of ['', '0', '-1', '11', '1.5']) {
    change(view, 'Additional Pods', value); formSubmit(view, 'Add Pods');
    assert.equal(view.operationCalls.length, 0);
  }
  change(view, 'Additional Pods', '4');
  assert.match(renderToStaticMarkup(view.render()), /12 additional slots/);
  formSubmit(view, 'Add Pods'); await tick();
  assert.deepEqual(JSON.parse(JSON.stringify(view.operationCalls)), [['createPod', { runtime_type: 'openclaw', pod_count: 4 }]]);
  assert.equal(view.changed(), 1);
  assert.ok(view.refetches.some(key => key[0] === 'runtime-pod-scale-options'));
});

test('maintenance rejects unavailable defaults and does not reuse a previous type while options are pending', () => {
  const view = setup({ component: 'RuntimeMaintenanceDialog', scaleOptions: scaleData });
  select(view.render(), 'Runtime type').props.onChange({ target: { value: 'openclaw' } });
  const stale = find(view.render(), node => node.type === 'form' && node.props['aria-label'] === 'Add Pods')[0].props.onSubmit;
  select(view.render(), 'Runtime type').props.onChange({ target: { value: 'hermes' } });
  stale({ preventDefault() {} });
  assert.equal(view.operationCalls.length, 0);
  assert.match(renderToStaticMarkup(view.render()), /8 slots per Pod/);
  view.scaleOptions(undefined, { isPending: true });
  formSubmit(view, 'Add Pods');
  assert.equal(view.operationCalls.length, 0);
  assert.doesNotMatch(renderToStaticMarkup(view.render()), /3 slots per Pod|8 slots per Pod/);
  view.scaleOptions({ ...scaleData, items: [scaleData.items[0]], unavailable: [{ runtime_type: 'hermes', reason: 'default deployment not found' }] });
  assert.match(renderToStaticMarkup(view.render()), /default deployment not found/);
  formSubmit(view, 'Add Pods');
  assert.equal(view.operationCalls.length, 0);
});

test('maintenance guards synchronous double submission until the first add request settles', async () => {
  let resolve;
  const view = setup({ component: 'RuntimeMaintenanceDialog', scaleOptions: scaleData, create: () => new Promise(done => { resolve = done; }) });
  select(view.render(), 'Runtime type').props.onChange({ target: { value: 'openclaw' } });
  const submit = find(view.render(), node => node.type === 'form' && node.props['aria-label'] === 'Add Pods')[0].props.onSubmit;
  submit({ preventDefault() {} }); submit({ preventDefault() {} });
  assert.equal(view.operationCalls.length, 1);
  resolve({ added_pods: 1, replicas: 3 }); await tick();
});

test('maintenance deletion requires empty inventory and exact pool name plus UID, using the right registered or pending API', async () => {
  for (const registered of [true, false]) {
    const pod = { ...target, deployment_name: managed.deployment_name, pod_uid: 'pod-uid' };
    const view = setup({ component: 'RuntimeMaintenanceDialog', templates: templateData, pods: registered ? [pod] : [] });
    button(view.render(), 'Existing managed pools').props.onClick();
    button(view.render(), 'Delete pool').props.onClick();
    assert.equal(button(view.render(), 'Delete empty pool').props.disabled, true);
    change(view, 'Deployment name confirmation', managed.deployment_name);
    change(view, 'Deployment UID confirmation', managed.deployment_uid);
    formSubmit(view, 'Delete empty pool'); await tick();
    assert.deepEqual(view.operationCalls[0], registered ? ['deletePod', 42, 'pod-uid'] : ['deletePodPool', managed.deployment_name, managed.namespace, managed.deployment_uid]);
    assert.equal(view.changed(), 1);
  }
  const occupied = setup({ component: 'RuntimeMaintenanceDialog', templates: templateData, pods: [{ ...source, deployment_name: managed.deployment_name, pod_uid: 'pod-uid' }] });
  button(occupied.render(), 'Existing managed pools').props.onClick();
  assert.equal(button(occupied.render(), 'Delete pool').props.disabled, true);
});

test('maintenance preserves deletion conflicts and never retries an uncertain create', async () => {
  const conflict = new AxiosError('Conflict', 'ERR_BAD_REQUEST', {}, undefined, { status: 409, data: { error: 'pool has pending claims' } });
  const view = setup({ component: 'RuntimeMaintenanceDialog', templates: templateData, pods: [], failure: conflict });
  button(view.render(), 'Existing managed pools').props.onClick();
  button(view.render(), 'Delete pool').props.onClick();
  change(view, 'Deployment name confirmation', managed.deployment_name); change(view, 'Deployment UID confirmation', managed.deployment_uid);
  formSubmit(view, 'Delete empty pool'); await tick();
  assert.match(renderToStaticMarkup(view.render()), /pool has pending claims/);
  assert.equal(view.changed(), 0);
  assert.equal(view.operationCalls.length, 1);
  const network = setup({ component: 'RuntimeMaintenanceDialog', scaleOptions: scaleData, failure: new AxiosError('Network Error', 'ERR_NETWORK') });
  select(network.render(), 'Runtime type').props.onChange({ target: { value: 'openclaw' } });
  formSubmit(network, 'Add Pods'); await tick();
  assert.match(renderToStaticMarkup(network.render()), /result is unconfirmed/);
  assert.equal(network.operationCalls.length, 1);
  assert.equal(network.changed(), 1);
});

test('uncertain expansion failures refresh metadata once and keep submission locked until that read settles', async () => {
  for (const failure of [new AxiosError('Network Error', 'ERR_NETWORK'), new AxiosError('Conflict', 'ERR_BAD_REQUEST', {}, undefined, {status:409,data:{error:'expansion was not confirmed; inspect replicas before retrying'}})]) {
    let resolveRead;
    const view=setup({component:'RuntimeMaintenanceDialog',scaleOptions:scaleData,failure,scaleRefetch:()=>new Promise(resolve=>{resolveRead=resolve;})});
    select(view.render(),'Runtime type').props.onChange({target:{value:'openclaw'}});
    const submit=find(view.render(),node=>node.type==='form'&&node.props['aria-label']==='Add Pods')[0].props.onSubmit;
    submit({preventDefault(){}});await tick();
    assert.equal(view.operationCalls.length,1);
    assert.equal(view.changed(),1);
    assert.equal(view.refetches.filter(key=>key[0]==='runtime-pod-scale-options').length,1);
    assert(find(view.render(),node=>node.type==='button'&&node.props.form==='runtime-create-pod')[0].props.disabled);
    submit({preventDefault(){}});assert.equal(view.operationCalls.length,1);
    assert.match(renderToStaticMarkup(view.render()),/result is unconfirmed|expansion was not confirmed/);
    view.scaleOptions(scaleData,{isError:true,error:new Error('metadata unavailable')});
    resolveRead({isError:true});await tick();
    assert.match(renderToStaticMarkup(view.render()),/result is unconfirmed|expansion was not confirmed/);
    formSubmit(view,'Add Pods');assert.equal(view.operationCalls.length,1);
    assert(find(view.render(),node=>node.type==='button'&&node.props.form==='runtime-create-pod')[0].props.disabled);
  }
});

const balance = { runtime_type: 'all', plan_fingerprint: 'plan-abc', allowed: true, moves: [{ instance_id: 1, instance_name: 'balance-instance', runtime_type: 'openclaw', source_pod_id: 41, target_pod_id: 42, target_slot: 1 }], groups: [{ runtime_type: 'openclaw', pod_ids: [41, 42], before: [{ pod_id: 41, count: 2, capacity: 3 }, { pod_id: 42, count: 0, capacity: 3 }], after: [{ pod_id: 41, count: 1, capacity: 3 }, { pod_id: 42, count: 1, capacity: 3 }], balanced: true }], blocked: [{ instance_id: 8, runtime_type: 'hermes', reason: 'incompatible storage' }] };

test('balance previews distributions, moves and blockers before an acknowledged fingerprint execution', async () => {
  assert.ok(existsSync(new URL('../src/pages/admin/RuntimeBalanceDialog.tsx', import.meta.url)), 'Production balance dialog is required');
  const view = setup({ component: 'RuntimeBalanceDialog', balancePlan: balance });
  assert.equal(view.operationCalls.length, 0);
  button(view.render(), 'Preview balance').props.onClick(); await tick();
  assert.deepEqual(view.operationCalls, [['planBalance', 'all']]);
  const markup = renderToStaticMarkup(view.render());
  assert.match(markup, /balance-instance/); assert.match(markup, /incompatible storage/); assert.match(markup, /Before/); assert.match(markup, /After/);
  assert.equal(button(view.render(), 'Execute balance').props.disabled, true);
  acknowledge(view);
  button(view.render(), 'Execute balance').props.onClick(); await tick();
  assert.deepEqual(view.operationCalls.at(-1), ['executeBalance', 'all', 'plan-abc']);
  assert.equal(view.submitted(), 1);
});

test('balance type changes invalidate the plan and stale handlers, while zero moves remains informational', async () => {
  const view = setup({ component: 'RuntimeBalanceDialog', balancePlan: balance });
  button(view.render(), 'Preview balance').props.onClick(); await tick(); acknowledge(view);
  const oldExecute = button(view.render(), 'Execute balance');
  select(view.render(), 'Runtime type').props.onChange({ target: { value: 'hermes' } });
  oldExecute.props.onClick(); await tick();
  assert.ok(!view.operationCalls.some(call => call[0] === 'executeBalance'));
  assert.doesNotMatch(renderToStaticMarkup(view.render()), /balance-instance/);
  const empty = setup({ component: 'RuntimeBalanceDialog', balancePlan: { ...balance, moves: [], blocked: [], allowed: false } });
  button(empty.render(), 'Preview balance').props.onClick(); await tick();
  assert.match(renderToStaticMarkup(empty.render()), /Already balanced/);
  assert.equal(button(empty.render(), 'Execute balance').props.disabled, true);
  const blocked = setup({ component: 'RuntimeBalanceDialog', balancePlan: { ...balance, moves: [], allowed: false } });
  button(blocked.render(), 'Preview balance').props.onClick(); await tick();
  assert.doesNotMatch(renderToStaticMarkup(blocked.render()), /Already balanced/);
  assert.match(renderToStaticMarkup(blocked.render()), /No safe moves/);
});

test('balance 409 clears the stale plan and requires another preview without retrying execution', async () => {
  const failure = new AxiosError('Conflict', 'ERR_BAD_REQUEST', {}, undefined, { status: 409, data: { error: 'balance plan changed' } });
  const view = setup({ component: 'RuntimeBalanceDialog', balancePlan: balance, failure });
  button(view.render(), 'Preview balance').props.onClick(); await tick(); acknowledge(view);
  button(view.render(), 'Execute balance').props.onClick(); await tick();
  assert.match(renderToStaticMarkup(view.render()), /balance plan changed/);
  assert.match(renderToStaticMarkup(view.render()), /Preview the current plan again/);
  assert.equal(button(view.render(), 'Execute balance').props.disabled, true);
  assert.equal(view.operationCalls.filter(call => call[0] === 'executeBalance').length, 1);
  assert.equal(view.submitted(), 0);
});

test('balance ignores late previews for another type and rejects missing fingerprints or disallowed plans', async () => {
  let resolve;
  const pending = setup({ component: 'RuntimeBalanceDialog', balancePlan: () => new Promise(done => { resolve = done; }) });
  button(pending.render(), 'Preview balance').props.onClick();
  select(pending.render(), 'Runtime type').props.onChange({ target: { value: 'hermes' } });
  resolve(balance); await tick();
  assert.doesNotMatch(renderToStaticMarkup(pending.render()), /balance-instance/);
  for (const plan of [{ ...balance, plan_fingerprint: '' }, { ...balance, allowed: false }]) {
    const view = setup({ component: 'RuntimeBalanceDialog', balancePlan: plan });
    button(view.render(), 'Preview balance').props.onClick(); await tick();
    const ack = input(view.render(), 'checkbox');
    if (ack) ack.props.onChange({ target: { checked: true } });
    assert.equal(button(view.render(), 'Execute balance').props.disabled, true);
    button(view.render(), 'Execute balance').props.onClick(); await tick();
    assert.ok(!view.operationCalls.some(call => call[0] === 'executeBalance'));
  }
});

test('balance execution consumes its plan once and reports an uncertain network outcome without retry', async () => {
  const view = setup({ component: 'RuntimeBalanceDialog', balancePlan: balance, failure: new AxiosError('Network Error', 'ERR_NETWORK') });
  button(view.render(), 'Preview balance').props.onClick(); await tick(); acknowledge(view);
  const execute = button(view.render(), 'Execute balance');
  execute.props.onClick(); execute.props.onClick(); await tick();
  assert.equal(view.operationCalls.filter(call => call[0] === 'executeBalance').length, 1);
  assert.match(renderToStaticMarkup(view.render()), /outcome is unconfirmed/);
  assert.equal(view.submitted(), 0);
});

test('Runtime toolbar opens the independent dialogs and routes accepted balance to scheduling records', () => {
  const maintenance = setup({ component: 'RuntimePodsPage', templates: templateData });
  button(maintenance.render(), 'Pod maintenance').props.onClick();
  let tree = maintenance.render();
  const maintenanceDialog = find(tree, node => node.type?.name === 'RuntimeMaintenanceDialog')[0];
  assert.ok(maintenanceDialog);
  maintenanceDialog.props.onChanged(); maintenance.runTimers();
  assert.equal(maintenance.invalidations.length, 2);
  const view = setup({ component: 'RuntimePodsPage', balancePlan: balance });
  button(view.render(), 'Balance').props.onClick();
  tree = view.render();
  const balanceDialog = find(tree, node => node.type?.name === 'RuntimeBalanceDialog')[0];
  assert.ok(balanceDialog);
  balanceDialog.props.onSubmitted();
  assert.ok(find(view.render(), node => node.props?.kind === 'history').length === 1);
});

test('scale-in history exposes cancellation while starting or needing attention and shows operation errors without items', () => {
  for (const status of ['starting', 'needs_attention']) {
    const view = setup({ kind: 'history', historyItems: [{ id: 'scale-in-1', kind: 'scale_in', status, error_message: 'deployment convergence needs attention', items: [] }] });
    const tree = view.render();
    assert.ok(button(tree, 'Stop further moves'));
    assert.match(renderToStaticMarkup(tree), /deployment convergence needs attention/);
  }
  const tree = setup({ kind: 'history', historyItems: [{ id: 'migration', kind: 'migration', status: 'starting', items: [] }] }).render();
  assert.equal(button(tree, 'Stop further moves'), undefined);
});
