import React, { useEffect, useMemo, useState } from 'react';
import AdminLayout from '../../../components/AdminLayout';
import {
  secplaneService,
  type SecplaneRule,
  type SecplaneAlert,
  type AlertSource,
  type Severity,
  type DispatchResult,
  type RuleMode,
} from '../../../services/secplaneService';
import DispatchPickerModal from '../../../components/secplane/DispatchPickerModal';
import { useI18n } from '../../../contexts/I18nContext';

// Defense spec keys — display names and help texts come from i18n
interface DefenseSpec {
  name: string;
  ruleID: string;
  supportsMode: boolean;
}

const DEFENSES: DefenseSpec[] = [
  { name: 'selfProtection', ruleID: 'defense.selfProtection', supportsMode: true },
  { name: 'commandBlock', ruleID: 'defense.commandBlock', supportsMode: true },
  { name: 'encodingGuard', ruleID: 'defense.encodingGuard', supportsMode: true },
  { name: 'scriptProvenanceGuard', ruleID: 'defense.scriptProvenanceGuard', supportsMode: true },
  { name: 'memoryGuard', ruleID: 'defense.memoryGuard', supportsMode: true },
  { name: 'userRiskScan', ruleID: 'defense.userRiskScan', supportsMode: false },
  { name: 'skillScan', ruleID: 'defense.skillScan', supportsMode: false },
  { name: 'toolResultScan', ruleID: 'defense.toolResultScan', supportsMode: false },
  { name: 'outputRedaction', ruleID: 'defense.outputRedaction', supportsMode: false },
  { name: 'promptGuard', ruleID: 'defense.promptGuard', supportsMode: false },
  { name: 'loopGuard', ruleID: 'defense.loopGuard', supportsMode: true },
  { name: 'exfiltrationGuard', ruleID: 'defense.exfiltrationGuard', supportsMode: true },
  { name: 'toolCallEnforcement', ruleID: 'defense.toolCallEnforcement', supportsMode: false },
  { name: 'dispatchGuard', ruleID: 'defense.dispatchGuard', supportsMode: true },
];

interface FlagSpec {
  flag: string;
  ruleID: string;
  i18nKey: string;
}

const USER_RISK_FLAGS: FlagSpec[] = [
  { flag: 'jailbreak-bypass', ruleID: 'urf.jailbreak-bypass', i18nKey: 'jailbreak-bypass' },
  { flag: 'system-prompt-exfiltration', ruleID: 'urf.system-prompt-exfiltration', i18nKey: 'system-prompt-exfiltration' },
  { flag: 'disable-plugin', ruleID: 'urf.disable-plugin', i18nKey: 'disable-plugin' },
  { flag: 'plugin-path-access', ruleID: 'urf.plugin-path-access', i18nKey: 'plugin-path-access' },
  { flag: 'dangerous-execution-request', ruleID: 'urf.dangerous-execution-request', i18nKey: 'dangerous-execution-request' },
  { flag: 'sensitive-secret-request', ruleID: 'urf.sensitive-secret-request', i18nKey: 'sensitive-secret-request' },
  { flag: 'third-party-as-instructions', ruleID: 'urf.third-party-as-instructions', i18nKey: 'third-party-as-instructions' },
];

const TOOL_RESULT_FLAGS: FlagSpec[] = [
  { flag: 'role-takeover', ruleID: 'trf.role-takeover', i18nKey: 'role-takeover' },
  { flag: 'policy-bypass', ruleID: 'trf.policy-bypass', i18nKey: 'policy-bypass' },
  { flag: 'tool-induction', ruleID: 'trf.tool-induction', i18nKey: 'tool-induction' },
  { flag: 'secret-request', ruleID: 'trf.secret-request', i18nKey: 'secret-request' },
  { flag: 'exfiltration-request', ruleID: 'trf.exfiltration-request', i18nKey: 'exfiltration-request' },
  { flag: 'remote-script-bootstrap', ruleID: 'trf.remote-script-bootstrap', i18nKey: 'remote-script-bootstrap' },
  { flag: 'remote-binary-bootstrap', ruleID: 'trf.remote-binary-bootstrap', i18nKey: 'remote-binary-bootstrap' },
  { flag: 'system-prompt-leak', ruleID: 'trf.system-prompt-leak', i18nKey: 'system-prompt-leak' },
  { flag: 'approval-bypass', ruleID: 'trf.approval-bypass', i18nKey: 'approval-bypass' },
  { flag: 'disable-claw-aegis', ruleID: 'trf.disable-claw-aegis', i18nKey: 'disable-claw-aegis' },
  { flag: 'high-risk-command', ruleID: 'trf.high-risk-command', i18nKey: 'high-risk-command' },
  { flag: 'credential-exfiltration', ruleID: 'trf.credential-exfiltration', i18nKey: 'credential-exfiltration' },
];

