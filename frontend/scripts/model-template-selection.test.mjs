import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const page = readFileSync(new URL('../src/pages/admin/ModelManagementPage.tsx', import.meta.url), 'utf8');
const ast = ts.createSourceFile('page.tsx', page, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const functions = ['buildCurrentTemplate', 'getSelectedTemplate', 'getTemplateOptions'];
const snippets = [];
function visit(node) {
  if (ts.isFunctionDeclaration(node) && functions.includes(node.name?.text)) snippets.push(node.getText(ast));
  if (ts.isVariableDeclaration(node) && ['updateCard', 'captureSnapshot'].includes(node.name.getText(ast))) {
    snippets.push(`const ${node.getText(ast)};`);
  }
  ts.forEachChild(node, visit);
}
visit(ast);
const { outputText: templatesJS } = ts.transpileModule(
  readFileSync(new URL('../src/lib/modelProviderTemplates.ts', import.meta.url), 'utf8'),
  { compilerOptions: { module: ts.ModuleKind.ESNext } },
);
const templates = await import(`data:text/javascript;base64,${Buffer.from(templatesJS).toString('base64')}`);
const { outputText } = ts.transpileModule(snippets.join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } });
function editor(initial) {
  let cards = [initial];
  const api = new Function('BUILTIN_PROVIDER_TEMPLATES', 'findProviderTemplate', 'normalizeBaseUrl', 'resolveProviderProtocolType', 'setModels', 't',
    `${outputText}\nreturn { updateCard, getSelectedTemplate, getTemplateOptions, captureSnapshot };`)(
    templates.BUILTIN_PROVIDER_TEMPLATES, templates.findProviderTemplate, templates.normalizeBaseUrl, templates.resolveProviderProtocolType,
    (update) => { cards = update(cards); }, () => 'Current provider',
  );
  return { ...api, current: () => cards[0] };
}

test('clearing and replacing a Local URL preserves template and editability, not stale discovered models', () => {
  const local = templates.BUILTIN_PROVIDER_TEMPLATES.find((item) => item.providerType === 'local');
  const draft = editor({ local_id: 'a', provider_type: local.providerType, protocol_type: local.protocolType, base_url: local.baseUrl,
    discovered_models: [{ id: 'old-model' }], discovery_key: 'old', plan_excluded_ids: ['old-model'] });
  draft.updateCard('a', { base_url: '' });
  assert.equal(draft.getSelectedTemplate(draft.current(), 'Current provider').id, local.id);
  assert.equal(draft.getSelectedTemplate(draft.current(), '').allowCustomBaseUrl, true);
  assert.equal(draft.current().base_url, '');
  assert.deepEqual(draft.current().discovered_models, []);
  assert.equal(draft.current().discovery_key, undefined);
  draft.updateCard('a', { base_url: 'https://new.example/v1' });
  assert.equal(draft.getSelectedTemplate(draft.current(), '').id, local.id);
});

test('an existing custom provider remains selected after URL deletion', () => {
  const draft = editor({ local_id: 'a', provider_type: 'openai-compatible', base_url: 'https://custom.example/v1' });
  const selected = draft.getSelectedTemplate(draft.current(), 'Current provider');
  draft.updateCard('a', { base_url: '' });
  assert.deepEqual(draft.getSelectedTemplate(draft.current(), 'Current provider'), selected);
  assert.equal(draft.getTemplateOptions(draft.current(), 'Current provider')[0].id, selected.id);
});

test('explicit template changes replace selection; snapshots preserve the original choice', () => {
  const [first, second] = templates.BUILTIN_PROVIDER_TEMPLATES;
  const draft = editor({ local_id: 'a', provider_type: first.providerType, base_url: first.baseUrl, selected_template: first });
  const snapshot = draft.captureSnapshot(draft.current());
  draft.updateCard('a', { selected_template: second, provider_type: second.providerType, base_url: second.baseUrl });
  assert.equal(draft.getSelectedTemplate(draft.current(), '').id, second.id);
  assert.equal(snapshot.selected_template.id, first.id);
  draft.updateCard('a', { provider_type: first.providerType, base_url: first.baseUrl });
  assert.equal(draft.getSelectedTemplate(draft.current(), '').id, first.id);
});
