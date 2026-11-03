import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const source = readFileSync(new URL('../src/components/RuntimeMaintenancePanel.tsx', import.meta.url), 'utf8');
function render({ role = 'admin', state, supported = false, busy = false } = {}) {
  const values = [true, state, supported, busy, ''];
  let index = 0;
  const mocks = {
    react: { useState: () => [values[index++], () => {}], useEffect: () => {}, useRef: value => ({ current: value }) },
    '../contexts/AuthContext': { useAuth: () => ({ user: { role } }) },
    '../contexts/I18nContext': { useI18n: () => ({ locale: 'zh-CN' }) },
    '../services/instanceService': { instanceService: {} },
  };
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } });
  const exports = {};
  new Function('require', 'exports', outputText)(id => mocks[id] || require(id), exports);
  return renderToStaticMarkup(exports.default({ instanceId: 8 }));
}
test('only administrators see maintenance controls', () => assert.equal(render({ role: 'user' }), ''));
test('legacy and unknown state cannot start a drain', () => {
  const html = render();
  assert.match(html, /旧实例可继续使用/);
  assert.match(html, /disabled=""[^>]*>开始排空检查/);
  assert.match(html, /不修改镜像/);
});
test('fresh supported runtime can start; active drain can only be cancelled', () => {
  const idle = render({ supported: true, state: { draining: false, drained: false, operation: null } });
  assert.match(idle, /<button type="button" class="app-button-secondary">开始排空检查/);
  const active = render({ supported: true, state: { draining: true, drained: false, operation: { operation_id: 'a'.repeat(32) } } });
  assert.match(active, /disabled=""[^>]*>开始排空检查/);
  assert.match(active, /<button type="button" class="app-button-secondary">取消排空，恢复接单/);
});
test('drained is not advertised as an upgrade or backup certificate', () => {
  const html = render({ supported: true, state: { draining: true, drained: true, operation: null } });
  assert.match(html, /尚不能自动升级或据此备份/);
  assert.doesNotMatch(source, /instanceService\.(?:stopInstance|restartInstance|updateInstance)\(/);
  assert.match(source, /operation_id/);
});

test('durable upgrade fence cannot be cancelled as an ordinary expiring drain', () => {
  const html = render({ supported: true, state: { draining: true, drained: true, operation: { operation_id: 'b'.repeat(32), upgrade_fenced: true } } });
  assert.match(html, /不会因排空超时自动恢复接单/);
  assert.match(html, /disabled=""[^>]*>取消排空，恢复接单/);
  assert.doesNotMatch(html, /5 分钟后自动恢复/);
});
