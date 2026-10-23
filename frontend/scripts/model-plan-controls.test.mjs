import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
async function load(path) {
  const { outputText } = ts.transpileModule(read(path), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}
const { modelRoutingPlanRows, mergeDiscoveredPlan, plannedReasoningEnabled } = await load('../src/lib/modelRoutingPlan.ts');
const { supportsManagedReasoningControl } = await load('../src/lib/modelProviderTemplates.ts');

test('manual reasoning opt-out overrides discovery and a reasoning pool suggestion', () => {
  assert.equal(plannedReasoningEnabled({ reasoning_enabled: false, supports_reasoning: true, suggested_pool: 'reasoning' }), false);
  assert.equal(plannedReasoningEnabled({ reasoning_enabled: true, supports_reasoning: false }), true);
  assert.equal(plannedReasoningEnabled({ supports_reasoning: true }), true);
  assert.equal(plannedReasoningEnabled({}), false);
});

test('batch preview preserves each model reasoning choice independently', () => {
  const rows = modelRoutingPlanRows({ isNew: true, provider_model_name: 'a', display_name: 'A', reasoning_enabled: false }, [
    { id: 'a', display_name: 'A', supports_reasoning: true },
    { id: 'b', display_name: 'B', reasoning_enabled: true },
    { id: 'c', display_name: 'C', reasoning_enabled: false, suggested_pool: 'reasoning' },
  ]);
  assert.deepEqual(rows.map(plannedReasoningEnabled), [false, true, false]);
});

test('refresh and existing model edits retain explicit reasoning false', () => {
  const discovered = [{ id: 'a', display_name: 'A', supports_reasoning: true }];
  const refreshed = mergeDiscoveredPlan(discovered, [{ id: 'a', display_name: 'Custom', reasoning_enabled: false, plan_edited: true }]);
  assert.equal(refreshed[0].reasoning_enabled, false);
  assert.equal(plannedReasoningEnabled(refreshed[0]), false);
  const saved = modelRoutingPlanRows({ provider_model_name: 'a', display_name: 'Saved', reasoning_enabled: false }, discovered);
  assert.equal(plannedReasoningEnabled(saved[0]), false);
});

test('moving reasoning control does not enable unsupported provider protocols or proxy URLs', () => {
  assert.equal(supportsManagedReasoningControl('local', 'openai-compatible', 'https://api.deepseek.com/v1', 'deepseek-v4-pro'), true);
  assert.equal(supportsManagedReasoningControl('local', 'anthropic', 'https://api.deepseek.com/v1', 'deepseek-v4-pro'), false);
  assert.equal(supportsManagedReasoningControl('local', 'openai-compatible', 'https://proxy.example/v1', 'deepseek-v4-pro'), false);
  assert.equal(supportsManagedReasoningControl('local', 'openai-compatible', 'https://api.deepseek.com/v1', 'other'), false);
});

test('single plan keeps a stable row key and manual ID entry; batch does not save an implicit first model', () => {
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  const plan = read('../src/components/ModelDiscoveryPlan.tsx');
  assert.doesNotMatch(page, /<select\s+value=\{card\.provider_model_name\}/);
  assert.doesNotMatch(page, /checked=\{reasoningSupported && card\.reasoning_enabled\}/);
  assert.match(plan, /key=\{batch \? model\.id : 'current-model'\}/);
  assert.match(plan, /onModelIdChange\(event\.target\.value\)/);
  assert.match(page, /batchPlan \? void applyDiscoveryPlan\(card\.local_id, selectedPlan\.map/);
  assert.doesNotMatch(plan, /onApply|applySelected|displayNamePlaceholder/);
  assert.match(plan, /model\.id\.trim\(\) && <input/);
  assert.match(page, /models\.some\(\(card\) => !card\.isEditing\) && <ModelCatalog/);
  assert.match(plan, /reasoningUnmanagedHelp/);
  assert.match(page, /patch\.reasoning_enabled !== undefined/);
  assert.match(plan, /'supportsTools', 'supportsVision', 'reasoning'/);
});

test('Secret Ref stays in collapsed advanced settings and remains in save payloads', () => {
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  const advanced = page.indexOf("t('modelManagementPage.advancedCredentials')");
  const secret = page.indexOf('value={card.api_key_secret_ref}', advanced);
  assert.ok(advanced >= 0 && secret > advanced && page.indexOf('</details>', advanced) > secret);
  assert.ok(page.lastIndexOf('<details ', advanced) > page.indexOf("t('modelManagementPage.apiKey')", page.indexOf('models.filter((card) => card.isEditing)')));
  assert.match(page, /api_key_secret_ref: card\.api_key_secret_ref\?\.trim\(\) \|\| undefined/);
});
