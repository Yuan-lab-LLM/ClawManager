import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../components/AdminLayout';
import {
  secplaneService,
  type SecplaneAlert,
  type AlertSource,
  type Severity,
} from '../../../services/secplaneService';
import { useI18n } from '../../../contexts/I18nContext';

const sourceBadgeTone = (src: string): string => {
  switch (src) {
    case 'aegis': return 'badge-red';
    case 'secureclaw': return 'badge-purple';
    case 'gateway': return 'badge-blue';
    case 'platform': return 'badge-slate';
    case 'ksecure': return 'badge-teal';
    case 'kubearmor': return 'badge-amber';
    default: return 'badge-slate';
  }
};

const severityTone = (sev: string): string => {
  switch (sev) {
    case 'high': return 'badge-red';
    case 'medium': return 'badge-orange';
    case 'low': return 'badge-slate';
    default: return 'badge-slate';
  }
};

const actionTone = (action: string): string => {
  const a = action?.toLowerCase();
  if (a === 'block') return 'badge-red';
  if (a === 'redact') return 'badge-orange';
  if (a === 'observe') return 'badge-slate';
  return 'badge-slate';
};

const SecurityEventsPage: React.FC = () => {
  const { t } = useI18n();

  const SOURCE_OPTIONS: { value: AlertSource | ''; label: string }[] = useMemo(() => [
    { value: '', label: t('secplane.events.sourceOptions.all') },
    { value: 'aegis', label: 'aegis' },
    { value: 'secureclaw', label: 'secureclaw' },
    { value: 'gateway', label: 'gateway' },
    { value: 'platform', label: 'platform' },
    { value: 'ksecure', label: 'ksecure' },
    { value: 'kubearmor', label: 'kubearmor' },
  ], [t]);

  const SEVERITY_OPTIONS: { value: Severity | ''; label: string }[] = useMemo(() => [
    { value: '', label: t('secplane.events.severityOptions.all') },
    { value: 'high', label: t('secplane.events.severityOptions.high') },
    { value: 'medium', label: t('secplane.events.severityOptions.medium') },
    { value: 'low', label: t('secplane.events.severityOptions.low') },
  ], [t]);

  const SCENE_OPTIONS: { value: string; label: string }[] = useMemo(() => [
    { value: '', label: t('secplane.events.sceneOptions.all') },
    { value: 'defense.userRiskScan', label: t('secplane.events.sceneOptions.inputSurface') },
    { value: 'defense.memoryGuard', label: t('secplane.events.sceneOptions.stateSurface') },
    { value: 'defense.commandBlock', label: t('secplane.events.sceneOptions.decisionSurface') },
    { value: 'defense.outputRedaction', label: t('secplane.events.sceneOptions.outputSurface') },
    { value: 'pp.', label: t('secplane.events.sceneOptions.assetAntiTamper') },
    { value: 'output_redaction', label: t('secplane.events.sceneOptions.redactionAlert') },
  ], [t]);

  const ACTION_OPTIONS: { value: string; label: string }[] = useMemo(() => [
    { value: '', label: t('secplane.events.actionOptions.all') },
    { value: 'block', label: 'BLOCK' },
    { value: 'redact', label: 'REDACT' },
    { value: 'observe', label: 'OBSERVE' },
    { value: 'approval', label: 'APPROVAL' },
    { value: 'warn', label: 'WARN' },
  ], [t]);

  const QUICK_CHIPS = useMemo(() => [
    { label: t('secplane.events.quickChips.blockOnly'), apply: { action: 'block' } },
    { label: t('secplane.events.quickChips.todayHigh'), apply: { severity: 'high' } },
    { label: t('secplane.events.quickChips.jailbreak'), apply: { rule_id: 'jailbreak' } },
    { label: t('secplane.events.quickChips.outbound'), apply: { rule_id: 'outbound' } },
    { label: t('secplane.events.quickChips.hostAnomaly'), apply: { source: 'ksecure' } },
  ], [t]);

  const [items, setItems] = useState<SecplaneAlert[]>([]);
  const [loading, setLoading] = useState(false);
  const [source, setSource] = useState<AlertSource | ''>('');
  const [severity, setSeverity] = useState<Severity | ''>('');
  const [ruleIdFilter, setRuleIdFilter] = useState('');
  const [keyword, setKeyword] = useState('');
  const [scene, setScene] = useState('');
  const [actionFilter, setActionFilter] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const params: { source?: AlertSource; severity?: Severity; rule_id?: string; limit?: number } = { limit: 200 };
      if (source) params.source = source;
      if (severity) params.severity = severity;
      if (ruleIdFilter.trim()) params.rule_id = ruleIdFilter.trim();
      setItems(await secplaneService.listAlerts(params));
    } catch { setItems([]); }
    finally { setLoading(false); }
  }, [source, severity, ruleIdFilter]);

  useEffect(() => { load(); }, [load]);

  const filtered = useMemo(() => {
    let list = items;
    if (scene) list = list.filter((a) => a.rule_id?.startsWith(scene));
    if (actionFilter) list = list.filter((a) => a.action?.toLowerCase() === actionFilter);
    if (keyword.trim()) {
      const k = keyword.trim().toLowerCase();
      list = list.filter((a) => [a.rule_id, a.rule_name, a.subject, a.evidence, a.trace_id, a.agent_id].filter(Boolean).some((v) => String(v).toLowerCase().includes(k)));
    }
    return list;
  }, [items, scene, actionFilter, keyword]);

  const counts = useMemo(() => {
    let blocks = 0, redacts = 0, observes = 0;
    for (const a of items) {
      const ac = a.action?.toLowerCase();
      if (ac === 'block') blocks += 1;
      else if (ac === 'redact') redacts += 1;
      else if (ac === 'observe') observes += 1;
    }
    return { total: items.length, blocks, redacts, observes };
  }, [items]);

  const applyChip = (apply: typeof QUICK_CHIPS[number]['apply']) => {
    if (apply.source) setSource(apply.source as AlertSource);
    if (apply.severity) setSeverity(apply.severity as Severity);
    if (apply.action) setActionFilter(apply.action);
    if (apply.rule_id) setRuleIdFilter(apply.rule_id);
  };

  const resetFilters = () => { setSource(''); setSeverity(''); setRuleIdFilter(''); setScene(''); setActionFilter(''); setKeyword(''); };

  return (
    <AdminLayout title={t('secplane.events.crumb.parent')}>
      <div className="secp-scope space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('secplane.events.crumb.parent')}</Link>
          <span>/</span>
          <span className="crumb-current">{t('secplane.events.crumb.current')}</span>
        </div>

        <div className="panel">
          <div className="flex items-start justify-between gap-6 mb-5">
            <div className="hero-block flex-1">
              <div className="h-eyebrow">{t('secplane.events.hero.eyebrow')}</div>
              <h2 className="h-title">{t('secplane.events.hero.title')}</h2>
              <p className="h-subtitle">{t('secplane.events.hero.subtitle')}</p>
            </div>
            <button type="button" className="btn-secondary btn-sm" onClick={load} disabled={loading}>
              {loading ? t('secplane.events.refreshing') : t('secplane.events.refresh')}
            </button>
          </div>
          <div className="grid grid-cols-4 gap-3">
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.events.stats.total')}</div>
              <div className="stat-card-value">{counts.total}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.events.stats.totalSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.events.stats.blocked')}</div>
              <div className="stat-card-value tone-red">{counts.blocks}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.events.stats.blockedSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.events.stats.redacted')}</div>
              <div className="stat-card-value tone-orange">{counts.redacts}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.events.stats.redactedSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.events.stats.observed')}</div>
              <div className="stat-card-value tone-slate">{counts.observes}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.events.stats.observedSub')}</div>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="flex items-center justify-between mb-3">
            <div>
              <div className="eyebrow">{t('secplane.events.filter.eyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.events.filter.title')}</h3>
            </div>
            <div className="text-xs muted-strong">
              {t('secplane.events.filter.totalFiltered', { total: items.length, filtered: filtered.length })}
              <span className="dot bg-green-500 ml-2" />
            </div>
          </div>
          <div className="grid grid-cols-3 gap-3 mb-3">
            <input type="text" className="input"
              placeholder={t('secplane.events.filter.searchPlaceholder')}
              value={keyword} onChange={(e) => setKeyword(e.target.value)} />
            <select className="input" value={source} onChange={(e) => setSource(e.target.value as AlertSource | '')}>
              {SOURCE_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
            <select className="input" value={severity} onChange={(e) => setSeverity(e.target.value as Severity | '')}>
              {SEVERITY_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
          </div>
          <div className="grid grid-cols-3 gap-3 mb-3">
            <select className="input" value={scene} onChange={(e) => setScene(e.target.value)}>
              {SCENE_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
            <select className="input" value={actionFilter} onChange={(e) => setActionFilter(e.target.value)}>
              {ACTION_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
            <input type="text" className="input"
              placeholder={t('secplane.events.ruleIdPlaceholder')}
              value={ruleIdFilter} onChange={(e) => setRuleIdFilter(e.target.value)} />
          </div>
          <div className="flex items-center justify-between mb-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs muted">{t('secplane.events.filter.quickFilterLabel')}</span>
              {QUICK_CHIPS.map((c) => (
                <button key={c.label} type="button" className="tag" onClick={() => applyChip(c.apply)}>{c.label}</button>
              ))}
            </div>
            <button type="button" className="btn-secondary btn-sm" onClick={resetFilters}>
              {t('secplane.events.filter.clearFilters')}
            </button>
          </div>

          {loading ? (
            <div className="muted text-sm py-6 text-center">{t('secplane.events.empty.loading')}</div>
          ) : filtered.length === 0 ? (
            <div className="muted text-sm py-6 text-center">{t('secplane.events.empty.noMatch')}</div>
          ) : (
            <table className="tbl">
              <thead>
                <tr>
                  <th>{t('secplane.events.table.time')}</th>
                  <th>{t('secplane.events.table.source')}</th>
                  <th>{t('secplane.events.table.rule')}</th>
                  <th>{t('secplane.events.table.subject')}</th>
                  <th>{t('secplane.events.table.evidence')}</th>
                  <th>{t('secplane.events.table.trace')}</th>
                  <th>{t('secplane.events.table.severity')}</th>
                  <th>{t('secplane.events.table.action')}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((a) => (
                  <tr key={a.id}>
                    <td className="muted text-xs whitespace-nowrap">{a.ts}</td>
                    <td><span className={`badge ${sourceBadgeTone(a.source)}`}>{a.source}</span></td>
                    <td>
                      <div className="text-sm">{a.rule_name || a.rule_id || '—'}</div>
                      {a.rule_id && a.rule_name && <div className="muted text-xs font-mono">{a.rule_id}</div>}
                    </td>
                    <td className="text-xs">{a.subject || a.agent_id || '—'}</td>
                    <td className="muted text-xs" style={{ maxWidth: 320, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{a.evidence || '—'}</td>
                    <td className="muted-strong text-xs font-mono">{a.trace_id ? a.trace_id.slice(0, 12) : '—'}</td>
                    <td><span className={`badge ${severityTone(a.severity)}`}>{a.severity}</span></td>
                    <td><span className={`badge ${actionTone(a.action)}`}>{a.action}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </AdminLayout>
  );
};

export default SecurityEventsPage;
