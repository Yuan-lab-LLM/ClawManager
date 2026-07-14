import React, { useEffect, useMemo, useState } from 'react';
import AdminLayout from '../../../components/AdminLayout';
import DispatchPickerModal from '../../../components/secplane/DispatchPickerModal';
import {
  secplaneService,
  type SecplaneRule,
  type SecplaneAlert,
  type AlertSource,
  type Severity,
  type DispatchResult,
} from '../../../services/secplaneService';
import { useI18n } from '../../../contexts/I18nContext';

type AuditTriState = 'enforce' | 'observe' | 'off';

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
    observed: 'bg-sky-100 text-sky-700 border-sky-200',
    allowed: 'bg-emerald-100 text-emerald-700 border-emerald-200',
  };
  return lookup[action] || 'bg-gray-100 text-gray-700 border-gray-200';
};

const InlineText: React.FC<{
  value: string; className?: string; onSave: (next: string) => void;
  t: (key: string, vars?: Record<string, string | number>) => string;
}> = ({ value, className = '', onSave, t }) => {
  const [editing, setEditing] = useState(false);
  if (!editing) {
    return (
      <code onClick={() => setEditing(true)}
        className={`cursor-pointer rounded px-1 font-mono text-xs text-gray-700 hover:bg-yellow-50 ${className}`}
        title={t('secplane.secureClaw.inline.clickToEdit')}>
        {value || <span className="text-gray-400 italic">{t('secplane.secureClaw.inline.emptyClickToAdd')}</span>}
      </code>
    );
  }
  return (
    <input autoFocus defaultValue={value}
      className={`rounded border border-indigo-300 px-1 font-mono text-xs text-gray-900 ${className}`}
      onBlur={(e) => { const next = e.target.value; setEditing(false); if (next !== value) onSave(next); }}
      onKeyDown={(e) => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') { setEditing(false); e.preventDefault(); } }} />
  );
};

const AddInput: React.FC<{
  placeholder: string; onAdd: (value: string) => void | Promise<void>;
  t: (key: string, vars?: Record<string, string | number>) => string;
}> = ({ placeholder, onAdd, t }) => {
  const [val, setVal] = useState('');
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    const v = val.trim(); if (!v) return;
    setBusy(true);
    try { await onAdd(v); setVal(''); } finally { setBusy(false); }
  };
  return (
    <div className="flex gap-2 border-t border-gray-100 bg-gray-50/50 px-4 py-2">
      <input type="text" value={val} onChange={(e) => setVal(e.target.value)}
        placeholder={placeholder} className="flex-1 rounded border border-gray-300 px-2 py-1 text-xs"
        onKeyDown={(e) => { if (e.key === 'Enter') submit(); }} disabled={busy} />
      <button onClick={submit} disabled={!val.trim() || busy}
        className="rounded bg-indigo-600 px-3 py-1 text-xs text-white hover:bg-indigo-700 disabled:opacity-60">
        {busy ? '…' : t('secplane.secureClaw.addInput.add')}
      </button>
    </div>
  );
};

