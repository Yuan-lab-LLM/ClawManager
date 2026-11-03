import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';
const require = createRequire(import.meta.url);
const source = ts.transpileModule(readFileSync(new URL('../src/pages/admin/RuntimeMaintenanceDialog.tsx', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 } }).outputText;
function setup({ allowed = true, executeError } = {}) {
  const state = []; let cursor = 0; let tree; let executions = 0; let previews = 0;
  const plan = { allowed, blockers: allowed ? [] : ['Insufficient capacity'], runtime_type: 'hermes', namespace: allowed ? 'runtime' : '', deployment_name: allowed ? 'hermes' : '', deployment_uid: allowed ? 'deployment' : '', previous_replicas: allowed ? 3 : 0, replicas: allowed ? 2 : 0, removed_pods: 1, plan_fingerprint: allowed ? 'current-plan' : '', source_pods: [{ id: 9, pod_name: 'source', pod_uid: 'uid', node_name: 'node-9', instance_count: 2 }], items: [{ instance_id: 1, source_pod_id: 9, target_pod_id: 10, target_slot: 1, state: 'pending' }, { instance_id: 2, source_pod_id: 9, target_pod_id: 10, target_slot: 2, state: 'pending' }] };
  const exports = {};
  runInNewContext(source, { exports, require: name => {
    if (name === 'react') return { useState(initial) { const i = cursor++; if (!(i in state)) state[i] = initial; return [state[i], value => { state[i] = typeof value === 'function' ? value(state[i]) : value; }]; }, useRef(initial) { const i = cursor++; return state[i] ??= { current: initial }; } };
    if (name === '@tanstack/react-query') return { useQuery: () => ({ data: { managed_pools: [], items: [{ runtime_type: 'hermes', replicas: 3, slot_count: 8 }], limits: { min_pods: 1, max_pods: 10 } }, refetch: async () => {}, isSuccess: true }) };
    if (name.endsWith('/I18nContext')) return { useI18n: () => ({ t: (key, values) => key + (values ? JSON.stringify(values) : '') }) };
    if (name.endsWith('/runtimePoolService')) return { runtimePoolService: { preflightScaleIn: async () => { previews++; return plan; }, scaleIn: async () => { executions++; if (executeError) throw executeError; return { id: 'operation-1', kind: 'scale_in', items: [] }; } } };
    if (name.endsWith('/runtimeManagement')) return { runtimeError: error => error.message, summarizePod: () => ({ occupied: 0 }) };
    if (name.endsWith('/instance')) return { formatInstanceType: value => value };
    if (name === './RuntimeManagementUI') return { RuntimeDialog: 'dialog', ErrorMessage: 'error' };
    return require(name);
  } });
  function render() { cursor = 0; tree = exports.default({ onClose() {}, onChanged() {}, onViewScheduling() {} }); return tree; }
  function nodes(node = tree) { return !node || typeof node !== 'object' ? [] : [node, ...[node.props?.children, node.props?.footer].flat(Infinity).flatMap(child => child == null ? [] : nodes(child))]; }
  function find(predicate) { const node = nodes().find(predicate); assert.ok(node, 'Expected control was rendered'); return node; }
  const label = key => find(node => node.props?.['aria-label'] === `runtimePods.${key}`);
  const button = key => find(node => node.type === 'button' && node.props.children === `runtimePods.${key}`);
  render(); button('scaleIn').props.onClick(); render();
  label('runtimeType').props.onChange({ target: { value: 'hermes' } }); render();
  return { render, label, button, nodes, counts: () => ({ executions, previews }) };
}
test('scale-in requires a reviewed matching plan and acknowledgement; changing count discards it', async () => {
  const ui = setup();
  assert.equal(ui.button('confirmScaleIn').props.disabled, true);
  await ui.button('previewScaleIn').props.onClick(); await new Promise(resolve => setImmediate(resolve)); ui.render();
  assert.ok(ui.nodes().some(node => node.props?.children === 'node-9'));
  assert.equal(ui.nodes().filter(node => node.type === 'li').length, 2);
  assert.equal(ui.button('confirmScaleIn').props.disabled, true);
  ui.label('scaleInAcknowledge').props.onChange({ target: { checked: true } }); ui.render();
  assert.equal(ui.button('confirmScaleIn').props.disabled, false);
  ui.label('removedPods').props.onChange({ target: { value: '2' } }); ui.render();
  assert.equal(ui.button('confirmScaleIn').props.disabled, true);
  assert.equal(ui.counts().executions, 0);
});
test('insufficient capacity blocks execution and explains the blocker', async () => {
  const ui = setup({ allowed: false });
  await ui.button('previewScaleIn').props.onClick(); await new Promise(resolve => setImmediate(resolve)); ui.render();
  assert.ok(ui.nodes().some(node => node.props?.children === 'Insufficient capacity'));
  assert.equal(ui.button('confirmScaleIn').props.disabled, true);
});
test('confirmed scale-in is single flight and consumes the plan after a failed mutation', async () => {
  const ui = setup({ executeError: new Error('Request failed') });
  await ui.button('previewScaleIn').props.onClick(); await new Promise(resolve => setImmediate(resolve)); ui.render();
  ui.label('scaleInAcknowledge').props.onChange({ target: { checked: true } }); ui.render();
  const submit = ui.button('confirmScaleIn').props.onClick;
  submit(); submit(); await new Promise(resolve => setImmediate(resolve)); ui.render();
  assert.equal(ui.counts().executions, 1);
  assert.equal(ui.button('confirmScaleIn').props.disabled, true);
});

test('invalid removal counts and removing the last Pod never request a plan', async () => {
  const ui = setup();
  for (const value of ['', '0', '-1', '1.5', '11', '3']) {
    ui.label('removedPods').props.onChange({ target: { value } }); ui.render();
    assert.equal(ui.button('previewScaleIn').props.disabled, true);
    await ui.button('previewScaleIn').props.onClick();
  }
  assert.equal(ui.counts().previews, 0);
});
test('accepted scale-in shows a durable operation and an explicit history action', async () => {
  const ui = setup();
  await ui.button('previewScaleIn').props.onClick(); await new Promise(resolve => setImmediate(resolve)); ui.render();
  ui.label('scaleInAcknowledge').props.onChange({ target: { checked: true } }); ui.render();
  ui.button('confirmScaleIn').props.onClick(); await new Promise(resolve => setImmediate(resolve)); ui.render();
  assert.equal(ui.counts().executions, 1);
  assert.ok(ui.button('viewScheduling'));
  assert.ok(ui.nodes().some(node => typeof node.props?.children === 'string' && node.props.children.includes('operation-1')));
});
