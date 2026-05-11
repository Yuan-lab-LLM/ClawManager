import React, { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../../../contexts/I18nContext';
import { adminService } from '../../../services/adminService';
import {
  AnalyzerGroup,
  assetTypeLabel,
  Badge,
  formatDateTime,
  jobStatusLabel,
  riskLabel,
  riskTone,
  scanModeLabel,
  scanScopeLabel,
  SecurityCenterShell,
  severityLabel,
  severityTone,
  StatCard,
  useSecurityCenterData,
} from './securityCenterShared';

const AdminSecurityReportsPage: React.FC = () => {
  const { locale, t } = useI18n();
  const { jobs, summary, error, setError } = useSecurityCenterData();
  const [selectedJobId, setSelectedJobId] = useState<number | null>(null);
  const [selectedJob, setSelectedJob] = useState<any | null>(null);

  useEffect(() => {
    setSelectedJobId((current) => current ?? jobs[0]?.id ?? null);
  }, [jobs]);

  useEffect(() => {
    if (!selectedJobId) {
      setSelectedJob(null);
      return;
    }
    let cancelled = false;
    const loadDetail = async () => {
      try {
        const detail = await adminService.getSecurityScanJob(selectedJobId);
        if (!cancelled) {
          setSelectedJob(detail);
        }
      } catch (err: any) {
        if (!cancelled) {
          setError(err.response?.data?.error || t('securityCenter.errors.loadReports'));
        }
      }
    };
    void loadDetail();
    return () => {
      cancelled = true;
    };
  }, [selectedJobId, setError, t]);

  const riskCards = useMemo(() => Object.entries(selectedJob?.report?.risk_counts ?? {}), [selectedJob]);

  return (
    <SecurityCenterShell summary={summary}>
      <div className="space-y-6">
        {error ? (
          <div className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
        ) : null}

        <section className="grid grid-cols-1 gap-6 xl:grid-cols-[380px_minmax(0,1fr)]">
          <div className="rounded-[28px] border border-[#dbe4f0] bg-white p-6 shadow-[0_24px_60px_-42px_rgba(30,64,175,0.35)]">
            <div>
              <h2 className="text-lg font-semibold text-[#171212]">{t('securityCenter.reports.title')}</h2>
              <p className="mt-1 text-sm text-[#8f8681]">{t('securityCenter.reports.subtitle')}</p>
            </div>
            <div className="mt-5 space-y-3">
              {jobs.length === 0 ? (
                <div className="rounded-2xl border border-dashed border-[#dbe4f0] px-4 py-8 text-sm text-[#8f8681]">{t('securityCenter.reports.noJobs')}</div>
              ) : (
                jobs.map((job) => (
                  <button
                    key={job.id}
                    type="button"
                    onClick={() => setSelectedJobId(job.id)}
                    className={`w-full rounded-2xl border px-4 py-4 text-left transition ${
                      selectedJobId === job.id ? 'border-[#93c5fd] bg-[#dbeafe]' : 'border-[#dbe4f0] bg-[#f8fbff] hover:border-[#dcc6b8]'
                    }`}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <div className="font-medium text-[#171212]">
                          {t('securityCenter.reports.jobTitle', {
                            scope: t(`securityCenter.scanScopes.${scanScopeLabel(job.scan_scope)}`),
                            mode: t(`securityCenter.scanModes.${scanModeLabel(job.scan_mode)}`),
                            id: job.id,
                          })}
                        </div>
                        <div className="mt-1 text-xs text-[#8f8681]">
                          {t('securityCenter.reports.jobMeta', {
                            assetType: t(`securityCenter.assetTypes.${assetTypeLabel(job.asset_type)}`),
                            scope: t(`securityCenter.scanScopes.${scanScopeLabel(job.scan_scope)}`),
                            mode: t(`securityCenter.scanModes.${scanModeLabel(job.scan_mode)}`),
                            completed: job.completed_items,
                            total: job.total_items,
                          })}
                        </div>
                        <div className="mt-1 text-xs text-[#8f8681]">{formatDateTime(job.created_at, locale)}</div>
                      </div>
                      <Badge tone={job.status === 'completed' ? 'green' : job.status === 'failed' ? 'red' : 'orange'}>
                        {t(`securityCenter.status.${jobStatusLabel(job.status)}`)}
                      </Badge>
                    </div>
                    <div className="mt-3 h-2 overflow-hidden rounded-full bg-[#f3e7df]">
                      <div className="h-full rounded-full bg-[#2563eb]" style={{ width: `${job.progress_pct}%` }} />
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>

          <div className="rounded-[28px] border border-[#dbe4f0] bg-white p-6 shadow-[0_24px_60px_-42px_rgba(30,64,175,0.35)]">
            {!selectedJob ? (
              <div className="rounded-2xl border border-dashed border-[#dbe4f0] px-4 py-10 text-center text-sm text-[#8f8681]">
                {t('securityCenter.reports.selectJob')}
              </div>
            ) : (
              <div className="space-y-5">
                <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                  <div className="flex items-center justify-between gap-3">
                    <div className="font-medium text-[#171212]">
                      {t('securityCenter.reports.jobTitle', {
                        scope: t(`securityCenter.scanScopes.${scanScopeLabel(selectedJob.scan_scope)}`),
                        mode: t(`securityCenter.scanModes.${scanModeLabel(selectedJob.scan_mode)}`),
                        id: selectedJob.id,
                      })}
                    </div>
                    <Badge tone={selectedJob.status === 'completed' ? 'green' : selectedJob.status === 'failed' ? 'red' : 'orange'}>
                      {t(`securityCenter.status.${jobStatusLabel(selectedJob.status)}`)}
                    </Badge>
                  </div>
                  <div className="mt-3 h-2 overflow-hidden rounded-full bg-[#f3e7df]">
                    <div className="h-full rounded-full bg-[#2563eb]" style={{ width: `${selectedJob.progress_pct}%` }} />
                  </div>
                  <div className="mt-3 text-xs text-[#8f8681]">
                    {t('securityCenter.reports.completedProgress', {
                      completed: selectedJob.completed_items,
                      total: selectedJob.total_items,
                    })}
                    {selectedJob.current_item_name ? ` · ${t('securityCenter.reports.currentItem', { name: selectedJob.current_item_name })}` : ''}
                  </div>
                  <div className="mt-2 text-xs text-[#8f8681]">
                    {t('securityCenter.reports.scopeMode', {
                      scope: t(`securityCenter.scanScopes.${scanScopeLabel(selectedJob.scan_scope)}`),
                      mode: t(`securityCenter.scanModes.${scanModeLabel(selectedJob.scan_mode)}`),
                    })}
                  </div>
                  <div className="mt-2 text-xs text-[#8f8681]">
                    {selectedJob.started_at ? t('securityCenter.reports.startedAt', { value: formatDateTime(selectedJob.started_at, locale) }) : t('securityCenter.reports.waitingToStart')}
                    {selectedJob.finished_at ? ` · ${t('securityCenter.reports.finishedAt', { value: formatDateTime(selectedJob.finished_at, locale) })}` : ''}
                  </div>
                </div>

                {selectedJob.report ? (
                  <>
                    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                      {riskCards.map(([level, count]) => (
                        <StatCard key={level} label={t(`securityCenter.risks.${riskLabel(level)}`)} value={String(count)} />
                      ))}
                    </div>

                    <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                      <div className="text-sm font-medium text-[#171212]">{t('securityCenter.reports.analyzerView')}</div>
                      <p className="mt-1 text-xs leading-5 text-[#8f8681]">
                        {t('securityCenter.reports.analyzerViewDesc')}
                      </p>
                      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
                        <AnalyzerGroup title={t('securityCenter.reports.configuredAnalyzers')} items={selectedJob.report.configured_analyzers ?? []} />
                        <AnalyzerGroup title={t('securityCenter.reports.availableAnalyzers')} items={selectedJob.report.available_analyzers ?? []} />
                        <AnalyzerGroup title={t('securityCenter.reports.triggeredAnalyzers')} items={selectedJob.report.triggered_analyzers ?? []} emptyLabel={t('securityCenter.reports.noAnalyzerFindings')} />
                      </div>
                    </div>

                    <div>
                      <div className="text-sm font-medium text-[#171212]">{t('securityCenter.reports.itemResults')}</div>
                      <div className="mt-3 max-h-[720px] space-y-3 overflow-y-auto pr-1">
                        {(selectedJob.report.items ?? []).map((item: any) => (
                          <div key={item.id} className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] px-4 py-3">
                            <div className="flex items-start justify-between gap-3">
                              <div className="min-w-0 flex-1">
                                <div className="font-medium text-[#171212]">{item.asset_name}</div>
                                <div className="mt-1 text-xs text-[#8f8681]">
                                  {item.cached_result ? t('securityCenter.reports.cachedResult') : t('securityCenter.reports.liveScan')} · {t(`securityCenter.status.${jobStatusLabel(item.status)}`)}
                                </div>
                                {item.triggered_analyzers && item.triggered_analyzers.length > 0 ? (
                                  <div className="mt-2 flex flex-wrap gap-2">
                                    {item.triggered_analyzers.map((analyzer: string) => (
                                      <span
                                        key={`${item.id}-${analyzer}`}
                                        className="inline-flex rounded-full border border-[#d9e0e7] bg-[#f6f8fb] px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#556070]"
                                      >
                                        {analyzer}
                                      </span>
                                    ))}
                                  </div>
                                ) : null}
                                {item.summary ? <div className="mt-2 text-sm text-[#6f6661]">{item.summary}</div> : null}
                                {item.findings && item.findings.length > 0 ? (
                                  <div className="mt-3 space-y-2">
                                    {item.findings.map((finding: any, findingIndex: number) => (
                                      <div key={`${item.id}-${finding.rule_id}-${findingIndex}`} className="rounded-xl border border-[#f1d9d9] bg-white px-3 py-3">
                                        <div className="flex flex-wrap items-center gap-2">
                                          <Badge tone={severityTone(finding.severity)}>{severityLabel(finding.severity)}</Badge>
                                          <span className="text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{finding.analyzer}</span>
                                          <span className="text-[11px] text-[#8f8681]">{finding.rule_id}</span>
                                        </div>
                                        <div className="mt-2 text-sm font-medium text-[#171212]">{finding.title}</div>
                                        <div className="mt-1 text-sm leading-6 text-[#6f6661]">{finding.description}</div>
                                        {finding.file_path ? (
                                          <div className="mt-2 text-xs text-[#8f8681]">
                                            {t('securityCenter.reports.evidence', { value: `${finding.file_path}${finding.line_number ? `:${finding.line_number}` : ''}` })}
                                          </div>
                                        ) : null}
                                        {finding.snippet ? (
                                          <pre className="mt-2 overflow-x-auto rounded-lg bg-[#f8fbff] px-3 py-2 text-xs text-[#7a4b34]">{finding.snippet}</pre>
                                        ) : null}
                                        {finding.remediation ? (
                                          <div className="mt-2 text-xs leading-5 text-[#8f8681]">{t('securityCenter.reports.remediation', { value: finding.remediation })}</div>
                                        ) : null}
                                      </div>
                                    ))}
                                  </div>
                                ) : null}
                                {item.error_message ? <div className="mt-2 text-sm text-red-700">{item.error_message}</div> : null}
                              </div>
                              <Badge tone={riskTone(item.risk_level || 'unknown')}>{t(`securityCenter.risks.${riskLabel(item.risk_level || item.status)}`)}</Badge>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  </>
                ) : (
                  <div className="text-sm text-[#8f8681]">{t('securityCenter.reports.reportGenerating')}</div>
                )}
              </div>
            )}
          </div>
        </section>
      </div>
    </SecurityCenterShell>
  );
};

export default AdminSecurityReportsPage;
