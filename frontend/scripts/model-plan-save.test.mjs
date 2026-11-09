import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const compile = (source) => ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText;
async function load(path) {
  return import(`data:text/javascript;base64,${Buffer.from(compile(read(path))).toString('base64')}`);
}
const routing = await load('../src/lib/modelRoutingPlan.ts');
const selection = await load('../src/lib/modelSelection.ts');
const providers = await load('../src/lib/modelProviderTemplates.ts');
const page = read('../src/pages/admin/ModelManagementPage.tsx');
const section = (start, end) => {
  const offset = page.indexOf(start);
  const limit = page.indexOf(end, offset);
  assert.ok(offset >= 0 && limit > offset);
  return page.slice(offset, limit);
};
// Execute the actual page callbacks with in-memory state and a mocked API.
const makeActions = new Function('env', compile(`
  const { models, setModels, updateCard, setBulkSaving, setPageError, modelService,
    t, selectedRows, mergeBatchSave, modelRoutingPlanRows, plannedModelKind, plannedReasoningEnabled,
    supportsManagedReasoningControl, resolveProviderProtocolType } = env;
  const bulkSaving = false;
  const hasEditingCard = true;
  ${section('const createEmptyModel =', 'const AUTO_DISCOVERY_PROVIDERS')}
  ${section('function uniqueImportedDisplayName', 'function getProviderTypeLabel')}
  ${section('  const applyDiscoveryPlan =', '  const saveCard =')}
  ${section('  const saveSelectedCards =', '  const deleteCard =')}
  return { applyDiscoveryPlan };
`));
const row = (id) => ({ id, display_name: id, suggested_pool: 'general', suggested_quality_tier: 2, suggested_priority: 40 });
function harness(rows, respond, existing = [], sourcePatch = {}) {
  const source = {
    local_id: 'source', isNew: true, isEditing: true,
    display_name: rows[0]?.display_name || '', provider_model_name: rows[0]?.id || '',
    provider_type: 'local', protocol_type: 'openai-compatible', base_url: 'https://proxy.example/v1',
    discovered_models: rows, context_window_tokens: 32000, max_concurrent_requests: 7,
    description: 'Shared description', is_active: false, is_secure: true,
    api_key: 'test-only', input_price: 2, output_price: 3, currency: 'USD',
    ...sourcePatch,
  };
  let state = [source, ...existing];
  let error;
  let busy = false;
  const calls = [];
  const actions = makeActions({
    ...routing, ...selection, ...providers, models: state,
    setModels: (update) => { state = update(state); },
    updateCard: (id, patch) => { state = state.map((item) => item.local_id === id ? { ...item, ...patch } : item); },
    setBulkSaving: (value) => { busy = value; }, setPageError: (value) => { error = value; },
    t: (key) => key,
    modelService: { saveModelsBatch: async (payloads) => {
      calls.push(payloads);
      return respond ? respond(payloads, calls.length) : {
        saved: payloads.map((item, index) => ({ ...item, id: calls.length * 1000 + index })), failed: [],
      };
    } },
  });
  return { save: (ids) => actions.applyDiscoveryPlan('source', ids), calls, state: () => state, error: () => error, busy: () => busy };
}

test('one Save submits only checked models, with shared settings and per-model choices', async () => {
  const h = harness([row('a'), { ...row('b'), tools_support_override: false, reasoning_control_mode: 'enable-thinking', reasoning_enabled: true }, row('c')]);
  await h.save(['b', 'stale-id']);
  assert.equal(h.calls.length, 1);
  const [saved] = h.calls[0];
  assert.equal(saved.provider_model_name, 'b');
  assert.equal(saved.tools_support_override, false);
  assert.equal(saved.reasoning_control_mode, 'enable-thinking');
  assert.equal(saved.reasoning_enabled, true);
  assert.equal(saved.context_window_tokens, 32000);
  assert.equal(saved.max_concurrent_requests, 7);
  assert.equal(saved.description, 'Shared description');
  assert.equal(saved.is_active, false);
  assert.equal(saved.is_secure, true);
  assert.equal(saved.input_price, 2);
  assert.equal(h.state().length, 1);
  assert.ok(h.state()[0].id);
  assert.equal(h.state()[0].isNew, false);
  assert.equal(h.busy(), false);
});

test('empty selection and invalid selected names do not create or save any entries', async () => {
  const h = harness([row('a'), { ...row('b'), display_name: '' }]);
  await h.save([]);
  await h.save(['b']);
  assert.equal(h.calls.length, 0);
  assert.equal(h.state().length, 1);
  assert.equal(h.state()[0].isEditing, true);
  assert.ok(h.state()[0].discovery_error);
});

test('selected image rows save independent capabilities without entering chat or enabling reasoning', async () => {
  const rows = [row('chat'),
    { ...row('image-a'), model_kind: 'image', chat_capable: false, image_generation_enabled: true, image_edit_enabled: false, reasoning_control_mode: 'enable-thinking', supports_reasoning: true },
    { ...row('image-b'), model_kind: 'image', chat_capable: false, image_generation_enabled: false, image_edit_enabled: true },
    { ...row('unconfirmed'), model_kind: 'image', chat_capable: false },
    { ...row('embedding'), model_kind: 'other', chat_capable: false },
  ];
  const h = harness(rows, undefined, [], { is_active: true });
  await h.save(rows.slice(1).map((item) => item.id));
  assert.equal(h.calls[0].length, 4);
  assert.deepEqual(h.calls[0].map((item) => [item.model_kind, item.image_generation_enabled, item.image_edit_enabled, item.is_active, item.reasoning_enabled]), [
    ['image', true, false, true, false], ['image', false, true, true, false], ['image', false, false, false, false], ['other', false, false, false, false],
  ]);
});

test('already configured models are not overwritten or deleted by batch save', async () => {
  const existing = { ...row('a'), id: 42, local_id: 'existing', provider_model_name: 'a', base_url: 'https://proxy.example/v1' };
  const h = harness([row('a'), row('b')], undefined, [existing]);
  await h.save(['a', 'b']);
  assert.deepEqual(h.calls[0].map((item) => item.provider_model_name), ['b']);
  assert.equal(h.state().find((item) => item.id === 42), existing);
});

test('partial API failures retain only failed entries as pending, successes have server IDs', async () => {
  const h = harness([row('a'), row('b')], (payloads) => ({
    saved: [{ ...payloads[0], id: 42 }], failed: [{ ...payloads[1], error: 'denied' }],
  }));
  await h.save(['a', 'b']);
  assert.equal(h.state()[0].id, 42);
  assert.equal(h.state()[0].isNew, false);
  assert.equal(h.state()[1].isNew, true);
  assert.equal(h.state()[1].error, 'denied');
  assert.ok(h.error());
  assert.equal(h.busy(), false);
});

test('interrupted later chunk preserves previous saved IDs and remaining retryable drafts', async () => {
  const rows = Array.from({ length: 201 }, (_, index) => row(`model-${index}`));
  const h = harness(rows, (payloads, call) => {
    if (call === 2) throw new Error('network');
    return { saved: payloads.map((item, index) => ({ ...item, id: index + 1 })), failed: [] };
  });
  await h.save(rows.map((item) => item.id));
  assert.deepEqual(h.calls.map((items) => items.length), [200, 1]);
  assert.equal(h.state().filter((item) => item.id && !item.isNew).length, 200);
  assert.equal(h.state().filter((item) => item.isNew).length, 1);
  assert.ok(h.error());
  assert.equal(h.busy(), false);
});
