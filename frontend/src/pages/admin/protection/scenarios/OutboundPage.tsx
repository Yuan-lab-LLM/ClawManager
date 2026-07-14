import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import ApplyDispatchButton from '../../../../components/secplane/ApplyDispatchButton';
import { useInstanceHealth } from '../../secplane/runtime/useInstanceHealth';
import { useSurfaceBackend } from '../../secplane/runtime/useSurfaceBackend';
import {
  secplaneService,
  type OutboundTrustedEndpoint,
} from '../../../../services/secplaneService';
import { useI18n } from '../../../../contexts/I18nContext';

// 出站治理 (scenario h) — 对齐 KSecForAIDemo/scenario-h-outbound.html
// 接 backend：require-https defense_toggle + "保存并应用" → dispatchAegisApply

const ALERT_PREFIXES = ['defense.requireHttps', 'defense.exfiltrationGuard', 'defense.outboundTrust'];

const OutboundPage: React.FC = () => {
  const { t } = useI18n();
  const o = 'secplane.protection.outbound';
  const { alerts, dispatching, dispatchMsg, modeOf, setMode, dispatchApply } = useSurfaceBackend(ALERT_PREFIXES);
  const { instances, healthy } = useInstanceHealth();
  const httpsMode = modeOf('defense.requireHttps', 'enforce');
  const httpsHits = alerts.filter((a) => a.rule_id?.startsWith('defense.requireHttps')).length;
  const trustMode = modeOf('defense.outboundTrust', 'enforce');
  const trustHits = alerts.filter((a) => a.rule_id?.startsWith('defense.outboundTrust')).length;

  // --- 受信端点 CRUD ---
  const [trusted, setTrusted] = useState<OutboundTrustedEndpoint[]>([]);
  const [trustedLoading, setTrustedLoading] = useState(false);
  const [trustedError, setTrustedError] = useState<string | null>(null);
  const [newDomain, setNewDomain] = useState('');
  const [newFingerprint, setNewFingerprint] = useState('');
  const [newLabel, setNewLabel] = useState('');

  const loadTrusted = useCallback(async () => {
    setTrustedLoading(true);
    setTrustedError(null);
    try {
      const list = await secplaneService.listOutboundTrusted();
      setTrusted(list);
    } catch (e) {
      const err = e as { message?: string };
      setTrustedError(err.message ?? t(`${o}.error.loadFail`));
    } finally {
      setTrustedLoading(false);
    }
  }, []);

  useEffect(() => { loadTrusted(); }, [loadTrusted]);

  const addTrusted = async () => {
    if (!newDomain.trim()) return;
    setTrustedError(null);
    try {
      await secplaneService.createOutboundTrusted({
        domain_pattern: newDomain.trim(),
        fingerprint_sha256: newFingerprint.trim() || undefined,
        label: newLabel.trim() || undefined,
      });
      setNewDomain('');
      setNewFingerprint('');
      setNewLabel('');
      loadTrusted();
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      setTrustedError(err.response?.data?.error ?? err.message ?? t(`${o}.error.saveFail`));
    }
  };

  const removeTrusted = async (id: number) => {
    if (!window.confirm(t(`${o}.trust.confirmDelete`, { id }))) return;
    try {
      await secplaneService.deleteOutboundTrusted(id);
      loadTrusted();
    } catch (e) {
      const err = e as { message?: string };
      setTrustedError(err.message ?? t(`${o}.error.deleteFail`));
    }
  };

  // 探测指纹：填表时点"探测"，后端 TLS 握手把摘要回填
  const [probing, setProbing] = useState(false);
  const [probeMsg, setProbeMsg] = useState<string | null>(null);
  const probeFingerprint = async () => {
    const host = newDomain.trim();
    if (!host) return;
    if (host.includes('*') || host.includes('?')) {
      setProbeMsg(t(`${o}.probe.wildcardError`));
      return;
    }
    setProbing(true);
    setProbeMsg(null);
    try {
      const r = await secplaneService.probeOutboundTrusted(host);
      setNewFingerprint(r.fingerprint_sha256);
      setProbeMsg(t(`${o}.probe.successSubject`, { subject: r.subject_cn || '-', issuer: r.issuer || '-', expires: (r.not_after || '').slice(0, 10) }));
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      setProbeMsg(t(`${o}.probe.failError`, { error: err.response?.data?.error ?? err.message ?? t(`${o}.error.unknownError`) }));
    } finally {
      setProbing(false);
    }
  };

  // 重新探测已存在条目：drift=true 时基线被自动刷为最新指纹
  const [reprobingId, setReprobingId] = useState<number | null>(null);
  const reprobeOne = async (id: number) => {
    setReprobingId(id);
    setTrustedError(null);
    try {
      const r = await secplaneService.reprobeOutboundTrusted(id);
      if (r.drift) {
        window.alert(
          t(`${o}.probe.driftAlert`, {
            domain: r.endpoint.domain_pattern,
            old: r.previous_fingerprint.slice(0, 16),
            new: r.probe.fingerprint_sha256.slice(0, 16),
          }),
        );
      } else {
        setProbeMsg(t(`${o}.probe.matchOk`, { domain: r.endpoint.domain_pattern, subject: r.probe.subject_cn || '-' }));
      }
      loadTrusted();
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      setTrustedError(t(`${o}.probe.reprobeFail`, { error: err.response?.data?.error ?? err.message ?? t(`${o}.error.unknownError`) }));
    } finally {
      setReprobingId(null);
    }
  };

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t(`${o}.breadcrumb1`)}</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-trust">{t(`${o}.breadcrumb2`)}</Link>
          <span>/</span>
          <span className="crumb-current">{t(`${o}.breadcrumb3`)}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t(`${o}.eyebrow`)}</div>
            <h2 className="h-title">{t(`${o}.title`)}</h2>
            <p className="h-subtitle">
              {t(`${o}.subtitle`)}
            </p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t(`${o}.stat1Label`)}</div>
              <div className="stat-card-value">{trusted.length}</div>
              <div className="stat-card-sub muted-strong">{t(`${o}.stat1Sub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${o}.stat2Label`)}</div>
              <div className={`stat-card-value ${alerts.length > 0 ? 'tone-red' : 'tone-green'}`}>{alerts.length}</div>
              <div className="stat-card-sub muted-strong">{t(`${o}.stat2Sub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${o}.stat3Label`)}</div>
              <div className="stat-card-value">{instances.length}</div>
              <div className="stat-card-sub muted-strong">{t(`${o}.stat3Sub`, { count: healthy.length })}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${o}.stat4Label`)}</div>
              <div className="stat-card-value" style={{ fontSize: '1rem' }}>install_skill</div>
              <div className="stat-card-sub muted-strong">{t(`${o}.stat4Sub`)}</div>
            </div>
          </div>
        </div>

        {/* --- 外联开启 TLS 总开关 (defense.requireHttps) --- */}
        <div className="panel">
          <div className="flex items-center justify-between mb-4 gap-4">
            <div>
              <div className="eyebrow">{t(`${o}.tls.eyebrow`)}</div>
              <h3 className="section-title-lg mt-1">{t(`${o}.tls.title`)}</h3>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-xs muted-strong">{t(`${o}.tls.mode`)}</span>
              <div className="mode-selector">
                <button className={httpsMode === 'enforce' ? 'active-enforce' : ''} onClick={() => setMode('defense.requireHttps', 'enforce')}>
                  {t(`${o}.tls.enforce`)}
                </button>
                <button className={httpsMode === 'observe' ? 'active-observe' : ''} onClick={() => setMode('defense.requireHttps', 'observe')}>
                  {t(`${o}.tls.observe`)}
                </button>
                <button className={httpsMode === 'off' ? 'active-off' : ''} onClick={() => setMode('defense.requireHttps', 'off')}>
                  {t(`${o}.tls.off`)}
                </button>
              </div>
              <ApplyDispatchButton onDispatch={dispatchApply} busy={dispatching} className="btn-primary btn-sm" triggerLabel={t(`${o}.tls.saveApply`)} />
              {dispatchMsg && <span className="text-xs muted ml-1">{dispatchMsg}</span>}
            </div>
          </div>
          <div className="grid grid-cols-[1fr_120px] gap-4 items-start">
            <div className="text-xs muted leading-6">
              {t(`${o}.tls.desc`)}
            </div>
            <div className="p-4 rounded-2xl border border-[#eadfd8] bg-[#fffaf7] text-right">
              <div className="text-[10px] muted-strong tracking-wider">{t(`${o}.tls.recentHits`)}</div>
              <div className={`text-2xl font-bold mt-1 tone-${httpsHits > 0 ? 'red' : 'green'}`}>{httpsHits}</div>
              <div className="text-xs muted mt-0.5">{t(`${o}.tls.recentHitsSub`)}</div>
            </div>
          </div>
        </div>

        {/* === 出站可信端点白名单 (defense.outboundTrust) === */}
        <div className="panel">
          <div className="flex items-center justify-between mb-4 gap-4 flex-wrap">
            <div>
              <div className="eyebrow">{t(`${o}.trust.eyebrow`)}</div>
              <h3 className="section-title-lg mt-1">{t(`${o}.trust.title`)}</h3>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-xs muted-strong">{t(`${o}.trust.mode`)}</span>
              <div className="mode-selector">
                <button className={trustMode === 'enforce' ? 'active-enforce' : ''} onClick={() => setMode('defense.outboundTrust', 'enforce')}>
                  {t(`${o}.trust.enforce`)}
                </button>
                <button className={trustMode === 'observe' ? 'active-observe' : ''} onClick={() => setMode('defense.outboundTrust', 'observe')}>
                  {t(`${o}.trust.observe`)}
                </button>
                <button className={trustMode === 'off' ? 'active-off' : ''} onClick={() => setMode('defense.outboundTrust', 'off')}>
                  {t(`${o}.trust.off`)}
                </button>
              </div>
              <ApplyDispatchButton onDispatch={dispatchApply} busy={dispatching} className="btn-primary btn-sm" triggerLabel={t(`${o}.trust.saveApply`)} />
              {dispatchMsg && <span className="text-xs muted ml-1">{dispatchMsg}</span>}
              <span className={`text-xs font-bold tone-${trustHits > 0 ? 'red' : 'green'}`}>{t(`${o}.trust.recentBlocks`, { count: trustHits })}</span>
            </div>
          </div>

          {/* 新增表单 */}
          <div className="grid gap-2 mb-3 items-center" style={{ gridTemplateColumns: '1.4fr 2fr 1.2fr auto' }}>
            <input
              className="input"
              placeholder={t(`${o}.trust.placeholderDomain`)}
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
            />
            <input
              className="input"
              placeholder={t(`${o}.trust.placeholderFingerprint`)}
              value={newFingerprint}
              onChange={(e) => setNewFingerprint(e.target.value)}
            />
            <button
              className="btn-secondary btn-sm"
              disabled={!newDomain.trim() || probing}
              onClick={probeFingerprint}
              title="TLS handshake to domain:443, fill leaf cert SHA256 into fingerprint field"
            >
              {probing ? t(`${o}.trust.probing`) : t(`${o}.trust.probeFingerprint`)}
            </button>
            <input
              className="input"
              placeholder={t(`${o}.trust.placeholderLabel`)}
              value={newLabel}
              onChange={(e) => setNewLabel(e.target.value)}
            />
            <button className="btn-primary btn-sm" disabled={!newDomain.trim()} onClick={addTrusted}>
              {t(`${o}.trust.add`)}
            </button>
          </div>
          {probeMsg && <div className="text-xs muted mb-2">{probeMsg}</div>}
          {trustedError && (
            <div className="alert alert-danger mb-3">
              <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01" />
              </svg>
              {trustedError}
            </div>
          )}

          {/* 列表 */}
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 70 }}>{t(`${o}.trust.colStatus`)}</th>
                <th>{t(`${o}.trust.colDomain`)}</th>
                <th>{t(`${o}.trust.colFingerprint`)}</th>
                <th>{t(`${o}.trust.colLabel`)}</th>
                <th style={{ width: 90 }}>{t(`${o}.trust.colAddedAt`)}</th>
                <th style={{ width: 60 }}></th>
              </tr>
            </thead>
            <tbody>
              {trustedLoading && (
                <tr>
                  <td colSpan={6} className="muted text-sm py-4 text-center">{t(`${o}.trust.loading`)}</td>
                </tr>
              )}
              {!trustedLoading && trusted.length === 0 && (
                <tr>
                  <td colSpan={6} className="muted text-sm py-4 text-center">
                    {t(`${o}.trust.emptyList`, { mode: trustMode })}
                    {trustMode === 'enforce' && trusted.length === 0
                      ? t(`${o}.trust.emptyEnforceWarning`)
                      : t(`${o}.trust.emptyAddPrompt`)}
                  </td>
                </tr>
              )}
              {trusted.map((ep) => (
                <tr key={ep.id}>
                  <td>
                    <span className={`badge badge-${ep.status === 'active' ? 'green' : 'slate'}`}>{ep.status}</span>
                  </td>
                  <td>
                    <code className="text-sm font-mono text-[#171212]">{ep.domain_pattern}</code>
                  </td>
                  <td>
                    {ep.fingerprint_sha256 ? (
                      <code className="text-[10px] muted-strong">{ep.fingerprint_sha256.slice(0, 32)}…</code>
                    ) : (
                      <span className="text-xs muted italic">{t(`${o}.trust.onlyDomain`)}</span>
                    )}
                  </td>
                  <td>
                    <span className="text-xs">{ep.label ?? '—'}</span>
                  </td>
                  <td>
                    <span className="text-xs muted">{ep.created_at?.slice(0, 10)}</span>
                  </td>
                  <td>
                    <div className="flex gap-2 items-center">
                      {!ep.domain_pattern.includes('*') && (
                        <button
                          className="text-xs text-[#0369a1] font-semibold hover:underline disabled:opacity-50"
                          disabled={reprobingId === ep.id}
                          onClick={() => reprobeOne(ep.id)}
                          title="Re-probe TLS handshake, compare with stored baseline; alert + auto-update baseline if mismatch"
                        >
                          {reprobingId === ep.id ? t(`${o}.trust.probing`) : t(`${o}.trust.reprobe`)}
                        </button>
                      )}
                      <button className="text-xs text-[#dc2626] font-semibold hover:underline" onClick={() => removeTrusted(ep.id)}>
                        {t(`${o}.trust.delete`)}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="text-xs muted mt-3 leading-5">
            <strong className="text-[#171212]">{t(`${o}.trust.behavior`)}</strong>：{t(`${o}.trust.behaviorDesc`)}
            <br />
            <strong className="text-[#171212]">{t(`${o}.trust.certPhase2a`)}</strong>：{t(`${o}.trust.certPhase2aDesc`)}
          </div>
        </div>
      </div>
    </AdminLayout>
  );
};

export default OutboundPage;
