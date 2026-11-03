import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const compile = (source) => ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText;
const routing = await import(`data:text/javascript;base64,${Buffer.from(compile(read('../src/lib/modelRoutingPlan.ts'))).toString('base64')}`);
const page = read('../src/pages/admin/ModelManagementPage.tsx');
const section = (start, end) => {
  const offset = page.indexOf(start);
  const limit = page.indexOf(end, offset);
  assert.ok(offset >= 0 && limit > offset, `missing callback ${start}`);
  return page.slice(offset, limit);
};
// Run the actual callbacks, not source-pattern assertions, so restore/reset and
// asynchronous discovery behavior are covered with no browser or live API.
const makeActions = new Function('env', compile(`
  const { models, setModels, modelService, mergeDiscoveredPlan, plannedModelKind } = env;
  const hasEditingCard = models.some((card) => card.isEditing);
  const t = (key) => key;
  const canDiscover = () => true;
  const buildDiscoveryKey = (card) => JSON.stringify([card.provider_type, card.protocol_type, card.base_url, card.api_key, card.api_key_secret_ref]);
  const resolveProviderProtocolType = (_provider, protocol) => protocol;
  ${section('const captureSnapshot =', 'const createEmptyModel =')}
  ${section('  const updateCard =', '  const updateDiscoveryPlan =')}
  return { captureSnapshot, buildDiscoveryKey, updateCard, startEditing, cancelEditing, discoverModels };
`));

function harness(respond = async () => [], patch = {}) {
  let calls = 0;
  let state = [{
    id: 1, local_id: 'saved', isNew: false, isEditing: false,
    provider_type: 'local', protocol_type: 'openai-compatible',
    base_url: 'https://example.invalid/v1', api_key: 'test-only', api_key_secret_ref: '',
    provider_model_name: 'a', display_name: 'Model A', model_group: 'general',
    reasoning_enabled: true, reasoning_control_mode: 'thinking',
    visibility_scope: 'selected_users', allowed_user_ids: [2, 3],
    plan_excluded_ids: ['b'], discovered_models: [
      { id: 'a', display_name: 'Model A' },
      { id: 'b', display_name: 'Manual B', suggested_pool: 'reasoning', suggested_quality_tier: 3,
        suggested_priority: 10, reasoning_enabled: true, reasoning_control_mode: 'thinking',
        visibility_scope: 'selected_users', allowed_user_ids: [2], plan_edited: true },
    ], ...patch,
  }, { id: 2, local_id: 'other', provider_model_name: 'other' }];
  const actions = () => makeActions({
    ...routing, models: state, setModels: (update) => { state = update(state); },
    modelService: { discoverModels: async (...args) => { calls++; return respond(...args); } },
  });
  state[0].discovery_key = actions().buildDiscoveryKey(state[0]);
  return { actions, card: () => state[0], state: () => state, calls: () => calls };
}

test('cancel restores model, reasoning, access and selection even after a connection edit', () => {
  const h = harness();
  const original = h.actions().captureSnapshot(h.card());
  const other = h.state()[1];
  h.actions().startEditing(h.card());
  h.actions().updateCard('saved', { base_url: 'https://another.invalid/v1' });
  assert.equal(h.card().provider_model_name, ''); // Connection changes still invalidate drafts.
  h.actions().updateCard('saved', { allowed_user_ids: [9], display_name: 'Changed', error: 'save failed' });
  h.actions().cancelEditing(h.card());
  assert.deepEqual(h.actions().captureSnapshot(h.card()), original);
  assert.equal(h.card().isEditing, false);
  assert.equal(h.card().error, null);
  assert.equal(h.card().edit_snapshot, undefined);
  assert.equal(h.state()[1], other);
  assert.equal(h.calls(), 0);
});

test('cancel without changing fields preserves provider ID and thinking on repeated edits', () => {
  const h = harness();
  for (let i = 0; i < 2; i++) {
    h.actions().startEditing(h.card());
    h.actions().cancelEditing(h.card());
    assert.equal(h.card().provider_model_name, 'a');
    assert.equal(h.card().reasoning_enabled, true);
    assert.equal(h.card().reasoning_control_mode, 'thinking');
  }
});

test('cancel new model discards only the unsaved card', () => {
  const h = harness(undefined, { id: undefined, isNew: true });
  h.actions().cancelEditing(h.card());
  assert.deepEqual(h.state().map((card) => card.local_id), ['other']);
  assert.equal(h.calls(), 0);
});

test('failed refresh preserves manual per-model draft and exclusions through a successful retry', async () => {
  let failing = true;
  const h = harness(async () => {
    if (failing) throw new Error('timeout');
    return [{ id: 'a', display_name: 'Provider A' }, { id: 'b', display_name: 'Provider B', suggested_quality_tier: 1 }];
  });
  const original = structuredClone(h.card());
  await h.actions().discoverModels('saved', true);
  assert.deepEqual(h.card().discovered_models, original.discovered_models);
  assert.deepEqual(h.card().plan_excluded_ids, original.plan_excluded_ids);
  assert.equal(h.card().discovery_key, original.discovery_key);
  assert.equal(h.card().discovering, false);
  assert.ok(h.card().discovery_error);
  failing = false;
  await h.actions().discoverModels('saved', true);
  const b = h.card().discovered_models.find((model) => model.id === 'b');
  for (const key of ['display_name', 'suggested_pool', 'suggested_quality_tier', 'suggested_priority',
    'reasoning_enabled', 'reasoning_control_mode', 'visibility_scope', 'allowed_user_ids']) {
    assert.deepEqual(b[key], original.discovered_models[1][key], key);
  }
  assert.equal(h.card().discovery_error, null);
  assert.equal(h.calls(), 2);
});

test('first discovery failure stops automatic retry loops but allows explicit refresh', async () => {
  const h = harness(async () => { throw new Error('offline'); });
  h.actions().updateCard('saved', { base_url: 'https://new.invalid/v1' });
  await h.actions().discoverModels('saved', false);
  assert.equal(h.card().discovery_key, h.actions().buildDiscoveryKey(h.card()));
  assert.ok(h.card().discovery_error);
  await h.actions().discoverModels('saved', false);
  assert.equal(h.calls(), 1);
  await h.actions().discoverModels('saved', true);
  assert.equal(h.calls(), 2);
  assert.deepEqual(h.card().discovered_models, []);
});

test('stale discovery failure cannot overwrite drafts for a new connection', async () => {
  let reject;
  const h = harness(() => new Promise((_resolve, fail) => { reject = fail; }));
  const pending = h.actions().discoverModels('saved', true);
  h.actions().updateCard('saved', { base_url: 'https://new.invalid/v1' });
  h.actions().updateCard('saved', { discovered_models: [{ id: 'new', display_name: 'New draft' }] });
  reject(new Error('old request failed'));
  await pending;
  assert.deepEqual(h.card().discovered_models, [{ id: 'new', display_name: 'New draft' }]);
  assert.equal(h.card().discovery_error, null);
});
