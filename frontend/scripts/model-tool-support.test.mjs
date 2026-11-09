import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
async function load(path) {
  const { outputText } = ts.transpileModule(read(path), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}
const { modelToolSupport } = await load('../src/lib/modelToolSupport.ts');
const { modelRoutingPlanRows } = await load('../src/lib/modelRoutingPlan.ts');

test('manual true and false take precedence over discovery', () => {
  for (const tools_support_override of [true, false]) {
    for (const supports_tools of [true, false]) {
      assert.equal(modelToolSupport({ tools_support_override, supports_tools, capability_source: 'provider_metadata' }), tools_support_override);
    }
  }
});

test('unconfirmed discovery does not pretend to establish lack of tools', () => {
  for (const capability_source of ['', 'unknown', 'name_rule', 'default_rule']) {
    assert.equal(modelToolSupport({ capability_source, supports_tools: false }), null);
  }
  assert.equal(modelToolSupport({ classification_source: 'provider_metadata', supports_tools: false }), false);
  assert.equal(modelToolSupport({ supports_tools: true }), true);
});

test('saved manual choice wins in plan preview despite refreshed metadata', () => {
  const discovered = [{ id: 'a', supports_tools: true, classification_source: 'provider_metadata' }];
  const rows = modelRoutingPlanRows({ provider_model_name: 'a', display_name: 'A', tools_support_override: false, supports_tools: true }, discovered);
  assert.equal(modelToolSupport(rows[0]), false);
  assert.equal(rows[0].tools_support_override, false);
  assert.equal(discovered[0].tools_support_override, undefined);
});

test('batch preview retains each row choice rather than copying the current model to all rows', () => {
  const discovered = [{ id: 'a', supports_tools: true }, { id: 'b', supports_tools: true, tools_support_override: false }];
  const rows = modelRoutingPlanRows({ isNew: true, provider_model_name: 'a', display_name: 'A', tools_support_override: true }, discovered);
  assert.deepEqual(rows.map(modelToolSupport), [true, false]);
  const manual = modelRoutingPlanRows({ isNew: true, provider_model_name: 'custom', tools_support_override: false }, discovered);
  assert.equal(modelToolSupport(manual[0]), false);
});

test('manual model works without discovery and cancel snapshot includes the override', () => {
  assert.equal(modelToolSupport(modelRoutingPlanRows({ provider_model_name: 'manual', tools_support_override: true }, [])[0]), true);
  const page = read('../src/pages/admin/ModelManagementPage.tsx');
  assert.equal(page.match(/tools_support_override: card\.tools_support_override/g).length, 3); // snapshot + single save + batch save
  assert.match(page, /tools_support_override: model\.tools_support_override/);
  assert.match(page, /discovered_models: mergeDiscoveredPlan/);
  assert.match(read('../src/lib/modelRoutingPlan.ts'), /tools_support_override: draft\.tools_support_override/);
  assert.match(page, /patch\.tools_support_override != null/);
  assert.match(read('../src/components/ModelDiscoveryPlan.tsx'), /tools_support_override: checked/);
});
