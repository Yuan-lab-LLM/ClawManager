import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import { secplaneService, type KillSwitchState } from '../../../../services/secplaneService';
import { instanceService } from '../../../../services/instanceService';
import { useI18n } from '../../../../contexts/I18nContext';

// 应急熔断 (scenario i) — 接 secplane kill-switch 后端。

type InstanceLite = { id: number; name: string; status?: string };

const BreakerPage: React.FC = () => {
  const { t } = useI18n();
  const k = 'secplane.protection.govern.breaker';

  // -- kill switch 状态 --
  const [ks, setKs] = useState<KillSwitchState | null>(null);
  const [ksLoading, setKsLoading] = useState(false);
  const [ksBusy, setKsBusy] = useState(false);
  const [lastDispatchCount, setLastDispatchCount] = useState<number | null>(null);
  const loadKs = useCallback(async () => {
    setKsLoading(true);
    try {
      setKs(await secplaneService.getKillSwitch());
    } catch {
      // ignore
    } finally {
      setKsLoading(false);
    }
  }, []);
  useEffect(() => {
    loadKs();
    const timer = window.setInterval(loadKs, 10_000);
    return () => window.clearInterval(timer);
  }, [loadKs]);
  const active = ks?.enabled === 1;

  // -- 真实实例列表 --
  const [instances, setInstances] = useState<InstanceLite[]>([]);
  const loadInstances = useCallback(async () => {
    try {
      const list = await instanceService.getInstances(1, 1000);
      const items = (list?.instances ?? []) as Array<{ id: number; name: string; status?: string; type?: string }>;
      const ocs = items
        .filter((i) => (i.type ?? 'openclaw') === 'openclaw')
        .map((i) => ({ id: i.id, name: i.name, status: i.status }));
      setInstances(ocs);
    } catch {
      setInstances([]);
    }
  }, []);
  useEffect(() => { loadInstances(); }, [loadInstances]);
  const runningInstances = instances.filter((i) => i.status === 'running');

  // -- 表单状态 --
  const [reason, setReason] = useState('');
  const [confirmText, setConfirmText] = useState('');
  const canExecute = !active && !ksBusy && reason.trim().length > 0 && confirmText.trim() === 'CONFIRM';

  const doEnable = async () => {
    if (!canExecute) return;
    setKsBusy(true);
    try {
      const res = await secplaneService.enableKillSwitch(reason.trim());
      setKs(res.state);
      setLastDispatchCount(res.dispatch?.target_count ?? null);
      setReason('');
      setConfirmText('');
      window.alert(t(`${k}.manualTrigger.enableSuccess`, { count: res.dispatch?.target_count ?? 0 }));
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      window.alert(t(`${k}.manualTrigger.enableFail`) + (err.response?.data?.error ?? err.message ?? t(`${k}.manualTrigger.unknownError`)));
    } finally {
      setKsBusy(false);
    }
  };

  const doDisable = async () => {
    if (!active || ksBusy) return;
    if (!window.confirm(t(`${k}.deactivation.disableConfirm`))) return;
    setKsBusy(true);
    try {
      const res = await secplaneService.disableKillSwitch();
      setKs(res.state);
      setLastDispatchCount(res.dispatch?.target_count ?? null);
      window.alert(t(`${k}.deactivation.disableSuccess`, { count: res.dispatch?.target_count ?? 0 }));
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      window.alert(t(`${k}.deactivation.disableFail`) + (err.response?.data?.error ?? err.message ?? t(`${k}.manualTrigger.unknownError`)));
    } finally {
      setKsBusy(false);
    }
  };

  const enableTitle = active ? t(`${k}.manualTrigger.alreadyEnabled`) : !reason.trim() ? t(`${k}.manualTrigger.pleaseFillReason`) : confirmText.trim() !== 'CONFIRM' ? t(`${k}.manualTrigger.pleaseTypeConfirm`) : '';

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('nav.secplane')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-govern">{t(`${k}.breadcrumb.parent`)}</Link>
          <span>/</span>
          <span className="crumb-current">{t(`${k}.breadcrumb.current`)}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t(`${k}.hero.eyebrow`)}</div>
            <h2 className="h-title">{t(`${k}.hero.title`)}</h2>
            <p className="h-subtitle">{t(`${k}.hero.subtitle`)}</p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.currentStatus`)}</div>
              <div className={`stat-card-value ${active ? 'tone-red' : 'tone-green'}`}>
                {ksLoading && !ks ? '…' : active ? t(`${k}.stats.statusEnabled`) : t(`${k}.stats.statusClosed`)}
              </div>
              <div className="stat-card-sub muted-strong">
                {active ? t(`${k}.stats.allCallsRejected`) : t(`${k}.stats.normalProtection`)}
              </div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.managedInstances`)}</div>
              <div className="stat-card-value">{instances.length}</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.stats.allStatuses`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.runningInstances`)}</div>
              <div className="stat-card-value tone-green">{runningInstances.length}</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.stats.breakerImpactScope`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.lastDispatch`)}</div>
              <div className="stat-card-value">{lastDispatchCount ?? '—'}</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.stats.targetCount`)}</div>
            </div>
          </div>
        </div>

        {active && (
          <div className="panel" style={{ border: '2px solid #f4b6b3', background: 'linear-gradient(180deg, #fdeded 0%, #ffffff 60%)' }}>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="w-12 h-12 rounded-2xl bg-red-600 flex items-center justify-center" style={{ flexShrink: 0 }}>
                  <svg width="22" height="22" fill="none" viewBox="0 0 24 24" stroke="white">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                  </svg>
                </div>
                <div>
                  <div className="eyebrow" style={{ color: '#b42318' }}>{t(`${k}.activePanel.breakerActive`)}</div>
                  <h3 className="text-2xl font-bold text-[#171212] mt-1">{t(`${k}.activePanel.systemLevelBreaker`)}</h3>
                  <div className="text-xs muted mt-1">
                    {t(`${k}.activePanel.reason`)}<strong className="text-[#171212]">{ks?.reason || t(`${k}.activePanel.none`)}</strong>
                    <span className="ml-3">{t(`${k}.activePanel.enabledBy`)}{ks?.set_by || t(`${k}.activePanel.none`)}</span>
                    <span className="ml-3">{t(`${k}.activePanel.enabledAt`)}{ks?.set_at?.replace('T', ' ').slice(0, 19) ?? '-'}</span>
                  </div>
                </div>
              </div>
              <button className="btn-secondary shrink-0" disabled={ksBusy} onClick={doDisable}>
                {ksBusy ? t(`${k}.activePanel.processing`) : t(`${k}.activePanel.disable`)}
              </button>
            </div>
          </div>
        )}

        <div className="grid grid-cols-2 gap-4">
          <div className="panel">
            <div className="eyebrow mb-3">{t(`${k}.manualTrigger.eyebrow`)}</div>
            <h3 className="section-title-lg mb-4">{t(`${k}.manualTrigger.title`)}</h3>
            <div className="alert alert-warning mb-4">
              <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01" />
              </svg>
              {t(`${k}.manualTrigger.alertNote`)}
            </div>

            <div className="space-y-3 mb-4 text-sm">
              <div>
                <div className="eyebrow text-[10px] mb-1">{t(`${k}.manualTrigger.impactPreview`)}</div>
                <div className="p-3 rounded-xl bg-[#fdf6f1] border border-[#eadfd8] text-xs">
                  {t(`${k}.manualTrigger.willAffect`)} <strong>{runningInstances.length}</strong> {t(`${k}.manualTrigger.runningInstancesUnit`)}
                  {runningInstances.length === 0 ? (
                    <span className="muted ml-1">{t(`${k}.manualTrigger.noRunningInstances`)}</span>
                  ) : (
                    <ul className="mt-2 space-y-1">
                      {runningInstances.map((i) => (
                        <li key={i.id} className="font-mono">
                          [{i.id}] {i.name} <span className="badge badge-green ml-1">{i.status}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>
              <div>
                <div className="eyebrow text-[10px] mb-1">{t(`${k}.manualTrigger.reasonRequired`)}</div>
                <textarea
                  className="input"
                  rows={3}
                  placeholder={t(`${k}.manualTrigger.reasonPlaceholder`)}
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  disabled={active || ksBusy}
                />
              </div>
              <div>
                <div className="eyebrow text-[10px] mb-1">
                  {t(`${k}.manualTrigger.confirmInstruction`)}<code className="text-[11px] text-[#b42318] bg-[#fdf6f1] px-1 rounded">{t(`${k}.manualTrigger.confirmCode`)}</code>{t(`${k}.manualTrigger.confirmCodeHint`)}
                </div>
                <input
                  className="input"
                  placeholder={t(`${k}.manualTrigger.confirmPlaceholder`)}
                  value={confirmText}
                  onChange={(e) => setConfirmText(e.target.value)}
                  disabled={active || ksBusy}
                />
              </div>
            </div>
            <button
              className="inline-flex items-center justify-center gap-2 rounded-2xl px-5 py-3 text-sm font-semibold text-white w-full"
              style={{
                background: canExecute ? 'linear-gradient(135deg,#dc2626,#991b1b)' : '#d8c4b9',
                cursor: canExecute ? 'pointer' : 'not-allowed',
              }}
              disabled={!canExecute}
              onClick={doEnable}
              title={enableTitle}
            >
              {ksBusy ? t(`${k}.manualTrigger.dispatching`) : active ? t(`${k}.manualTrigger.enabled`) : t(`${k}.manualTrigger.executeSystemBreaker`)}
            </button>
          </div>

          <div className="panel">
            <div className="eyebrow mb-3">{t(`${k}.deactivation.eyebrow`)}</div>
            <h3 className="section-title-lg mb-4">{t(`${k}.deactivation.title`)}</h3>
            <div className={`alert ${active ? 'alert-info' : 'alert-success'} mb-4`}>
              <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              {active ? t(`${k}.deactivation.afterDeactivation`) : t(`${k}.deactivation.currentlyInactive`)}
            </div>
            <div className="space-y-3 text-sm mb-4">
              <div className="p-3 rounded-xl border bg-[#fdf6f1]" style={{ borderColor: '#eadfd8' }}>
                <div className="flex items-center justify-between">
                  <span className="text-xs muted-strong">{t(`${k}.deactivation.status`)}</span>
                  <span className={`badge ${active ? 'badge-red' : 'badge-green'}`}>
                    {active ? t(`${k}.deactivation.enabled`) : t(`${k}.deactivation.closed`)}
                  </span>
                </div>
                {active && (
                  <>
                    <div className="text-xs muted mt-2">
                      {t(`${k}.deactivation.reason`)}<span className="text-[#171212]">{ks?.reason || t(`${k}.activePanel.none`)}</span>
                    </div>
                    <div className="text-xs muted">
                      {t(`${k}.deactivation.enabledAt`)}{ks?.set_at?.replace('T', ' ').slice(0, 19) ?? '-'}
                    </div>
                  </>
                )}
              </div>
            </div>
            <button
              className="btn-secondary w-full"
              disabled={!active || ksBusy}
              style={!active || ksBusy ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
              onClick={doDisable}
            >
              {ksBusy ? t(`${k}.activePanel.processing`) : active ? t(`${k}.deactivation.disable`) : t(`${k}.deactivation.currentlyNotEnabled`)}
            </button>
            <div className="text-xs muted mt-3 leading-5">
              <strong>{t(`${k}.deactivation.futurePlans`)}</strong>：{t(`${k}.deactivation.futurePlansDesc`)}
            </div>
          </div>
        </div>
      </div>
    </AdminLayout>
  );
};

export default BreakerPage;
