import React, { useMemo, useState } from 'react';
import { useI18n } from '../../../contexts/I18nContext';
import { adminService } from '../../../services/adminService';
import {
  Badge,
  formatDateTime,
  riskLabel,
  riskTone,
  scanModeLabel,
  scanScopeLabel,
  scanStatusLabel,
  scanStatusTone,
  SecurityCenterShell,
  sourceLabel,
  useSecurityCenterData,
} from './securityCenterShared';

const pageSize = 8;

const AdminSecurityDashboardPage: React.FC = () => {
  const { locale, t } = useI18n();
  const { skills, jobs, config, loading, error, summary, setError, loadAll } = useSecurityCenterData();
  const [search, setSearch] = useState('');
  const [assetPage, setAssetPage] = useState(1);
  const [startingScan, setStartingScan] = useState<'' | 'incremental' | 'full'>('');

  const filteredSkills = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    if (!keyword) {
      return skills;
    }
    return skills.filter((item) =>
      [item.name, item.skill_key, item.source_type, item.risk_level, item.scan_status, String(item.user_id)].some((value) =>
        value.toLowerCase().includes(keyword),
      ),
    );
  }, [search, skills]);

  const totalPages = Math.max(1, Math.ceil(filteredSkills.length / pageSize));
  const currentPage = Math.min(assetPage, totalPages);
  const paginatedSkills = useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return filteredSkills.slice(start, start + pageSize);
  }, [currentPage, filteredSkills]);

  const scannedRatio = summary.total > 0 ? Math.round((summary.completed / summary.total) * 100) : 0;
  const scannedProgress = summary.total > 0 ? summary.completed / summary.total : 0;
  const safeCount = skills.filter((item) => item.risk_level === 'none').length;
  const lowCount = skills.filter((item) => item.risk_level === 'low').length;
  const pendingCount = skills.filter((item) => item.scan_status === 'pending').length;
  const failedCount = skills.filter((item) => item.scan_status === 'failed').length;

  const riskBands = [
    { label: t('securityCenter.risks.safe'), count: safeCount, color: '#139a74', text: 'text-[var(--cm-success)]' },
    { label: t('securityCenter.risks.low'), count: lowCount, color: '#d4a11f', text: 'text-[var(--cm-warning)]' },
    { label: t('securityCenter.risks.medium'), count: summary.mediumRisk, color: '#df7a1c', text: 'text-[var(--cm-warning)]' },
    { label: t('securityCenter.risks.high'), count: summary.highRisk, color: '#c33b31', text: 'text-[var(--cm-error)]' },
    { label: t('securityCenter.status.pending'), count: pendingCount, color: '#4d6b89', text: 'text-[var(--cm-muted)]' },
  ];
  const maxRiskCount = Math.max(...riskBands.map((item) => item.count), 1);

  const warningAssets = skills.filter((item) => item.risk_level === 'high' || item.risk_level === 'medium').slice(0, 4);
  const hotAssets = [...skills]
    .sort((left, right) => right.instance_count - left.instance_count || left.name.localeCompare(right.name))
    .slice(0, 5);
  const recentJobs = jobs.slice(0, 4);

  const handleStartScan = async (scanScope: 'incremental' | 'full') => {
    try {
      setStartingScan(scanScope);
      setError(null);
      await adminService.startSecurityScan({ asset_type: 'skill', scan_scope: scanScope });
      await loadAll('refresh');
    } catch (err: any) {
      setError(err.response?.data?.error || t(scanScope === 'full' ? 'securityCenter.errors.startFullScan' : 'securityCenter.errors.startIncrementalScan'));
    } finally {
      setStartingScan('');
    }
  };

  return (
    <SecurityCenterShell summary={summary}>
      <div className="space-y-6">
        {error ? (
          <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
        ) : null}

        <section className="rounded-lg border border-[var(--cm-border)] bg-white p-6 shadow-none">
          <div className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <p className="mt-2 text-sm font-medium text-[var(--cm-muted)]">
                {t('securityCenter.dashboard.activeMode', {
                  mode: t(`securityCenter.scanModes.${scanModeLabel(config?.active_mode || config?.default_mode || 'quick')}`),
                })}
              </p>
            </div>
            <div className="flex flex-wrap gap-3">
              <button
                type="button"
                onClick={() => void handleStartScan('incremental')}
                disabled={startingScan !== ''}
                className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {startingScan === 'incremental' ? t('securityCenter.dashboard.starting') : t('securityCenter.scanScopes.incremental')}
              </button>
              <button
                type="button"
                onClick={() => void handleStartScan('full')}
                disabled={startingScan !== ''}
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {startingScan === 'full' ? t('securityCenter.dashboard.starting') : t('securityCenter.scanScopes.full')}
              </button>
            </div>
          </div>

          <div className="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <StatCard label={t('securityCenter.dashboard.totalAssets')} value={String(summary.total)} />
            <StatCard label={t('securityCenter.dashboard.completedScans')} value={String(summary.completed)} />
            <StatCard label={t('securityCenter.risks.high')} value={String(summary.highRisk)} />
            <StatCard label={t('securityCenter.risks.medium')} value={String(summary.mediumRisk)} />
          </div>
        </section>

        <section className="grid grid-cols-1 items-start gap-6 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div className="space-y-6">
            <div className="rounded-lg border border-[var(--cm-border)] bg-white p-6 shadow-none">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="min-w-0 rounded-lg border border-[var(--cm-border-subtle)] bg-[var(--cm-surface)] p-5 sm:col-span-2">
                  <div className="text-xs font-semibold tracking-normal text-[#1980a1]">{t('securityCenter.dashboard.coverage')}</div>
                  <div className="mt-4 flex flex-wrap items-center gap-4">
                    <CoverageDonut ratio={scannedRatio} progress={scannedProgress} />
                    <div className="min-w-[160px] flex-1">
                      <div className="text-sm font-semibold text-[var(--cm-ink)]">{summary.completed}/{summary.total}</div>
                      <div className="mt-1 text-xs leading-5 text-[var(--cm-muted)]">{t('securityCenter.dashboard.coverageDesc')}</div>
                    </div>
                  </div>
                </div>

                <MiniMetric title={t('securityCenter.risks.safe')} value={safeCount} subtitle={t('securityCenter.dashboard.safeSubtitle')} tone="green" />
                <MiniMetric title={t('securityCenter.risks.high')} value={summary.highRisk} subtitle={t('securityCenter.dashboard.highRiskSubtitle')} tone="red" />
                <MiniMetric title={t('securityCenter.status.pending')} value={pendingCount} subtitle={t('securityCenter.dashboard.pendingSubtitle')} tone="slate" />
                <MiniMetric title={t('securityCenter.status.failed')} value={failedCount} subtitle={t('securityCenter.dashboard.failedSubtitle')} tone="orange" />
              </div>
              <div className="mt-5 grid grid-cols-1 items-start gap-4">
                <div className="rounded-lg border border-[var(--cm-border-subtle)] bg-[var(--cm-subtle)] p-5">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="mt-2 text-base font-semibold text-[var(--cm-ink)]">{t('securityCenter.dashboard.riskDistributionTitle')}</div>
                    </div>
                    <div className="text-xs text-[var(--cm-muted)]">{t('securityCenter.dashboard.groupedByRisk')}</div>
                  </div>
                  <div className="mt-5 space-y-4">
                  {riskBands.map((item) => {
                    const width = item.count <= 0
                      ? '0%'
                      : `${Math.max(2, Math.round((item.count / maxRiskCount) * 100))}%`;
                    return (
                      <div key={`compact-${item.label}`}>
                          <div className="mb-2 flex items-center justify-between gap-3">
                            <div className="text-xs font-semibold tracking-normal text-[var(--cm-muted)]">{item.label}</div>
                            <div className={`text-sm font-semibold ${item.text}`}>{item.count}</div>
                          </div>
                          <div className="h-3 overflow-hidden rounded-full bg-[var(--cm-border-subtle)]">
                            <div className="h-full rounded-full" style={{ width, backgroundColor: item.color }} />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
                <div className="rounded-lg border border-[var(--cm-border-subtle)] bg-[var(--cm-subtle)] p-5">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="mt-2 text-base font-semibold text-[var(--cm-ink)]">{t('securityCenter.dashboard.hotAssetsTitle')}</div>
                    </div>
                    <div className="text-xs text-[var(--cm-muted)]">Top 5</div>
                  </div>
                  <div className="mt-5 space-y-3">
                    {hotAssets.length === 0 ? (
                      <div className="text-sm text-[var(--cm-muted)]">{t('securityCenter.dashboard.noAssetUsage')}</div>
                    ) : (
                      hotAssets.map((item, index) => (
                        <div key={`compact-hot-${item.id}`} className="flex flex-wrap items-center gap-3 rounded-lg border border-[var(--cm-border)] bg-white px-4 py-3">
                          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-[var(--cm-border-subtle)] text-sm font-semibold text-[var(--cm-muted)]">
                            {index + 1}
                          </div>
                          <div className="min-w-[120px] flex-1 break-words">
                            <div className="font-medium text-[var(--cm-ink)] [overflow-wrap:anywhere]">{item.name}</div>
                            <div className="mt-1 text-xs text-[var(--cm-muted)] [overflow-wrap:anywhere]">{item.skill_key}</div>
                          </div>
                          <div className="shrink-0 text-right">
                            <div className="text-base font-semibold text-[var(--cm-ink)]">{item.instance_count}</div>
                            <div className="text-xs tracking-normal text-[var(--cm-muted)]">{t('securityCenter.dashboard.instances')}</div>
                          </div>
                        </div>
                      ))
                    )}
                  </div>
                </div>
              </div>
            </div>

            <section className="rounded-lg border border-[var(--cm-border)] bg-white p-6 shadow-none">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <h2 className="text-base leading-[22px] font-semibold text-[var(--cm-ink)]">{t('securityCenter.dashboard.assetListTitle')}</h2>
                  <p className="mt-1 text-sm text-[var(--cm-muted)]">{t('securityCenter.dashboard.assetListDesc')}</p>
                </div>
                <input
                  value={search}
                  onChange={(event) => {
                    setSearch(event.target.value);
                    setAssetPage(1);
                  }}
                  placeholder={t('securityCenter.dashboard.searchPlaceholder')}
                  className="app-input w-full max-w-sm"
                />
              </div>

              <div className="mt-5 overflow-x-auto">
                {loading ? (
                  <div className="py-12 text-center text-[var(--cm-muted)]">{t('securityCenter.dashboard.loadingAssets')}</div>
                ) : filteredSkills.length === 0 ? (
                  <div className="py-12 text-center text-[var(--cm-muted)]">{t('securityCenter.dashboard.noMatchingAssets')}</div>
                ) : (
                  <>
                    <table className="min-w-full divide-y divide-[var(--cm-border-subtle)] text-sm">
                      <thead className="bg-[var(--cm-subtle)] text-left text-xs font-semibold tracking-normal text-[var(--cm-muted)]">
                        <tr>
                          <th className="px-4 py-3">{t('securityCenter.dashboard.columns.asset')}</th>
                          <th className="px-4 py-3">{t('securityCenter.dashboard.columns.user')}</th>
                          <th className="px-4 py-3">{t('securityCenter.dashboard.columns.source')}</th>
                          <th className="px-4 py-3">{t('securityCenter.dashboard.columns.scanStatus')}</th>
                          <th className="px-4 py-3">{t('securityCenter.dashboard.columns.risk')}</th>
                          <th className="px-4 py-3">{t('securityCenter.dashboard.columns.lastScan')}</th>
                          <th className="px-4 py-3">{t('securityCenter.dashboard.columns.instances')}</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[var(--cm-border-subtle)]">
                        {paginatedSkills.map((item) => (
                          <tr key={item.id} className="transition hover:bg-[var(--cm-subtle)]">
                            <td className="px-4 py-3">
                              <div className="font-medium text-[var(--cm-ink)]">{item.name}</div>
                              <div className="mt-1 text-xs text-[var(--cm-muted)]">{item.skill_key}</div>
                            </td>
                            <td className="px-4 py-3 text-[var(--cm-text)]">#{item.user_id}</td>
                            <td className="px-4 py-3"><Badge tone={item.source_type === 'uploaded' ? 'amber' : 'slate'}>{t(`securityCenter.sources.${sourceLabel(item.source_type)}`)}</Badge></td>
                            <td className="px-4 py-3"><Badge tone={scanStatusTone(item.scan_status)}>{t(`securityCenter.scanStatus.${scanStatusLabel(item.scan_status)}`)}</Badge></td>
                            <td className="px-4 py-3"><Badge tone={riskTone(item.risk_level)}>{t(`securityCenter.risks.${riskLabel(item.risk_level)}`)}</Badge></td>
                            <td className="px-4 py-3 text-[var(--cm-text)]">{item.last_scanned_at ? formatDateTime(item.last_scanned_at, locale) : t('securityCenter.dashboard.notScanned')}</td>
                            <td className="px-4 py-3 text-[var(--cm-text)]">{item.instance_count}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    <div className="mt-4 flex flex-col gap-3 border-t border-[var(--cm-border-subtle)] pt-4 sm:flex-row sm:items-center sm:justify-between">
                      <div className="text-sm text-[var(--cm-muted)]">
                        {t('securityCenter.pagination.summary', {
                          pageSize,
                          from: (currentPage - 1) * pageSize + 1,
                          to: Math.min(currentPage * pageSize, filteredSkills.length),
                        })}
                      </div>
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={() => setAssetPage((current) => Math.max(1, current - 1))}
                          disabled={currentPage <= 1}
                          className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {t('securityCenter.pagination.prev')}
                        </button>
                        <div className="min-w-[88px] text-center text-sm font-medium text-[var(--cm-text)]">
                          {currentPage} / {totalPages}
                        </div>
                        <button
                          type="button"
                          onClick={() => setAssetPage((current) => Math.min(totalPages, current + 1))}
                          disabled={currentPage >= totalPages}
                          className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {t('securityCenter.pagination.next')}
                        </button>
                      </div>
                    </div>
                  </>
                )}
              </div>
            </section>
          </div>

          <div className="rounded-lg border border-[var(--cm-border)] bg-white p-6 shadow-none">
            <div className="mt-2 text-base leading-[22px] font-semibold text-[var(--cm-ink)]">{t('securityCenter.dashboard.scannerAvailability')}</div>
            <div className="mt-4 rounded-lg border border-[var(--cm-border-subtle)] bg-[var(--cm-subtle)] p-4">
              <div className="flex items-center justify-between gap-3">
                <div className="font-medium text-[var(--cm-ink)]">{config?.scanner_status.status_label || t('securityCenter.dashboard.notEnabled')}</div>
                <Badge tone={config?.scanner_status.connected ? 'green' : 'slate'}>
                  {config?.scanner_status.connected ? t('securityCenter.dashboard.connected') : t('securityCenter.dashboard.disconnected')}
                </Badge>
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                {(config?.scanner_status.available_capabilities ?? []).map((item) => (
                  <span
                    key={item}
                    className="inline-flex rounded-full border border-[var(--cm-border)] bg-[var(--cm-subtle)] px-3 py-1 text-xs font-semibold text-[var(--cm-muted)]"
                  >
                    {item}
                  </span>
                ))}
              </div>
              <div className="mt-3 text-sm leading-5 text-[var(--cm-muted)]">
                {config?.scanner_status.llm_enabled
                  ? t('securityCenter.dashboard.scannerLlmEnabled')
                  : t('securityCenter.dashboard.scannerLlmDisabled')}
              </div>
            </div>
            <div className="mt-2 text-base leading-[22px] font-semibold text-[var(--cm-ink)]">{t('securityCenter.dashboard.alertTitle')}</div>
            <div className="mt-4 space-y-3">
              {warningAssets.length === 0 ? (
                <div className="rounded-lg border border-[#d7ecd9] bg-[var(--cm-success-background)] px-4 py-4 text-sm leading-5 text-[var(--cm-success)]">
                  {t('securityCenter.dashboard.noWarnings')}
                </div>
              ) : (
                warningAssets.map((item) => (
                  <div key={item.id} className="rounded-lg border border-[#f1d9d9] bg-[var(--cm-error-background)] px-4 py-4">
                    <div className="flex items-center justify-between gap-3">
                      <div className="font-medium text-[var(--cm-ink)]">{item.name}</div>
                      <Badge tone={riskTone(item.risk_level)}>{t(`securityCenter.risks.${riskLabel(item.risk_level)}`)}</Badge>
                    </div>
                    <div className="mt-2 text-sm leading-5 text-[var(--cm-muted)]">
                      {item.risk_reason || t('securityCenter.dashboard.warningFallback')}
                    </div>
                  </div>
                ))
              )}
            </div>
            <div className="mt-2 text-base leading-[22px] font-semibold text-[var(--cm-ink)]">{t('securityCenter.dashboard.recentScansTitle')}</div>
            <div className="mt-4 space-y-3">
              {recentJobs.length === 0 ? (
                <div className="rounded-lg border border-dashed border-[var(--cm-border)] px-4 py-5 text-sm text-[var(--cm-muted)]">
                  {t('securityCenter.dashboard.noScanJobs')}
                </div>
              ) : (
                recentJobs.map((job) => (
                  <div key={job.id} className="rounded-lg border border-[var(--cm-border-subtle)] bg-[var(--cm-subtle)] px-4 py-4">
                    <div className="flex items-center justify-between gap-3">
                      <div className="min-w-0">
                        <div className="truncate font-medium text-[var(--cm-ink)]">
                          {t('securityCenter.dashboard.jobTitle', {
                            scope: t(`securityCenter.scanScopes.${scanScopeLabel(job.scan_scope)}`),
                            mode: t(`securityCenter.scanModes.${scanModeLabel(job.scan_mode)}`),
                            id: job.id,
                          })}
                        </div>
                        <div className="mt-1 text-xs text-[var(--cm-muted)]">
                          {job.completed_items}/{job.total_items} · {formatDateTime(job.created_at, locale)}
                        </div>
                      </div>
                      <Badge tone={job.status === 'completed' ? 'green' : job.status === 'failed' ? 'red' : 'orange'}>
                        {t(`securityCenter.scanStatus.${scanStatusLabel(job.status)}`)}
                      </Badge>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </section>
      </div>
    </SecurityCenterShell>
  );
};

function MiniMetric({
  title,
  value,
  subtitle,
  tone,
}: {
  title: string;
  value: number;
  subtitle: string;
  tone: 'green' | 'red' | 'orange' | 'slate';
}) {
  const colorClass =
    tone === 'green'
      ? 'border-[#cfe8d7] bg-[var(--cm-success-background)] text-[var(--cm-success)]'
      : tone === 'red'
        ? 'border-[#f0d1d1] bg-[var(--cm-error-background)] text-[var(--cm-error)]'
        : tone === 'orange'
          ? 'border-[#f1ddbf] bg-[var(--cm-warning-background)] text-[var(--cm-warning)]'
          : 'border-[#dde5ec] bg-[var(--cm-subtle)] text-[var(--cm-muted)]';

  return (
    <div className={`rounded-lg border p-5 ${colorClass}`}>
      <div className="text-xs font-semibold tracking-normal">{title}</div>
      <div className="mt-3 text-3xl font-semibold">{value}</div>
      <div className="mt-2 text-xs leading-5 opacity-80">{subtitle}</div>
    </div>
  );
}

function CoverageDonut({ ratio, progress }: { ratio: number; progress: number }) {
  const normalized = Math.min(Math.max(progress, 0), 1);
  const radius = 34;
  const circumference = 2 * Math.PI * radius;
  const dashOffset = circumference * (1 - normalized);

  return (
    <div className="relative flex h-24 w-24 items-center justify-center">
      <svg viewBox="0 0 88 88" className="h-24 w-24 -rotate-90">
        <circle cx="44" cy="44" r={radius} fill="none" stroke="#dbeaf1" strokeWidth="12" />
        <circle
          cx="44"
          cy="44"
          r={radius}
          fill="none"
          stroke="#1786b1"
          strokeWidth="12"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={dashOffset}
        />
      </svg>
      <div className="absolute inset-0 flex items-center justify-center">
        <div className="flex h-16 w-16 flex-col items-center justify-center rounded-full bg-white shadow-none">
          <div className="text-xl font-semibold text-[#156b8b]">{ratio}%</div>
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-[var(--cm-border-subtle)] bg-white px-4 py-3">
      <div className="text-xs font-semibold tracking-normal text-[var(--cm-muted)]">{label}</div>
      <div className="mt-2 text-2xl font-semibold text-[var(--cm-ink)]">{value}</div>
    </div>
  );
}

export default AdminSecurityDashboardPage;
