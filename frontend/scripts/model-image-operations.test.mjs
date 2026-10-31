import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(read('../src/lib/modelRoutingPlan.ts'), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { plannedModelKind, modelRoutingPlanRows, mergeDiscoveredPlan } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('image model discovery is separate from chat vision and never enables generation implicitly', () => {
  assert.equal(plannedModelKind({}), 'chat');
  assert.equal(plannedModelKind({ supports_vision: true }), 'chat');
  assert.equal(plannedModelKind({ model_kind: 'image_generation' }), 'image');
  assert.equal(plannedModelKind({ model_kind: 'embedding', chat_capable: false }), 'other');
  const rows = modelRoutingPlanRows({ isNew: true, provider_model_name: 'a' }, [{ id: 'a', model_kind: 'image', chat_capable: false }, { id: 'b', model_kind: 'chat' }]);
  assert.equal(rows[0].model_kind, 'image');
  assert.equal(rows[0].chat_capable, false);
  assert.equal(rows[0].image_generation_enabled, false);
  assert.equal(rows[0].image_edit_enabled, false);
  assert.equal(rows[1].model_kind, 'chat');
});

test('saved models and per-row edits take precedence over rediscovery', () => {
  const provider = [{ id: 'a', model_kind: 'image', chat_capable: false }];
  const legacy = modelRoutingPlanRows({ provider_model_name: 'a' }, provider)[0];
  assert.equal(legacy.model_kind, 'chat');
  assert.equal(legacy.chat_capable, true);
  const oldSaved = modelRoutingPlanRows({ provider_model_name: 'a' }, [{ ...provider[0], image_generation_enabled: true, image_edit_enabled: true }])[0];
  assert.equal(oldSaved.image_generation_enabled, false);
  assert.equal(oldSaved.image_edit_enabled, false);
  const saved = modelRoutingPlanRows({ provider_model_name: 'a', model_kind: 'image', image_generation_enabled: false, image_edit_enabled: true }, [{ id: 'a', model_kind: 'chat', image_generation_enabled: true }])[0];
  assert.equal(saved.model_kind, 'image');
  assert.equal(saved.image_generation_enabled, false);
  assert.equal(saved.image_edit_enabled, true);
  const merged = mergeDiscoveredPlan(provider, [{ id: 'a', plan_edited: true, model_kind: 'chat', image_generation_enabled: false, image_edit_enabled: false }]);
  assert.equal(merged[0].model_kind, 'chat');
  assert.equal(merged[0].chat_capable, true);
  assert.equal(merged[0].image_generation_enabled, false);
});

test('operation fields are included in snapshots, individual and bulk save paths', () => {
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  for (const field of ['model_kind', 'image_generation_enabled', 'image_edit_enabled', 'image_price_per_image']) {
    assert.equal(page.match(new RegExp(`${field}: card\\.${field}`, 'g')).length, 3, field);
    assert.match(page, new RegExp(`patch\\.${field}`));
    assert.match(read('../src/services/modelService.ts'), new RegExp(field));
  }
  assert.match(page, /model_kind: plannedModelKind\(model\)/);
  assert.match(page, /image_generation_enabled: model.image_generation_enabled \?\? false/);
  assert.match(read('../src/components/ModelDiscoveryPlan.tsx'), /image_edit_enabled: checked/);
  assert.match(read('../src/lib/i18n.ts'), /不自动重试/);
});

test('image estimate is per-row, preserves explicit zero and can be cleared', () => {
  const discovered = [{ id: 'a', model_kind: 'image' }, { id: 'b', model_kind: 'image' }];
  const rows = modelRoutingPlanRows({ isNew: true, provider_model_name: 'a', image_price_per_image: 0 }, discovered);
  assert.equal(rows[0].image_price_per_image, 0);
  assert.equal(rows[1].image_price_per_image, undefined);
  const merged = mergeDiscoveredPlan(discovered, [{ id: 'a', plan_edited: true, image_price_per_image: null, reset_image_price: true }]);
  assert.equal(merged[0].image_price_per_image, null);
  assert.equal(merged[0].reset_image_price, true);
});
