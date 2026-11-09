import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(read('../src/lib/modelSelection.ts'), {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
});
const { selectedRows, toggleSelection, mergeBatchSave } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const id = (row) => row.local_id;
const a = { local_id: 'a', display_name: ' A ', provider_model_name: 'model-a ', isNew: true };
const b = { local_id: 'b', display_name: 'B', provider_model_name: 'model-b', isNew: true };
const c = { local_id: 'c', display_name: 'A', provider_model_name: 'model-a', isNew: true };

test('selection excludes unchecked and stale rows, including zero selections', () => {
  assert.deepEqual(selectedRows([a, b], new Set(['b', 'deleted']), id), [b]);
  assert.deepEqual(selectedRows([a, b], new Set(), id), []);
});

test('group selection and deselection preserve other groups without mutation', () => {
  const original = new Set(['outside']);
  const checked = toggleSelection(original, ['a', 'b'], true);
  assert.deepEqual([...checked], ['outside', 'a', 'b']);
  assert.deepEqual([...toggleSelection(checked, ['a'], false)], ['outside', 'b']);
  assert.deepEqual([...toggleSelection(checked, ['a', 'b'], false)], ['outside']);
  assert.deepEqual([...original], ['outside']);
});

test('discovery exclusions persist across refresh and do not imply deletion', () => {
  const excluded = toggleSelection(new Set(), ['b'], true);
  const refreshed = [{ id: 'b' }, { id: 'a' }, { id: 'new' }];
  assert.deepEqual(refreshed.filter((row) => !excluded.has(row.id)).map((row) => row.id), ['a', 'new']);
  const none = toggleSelection(excluded, refreshed.map((row) => row.id), true);
  assert.equal(refreshed.filter((row) => !none.has(row.id)).length, 0);
});

test('save reconciliation attaches server IDs only to submitted rows and trims matching keys', () => {
  const result = mergeBatchSave([a, b, c], new Set(['a']), {
    saved: [{ id: 42, display_name: 'A', provider_model_name: 'model-a', quality_tier: 2 }], failed: [],
  }, 'failed');
  assert.equal(result[0].id, 42);
  assert.equal(result[0].local_id, 'a');
  assert.equal(result[0].isNew, false);
  assert.equal(result[0].quality_tier, 2);
  assert.equal(result[1], b);
  assert.equal(result[2], c);
  assert.equal(a.id, undefined);
});

test('partial failures and missing results remain pending for retry', () => {
  const result = mergeBatchSave([a, b], new Set(['a', 'b']), {
    saved: [], failed: [{ display_name: 'A', provider_model_name: 'model-a', error: 'denied' }],
  }, 'missing');
  assert.equal(result[0].error, 'denied');
  assert.equal(result[1].error, 'missing');
  assert.ok(result.every((row) => row.isNew && !row.id));
  const missingID = mergeBatchSave([b], new Set(['b']), { saved: [{ ...b }], failed: [] }, 'missing ID');
  assert.equal(missingID[0].isNew, true);
  assert.equal(missingID[0].error, 'missing ID');
});

test('a later batch failure cannot revert earlier persisted rows', () => {
  const first = mergeBatchSave([a, b], new Set(['a']), {
    saved: [{ id: 42, display_name: 'A', provider_model_name: 'model-a' }], failed: [],
  }, 'failed');
  const second = mergeBatchSave(first, new Set(['b']), { saved: [], failed: [] }, 'failed');
  assert.equal(second[0], first[0]);
  assert.equal(second[0].id, 42);
  assert.equal(second[1].isNew, true);
});

test('UI wires explicit selections through apply, save and confirmed deletion', () => {
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  const plan = read('../src/components/ModelDiscoveryPlan.tsx');
  const catalog = read('../src/components/ModelCatalog.tsx');
  assert.match(plan, /onExcludedChange\(toggleSelection\(excluded/);
  assert.match(page, /selectedPlan\.length === 0 \|\| selectedPlan\.some/);
  assert.match(page, /applyDiscoveryPlan\(card\.local_id, selectedPlan\.map/);
  assert.match(page, /await saveSelectedCards\(planned, true\)/);
  assert.match(page, /selectedRows\(modelRoutingPlanRows/);
  assert.match(page, /current\.filter\(\(item\) => !\(item\.local_id === localId && item\.isNew\)\)/);
  assert.match(catalog, /onSaveSelected\(pendingSelected\)/);
  assert.match(catalog, /onDeleteSelected\(selected\)/);
  assert.match(catalog, /setSelection\(new Set\(\)\)/);
  assert.match(page, /if \(!window\.confirm\(t\('modelManagementPage\.confirmDeleteSelected'/);
  assert.doesNotMatch(page, /saveAllNewCards/);
});
