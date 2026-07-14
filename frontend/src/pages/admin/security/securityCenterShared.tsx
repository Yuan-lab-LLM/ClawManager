import React, { useEffect, useMemo, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import AdminLayout from '../../../components/AdminLayout';
import { useI18n } from '../../../contexts/I18nContext';
import { adminService, type AdminSkillRecord, type SecurityScanConfig, type SecurityScanJob } from '../../../services/adminService';

export interface SecurityCenterSummary {
  total: number;
  uploaded: number;
  discovered: number;
  highRisk: number;
  mediumRisk: number;
  completed: number;
}

export function useSecurityCenterData() {
  const { t } = useI18n();
  const [skills, setSkills] = useState<AdminSkillRecord[]>([]);
  const [jobs, setJobs] = useState<SecurityScanJob[]>([]);
  const [config, setConfig] = useState<SecurityScanConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadAll = async (mode: 'initial' | 'refresh' = 'initial') => {
    try {
      if (mode === 'initial') {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError(null);
      const [skillItems, jobItems, configItem] = await Promise.all([
        adminService.listSkills(),
        adminService.listSecurityScanJobs(20),
        adminService.getSecurityConfig(),
      ]);
      setSkills(skillItems);
      setJobs(jobItems);
      setConfig(configItem);
    } catch (err: any) {
      setError(err.response?.data?.error || t('securityCenter.errors.loadCenter'));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  };

  useEffect(() => {
    void loadAll();
  }, [t]);

  useEffect(() => {
    const hasRunningJob = jobs.some((job) => job.status === 'queued' || job.status === 'running');
    if (!hasRunningJob) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadAll('refresh');
    }, 3000);
    return () => window.clearInterval(timer);
  }, [jobs]);

  const summary = useMemo<SecurityCenterSummary>(() => ({
    total: skills.length,
    uploaded: skills.filter((item) => item.source_type === 'uploaded').length,
    discovered: skills.filter((item) => item.source_type !== 'uploaded').length,
    highRisk: skills.filter((item) => item.risk_level === 'high').length,
    mediumRisk: skills.filter((item) => item.risk_level === 'medium').length,
    completed: skills.filter((item) => item.scan_status === 'completed').length,
  }), [skills]);

  return {
    skills,
    jobs,
    config,
    loading,
    refreshing,
    error,
    setError,
    setConfig,
    loadAll,
    summary,
  };
}

export function SecurityCenterShell({
  summary: _summary,
  children,
}: {
  summary: SecurityCenterSummary;
  children: React.ReactNode;
}) {
  const location = useLocation();
  const { t } = useI18n();
  const navItems = [
    { path: '/admin/security', label: t('securityCenter.nav.dashboard'), description: t('securityCenter.navDashboardDesc') },
    { path: '/admin/security/reports', label: t('securityCenter.nav.reports'), description: t('securityCenter.navReportsDesc') },
    { path: '/admin/security/scanner', label: t('securityCenter.nav.scanner'), description: t('securityCenter.navScannerDesc') },
  ];

  const isActive = (path: string) => {
    if (path === '/admin/security') {
      return location.pathname === path;
    }
    return location.pathname === path || location.pathname.startsWith(`${path}/`);
  };

  return (
    <AdminLayout title={t('securityCenter.title')}>
      <div className="space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('nav.secplane')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-trust">{t('secplane.protection.category.trust')}</Link>
          <span>/</span>
          <span className="crumb-current">{t('secplane.protection.category.scenarioSK')}</span>
        </div>
        <section className="grid grid-cols-1 gap-6 xl:grid-cols-[176px_minmax(0,1fr)]">
          <div className="xl:sticky xl:top-6 xl:self-start">
            <div className="rounded-[24px] border border-[#eadfd8] bg-[linear-gradient(180deg,#fffaf7_0%,#ffffff_100%)] p-3 shadow-[0_18px_44px_-36px_rgba(72,44,24,0.35)]">
              <div className="flex flex-col gap-2">
                {navItems.map((item) => (
                <Link
                  key={item.path}
                  to={item.path}
                  className={`group rounded-[18px] border px-3 py-3 text-left transition ${
                    isActive(item.path)
                      ? 'border-[#9fc7ee] bg-[#eef7ff] shadow-[inset_0_0_0_1px_rgba(116,173,223,0.35)]'
                      : 'border-[#eadfd8] bg-[#fffaf7] hover:border-[#d6c7be] hover:bg-white'
                  }`}
                >
                  <div className="min-w-0">
                    <div className="whitespace-nowrap text-sm font-semibold text-[#171212]">{item.label}</div>
                  </div>
                </Link>
              ))}
              </div>
            </div>
          </div>

          <div className="min-w-0">{children}</div>
        </section>
      </div>
    </AdminLayout>
  );
}

export function AnalyzerGroup({ title, items, emptyLabel }: { title: string; items: string[]; emptyLabel?: string }) {
  const { t } = useI18n();
  const safeItems = Array.isArray(items) ? items : [];
  return (
    <div className="rounded-2xl border border-[#f1e2d9] bg-white p-4">
      <div className="text-xs font-semibold uppercase tracking-[0.16em] text-[#b46c50]">{title}</div>
      {safeItems.length === 0 ? (
        <div className="mt-3 text-sm text-[#8f8681]">{emptyLabel || t('securityCenter.common.noneYet')}</div>
      ) : (
        <div className="mt-3 flex flex-wrap gap-2">
          {safeItems.map((item) => (
            <span
              key={`${title}-${item}`}
              className="inline-flex rounded-full border border-[#d9e0e7] bg-[#f6f8fb] px-3 py-1 text-xs font-semibold uppercase tracking-[0.12em] text-[#556070]"
            >
              {item}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

export function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-[#f1e2d9] bg-white px-4 py-3">
      <div className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b46c50]">{label}</div>
      <div className="mt-2 text-2xl font-semibold text-[#171212]">{value}</div>
    </div>
  );
}

export function Badge({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone: 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate';
}) {
  const toneClass =
    tone === 'green'
      ? 'border-[#bde8ca] bg-[#edfdf2] text-[#177245]'
      : tone === 'yellow'
        ? 'border-[#f5df9f] bg-[#fff8dd] text-[#9a6a00]'
        : tone === 'orange'
          ? 'border-[#f7c8a4] bg-[#fff1e6] text-[#b45309]'
          : tone === 'red'
            ? 'border-[#f2c2c2] bg-[#fff0f0] text-[#b42318]'
            : tone === 'amber'
              ? 'border-[#f1d9c7] bg-[#fff6f0] text-[#b46c50]'
              : 'border-[#d9e0e7] bg-[#f6f8fb] text-[#556070]';

  return (
    <span className={`inline-flex rounded-full border px-3 py-1 text-xs font-semibold uppercase tracking-[0.12em] ${toneClass}`}>
      {children}
    </span>
  );
}

export function splitAnalyzers(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
}

export function assetTypeLabel(value: string): string {
  if (value === 'skill') {
    return 'skill';
  }
  return value;
}

export function sourceLabel(value: string): string {
  if (value === 'uploaded') {
    return 'uploaded';
  }
  if (value === 'discovered') {
    return 'discovered';
  }
  return value;
}

export function riskLabel(value: string): string {
  switch (value) {
    case 'high':
      return 'high';
    case 'medium':
      return 'medium';
    case 'low':
      return 'low';
    case 'none':
      return 'SAFE';
    case 'completed':
      return 'completed';
    case 'running':
      return 'running';
    case 'failed':
      return 'failed';
    case 'pending':
      return 'pending';
    case 'queued':
      return 'queued';
    default:
      return value;
  }
}

export function jobStatusLabel(value: string): string {
  switch (value) {
    case 'queued':
      return 'queued';
    case 'pending':
      return 'pending';
    case 'running':
      return 'running';
    case 'completed':
      return 'completed';
    case 'failed':
      return 'failed';
    default:
      return value;
  }
}

export function scanModeLabel(value: string): string {
  return value === 'deep' ? 'deep' : 'quick';
}

export function scanScopeLabel(value: string): string {
  return value === 'full' ? 'full' : 'incremental';
}

export function formatDateTime(value: string, locale = 'en'): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(locale, {
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

export function riskTone(value: string): 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate' {
  switch (value) {
    case 'high':
      return 'red';
    case 'medium':
      return 'orange';
    case 'low':
      return 'yellow';
    case 'none':
      return 'green';
    case 'uploaded':
      return 'amber';
    default:
      return 'slate';
  }
}

export function severityTone(value: string): 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate' {
  switch (value.toUpperCase()) {
    case 'CRITICAL':
    case 'HIGH':
      return 'red';
    case 'MEDIUM':
    case 'MODERATE':
      return 'orange';
    case 'LOW':
    case 'WARNING':
      return 'yellow';
    case 'INFO':
    case 'SAFE':
      return 'green';
    default:
      return 'slate';
  }
}

export function severityLabel(value: string): string {
  switch (value.toUpperCase()) {
    case 'CRITICAL':
      return 'CRITICAL';
    case 'HIGH':
      return 'HIGH';
    case 'MEDIUM':
    case 'MODERATE':
      return 'MEDIUM';
    case 'LOW':
    case 'WARNING':
      return 'LOW';
    case 'INFO':
      return 'INFO';
    default:
      return value;
  }
}

export function scanStatusLabel(value: string): string {
  switch (value) {
    case 'completed':
      return 'completed';
    case 'pending':
      return 'pending';
    case 'failed':
      return 'failed';
    case 'running':
      return 'running';
    default:
      return value || 'unknown';
  }
}

export function scanStatusTone(value: string): 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate' {
  switch (value) {
    case 'completed':
      return 'green';
    case 'pending':
      return 'amber';
    case 'running':
      return 'orange';
    case 'failed':
      return 'red';
    default:
      return 'slate';
  }
}
