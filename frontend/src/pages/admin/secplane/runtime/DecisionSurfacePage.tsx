import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import ApplyDispatchButton from '../../../../components/secplane/ApplyDispatchButton';
import { useInstanceHealth } from './useInstanceHealth';
import { useSurfaceBackend } from './useSurfaceBackend';
import { useI18n } from '../../../../contexts/I18nContext';

const SCENARIO_DEFENSES = [
  'defense.commandBlock',
  'defense.loopGuard',
  'defense.encodingGuard',
  'defense.scriptProvenanceGuard',
  'defense.exfiltrationGuard',
];

// Decision Surface Protection (scenario c)
// Backend: 5 defense_toggle items + dispatchAegisApply + alerts

type Tone = 'red' | 'orange' | 'amber' | 'blue' | 'purple' | 'green' | 'slate';

// 5 danger categories mapped to defense_toggle rule_ids
const DANGER_CATEGORIES: Array<[string, string, string, string, Tone]> = [
  ['defense.commandBlock',          'commandBlock', 'commandBlockEnabled',          'rm -rf / dd / mkfs / fork bomb',          'red'],
  ['defense.loopGuard',             'loopGuard', 'loopGuardEnabled',             'Same mutable tool high-frequency retry / repeated mutating within budget', 'red'],
  ['defense.encodingGuard',         'encodingGuard', 'encodingGuardEnabled',         'base64 / hex / Unicode escape bypass',         'red'],
  ['defense.scriptProvenanceGuard', 'scriptProvenanceGuard', 'scriptProvenanceGuardEnabled', 'curl|bash / wget|sh / chained calls',          'red'],
  ['defense.exfiltrationGuard',     'exfiltrationGuard', 'exfiltrationGuardEnabled',     'Internal network scan / reverse shell / DNS tunnel',         'red'],
];

const ALERT_PREFIXES = [
  'defense.commandBlock',
  'defense.loopGuard',
  'defense.encodingGuard',
  'defense.scriptProvenanceGuard',
  'defense.exfiltrationGuard',
];

