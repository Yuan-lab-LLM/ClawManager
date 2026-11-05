import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';
import { AxiosError } from 'axios';
import { parseAutoStopDays, MAX_AUTO_STOP_DAYS } from '../src/lib/runtimeAutoStop.ts';

const require = createRequire(import.meta.url);
const initialSettings = { enabled: true, check_interval_days: 1, idle_timeout_days: 1, version: 3, next_check_at: '2026-11-03T20:00:00Z' };
function setup({ settings = initialSettings, activity, update, keepRunning, isError = false } = {}) {
  const values = []; let cursor = 0; let tree; let queryOptions; let refreshes = 0;
  const updates = []; const changes = [];
  const exports = {};
  const file = activity ? '../src/components/InstanceAutoStopInfo.tsx' : '../src/pages/admin/RuntimeAutoStopDialog.tsx';
  const compiled = ts.transpileModule(readFileSync(new URL(file, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 } }).outputText;
  const service = {
    updateAutoStopSettings: async body => { updates.push(body); return update ? update(body) : { ...body, version: body.version + 1, next_check_at: settings.next_check_at }; },
    setInstanceAutoStopDisabled: async (id, disabled) => { updates.push({ id, disabled }); return keepRunning ? keepRunning() : { instance_id: id, auto_stop_disabled: disabled }; },
  };
  runInNewContext(compiled, { exports, require: name => {
    if (name === 'react') return {
      useState(initial) { const i = cursor++; if (!(i in values)) values[i] = initial; return [values[i], value => { values[i] = typeof value === 'function' ? value(values[i]) : value; }]; },
      useRef(initial) { const i = cursor++; return values[i] ??= { current: initial }; },
    };
    if (name === '@tanstack/react-query') return { useQuery: options => { queryOptions = options; return { data: settings, dataUpdatedAt: 1, isError, error: new Error('settings unavailable'), isFetching: false, refetch: async () => { refreshes++; } }; } };
    if (name.endsWith('/I18nContext')) return { useI18n: () => ({ locale: 'en', t: key => key }) };
    if (name.endsWith('/runtimePoolService')) return { runtimePoolService: service };
    if (name.endsWith('/runtimeManagement')) return { runtimeError: error => error.message };
    if (name.endsWith('/runtimeAutoStop')) return { parseAutoStopDays, MAX_AUTO_STOP_DAYS };
    if (name === './RuntimeManagementUI') return { RuntimeDialog: 'dialog', ErrorMessage: 'error' };
    return require(name);
  } });
  const props = activity ? { instance: activity, onChanged: value => changes.push(value) } : { onClose() {} };
  function render(control = false) {
    cursor = 0;
    tree = (control ? exports.InstanceAutoStopControl : exports.default)(props);
    if (typeof tree?.type === 'function') tree = tree.type(tree.props);
    return tree;
  }
  const nodes = (node = tree) => !node || typeof node !== 'object' ? [] : [node, ...[node.props?.children, node.props?.footer].flat(Infinity).flatMap(child => child == null ? [] : nodes(child))];
  const find = predicate => { const node = nodes().find(predicate); assert.ok(node, 'Expected rendered control'); return node; };
  const button = key => find(node => node.type === 'button' && node.props.children === key);
  const inputs = () => nodes().filter(node => node.type === 'input');
  const submit = () => find(node => node.type === 'form').props.onSubmit({ preventDefault() {} });
  const flush = async () => { await new Promise(resolve => setImmediate(resolve)); render(!!activity); };
  render(!!activity);
  return { render, nodes, find, button, inputs, submit, flush, updates, changes, query: () => queryOptions, refreshes: () => refreshes };
}

test('settings preserve independent intervals and use each confirmed server version', async () => {
  const ui = setup();
  ui.inputs()[1].props.onChange({ target: { value: '2' } }); ui.render();
  ui.inputs()[2].props.onChange({ target: { value: '7' } }); ui.render();
  ui.submit(); await ui.flush();
  assert.deepEqual(JSON.parse(JSON.stringify(ui.updates)), [{ enabled: true, check_interval_days: 2, idle_timeout_days: 7, version: 3 }]);
  assert.ok(ui.nodes().some(node => node.props?.children === 'autoStop.saved'));
  ui.submit(); await ui.flush();
  assert.equal(ui.updates[1].version, 4);
  assert.equal(ui.query().refetchOnWindowFocus, false);
});

