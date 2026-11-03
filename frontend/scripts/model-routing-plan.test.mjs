import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/modelRoutingPlan.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const { modelRoutingPlanRows, mergeDiscoveredPlan } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const card = { display_name: 'My model', provider_model_name: 'model-a', model_group: 'custom-group', quality_tier: 3, deployment_priority: 7 };
const discovered = [{ id: 'model-a', suggested_pool: 'cheap', suggested_quality_tier: 1, suggested_priority: 50 }, { id: 'model-b', suggested_pool: 'general', suggested_quality_tier: 2 }];

test('editing an existing model retains saved routing values instead of discovery suggestions', () => {
  const rows = modelRoutingPlanRows(card, discovered);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].suggested_pool, 'custom-group');
  assert.equal(rows[0].suggested_quality_tier, 3);
  assert.equal(rows[0].suggested_priority, 7);
});

test('manual model remains editable when discovery fails or returns no models', () => {
  assert.equal(modelRoutingPlanRows(card, [])[0].id, 'model-a');
  assert.equal(modelRoutingPlanRows({ ...card, provider_model_name: '', isNew: true }, []).length, 1);
});

test('batch preview and application retain current edits and other models suggestions', () => {
  const rows = modelRoutingPlanRows({ ...card, isNew: true }, discovered);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].suggested_quality_tier, 3);
  assert.equal(rows[1].suggested_quality_tier, 2);
  assert.equal(discovered[0].suggested_quality_tier, 1);
});

test('manually entered models absent from discovery are not lost on apply', () => {
  const rows = modelRoutingPlanRows({ ...card, isNew: true, provider_model_name: 'custom-model' }, discovered);
  assert.equal(rows.length, 3);
  assert.equal(rows[0].id, 'custom-model');
});

test('every discovered row keeps its own editable name and upstream ID', () => {
  const discovered = [{ id: 'model-a', display_name: 'First alias' }, { id: 'model-b', display_name: 'Second alias', plan_edited: true }];
  const rows = modelRoutingPlanRows({ ...card, display_name: 'Custom A', isNew: true }, discovered);
  assert.deepEqual(rows.map((row) => [row.id, row.display_name]), [['model-a', 'Custom A'], ['model-b', 'Second alias']]);
  const switched = modelRoutingPlanRows({ ...card, isNew: true, provider_model_name: 'model-b', display_name: discovered[1].display_name }, discovered);
  assert.deepEqual(switched.map((row) => row.display_name), ['First alias', 'Second alias']);
});

test('clearing a name keeps an empty draft instead of restoring a provider ID', () => {
  const rows = modelRoutingPlanRows({ ...card, display_name: '', isNew: true }, discovered);
  assert.equal(rows[0].display_name, '');
  assert.equal(rows[0].id, 'model-a');
});

test('refresh retains per-model names and tool settings while accepting new provider metadata', () => {
  const before = [{ id: 'a', display_name: 'Custom A', suggested_pool: 'coding', suggested_quality_tier: 3, tools_support_override: false, plan_edited: true }, { id: 'b', display_name: 'Old default' }];
  const after = [{ id: 'a', display_name: 'Vendor A', context_window_tokens: 128000 }, { id: 'b', display_name: 'New default' }];
  const refreshed = mergeDiscoveredPlan(after, before);
  assert.equal(refreshed[0].display_name, 'Custom A');
  assert.equal(refreshed[0].suggested_quality_tier, 3);
  assert.equal(refreshed[0].tools_support_override, false);
  assert.equal(refreshed[0].context_window_tokens, 128000);
  assert.equal(refreshed[1].display_name, 'New default');
  assert.equal(after[0].display_name, 'Vendor A');
});

test('refresh preserves intentionally empty names and a new provider does not inherit old aliases', () => {
  const discovered = [{ id: 'a', display_name: 'Vendor A' }];
  assert.equal(mergeDiscoveredPlan(discovered, [{ id: 'a', display_name: '', plan_edited: true }])[0].display_name, '');
  assert.equal(mergeDiscoveredPlan(discovered, [])[0].display_name, 'Vendor A');
});

test('name editor exists only in routing plan and applies names per row', () => {
  const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  const plan = read('../src/components/ModelDiscoveryPlan.tsx');
  assert.doesNotMatch(page, /value=\{card\.display_name\}/);
  assert.match(plan, /value=\{model\.display_name \?\? ''\}/);
  assert.match(plan, /display_name: event\.target\.value/);
  assert.match(page, /patch\.display_name !== undefined/);
  assert.match(page, /uniqueImportedDisplayName\(\s*model\.display_name\.trim\(\)/);
  assert.doesNotMatch(page, /<select\s+value=\{card\.provider_model_name\}/);
  assert.match(page, /display_name: card\.display_name/); // cancel snapshot
  const template = page.indexOf('<ProviderTemplatePicker');
  const protocol = page.indexOf("t('modelManagementPage.protocol')", template);
  const baseURL = page.indexOf("t('modelManagementPage.baseUrl')", template);
  assert.ok(template < protocol && protocol < baseURL);
});
