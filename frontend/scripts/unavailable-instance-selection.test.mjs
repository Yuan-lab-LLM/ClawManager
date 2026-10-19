import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const source = readFileSync(new URL('../src/lib/unavailableInstanceSelection.ts', import.meta.url), 'utf8');
const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { collectUnavailableLiteIds, pruneVisibleSelection, isUnavailableLiteCandidate } = await import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));
const item = id => ({ id, name: `test-${id}`, instance_mode: 'lite', runtime_type: 'gateway', type: 'hermes', status: 'error' });

test('collects every page sequentially, scoped to Lite and unavailable', async () => {
  const rows = Array.from({ length: 250 }, (_, i) => item(i + 1));
  const calls = [];
  const ids = await collectUnavailableLiteIds(async (page, limit, filters) => {
    calls.push(page); assert.equal(limit, 100);
    assert.deepEqual(filters, { instance_mode: 'lite', availability: 'unavailable' });
    return { instances: rows.slice((page - 1) * 100, page * 100), total: 250 };
  }, new Set([102]), () => false);
  assert.deepEqual(calls, [1, 2, 3]); assert.equal(ids.length, 249); assert.ok(ids.includes(250)); assert.ok(!ids.includes(102));
});
test('excludes Pro, available, deleting, unsupported and cleanup entries', () => {
  for (const override of [{ instance_mode: 'pro' }, { status: 'running' }, { status: 'deleting' }, { type: 'unsupported' }, { name: 'cleanup-pending-1' }, { name: 'reset-1' }]) {
    assert.equal(isUnavailableLiteCandidate({ ...item(1), ...override }), false);
  }
  assert.equal(isUnavailableLiteCandidate({ ...item(1), status: 'stopped' }), true);
});
test('changing pages preserves selection outside the visible page', () => {
  assert.deepEqual(pruneVisibleSelection([1, 101, 201], [item(101)]), [1, 101, 201]);
  assert.deepEqual(pruneVisibleSelection([1, 101], [{ ...item(101), status: 'deleting' }]), [1]);
});
test('failure, cancellation, incomplete and changing lists never return a partial selection', async () => {
  await assert.rejects(collectUnavailableLiteIds(async () => { throw new Error('network'); }, new Set(), () => false), /network/);
  await assert.rejects(collectUnavailableLiteIds(async () => ({ instances: [], total: 0 }), new Set(), () => true), /取消/);
  await assert.rejects(collectUnavailableLiteIds(async () => ({ instances: [item(1)], total: 2 }), new Set(), () => false), /不完整/);
  await assert.rejects(collectUnavailableLiteIds(async page => ({ instances: [item(page)], total: page === 1 ? 101 : 100 }), new Set(), () => false), /变化/);
});
