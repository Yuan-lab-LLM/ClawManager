import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const page = readFileSync(new URL('../src/pages/admin/protection/scenarios/EgressPrivateExceptionPage.tsx', import.meta.url), 'utf8');
const helper = readFileSync(new URL('../src/lib/egressScopeOptions.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(helper, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { loadAllScopeOptions } = await import(`data:text/javascript;base64,${Buffer.from(compiled.outputText).toString('base64')}`);

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

// Execute the page's actual loader callback, including all result/error/finally
// guards, so these tests detect missing guards in the component wiring.
function pageLoader(requests) {
  const start = page.indexOf('const loadOptions = useCallback(') + 'const loadOptions = useCallback('.length;
  const end = page.indexOf(', [k, t]);', start);
  assert.ok(start > 0 && end > start);
  const state = { instances: [], instanceError: null, instanceLoading: false };
  const version = { current: 0 };
  const cleanup = page.match(/return \(\) => \{([\s\S]*?)\};\s*\}, \[loadItems, loadOptions\]\);/);
  assert.ok(cleanup, 'page effect must invalidate pending requests on cleanup');
  let call = 0;
  const noOp = () => {};
  const load = new Function('loadAllScopeOptions', 'adminInstanceService', 'userService', 'optionsRequest', 'setInstances', 'setUsers', 'setInstancesLoading', 'setUsersLoading', 'setInstanceOptionsError', 'setUserOptionsError', 't', 'k', `return (${page.slice(start, end)});`)(
    loadAllScopeOptions,
    { getInstances: () => requests[call++].promise },
    { getUsers: async () => ({ users: [], total: 0 }) },
    version,
    (value) => { state.instances = value; }, noOp,
    (value) => { state.instanceLoading = value; }, noOp,
    (value) => { state.instanceError = value; }, noOp,
    (key) => key, 'egress',
  );
  const invalidate = new Function('itemsRequest', 'optionsRequest', cleanup[1]);
  return { load, state, invalidate: () => invalidate({ current: 0 }, version) };
}

test('late old success cannot replace the latest scope list', async () => {
  const old = deferred(), current = deferred();
  const { load, state } = pageLoader([old, current]);
  const first = load(), second = load();
  current.resolve({ instances: [{ id: 2, name: 'current' }], total: 1 });
  await second;
  old.resolve({ instances: [{ id: 1, name: 'old' }], total: 1 });
  await first;
  assert.equal(state.instances[0].id, 2);
});

test('old completion cannot enable the form while a newer load is pending', async () => {
  const old = deferred(), current = deferred();
  const { load, state } = pageLoader([old, current]);
  const first = load(), second = load();
  old.resolve({ instances: [{ id: 1, name: 'old' }], total: 1 });
  await first;
  assert.equal(state.instanceLoading, true);
  assert.deepEqual(state.instances, []);
  current.resolve({ instances: [{ id: 2, name: 'current' }], total: 1 });
  await second;
  assert.equal(state.instanceLoading, false);
});

test('old failure cannot replace newer success with an error', async () => {
  const old = deferred(), current = deferred();
  const { load, state } = pageLoader([old, current]);
  const first = load(), second = load();
  current.resolve({ instances: [{ id: 2, name: 'current' }], total: 1 });
  await second;
  old.reject(new Error('old network failure'));
  await first;
  assert.equal(state.instanceError, null);
  assert.equal(state.instances[0].id, 2);
});

test('effect cleanup invalidates pending loaders', async () => {
  const request = deferred();
  const { load, state, invalidate } = pageLoader([request]);
  const pending = load();
  invalidate();
  request.resolve({ instances: [{ id: 1, name: 'obsolete' }], total: 1 });
  await pending;
  assert.deepEqual(state.instances, []);
});
