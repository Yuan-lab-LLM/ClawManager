import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
async function load(path) {
  const { outputText } = ts.transpileModule(read(path), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}
const { modelVisionSupport } = await load('../src/lib/modelVisionSupport.ts');
const { modelRoutingPlanRows, mergeDiscoveredPlan } = await load('../src/lib/modelRoutingPlan.ts');

test('vision manual choices override discovery and unknown is not false', () => {
  for (const value of [true, false]) assert.equal(modelVisionSupport({ vision_support_override: value, supports_vision: !value, capability_source: 'provider_metadata' }), value);
  assert.equal(modelVisionSupport({ supports_vision: false, capability_source: 'provider_metadata' }), null);
  assert.equal(modelVisionSupport({ supports_vision: true, capability_source: 'name_rule' }), null);
  assert.equal(modelVisionSupport({ supports_vision: true, capability_source: 'provider_metadata' }), true);
});

test('vision choices are per row and refresh preserves edited choices including reset', () => {
  const discovered = [{ id: 'a', supports_vision: true, classification_source: 'provider_metadata' }, { id: 'b', vision_support_override: false }];
  const rows = modelRoutingPlanRows({ isNew: true, provider_model_name: 'a', vision_support_override: true }, discovered);
  assert.deepEqual(rows.map(modelVisionSupport), [true, false]);
  const merged = mergeDiscoveredPlan(discovered, [{ id: 'a', plan_edited: true, vision_support_override: false }, { id: 'b', plan_edited: true, vision_support_override: null, reset_vision_support_override: true }]);
  assert.equal(merged[0].vision_support_override, false);
  assert.equal(merged[1].reset_vision_support_override, true);
});

test('saved model preview preserves its metadata and manual off', () => {
  const row = modelRoutingPlanRows({ provider_model_name: 'a', supports_vision: true, capability_source: 'provider_metadata', vision_support_override: false }, [{ id: 'a', supports_vision: false }])[0];
  assert.equal(row.supports_vision, true);
  assert.equal(modelVisionSupport(row), false);
});

test('vision is wired through snapshots, single/batch saves, plan editor and catalog', () => {
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  assert.equal(page.match(/vision_support_override: card\.vision_support_override/g).length, 3);
  assert.match(page, /vision_support_override: model\.vision_support_override/);
  assert.match(page, /patch\.reset_vision_support_override/);
  assert.match(read('../src/components/ModelDiscoveryPlan.tsx'), /vision_support_override: checked/);
  assert.match(read('../src/components/ModelCatalog.tsx'), /modelVisionSupport\(model\)/);
});
