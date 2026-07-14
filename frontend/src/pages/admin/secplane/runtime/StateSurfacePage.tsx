import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import ApplyDispatchButton from '../../../../components/secplane/ApplyDispatchButton';
import { useInstanceHealth } from './useInstanceHealth';
import { useSurfaceBackend } from './useSurfaceBackend';
import { FEATURES } from '../../../../config/features';
import { useI18n } from '../../../../contexts/I18nContext';

const SCENARIO_DEFENSES = ['defense.memoryGuard', 'defense.selfProtection'];

// State Surface Protection (scenario b)
// Backend: defense.memoryGuard / defense.selfProtection toggles + related alerts

type Tone = 'red' | 'orange' | 'amber' | 'blue' | 'purple' | 'green' | 'slate';
type Mode = 'enforce' | 'observe' | 'off';
const ALERT_PREFIXES = ['defense.memoryGuard', 'defense.selfProtection', 'pp.'];

const PROTECTED_PATHS: Array<[string, string, string, Mode, number]> = [
  ['memory_store/', 'memoryStore', 'memory_store/**/*', 'enforce', 18],
  ['MEMORY.md', 'memoryMd', '**/MEMORY.md', 'enforce', 8],
  ['SOUL.md', 'soulMd', '**/SOUL.md', 'enforce', 6],
  ['memory/', 'memoryDir', '**/memory/**', 'enforce', 6],
];

const INTEGRITY_EVENTS: Array<[string, string, string, string, Tone, string]> = [
  ['openclaw-prod-east-12', 'memory_store/long_term.json', 'prodEast', '2m', 'red', 'prodEast'],
  ['openclaw-finance-svc', 'MEMORY.md', 'financeSvc', '5m', 'orange', 'financeSvc'],
  ['openclaw-mcp-router', 'SOUL.md', 'mcpRouter', '12m', 'red', 'mcpRouter'],
  ['openclaw-staging-7', 'memory_store/new-session.md', 'staging', '25m', 'amber', 'staging'],
  ['openclaw-dev-test-1', 'memory_store/long_term.json', 'devTest', '1h', 'slate', 'devTest'],
];

const PATH_OPTIONS = ['memory_store/', 'MEMORY.md', 'SOUL.md', 'memory/'];

