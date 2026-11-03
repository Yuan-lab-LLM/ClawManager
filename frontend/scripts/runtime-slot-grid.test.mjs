import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const modules = new Map();
function load(url) {
  if (modules.has(url.href)) return modules.get(url.href);
  const exports = {};
  const code = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  runInNewContext(code, { exports, require: name => {
    if (name.endsWith('/I18nContext')) return { useI18n: () => ({ t: key => key }) };
    return name.startsWith('.') ? load(new URL(name + '.ts', url)) : require(name);
  } });
  modules.set(url.href, exports);
  return exports;
}
const { SlotGrid } = load(new URL('../src/pages/admin/RuntimeManagementUI.tsx', import.meta.url));
const states = ['running', 'starting', 'migrating', 'error', 'idle', 'unavailable'];
const pod = count => ({ slots: Array.from({ length: count }, (_, i) => ({
  index: i + 1, container_name: `agent-${i + 1}`, ready: i % 6 !== 5,
  ...(i % 6 < 4 ? { instance_id: i + 1, instance_state: states[i % 6] } : {}),
})) });

test('compact cards render every slot state and select slots beyond the old eight-slot preview', () => {
  const fixture = pod(16);
  let selected;
  const tree = SlotGrid({ pod: fixture, compact: true, selected: 16, onSelect: slot => { selected = slot; } });
  const buttons = tree.props.children.flat().filter(Boolean);
  assert.equal(buttons.length, 16);
  assert.equal(tree.props['aria-label'], 'runtimePods.distribution');
  assert.match(tree.props.className, /shrink-0/);
  assert.match(tree.props.className, /min-h-\[38px\]/);
  for (const [i, button] of buttons.entries()) {
    assert.equal(button.type, 'button');
    assert.equal(button.props.children[0].props.children, String(i + 1).padStart(2, '0'));
    assert.match(button.props['aria-label'], new RegExp(`runtimePods\\.${states[i % 6]}$`));
    assert.equal(button.props['aria-pressed'], i === 15);
    let stopped = false;
    button.props.onClick({ stopPropagation() { stopped = true; } });
    assert.equal(stopped, true);
    assert.equal(selected, fixture.slots[i]);
  }
});

test('large custom inventories retain all actionable slots in a bounded compact grid', () => {
  const tree = SlotGrid({ pod: pod(256), compact: true, onSelect() {} });
  assert.equal(tree.props.children.flat().filter(Boolean).length, 256);
  assert.match(tree.props.className, /max-h-10/);
  assert.match(tree.props.className, /overflow-y-auto/);
});
