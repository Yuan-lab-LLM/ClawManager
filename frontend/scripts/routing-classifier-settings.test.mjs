import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createRequire } from 'node:module';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const source = readFileSync(new URL('../src/components/RoutingClassifierSettings.tsx', import.meta.url), 'utf8');
const setting = { base_url: 'http://laya.example:8000', timeout_ms: 5000, has_api_key: true, encryption_ready: true, model: 'multilingual', mode: 'manual_test_only' };
const flush = () => new Promise((resolve) => setImmediate(resolve));
function harness(result, config = setting) {
  const states = []; const requests = []; let index = 0, effect;
  const mocks = {
    react: {
      useState: (initial) => { const key = index++; if (!(key in states)) states[key] = initial; return [states[key], (value) => { states[key] = value; }]; },
      useEffect: (fn) => { effect = fn; },
    },
    '../contexts/I18nContext': { useI18n: () => ({ locale: 'zh-CN' }) },
    '../services/routingClassifierService': { routingClassifierService: {
      get: async () => config,
      save: async (draft) => { requests.push({ type: 'save', draft }); return config; },
      setMode: async (mode, confirmExternal) => {
        requests.push({ type: 'mode', mode, confirmExternal });
        config = { ...config, routing_mode: mode, mode_source: 'saved', mode: mode === 'rules' ? 'manual_test_only' : `${mode}_auto` };
        return config;
      },
      compare: async (text, draft) => { requests.push({ type: 'compare', text, draft }); return result; },
    } },
  };
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } });
  const exports = {};
  new Function('require', 'exports', outputText)((id) => mocks[id] ?? require(id), exports);
  const render = () => { index = 0; return exports.default(); };
  function nodes(node, predicate) {
    if (!node || typeof node !== 'object') return [];
    if (Array.isArray(node)) return node.flatMap((item) => nodes(item, predicate));
    return [...(predicate(node) ? [node] : []), ...nodes(node.props?.children, predicate)];
  }
  return { requests, render, markup: () => renderToStaticMarkup(render()),
    async mount() { render(); effect(); await flush(); },
    find(predicate) { const node = nodes(render(), predicate)[0]; assert.ok(node, 'element missing'); return node; },
  };
}
const result = (error = false) => ({
  production_routing_changed: false,
  ...(error ? {} : { same_tier: true }),
  rule: { status: 'success', classifier: 'heuristic', duration_ms: 0.2, classification: { required_tier: 2, score: 0.4, signals: [], warnings: [] } },
  laya: error ? { status: 'error', classifier: 'laya', duration_ms: 5000, error_code: 'timeout' } : { status: 'success', classifier: 'laya', duration_ms: 350, classification: { required_tier: 2, confidence: 0.9, signals: [], warnings: [] } },
});

