import { useEffect, useState } from 'react';
import { useI18n } from '../contexts/I18nContext';
import { routingClassifierService, type ClassifierComparison, type ClassifierDraft, type ClassifierResult, type ClassifierSettings, type RoutingMode } from '../services/routingClassifierService';

const errorLabels: Record<string, string> = {
  invalid_url: '服务地址必须是 HTTP/HTTPS 根地址，不能带路径、参数或凭据。',
  blocked_address: '不允许访问回环、链路本地或元数据地址，请使用内网节点或 Service 地址。',
  invalid_timeout: '超时时间应为 100～30000 毫秒。',
  invalid_api_key: '请提供有效的 API Key（无空白的 ASCII 字符串）。',
  key_required_for_new_url: '修改服务地址后需要重新填写 API Key，或明确清除已保存密钥。',
  credential_unreadable: '保存的密钥无法解密，可能是加密密钥发生变化，请重新填写 API Key。',
  encryption_unavailable: '服务器未配置安全的加密密钥；请配置至少 32 字符的 ROUTING_CLASSIFIER_ENCRYPTION_KEY 后重启。',
  storage_error: '配置存储不可用，请检查数据库迁移和服务日志。',
  connection_failed: '后端连接失败，请检查 Service、NodePort、DNS 和防火墙。',
  timeout: 'Laya 调用超时，可适当增加调用超时并检查服务器负载。',
  http_401: 'Laya 访问密钥不正确。',
  http_403: 'Laya 拒绝访问。',
  http_404: '接口不存在，请确认不是镜像仓库地址，并使用服务根地址。',
  http_422: 'Laya 拒绝了请求参数，请确认服务支持当前多语言模型和 token 上限。',
  http_429: 'Laya 请求过多，请稍后重试。',
  http_503: 'Laya 繁忙或未就绪，请稍后重试。',
  busy: '当前 Laya 调用并发已满（测试与 Auto 共用），请稍后重试。',
  input_too_long: '当前提问超出保守输入预算（序列化后 1200 字节），请缩短文本；未静默截断。',
  invalid_input: '请输入有效测试文本（最多 16000 UTF-8 字节）。',
  unsupported_input: '该适配器暂不分类图片或已截断的输入。',
  invalid_response: 'Laya 返回的档位、模型或概率格式不符合约定。',
  response_too_large: '服务响应超过大小限制。',
  response_read_failed: '读取服务响应失败。',
  canceled: '测试已取消。',
  configuration_missing: '尚未保存完整的 Laya 连接配置。',
  invalid_mode: '路由模式无效，请刷新页面后重试。',
  external_confirmation_required: '启用前请确认允许发送用户任务文本及必要用户历史到 Laya。',
};

const settingMode = (setting: ClassifierSettings): RoutingMode => setting.routing_mode ?? (setting.mode === 'hybrid_auto' ? 'hybrid' : setting.mode === 'laya_auto' ? 'laya' : 'rules');

