import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/modelCatalog.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { groupCatalog } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const models = [
  { local_id: 'a', display_name: 'First', provider_model_name: 'qwen', base_url: 'https://a.example/v1/', quality_tier: 2, is_active: true },
  { local_id: 'b', display_name: 'Second', provider_model_name: 'custom', base_url: 'https://a.example/v1', quality_tier: 0, is_active: false, isNew: true },
  { local_id: 'c', display_name: 'Third', provider_model_name: 'qwen', base_url: 'https://a.example/other/v1', quality_tier: 2, is_active: true },
];
test('groups full URLs without merging distinct API paths', () => {
  const groups = groupCatalog(models, 'url', '', 'all', 'all');
  assert.equal(groups.length, 2);
  assert.equal(groups.find((group) => group.key === 'https://a.example/v1').items.length, 2);
});
test('tier grouping combines sources and retains unclassified models', () => {
  const groups = groupCatalog(models, 'tier', '', 'all', 'all');
  assert.deepEqual(groups.map((group) => [group.key, group.items.length]), [['0', 1], ['2', 2]]);
});
test('search, tier and status filters combine without mutating models', () => {
  const before = JSON.stringify(models);
  const groups = groupCatalog(models, 'url', ' QWEN ', '2', 'active');
  assert.equal(groups.flatMap((group) => group.items).length, 2);
  assert.equal(JSON.stringify(models), before);
  assert.equal(groupCatalog(models, 'url', 'not found', 'all', 'all').length, 0);
});
test('pending filter isolates unsaved models', () => {
  const groups = groupCatalog(models, 'url', '', 'all', 'pending');
  assert.deepEqual(groups.flatMap((group) => group.items.map((model) => model.local_id)), ['b']);
});