const StateSurfacePage: React.FC = () => {
  const { t } = useI18n();
  const { rules, alerts, dispatching, dispatchMsg, modeOf, setMode, dispatchApply } = useSurfaceBackend(ALERT_PREFIXES);
  const { instances, healthy } = useInstanceHealth();
  const enabledDefenseCount = rules.filter((r) => SCENARIO_DEFENSES.includes(r.rule_id) && r.is_enabled).length;
  const mode = modeOf('defense.memoryGuard', 'enforce');
  const handleModeChange = (next: Mode) => {
    setMode('defense.memoryGuard', next);
    setMode('defense.selfProtection', next);
  };

  const [pathFilter, setPathFilter] = useState<string>('all');
  const [actionFilter, setActionFilter] = useState<'all' | 'block' | 'observe' | 'redact'>('all');
  const [query, setQuery] = useState('');

  const q = query.trim().toLowerCase();
  const filteredAlerts = alerts.filter((a) => {
    if (actionFilter !== 'all' && a.action !== actionFilter) return false;
    if (pathFilter !== 'all' && !a.subject?.includes(pathFilter)) return false;
    if (!q) return true;
    return [a.agent_id, a.subject, a.rule_id, a.rule_name, a.evidence]
      .some((v) => v?.toLowerCase().includes(q));
  });

  const exportJsonl = () => {
    const text = filteredAlerts.map((a) => JSON.stringify(a)).join('\n');
    const blob = new Blob([text], { type: 'application/jsonl' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `state-surface-alerts-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.jsonl`;
    link.click();
    URL.revokeObjectURL(url);
  };
  return (
    <AdminLayout title={t('secplane.runtime.shared.crumbSecurity')}>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('secplane.runtime.shared.crumbSecurity')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/runtime">{t('secplane.runtime.shared.crumbRuntime')}</Link>
          <span>/</span>
          <span className="crumb-current">{t('secplane.runtime.stateSurface.crumbCurrent')}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t('secplane.runtime.stateSurface.heroEyebrow')}</div>
            <h2 className="h-title">{t('secplane.runtime.stateSurface.heroTitle')}</h2>
            <p className="h-subtitle">{t('secplane.runtime.stateSurface.heroSubtitle')}</p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statToggle')}</div>
              <div className={`stat-card-value ${enabledDefenseCount === SCENARIO_DEFENSES.length ? 'tone-green' : 'tone-orange'}`}>{enabledDefenseCount}/{SCENARIO_DEFENSES.length}</div>
              <div className="stat-card-sub muted-strong">memoryGuard · selfProtection</div>
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
              <div className="eyebrow">{t('secplane.runtime.stateSurface.pathsEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.stateSurface.pathsTitle')}</h3>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-xs muted-strong">{t('secplane.runtime.stateSurface.defenseMode')}</span>
              <div className="mode-selector">
                <button className={mode === 'enforce' ? 'active-enforce' : ''} onClick={() => handleModeChange('enforce')}>
                  {t('secplane.runtime.shared.modeEnforce')}
                </button>
                <button className={mode === 'observe' ? 'active-observe' : ''} onClick={() => handleModeChange('observe')}>
                  {t('secplane.runtime.shared.modeMonitor')}
                </button>
                <button className={mode === 'off' ? 'active-off' : ''} onClick={() => handleModeChange('off')}>
                  {t('secplane.runtime.shared.modeStop')}
                </button>
              </div>
              <ApplyDispatchButton onDispatch={dispatchApply} busy={dispatching} className="btn-primary btn-sm" triggerLabel={t('secplane.runtime.shared.saveApply')} />
              {dispatchMsg && <span className="text-xs muted ml-2">{dispatchMsg}</span>}
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            {PROTECTED_PATHS.map(([path, descKey, pattern, , hits]) => (
              <div key={path} className="p-4 rounded-2xl border border-[#eadfd8] bg-[#fffaf7]">
                <div className="flex items-start justify-between mb-2">
                  <code className="text-sm font-bold text-[#171212]">{path}</code>
                  <span className="badge badge-slate">{t('secplane.runtime.stateSurface.sharedMode')}</span>
                </div>
                <div className="text-xs muted mb-2">{t('secplane.runtime.stateSurface.protectedPaths.' + descKey + '.desc')}</div>
                <code className="block text-xs muted-strong bg-white px-2 py-1 rounded">{pattern}</code>
                <div className="divider" />
                <div className="flex items-center justify-between">
                  <span className="text-xs muted-strong">{t('secplane.runtime.shared.hits24h')}</span>
                  <span className="text-lg font-bold tone-red">{hits}</span>
                </div>
              </div>
            ))}
          </div>
        </div>

        {FEATURES.memoryIntegrityCheck && <div className="panel">
          <div className="mb-4">
            <div className="eyebrow">{t('secplane.runtime.stateSurface.integrityEyebrow')}</div>
            <h3 className="section-title-lg mt-1">{t('secplane.runtime.stateSurface.integrityTitle')}</h3>
          </div>
          <div className="alert alert-warning mb-3">
            <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            {t('secplane.runtime.stateSurface.integrityWarning', { count: 5, critical: 2, high: 1, medium: 1, info: 1 })}
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th>{t('secplane.runtime.shared.colInstance')}</th>
                <th>{t('secplane.runtime.shared.colFile')}</th>
                <th>{t('secplane.runtime.shared.colStatus')}</th>
                <th>{t('secplane.runtime.shared.colCheckTime')}</th>
              </tr>
            </thead>
            <tbody>
              {INTEGRITY_EVENTS.map(([inst, file, sevKey, time, tone, triggerKey]) => (
                <tr key={inst + file}>
                  <td><span className="font-mono text-xs">{inst}</span></td>
                  <td><code className="text-xs">{file}</code></td>
                  <td>
                    <span className={`badge badge-${tone}`}>{t('secplane.runtime.stateSurface.integrityEvents.' + sevKey + '.severity')}</span>{' '}
                    <span className="text-xs muted ml-1">{t('secplane.runtime.stateSurface.integrityEvents.' + triggerKey + '.trigger')}</span>
                  </td>
                  <td><span className="text-xs muted-strong">{time}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>}

        <div className="panel">
          <div className="flex items-center justify-between mb-4 gap-4 flex-wrap">
            <div>
              <div className="eyebrow">{t('secplane.runtime.stateSurface.logEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.stateSurface.logTitle')}</h3>
            </div>
            <div className="flex gap-2 items-center flex-wrap">
              <select
                className="input"
                style={{ width: 150 }}
                value={pathFilter}
                onChange={(e) => setPathFilter(e.target.value)}
              >
                <option value="all">{t('secplane.runtime.shared.allPaths')}</option>
                {PATH_OPTIONS.map((p) => <option key={p} value={p}>{p}</option>)}
              </select>
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
                style={{ width: 200 }}
                placeholder={t('secplane.runtime.stateSurface.logSearchPlaceholder') ?? ''}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
              <button
                className="btn-secondary btn-sm"
                onClick={exportJsonl}
                disabled={filteredAlerts.length === 0}
              >
                {t('secplane.runtime.shared.exportJsonl')}
              </button>
            </div>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colTime')}</th>
                <th>{t('secplane.runtime.shared.colInstance')}</th>
                <th>{t('secplane.runtime.shared.colHitPath')}</th>
                <th style={{ width: 140 }}>{t('secplane.runtime.shared.colRule')}</th>
                <th>{t('secplane.runtime.shared.colTrigger')}</th>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colAction')}</th>
              </tr>
            </thead>
            <tbody>
              {filteredAlerts.length === 0 && (
                <tr>
                  <td colSpan={6} className="text-xs muted" style={{ textAlign: 'center', padding: 20 }}>
                    {alerts.length === 0 ? t('secplane.runtime.shared.noAlertEvents') : t('secplane.runtime.shared.noMatchEvents')}
                  </td>
                </tr>
              )}
              {filteredAlerts.slice(0, 50).map((a) => (
                <tr key={a.id}>
                  <td><span className="muted-strong text-xs">{a.ts?.replace('T', ' ').slice(0, 19) ?? '—'}</span></td>
                  <td><span className="font-mono text-xs">{a.agent_id ?? '—'}</span></td>
                  <td><code className="text-xs">{a.subject ?? '—'}</code></td>
                  <td><code className="text-xs text-[#7a4a30]">{a.rule_id ?? '—'}</code></td>
                  <td><span className="text-xs muted">{a.evidence ?? a.rule_name ?? '—'}</span></td>
                  <td>
                    <span className={`badge badge-${a.action === 'block' ? 'red' : a.action === 'redact' ? 'orange' : 'slate'}`}>
                      {a.action}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="text-xs muted mt-3 text-center">
            {t('secplane.runtime.shared.filteredRows', { total: alerts.length, filtered: filteredAlerts.length })}
          </div>
        </div>
      </div>
    </AdminLayout>
  );
};

export default StateSurfacePage;
