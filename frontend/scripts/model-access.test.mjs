import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/modelRoutingPlan.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const { modelRoutingPlanRows, mergeDiscoveredPlan } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('existing model access wins over rediscovery metadata', () => {
  const rows = modelRoutingPlanRows({ provider_model_name: 'm', display_name: 'M', visibility_scope: 'selected_users', allowed_user_ids: [2] },
    [{ id: 'm', visibility_scope: 'all', allowed_user_ids: [] }]);
  assert.equal(rows[0].visibility_scope, 'selected_users');
  assert.deepEqual(rows[0].allowed_user_ids, [2]);
});
test('refresh retains per-row edited access without applying it to other models', () => {
  const rows = mergeDiscoveredPlan([{ id: 'a' }, { id: 'b' }],
    [{ id: 'a', plan_edited: true, visibility_scope: 'selected_users', allowed_user_ids: [7] }]);
  assert.deepEqual(rows[0].allowed_user_ids, [7]);
  assert.equal(rows[1].visibility_scope, undefined);
});
test('single and batch saves send access version and explicit scope', () => {
  const page = readFileSync(new URL('../src/pages/admin/ModelManagementPage.tsx', import.meta.url), 'utf8');
  assert.equal(page.match(/access_policy_version: card.access_policy_version/g)?.length, 3); // snapshot + two save paths
  assert.ok(page.includes('visibility_scope: model.visibility_scope'));
  assert.ok(page.includes("...(patch.allowed_user_ids !== undefined"));
});
