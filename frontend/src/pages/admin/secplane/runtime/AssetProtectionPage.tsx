import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import ApplyDispatchButton from '../../../../components/secplane/ApplyDispatchButton';
import { secplaneService, type SecplaneRule } from '../../../../services/secplaneService';
import { useInstanceHealth } from './useInstanceHealth';
import { useSurfaceBackend } from './useSurfaceBackend';
import { FEATURES } from '../../../../config/features';
import { useI18n } from '../../../../contexts/I18nContext';

const SCENARIO_DEFENSES = ['defense.memoryGuard', 'defense.loopGuard', 'defense.selfProtection'];

// Asset Anti-Tamper (scenario f)
// Backend: 3 defense_toggle items + dispatchAegisApply + alerts
// Operator-added protectedPaths/Skills/Plugins go through secplane policy_rule (kind=protected_*),
// add/remove immediately upserts to backend; ApplyDispatch compiles current rules and pushes.

const CUSTOM_KINDS = [
  { kind: 'protected_path' as const, key: 'path', badge: 'orange' as const, idPrefix: 'pp.' },
  { kind: 'protected_skill' as const, key: 'skill', badge: 'purple' as const, idPrefix: 'psk.' },
  { kind: 'protected_plugin' as const, key: 'plugin', badge: 'green' as const, idPrefix: 'ppl.' },
];
type CustomKind = (typeof CUSTOM_KINDS)[number]['kind'];

async function sha8(s: string): Promise<string> {
  const buf = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(s));
  return Array.from(new Uint8Array(buf)).map((b) => b.toString(16).padStart(2, '0')).join('').slice(0, 8);
}

const ALERT_PREFIXES = ['defense.memoryGuard', 'defense.loopGuard', 'defense.selfProtection', 'pp.', 'psk.', 'ppl.'];

type Tone = 'red' | 'orange' | 'amber' | 'blue' | 'purple' | 'green' | 'slate';
type Mode = 'enforce' | 'observe' | 'off';
type AssetKind = 'memory' | 'skill' | 'plugin' | 'credential';

// [ruleId, nameKey, flag, descKey, hook, tone, default-count, pattern]
const RULES: Array<[string, string, string, string, string, Tone, number, string]> = [
  ['defense.memoryGuard', 'memoryGuard', 'memoryGuardEnabled + memoryGuardMode', 'memoryGuard', 'before_tool_call · before_message_write', 'red', 23, 'target ∈ {memory_store, MEMORY.md, SOUL.md, memory/**}'],
  ['defense.loopGuard', 'loopGuard', 'loopGuardEnabled + loopGuardMode', 'loopGuard', 'before_tool_call', 'red', 4, 'memory_store ∈ LOOP_GUARD_TOOL_NAMES · retry > budget / run'],
  ['defense.selfProtection', 'selfProtection', 'selfProtectionEnabled + selfProtectionMode', 'selfProtection', 'before_tool_call', 'red', 7, 'skill ∈ {claude-skill-v2}; plugin ∈ {@openclaw/auth}'],
];

const CORE_ASSETS: Array<[string, string, AssetKind, 'realtime' | 'install' | 'manual']> = [
  ['skill:claude-skill-v2', 'skillClaude', 'skill', 'install'],
  ['plugin:@openclaw/auth', 'pluginAuth', 'plugin', 'manual'],
  ['memory_store/', 'memoryStore', 'memory', 'realtime'],
  ['~/.openclaw/SOUL.md', 'soulMd', 'memory', 'realtime'],
  ['~/.openclaw/MEMORY.md', 'memoryMd', 'memory', 'realtime'],
  ['<stateDir>/memory/*.md', 'memoryShards', 'memory', 'realtime'],
  ['<stateDir>/credentials/', 'credentialsDir', 'credential', 'realtime'],
  ['<stateDir>/.env', 'envFile', 'credential', 'realtime'],
];

const DRIFT_CHECKS: Array<[string, string, string, 'drift' | 'ok', Tone, string]> = [
  ['<stateDir>/memory/SOUL.md', 'openclaw-prod-east-12', '24m', 'drift', 'red', 'soulMd'],
  ['<stateDir>/memory/MEMORY.md', 'openclaw-finance-svc', '1h', 'drift', 'red', 'memoryMd'],
  ['<stateDir>/skill/auth-helper/SKILL.md', 'openclaw-ops-bot-3', '3h', 'drift', 'orange', 'skillMd'],
  ['<stateDir>/baseline/credentials.sha256', 'all', '5h', 'ok', 'green', 'credBaseline'],
  ['<stateDir>/plugin/auth/manifest.json', 'openclaw-staging-7', '6h', 'ok', 'green', 'pluginManifest'],
];

