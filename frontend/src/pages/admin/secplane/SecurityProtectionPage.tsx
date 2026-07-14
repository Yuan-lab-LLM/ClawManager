import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../components/AdminLayout';
import { CATEGORIES } from '../protection/_data';
import LiveAegisConfigButton from '../../../components/protection/LiveAegisConfigButton';
import { secplaneService, type SecplaneAlert, type KillSwitchState } from '../../../services/secplaneService';
import { useI18n } from '../../../contexts/I18nContext';

// Helper: build source label map from i18n
const sourceLabelKeys: Record<string, string> = {
  aegis: 'secplane.protection.sourceLabel.aegis',
  secureclaw: 'secplane.protection.sourceLabel.secureclaw',
  ksecure: 'secplane.protection.sourceLabel.ksecure',
  kubearmor: 'secplane.protection.sourceLabel.kubearmor',
  gateway: 'secplane.protection.sourceLabel.gateway',
  platform: 'secplane.protection.sourceLabel.platform',
};

// Scene label keys
const sceneLabelKeys: Array<[string, string]> = [
  ['defense.requireHttps', 'secplane.protection.sceneLabel.outboundGovernance'],
  ['defense.outboundTrust', 'secplane.protection.sceneLabel.outboundGovernance'],
  ['outbound_trust', 'secplane.protection.sceneLabel.outboundGovernance'],
  ['require_https', 'secplane.protection.sceneLabel.outboundGovernance'],
  ['defense.exfiltrationGuard', 'secplane.protection.sceneLabel.outputSurface'],
  ['exfiltration_guard', 'secplane.protection.sceneLabel.outputSurface'],
  ['tool_result_scan', 'secplane.protection.sceneLabel.outputSurface'],
  ['trf.', 'secplane.protection.sceneLabel.outputSurface'],
  ['defense.selfProtection', 'secplane.protection.sceneLabel.assetProtection'],
  ['self_protection', 'secplane.protection.sceneLabel.assetProtection'],
  ['defense.toolCall', 'secplane.protection.sceneLabel.decisionSurface'],
  ['tool_call_guard', 'secplane.protection.sceneLabel.decisionSurface'],
  ['tic.', 'secplane.protection.sceneLabel.decisionSurface'],
  ['tcc.', 'secplane.protection.sceneLabel.decisionSurface'],
  ['user_risk_flag', 'secplane.protection.sceneLabel.inputSurface'],
  ['prompt_guard', 'secplane.protection.sceneLabel.inputSurface'],
  ['urf.', 'secplane.protection.sceneLabel.inputSurface'],
  ['secureclaw.', 'secplane.protection.sceneLabel.componentTrust'],
];

// ts → relative time
const relTime = (t: (key: string, vars?: Record<string, string | number>) => string, iso: string) => {
  const now = Date.now();
  const ts = new Date(iso).getTime();
  if (Number.isNaN(ts)) return iso;
  const sec = Math.max(0, Math.floor((now - ts) / 1000));
  if (sec < 30) return t('secplane.protection.relTime.justNow');
  if (sec < 60) return t('secplane.protection.relTime.secondsAgo', { count: sec });
  if (sec < 3600) return t('secplane.protection.relTime.minutesAgo', { count: Math.floor(sec / 60) });
  if (sec < 86400) return t('secplane.protection.relTime.hoursAgo', { count: Math.floor(sec / 3600) });
  return iso.slice(0, 16).replace('T', ' ');
};