type TabKey = 'defenses' | 'userRisk' | 'toolResult' | 'protected' | 'alerts';

const TAB_KEYS: TabKey[] = ['defenses', 'userRisk', 'toolResult', 'protected', 'alerts'];

const severityChip = (sev: Severity) => {
  const map: Record<Severity, string> = {
    low: 'bg-emerald-100 text-emerald-700 border-emerald-200',
    medium: 'bg-amber-100 text-amber-700 border-amber-200',
    high: 'bg-rose-100 text-rose-700 border-rose-200',
  };
  return map[sev] ?? 'bg-gray-100 text-gray-700 border-gray-200';
};

const actionPill = (action: string) => {
  const lookup: Record<string, string> = {
    blocked: 'bg-rose-100 text-rose-700 border-rose-200',
    redacted: 'bg-amber-100 text-amber-700 border-amber-200',
    observed: 'bg-sky-100 text-sky-700 border-sky-200',
    rerouted: 'bg-indigo-100 text-indigo-700 border-indigo-200',
    allowed: 'bg-emerald-100 text-emerald-700 border-emerald-200',
    block: 'bg-rose-100 text-rose-700 border-rose-200',
    redact: 'bg-amber-100 text-amber-700 border-amber-200',
    observe: 'bg-sky-100 text-sky-700 border-sky-200',
  };
  return lookup[action] || 'bg-gray-100 text-gray-700 border-gray-200';
};

const slugifyResource = (kind: 'pp' | 'psk' | 'ppl', value: string): string => {
  const cleaned = value.trim().toLowerCase().replace(/[^a-z0-9._-]+/g, '_');
  const trimmed = cleaned.replace(/^_+|_+$/g, '').slice(0, 60);
  return `${kind}.${trimmed || Date.now().toString(36)}`;
};

