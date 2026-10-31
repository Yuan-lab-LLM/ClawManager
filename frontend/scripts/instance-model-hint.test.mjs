import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createRequire } from 'node:module';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const source = read('../src/components/InstanceModelHint.tsx');
function harness() {
  const states = [];
  const requests = [];
  const timers = new Map();
  let index = 0, effect, cleanup;
  const document = { hidden: false };
  const mocks = {
    react: {
      useId: () => 'hint',
      useState: (initial) => { const key = index++; if (!(key in states)) states[key] = initial; return [states[key], (value) => { states[key] = value; }]; },
      useEffect: (fn) => { effect = fn; },
    },
    '../contexts/I18nContext': { useI18n: () => ({ locale: 'zh-CN' }) },
    '../services/instanceService': { instanceService: { getInstanceSessionUsage: (id) => new Promise((resolve, reject) => requests.push({ id, resolve, reject })) } },
  };
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } });
  const exports = {};
  new Function('require', 'exports', 'document', 'setTimeout', 'clearTimeout', outputText)(
    (id) => mocks[id] || require(id), exports, document,
    (fn) => { const id = Symbol(); timers.set(id, fn); return id; }, (id) => timers.delete(id),
  );
  return {
    requests, timers, document,
    render(props = { instanceId: 1, active: true }) { index = 0; return renderToStaticMarkup(exports.default(props)); },
    start() { cleanup = effect(); },
    stop() { cleanup?.(); },
    async tick() { const [id, fn] = timers.entries().next().value; timers.delete(id); await fn(); },
  };
}
const flush = () => new Promise((resolve) => setImmediate(resolve));
const result = (model, status = 'completed') => ({ summary: { latest_requested_model: 'auto', latest_actual_model: model, latest_model_status: status, latest_latency_ms: 9000, latest_ttft_ms: 123, latest_model_at: '2026-09-23T07:16:54Z' } });

test('detail toolbar includes hint next to refresh, history includes actual model', () => {
  const frame = read('../src/components/InstanceServiceFrame.tsx');
  assert.match(frame, /<InstanceModelHint instanceId=\{instanceId\} active=\{isAvailable\}/);
  assert.ok(frame.indexOf('<InstanceModelHint') < frame.indexOf('onClick={handleRefresh}'));
  assert.match(read('../src/components/InstanceSessionUsagePanel.tsx'), /trace.actual_provider_model/);
});

test('completed model, timings, scope disclaimer and keyboard-accessible tooltip render', async () => {
  const h = harness(); h.render(); h.start();
  assert.equal(h.requests[0].id, 1);
  assert.equal(h.timers.size, 0); // no overlapping request
  h.requests[0].resolve(result('deepseek-v4-flash')); await flush();
  const html = h.render();
  for (const text of ['最近回答 · deepseek-v4-flash', 'auto', '9.00', '123', '其他会话', 'aria-describedby', 'group-focus-within:visible']) assert.ok(html.includes(text), text);
  assert.equal(h.timers.size, 1);
  h.stop(); assert.equal(h.timers.size, 0);
});

test('instance switch fences late responses and does not leak previous model', async () => {
  const h = harness(); h.render(); h.start(); h.stop();
  h.render({ instanceId: 2, active: true }); h.start();
  h.requests[0].resolve(result('old-model')); await flush();
  assert.ok(!h.render({ instanceId: 2, active: true }).includes('old-model'));
  h.requests[1].resolve(result('new-model')); await flush();
  assert.ok(h.render({ instanceId: 2, active: true }).includes('new-model'));
  h.stop();
});

test('errors, empty records and failed calls are not described as successful replies', async () => {
  for (const [payload, expected] of [[{ summary: {} }, '暂无记录'], [result('failed-model', 'failed'), '最近调用 · failed-model'], [null, '暂不可用']]) {
    const h = harness(); h.render(); h.start();
    if (payload) h.requests[0].resolve(payload); else h.requests[0].reject(new Error('network'));
    await flush(); assert.ok(h.render().includes(expected)); h.stop();
  }
});

test('inactive instances and hidden tabs do not poll', async () => {
  const h = harness(); h.render({ instanceId: 1, active: false }); h.start();
  assert.equal(h.requests.length, 0);
  h.document.hidden = true; h.render(); h.start(); await flush();
  assert.equal(h.requests.length, 0);
  assert.equal(h.timers.size, 1); h.stop();
});

test('retrying, attempt failure and terminal failure have distinct labels', async () => {
  for (const [status, expected] of [['retrying', '已切换，等待结果 · glm'], ['failed', '本次尝试失败（不代表整轮任务失败）'], ['request_failed', '自动切换已结束'], ['retry_unknown', '最终结果暂未确认']]) {
    const h = harness(); h.render(); h.start();
    h.requests[0].resolve(result('glm', status)); await flush();
    assert.ok(h.render().includes(expected), status); h.stop();
  }
});