// Module visual data — non-translatable (colors, SVG paths, etc.)
type Layer = 'runtime' | 'host' | 'audit' | 'control' | 'planned';
interface ModuleVisual {
  num: string;
  layer: Layer;
  cardBorder: string;
  cardBg: string;
  iconGradient: string;
  iconShadow: string;
  iconPath: string;
  arrowColor: string;
  footerNoteKey: string;
  layerLabelKey: string;
  layerTagClass: string;
  badgeClass: string;
}
const MODULE_VISUAL: Record<string, ModuleVisual> = {
  'cat-1': { num: '①', layer: 'runtime', cardBorder: '#f4b6b3', cardBg: 'linear-gradient(135deg,#fff,#fdeded)', iconGradient: 'linear-gradient(135deg,#ef4444,#991b1b)', iconShadow: '0 8px 20px -8px rgba(239,68,68,0.5)', iconPath: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z', arrowColor: '#ef4444', footerNoteKey: 'secplane.protection.moduleVisual.cat1.footerNote', layerLabelKey: 'secplane.protection.moduleVisual.cat1.layerLabel', layerTagClass: 'layer-tag-runtime', badgeClass: 'badge-red' },
  'cat-6': { num: '⑥', layer: 'host', cardBorder: '#a8d9d2', cardBg: 'linear-gradient(135deg,#fff,#e8f8f5)', iconGradient: 'linear-gradient(135deg,#0f766e,#115e59)', iconShadow: '0 8px 20px -8px rgba(15,118,110,0.4)', iconPath: 'M3 11v11h18V11M7 11V7a5 5 0 0110 0v4', arrowColor: '#0f766e', footerNoteKey: 'secplane.protection.moduleVisual.cat6.footerNote', layerLabelKey: 'secplane.protection.moduleVisual.cat6.layerLabel', layerTagClass: 'layer-tag-host', badgeClass: 'badge-blue' },
  'cat-4': { num: '②', layer: 'audit', cardBorder: '#d9c7f5', cardBg: 'linear-gradient(135deg,#fff,#f3edff)', iconGradient: 'linear-gradient(135deg,#7c3aed,#6b21a8)', iconShadow: '0 8px 20px -8px rgba(107,33,168,0.4)', iconPath: 'M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z', arrowColor: '#6b21a8', footerNoteKey: 'secplane.protection.moduleVisual.cat4.footerNote', layerLabelKey: 'secplane.protection.moduleVisual.cat4.layerLabel', layerTagClass: 'layer-tag-audit', badgeClass: 'badge-purple' },
  'cat-3': { num: '④', layer: 'control', cardBorder: '#c8d4e2', cardBg: 'linear-gradient(135deg,#fff,#f3f6fa)', iconGradient: 'linear-gradient(135deg,#64748b,#475569)', iconShadow: '0 8px 20px -8px rgba(71,85,105,0.35)', iconPath: 'M17 8h2a2 2 0 012 2v8a2 2 0 01-2 2h-2m-10 0H5a2 2 0 01-2-2v-8a2 2 0 012-2h2m3 4h4m-8 4h8M9 4h6a1 1 0 011 1v3H8V5a1 1 0 011-1z', arrowColor: '#64748b', footerNoteKey: 'secplane.protection.moduleVisual.cat3.footerNote', layerLabelKey: 'secplane.protection.moduleVisual.cat3.layerLabel', layerTagClass: 'layer-tag-control', badgeClass: 'badge-blue' },
  'cat-2': { num: '③', layer: 'control', cardBorder: '#b8d8f4', cardBg: 'linear-gradient(135deg,#fff,#e8f3fd)', iconGradient: 'linear-gradient(135deg,#2563eb,#1d4ed8)', iconShadow: '0 8px 20px -8px rgba(37,99,235,0.4)', iconPath: 'M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z', arrowColor: '#1d4ed8', footerNoteKey: 'secplane.protection.moduleVisual.cat2.footerNote', layerLabelKey: 'secplane.protection.moduleVisual.cat2.layerLabel', layerTagClass: 'layer-tag-control', badgeClass: 'badge-blue' },
  'cat-7': { num: '⑦', layer: 'control', cardBorder: '#eadfd8', cardBg: 'linear-gradient(135deg,#fff,#fdf6f1)', iconGradient: 'linear-gradient(135deg,#92400e,#78350f)', iconShadow: '0 8px 20px -8px rgba(146,64,14,0.4)', iconPath: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4', arrowColor: '#78350f', footerNoteKey: 'secplane.protection.moduleVisual.cat7.footerNote', layerLabelKey: 'secplane.protection.moduleVisual.cat7.layerLabel', layerTagClass: 'layer-tag-control', badgeClass: 'badge-orange' },
  'cat-5': { num: '⑧', layer: 'control', cardBorder: '#f4cba0', cardBg: 'linear-gradient(135deg,#fff,#fff3e1)', iconGradient: 'linear-gradient(135deg,#d97706,#b45309)', iconShadow: '0 8px 20px -8px rgba(217,119,6,0.4)', iconPath: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01', arrowColor: '#b45309', footerNoteKey: 'secplane.protection.moduleVisual.cat5.footerNote', layerLabelKey: 'secplane.protection.moduleVisual.cat5.layerLabel', layerTagClass: 'layer-tag-control', badgeClass: 'badge-orange' },
};

// Bubble label keys per cat
const BUBBLE_LABEL_KEYS: Record<string, string[]> = {
  'cat-1': ['cat1_0', 'cat1_1', 'cat1_2', 'cat1_3', 'cat1_4', 'cat1_5'],
  'cat-6': ['cat6_0', 'cat6_1'],
  'cat-4': ['cat4_0'],
  'cat-3': ['cat3_0', 'cat3_1'],
  'cat-2': ['cat2_0'],
  'cat-7': ['cat7_0'],
  'cat-5': ['cat5_0', 'cat5_1'],
};

// Ring short label keys
const RING_SHORT_LABEL_KEYS: Record<string, string> = {
  'cat-6': 'secplane.protection.ringShortLabel.cat6',
  'cat-3': 'secplane.protection.ringShortLabel.cat3',
};

// Ring bubble short keys
const RING_BUBBLE_KEYS: Record<string, string[]> = {
  'cat-1': ['cat1_0', 'cat1_1', 'cat1_2', 'cat1_3', 'cat1_4', 'cat1_5'],
  'cat-6': ['cat6_0', 'cat6_1'],
  'cat-3': ['cat3_0', 'cat3_1'],
  'cat-4': ['cat4_0'],
  'cat-2': ['cat2_0'],
  'cat-7': ['cat7_0'],
  'cat-5': ['cat5_0', 'cat5_1'],
};

// Layer subtitle keys
const LAYER_SUBTITLE_KEYS: Record<string, string> = {
  'cat-1': 'secplane.protection.layerSubtitle.cat1',
  'cat-6': 'secplane.protection.layerSubtitle.cat6',
  'cat-4': 'secplane.protection.layerSubtitle.cat4',
  'cat-3': 'secplane.protection.layerSubtitle.cat3',
};

// Source badge tone (based on label content — remains language-agnostic)
const sourceBadgeTone = (src: string, t: (key: string, vars?: Record<string, string | number>) => string) => {
  const label = t(sourceLabelKeys[src] || src);
  if (label === t('secplane.protection.sourceLabel.aegis')) return 'badge-red';
  if (label === t('secplane.protection.sourceLabel.ksecure')) return 'badge-blue';
  if (label === t('secplane.protection.sourceLabel.gateway')) return 'badge-orange';
  return 'badge-purple';
};
const severityTone = (sev: string) => (sev === 'high' ? 'red' : sev === 'medium' ? 'orange' : 'slate');

// Ring view card
interface RingCardProps {
  catId: string;
  style: React.CSSProperties;
  bubbleKeys: string[];
  bubbleOpacity?: number;
  t: (key: string, vars?: Record<string, string | number>) => string;
}
const RingCard: React.FC<RingCardProps> = ({ catId, style, bubbleKeys, bubbleOpacity, t }) => {
  const vis = MODULE_VISUAL[catId];
  const cat = CATEGORIES.find((c) => c.id === catId);
  if (!cat || !vis) return null;
  const planned = vis.layer === 'planned';
  const label = RING_SHORT_LABEL_KEYS[catId] ? t(RING_SHORT_LABEL_KEYS[catId]) : (cat.labelKey ? t(cat.labelKey) : cat.label);
  return (
    <div style={{ position: 'absolute', background: '#fff', borderRadius: 16, border: `1px solid ${vis.cardBorder}`, padding: '10px 12px', boxShadow: '0 8px 24px -12px rgba(0,0,0,0.2)', ...style }}>
      <div className="flex items-center gap-2 mb-1">
        <div style={{ width: 28, height: 28, borderRadius: 8, background: vis.iconGradient, display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5">
            <path strokeLinecap="round" strokeLinejoin="round" d={vis.iconPath} />
          </svg>
        </div>
        <div>
          <div style={{ fontSize: '0.75rem', fontWeight: 700, color: '#171212' }}>{label}</div>
          <div style={{ fontSize: '0.6rem', color: planned ? '#94a3b8' : vis.arrowColor, fontWeight: 600 }}>{t(vis.layerLabelKey)}</div>
        </div>
      </div>
      <div className="flex flex-wrap gap-1">
        {bubbleKeys.map((bk, i) => (
          <span key={i} className="scenario-bubble" style={{ fontSize: '0.6rem', padding: '2px 6px', ...(bubbleOpacity ? { opacity: bubbleOpacity } : {}) }}>{t(`secplane.protection.ringBubble.${bk}`)}</span>
        ))}
      </div>
    </div>
  );
};

// Layer view card
const LayerCard: React.FC<{ catId: string; sceneSubtitle?: string; t: (key: string, vars?: Record<string, string | number>) => string }> = ({ catId, sceneSubtitle, t }) => {
  const cat = CATEGORIES.find((c) => c.id === catId);
  const vis = MODULE_VISUAL[catId];
  if (!cat || !vis) return null;
  const planned = vis.layer === 'planned';
  const sceneCount = BUBBLE_LABEL_KEYS[catId]?.length ?? 0;
  const bubbles = BUBBLE_LABEL_KEYS[catId]?.map((bk) => t(`secplane.protection.bubbleLabels.${bk}`)) ?? [];
  const inner = (
    <div className="panel-tight flex items-start gap-3" style={{ borderLeft: `4px solid ${vis.arrowColor}`, cursor: planned ? 'not-allowed' : 'pointer', opacity: planned ? 0.75 : 1 }}>
      <div style={{ width: 40, height: 40, borderRadius: 10, background: vis.iconGradient, display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2">
          <path strokeLinecap="round" strokeLinejoin="round" d={vis.iconPath} />
        </svg>
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1 flex-wrap">
          <div style={{ fontSize: '0.875rem', fontWeight: 700, color: '#171212' }}>{cat.labelKey ? t(cat.labelKey) : cat.label}</div>
          {planned ? (
            <span className="badge badge-slate" style={{ fontSize: '0.5625rem', padding: '2px 6px' }}>{t('secplane.protection.layerCard.planned')}</span>
          ) : (
            <>
              <span className={`layer-tag ${vis.layerTagClass}`}>{t(vis.layerLabelKey)}</span>
              <span className={`badge ${vis.badgeClass}`} style={{ fontSize: '0.5625rem', padding: '2px 6px' }}>{t('secplane.protection.layerCard.sceneCount', { count: sceneCount })}</span>
            </>
          )}
        </div>
        <div className="text-xs muted mb-2">{sceneSubtitle ?? (planned ? t('secplane.protection.layerSubtitle.cat3') : t('secplane.protection.layerCard.sceneCount', { count: sceneCount }))}</div>
        <div className="flex flex-wrap gap-2">
          {bubbles.map((b) => (
            <span key={b} className="scenario-bubble" style={planned ? { opacity: 0.5 } : undefined}>{b}</span>
          ))}
        </div>
      </div>
      <div style={{ color: planned ? '#94a3b8' : vis.arrowColor, fontWeight: 600, fontSize: '0.8125rem' }}>{planned ? t('secplane.protection.layerCard.plannedLink') : t('secplane.protection.layerCard.viewLink')}</div>
    </div>
  );
  if (planned) {
    return inner;
  }
  return <Link to={cat.path} style={{ textDecoration: 'none', color: 'inherit', display: 'block' }}>{inner}</Link>;
};

// Layer section
const LayerSection: React.FC<{ title: string; dotColor: string; rows: Array<string[]>; t: (key: string, vars?: Record<string, string | number>) => string }> = ({ title, dotColor, rows, t }) => (
  <div>
    <div className="zone-divider">
      <div className="zone-divider-line" />
      <div className="zone-divider-label">
        <span style={{ display: 'inline-block', width: 8, height: 8, borderRadius: '50%', background: dotColor, marginRight: 6 }} />
        {title}
      </div>
      <div className="zone-divider-line" />
    </div>
    {rows.map((row, i) => {
      const colsClass = row.length === 1 ? 'grid-cols-1' : row.length === 2 ? 'grid-cols-2' : 'grid-cols-3';
      return (
        <div key={i} className={`grid ${colsClass} gap-3`}>
          {row.map((catId) => (
            <LayerCard key={catId} catId={catId} sceneSubtitle={LAYER_SUBTITLE_KEYS[catId] ? t(LAYER_SUBTITLE_KEYS[catId]) : undefined} t={t} />
          ))}
        </div>
      );
    })}
  </div>
);

const SecurityProtectionPage: React.FC = () => {
  const { t } = useI18n();
  const [viewMode, setViewMode] = useState<'layer' | 'ring'>('layer');

  // Alert data
  const [allAlerts, setAllAlerts] = useState<SecplaneAlert[]>([]);
  const [alertsLoading, setAlertsLoading] = useState(false);
  const [alertsError, setAlertsError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      setAlertsLoading(true);
      try {
        const list = await secplaneService.listAlerts({ limit: 500 });
        if (!cancelled) {
          setAllAlerts(list);
          setAlertsError(null);
        }
      } catch (e) {
        if (!cancelled) {
          const err = e as { message?: string };
          setAlertsError(err.message ?? t('secplane.protection.eventTable.loadFail'));
        }
      } finally {
        if (!cancelled) setAlertsLoading(false);
      }
    };
    load();
    const timer = window.setInterval(load, 30_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [t]);
  const recentAlerts = allAlerts.slice(0, 10);

  // Stats
  const now = Date.now();
  const todayStart = new Date(); todayStart.setHours(0, 0, 0, 0);
  const todayStartMs = todayStart.getTime();
  const last24hMs = now - 24 * 3600 * 1000;
  const isBlock = (a: SecplaneAlert) =>
    /block|deny/i.test(a.action || '') || /enforce|blocked/i.test(a.action || '');
  const todayHits = allAlerts.filter((a) => new Date(a.ts).getTime() >= todayStartMs).length;
  const high24h = allAlerts.filter((a) => new Date(a.ts).getTime() >= last24hMs && a.severity === 'high').length;
  const block24h = allAlerts.filter((a) => new Date(a.ts).getTime() >= last24hMs && isBlock(a)).length;
  const distinctAgents24h = new Set(
    allAlerts
      .filter((a) => new Date(a.ts).getTime() >= last24hMs && a.agent_id)
      .map((a) => a.agent_id as string),
  ).size;

  // Kill switch
  const [killSwitch, setKillSwitch] = useState<KillSwitchState | null>(null);
  const [killBusy, setKillBusy] = useState(false);
  const loadKillSwitch = async () => {
    try {
      setKillSwitch(await secplaneService.getKillSwitch());
    } catch {
      // ignore
    }
  };
  useEffect(() => {
    loadKillSwitch();
    const timer = window.setInterval(loadKillSwitch, 15_000);
    return () => window.clearInterval(timer);
  }, []);
  const enableKillSwitch = async () => {
    const reason = window.prompt(t('secplane.protection.killSwitch.enableReasonPrompt'), '');
    if (reason === null) return;
    if (!window.confirm(t('secplane.protection.killSwitch.enableConfirm'))) return;
    setKillBusy(true);
    try {
      const res = await secplaneService.enableKillSwitch(reason);
      setKillSwitch(res.state);
      const tc = res.dispatch?.target_count ?? 0;
      window.alert(t('secplane.protection.killSwitch.enableSuccess', { count: tc }));
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      window.alert(t('secplane.protection.killSwitch.enableFail') + (err.response?.data?.error ?? err.message ?? t('secplane.protection.killSwitch.unknownError')));
    } finally {
      setKillBusy(false);
    }
  };
  const disableKillSwitch = async () => {
    if (!window.confirm(t('secplane.protection.killSwitch.disableConfirm'))) return;
    setKillBusy(true);
    try {
      const res = await secplaneService.disableKillSwitch();
      setKillSwitch(res.state);
      const tc = res.dispatch?.target_count ?? 0;
      window.alert(t('secplane.protection.killSwitch.disableSuccess', { count: tc }));
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      window.alert(t('secplane.protection.killSwitch.disableFail') + (err.response?.data?.error ?? err.message ?? t('secplane.protection.killSwitch.unknownError')));
    } finally {
      setKillBusy(false);
    }
  };
  const killActive = killSwitch?.enabled === 1;

  const exportReport = () => {
    if (allAlerts.length === 0) return;
    const text = allAlerts.map((a) => JSON.stringify(a)).join('\n');
    const blob = new Blob([text], { type: 'application/jsonl' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `secplane-alerts-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.jsonl`;
    link.click();
    URL.revokeObjectURL(url);
  };

  // Scene resolver
  const sceneOf = (ruleID?: string) => {
    if (!ruleID) return '—';
    for (const [pfx, key] of sceneLabelKeys) {
      if (ruleID.startsWith(pfx)) return t(key);
    }
    return ruleID;
  };

  // Source label resolver
  const getSourceLabel = (src: string) => t(sourceLabelKeys[src] || src);

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        {killActive && (
          <div
            className="panel"
            style={{
              borderColor: '#dc2626',
              background: 'linear-gradient(90deg,#fef2f2,#fee2e2)',
              borderWidth: 2,
            }}
          >
            <div className="flex items-start gap-3">
              <svg width="28" height="28" fill="none" viewBox="0 0 24 24" stroke="#dc2626" strokeWidth="2.5" style={{ marginTop: 2 }}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              <div className="flex-1">
                <div className="font-bold text-base" style={{ color: '#991b1b' }}>{t('secplane.protection.killSwitch.bannerTitle')}</div>
                <div className="text-sm mt-1" style={{ color: '#7f1d1d' }}>
                  {t('secplane.protection.killSwitch.reason')}<span className="font-semibold">{killSwitch?.reason || t('secplane.protection.killSwitch.noReason')}</span>
                  <span className="muted ml-3">
                    {t('secplane.protection.killSwitch.enabledBy')}{killSwitch?.set_by || t('secplane.protection.killSwitch.noReason')} · {t('secplane.protection.killSwitch.enabledAt')}{killSwitch?.set_at?.replace('T', ' ').slice(0, 19) ?? '-'}
                  </span>
                </div>
                <div className="text-xs muted mt-1">{t('secplane.protection.killSwitch.podNote')}</div>
              </div>
              <button className="btn-secondary btn-sm shrink-0" disabled={killBusy} onClick={disableKillSwitch}>
                {killBusy ? t('secplane.protection.killSwitch.processing') : t('secplane.protection.killSwitch.disable')}
              </button>
            </div>
          </div>
        )}
        {/* Hero */}
        <div className="panel">
          <div className="flex items-start justify-between gap-6 mb-5">
            <div className="hero-block flex-1">
              <div className="h-eyebrow">{t('secplane.protection.hero.eyebrow')}</div>
              <h2 className="h-title">{t('secplane.protection.hero.title')}</h2>
              <p className="h-subtitle">
                {t('secplane.protection.hero.subtitle')}
              </p>
            </div>
            <div className="flex flex-col gap-2 shrink-0">
              <LiveAegisConfigButton />
              <button
                className="btn-secondary"
                onClick={exportReport}
                disabled={allAlerts.length === 0}
                title={allAlerts.length === 0 ? t('secplane.protection.export.noData') : t('secplane.protection.export.hasData', { count: allAlerts.length })}
              >
                <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 10v6m0 0l-3-3m3 3l3-3m2 8H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                </svg>
                {t('secplane.protection.export.button')}
              </button>
              {killActive ? (
                <button className="btn-secondary" disabled={killBusy} onClick={disableKillSwitch} title={t('secplane.protection.killSwitch.buttonDisableTitle')}>
                  <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M5 13l4 4L19 7" />
                  </svg>
                  {killBusy ? t('secplane.protection.killSwitch.processing') : t('secplane.protection.killSwitch.disable')}
                </button>
              ) : (
                <button className="btn-danger" disabled={killBusy} onClick={enableKillSwitch} title={t('secplane.protection.killSwitch.buttonEnableTitle')}>
                  <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                  </svg>
                  {killBusy ? t('secplane.protection.killSwitch.buttonDispatching') : t('secplane.protection.killSwitch.buttonEnable')}
                </button>
              )}
            </div>
          </div>
          <div className="grid grid-cols-4 gap-3">
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.protection.stats.todayHits')}</div>
              <div className="stat-card-value">{todayHits}</div>
              <div className="stat-card-sub muted">{t('secplane.protection.stats.todayHitsSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.protection.stats.high24h')}</div>
              <div className={`stat-card-value ${high24h > 0 ? 'tone-red' : 'tone-green'}`}>{high24h}</div>
              <div className="stat-card-sub muted">{t('secplane.protection.stats.high24hSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.protection.stats.block24h')}</div>
              <div className={`stat-card-value ${block24h > 0 ? 'tone-orange' : 'tone-green'}`}>{block24h}</div>
              <div className="stat-card-sub muted">{t('secplane.protection.stats.block24hSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.protection.stats.agents24h')}</div>
              <div className="stat-card-value">{distinctAgents24h}</div>
              <div className="stat-card-sub muted">{t('secplane.protection.stats.agents24hSub')}</div>
            </div>
          </div>
        </div>

        {/* KSecure banner */}
        <div className="ksecure-banner">
          <div className="ksecure-banner-left">
            <div className="ksecure-banner-title">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
              </svg>
              {t('secplane.protection.banner.title')}
            </div>
            <div className="ksecure-banner-sub">{t('secplane.protection.banner.subtitle')}</div>
            <div className="ksecure-banner-stats">
              <div className="ksecure-stat"><div className="ksecure-stat-num">7</div><div className="ksecure-stat-label">{t('secplane.protection.banner.riskSurfaces')}</div></div>
              <div className="ksecure-stat"><div className="ksecure-stat-num">15</div><div className="ksecure-stat-label">{t('secplane.protection.banner.scenarios')}</div></div>
              <div className="ksecure-stat"><div className="ksecure-stat-num">4</div><div className="ksecure-stat-label">{t('secplane.protection.banner.layers')}</div></div>
            </div>
          </div>
          <div className="ksecure-banner-path">
            <div className="ksecure-path-step"><div className="ksecure-path-dot" style={{ background: '#ef4444' }} /><span>{t('secplane.protection.bannerPath.runtime')}</span></div>
            <div className="ksecure-path-arrow">↓</div>
            <div className="ksecure-path-step"><div className="ksecure-path-dot" style={{ background: '#6b21a8' }} /><span>{t('secplane.protection.bannerPath.data')}</span></div>
            <div className="ksecure-path-arrow">↓</div>
            <div className="ksecure-path-step"><div className="ksecure-path-dot" style={{ background: '#1d4ed8' }} /><span>{t('secplane.protection.bannerPath.identity')}</span></div>
            <div className="ksecure-path-arrow">↔</div>
            <div className="ksecure-path-step"><div className="ksecure-path-dot" style={{ background: '#b45309' }} /><span>{t('secplane.protection.bannerPath.governance')}</span></div>
          </div>
        </div>

        {/* 7 Risk Surfaces */}
        <div className="panel" style={{ padding: 24 }}>
          <div className="flex items-center justify-between mb-5">
            <div>
              <div className="eyebrow">{t('secplane.protection.section.eyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.protection.section.title')}</h3>
            </div>
            <div className="layer-legend">
              <div className="layer-dot"><div className="layer-dot-inner" style={{ background: '#ef4444' }} /><span>{t('secplane.protection.legend.runtime')}</span></div>
              <div className="layer-dot"><div className="layer-dot-inner" style={{ background: '#1d4ed8' }} /><span>{t('secplane.protection.legend.host')}</span></div>
              <div className="layer-dot"><div className="layer-dot-inner" style={{ background: '#6b21a8' }} /><span>{t('secplane.protection.legend.audit')}</span></div>
              <div className="layer-dot"><div className="layer-dot-inner" style={{ background: '#b45309' }} /><span>{t('secplane.protection.legend.control')}</span></div>
            </div>
          </div>

          <div className="flex items-center gap-2 mb-5">
            <button type="button" className={`sec-tab ${viewMode === 'layer' ? 'active' : ''}`} onClick={() => setViewMode('layer')}>{t('secplane.protection.views.layer')}</button>
            <button type="button" className={`sec-tab ${viewMode === 'ring' ? 'active' : ''}`} onClick={() => setViewMode('ring')}>{t('secplane.protection.views.ring')}</button>
          </div>

          {viewMode === 'ring' && (
            <div className="flex flex-col items-center">
              <div className="flex gap-5 mb-6 flex-wrap justify-center">
                <div className="flex items-center gap-2 text-xs"><div style={{ width: 10, height: 10, borderRadius: '50%', background: '#ef4444' }} />{t('secplane.protection.legend.runtime')}</div>
                <div className="flex items-center gap-2 text-xs"><div style={{ width: 10, height: 10, borderRadius: '50%', background: '#1d4ed8' }} />{t('secplane.protection.legend.host')}</div>
                <div className="flex items-center gap-2 text-xs"><div style={{ width: 10, height: 10, borderRadius: '50%', background: '#6b21a8' }} />{t('secplane.protection.legend.audit')}</div>
                <div className="flex items-center gap-2 text-xs"><div style={{ width: 10, height: 10, borderRadius: '50%', background: '#b45309' }} />{t('secplane.protection.legend.control')}</div>
              </div>
              <div className="relative flex items-center justify-center" style={{ width: 560, height: 560 }}>
                <div style={{ position: 'absolute', width: 540, height: 540, top: '50%', left: '50%', transform: 'translate(-50%,-50%)', borderRadius: '50%', border: '2px solid #ef4444', opacity: 0.25 }} />
                <div style={{ position: 'absolute', width: 390, height: 390, top: '50%', left: '50%', transform: 'translate(-50%,-50%)', borderRadius: '50%', border: '2px solid #1d4ed8', opacity: 0.25 }} />
                <div style={{ position: 'absolute', width: 260, height: 260, top: '50%', left: '50%', transform: 'translate(-50%,-50%)', borderRadius: '50%', border: '2px solid #6b21a8', opacity: 0.25 }} />
                <div style={{ position: 'absolute', width: 140, height: 140, top: '50%', left: '50%', transform: 'translate(-50%,-50%)', borderRadius: '50%', border: '2px solid #b45309', opacity: 0.25 }} />

                <div style={{ position: 'absolute', top: '50%', left: '50%', transform: 'translate(-50%,-50%)', width: 100, height: 100, borderRadius: '50%', background: 'linear-gradient(135deg,#ef6b4a,#dc2626)', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', color: '#fff', boxShadow: '0 12px 32px -12px rgba(220,38,38,0.5)', zIndex: 2 }}>
                  <div style={{ fontSize: '2.25rem', fontWeight: 800, lineHeight: 1 }}>7</div>
                  <div style={{ fontSize: '0.5625rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.1em', opacity: 0.9, whiteSpace: 'pre-line' }}>{t('secplane.protection.ringCenter.riskSurfaces')}</div>
                </div>

                <RingCard catId="cat-1" t={t} style={{ top: 8, left: '50%', transform: 'translateX(-50%)', width: 180 }} bubbleKeys={RING_BUBBLE_KEYS['cat-1']} />
                <RingCard catId="cat-6" t={t} style={{ bottom: 70, right: 38, width: 170 }} bubbleKeys={RING_BUBBLE_KEYS['cat-6']} />
                <RingCard catId="cat-3" t={t} style={{ top: '50%', right: 18, transform: 'translateY(-50%)', width: 150 }} bubbleKeys={RING_BUBBLE_KEYS['cat-3']} />
                <RingCard catId="cat-4" t={t} style={{ top: '50%', left: 18, transform: 'translateY(-50%)', width: 165 }} bubbleKeys={RING_BUBBLE_KEYS['cat-4']} />
                <RingCard catId="cat-2" t={t} style={{ top: 115, left: 48, width: 155 }} bubbleKeys={RING_BUBBLE_KEYS['cat-2']} />
                <RingCard catId="cat-7" t={t} style={{ top: 115, right: 48, width: 155 }} bubbleKeys={RING_BUBBLE_KEYS['cat-7']} />
                <RingCard catId="cat-5" t={t} style={{ bottom: 115, left: '50%', transform: 'translateX(-50%)', width: 165 }} bubbleKeys={RING_BUBBLE_KEYS['cat-5']} />
              </div>

              <div className="flex flex-col gap-2 mt-4" style={{ width: 560 }}>
                <div className="flex items-center gap-3 p-3 rounded-xl" style={{ border: '1px solid #f4b6b3', background: '#fdeded' }}>
                  <div style={{ width: 12, height: 12, borderRadius: '50%', background: '#ef4444', flexShrink: 0 }} />
                  <div style={{ fontSize: '0.8125rem', fontWeight: 700, color: '#b42318' }}>{t('secplane.protection.ringLegend.runtimeTitle')}</div>
                  <div style={{ fontSize: '0.75rem', color: '#6f6661' }}>{t('secplane.protection.ringLegend.runtimeDesc')}</div>
                </div>
                <div className="flex items-center gap-3 p-3 rounded-xl" style={{ border: '1px solid #b8d4f4', background: '#e8f3fd' }}>
                  <div style={{ width: 12, height: 12, borderRadius: '50%', background: '#1d4ed8', flexShrink: 0 }} />
                  <div style={{ fontSize: '0.8125rem', fontWeight: 700, color: '#1d4ed8' }}>{t('secplane.protection.ringLegend.hostTitle')}</div>
                  <div style={{ fontSize: '0.75rem', color: '#6f6661' }}>{t('secplane.protection.ringLegend.hostDesc')}</div>
                </div>
                <div className="flex items-center gap-3 p-3 rounded-xl" style={{ border: '1px solid #d9c7f5', background: '#f3edff' }}>
                  <div style={{ width: 12, height: 12, borderRadius: '50%', background: '#6b21a8', flexShrink: 0 }} />
                  <div style={{ fontSize: '0.8125rem', fontWeight: 700, color: '#6b21a8' }}>{t('secplane.protection.ringLegend.auditTitle')}</div>
                  <div style={{ fontSize: '0.75rem', color: '#6f6661' }}>{t('secplane.protection.ringLegend.auditDesc')}</div>
                </div>
                <div className="flex items-center gap-3 p-3 rounded-xl" style={{ border: '1px solid #f4cba0', background: '#fff3e1' }}>
                  <div style={{ width: 12, height: 12, borderRadius: '50%', background: '#b45309', flexShrink: 0 }} />
                  <div style={{ fontSize: '0.8125rem', fontWeight: 700, color: '#b45309' }}>{t('secplane.protection.ringLegend.controlTitle')}</div>
                  <div style={{ fontSize: '0.75rem', color: '#6f6661' }}>{t('secplane.protection.ringLegend.controlDesc')}</div>
                </div>
              </div>
            </div>
          )}

          {viewMode === 'layer' && (
            <div className="space-y-5">
              <LayerSection title={t('secplane.protection.layerSection.runtime')} dotColor="#ef4444" rows={[['cat-1']]} t={t} />
              <LayerSection title={t('secplane.protection.layerSection.host')} dotColor="#1d4ed8" rows={[['cat-6']]} t={t} />
              <LayerSection title={t('secplane.protection.layerSection.audit')} dotColor="#6b21a8" rows={[['cat-4']]} t={t} />
              <LayerSection title={t('secplane.protection.layerSection.control')} dotColor="#b45309" rows={[['cat-2', 'cat-7', 'cat-5', 'cat-3']]} t={t} />
            </div>
          )}
        </div>

        {/* Event table */}
        <div className="panel">
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className="eyebrow">{t('secplane.protection.eventTable.eyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.protection.eventTable.title')}</h3>
            </div>
            <Link to="/admin/secplane/events" className="btn-secondary btn-sm" style={{ textDecoration: 'none' }}>
              {t('secplane.protection.eventTable.fullLink')}
            </Link>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 110 }}>{t('secplane.protection.eventTable.time')}</th>
                <th style={{ width: 80 }}>{t('secplane.protection.eventTable.source')}</th>
                <th style={{ width: 130 }}>{t('secplane.protection.eventTable.scene')}</th>
                <th>{t('secplane.protection.eventTable.event')}</th>
                <th style={{ width: 220 }}>{t('secplane.protection.eventTable.target')}</th>
                <th style={{ width: 90 }}>{t('secplane.protection.eventTable.severity')}</th>
              </tr>
            </thead>
            <tbody>
              {alertsLoading && recentAlerts.length === 0 && (
                <tr>
                  <td colSpan={6} className="muted text-sm py-4 text-center">{t('secplane.protection.eventTable.loading')}</td>
                </tr>
              )}
              {alertsError && !alertsLoading && (
                <tr>
                  <td colSpan={6} className="text-sm py-4 text-center" style={{ color: '#b42318' }}>
                    {alertsError}
                  </td>
                </tr>
              )}
              {!alertsLoading && !alertsError && recentAlerts.length === 0 && (
                <tr>
                  <td colSpan={6} className="muted text-sm py-4 text-center">
                    {t('secplane.protection.eventTable.noAlerts')}
                  </td>
                </tr>
              )}
              {recentAlerts.map((a) => {
                const target = a.agent_id || a.subject || '—';
                const event = a.rule_name || a.evidence || a.rule_id || t('secplane.protection.eventTable.unnamedEvent');
                return (
                  <tr key={a.id}>
                    <td>
                      <span className="muted-strong text-xs" title={a.ts}>{relTime(t, a.ts)}</span>
                    </td>
                    <td>
                      <span className={`badge ${sourceBadgeTone(a.source, t)}`}>{getSourceLabel(a.source)}</span>
                    </td>
                    <td>
                      <span className="text-xs font-medium text-[#171212]">{sceneOf(a.rule_id)}</span>
                    </td>
                    <td>
                      <span className="text-sm text-[#171212]" title={a.evidence ?? ''}>{event}</span>
                    </td>
                    <td>
                      <span className="font-mono text-xs">{target}</span>
                    </td>
                    <td>
                      <span className={`badge badge-${severityTone(a.severity)}`}>{a.severity}</span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </AdminLayout>
  );
};

export default SecurityProtectionPage;