const DecisionSurfacePage: React.FC = () => {
  const { t } = useI18n();
  const { rules, alerts, dispatching, dispatchMsg, modeOf, setMode: setRuleMode, dispatchApply } = useSurfaceBackend(ALERT_PREFIXES);
  const { instances, healthy } = useInstanceHealth();
  const enabledDefenseCount = rules.filter((r) => SCENARIO_DEFENSES.includes(r.rule_id) && r.is_enabled).length;
  const [actionFilter, setActionFilter] = useState<'all' | 'block' | 'observe' | 'redact'>('all');
  const [query, setQuery] = useState('');

  const q = query.trim().toLowerCase();
  const filteredAlerts = alerts.filter((a) => {
    if (actionFilter !== 'all' && a.action !== actionFilter) return false;
    if (!q) return true;
    return [a.agent_id, a.rule_id, a.rule_name, a.subject, a.evidence]
      .some((v) => v?.toLowerCase().includes(q));
  });

  return (
    <AdminLayout title={t('secplane.runtime.shared.crumbSecurity')}>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('secplane.runtime.shared.crumbSecurity')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/runtime">{t('secplane.runtime.shared.crumbRuntime')}</Link>
          <span>/</span>
          <span className="crumb-current">{t('secplane.runtime.decisionSurface.crumbCurrent')}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t('secplane.runtime.decisionSurface.heroEyebrow')}</div>
            <h2 className="h-title">{t('secplane.runtime.decisionSurface.heroTitle')}</h2>
            <p className="h-subtitle">{t('secplane.runtime.decisionSurface.heroSubtitle')}</p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statToggle')}</div>
              <div className={`stat-card-value ${enabledDefenseCount === SCENARIO_DEFENSES.length ? 'tone-green' : 'tone-orange'}`}>{enabledDefenseCount}/{SCENARIO_DEFENSES.length}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.runtime.decisionSurface.statDangerCount')}</div>
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
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statChannel')}</div>
              <div className="stat-card-value" style={{ fontSize: '1rem' }}>install_skill</div>
              <div className="stat-card-sub muted-strong">hot-reload via mtime</div>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="flex items-center justify-between mb-4 gap-4">
            <div>
              <div className="eyebrow">{t('secplane.runtime.decisionSurface.rulesEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.decisionSurface.rulesTitle')}</h3>
            </div>
            <ApplyDispatchButton onDispatch={dispatchApply} busy={dispatching} className="btn-primary btn-sm" triggerLabel={t('secplane.runtime.shared.saveApply')} />
            {dispatchMsg && <span className="text-xs muted ml-2">{dispatchMsg}</span>}
          </div>
          <div className="space-y-2.5">
            {DANGER_CATEGORIES.map(([ruleId, catKey, flag, _desc, tone]) => {
              const curMode = modeOf(ruleId, 'enforce');
              const hitCount = alerts.filter((a) => a.rule_id?.startsWith(ruleId)).length;
              return (
                <div key={ruleId} className="flex items-center gap-4 p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1 flex-wrap">
                      <span className="font-semibold text-[#171212]">{t(`secplane.runtime.decisionSurface.categories.${catKey}.name`)}</span>
                      <code className="text-[10px] muted-strong tracking-wider">{flag}</code>
                      <code className="text-[10px] text-[#7a4a30] bg-[#fdf6f1] px-1.5 py-0.5 rounded">before_tool_call</code>
                    </div>
                    <div className="text-xs muted">{t(`secplane.runtime.decisionSurface.categories.${catKey}.desc`)}</div>
                  </div>
                  <div className="shrink-0">
                    <div className="mode-selector">
                      <button className={curMode === 'enforce' ? 'active-enforce' : ''} onClick={() => setRuleMode(ruleId, 'enforce')}>{t('secplane.runtime.shared.modeEnforce')}</button>
                      <button className={curMode === 'observe' ? 'active-observe' : ''} onClick={() => setRuleMode(ruleId, 'observe')}>{t('secplane.runtime.shared.modeMonitor')}</button>
                      <button className={curMode === 'off' ? 'active-off' : ''} onClick={() => setRuleMode(ruleId, 'off')}>{t('secplane.runtime.shared.modeStop')}</button>
                    </div>
                  </div>
                  <div className="text-right shrink-0" style={{ minWidth: 80 }}>
                    <div className={`text-lg font-bold leading-none ${hitCount > 0 ? `tone-${tone}` : 'muted-strong'}`}>{hitCount}</div>
                    <div className="text-xs muted-strong mt-0.5">{t('secplane.runtime.shared.recentHits')}</div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        <div className="panel">
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className="eyebrow">{t('secplane.runtime.decisionSurface.logEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.decisionSurface.logTitle')}</h3>
            </div>
            <div className="flex gap-2 items-center">
              <select
                className="input"
                style={{ width: 140 }}
                value={actionFilter}
                onChange={(e) => setActionFilter(e.target.value as typeof actionFilter)}
              >
                <option value="all">{t('secplane.runtime.shared.allActions')}</option>
                <option value="block">{t('secplane.runtime.shared.blockAction')}</option>
                <option value="observe">{t('secplane.runtime.shared.observeAction')}</option>
                <option value="redact">{t('secplane.runtime.shared.redactAction')}</option>
              </select>
              <input
                className="input"
                style={{ width: 240 }}
                placeholder={t('secplane.runtime.decisionSurface.searchPlaceholder') ?? ''}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colTime')}</th>
                <th>{t('secplane.runtime.shared.colInstance')}</th>
                <th>{t('secplane.runtime.shared.colRule')}</th>
                <th>{t('secplane.runtime.shared.colCommand')}</th>
                <th>{t('secplane.runtime.shared.colHitPattern')}</th>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colSeverity')}</th>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colAction')}</th>
              </tr>
            </thead>
            <tbody>
              {filteredAlerts.length === 0 && (
                <tr>
                  <td colSpan={7} className="text-xs muted" style={{ textAlign: 'center', padding: 20 }}>
                    {alerts.length === 0 ? t('secplane.runtime.shared.noAlertEvents') : t('secplane.runtime.shared.noMatchEvents')}
                  </td>
                </tr>
              )}
              {filteredAlerts.slice(0, 50).map((a) => (
                <tr key={a.id}>
                  <td><span className="muted-strong text-xs">{a.ts?.replace('T', ' ').slice(11, 19)}</span></td>
                  <td><span className="font-mono text-xs">{a.agent_id ?? '—'}</span></td>
                  <td><code className="text-xs">{a.rule_id?.split('.')[1] ?? a.rule_id ?? '—'}</code></td>
                  <td><code className="text-xs text-[#171212] truncate inline-block" style={{ maxWidth: 320 }}>{a.subject ?? a.evidence?.slice(0, 80) ?? '—'}</code></td>
                  <td><span className="text-xs">{a.rule_name ?? '—'}</span></td>
                  <td><span className={`badge ${a.severity === 'high' ? 'badge-red' : a.severity === 'medium' ? 'badge-orange' : 'badge-slate'}`}>{a.severity}</span></td>
                  <td><span className={`badge badge-${a.action === 'block' ? 'red' : a.action === 'observe' ? 'orange' : 'slate'}`}>{a.action}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

    </AdminLayout>
  );
};

export default DecisionSurfacePage;
