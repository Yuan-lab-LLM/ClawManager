import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
async function load(path) {
  const { outputText } = ts.transpileModule(read(path), { compilerOptions: { module: ts.ModuleKind.ESNext } });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}
const { supportsManagedReasoningControl } = await load('../src/lib/modelProviderTemplates.ts');
const { modelRoutingPlanRows, mergeDiscoveredPlan } = await load('../src/lib/modelRoutingPlan.ts');

test('explicit wire formats work on custom URLs without guessing from names', () => {
  for (const mode of ['thinking-type', 'deepseek-thinking', 'enable-thinking', 'chat-template-kwargs']) {
    assert.equal(supportsManagedReasoningControl('local', 'openai-compatible', 'https://proxy.example/v1', 'custom', mode), true);
    assert.equal(supportsManagedReasoningControl('local', 'anthropic', 'https://proxy.example/v1', 'custom', mode), false);
  }
  for (const mode of ['', 'none', 'invalid']) {
    assert.equal(supportsManagedReasoningControl('local', 'openai-compatible', 'https://proxy.example/v1', 'qwen3', mode), false);
  }
  assert.equal(supportsManagedReasoningControl('local', 'openai-compatible', 'https://api.deepseek.com', 'deepseek-v4-pro', 'none'), false);
});

test('per-model reasoning modes survive refresh and current row preview', () => {
  const rows = [{ id: 'a', display_name: 'A', reasoning_control_mode: 'thinking-type', reasoning_enabled: false, plan_edited: true }];
  const refreshed = mergeDiscoveredPlan([{ id: 'a', display_name: 'Vendor A' }], rows);
  assert.equal(refreshed[0].reasoning_control_mode, 'thinking-type');
  assert.equal(refreshed[0].reasoning_enabled, false);
  const current = modelRoutingPlanRows({ provider_model_name: 'a', display_name: 'A', reasoning_control_mode: 'enable-thinking', reasoning_enabled: true }, refreshed);
  assert.equal(current[0].reasoning_control_mode, 'enable-thinking');
});

test('connection controls use the standard input height and compact template icon', () => {
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  assert.doesNotMatch(page, /min-h-\[56px\]/);
  assert.equal(page.match(/h-\[38px\]/g)?.length, 3);
  assert.match(page, /h-6 w-6 rounded-lg/);
});

test('mode is preserved in cancel snapshot, single and batch payloads; endpoint changes reset it', () => {
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  assert.match(page, /reasoning_control_mode: card\.reasoning_control_mode,/);
  assert.equal(page.match(/reasoning_control_mode: card\.reasoning_control_mode \?\? ''/g)?.length, 2);
  assert.match(page, /reasoning_control_mode: model\.reasoning_control_mode \?\? ''/);
  assert.match(page, /next\.reasoning_control_mode = ''/);
  assert.match(page, /patch\.reasoning_control_mode !== undefined/);
});