export default function RoutingClassifierSettings() {
  const { locale } = useI18n();
  const zh = locale.startsWith('zh');
  const tr = (cn: string, en: string) => zh ? cn : en;
  const [saved, setSaved] = useState<ClassifierSettings | null>(null);
  const [baseURL, setBaseURL] = useState('');
  const [timeout, setTimeoutValue] = useState(5000);
  const [apiKey, setAPIKey] = useState('');
  const [clearKey, setClearKey] = useState(false);
  const [text, setText] = useState('帮我写一个 Python 排序函数');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [comparison, setComparison] = useState<ClassifierComparison | null>(null);
  const [selectedMode, setSelectedMode] = useState<RoutingMode>('rules');
  const [confirmExternal, setConfirmExternal] = useState(false);

  const message = (code: string) => zh ? (errorLabels[code] ?? `请求失败：${code}`) : `Request failed: ${code}`;
  useEffect(() => {
    let active = true;
    routingClassifierService.get().then((setting) => {
      if (!active) return;
      setSaved(setting); setBaseURL(setting.base_url); setTimeoutValue(setting.timeout_ms);
      setSelectedMode(settingMode(setting));
    }).catch(() => { if (active) setError(zh ? '读取分类器配置失败，请刷新重试。' : 'Failed to load classifier settings. Reload to retry.'); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [zh]);

  const changed = () => { setComparison(null); setNotice(''); setError(''); setConfirmExternal(false); };
  const draft = (): ClassifierDraft => ({ base_url: baseURL.trim(), timeout_ms: timeout, ...(clearKey ? { clear_api_key: true } : apiKey.trim() ? { api_key: apiKey.trim() } : {}) });
  const valid = baseURL.trim() !== '' && Number.isInteger(timeout) && timeout >= 100 && timeout <= 30000;
  const run = async (save: boolean) => {
    setBusy(true); setError(''); setNotice(''); setComparison(null);
    try {
      if (save) {
        const setting = await routingClassifierService.save(draft());
        setSaved(setting); setBaseURL(setting.base_url); setTimeoutValue(setting.timeout_ms);
        setSelectedMode(settingMode(setting)); setConfirmExternal(false);
        setAPIKey(''); setClearKey(false);
        setNotice(setting.mode === 'hybrid_auto'
          ? tr('配置已保存；规则明确命中的请求不调用 Laya，其余符合条件的后续 Auto 请求使用此配置。Laya 不可用时回退规则。', 'Saved. Clear rule matches bypass Laya; subsequent eligible unresolved Auto requests use these settings, with rule fallback on failure.')
          : setting.mode === 'laya_auto'
          ? tr('配置已保存，将用于后续 Auto 请求；进行中的请求可能仍使用旧配置。Laya 不可用时回退到本地规则。', 'Saved for subsequent Auto requests; in-flight requests may use the previous settings. Unavailable Laya falls back to local rules.')
          : tr('配置已保存；正式路由仍使用原有规则。', 'Settings saved; production routing is unchanged.'));
      } else {
        setComparison(await routingClassifierService.compare(text, draft()));
      }
    } catch (err) {
      const code = (err as { response?: { data?: { error?: string } } }).response?.data?.error;
      setError(message(typeof code === 'string' && /^[a-z0-9_]+$/.test(code) ? code : 'connection_failed'));
    } finally { setBusy(false); }
  };
  const connectionDirty = saved != null && (baseURL.trim() !== saved.base_url || timeout !== saved.timeout_ms || apiKey.trim() !== '' || clearKey);
  const canEnable = saved != null && saved.has_api_key && !!saved.base_url && saved.encryption_ready && !connectionDirty;
  const currentMode = saved ? settingMode(saved) : 'rules';
  const applyMode = async (mode: RoutingMode) => {
    setBusy(true); setError(''); setNotice(''); setComparison(null);
    try {
      const setting = await routingClassifierService.setMode(mode, mode !== 'rules' && confirmExternal);
      setSaved(setting); setSelectedMode(settingMode(setting)); setConfirmExternal(false);
      setNotice(settingMode(setting) === 'rules'
        ? tr('已切回本地规则，后续 Auto 请求不再调用 Laya。已开始的分类可能仍按原配置完成。', 'Local rules enabled. Subsequent Auto requests no longer call Laya; in-flight classification may finish with previous settings.')
        : tr('路由模式已启用，无需重启；所有后端副本的后续 Auto 请求使用已保存模式。已开始的分类可能仍按原配置完成。', 'Routing mode enabled without restarting. Subsequent Auto requests across backend replicas use the saved mode; in-flight classification may finish with previous settings.'));
    } catch (err) {
      const code = (err as { response?: { data?: { error?: string } } }).response?.data?.error;
      setError(message(typeof code === 'string' && /^[a-z0-9_]+$/.test(code) ? code : 'connection_failed'));
    } finally { setBusy(false); }
  };
  const tiers = zh ? ['', '1 · 简单任务', '2 · 通用 / 编程', '3 · 复杂任务', '4 · 严格推理'] : ['', '1 · Simple', '2 · General / Coding', '3 · Complex', '4 · Formal reasoning'];
  const assessmentStatus = (status: string) => ({ matched: tr('明确命中', 'Clear match'), ambiguous: tr('弱匹配 / 存在歧义', 'Weak or ambiguous match'), unmatched: tr('未命中', 'No match') })[status] ?? status;
  const assessmentReason = (reason: string) => ({
    explicit_task_pattern: tr('完整匹配已验证的任务模板', 'Full match of a regression-tested task pattern'),
    weak_rule_match: tr('只识别到部分意图，不能独立确定档位', 'Partial intent is insufficient to decide the tier'),
    no_rule_match: tr('没有适用规则，现有档位只是默认值', 'No applicable rule; the current tier is a default'),
    no_user_task: tr('没有可分类的用户文本', 'No user text to classify'),
    context_required: tr('续问或指代需要结合上下文判断', 'Continuation or reference needs context'),
    compound_task: tr('包含多项或附加任务，需要进一步判断', 'Compound or additional tasks need assessment'),
    unsupported_input: tr('包含非文本或被截断的输入', 'Non-text or truncated input'),
  })[reason] ?? reason;
  const probabilityDetails = (result: ClassifierResult) => {
    const probabilities = result.classification?.probabilities;
    if (!probabilities) return null;
    const keys = ['simple', 'medium', 'complex', 'reasoning'] as const;
    if (keys.some((key) => !Number.isFinite(probabilities[key]) || probabilities[key] < 0 || probabilities[key] > 1)) return null;
    const ranking = keys.map((key) => probabilities[key]).sort((a, b) => b - a);
    return <div className="mt-3">
      <table className="w-full text-left text-sm">
        <caption className="mb-2 text-left text-xs text-slate-500">{tr('四档概率分布（模型输出，未经本业务校准）', 'Tier probabilities (not calibrated for this task)')}</caption>
        <thead><tr><th scope="col">{tr('能力档位', 'Tier')}</th><th scope="col" className="text-right">{tr('概率', 'Probability')}</th></tr></thead>
        <tbody>{keys.map((key, index) => <tr key={key} className={result.classification?.required_tier === index + 1 ? 'font-semibold text-blue-700' : ''}>
          <th scope="row" className="py-1 font-normal">{tiers[index + 1]}{result.classification?.required_tier === index + 1 ? tr('（选中）', ' (selected)') : ''}</th>
          <td className="text-right tabular-nums">{(probabilities[key] * 100).toFixed(2)}%</td>
        </tr>)}</tbody>
      </table>
      <p className="mt-2 text-xs text-slate-600">{tr('最高选项概率', 'Top option probability')}: {(ranking[0] * 100).toFixed(2)}% · {tr('前两名差距', 'Top-two gap')}: {((ranking[0] - ranking[1]) * 100).toFixed(2)} {tr('个百分点', 'percentage points')}</p>
      <p className="mt-1 text-xs text-slate-500">{tr('概率和差距仅供诊断，不代表正确率，也没有用于自动回退阈值。', 'These values are diagnostic, not accuracy estimates or automatic fallback thresholds.')}</p>
    </div>;
  };
  const resultCard = (title: string, result: ClassifierResult) => (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <h3 className="font-medium">{title}</h3>
      <p className="mt-1 text-xs text-slate-500">{result.classifier} · {result.duration_ms.toFixed(2)} ms</p>
      {result.classification ? <>
        <p className="mt-3 font-semibold">{tiers[result.classification.required_tier]}</p>
        {result.classification.confidence != null && <p className="mt-1 text-sm">{result.classification.confidence_kind === 'normalized_entropy' || result.classifier.startsWith('laya')
          ? tr('分布集中度（非正确概率）', 'Distribution concentration (not correctness probability)')
          : tr('置信度（非准确率）', 'Confidence (not accuracy)')}: {(result.classification.confidence * 100).toFixed(1)}%</p>}
        {probabilityDetails(result)}
        {result.classification.score != null && <p className="mt-1 text-sm">{tr('规则评分（非置信度）', 'Rule score (not confidence)')}: {result.classification.score.toFixed(3)}</p>}
        {result.classification.rule_assessment && <p className="mt-2 text-sm">{tr('规则覆盖', 'Rule coverage')}: {assessmentStatus(result.classification.rule_assessment.status)} · {assessmentReason(result.classification.rule_assessment.reason)}
          {result.classification.rule_assessment.rule_ids.length > 0 && <span className="mt-1 block text-xs text-slate-500">{result.classification.rule_assessment.rule_ids.join(', ')}</span>}
        </p>}
        {result.classification.signals?.length > 0 && <p className="mt-2 break-words text-xs text-slate-500">{result.classification.signals.join(', ')}</p>}
        {result.classification.warnings?.length > 0 && <p className="mt-2 break-words text-xs text-amber-700">{result.classification.warnings.join(', ')}</p>}
      </> : <p className="mt-3 text-sm text-red-600">{message(result.error_code ?? 'internal_error')}</p>}
    </div>
  );

  return <section className="app-panel p-6" aria-labelledby="routing-classifier-title">
    <h2 id="routing-classifier-title" className="text-xl font-semibold">{tr('自动路由分类器 · Laya 路由与对照', 'Routing classifier · Laya routing and comparison')}</h2>
    <p className="mt-2 text-sm text-slate-600">{tr('Laya 判断任务所需能力档位，Gateway 继续负责权限、模型能力和负载筛选。下方对照测试本身不会切换路由模式。', 'Laya assesses task capability tiers; the Gateway still checks permissions, capabilities and load. Comparison tests do not switch routing mode.')}</p>
    {loading && <p className="mt-3">{tr('正在读取配置…', 'Loading settings…')}</p>}
    {!loading && !saved && <p className="mt-3 text-sm text-red-600">{error}</p>}
    {saved && <>
      <div className={`mt-4 rounded-lg border p-3 text-sm ${saved.mode !== 'manual_test_only' ? 'border-amber-300 bg-amber-50 text-amber-900' : 'border-slate-200 bg-slate-50 text-slate-700'}`}>
        <p className="font-medium">{saved.mode === 'hybrid_auto'
          ? tr('当前模式：Hybrid Auto 已启用（规则优先，拿不准再调用 Laya）', 'Current mode: Hybrid Auto enabled (rules first, Laya for unresolved tasks)')
          : saved.mode === 'laya_auto'
          ? tr('当前模式：Laya Auto 已启用（失败回退规则）', 'Current mode: Laya Auto enabled (rule fallback)')
          : tr('当前模式：本地规则；Laya 仅手动测试', 'Current mode: local rules; Laya manual testing only')}</p>
        <p className="mt-1">{saved.mode === 'hybrid_auto'
          ? tr('规则明确命中时不发送到 Laya；只有拿不准且符合条件的 Auto 请求才将用户任务文本及必要的用户历史发送到已保存地址。安全策略强制本地、手动选模、超长或多模态输入不走 Laya。失败时回退本地规则。', 'Clear rule matches stay local. Only eligible unresolved Auto requests send user task text and relevant user history to the saved URL. Security-forced local requests, explicit models, oversized or multimodal input bypass Laya. Failures fall back to rules.')
          : saved.mode === 'laya_auto'
          ? tr('符合条件的 Auto 请求会将用户任务文本及必要的用户历史发送到已保存的 Laya 地址。安全策略强制本地、手动选模、超长或多模态输入不走 Laya。每次分类会增加网络与推理耗时。', 'Eligible Auto requests send user task text and relevant user history to the saved Laya URL. Security-forced local requests, explicit models, oversized or multimodal input bypass Laya. Classification adds network and inference latency.')
          : tr('不会自动发送真实用户会话。先保存连接配置并完成对照测试，再在下方启用路由模式。', 'Real conversations are not sent automatically. Save and test the connection before enabling a routing mode below.')}</p>
        <p className="mt-1 text-xs">{tr('页面保存的模式优先于部署环境变量，无需重启。保存连接配置和运行对照不会切换模式；启用对所有符合条件的 Auto 请求生效，不限当前实例。', 'The saved mode overrides the deployment environment without restarting. Connection saves and comparisons do not switch modes. Enabling affects all eligible Auto requests, not just this instance.')}</p>
        <p className="mt-1 text-xs">{saved.mode_source === 'saved' ? tr('模式来源：管理端已保存配置', 'Mode source: saved administrator configuration') : tr('模式来源：部署默认配置', 'Mode source: deployment default')}</p>
        {saved.mode !== 'manual_test_only' && (!saved.has_api_key || !saved.base_url) && <p className="mt-2 font-medium">{tr('尚未保存完整连接配置，Auto 将回退到本地规则。', 'Saved connection settings are incomplete; Auto will fall back to local rules.')}</p>}
      </div>
      <fieldset disabled={busy} className="mt-4 rounded-lg border border-slate-200 p-4 disabled:opacity-70">
        <legend className="px-1 text-sm font-semibold">{tr('启用自动路由模式', 'Enable automatic routing mode')}</legend>
        <label className="block text-sm">{tr('路由模式', 'Routing mode')}
          <select className="app-input mt-1 w-full" value={selectedMode} onChange={(e) => { setSelectedMode(e.target.value as RoutingMode); setConfirmExternal(false); setNotice(''); setError(''); }}>
            <option value="rules">{tr('仅本地规则（不调用 Laya）', 'Local rules only (no Laya)')}</option>
            <option value="hybrid">{tr('规则优先 + Laya（推荐）', 'Rules first + Laya (recommended)')}</option>
            <option value="laya">{tr('Laya 优先（失败回退规则）', 'Laya first (rule fallback on failure)')}</option>
          </select>
        </label>
        {selectedMode !== 'rules' && <>
          <p className="mt-2 text-xs text-slate-600">{selectedMode === 'hybrid' ? tr('规则明确命中时直接使用；拿不准再调用 Laya，失败回退规则。', 'Clear rule matches stay local; unresolved tasks use Laya, with rule fallback on failure.') : tr('符合条件的任务优先调用 Laya，会增加分类延迟；失败回退规则。', 'Eligible tasks use Laya first, adding classification latency, with rule fallback on failure.')}</p>
          <label className="mt-3 flex items-start gap-2 text-sm"><input type="checkbox" checked={confirmExternal} onChange={(e) => setConfirmExternal(e.target.checked)} />{tr('我确认允许将符合条件的用户任务文本及必要用户历史发送到已保存的 Laya 服务。', 'I allow eligible user task text and necessary user history to be sent to the saved Laya service.')}</label>
          {!canEnable && <p className="mt-2 text-sm text-amber-700">{tr('请先保存完整连接配置和密钥；如有未保存的修改，请先保存。建议完成对照测试后启用。', 'Save complete connection settings and credentials first, including any pending edits. Test the connection before enabling.')}</p>}
        </>}
        <div className="mt-3 flex flex-wrap gap-3">
          <button type="button" className="app-button-primary" disabled={busy || selectedMode === currentMode || (selectedMode !== 'rules' && (!canEnable || !confirmExternal))} onClick={() => void applyMode(selectedMode)}>
            {selectedMode === currentMode ? tr('当前模式已生效', 'Current mode is active') : selectedMode === 'rules' ? tr('保存并使用本地规则', 'Save and use local rules') : selectedMode === 'hybrid' ? tr('启用规则优先 + Laya', 'Enable rules first + Laya') : tr('启用 Laya 优先', 'Enable Laya first')}
          </button>
          {currentMode !== 'rules' && <button type="button" className="app-button-secondary" disabled={busy} onClick={() => void applyMode('rules')}>{tr('停用 Laya，切回本地规则', 'Disable Laya and use local rules')}</button>}
        </div>
      </fieldset>
      <fieldset disabled={busy} className="mt-5 grid gap-4 md:grid-cols-2 disabled:opacity-70">
        <label className="text-sm">{tr('Laya 服务根地址', 'Laya service URL')}
          <input className="app-input mt-1 w-full" value={baseURL} placeholder="http://10.130.14.23:30800" onChange={(e) => { setBaseURL(e.target.value); changed(); }} />
        </label>
        <label className="text-sm">{tr('Laya 调用超时 / 毫秒（测试与 Auto 共用）', 'Laya timeout / ms (tests and Auto)')}
          <input type="number" min={100} max={30000} step={100} className="app-input mt-1 w-full" value={timeout} onChange={(e) => { setTimeoutValue(Number(e.target.value)); changed(); }} />
        </label>
        <label className="text-sm">API Key
          <input type="password" autoComplete="new-password" disabled={clearKey} className="app-input mt-1 w-full" value={apiKey} placeholder={saved.has_api_key ? tr('已保存；留空保留，不回显密钥', 'Saved; leave blank to retain') : tr('填写 Laya 访问密钥', 'Enter Laya API key')} onChange={(e) => { setAPIKey(e.target.value); changed(); }} />
        </label>
        <div className="text-sm"><span>{tr('模型', 'Model')}: multilingual</span>
          <label className="mt-3 flex items-center gap-2"><input type="checkbox" checked={clearKey} onChange={(e) => { setClearKey(e.target.checked); setAPIKey(''); changed(); }} />{tr('清除已保存的密钥（保存后生效）', 'Clear saved key on save')}</label>
        </div>
        <p className="text-xs text-slate-500 md:col-span-2">{tr('地址由 ClawManager 后端访问，不是浏览器访问。同集群可用 http://laya.laya-system.svc.cluster.local:8000；跨集群使用可达的 NodePort/HTTPS 地址。HTTP 仅用于可信内网。', 'The ClawManager backend connects to this URL. Use the internal Service address in the same cluster, or a reachable NodePort/HTTPS endpoint across clusters. HTTP is for trusted LANs only.')}</p>
        {!saved.encryption_ready && <p className="text-sm text-amber-700 md:col-span-2">{message('encryption_unavailable')}</p>}
        <div className="md:col-span-2"><button type="button" className="app-button-primary" disabled={!valid || busy} onClick={() => void run(true)}>{tr('保存连接配置', 'Save connection settings')}</button></div>
        <label className="text-sm md:col-span-2">{tr('测试文本（仅发送你在此处填写的内容）', 'Test text (only this text is sent)')}
          <textarea rows={4} maxLength={4000} className="app-input mt-1 w-full" value={text} onChange={(e) => { setText(e.target.value); changed(); }} />
        </label>
        <p className="text-xs text-slate-500 md:col-span-2">{tr('本版采用保守输入预算：序列化后最多 1200 UTF-8 字节，过长会明确报错。使用短文本测试，勿粘贴敏感信息。', 'Conservative input budget: 1200 serialized UTF-8 bytes. Long input is rejected explicitly. Use short text without sensitive information.')}</p>
        <div className="md:col-span-2"><button type="button" className="app-button-primary" disabled={!valid || !text.trim() || clearKey || (!apiKey.trim() && !saved.has_api_key) || busy} onClick={() => void run(false)}>{busy ? tr('处理中…', 'Working…') : tr('测试连接并对照分类', 'Test connection and compare')}</button>
          <span className="ml-3 text-xs text-slate-500">{tr('使用当前表单；无需先保存，不自动保存测试内容。', 'Uses the current form without saving settings or test text.')}</span>
        </div>
      </fieldset>
      {error && <p role="alert" className="mt-3 text-sm text-red-600">{error}</p>}
      {notice && <p role="status" className="mt-3 text-sm text-green-700">{notice}</p>}
      {comparison && <div aria-live="polite" className="mt-5">
        <div className="grid gap-4 md:grid-cols-2">{resultCard(tr('现有本地规则', 'Existing local rules'), comparison.rule)}{resultCard('Laya', comparison.laya)}</div>
        {comparison.hybrid && <div className="mt-4 rounded-lg border border-blue-200 bg-blue-50 p-4 text-sm">
          <h3 className="font-semibold">{tr('Hybrid 决策预览（不改变当前模式）', 'Hybrid decision preview (does not change the current mode)')}</h3>
          <p className="mt-2">{tr('规则判断', 'Rule assessment')}: {assessmentStatus(comparison.hybrid.rule_assessment.status)} · {assessmentReason(comparison.hybrid.rule_assessment.reason)}</p>
          <p className="mt-1">{tr('混合流程需要调用 Laya', 'Hybrid needs Laya')}: {comparison.hybrid.would_call_laya ? tr('是', 'Yes') : tr('否', 'No')}</p>
          <p className="mt-1">{tr('最终采用', 'Selected source')}: {comparison.hybrid.source === 'laya' ? 'Laya' : comparison.hybrid.source === 'rules_fallback' ? tr('本地规则兜底', 'Local rule fallback') : tr('本地规则', 'Local rules')} · {tiers[comparison.hybrid.required_tier]}</p>
          {comparison.hybrid.fallback_reason && <p className="mt-1 text-amber-800">{tr('回退原因', 'Fallback reason')}: {message(comparison.hybrid.fallback_reason)}</p>}
          <p className="mt-1">{tr('分类路径耗时（复用本次对照测量）', 'Classification path time (reused comparison measurements)')}: {comparison.hybrid.duration_ms.toFixed(2)} ms</p>
          <p className="mt-2 text-xs text-slate-600">{tr('为了对照，本次测试仍会尝试调用 Laya；预览说明同一文本在 Hybrid 下会如何处理，不代表已经启用。未模拟真实请求的安全策略、历史上下文或模型筛选。', 'This comparison still attempts Laya for diagnosis. The preview shows how Hybrid would handle this text, not an enabled mode. It does not simulate live security policy, conversation history or model selection.')}</p>
        </div>}
        <p className="mt-3 text-sm">{comparison.same_tier == null ? tr('Laya 未返回有效结果，不能判断是否一致。', 'No valid Laya result to compare.') : comparison.same_tier ? tr('两者档位一致。', 'Both selected the same tier.') : tr('两者档位不同，请结合任务要求评估；一致性不代表准确率。', 'Tiers differ. Review against the task; agreement is not accuracy.')}</p>
        <p className="mt-1 text-xs text-slate-500">{tr('置信度不是准确率，规则评分也不是概率。Laya 耗时包含网络和排队；此次测试不会改变实际选模。', 'Confidence is not accuracy; rule score is not probability. Laya timing includes network and queueing. This test does not change production selection.')}</p>
      </div>}
    </>}
  </section>;
}