test('manual-only entry, saved key is never echoed', async () => {
  const h = harness(result()); await h.mount();
  assert.match(h.markup(), /仅手动测试/);
  const password = h.find((node) => node.type === 'input' && node.props.type === 'password');
  assert.equal(password.props.value, ''); assert.match(password.props.placeholder, /已保存/);
  assert.equal(h.requests.length, 0);
});
test('draft comparison does not save, shows different score semantics, edits clear result', async () => {
  const h = harness(result()); await h.mount();
  h.find((node) => node.type === 'button' && node.props.children === '测试连接并对照分类').props.onClick(); await flush();
  assert.equal(h.requests.length, 1); assert.equal(h.requests[0].type, 'compare'); assert.equal(h.requests[0].draft.api_key, undefined);
  assert.match(h.markup(), /规则评分（非置信度）/); assert.match(h.markup(), /90.0%/); assert.match(h.markup(), /两者档位一致/);
  h.find((node) => node.type === 'textarea').props.onChange({ target: { value: '新问题' } });
  assert.doesNotMatch(h.markup(), /两者档位一致/);
});
test('save clears typed key; clear action is explicit', async () => {
  const h = harness(result()); await h.mount();
  h.find((node) => node.type === 'input' && node.props.type === 'password').props.onChange({ target: { value: 'test-only-key' } });
  h.find((node) => node.type === 'button' && node.props.children === '保存连接配置').props.onClick(); await flush();
  assert.equal(h.requests[0].draft.api_key, 'test-only-key');
  assert.equal(h.find((node) => node.type === 'input' && node.props.type === 'password').props.value, '');
  h.find((node) => node.type === 'input' && node.props.type === 'checkbox').props.onChange({ target: { checked: true } });
  h.find((node) => node.type === 'button' && node.props.children === '保存连接配置').props.onClick(); await flush();
  assert.equal(h.requests[1].draft.clear_api_key, true); assert.equal(h.requests[1].draft.api_key, undefined);
});
test('Laya failure keeps rule result and is not reported as agreement', async () => {
  const h = harness(result(true)); await h.mount();
  h.find((node) => node.type === 'button' && node.props.children === '测试连接并对照分类').props.onClick(); await flush();
  assert.match(h.markup(), /Laya 调用超时/); assert.match(h.markup(), /规则评分/);
  assert.match(h.markup(), /不能判断是否一致/); assert.doesNotMatch(h.markup(), /两者档位一致/);
});
test('settings page includes entry and endpoints use admin-only group', () => {
  const page = readFileSync(new URL('../src/pages/admin/SystemSettingsPage.tsx', import.meta.url), 'utf8');
  assert.match(page, /<RoutingClassifierSettings \/>/);
  const main = readFileSync(new URL('../../backend/cmd/server/main.go', import.meta.url), 'utf8');
  assert.match(main, /adminSystemSettings\.Use\(middleware.NewAdminAuth\(userRepo\)\)/);
  assert.match(main, /adminSystemSettings\.POST\("\/routing-classifier\/compare"/);
  assert.match(main, /adminSystemSettings\.PUT\("\/routing-classifier\/mode", routingClassifierHandler.SetMode\)/);
  assert.doesNotMatch(main, /WithRoutingClassifier\(/);
  assert.match(main, /LiveRoutingOption\(os\.Getenv\("CLAWMANAGER_ROUTING_CLASSIFIER"\)\)/);
  assert.match(main, /WithDeploymentStore\(deploymentStore\),\s*classifierOption(?:,|\))/);
});

test('live mode discloses data transfer and save affects subsequent Auto requests', async () => {
  const h = harness(result(), { ...setting, mode: 'laya_auto' }); await h.mount();
  assert.match(h.markup(), /Laya Auto 已启用/);
  assert.match(h.markup(), /用户任务文本及必要的用户历史/);
  assert.doesNotMatch(h.markup(), /不会自动发送真实用户会话/);
  h.find((node) => node.type === 'button' && node.props.children === '保存连接配置').props.onClick(); await flush();
  assert.match(h.markup(), /将用于后续 Auto 请求/);
  assert.doesNotMatch(h.markup(), /正式路由仍使用原有规则/);
});

test('live mode without credentials warns that Auto will fall back', async () => {
  const h = harness(result(), { ...setting, mode: 'laya_auto', has_api_key: false }); await h.mount();
  assert.match(h.markup(), /尚未保存完整连接配置/);
});

test('Laya diagnostics distinguish entropy from four probabilities and top-two gap', async () => {
  const data = result();
  data.laya.classification = { required_tier: 3, confidence: 0.528, confidence_kind: 'normalized_entropy',
    probabilities: { simple: 0.05, medium: 0.20, complex: 0.70, reasoning: 0.05 }, signals: [], warnings: [] };
  const h = harness(data); await h.mount();
  h.find((node) => node.type === 'button' && node.props.children === '测试连接并对照分类').props.onClick(); await flush();
  const html = h.markup();
  assert.match(html, /分布集中度（非正确概率）/);
  assert.match(html, /52.8%/);
  assert.match(html, /四档概率分布/);
  assert.match(html, /3 · 复杂任务（选中）/);
  assert.match(html, /70.00%/);
  assert.match(html, /50.00.*个百分点/);
  assert.match(html, /没有用于自动回退阈值/);
});

test('legacy response without probabilities renders without fabricated distribution', async () => {
  const h = harness(result()); await h.mount();
  h.find((node) => node.type === 'button' && node.props.children === '测试连接并对照分类').props.onClick(); await flush();
  assert.doesNotMatch(h.markup(), /四档概率分布/);
  assert.match(h.markup(), /分布集中度（非正确概率）/);
});

test('hybrid mode discloses selective transfer and saving does not switch mode', async () => {
  const h = harness(result(), { ...setting, mode: 'hybrid_auto' }); await h.mount();
  assert.match(h.markup(), /Hybrid Auto 已启用/);
  assert.match(h.markup(), /规则明确命中时不发送到 Laya/);
  assert.match(h.markup(), /页面保存的模式优先于部署环境变量，无需重启/);
  assert.doesNotMatch(h.markup(), /不会自动发送真实用户会话/);
  h.find((node) => node.type === 'button' && node.props.children === '保存连接配置').props.onClick(); await flush();
  assert.match(h.markup(), /规则明确命中的请求不调用 Laya/);
  assert.equal(h.requests[0].draft.mode, undefined);
});

test('enable hybrid requires saved settings and explicit data-transfer confirmation', async () => {
  const h = harness(result()); await h.mount();
  h.find((node) => node.type === 'select').props.onChange({ target: { value: 'hybrid' } });
  const button = () => h.find((node) => node.type === 'button' && node.props.children === '启用规则优先 + Laya');
  assert.equal(button().props.disabled, true);
  h.find((node) => node.type === 'input' && node.props.type === 'checkbox').props.onChange({ target: { checked: true } });
  assert.equal(button().props.disabled, false);
  button().props.onClick(); await flush();
  assert.deepEqual(h.requests, [{ type: 'mode', mode: 'hybrid', confirmExternal: true }]);
  assert.match(h.markup(), /Hybrid Auto 已启用/);
  assert.match(h.markup(), /路由模式已启用，无需重启/);
  assert.match(h.markup(), /模式来源：管理端已保存配置/);
});

test('unsaved connection edits prevent enabling and reset consent', async () => {
  const h = harness(result()); await h.mount();
  h.find((node) => node.type === 'select').props.onChange({ target: { value: 'laya' } });
  h.find((node) => node.type === 'input' && node.props.type === 'checkbox').props.onChange({ target: { checked: true } });
  h.find((node) => node.type === 'input' && node.props.type === 'password').props.onChange({ target: { value: 'new-key' } });
  assert.equal(h.find((node) => node.type === 'button' && node.props.children === '启用 Laya 优先').props.disabled, true);
  assert.equal(h.find((node) => node.type === 'input' && node.props.type === 'checkbox').props.checked, false);
  assert.equal(h.requests.length, 0);
});

test('disable works with missing credentials and invalid unsaved connection', async () => {
  const h = harness(result(), { ...setting, mode: 'hybrid_auto', has_api_key: false, encryption_ready: false }); await h.mount();
  h.find((node) => node.type === 'input' && node.props.type === 'number').props.onChange({ target: { value: '0' } });
  const stop = h.find((node) => node.type === 'button' && node.props.children === '停用 Laya，切回本地规则');
  assert.equal(stop.props.disabled, false);
  stop.props.onClick(); await flush();
  assert.deepEqual(h.requests, [{ type: 'mode', mode: 'rules', confirmExternal: false }]);
  assert.match(h.markup(), /已切回本地规则/);
  assert.doesNotMatch(h.markup(), /Hybrid Auto 已启用/);
});

test('hybrid preview shows clear rule match even when diagnostic Laya fails', async () => {
  const data = result(true);
  const assessment = { status: 'matched', reason: 'explicit_task_pattern', rule_ids: ['small_function_v1'] };
  data.rule.classification.rule_assessment = assessment;
  data.hybrid = { rule_assessment: assessment, would_call_laya: false, source: 'rules', required_tier: 2, duration_ms: 0.2 };
  const h = harness(data); await h.mount();
  h.find((node) => node.type === 'button' && node.props.children === '测试连接并对照分类').props.onClick(); await flush();
  assert.match(h.markup(), /明确命中/);
  assert.match(h.markup(), /small_function_v1/);
  assert.match(h.markup(), /混合流程需要调用 Laya.*否/);
  assert.match(h.markup(), /最终采用.*本地规则/);
  assert.match(h.markup(), /本次测试仍会尝试调用 Laya/);
  assert.match(h.markup(), /不代表已经启用/);
  assert.equal(h.requests.length, 1);
});

test('hybrid preview shows unresolved Laya selection and error fallback distinctly', async () => {
  for (const failed of [false, true]) {
    const data = result(failed);
    data.hybrid = { rule_assessment: { status: 'unmatched', reason: 'no_rule_match', rule_ids: [] },
      would_call_laya: true, source: failed ? 'rules_fallback' : 'laya', required_tier: failed ? 1 : 2,
      duration_ms: failed ? 5000.2 : 350.2, ...(failed ? { fallback_reason: 'timeout' } : {}) };
    const h = harness(data); await h.mount();
    h.find((node) => node.type === 'button' && node.props.children === '测试连接并对照分类').props.onClick(); await flush();
    assert.match(h.markup(), /没有适用规则，现有档位只是默认值/);
    assert.match(h.markup(), /混合流程需要调用 Laya.*是/);
    if (failed) {
      assert.match(h.markup(), /本地规则兜底/);
      assert.match(h.markup(), /回退原因.*Laya 调用超时/);
    } else {
      assert.match(h.markup(), /最终采用.*Laya/);
      assert.doesNotMatch(h.markup(), /回退原因/);
    }
  }
});