const MEMORY_ALERTS: Array<[string, string, string, string, string, Tone]> = [
  ['alert1', 'prod-east-12', 'SOUL.md', 'alert1', 'alert1', 'red'],
  ['alert2', 'finance-svc', 'MEMORY.md', 'alert2', 'alert2', 'orange'],
  ['alert3', 'ops-bot-3', 'memory/task-12.md', 'alert3', 'alert3', 'amber'],
  ['alert4', 'staging-7', 'memory/notes.md', 'alert4', 'alert4', 'green'],
  ['alert5', 'mcp-router', 'SOUL.md', 'alert5', 'alert5', 'green'],
];

const assetBadge = (k: AssetKind) =>
  k === 'memory' ? 'badge-orange' : k === 'skill' ? 'badge-purple' : k === 'plugin' ? 'badge-green' : 'badge-red';

const autoBadge = (a: 'realtime' | 'install' | 'manual') =>
  a === 'realtime'
    ? { class: 'badge-green', labelKey: 'secplane.runtime.shared.realtime' }
    : a === 'install'
    ? { class: 'badge-orange', labelKey: 'secplane.runtime.shared.installOnce' }
    : { class: 'badge-red', labelKey: 'secplane.runtime.shared.manual' };

const AssetProtectionPage: React.FC = () => {
  const { t } = useI18n();
  const { rules, alerts, dispatching, dispatchMsg, modeOf, setMode: setRuleMode, dispatchApply } = useSurfaceBackend(ALERT_PREFIXES);
  const { instances, healthy } = useInstanceHealth();
  const enabledDefenseCount = rules.filter((r) => SCENARIO_DEFENSES.includes(r.rule_id) && r.is_enabled).length;
  const [customMode, setCustomMode] = useState<Mode>('enforce');
  const [customRules, setCustomRules] = useState<Record<CustomKind, SecplaneRule[]>>({
    protected_path: [],
    protected_skill: [],
    protected_plugin: [],
  });
  const [customInputs, setCustomInputs] = useState<Record<CustomKind, string>>({
    protected_path: '',
    protected_skill: '',
    protected_plugin: '',
  });
  const [customBusy, setCustomBusy] = useState(false);
  const [customError, setCustomError] = useState<string | null>(null);

  useEffect(() => {
    (async () => {
      try {
        const [p, s, pl] = await Promise.all(
          CUSTOM_KINDS.map((k) => secplaneService.listRules(k.kind)),
        );
        setCustomRules({
          protected_path: p.filter((r) => r.is_enabled),
          protected_skill: s.filter((r) => r.is_enabled),
          protected_plugin: pl.filter((r) => r.is_enabled),
        });
      } catch {
        // ignore — UI can still manually add; next dispatch will re-fetch
      }
    })();
  }, []);

  const addCustomItem = async (cfg: (typeof CUSTOM_KINDS)[number]) => {
    const pattern = customInputs[cfg.kind].trim();
    if (!pattern) return;
    setCustomBusy(true);
    setCustomError(null);
    try {
      const id = `${cfg.idPrefix}${await sha8(pattern)}`;
      const displayName = pattern.length > 60 ? `${pattern.slice(0, 60)}…` : pattern;
      const saved = await secplaneService.saveRule({
        rule_id: id,
        kind: cfg.kind,
        display_name: displayName,
        pattern,
        target: 'user_input',
        severity: 'medium',
        action: 'block',
        mode: customMode,
        is_enabled: true,
        sort_order: 0,
      });
      setCustomRules((prev) => {
        const list = prev[cfg.kind];
        const next = list.some((r) => r.rule_id === saved.rule_id)
          ? list.map((r) => (r.rule_id === saved.rule_id ? saved : r))
          : [...list, saved];
        return { ...prev, [cfg.kind]: next };
      });
      setCustomInputs((prev) => ({ ...prev, [cfg.kind]: '' }));
    } catch (e) {
      setCustomError(e instanceof Error ? e.message : String(e));
    } finally {
      setCustomBusy(false);
    }
  };

  const removeCustomItem = async (kind: CustomKind, rule: SecplaneRule) => {
    setCustomBusy(true);
    setCustomError(null);
    try {
      await secplaneService.disableRule(rule.rule_id);
      setCustomRules((prev) => ({
        ...prev,
        [kind]: prev[kind].filter((r) => r.rule_id !== rule.rule_id),
      }));
    } catch (e) {
      setCustomError(e instanceof Error ? e.message : String(e));
    } finally {
      setCustomBusy(false);
    }
  };

  return (
    <AdminLayout title={t('secplane.runtime.shared.crumbSecurity')}>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('secplane.runtime.shared.crumbSecurity')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/runtime">{t('secplane.runtime.shared.crumbRuntime')}</Link>
          <span>/</span>
          <span className="crumb-current">{t('secplane.runtime.assetProtection.crumbCurrent')}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t('secplane.runtime.assetProtection.heroEyebrow')}</div>
            <h2 className="h-title">{t('secplane.runtime.assetProtection.heroTitle')}</h2>
            <p className="h-subtitle">{t('secplane.runtime.assetProtection.heroSubtitle')}</p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statToggle')}</div>
              <div className={`stat-card-value ${enabledDefenseCount === SCENARIO_DEFENSES.length ? 'tone-green' : 'tone-orange'}`}>{enabledDefenseCount}/{SCENARIO_DEFENSES.length}</div>
              <div className="stat-card-sub muted-strong">memoryGuard · loopGuard · selfProtection</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.assetProtection.statCustom')}</div>
              <div className="stat-card-value">{customRules.protected_path.length + customRules.protected_skill.length + customRules.protected_plugin.length}</div>
              <div className="stat-card-sub muted-strong">paths {customRules.protected_path.length} · skills {customRules.protected_skill.length} · plugins {customRules.protected_plugin.length}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statAlerts')}</div>
              <div className={`stat-card-value ${alerts.length > 0 ? 'tone-red' : 'tone-green'}`}>{alerts.length}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.runtime.shared.statAlertsSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statInstances')}</div>
              <div className="stat-card-value">{instances.length}</div>
              <div className="stat-card-sub muted-strong">{healthy.length} running</div>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="flex items-center justify-between mb-4 gap-4">
            <div>
              <div className="eyebrow">{t('secplane.runtime.assetProtection.rulesEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.assetProtection.rulesTitle')}</h3>
            </div>
            <ApplyDispatchButton onDispatch={dispatchApply} busy={dispatching} className="btn-primary btn-sm" triggerLabel={t('secplane.runtime.shared.saveApply')} />
            {dispatchMsg && <span className="text-xs muted ml-2">{dispatchMsg}</span>}
          </div>
          <div className="space-y-2.5">
            {RULES.map(([ruleId, nameKey, flag, descKey, hook, tone, count, pat]) => {
              const curMode = modeOf(ruleId, 'enforce');
              const realHits = alerts.filter((a) => a.rule_id?.startsWith(ruleId)).length;
              return (
                <div key={ruleId} className="flex items-center gap-4 p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1 flex-wrap">
                      <span className="font-semibold text-[#171212]">{t(`secplane.runtime.assetProtection.rules.${nameKey}.name`)}</span>
                      <code className="text-[10px] muted-strong tracking-wider">{flag}</code>
                      <code className="text-[10px] text-[#7a4a30] bg-[#fdf6f1] px-1.5 py-0.5 rounded">{hook}</code>
                    </div>
                    <div className="text-xs muted mb-1">{t(`secplane.runtime.assetProtection.rules.${descKey}.desc`)}</div>
                    <code className="block text-[10px] muted-strong bg-[#fdf6f1] px-2 py-1 rounded font-mono truncate" style={{ maxWidth: 480 }}>{pat}</code>
                  </div>
                  <div className="shrink-0">
                    <div className="mode-selector">
                      <button className={curMode === 'enforce' ? 'active-enforce' : ''} onClick={() => setRuleMode(ruleId, 'enforce')}>{t('secplane.runtime.shared.modeEnforce')}</button>
                      <button className={curMode === 'observe' ? 'active-observe' : ''} onClick={() => setRuleMode(ruleId, 'observe')}>{t('secplane.runtime.shared.modeMonitor')}</button>
                      <button className={curMode === 'off' ? 'active-off' : ''} onClick={() => setRuleMode(ruleId, 'off')}>{t('secplane.runtime.shared.modeStop')}</button>
                    </div>
                  </div>
                  <div className="text-right shrink-0 flex flex-col items-end gap-1.5" style={{ minWidth: 80 }}>
                    <div>
                      <div className={`text-lg font-bold tone-${tone} leading-none`}>{realHits || count}</div>
                      <div className="text-xs muted-strong mt-0.5">{t('secplane.runtime.shared.hits24h')}</div>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        <div className="panel">
          <div className="flex items-center justify-between mb-4 gap-4">
            <div>
              <div className="eyebrow">{t('secplane.runtime.assetProtection.customEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.assetProtection.customTitle')}</h3>
            </div>
            <div className="shrink-0 flex items-center gap-3">
              <div className="mode-selector" title={t('secplane.runtime.assetProtection.modeSelectorTitle') ?? ''}>
                <button className={customMode === 'enforce' ? 'active-enforce' : ''} onClick={() => setCustomMode('enforce')}>{t('secplane.runtime.shared.modeEnforce')}</button>
                <button className={customMode === 'observe' ? 'active-observe' : ''} onClick={() => setCustomMode('observe')}>{t('secplane.runtime.shared.modeMonitor')}</button>
                <button className={customMode === 'off' ? 'active-off' : ''} onClick={() => setCustomMode('off')}>{t('secplane.runtime.shared.modeStop')}</button>
              </div>
              <ApplyDispatchButton
                onDispatch={dispatchApply}
                busy={dispatching || customBusy}
                className="btn-primary btn-sm"
                triggerLabel={t('secplane.runtime.shared.saveApply')}
              />
            </div>
          </div>
          {customError && (
            <div className="mb-3 rounded border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">{customError}</div>
          )}
          <div className="space-y-3">
            {CUSTOM_KINDS.map((cfg) => {
              const items = customRules[cfg.kind];
              return (
                <div key={cfg.kind} className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="flex items-center justify-between mb-2.5 gap-3 flex-wrap">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="font-semibold text-[#171212]">{t(`secplane.runtime.assetProtection.customKinds.${cfg.key}.name`)}</span>
                      <code className="text-[10px] muted-strong tracking-wider">{t(`secplane.runtime.assetProtection.customKinds.${cfg.key}.field`)}</code>
                      <span className={`badge badge-${cfg.badge} text-[10px]`}>{t('secplane.runtime.shared.items', { count: items.length })}</span>
                    </div>
                    <div className="flex gap-2 items-center">
                      <input
                        className="input"
                        placeholder={t(`secplane.runtime.assetProtection.customKinds.${cfg.key}.placeholder`) ?? ''}
                        style={{ width: 200, height: 30, fontSize: 12 }}
                        value={customInputs[cfg.kind]}
                        onChange={(e) => setCustomInputs((p) => ({ ...p, [cfg.kind]: e.target.value }))}
                        onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void addCustomItem(cfg); } }}
                        disabled={customBusy}
                      />
                      <button
                        className="btn-primary btn-sm"
                        onClick={() => void addCustomItem(cfg)}
                        disabled={customBusy || !customInputs[cfg.kind].trim()}
                      >
                        {t('secplane.runtime.shared.addItem')}
                      </button>
                    </div>
                  </div>
                  <div className="text-xs muted mb-2">{t(`secplane.runtime.assetProtection.customKinds.${cfg.key}.hint`)}</div>
                  <div className="flex flex-wrap gap-2">
                    {items.length === 0 && <span className="text-xs muted">{t('secplane.runtime.shared.emptyItems')}</span>}
                    {items.map((r) => (
                      <span
                        key={r.rule_id}
                        className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-mono"
                        style={{ background: '#fdf6f1', border: '1px solid #eadfd8', color: '#7a4a30' }}
                        title={`mode=${r.mode}`}
                      >
                        {r.pattern}
                        <button
                          className="text-[#dc2626] hover:text-[#991b1b] text-sm leading-none ml-1 font-bold disabled:opacity-50"
                          onClick={() => void removeCustomItem(cfg.kind, r)}
                          disabled={customBusy}
                          aria-label={t('secplane.runtime.shared.removePattern', { pattern: r.pattern })}
                        >
                          ×
                        </button>
                      </span>
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
          <div className="text-xs muted mt-4 pt-3 border-t border-[#eadfd8]">
            {t('secplane.runtime.assetProtection.customNotePrefix')}<strong className="text-[#171212]">{t('secplane.runtime.assetProtection.customNoteHighlight')}</strong>{t('secplane.runtime.assetProtection.customNoteSuffix')}
          </div>
        </div>

        {FEATURES.coreAssetsInventory && <div className="panel">
          <div className="mb-4">
            <div className="eyebrow">{t('secplane.runtime.assetProtection.coreAssetsEyebrow')}</div>
            <h3 className="section-title-lg mt-1">{t('secplane.runtime.assetProtection.coreAssetsTitle')}</h3>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th>{t('secplane.runtime.shared.colPathResource')}</th>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colType')}</th>
                <th>{t('secplane.runtime.shared.colMechanism')}</th>
              </tr>
            </thead>
            <tbody>
              {CORE_ASSETS.map(([path, assetKey, kind, auto]) => {
                const a = autoBadge(auto as 'realtime' | 'install' | 'manual');
                return (
                  <tr key={path}>
                    <td><code className="text-sm font-mono text-[#171212]">{path}</code></td>
                    <td><span className={`badge ${assetBadge(kind)}`}>{t(`secplane.runtime.assetProtection.coreAssets.${assetKey}.type`)}</span></td>
                    <td>
                      <span className="text-xs" style={{ color: '#7a4a30', fontWeight: 500 }}>✓ {t(`secplane.runtime.assetProtection.coreAssets.${assetKey}.sec`)}</span>{' '}
                      <span className={`badge ${a.class} text-[10px] ml-2 whitespace-nowrap`}>{t(a.labelKey)}</span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>}

        <div className="grid grid-cols-2 gap-4">
          {FEATURES.assetDriftMonitor && <div className="panel">
            <div className="flex items-start justify-between mb-4 gap-3">
              <div>
                <div className="eyebrow">{t('secplane.runtime.assetProtection.driftEyebrow')}</div>
                <h3 className="section-title-lg mt-1">{t('secplane.runtime.assetProtection.driftTitle')}</h3>
              </div>
              <button className="btn-secondary btn-sm">{t('secplane.runtime.assetProtection.verifyNow')}</button>
            </div>
            <div className="space-y-2">
              {DRIFT_CHECKS.map(([path, node, time, status, tone, descKey]) => (
                <div key={path} className="flex items-center gap-3 p-3 rounded-xl border border-[#eadfd8] bg-white">
                  <span className={`dot bg-${tone}-500`} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <code className="text-xs font-mono text-[#171212]">{path}</code>
                      <span className="text-xs muted-strong">on {node}</span>
                    </div>
                    <div className="text-xs muted mt-0.5">{t(`secplane.runtime.assetProtection.driftChecks.${descKey}.desc`)}</div>
                  </div>
                  <span className={`badge badge-${tone}`}>{status === 'drift' ? t('secplane.runtime.shared.drift') : t('secplane.runtime.shared.consistent')}</span>
                  <span className="text-xs muted-strong">{time}</span>
                </div>
              ))}
            </div>
          </div>}

          {FEATURES.memoryDriftAlerts && <div className="panel">
            <div className="mb-4">
              <div className="eyebrow">{t('secplane.runtime.assetProtection.memoryAlertEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.assetProtection.memoryAlertTitle')}</h3>
            </div>
            <div className="space-y-2">
              {MEMORY_ALERTS.map(([alertKey, inst, file, _reason, _sev, tone], i) => (
                <div key={i} className="flex items-center gap-3 p-2.5 rounded-xl border border-[#eadfd8] bg-white">
                  <span className="muted-strong text-xs shrink-0 w-12">{t(`secplane.runtime.assetProtection.memoryAlerts.${alertKey}.time`)}</span>
                  <code className="text-xs font-mono text-[#171212] shrink-0">{inst}</code>
                  <code className="text-xs text-[#171212] shrink-0">{file}</code>
                  <span className="text-xs muted flex-1 truncate">{t(`secplane.runtime.assetProtection.memoryAlerts.${alertKey}.reason`)}</span>
                  <span className={`badge badge-${tone} shrink-0`}>{t(`secplane.runtime.assetProtection.memoryAlerts.${alertKey}.severity`)}</span>
                </div>
              ))}
            </div>
          </div>}
        </div>
      </div>
    </AdminLayout>
  );
};

export default AssetProtectionPage;
