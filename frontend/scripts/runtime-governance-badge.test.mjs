import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createRequire } from 'node:module';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const source = readFileSync(new URL('../src/components/RuntimeGovernanceBadge.tsx', import.meta.url), 'utf8');
function harness() {
  let state, effect, cleanup;
  const requests = [], timers = new Map();
  const document = { hidden: false };
  const mocks = {
    react: { useState: () => [state, value => { state = value; }], useEffect: fn => { effect = fn; } },
    '../contexts/I18nContext': { useI18n: () => ({ locale: 'zh-CN' }) },
    '../services/instanceService': { instanceService: { getRuntimeGovernance: id => new Promise((resolve, reject) => requests.push({ id, resolve, reject })) } },
  };
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } });
  const exports = {};
  new Function('require', 'exports', 'document', 'setTimeout', 'clearTimeout', outputText)(id => mocks[id] || require(id), exports, document,
    fn => { const id = Symbol(); timers.set(id, fn); return id; }, id => timers.delete(id));
  return { requests, timers, document, render(id = 1) { return renderToStaticMarkup(exports.default({ instanceId: id })); }, start() { cleanup = effect(); }, stop() { cleanup?.(); } };
}
const flush = () => new Promise(resolve => setImmediate(resolve));

test('legacy, enrolled, stale and failed policy reads are not conflated', async () => {
  for (const [mode, expected] of [['legacy', '兼容模式'], ['governed', '完整协议已连接'], ['awaiting_runtime', '等待运行时确认'], [null, '状态暂不可用']]) {
    const h = harness(); h.render(); h.start();
    if (mode) h.requests[0].resolve({ mode }); else h.requests[0].reject(new Error('offline'));
    await flush(); const html = h.render(); assert.ok(html.includes(expected));
    if (!mode) assert.ok(!html.includes('兼容模式'));
    h.stop(); assert.equal(h.timers.size, 0);
  }
});

test('late responses from the previous instance cannot overwrite current state', async () => {
  const h = harness(); h.render(1); h.start(); h.stop(); h.render(2); h.start();
  h.requests[1].resolve({ mode: 'governed' }); await flush();
  h.requests[0].resolve({ mode: 'legacy' }); await flush();
  assert.ok(h.render(2).includes('完整协议已连接')); h.stop();
});

test('hidden pages do not request capability state', () => {
  const h = harness(); h.document.hidden = true; h.render(); h.start();
  assert.equal(h.requests.length, 0); h.stop();
});