test('invalid days never submit settings', () => {
  const ui = setup();
  for (const value of ['', '0', '-1', '1.5', '1e2', '106752']) {
    ui.inputs()[1].props.onChange({ target: { value } }); ui.render();
    assert.equal(ui.button('autoStop.save').props.disabled, true);
    ui.submit();
  }
  assert.equal(ui.updates.length, 0);
});

test('settings save is single flight and only reports saved after API success', async () => {
  let complete;
  const ui = setup({ update: () => new Promise(resolve => { complete = resolve; }) });
  ui.submit(); ui.submit(); ui.render();
  assert.equal(ui.updates.length, 1);
  assert.equal(ui.button('autoStop.saving').props.disabled, true);
  assert.equal(ui.nodes().some(node => node.props?.children === 'autoStop.saved'), false);
  complete({ ...initialSettings, version: 4 }); await ui.flush();
  assert.ok(ui.nodes().some(node => node.props?.children === 'autoStop.saved'));
});

test('settings conflicts require refresh and preserve actionable errors while editing', async () => {
  const ui = setup({ update: () => { throw new AxiosError('Changed elsewhere', 'ERR_BAD_REQUEST', {}, undefined, { status: 409 }); } });
  ui.submit(); await ui.flush();
  assert.match(ui.find(node => node.type === 'error').props.children, /autoStop.conflict/);
  assert.equal(ui.button('autoStop.save').props.disabled, true);
  ui.inputs()[1].props.onChange({ target: { value: '2' } }); ui.render();
  assert.match(ui.find(node => node.type === 'error').props.children, /autoStop.conflict/);
  ui.submit(); assert.equal(ui.updates.length, 1);
  ui.button('common.refresh').props.onClick(); assert.equal(ui.refreshes(), 1);
});

test('settings load failures show an explicit retry with no editable defaults', () => {
  const ui = setup({ isError: true });
  assert.equal(ui.inputs().length, 0);
  ui.button('runtimePods.retry').props.onClick();
  assert.equal(ui.refreshes(), 1);
});

test('keep-running control stays unchanged until confirmed and never retries failures', async () => {
  const instance = { id: 9, instance_mode: 'lite', status: 'running', auto_stop_disabled: false };
  const ui = setup({ activity: instance, keepRunning: () => { throw new Error('Offline'); } });
  const change = ui.inputs()[0].props.onChange;
  change({ target: { checked: true } }); change({ target: { checked: true } }); await ui.flush();
  assert.equal(ui.updates.length, 1);
  assert.equal(ui.inputs()[0].props.checked, false);
  assert.equal(ui.changes.length, 0);
  assert.match(ui.find(node => node.props?.role === 'alert').props.children, /autoStop.saveError/);
});

test('keep-running successful response reaches the administrator list, and Pro has no control', async () => {
  const ui = setup({ activity: { id: 9, instance_mode: 'lite', status: 'stopped' } });
  ui.inputs()[0].props.onChange({ target: { checked: true } }); await ui.flush();
  assert.equal(ui.changes[0].auto_stop_disabled, true);
  const pro = setup({ activity: { id: 10, instance_mode: 'pro' } });
  assert.equal(pro.render(true), null);
});

test('Lite presentation exposes reason, activity and blockers without offering another start path', () => {
  const ui = setup({ activity: { id: 9, instance_mode: 'lite', status: 'stopped', stopped_reason: 'idle_timeout', auto_stop_blocker: 'native execution cannot be observed', monitoring_since: '2026-11-01T00:00:00Z' } });
  ui.render();
  const text = JSON.stringify(ui.nodes().map(node => node.props.children));
  assert.match(text, /autoStop.reason_idle_timeout/);
  assert.match(text, /autoStop.lastActivity/);
  assert.match(text, /native execution cannot be observed/);
  assert.match(text, /autoStop.startHelp/);
  assert.equal(ui.nodes().filter(node => node.type === 'button').length, 0);
});
