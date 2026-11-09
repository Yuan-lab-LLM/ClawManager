import assert from 'node:assert/strict';
import fs from 'node:fs';
import ts from 'typescript';

function extract(file, names) {
  const text = fs.readFileSync(new URL(file, import.meta.url), 'utf8');
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  return source.statements.filter(node =>
    (ts.isFunctionDeclaration(node) && names.includes(node.name?.text)) ||
    (ts.isVariableStatement(node) && node.declarationList.declarations.some(d => names.includes(d.name.getText(source))))
  ).map(node => node.getText(source).replace(/^export /, '')).join('\n');
}
const code = extract('../src/pages/teams/TeamDetailPage.tsx', ['latestMiniHandoff', 'payloadBool']) + '\n' + extract('../src/components/teams/TeamChatWorkspace.tsx', ['workspaceMode']) + '\n' + extract('../src/components/teams/TeamWorkflowMini.tsx', ['buildMiniWorkflow']) + '\n' + extract('../src/components/teams/CollapsibleTable.tsx', ['markdownColumnWidths']);
const compiled = ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
const { latestMiniHandoff, workspaceMode, buildMiniWorkflow, markdownColumnWidths } = new Function(compiled + '\nreturn { latestMiniHandoff, workspaceMode, buildMiniWorkflow, markdownColumnWidths };')();
const event = (id, type, extra = {}) => ({ event: { id }, from: 'leader', to: 'developer', eventType: type, payload: {}, ...extra });
assert.equal(latestMiniHandoff(undefined), undefined);
assert.equal(latestMiniHandoff({ items: [event(1, 'assignment_heartbeat')] }), undefined, 'heartbeat must not masquerade as handoff');
assert.equal(latestMiniHandoff({ items: [event(2, 'outbound', { payload: { sent: true, messageDelivered: false } })] }), undefined, 'failed delivery must not animate');
assert.equal(latestMiniHandoff({ items: [event(3, 'outbound', { to: 'leader' })] }), undefined, 'self action is not transfer');
assert.equal(latestMiniHandoff({ items: [event(5, 'peer_handoff'), event(4, 'outbound'), event(7, 'message_failed')] }).event.id, 5, 'latest valid transfer uses server event identity');
assert.equal(workspaceMode(1540), 'dual');
assert.equal(workspaceMode(1539), 'single');
assert.equal(workspaceMode(1248), 'single');
assert.equal(workspaceMode(1247), 'drawer');
assert.equal(workspaceMode(390), 'drawer');
const cards = [1, 2, 3].map(id => ({ id: `work-item-${id}`, owner: `worker-${id}`, title: `Work ${id}`, summary: 'details', column: 'doing', statusLabel: '执行中', eventType: 'progress', time: id }));
const items = [{ id: 1, work_id: 'a', status: 'running', depends_on: ['b'] }, { id: 2, work_id: 'b', status: 'running' }, { id: 3, status: 'waiting' }];
const nodes = buildMiniWorkflow(cards, items, [], false);
assert.deepEqual(nodes.map(node => node.id), ['work-item-2', 'work-item-1', 'work-item-3']);
assert.equal(nodes.filter(node => node.active).length, 2, 'parallel workers must both be active');
assert.deepEqual(nodes[0].dependsOn, [], 'no fabricated serial dependency');
assert.deepEqual(nodes[1].dependsOn, ['work-item-2']);
assert.equal(nodes[2].active, false, 'blocked worker must not animate');
assert.equal(buildMiniWorkflow(cards, items, [], true).some(node => node.active), false, 'terminal root stops active animation without rewriting ledger');
const failed = buildMiniWorkflow([{ ...cards[0], eventType: 'task_failed' }], items, [], false);
assert.equal(failed[0].tone, 'error');
assert.equal(buildMiniWorkflow(cards, [{ ...items[0], depends_on: ['b'] }, { ...items[1], depends_on: ['a'] }], [], false).length, 3, 'cyclic old ledger must not drop cards or loop');
console.log('PASS: floating workspace breakpoints and evidence-only handoff projection');
const widths = markdownColumnWidths(['成员 ID', '显示名称', '角色', 'Runtime', 'Mode', '状态', '职责'], [['leader', '团队负责人', 'leader', 'openclaw', 'lite', 'idle/idle', '职责'.repeat(1000)]]);
assert.ok(widths.slice(0, 6).every(width => width >= 96 && width <= 176));
assert.equal(widths[6], 400, 'verbose column is bounded without consuming every other column');
assert.deepEqual(markdownColumnWidths(['A', 'B'], [[]]), [96, 96]);
assert.deepEqual(markdownColumnWidths(['A', 'B'], []), [96, 96]);
console.log('PASS: bounded generic table column allocation');