const SecureClawPage: React.FC = () => {
  const { t } = useI18n();

  const FAILURE_MODES = [
    { value: 'block_all', label: t('secplane.secureClaw.failureMode.blockAll.label'), help: t('secplane.secureClaw.failureMode.blockAll.help') },
    { value: 'safe_mode', label: t('secplane.secureClaw.failureMode.safeMode.label'), help: t('secplane.secureClaw.failureMode.safeMode.help') },
    { value: 'read_only', label: t('secplane.secureClaw.failureMode.readOnly.label'), help: t('secplane.secureClaw.failureMode.readOnly.help') },
  ] as const;

  const RISK_PROFILES = [
    { value: 'strict', label: t('secplane.secureClaw.riskProfile.strict.label'), help: t('secplane.secureClaw.riskProfile.strict.help') },
    { value: 'standard', label: t('secplane.secureClaw.riskProfile.standard.label'), help: t('secplane.secureClaw.riskProfile.standard.help') },
    { value: 'permissive', label: t('secplane.secureClaw.riskProfile.permissive.label'), help: t('secplane.secureClaw.riskProfile.permissive.help') },
  ] as const;

  const CATEGORY_LABEL = useMemo(() => {
    const keys = ['access-control', 'control-tokens', 'cost', 'credentials', 'cross-layer', 'degradation', 'execution', 'gateway', 'ioc', 'kill-switch', 'memory', 'memory-trust', 'supply-chain'];
    const out: Record<string, string> = {};
    for (const k of keys) out[k] = t(`secplane.secureClaw.categoryLabel.${k}`);
    return out;
  }, [t]);

  const [rules, setRules] = useState<SecplaneRule[]>([]);
  const [auditRules, setAuditRules] = useState<SecplaneRule[]>([]);
  const [hardeningRules, setHardeningRules] = useState<SecplaneRule[]>([]);
  const [dangerousCatRules, setDangerousCatRules] = useState<SecplaneRule[]>([]);
  const [dangerousPatRules, setDangerousPatRules] = useState<SecplaneRule[]>([]);
  const [injectionPatRules, setInjectionPatRules] = useState<SecplaneRule[]>([]);
  const [privacyRules, setPrivacyRules] = useState<SecplaneRule[]>([]);
  const [iocRules, setIocRules] = useState<SecplaneRule[]>([]);
  const [rulesLoading, setRulesLoading] = useState(true);
  const [rulesError, setRulesError] = useState<string | null>(null);
  const [savingRuleID, setSavingRuleID] = useState<string | null>(null);
  const [alerts, setAlerts] = useState<SecplaneAlert[]>([]);
  const [alertsLoading, setAlertsLoading] = useState(false);
  const [alertsError, setAlertsError] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [dispatching, setDispatching] = useState(false);
  const [dispatchResult, setDispatchResult] = useState<DispatchResult | null>(null);
  const [dispatchError, setDispatchError] = useState<string | null>(null);

  const loadRules = async () => {
    setRulesLoading(true); setRulesError(null);
    try {
      const [cfg, audit, harden, dCat, dPat, iPat, pri, ioc] = await Promise.all([
        secplaneService.listRules('secureclaw_config'), secplaneService.listRules('secureclaw_audit_check'),
        secplaneService.listRules('secureclaw_hardening'), secplaneService.listRules('secureclaw_dangerous_cat'),
        secplaneService.listRules('secureclaw_dangerous_pat'), secplaneService.listRules('secureclaw_injection_pat'),
        secplaneService.listRules('secureclaw_privacy_rule'), secplaneService.listRules('secureclaw_ioc'),
      ]);
      setRules(cfg); setAuditRules(audit); setHardeningRules(harden);
      setDangerousCatRules(dCat); setDangerousPatRules(dPat); setInjectionPatRules(iPat);
      setPrivacyRules(pri); setIocRules(ioc);
    } catch (e: any) { setRulesError(e?.response?.data?.error ?? e?.message ?? 'failed to load rules'); }
    finally { setRulesLoading(false); }
  };

  const loadAlerts = async () => {
    setAlertsLoading(true); setAlertsError(null);
    try { setAlerts(await secplaneService.listAlerts({ source: 'secureclaw' as AlertSource, limit: 100 })); }
    catch (e: any) { setAlertsError(e?.response?.data?.error ?? e?.message ?? 'failed to load alerts'); }
    finally { setAlertsLoading(false); }
  };

  useEffect(() => { loadRules(); loadAlerts(); }, []);

  const rulesByID = useMemo(() => { const m = new Map<string, SecplaneRule>(); for (const r of rules) m.set(r.rule_id, r); return m; }, [rules]);

  const persistRule = async (next: SecplaneRule) => {
    setSavingRuleID(next.rule_id); setRulesError(null);
    try { await secplaneService.saveRule(next); await loadRules(); }
    catch (e: any) { setRulesError(e?.response?.data?.error ?? e?.message ?? 'failed to save'); }
    finally { setSavingRuleID(null); }
  };

  const hardDeleteRule = async (rule_id: string) => {
    setSavingRuleID(rule_id);
    try { await secplaneService.disableRule(rule_id); await loadRules(); }
    catch (e: any) { setRulesError(e?.response?.data?.error ?? e?.message ?? 'failed to delete'); }
    finally { setSavingRuleID(null); }
  };

  const runDispatch = async (instanceIDs: number[] | null) => {
    setDispatching(true); setDispatchError(null); setDispatchResult(null);
    try { setDispatchResult(await secplaneService.dispatchSecureClaw(instanceIDs ?? undefined)); setPickerOpen(false); }
    catch (e: any) { setDispatchError(e?.response?.data?.error ?? e?.message ?? 'dispatch failed'); }
    finally { setDispatching(false); }
  };

  const renderToggleRow = (ruleID: string, label: string, help?: React.ReactNode) => {
    const rule = rulesByID.get(ruleID); if (!rule) return null;
    const isSaving = savingRuleID === ruleID;
    return (
      <div className="flex items-start gap-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="font-medium text-gray-900">{label}</div>
          {help && <div className="mt-0.5 text-xs text-gray-500">{help}</div>}
          <div className="mt-0.5 font-mono text-[10px] text-gray-400">{rule.rule_id}</div>
        </div>
        <button disabled={isSaving} onClick={() => persistRule({ ...rule, is_enabled: !rule.is_enabled })}
          className={`shrink-0 rounded-full border px-3 py-1 text-xs ${rule.is_enabled ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-gray-100 text-gray-500 border-gray-200'} disabled:opacity-60`}>
          {isSaving ? '…' : rule.is_enabled ? t('secplane.secureClaw.toggle.on') : t('secplane.secureClaw.toggle.off')}
        </button>
      </div>
    );
  };

  function renderRadioRow(ruleID: string, label: string, options: ReadonlyArray<{ value: string; label: string; help: string }>) {
    const rule = rulesByID.get(ruleID); if (!rule) return null;
    const isSaving = savingRuleID === ruleID;
    const current = rule.pattern || options[0].value;
    const groupName = `radio-${ruleID}`;
    return (
      <div className="py-3">
        <div className="mb-2 flex items-center justify-between">
          <div>
            <div className="font-medium text-gray-900">{label}</div>
            <div className="font-mono text-[10px] text-gray-400">{rule.rule_id}</div>
          </div>
          <div className="text-xs text-gray-500">{t('secplane.secureClaw.audit.current')} <code className="font-semibold text-indigo-700">{current}</code></div>
        </div>
        <div className="grid gap-2 md:grid-cols-3">
          {options.map((opt) => {
            const active = current === opt.value;
            return (
              <label key={opt.value} className={`flex cursor-pointer items-start gap-2 rounded-lg border px-3 py-2 text-sm transition ${active ? 'border-indigo-400 bg-indigo-50 ring-1 ring-indigo-300' : 'border-gray-200 bg-white hover:bg-gray-50'} ${isSaving ? 'pointer-events-none opacity-60' : ''}`}>
                <input type="radio" name={groupName} value={opt.value} checked={active} disabled={isSaving}
                  onChange={() => persistRule({ ...rule, pattern: opt.value, is_enabled: true })}
                  className="mt-1 h-4 w-4 accent-indigo-600" />
                <div className="min-w-0 flex-1">
                  <div className={`font-medium ${active ? 'text-indigo-700' : 'text-gray-900'}`}>{opt.label}</div>
                  <div className="mt-0.5 text-xs text-gray-500">{opt.help}</div>
                </div>
              </label>
            );
          })}
        </div>
      </div>
    );
  }

  const renderNumberRow = (ruleID: string, label: string, placeholder: string, opts: { step?: string; unit?: string; min?: string } = {}) => {
    const rule = rulesByID.get(ruleID); if (!rule) return null;
    const isSaving = savingRuleID === ruleID;
    return (
      <div className="flex items-center gap-3 py-2">
        <input type="checkbox" checked={rule.is_enabled} disabled={isSaving}
          onChange={() => persistRule({ ...rule, is_enabled: !rule.is_enabled })}
          className="h-4 w-4 rounded border-gray-300" />
        <div className="min-w-0 flex-1 text-sm text-gray-700">{label}</div>
        <input type="number" value={rule.pattern} step={opts.step ?? '0.01'} min={opts.min ?? '0'}
          disabled={isSaving || !rule.is_enabled}
          onBlur={(e) => { if (e.target.value !== rule.pattern) persistRule({ ...rule, pattern: e.target.value }); }}
          onChange={(e) => setRules((prev) => prev.map((r) => (r.rule_id === rule.rule_id ? { ...r, pattern: e.target.value } : r)))}
          placeholder={placeholder} className="w-28 rounded border border-gray-300 px-2 py-1 text-right text-sm disabled:bg-gray-100" />
        <span className="text-xs text-gray-500">{opts.unit ?? t('secplane.secureClaw.numberRow.usd')}</span>
      </div>
    );
  };

  const triStateOf = (r: SecplaneRule): AuditTriState => {
    if (!r.is_enabled || r.mode === 'off') return 'off';
    if (r.mode === 'observe') return 'observe';
    return 'enforce';
  };

  const persistAuditTri = (r: SecplaneRule, next: AuditTriState) => {
    if (next === 'off') return persistRule({ ...r, is_enabled: false });
    return persistRule({ ...r, is_enabled: true, mode: next });
  };

  const auditByCategory = useMemo(() => {
    const groups: Record<string, SecplaneRule[]> = {};
    for (const r of auditRules) { const cat = (r.pattern || 'general').trim(); (groups[cat] = groups[cat] ?? []).push(r); }
    return Object.entries(groups).sort(([a], [b]) => a.localeCompare(b));
  }, [auditRules]);

  const SEG_TONE: Record<AuditTriState, string> = { off: 'bg-gray-700 text-white', observe: 'bg-amber-500 text-white', enforce: 'bg-emerald-600 text-white' };

  const renderAuditRow = (r: SecplaneRule) => {
    const state = triStateOf(r); const isSaving = savingRuleID === r.rule_id;
    return (
      <tr key={r.rule_id} className={`hover:bg-gray-50 ${state === 'off' ? 'bg-gray-50/50' : ''}`}>
        <td className="px-3 py-2 align-top">
          <div className="flex items-center gap-2">
            <code className="font-mono text-xs text-gray-700">{r.display_name}</code>
            <span className={`rounded-full border px-1.5 py-0.5 text-[10px] ${severityChip(r.severity)}`}>{r.severity}</span>
          </div>
          <div className="mt-0.5 max-w-xl truncate text-xs text-gray-500" title={r.description ?? ''}>{r.description ?? '-'}</div>
        </td>
        <td className="px-3 py-2 text-right align-top">
          <div className="inline-flex divide-x divide-gray-300 overflow-hidden rounded-md border border-gray-300">
            {(['off', 'observe', 'enforce'] as AuditTriState[]).map((s) => (
              <button key={s} disabled={isSaving} onClick={() => persistAuditTri(r, s)}
                className={`px-2.5 py-1 text-[10px] transition first:rounded-l-md last:rounded-r-md ${state === s ? SEG_TONE[s] : 'bg-white text-gray-600 hover:bg-gray-50'} disabled:opacity-60 disabled:cursor-not-allowed`}>
                {s === 'off' ? 'Off' : s === 'observe' ? 'Observe' : 'Enforce'}
              </button>
            ))}
          </div>
        </td>
      </tr>
    );
  };

  const bulkSetCategory = async (cat: string, next: AuditTriState) => {
    for (const r of auditByCategory.find(([c]) => c === cat)?.[1] ?? []) await persistAuditTri(r, next);
  };

  const autoHarden = rulesByID.get('sc.autoHarden');

  return (
    <AdminLayout title={t('secplane.secureClaw.title')}>
      <div className="space-y-4">
        <div className="rounded-xl border border-gray-200 bg-white p-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="text-xs font-medium uppercase tracking-wide text-indigo-600">{t('secplane.secureClaw.eyebrow')}</div>
              <div className="mt-1 text-2xl font-semibold text-gray-900">{t('secplane.secureClaw.heading')}</div>
              <div className="mt-1 text-sm text-gray-600">{t('secplane.secureClaw.description')}</div>
            </div>
            <button onClick={() => setPickerOpen(true)} disabled={dispatching}
              className="shrink-0 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white shadow hover:bg-indigo-700 disabled:opacity-60"
              title={t('secplane.secureClaw.dispatchHint')}>
              {dispatching ? t('secplane.secureClaw.dispatching') : t('secplane.secureClaw.dispatchButton')}
            </button>
          </div>
          {dispatchError && <div className="mt-3 rounded border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{dispatchError}</div>}
          {dispatchResult && (
            <div className="mt-3 rounded-lg border border-emerald-200 bg-emerald-50 p-3 text-xs">
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                <span className="font-medium text-emerald-800">{t('secplane.secureClaw.dispatchComplete')}</span>
                <span className="text-gray-600">revision: <code>{dispatchResult.revision}</code></span>
                <span className="text-gray-600">sha: <code>{dispatchResult.sha256.slice(0, 12)}…</code></span>
                {dispatchResult.skill_id !== undefined && <span className="text-gray-600">skill_id={dispatchResult.skill_id} v{dispatchResult.version_no}</span>}
              </div>
              <div className="mt-2 grid gap-1">
                {dispatchResult.targets.map((tgt) => (
                  <div key={tgt.instance_id} className="flex items-center gap-2">
                    <span className="text-gray-500">#{tgt.instance_id}</span>
                    <span className="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-700">{tgt.command_type}</span>
                    <span className={`rounded-full px-2 py-0.5 ${tgt.status === 'succeeded' ? 'bg-emerald-100 text-emerald-700' : tgt.status === 'failed' ? 'bg-rose-100 text-rose-700' : 'bg-amber-100 text-amber-700'}`}>{tgt.status}</span>
                    {tgt.command_id && <span className="text-gray-500">cmd #{tgt.command_id}</span>}
                    {tgt.error && <span className="text-rose-700">{tgt.error}</span>}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {rulesError && <div className="rounded border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{rulesError}</div>}
        {rulesLoading && <div className="rounded border border-gray-200 bg-gray-50 p-3 text-sm text-gray-600">{t('secplane.secureClaw.loading')}</div>}

        {/* Section 1: Runtime Policy */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.policy.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.policy.subtitle')}</div>
          </div>
          {autoHarden && renderToggleRow('sc.autoHarden', t('secplane.secureClaw.policy.autoHarden'),
            <><span className="text-rose-600 font-medium">{t('secplane.secureClaw.policy.autoHardenWarn')}</span><span>{t('secplane.secureClaw.policy.autoHardenNote')}</span></>)}
          {renderRadioRow('sc.failureMode', t('secplane.secureClaw.failureMode.label'), FAILURE_MODES)}
          {renderRadioRow('sc.riskProfile', t('secplane.secureClaw.riskProfile.label'), RISK_PROFILES)}
        </section>

        {/* Section 2: Cost Circuit Breaker */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.costCircuit.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.costCircuit.subtitle')}</div>
          </div>
          {renderToggleRow('sc.cost.circuitBreakerEnabled', t('secplane.secureClaw.costCircuit.enabled'), t('secplane.secureClaw.costCircuit.enabledHelp'))}
          <div className="mt-2 space-y-1">
            {renderNumberRow('sc.cost.hourlyLimitUsd', t('secplane.secureClaw.costCircuit.hourlyLimit'), '10.0')}
            {renderNumberRow('sc.cost.dailyLimitUsd', t('secplane.secureClaw.costCircuit.dailyLimit'), '100.0')}
            {renderNumberRow('sc.cost.monthlyLimitUsd', t('secplane.secureClaw.costCircuit.monthlyLimit'), '2000.0')}
          </div>
        </section>

        {/* Section 3: Monitors */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.monitors.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.monitors.subtitle')}</div>
          </div>
          {renderToggleRow('sc.monitors.credentials', t('secplane.secureClaw.monitors.credentials'), t('secplane.secureClaw.monitors.credentialsHelp'))}
          {renderToggleRow('sc.monitors.memory', t('secplane.secureClaw.monitors.memory'), t('secplane.secureClaw.monitors.memoryHelp'))}
          {renderToggleRow('sc.monitors.skills', t('secplane.secureClaw.monitors.skills'), t('secplane.secureClaw.monitors.skillsHelp'))}
          {renderToggleRow('sc.monitors.cost', t('secplane.secureClaw.monitors.cost'), t('secplane.secureClaw.monitors.costHelp'))}
        </section>

        {/* Section 4: Memory Review */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.memoryReview.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.memoryReview.subtitle')}</div>
          </div>
          {renderToggleRow('sc.memory.integrityChecks', t('secplane.secureClaw.memoryReview.integrityChecks'), t('secplane.secureClaw.memoryReview.integrityChecksHelp'))}
          {renderToggleRow('sc.memory.promptInjectionScan', t('secplane.secureClaw.memoryReview.promptInjectionScan'), t('secplane.secureClaw.memoryReview.promptInjectionScanHelp'))}
          {renderToggleRow('sc.memory.quarantineEnabled', t('secplane.secureClaw.memoryReview.quarantineEnabled'), t('secplane.secureClaw.memoryReview.quarantineEnabledHelp'))}
          {renderToggleRow('sc.memory.trustLevels', t('secplane.secureClaw.memoryReview.trustLevels'), t('secplane.secureClaw.memoryReview.trustLevelsHelp'))}
        </section>

        {/* Section 5: Skill Audit */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.skillAudit.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.skillAudit.subtitle')}</div>
          </div>
          {renderToggleRow('sc.skills.blockUnaudited', t('secplane.secureClaw.skillAudit.blockUnaudited'), t('secplane.secureClaw.skillAudit.blockUnauditedHelp'))}
          {renderToggleRow('sc.skills.scanOnInstall', t('secplane.secureClaw.skillAudit.scanOnInstall'), t('secplane.secureClaw.skillAudit.scanOnInstallHelp'))}
          {renderToggleRow('sc.skills.iocCheckEnabled', t('secplane.secureClaw.skillAudit.iocCheckEnabled'), t('secplane.secureClaw.skillAudit.iocCheckEnabledHelp'))}
        </section>

        {/* Section 6: Egress Control */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.egressControl.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.egressControl.subtitle')}</div>
          </div>
          {renderToggleRow('sc.network.egressAllowlistEnabled', t('secplane.secureClaw.egressControl.allowlistEnabled'),
            <>{t('secplane.secureClaw.egressControl.allowlistEnabledHelp')}<span className="ml-1 text-amber-700">{t('secplane.secureClaw.egressControl.allowlistNote')}</span></>)}
        </section>

        {/* Section 7: Behavioral Baseline */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.behavioral.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.behavioral.subtitle')}</div>
          </div>
          {renderToggleRow('sc.behavioral.baselineEnabled', t('secplane.secureClaw.behavioral.baselineEnabled'), t('secplane.secureClaw.behavioral.baselineEnabledHelp'))}
          <div className="mt-2 space-y-1">
            {renderNumberRow('sc.behavioral.deviationThreshold', t('secplane.secureClaw.behavioral.deviationThreshold'), '0.5', { step: '0.01', unit: '0-1', min: '0' })}
            {renderNumberRow('sc.behavioral.windowMinutes', t('secplane.secureClaw.behavioral.windowMinutes'), '60', { step: '1', unit: t('secplane.secureClaw.behavioral.minutes'), min: '1' })}
          </div>
        </section>

        {/* Section 8: Audit Checks */}
        <section className="rounded-xl border border-gray-200 bg-white">
          <div className="border-b border-gray-100 px-4 py-3">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.audit.title')}</div>
                <div className="text-xs text-gray-500">{t('secplane.secureClaw.audit.subtitle')}</div>
              </div>
              <div className="text-xs text-gray-500">
                {t('secplane.secureClaw.audit.total')} {auditRules.length}；
                {t('secplane.secureClaw.audit.enforceCount')} {auditRules.filter((r) => triStateOf(r) === 'enforce').length}；
                {t('secplane.secureClaw.audit.observeCount')} {auditRules.filter((r) => triStateOf(r) === 'observe').length}；
                {t('secplane.secureClaw.audit.offCount')} {auditRules.filter((r) => triStateOf(r) === 'off').length}
              </div>
            </div>
          </div>
          <div className="divide-y divide-gray-100">
            {auditByCategory.map(([cat, items]) => (
              <details key={cat} open className="group">
                <summary className="flex cursor-pointer items-center justify-between bg-gray-50 px-4 py-2 text-sm hover:bg-gray-100">
                  <span className="font-medium text-gray-800">{CATEGORY_LABEL[cat] ?? cat}<span className="ml-2 text-xs text-gray-500">({items.length})</span></span>
                  <span className="flex items-center gap-1 text-[10px]" onClick={(e) => e.stopPropagation()}>
                    <button onClick={() => bulkSetCategory(cat, 'enforce')} className="rounded border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-emerald-700 hover:bg-emerald-100">{t('secplane.secureClaw.audit.bulkEnforce')}</button>
                    <button onClick={() => bulkSetCategory(cat, 'observe')} className="rounded border border-amber-200 bg-amber-50 px-2 py-0.5 text-amber-700 hover:bg-amber-100">{t('secplane.secureClaw.audit.bulkObserve')}</button>
                    <button onClick={() => bulkSetCategory(cat, 'off')} className="rounded border border-gray-300 bg-gray-100 px-2 py-0.5 text-gray-700 hover:bg-gray-200">{t('secplane.secureClaw.audit.bulkOff')}</button>
                  </span>
                </summary>
                <table className="min-w-full text-sm"><tbody className="divide-y divide-gray-100">{items.map(renderAuditRow)}</tbody></table>
              </details>
            ))}
            {auditRules.length === 0 && !rulesLoading && <div className="p-6 text-center text-sm text-gray-500">{t('secplane.secureClaw.audit.empty')}</div>}
          </div>
        </section>

        {/* Section 9: Hardening Modules */}
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <div className="mb-3 border-b border-gray-100 pb-2">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.hardening.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.hardening.subtitle')}<span className="ml-1 text-rose-600">{t('secplane.secureClaw.hardening.warn')}</span></div>
          </div>
          {hardeningRules.length === 0 && !rulesLoading && <div className="text-sm text-gray-500">{t('secplane.secureClaw.hardening.empty')}</div>}
          {hardeningRules.map((r) => (
            <div key={r.rule_id} className="flex items-start gap-4 py-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <code className="font-mono text-xs text-gray-900">{r.display_name}</code>
                  {!r.is_enabled && <span className="rounded-full border border-gray-200 bg-gray-100 px-2 py-0.5 text-[10px] text-gray-600">disabled</span>}
                </div>
                {r.description && <div className="mt-0.5 text-xs text-gray-500">{r.description}</div>}
                <div className="mt-0.5 font-mono text-[10px] text-gray-400">{r.rule_id}</div>
              </div>
              <button disabled={savingRuleID === r.rule_id} onClick={() => persistRule({ ...r, is_enabled: !r.is_enabled })}
                className={`shrink-0 rounded-full border px-3 py-1 text-xs ${r.is_enabled ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-gray-100 text-gray-500 border-gray-200'} disabled:opacity-60`}>
                {savingRuleID === r.rule_id ? '…' : r.is_enabled ? t('secplane.secureClaw.hardening.allowHarden') : t('secplane.secureClaw.hardening.noHarden')}
              </button>
            </div>
          ))}
        </section>

        {/* Section 10: Dangerous Commands */}
        <section className="rounded-xl border border-gray-200 bg-white">
          <div className="flex items-center justify-between border-b border-gray-100 px-4 py-3">
            <div>
              <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.dangerousCommands.title')}</div>
              <div className="text-xs text-gray-500">{t('secplane.secureClaw.dangerousCommands.subtitle')}</div>
            </div>
            <button onClick={async () => {
              const name = window.prompt(t('secplane.secureClaw.dangerousCommands.newCategoryPrompt')); if (!name) return;
              const key = name.trim().replace(/\W+/g, '_').toLowerCase(); if (!key) return;
              await persistRule({ rule_id: `sc.dc.cat.${key}`, kind: 'secureclaw_dangerous_cat', display_name: key, pattern: 'block', target: 'user_input', severity: 'high', action: 'block', mode: 'enforce', is_enabled: true, sort_order: 4999 });
            }} className="rounded border border-indigo-300 bg-white px-3 py-1.5 text-xs text-indigo-700 hover:bg-indigo-50">
              {t('secplane.secureClaw.dangerousCommands.newCategory')}
            </button>
          </div>
          {dangerousCatRules.map((cat) => {
            const catKey = cat.rule_id.replace(/^sc\.dc\.cat\./, '');
            const patterns = dangerousPatRules.filter((p) => p.tags === catKey);
            const isSaving = savingRuleID === cat.rule_id;
            return (
              <details key={cat.rule_id} className="group border-t border-gray-100 first:border-t-0" open>
                <summary className="flex cursor-pointer items-center justify-between gap-3 bg-gray-50 px-4 py-2 hover:bg-gray-100">
                  <div className="flex min-w-0 flex-1 items-center gap-2">
                    <span className="font-medium text-gray-900">{catKey}</span>
                    <span className="text-xs text-gray-500">{patterns.length} {t('secplane.secureClaw.dangerousCommands.patterns')}</span>
                    {!cat.is_enabled && <span className="rounded-full border border-gray-200 bg-gray-100 px-2 py-0.5 text-[10px] text-gray-600">{t('secplane.secureClaw.dangerousCommands.disabled')}</span>}
                  </div>
                  <div className="flex items-center gap-2 text-xs" onClick={(e) => e.stopPropagation()}>
                    <select value={cat.severity} disabled={isSaving} onChange={(e) => persistRule({ ...cat, severity: e.target.value as any })} className="rounded border border-gray-300 px-2 py-0.5">
                      <option value="critical">critical</option><option value="high">high</option><option value="medium">medium</option><option value="low">low</option>
                    </select>
                    <select value={cat.pattern} disabled={isSaving} onChange={(e) => persistRule({ ...cat, pattern: e.target.value })} className="rounded border border-gray-300 px-2 py-0.5">
                      <option value="block">block</option><option value="require_approval">require_approval</option><option value="warn">warn</option>
                    </select>
                    <button disabled={isSaving} onClick={() => persistRule({ ...cat, is_enabled: !cat.is_enabled })}
                      className={`rounded-full border px-2 py-0.5 ${cat.is_enabled ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-gray-100 text-gray-500 border-gray-200'}`}>
                      {cat.is_enabled ? t('secplane.secureClaw.dangerousCommands.enable') : t('secplane.secureClaw.dangerousCommands.disable')}
                    </button>
                    <button disabled={isSaving || patterns.length > 0} onClick={() => hardDeleteRule(cat.rule_id)}
                      title={patterns.length > 0 ? t('secplane.secureClaw.dangerousCommands.removePatternsFirst') : t('secplane.secureClaw.dangerousCommands.deleteCategory')}
                      className="rounded border border-rose-200 bg-white px-2 py-0.5 text-rose-700 hover:bg-rose-50 disabled:opacity-60">
                      {t('secplane.secureClaw.dangerousCommands.delete')}
                    </button>
                  </div>
                </summary>
                <ul className="divide-y divide-gray-100">
                  {patterns.map((p) => (
                    <li key={p.rule_id} className="flex items-center gap-2 px-5 py-1.5">
                      <input type="checkbox" checked={p.is_enabled} disabled={savingRuleID === p.rule_id} onChange={() => persistRule({ ...p, is_enabled: !p.is_enabled })} className="h-4 w-4 rounded border-gray-300" />
                      <div className="flex-1 min-w-0"><InlineText value={p.pattern} onSave={(v) => persistRule({ ...p, pattern: v, display_name: v })} t={t} /></div>
                      <button onClick={() => hardDeleteRule(p.rule_id)} className="text-xs text-rose-600 hover:text-rose-800">{t('secplane.secureClaw.dangerousCommands.delete')}</button>
                    </li>
                  ))}
                  {patterns.length === 0 && <li className="px-5 py-2 text-xs text-gray-400 italic">{t('secplane.secureClaw.dangerousCommands.emptyPatterns')}</li>}
                </ul>
                <AddInput placeholder={t('secplane.secureClaw.dangerousCommands.addPlaceholder', { cat: catKey })} t={t}
                  onAdd={async (v) => {
                    const slug = Date.now().toString(36) + Math.random().toString(36).slice(2, 5);
                    await persistRule({ rule_id: `sc.dc.pat.${catKey}.user-${slug}`, kind: 'secureclaw_dangerous_pat', display_name: v, pattern: v, tags: catKey, target: 'user_input', severity: 'high', action: 'block', mode: 'enforce', is_enabled: true, sort_order: 4999 });
                  }} />
              </details>
            );
          })}
        </section>

        {/* Section 11: Injection Patterns */}
        <section className="rounded-xl border border-gray-200 bg-white">
          <div className="border-b border-gray-100 px-4 py-3">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.injectionPatterns.title')}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.injectionPatterns.subtitle', { count: injectionPatRules.length })}</div>
          </div>
          {Array.from(new Set(injectionPatRules.map((r) => r.tags ?? ''))).sort().map((cat) => {
            const items = injectionPatRules.filter((r) => r.tags === cat);
            return (
              <details key={cat} className="group border-t border-gray-100 first:border-t-0">
                <summary className="cursor-pointer bg-gray-50 px-4 py-2 hover:bg-gray-100 text-sm">
                  <span className="font-medium text-gray-900">{cat}</span><span className="ml-2 text-xs text-gray-500">{items.length}</span>
                </summary>
                <ul className="divide-y divide-gray-100">
                  {items.map((r) => (
                    <li key={r.rule_id} className="flex items-center gap-2 px-5 py-1.5">
                      <input type="checkbox" checked={r.is_enabled} disabled={savingRuleID === r.rule_id} onChange={() => persistRule({ ...r, is_enabled: !r.is_enabled })} className="h-4 w-4 rounded border-gray-300" />
                      <div className="flex-1 min-w-0"><InlineText value={r.pattern} onSave={(v) => persistRule({ ...r, pattern: v, display_name: v })} t={t} /></div>
                      <button onClick={() => hardDeleteRule(r.rule_id)} className="text-xs text-rose-600 hover:text-rose-800">{t('secplane.secureClaw.dangerousCommands.delete')}</button>
                    </li>
                  ))}
                  {items.length === 0 && <li className="px-5 py-2 text-xs text-gray-400 italic">{t('secplane.secureClaw.injectionPatterns.empty')}</li>}
                </ul>
                <AddInput placeholder={t('secplane.secureClaw.injectionPatterns.addPlaceholder', { cat })} t={t}
                  onAdd={async (v) => {
                    const slug = Date.now().toString(36) + Math.random().toString(36).slice(2, 5);
                    await persistRule({ rule_id: `sc.ip.pat.${cat}.user-${slug}`, kind: 'secureclaw_injection_pat', display_name: v, pattern: v, tags: cat, target: 'user_input', severity: 'high', action: 'block', mode: 'enforce', is_enabled: true, sort_order: 5999 });
                  }} />
              </details>
            );
          })}
          <div className="flex items-center justify-end gap-2 border-t border-gray-100 bg-gray-50/50 px-4 py-2">
            <button onClick={async () => {
              const name = window.prompt(t('secplane.secureClaw.injectionPatterns.newCategoryPrompt')); if (!name) return;
              const key = name.trim().replace(/\W+/g, '_').toLowerCase(); if (!key) return;
              const phrase = window.prompt(t('secplane.secureClaw.injectionPatterns.firstPhrasePrompt')) ?? 'placeholder';
              const slug = Date.now().toString(36);
              await persistRule({ rule_id: `sc.ip.pat.${key}.user-${slug}`, kind: 'secureclaw_injection_pat', display_name: phrase, pattern: phrase, tags: key, target: 'user_input', severity: 'high', action: 'block', mode: 'enforce', is_enabled: true, sort_order: 5999 });
            }} className="rounded border border-indigo-300 bg-white px-3 py-1 text-xs text-indigo-700 hover:bg-indigo-50">
              {t('secplane.secureClaw.injectionPatterns.newCategory')}
            </button>
          </div>
        </section>

        {/* Section 12: Privacy Rules */}
        <section className="rounded-xl border border-gray-200 bg-white">
          <div className="flex items-center justify-between border-b border-gray-100 px-4 py-3">
            <div>
              <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.privacyRules.title', { count: privacyRules.length })}</div>
              <div className="text-xs text-gray-500">{t('secplane.secureClaw.privacyRules.subtitle')}</div>
            </div>
            <button onClick={async () => {
              const id = window.prompt(t('secplane.secureClaw.privacyRules.newRulePrompt')); if (!id) return;
              const key = id.trim().replace(/\W+/g, '_').toLowerCase(); if (!key) return;
              const regex = window.prompt(t('secplane.secureClaw.privacyRules.regexPrompt')) ?? '';
              const fix = window.prompt(t('secplane.secureClaw.privacyRules.fixPrompt')) ?? '';
              await persistRule({ rule_id: `sc.pr.${key}`, kind: 'secureclaw_privacy_rule', display_name: key, pattern: regex, description: fix, target: 'user_input', severity: 'medium', action: 'redact', mode: 'enforce', is_enabled: true, sort_order: 7999 });
            }} className="rounded border border-indigo-300 bg-white px-3 py-1.5 text-xs text-indigo-700 hover:bg-indigo-50">
              {t('secplane.secureClaw.privacyRules.newRule')}
            </button>
          </div>
          <table className="min-w-full text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase text-gray-500">
              <tr>
                <th className="px-4 py-2">{t('secplane.secureClaw.privacyRules.idCol')}</th>
                <th className="px-4 py-2">{t('secplane.secureClaw.privacyRules.regexCol')}</th>
                <th className="px-4 py-2">Severity</th>
                <th className="px-4 py-2">{t('secplane.secureClaw.privacyRules.actionCol')}</th>
                <th className="px-4 py-2">{t('secplane.secureClaw.privacyRules.fixCol')}</th>
                <th className="px-4 py-2 text-right">{t('secplane.secureClaw.privacyRules.enableCol')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {privacyRules.map((r) => {
                const isSaving = savingRuleID === r.rule_id;
                return (
                  <tr key={r.rule_id} className={`hover:bg-gray-50 ${!r.is_enabled ? 'bg-gray-50/50' : ''}`}>
                    <td className="px-4 py-2 font-mono text-xs">{r.display_name}</td>
                    <td className="px-4 py-2"><InlineText value={r.pattern} className="max-w-xs" onSave={(v) => persistRule({ ...r, pattern: v })} t={t} /></td>
                    <td className="px-4 py-2">
                      <select value={r.severity} disabled={isSaving} onChange={(e) => persistRule({ ...r, severity: e.target.value as any })} className="rounded border border-gray-300 px-2 py-0.5 text-xs">
                        <option value="critical">critical</option><option value="high">high</option><option value="medium">medium</option><option value="low">low</option>
                      </select>
                    </td>
                    <td className="px-4 py-2">
                      <select value={r.action} disabled={isSaving} onChange={(e) => persistRule({ ...r, action: e.target.value as any })} className="rounded border border-gray-300 px-2 py-0.5 text-xs">
                        <option value="block">block</option><option value="remove">remove</option><option value="rewrite">rewrite</option>
                      </select>
                    </td>
                    <td className="px-4 py-2 text-xs text-gray-600"><InlineText value={r.description ?? ''} onSave={(v) => persistRule({ ...r, description: v })} t={t} /></td>
                    <td className="px-4 py-2 text-right">
                      <div className="inline-flex items-center gap-1">
                        <button disabled={isSaving} onClick={() => persistRule({ ...r, is_enabled: !r.is_enabled })}
                          className={`rounded-full border px-2 py-0.5 text-xs ${r.is_enabled ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-gray-100 text-gray-500 border-gray-200'}`}>
                          {r.is_enabled ? t('secplane.secureClaw.privacyRules.on') : t('secplane.secureClaw.privacyRules.off')}
                        </button>
                        <button onClick={() => hardDeleteRule(r.rule_id)} className="rounded border border-rose-200 bg-white px-2 py-0.5 text-xs text-rose-700 hover:bg-rose-50">{t('secplane.secureClaw.privacyRules.delete')}</button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>

        {/* Section 13: IOC */}
        <section className="rounded-xl border border-gray-200 bg-white">
          <div className="border-b border-gray-100 px-4 py-3">
            <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.ioc.title', { count: iocRules.length })}</div>
            <div className="text-xs text-gray-500">{t('secplane.secureClaw.ioc.subtitle')}</div>
          </div>
          {(['suspicious_skill_pattern', 'c2_server', 'clawhavoc_name', 'clawhavoc_malware', 'malicious_domain', 'infostealer_target'] as const).map((sub) => {
            const items = iocRules.filter((r) => r.tags === sub);
            return (
              <details key={sub} className="border-t border-gray-100 first:border-t-0">
                <summary className="cursor-pointer bg-gray-50 px-4 py-2 hover:bg-gray-100 text-sm">
                  <span className="font-medium text-gray-900">{sub}</span><span className="ml-2 text-xs text-gray-500">{items.length}</span>
                </summary>
                <ul className="divide-y divide-gray-100">
                  {items.length === 0 && <li className="px-5 py-2 text-xs text-gray-400 italic">{t('secplane.secureClaw.ioc.empty')}</li>}
                  {items.map((r) => (
                    <li key={r.rule_id} className="flex items-center gap-2 px-5 py-1.5">
                      <input type="checkbox" checked={r.is_enabled} disabled={savingRuleID === r.rule_id} onChange={() => persistRule({ ...r, is_enabled: !r.is_enabled })} className="h-4 w-4 rounded border-gray-300" />
                      <div className="flex-1 min-w-0"><InlineText value={r.pattern} onSave={(v) => persistRule({ ...r, pattern: v, display_name: v })} t={t} /></div>
                      <button onClick={() => hardDeleteRule(r.rule_id)} className="text-xs text-rose-600 hover:text-rose-800">{t('secplane.secureClaw.dangerousCommands.delete')}</button>
                    </li>
                  ))}
                </ul>
                <AddInput placeholder={t(`secplane.secureClaw.ioc.addPlaceholder.${sub}`)} t={t}
                  onAdd={async (v) => {
                    const slug = Date.now().toString(36) + Math.random().toString(36).slice(2, 5);
                    await persistRule({ rule_id: `sc.ioc.${sub}.user-${slug}`, kind: 'secureclaw_ioc', display_name: v, pattern: v, tags: sub, target: 'user_input', severity: 'high', action: 'block', mode: 'enforce', is_enabled: true, sort_order: 8999 });
                  }} />
              </details>
            );
          })}
        </section>

        {/* Section 14: Alerts */}
        <section className="rounded-xl border border-gray-200 bg-white">
          <div className="flex items-center justify-between border-b border-gray-100 px-4 py-3">
            <div>
              <div className="text-sm font-semibold text-gray-800">{t('secplane.secureClaw.alerts.title')}</div>
              <div className="text-xs text-gray-500">{t('secplane.secureClaw.alerts.subtitle')}</div>
            </div>
            <button onClick={loadAlerts} className="rounded border border-gray-300 bg-white px-3 py-1 text-xs text-gray-700 hover:bg-gray-50">{t('secplane.secureClaw.alerts.refresh')}</button>
          </div>
          {alertsError && <div className="m-4 rounded border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{alertsError}</div>}
          <div className="overflow-hidden">
            <table className="min-w-full divide-y divide-gray-200 text-sm">
              <thead className="bg-gray-50 text-left text-xs uppercase text-gray-500">
                <tr>
                  <th className="px-4 py-2">{t('secplane.secureClaw.alerts.time')}</th>
                  <th className="px-4 py-2">{t('secplane.secureClaw.alerts.source')}</th>
                  <th className="px-4 py-2">{t('secplane.secureClaw.alerts.rule')}</th>
                  <th className="px-4 py-2">{t('secplane.secureClaw.alerts.severity')}</th>
                  <th className="px-4 py-2">{t('secplane.secureClaw.alerts.action')}</th>
                  <th className="px-4 py-2">{t('secplane.secureClaw.alerts.evidence')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {alerts.map((alert) => (
                  <tr key={alert.id} className="hover:bg-gray-50">
                    <td className="px-4 py-2 text-xs text-gray-500">{new Date(alert.ts).toLocaleString()}</td>
                    <td className="px-4 py-2"><span className="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-700">{alert.source}</span></td>
                    <td className="px-4 py-2">
                      <div className="font-medium text-gray-900">{alert.rule_name ?? alert.rule_id ?? '-'}</div>
                      {alert.rule_id && alert.rule_name && <div className="text-xs text-gray-500">{alert.rule_id}</div>}
                    </td>
                    <td className="px-4 py-2"><span className={`rounded-full border px-2 py-0.5 text-xs ${severityChip(alert.severity)}`}>{alert.severity}</span></td>
                    <td className="px-4 py-2"><span className={`rounded-full border px-2 py-0.5 text-xs ${actionPill(alert.action)}`}>{alert.action}</span></td>
                    <td className="px-4 py-2 text-xs text-gray-700"><div className="max-w-md truncate" title={alert.evidence ?? ''}>{alert.evidence ?? '-'}</div></td>
                  </tr>
                ))}
                {alerts.length === 0 && !alertsLoading && (
                  <tr><td colSpan={6} className="px-4 py-8 text-center text-sm text-gray-500">{t('secplane.secureClaw.alerts.noAlerts')}</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <DispatchPickerModal open={pickerOpen} onClose={() => setPickerOpen(false)} onDispatch={runDispatch} dispatching={dispatching}
        title={t('secplane.secureClaw.dispatchTitle')} hint={t('secplane.secureClaw.dispatchHint')} />
    </AdminLayout>
  );
};

export default SecureClawPage;