const InputDetectionPage: React.FC = () => {
  const { t } = useI18n();
  const [tab, setTab] = useState<TabKey>('defenses');

  const [defenseRules, setDefenseRules] = useState<SecplaneRule[]>([]);
  const [userRiskRules, setUserRiskRules] = useState<SecplaneRule[]>([]);
  const [toolResultRules, setToolResultRules] = useState<SecplaneRule[]>([]);
  const [protectedPaths, setProtectedPaths] = useState<SecplaneRule[]>([]);
  const [protectedSkills, setProtectedSkills] = useState<SecplaneRule[]>([]);
  const [protectedPlugins, setProtectedPlugins] = useState<SecplaneRule[]>([]);
  const [rulesLoading, setRulesLoading] = useState(true);
  const [rulesError, setRulesError] = useState<string | null>(null);

  const [alerts, setAlerts] = useState<SecplaneAlert[]>([]);
  const [alertsLoading, setAlertsLoading] = useState(false);
  const [alertsError, setAlertsError] = useState<string | null>(null);
  const [alertFilter, setAlertFilter] = useState<{ source: string; ruleID: string }>({ source: '', ruleID: '' });

  const [dispatching, setDispatching] = useState(false);
  const [dispatchResult, setDispatchResult] = useState<DispatchResult | null>(null);
  const [dispatchError, setDispatchError] = useState<string | null>(null);

  const [pickerOpen, setPickerOpen] = useState(false);
  const [savingRuleID, setSavingRuleID] = useState<string | null>(null);
  const [protectedDrafts, setProtectedDrafts] = useState<{ pp: string; psk: string; ppl: string }>({ pp: '', psk: '', ppl: '' });

  const loadAllRules = async () => {
    setRulesLoading(true);
    setRulesError(null);
    try {
      const [d, ur, tr, pp, psk, ppl] = await Promise.all([
        secplaneService.listRules('defense_toggle'),
        secplaneService.listRules('user_risk_flag'),
        secplaneService.listRules('tool_result_flag'),
        secplaneService.listRules('protected_path'),
        secplaneService.listRules('protected_skill'),
        secplaneService.listRules('protected_plugin'),
      ]);
      setDefenseRules(d);
      setUserRiskRules(ur);
      setToolResultRules(tr);
      setProtectedPaths(pp);
      setProtectedSkills(psk);
      setProtectedPlugins(ppl);
    } catch (e: any) {
      setRulesError(e?.response?.data?.error ?? e?.message ?? 'failed to load rules');
    } finally {
      setRulesLoading(false);
    }
  };

  const loadAlerts = async () => {
    setAlertsLoading(true);
    setAlertsError(null);
    try {
      const params: { source?: AlertSource; rule_id?: string; limit: number } = { limit: 100 };
      if (alertFilter.source) params.source = alertFilter.source as AlertSource;
      if (alertFilter.ruleID) params.rule_id = alertFilter.ruleID;
      const items = await secplaneService.listAlerts(params);
      setAlerts(items);
    } catch (e: any) {
      setAlertsError(e?.response?.data?.error ?? e?.message ?? 'failed to load alerts');
    } finally {
      setAlertsLoading(false);
    }
  };

  useEffect(() => { loadAllRules(); }, []);
  useEffect(() => { if (tab === 'alerts') loadAlerts(); }, [tab]);

  const allRulesByID = useMemo(() => {
    const m = new Map<string, SecplaneRule>();
    for (const r of [...defenseRules, ...userRiskRules, ...toolResultRules, ...protectedPaths, ...protectedSkills, ...protectedPlugins]) {
      m.set(r.rule_id, r);
    }
    return m;
  }, [defenseRules, userRiskRules, toolResultRules, protectedPaths, protectedSkills, protectedPlugins]);

  const persistRule = async (next: SecplaneRule) => {
    setSavingRuleID(next.rule_id);
    setRulesError(null);
    try {
      await secplaneService.saveRule(next);
      await loadAllRules();
    } catch (e: any) {
      setRulesError(e?.response?.data?.error ?? e?.message ?? 'failed to save');
    } finally {
      setSavingRuleID(null);
    }
  };

  const hardDeleteRule = async (rule_id: string) => {
    setSavingRuleID(rule_id);
    try {
      await secplaneService.disableRule(rule_id);
      await loadAllRules();
    } catch (e: any) {
      setRulesError(e?.response?.data?.error ?? e?.message ?? 'failed to delete');
    } finally {
      setSavingRuleID(null);
    }
  };

  const runDispatch = async (instanceIDs: number[] | null) => {
    setDispatching(true);
    setDispatchError(null);
    setDispatchResult(null);
    try {
      const result = await secplaneService.dispatchAegis(instanceIDs ?? undefined);
      setDispatchResult(result);
      setPickerOpen(false);
    } catch (e: any) {
      setDispatchError(e?.response?.data?.error ?? e?.message ?? 'dispatch failed');
    } finally {
      setDispatching(false);
    }
  };

  // Get defense display text from i18n
  const defenseDisplay = (name: string) => t(`secplane.inputDetection.defense.${name}`);
  const defenseHelp = (name: string) => t(`secplane.inputDetection.defense.${name}Help`);
  const flagDisplay = (i18nKey: string, kind: 'userRiskFlag' | 'toolResultFlag') => t(`secplane.inputDetection.${kind}.${i18nKey}`);

  const renderToggleRow = (
    spec: { ruleID: string; display: string; help?: string; supportsMode: boolean; defaultSeverity: Severity },
    seedTemplate: () => SecplaneRule,
  ) => {
    const existing = allRulesByID.get(spec.ruleID);
    const rule: SecplaneRule = existing ?? seedTemplate();
    const isSaving = savingRuleID === spec.ruleID;
    const isDisabled = !rule.is_enabled || rule.mode === 'off';
    return (
      <tr key={spec.ruleID} className={`hover:bg-gray-50 ${isDisabled ? 'bg-gray-50/50' : ''}`}>
        <td className="px-4 py-3 align-top">
          <div className={`font-medium ${isDisabled ? 'text-gray-500' : 'text-gray-900'}`}>{spec.display}</div>
          <div className="text-xs text-gray-500">{spec.ruleID}</div>
          {spec.help && <div className="mt-1 max-w-xl text-xs text-gray-500">{spec.help}</div>}
        </td>
        <td className="px-4 py-3 text-center">
          <button
            disabled={isSaving}
            onClick={() => persistRule({ ...rule, is_enabled: !rule.is_enabled })}
            className={`rounded-full border px-3 py-0.5 text-xs ${
              rule.is_enabled
                ? 'bg-emerald-50 text-emerald-700 border-emerald-200'
                : 'bg-gray-100 text-gray-500 border-gray-200'
            } disabled:opacity-60`}
          >
            {isSaving ? '…' : rule.is_enabled ? 'enabled' : 'disabled'}
          </button>
        </td>
        <td className="px-4 py-3 text-center">
          {spec.supportsMode ? (
            <select
              value={rule.mode}
              disabled={isSaving || !rule.is_enabled}
              onChange={(e) => persistRule({ ...rule, mode: e.target.value as RuleMode })}
              className="rounded border border-gray-300 px-2 py-1 text-sm disabled:bg-gray-100 disabled:text-gray-400"
            >
              {(['enforce', 'observe', 'off'] as RuleMode[]).map((m) => (
                <option key={m} value={m}>{t(`secplane.inputDetection.mode.${m}`)}</option>
              ))}
            </select>
          ) : (
            <span className="text-xs text-gray-400" title={t('secplane.inputDetection.defenses.noModeSupport')}>—</span>
          )}
        </td>
        <td className="px-4 py-3 text-center">
          {isDisabled ? (
            <span className="text-xs text-gray-400" title={t('secplane.inputDetection.flag.disabled')}>—</span>
          ) : (
            <span className={`rounded-full border px-2 py-0.5 text-xs ${severityChip(rule.severity)}`}>{rule.severity}</span>
          )}
        </td>
      </tr>
    );
  };

  const renderDefenses = () => {
    type TriState = 'off' | 'observe' | 'enforce';
    type BiState = 'off' | 'on';
    const triStateOf = (r: SecplaneRule): TriState => {
      if (!r.is_enabled || r.mode === 'off') return 'off';
      if (r.mode === 'observe') return 'observe';
      return 'enforce';
    };
    const biStateOf = (r: SecplaneRule): BiState => (r.is_enabled ? 'on' : 'off');

    const applyTri = (r: SecplaneRule, next: TriState): SecplaneRule => {
      switch (next) {
        case 'off': return { ...r, is_enabled: false };
        case 'observe': return { ...r, is_enabled: true, mode: 'observe' };
        case 'enforce': return { ...r, is_enabled: true, mode: 'enforce' };
      }
    };
    const applyBi = (r: SecplaneRule, next: BiState): SecplaneRule => ({
      ...r,
      is_enabled: next === 'on',
      mode: 'enforce',
    });

    const SegBtn: React.FC<{
      active: boolean; disabled?: boolean;
      tone: 'off' | 'observe' | 'enforce' | 'on';
      onClick: () => void; children: React.ReactNode;
    }> = ({ active, disabled, tone, onClick, children }) => {
      const activeClass = {
        off: 'bg-gray-700 text-white',
        observe: 'bg-amber-500 text-white',
        enforce: 'bg-emerald-600 text-white',
        on: 'bg-emerald-600 text-white',
      }[tone];
      return (
        <button disabled={disabled} onClick={onClick}
          className={`px-3 py-1 text-xs transition first:rounded-l-md last:rounded-r-md ${active ? activeClass : 'bg-white text-gray-600 hover:bg-gray-50'} disabled:opacity-60 disabled:cursor-not-allowed`}>
          {children}
        </button>
      );
    };

    const renderTriRow = (d: DefenseSpec) => {
      const existing = allRulesByID.get(d.ruleID);
      const rule: SecplaneRule = existing ?? { rule_id: d.ruleID, kind: 'defense_toggle', display_name: defenseDisplay(d.name), pattern: '', target: 'user_input', severity: 'medium', action: 'observe', mode: 'enforce', is_enabled: true, sort_order: 100 + DEFENSES.findIndex((x) => x.name === d.name) * 10 };
      const state = triStateOf(rule);
      const isSaving = savingRuleID === d.ruleID;
      return (
        <tr key={d.ruleID} className={`hover:bg-gray-50 ${state === 'off' ? 'bg-gray-50/50' : ''}`}>
          <td className="px-4 py-3 align-top">
            <div className={`font-medium ${state === 'off' ? 'text-gray-500' : 'text-gray-900'}`}>{defenseDisplay(d.name)}</div>
            <div className="mt-0.5 text-xs text-gray-500">{defenseHelp(d.name)}</div>
            <div className="mt-0.5 font-mono text-[10px] text-gray-400">{d.ruleID}</div>
          </td>
          <td className="px-4 py-3 text-right">
            <div className="inline-flex divide-x divide-gray-300 overflow-hidden rounded-md border border-gray-300">
              <SegBtn tone="off" active={state === 'off'} disabled={isSaving}
                onClick={() => persistRule(applyTri(rule, 'off'))}>Off</SegBtn>
              <SegBtn tone="observe" active={state === 'observe'} disabled={isSaving}
                onClick={() => persistRule(applyTri(rule, 'observe'))}>Observe</SegBtn>
              <SegBtn tone="enforce" active={state === 'enforce'} disabled={isSaving}
                onClick={() => persistRule(applyTri(rule, 'enforce'))}>Enforce</SegBtn>
            </div>
          </td>
        </tr>
      );
    };

    const renderBiRow = (d: DefenseSpec) => {
      const existing = allRulesByID.get(d.ruleID);
      const rule: SecplaneRule = existing ?? { rule_id: d.ruleID, kind: 'defense_toggle', display_name: defenseDisplay(d.name), pattern: '', target: 'user_input', severity: 'medium', action: 'observe', mode: 'enforce', is_enabled: true, sort_order: 100 + DEFENSES.findIndex((x) => x.name === d.name) * 10 };
      const state = biStateOf(rule);
      const isSaving = savingRuleID === d.ruleID;
      return (
        <tr key={d.ruleID} className={`hover:bg-gray-50 ${state === 'off' ? 'bg-gray-50/50' : ''}`}>
          <td className="px-4 py-3 align-top">
            <div className={`font-medium ${state === 'off' ? 'text-gray-500' : 'text-gray-900'}`}>{defenseDisplay(d.name)}</div>
            <div className="mt-0.5 text-xs text-gray-500">{defenseHelp(d.name)}</div>
            <div className="mt-0.5 font-mono text-[10px] text-gray-400">{d.ruleID}</div>
          </td>
          <td className="px-4 py-3 text-right">
            <div className="inline-flex divide-x divide-gray-300 overflow-hidden rounded-md border border-gray-300">
              <SegBtn tone="off" active={state === 'off'} disabled={isSaving}
                onClick={() => persistRule(applyBi(rule, 'off'))}>Off</SegBtn>
              <SegBtn tone="on" active={state === 'on'} disabled={isSaving}
                onClick={() => persistRule(applyBi(rule, 'on'))}>On</SegBtn>
            </div>
          </td>
        </tr>
      );
    };

    const modeSupporting = DEFENSES.filter((d) => d.supportsMode);
    const booleanOnly = DEFENSES.filter((d) => !d.supportsMode);

    return (
      <div className="space-y-4">
        <div className="rounded-lg bg-blue-50 p-3 text-xs text-blue-700">
          {t('secplane.inputDetection.defenses.explainer')}
          <span className="ml-1 text-blue-900/70">{t('secplane.inputDetection.defenses.enforceHelp')}</span> = {t('secplane.inputDetection.defenses.enforceHelp').includes('Enforce') ? 'block + LLM prompt' : ''};
          <span className="ml-1 text-blue-900/70">{t('secplane.inputDetection.defenses.observeHelp')}</span> = {t('secplane.inputDetection.defenses.observeHelp').includes('Observe') ? 'log only' : ''};
          <span className="ml-1 text-blue-900/70">{t('secplane.inputDetection.defenses.offHelp')}</span>.
        </div>

        <section className="overflow-hidden rounded-xl border border-gray-200 bg-white">
          <div className="border-b border-gray-200 bg-gray-50 px-4 py-2">
            <div className="text-xs font-semibold uppercase tracking-wide text-gray-600">{t('secplane.inputDetection.defenses.modeSupporting')}</div>
            <div className="text-xs text-gray-500">{t('secplane.inputDetection.defenses.modeSupportingSub')}</div>
          </div>
          <table className="min-w-full divide-y divide-gray-100">
            <tbody className="divide-y divide-gray-100">
              {modeSupporting.map(renderTriRow)}
            </tbody>
          </table>
        </section>

        <section className="overflow-hidden rounded-xl border border-gray-200 bg-white">
          <div className="border-b border-gray-200 bg-gray-50 px-4 py-2">
            <div className="text-xs font-semibold uppercase tracking-wide text-gray-600">{t('secplane.inputDetection.defenses.booleanOnly')}</div>
            <div className="text-xs text-gray-500">{t('secplane.inputDetection.defenses.booleanOnlySub')}</div>
          </div>
          <table className="min-w-full divide-y divide-gray-100">
            <tbody className="divide-y divide-gray-100">
              {booleanOnly.map(renderBiRow)}
            </tbody>
          </table>
        </section>
      </div>
    );
  };

  const renderFlagTab = (
    kind: 'user_risk_flag' | 'tool_result_flag',
    flags: FlagSpec[],
    pool: SecplaneRule[],
  ) => {
    const titleKey = kind === 'user_risk_flag' ? 'secplane.inputDetection.flag.userRiskTitle' : 'secplane.inputDetection.flag.toolResultTitle';
    const explainerKey = kind === 'user_risk_flag' ? 'secplane.inputDetection.flag.userRiskExplainer' : 'secplane.inputDetection.flag.toolResultExplainer';
    const flagKind = kind === 'user_risk_flag' ? 'userRiskFlag' : 'toolResultFlag';

    const seedFor = (s: FlagSpec, idx: number): SecplaneRule => ({
      rule_id: s.ruleID,
      kind,
      display_name: flagDisplay(s.i18nKey, flagKind as 'userRiskFlag' | 'toolResultFlag'),
      pattern: '',
      target: kind === 'user_risk_flag' ? 'user_input' : 'tool_output',
      severity: 'high',
      action: 'block',
      mode: 'enforce',
      is_enabled: true,
      sort_order: (kind === 'user_risk_flag' ? 300 : 500) + idx * 10,
    });

    return (
      <div className="space-y-3">
        <div className="rounded-lg bg-blue-50 p-3 text-xs text-blue-700">{t(explainerKey)}</div>
        <div className="overflow-hidden rounded-xl border border-gray-200 bg-white">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50 text-left text-xs uppercase text-gray-500">
              <tr>
                <th className="px-4 py-3">{t(titleKey)}</th>
                <th className="px-4 py-3 text-center">{t('secplane.inputDetection.flag.enabled')}</th>
                <th className="px-4 py-3 text-center">{t('secplane.inputDetection.flag.mode')}</th>
                <th className="px-4 py-3 text-center">{t('secplane.inputDetection.flag.severity')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {flags.map((f, idx) =>
                renderToggleRow(
                  { ruleID: f.ruleID, display: flagDisplay(f.i18nKey, flagKind as 'userRiskFlag' | 'toolResultFlag'), supportsMode: true, defaultSeverity: 'high' },
                  () => seedFor(f, idx),
                ),
              )}
            </tbody>
          </table>
        </div>
        <div className="text-xs text-gray-500">{t('secplane.inputDetection.flag.ruleCount', { count: pool.length, kind })}</div>
      </div>
    );
  };

  const renderProtectedList = (
    titleKey: string,
    placeholder: string,
    pool: SecplaneRule[],
    kindShort: 'pp' | 'psk' | 'ppl',
    kindLong: 'protected_path' | 'protected_skill' | 'protected_plugin',
  ) => {
    const visible = pool.filter((r) => r.is_enabled);
    const draft = protectedDrafts[kindShort];
    const setDraft = (next: string) => setProtectedDrafts((prev) => ({ ...prev, [kindShort]: next }));
    const handleAdd = async () => {
      const value = draft.trim();
      if (!value) return;
      const ruleID = slugifyResource(kindShort, value);
      const next: SecplaneRule = {
        rule_id: ruleID, kind: kindLong, display_name: value, pattern: value,
        target: 'user_input', severity: 'high', action: 'block', mode: 'enforce',
        is_enabled: true, sort_order: 700,
      };
      await persistRule(next);
      setDraft('');
    };
    return (
      <div className="rounded-xl border border-gray-200 bg-white p-4">
        <div className="mb-3 flex items-center justify-between">
          <div className="text-sm font-semibold text-gray-800">{t(titleKey)}</div>
          <span className="text-xs text-gray-500">{t('secplane.inputDetection.protected.activeCount', { count: visible.length })}</span>
        </div>
        <div className="mb-3 flex gap-2">
          <input type="text" value={draft} onChange={(e) => setDraft(e.target.value)}
            placeholder={placeholder} className="flex-1 rounded border border-gray-300 px-3 py-2 text-sm"
            onKeyDown={(e) => { if (e.key === 'Enter') handleAdd(); }} />
          <button disabled={!draft.trim()} onClick={handleAdd}
            className="rounded bg-indigo-600 px-3 py-2 text-sm text-white hover:bg-indigo-700 disabled:opacity-60">
            {t('secplane.inputDetection.protected.addButton')}
          </button>
        </div>
        <ul className="space-y-1">
          {visible.map((r) => (
            <li key={r.rule_id} className="flex items-center gap-2 rounded border border-gray-200 bg-gray-50 px-3 py-2">
              <code className="flex-1 truncate font-mono text-xs text-gray-700" title={r.pattern}>{r.pattern}</code>
              <button disabled={savingRuleID === r.rule_id} onClick={() => hardDeleteRule(r.rule_id)}
                className="text-xs text-rose-600 hover:text-rose-800 disabled:opacity-60">
                {savingRuleID === r.rule_id ? '…' : t('secplane.inputDetection.protected.removeButton')}
              </button>
            </li>
          ))}
          {visible.length === 0 && <li className="text-xs text-gray-500">{t('secplane.inputDetection.protected.empty')}</li>}
        </ul>
      </div>
    );
  };

  const renderProtected = () => (
    <div className="grid gap-4 lg:grid-cols-3">
      {renderProtectedList('secplane.inputDetection.protected.pathTitle', '/path/to/protect', protectedPaths, 'pp', 'protected_path')}
      {renderProtectedList('secplane.inputDetection.protected.skillTitle', 'release-guard', protectedSkills, 'psk', 'protected_skill')}
      {renderProtectedList('secplane.inputDetection.protected.pluginTitle', 'audit-guard', protectedPlugins, 'ppl', 'protected_plugin')}
    </div>
  );

  const renderAlerts = () => (
    <div className="space-y-3">
      <div className="rounded-xl border border-gray-200 bg-white p-3">
        <div className="flex flex-wrap items-center gap-3">
          <label className="text-sm text-gray-700">{t('secplane.inputDetection.alerts.source')}
            <select value={alertFilter.source}
              onChange={(e) => setAlertFilter({ ...alertFilter, source: e.target.value })}
              className="ml-2 rounded border border-gray-300 px-2 py-1 text-sm">
              <option value="">{t('secplane.inputDetection.alerts.allSources')}</option>
              <option value="aegis">aegis</option>
              <option value="platform">platform</option>
              <option value="gateway">gateway</option>
              <option value="secureclaw">secureclaw</option>
              <option value="ksecure">ksecure</option>
              <option value="kubearmor">kubearmor</option>
            </select>
          </label>
          <label className="text-sm text-gray-700">{t('secplane.inputDetection.alerts.ruleId')}
            <input type="text" value={alertFilter.ruleID}
              onChange={(e) => setAlertFilter({ ...alertFilter, ruleID: e.target.value })}
              placeholder="user_risk_scan / tool_result_scan / ..."
              className="ml-2 w-64 rounded border border-gray-300 px-2 py-1 text-sm" />
          </label>
          <button onClick={loadAlerts}
            className="ml-auto rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50">
            {t('secplane.inputDetection.alerts.refresh')}
          </button>
        </div>
      </div>
      {alertsError && <div className="rounded border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{alertsError}</div>}
      <div className="overflow-hidden rounded-xl border border-gray-200 bg-white">
        <table className="min-w-full divide-y divide-gray-200 text-sm">
          <thead className="bg-gray-50 text-left text-xs uppercase text-gray-500">
            <tr>
              <th className="px-4 py-3">{t('secplane.inputDetection.alerts.time')}</th>
              <th className="px-4 py-3">{t('secplane.inputDetection.alerts.source')}</th>
              <th className="px-4 py-3">{t('secplane.inputDetection.alerts.rule')}</th>
              <th className="px-4 py-3">{t('secplane.inputDetection.alerts.severityCol')}</th>
              <th className="px-4 py-3">{t('secplane.inputDetection.alerts.action')}</th>
              <th className="px-4 py-3">{t('secplane.inputDetection.alerts.evidence')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {alerts.map((alert) => (
              <tr key={alert.id} className="hover:bg-gray-50">
                <td className="px-4 py-3 text-xs text-gray-500">{new Date(alert.ts).toLocaleString()}</td>
                <td className="px-4 py-3"><span className="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-700">{alert.source}</span></td>
                <td className="px-4 py-3">
                  <div className="font-medium text-gray-900">{alert.rule_name ?? alert.rule_id ?? '-'}</div>
                  {alert.rule_id && alert.rule_name && <div className="text-xs text-gray-500">{alert.rule_id}</div>}
                </td>
                <td className="px-4 py-3"><span className={`rounded-full border px-2 py-0.5 text-xs ${severityChip(alert.severity)}`}>{alert.severity}</span></td>
                <td className="px-4 py-3"><span className={`rounded-full border px-2 py-0.5 text-xs ${actionPill(alert.action)}`}>{alert.action}</span></td>
                <td className="px-4 py-3 text-xs text-gray-700">
                  <div className="max-w-md truncate" title={alert.evidence ?? ''}>{alert.evidence ?? '-'}</div>
                </td>
              </tr>
            ))}
            {alerts.length === 0 && !alertsLoading && (
              <tr><td colSpan={6} className="px-4 py-8 text-center text-sm text-gray-500">{t('secplane.inputDetection.alerts.noAlerts')}</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );

  return (
    <AdminLayout title={t('secplane.inputDetection.title')}>
      <div className="space-y-4">
        <div className="rounded-xl border border-gray-200 bg-white p-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="text-xs font-medium uppercase tracking-wide text-indigo-600">{t('secplane.inputDetection.eyebrow')}</div>
              <div className="mt-1 text-2xl font-semibold text-gray-900">{t('secplane.inputDetection.heading')}</div>
              <div className="mt-1 text-sm text-gray-600">
                {t('secplane.inputDetection.description', { flagCount: USER_RISK_FLAGS.length + TOOL_RESULT_FLAGS.length })}
              </div>
            </div>
            <button onClick={() => setPickerOpen(true)} disabled={dispatching}
              className="shrink-0 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white shadow hover:bg-indigo-700 disabled:opacity-60"
              title={t('secplane.inputDetection.dispatchHint')}>
              {dispatching ? t('secplane.inputDetection.dispatching') : t('secplane.inputDetection.dispatchButton')}
            </button>
          </div>
          {dispatchError && (
            <div className="mt-3 rounded border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{dispatchError}</div>
          )}
          {dispatchResult && (
            <div className="mt-3 rounded-lg border border-emerald-200 bg-emerald-50 p-3 text-xs">
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                <span className="font-medium text-emerald-800">{t('secplane.inputDetection.dispatchComplete')}</span>
                <span className="text-gray-600">{t('secplane.inputDetection.dispatchResult.revision')} <code>{dispatchResult.revision}</code></span>
                <span className="text-gray-600">{t('secplane.inputDetection.dispatchResult.sha')} <code>{dispatchResult.sha256.slice(0, 12)}…</code></span>
                {dispatchResult.skill_id !== undefined && (
                  <span className="text-gray-600">skill_id={dispatchResult.skill_id} v{dispatchResult.version_no}</span>
                )}
              </div>
              <div className="mt-2 grid gap-1">
                {dispatchResult.targets.map((tgt) => (
                  <div key={tgt.instance_id} className="flex items-center gap-2">
                    <span className="text-gray-500">{t('secplane.inputDetection.dispatchResult.instance', { id: tgt.instance_id })}</span>
                    <span className="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-700">{tgt.command_type}</span>
                    <span className={`rounded-full px-2 py-0.5 ${tgt.status === 'succeeded' ? 'bg-emerald-100 text-emerald-700' : tgt.status === 'failed' ? 'bg-rose-100 text-rose-700' : 'bg-amber-100 text-amber-700'}`}>
                      {tgt.status}
                    </span>
                    {tgt.command_id && <span className="text-gray-500">cmd #{tgt.command_id}</span>}
                    {tgt.error && <span className="text-rose-700">{tgt.error}</span>}
                  </div>
                ))}
              </div>
              <div className="mt-2 text-gray-500">
                {t('secplane.inputDetection.dispatchResult.autoReload')}
              </div>
            </div>
          )}
        </div>

        {rulesError && <div className="rounded border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{rulesError}</div>}

        <div className="flex flex-wrap gap-1 border-b border-gray-200">
          {TAB_KEYS.map((key) => (
            <button key={key} onClick={() => setTab(key)}
              className={`px-4 py-2 text-sm transition ${
                tab === key ? 'border-b-2 border-indigo-500 text-indigo-600 font-medium' : 'text-gray-600 hover:text-gray-900'
              }`}
              title={t(`secplane.inputDetection.tabHelp.${key}`)}>
              {t(`secplane.inputDetection.tab.${key}`)}
            </button>
          ))}
          {rulesLoading && <span className="ml-auto self-center text-xs text-gray-500">{t('secplane.inputDetection.loading')}</span>}
        </div>

        <div>
          {tab === 'defenses' && renderDefenses()}
          {tab === 'userRisk' && renderFlagTab('user_risk_flag', USER_RISK_FLAGS, userRiskRules)}
          {tab === 'toolResult' && renderFlagTab('tool_result_flag', TOOL_RESULT_FLAGS, toolResultRules)}
          {tab === 'protected' && renderProtected()}
          {tab === 'alerts' && renderAlerts()}
        </div>
      </div>

      <DispatchPickerModal
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        onDispatch={runDispatch}
        dispatching={dispatching}
        hint={t('secplane.inputDetection.dispatchHint')}
      />
    </AdminLayout>
  );
};

export default InputDetectionPage;
