import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../components/AdminLayout';
import { userService } from '../../services/userService';
import { adminInstanceService } from '../../services/adminInstanceService';
import { adminService, type ClusterResourceOverview, type ResourceSummary } from '../../services/adminService';
import { useI18n } from '../../contexts/I18nContext';

interface DashboardStats {
  totalUsers: number;
  totalInstances: number;
  runningInstances: number;
  totalStorageGB: number;
}

interface ClusterMetricCardProps {
  title: string;
  eyebrow: string;
  value: string;
  subtitle: string;
  progress: number;
  tone: 'coral' | 'amber' | 'blue' | 'slate';
}

interface NodeResourceBarProps {
  label: string;
  summary: ResourceSummary;
  format: (value: number, digits?: number) => string;
}

interface NodeSummaryRowProps {
  node: ClusterResourceOverview['nodes'][number];
  expanded: boolean;
  onToggle: () => void;
  format: (value: number, digits?: number) => string;
}

const toneStyles: Record<ClusterMetricCardProps['tone'], { shell: string; line: string; glow: string }> = {
  coral: {
    shell: 'border-[var(--cm-border)]',
    line: 'bg-[#ef6b4a]',
    glow: 'shadow-sm',
  },
  amber: {
    shell: 'border-[var(--cm-border)]',
    line: 'bg-[#d59a22]',
    glow: 'shadow-sm',
  },
  blue: {
    shell: 'border-[var(--cm-border)]',
    line: 'bg-[#3b82f6]',
    glow: 'shadow-sm',
  },
  slate: {
    shell: 'border-[var(--cm-border)]',
    line: 'bg-[#5b6478]',
    glow: 'shadow-sm',
  },
};

const AdminDashboard: React.FC = () => {
  const pageSize = 10;
  const { t } = useI18n();
  const [stats, setStats] = useState<DashboardStats>({
    totalUsers: 0,
    totalInstances: 0,
    runningInstances: 0,
    totalStorageGB: 0,
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [clusterResources, setClusterResources] = useState<ClusterResourceOverview | null>(null);
  const [expandedNode, setExpandedNode] = useState<string | null>(null);
  const [currentPage, setCurrentPage] = useState(1);

  useEffect(() => {
    const loadDashboardStats = async () => {
      try {
        setLoading(true);
        setError(null);

        const [usersData, instancesData, clusterData] = await Promise.all([
          userService.getUsers(1, 1),
          adminInstanceService.getSummary(),
          adminService.getClusterResources(),
        ]);

        setStats({
          totalUsers: usersData.total,
          totalInstances: instancesData.total,
          runningInstances: instancesData.running,
          totalStorageGB: instancesData.allocated_storage_gb,
        });
        setClusterResources(clusterData);
      } catch (err: any) {
        setError(err.response?.data?.error || t('admin.dashboardLoadFailed'));
      } finally {
        setLoading(false);
      }
    };

    loadDashboardStats();
  }, []);

  const formatValue = (value: number, digits = 1) => value.toFixed(digits).replace(/\.0$/, '');
  const usagePercent = (summary?: ResourceSummary) =>
    summary && summary.allocatable > 0 ? Math.min((summary.requested / summary.allocatable) * 100, 100) : 0;
  const readyRatio = clusterResources && clusterResources.node_count > 0
    ? Math.round((clusterResources.ready_nodes / clusterResources.node_count) * 100)
    : 0;
  const allNodes = clusterResources?.nodes || [];
  const totalPages = Math.max(1, Math.ceil(allNodes.length / pageSize));
  const pagedNodes = allNodes.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const highestPressureNode = clusterResources?.nodes.reduce<ClusterResourceOverview['nodes'][number] | null>((highest, node) => {
    const current = usagePercent(node.memory);
    const previous = highest ? usagePercent(highest.memory) : -1;
    return current > previous ? node : highest;
  }, null);

  useEffect(() => {
    setCurrentPage(1);
  }, [allNodes.length]);

  useEffect(() => {
    if (!expandedNode) {
      return;
    }

    const isExpandedNodeVisible = pagedNodes.some((node) => node.name === expandedNode);
    if (!isExpandedNodeVisible) {
      setExpandedNode(null);
    }
  }, [expandedNode, pagedNodes]);

  return (
    <AdminLayout title={t('admin.dashboardTitle')}>
      {error && (
        <div className="mb-6 rounded-3xl border border-red-200 bg-[var(--cm-subtle)] px-5 py-4 text-red-700 shadow-sm">
          {error}
        </div>
      )}

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <HeroStat label={t('admin.totalUsers')} value={loading ? '--' : `${stats.totalUsers}`} hint={t('admin.accountsWithAccess')} />
        <HeroStat label={t('admin.totalInstances')} value={loading ? '--' : `${stats.totalInstances}`} hint={t('admin.provisionedDesktops')} />
        <HeroStat label={t('admin.runningInstances')} value={loading ? '--' : `${stats.runningInstances}`} hint={t('admin.currentlyOnline')} highlight />
        <HeroStat label={t('admin.totalStorage')} value={loading ? '--' : `${stats.totalStorageGB} GB`} hint={t('admin.allocatedToInstances')} />
      </section>
      <section className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--cm-border)] bg-[var(--cm-surface)] px-4 py-3 text-sm">
        <div className="flex flex-wrap items-center gap-3">
          <span className="font-semibold text-[var(--cm-ink)]">{t('admin.readiness')}</span>
          <span className="tabular-nums text-[var(--cm-text)]">{loading || !clusterResources ? '--' : `${clusterResources.ready_nodes}/${clusterResources.node_count}`}</span>
          <span className="text-[var(--cm-muted)]">{loading || !clusterResources ? t('admin.clusterStatusPending') : t('admin.nodesHealthySchedulable', { percent: readyRatio })}</span>
        </div>
        <span className="text-[var(--cm-muted)]">{t('admin.hotNode')}: <span className="font-medium text-[var(--cm-text)]">{highestPressureNode?.name || '--'}</span></span>
      </section>

      <section className="mt-4 grid gap-4 lg:grid-cols-3">
        <CommandTile
          to="/admin/users"
          title={t('admin.userManagement')}
          description={t('admin.userManagementDesc')}
          tone="coral"
          iconPath="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z"
          openLabel={t('admin.openSection')}
        />
        <CommandTile
          to="/admin/instances"
          title={t('admin.instanceManagement')}
          description={t('admin.instanceManagementDesc')}
          tone="blue"
          iconPath="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01"
          openLabel={t('admin.openSection')}
        />
        <CommandTile
          to="/admin/settings"
          title={t('admin.systemSettings')}
          description={t('admin.systemSettingsDesc')}
          tone="amber"
          iconPath="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z"
          openLabel={t('admin.openSection')}
        />
      </section>

      <section className="mt-4">
        <div className="mb-3 flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="text-base font-semibold leading-[22px] text-[var(--cm-ink)]">{t('admin.capacityBoard')}</h2>
          </div>
          <div className="rounded-full border border-[var(--cm-border)] bg-white/80 px-4 py-2 text-sm text-[var(--cm-text)] shadow-sm">
            {loading || !clusterResources ? t('admin.waitingForClusterData') : t('admin.nodesReadySummary', { ready: clusterResources.ready_nodes, total: clusterResources.node_count })}
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <ClusterMetricCard
            tone="blue"
            eyebrow={t('admin.nodeFleet')}
            title={t('admin.nodes')}
            value={loading || !clusterResources ? '--' : `${clusterResources.ready_nodes}/${clusterResources.node_count}`}
            subtitle={t('admin.healthyNodesScheduling')}
            progress={readyRatio}
          />
          <ClusterMetricCard
            tone="coral"
            eyebrow={t('admin.computePressure')}
            title={t('common.cpu')}
            value={loading || !clusterResources ? '--' : `${formatValue(clusterResources.cpu.requested)} / ${formatValue(clusterResources.cpu.allocatable)} cores`}
            subtitle={loading || !clusterResources ? t('admin.requestedVsAllocatable') : t('admin.capacityCores', { value: formatValue(clusterResources.cpu.capacity) })}
            progress={usagePercent(clusterResources?.cpu)}
          />
          <ClusterMetricCard
            tone="amber"
            eyebrow={t('admin.memoryPressure')}
            title={t('common.memory')}
            value={loading || !clusterResources ? '--' : `${formatValue(clusterResources.memory.requested)} / ${formatValue(clusterResources.memory.allocatable)} GiB`}
            subtitle={loading || !clusterResources ? t('admin.requestedVsAllocatable') : t('admin.capacityGiB', { value: formatValue(clusterResources.memory.capacity) })}
            progress={usagePercent(clusterResources?.memory)}
          />
          <ClusterMetricCard
            tone="slate"
            eyebrow={t('admin.persistentAllocation')}
            title={t('common.disk')}
            value={loading || !clusterResources ? '--' : `${formatValue(clusterResources.disk.requested)} / ${formatValue(clusterResources.disk.allocatable)} GiB`}
            subtitle={loading || !clusterResources ? t('admin.allocatedVsAllocatable') : t('admin.capacityGiB', { value: formatValue(clusterResources.disk.capacity) })}
            progress={usagePercent(clusterResources?.disk)}
          />
        </div>
      </section>

      <section className="mt-4 overflow-hidden rounded-lg border border-[var(--cm-border)] bg-[var(--cm-subtle)] shadow-sm">
        <div className="border-b border-[var(--cm-border)] px-4 py-3">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h2 className="text-base font-semibold leading-[22px] text-[var(--cm-ink)]">{t('admin.infrastructureTable')}</h2>
            </div>
          </div>
        </div>

        <div className="overflow-x-auto">
          <table className="min-w-full">
            <thead>
              <tr className="border-b border-[var(--cm-border)] bg-[var(--cm-subtle)] text-left text-xs font-semibold text-[var(--cm-muted)]">
                <th className="px-4 py-3">{t('admin.node')}</th>
                <th className="px-4 py-3">{t('admin.health')}</th>
                <th className="px-4 py-3">{t('admin.role')}</th>
                <th className="px-4 py-3">{t('admin.internalIp')}</th>
                <th className="px-4 py-3">{t('admin.pods')}</th>
                <th className="px-4 py-3">{t('admin.cpuLoad')}</th>
                <th className="px-4 py-3">{t('admin.memoryLoad')}</th>
                <th className="px-4 py-3 text-right">{t('admin.inspect')}</th>
              </tr>
            </thead>
            <tbody>
              {pagedNodes.map((node) => (
                <React.Fragment key={node.name}>
                  <NodeSummaryRow
                    node={node}
                    expanded={expandedNode === node.name}
                    onToggle={() => setExpandedNode(expandedNode === node.name ? null : node.name)}
                    format={formatValue}
                  />
                  {expandedNode === node.name && (
                    <tr className="border-b border-[var(--cm-border)] bg-[var(--cm-subtle)]">
                      <td colSpan={8} className="px-6 py-6">
                        <div className="grid items-start gap-4 xl:grid-cols-[0.95fr_1.8fr]">
                          <div className="rounded-lg border border-[var(--cm-border)] bg-white/85 p-5 shadow-sm">
                            <p className="text-xs font-semibold text-[var(--cm-muted)]">{t('admin.nodeDossier')}</p>
                            <dl className="mt-4 space-y-4">
                              <DetailRow label={t('admin.kubeletVersion')} value={node.kubelet_version || '--'} />
                              <DetailRow label={t('admin.roles')} value={node.roles.join(', ')} />
                              <DetailRow label={t('admin.podCount')} value={`${node.pod_count}`} />
                              <DetailRow label={t('admin.diskSummary')} value={`${formatValue(node.disk.requested)} / ${formatValue(node.disk.allocatable)} ${node.disk.unit}`} />
                            </dl>
                          </div>

                          <div className="self-start rounded-lg border border-[var(--cm-border)] bg-white/75 p-5 shadow-sm">
                            <p className="text-xs font-semibold text-[var(--cm-muted)]">{t('admin.resourceProfile')}</p>
                            <div className="mt-5 grid items-start gap-5 lg:grid-cols-3">
                              <NodeResourceBar label="CPU" summary={node.cpu} format={formatValue} />
                              <NodeResourceBar label="Memory" summary={node.memory} format={formatValue} />
                              <NodeResourceBar label="Disk" summary={node.disk} format={formatValue} />
                            </div>
                          </div>
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        </div>

        {!loading && clusterResources?.nodes?.length === 0 && (
          <div className="px-6 py-16 text-center text-sm text-[var(--cm-muted)]">
            {t('admin.noNodeData')}
          </div>
        )}

        {!loading && allNodes.length > 0 && (
          <div className="flex flex-col gap-4 border-t border-[var(--cm-border)] px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm text-[var(--cm-text)]">
              {t('admin.showingNodes', { from: (currentPage - 1) * pageSize + 1, to: Math.min(currentPage * pageSize, allNodes.length), total: allNodes.length })}
            </p>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setCurrentPage((page) => Math.max(1, page - 1))}
                disabled={currentPage === 1}
                className="rounded-full border border-[var(--cm-border)] bg-white px-4 py-2 text-xs font-semibold text-[var(--cm-muted)] transition-all hover:border-[#ef6b4a] hover:text-[#ef6b4a] disabled:cursor-not-allowed disabled:opacity-40"
              >
                {t('admin.prev')}
              </button>
              <div className="rounded-full border border-[var(--cm-border)] bg-[var(--cm-subtle)] px-4 py-2 text-xs font-semibold text-[var(--cm-muted)]">
                {t('admin.pageSummary', { page: currentPage, total: totalPages })}
              </div>
              <button
                type="button"
                onClick={() => setCurrentPage((page) => Math.min(totalPages, page + 1))}
                disabled={currentPage === totalPages}
                className="rounded-full border border-[var(--cm-border)] bg-white px-4 py-2 text-xs font-semibold text-[var(--cm-muted)] transition-all hover:border-[#ef6b4a] hover:text-[#ef6b4a] disabled:cursor-not-allowed disabled:opacity-40"
              >
                {t('admin.nextPage')}
              </button>
            </div>
          </div>
        )}
      </section>
    </AdminLayout>
  );
};

function HeroStat({
  label,
  value,
  hint,
  highlight = false,
}: {
  label: string;
  value: string;
  hint: string;
  highlight?: boolean;
}) {
  return (
    <div className="min-w-0 rounded-lg border border-[var(--cm-border)] bg-[var(--cm-surface)] p-4">
      <p className="text-sm leading-5 text-[var(--cm-muted)]">{label}</p>
      <p className={`mt-2 break-words text-[32px] font-semibold leading-10 tabular-nums ${highlight ? 'text-[var(--cm-primary)]' : 'text-[var(--cm-ink)]'}`}>{value}</p>
      <p className="mt-1 text-xs leading-4 text-[var(--cm-muted)]">{hint}</p>
    </div>
  );
}

function CommandTile({
  to,
  title,
  description,
  tone,
  iconPath,
  openLabel,
}: {
  to: string;
  title: string;
  description: string;
  tone: 'coral' | 'amber' | 'blue';
  iconPath: string;
  openLabel: string;
}) {
  const accents = {
    coral: 'border-[var(--cm-border)] text-[#ef6b4a] after:bg-[var(--cm-subtle)]',
    amber: 'border-[var(--cm-border)] text-[#d59a22] after:bg-[var(--cm-subtle)]',
    blue: 'border-[var(--cm-border)] text-[#3b82f6] after:bg-[var(--cm-subtle)]',
  }[tone];

  return (
    <Link
      to={to}
      className={`min-w-0 rounded-lg border bg-[var(--cm-surface)] p-4 transition-shadow hover:shadow-sm ${accents}`}
    >
      <div className="flex items-start gap-3">
        <div className="shrink-0 rounded-lg bg-[var(--cm-subtle)] p-2">
          <svg className="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={iconPath} />
          </svg>
        </div>
        <div className="min-w-0">
          <h3 className="break-words text-base font-semibold leading-[22px] text-[var(--cm-ink)]">{title}</h3>
          <p className="mt-1 text-[13px] leading-5 text-[var(--cm-text)]">{description}</p>
          <p className="mt-2 text-xs text-[var(--cm-muted)]">{openLabel}</p>
        </div>
      </div>
    </Link>
  );
}

function ClusterMetricCard({ title, value, subtitle, progress, tone }: ClusterMetricCardProps) {
  const style = toneStyles[tone];
  return (
    <div className="min-w-0 rounded-lg border border-[var(--cm-border)] bg-[var(--cm-surface)] p-4">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-base font-semibold leading-[22px] text-[var(--cm-ink)]">{title}</h3>
        <span className="text-xs tabular-nums text-[var(--cm-muted)]">{Math.round(progress)}%</span>
      </div>
      <p className="mt-3 break-words text-xl font-semibold leading-7 text-[var(--cm-ink)] tabular-nums">{value}</p>
      <div className="mt-3 h-2 overflow-hidden rounded-full bg-[var(--cm-canvas)]">
        <div className={`h-full rounded-full transition-all ${style.line}`} style={{ width: `${progress}%` }} />
      </div>
      <p className="mt-2 text-xs leading-4 text-[var(--cm-muted)]">{subtitle}</p>
    </div>
  );
}

function NodeResourceBar({ label, summary, format }: NodeResourceBarProps) {
  const { t } = useI18n();
  const percent = summary.allocatable > 0 ? Math.min((summary.requested / summary.allocatable) * 100, 100) : 0;

  return (
    <div className="rounded-lg border border-[var(--cm-border)] bg-[var(--cm-subtle)] p-4">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-semibold text-[var(--cm-ink)]">{label}</span>
        <span className="rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-[var(--cm-muted)]">
          {Math.round(percent)}%
        </span>
      </div>
      <p className="mt-3 break-words text-base font-semibold leading-none text-[#111827] tabular-nums">
        {format(summary.requested)} / {format(summary.allocatable)} {summary.unit}
      </p>
      <p className="mt-2 text-[13px] leading-5 text-[var(--cm-muted)]">{t('admin.capacityGiB', { value: format(summary.capacity) }).replace('GiB', summary.unit)}</p>
      <div className="mt-4 h-2.5 overflow-hidden rounded-full bg-[var(--cm-subtle)]">
        <div
          className="h-full rounded-full bg-[var(--cm-primary)]"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="border-b border-[var(--cm-border)] pb-3 last:border-b-0 last:pb-0">
      <dt className="text-xs font-semibold text-[var(--cm-muted)]">{label}</dt>
      <dd className="mt-1 text-sm font-medium text-[var(--cm-ink)]">{value}</dd>
    </div>
  );
}

function NodeSummaryRow({ node, expanded, onToggle, format }: NodeSummaryRowProps) {
  const { t } = useI18n();
  return (
    <tr className="border-b border-[var(--cm-border)] text-sm transition-colors hover:bg-[var(--cm-subtle)]">
      <td className="px-4 py-3">
        <div>
          <p className="font-semibold text-[var(--cm-ink)]">{node.name}</p>
          <p className="mt-1 text-xs text-[var(--cm-muted)]">{t('admin.node')}</p>
        </div>
      </td>
      <td className="px-4 py-3">
        <span
          className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${
            node.ready ? 'bg-[#dcfce7] text-[#15803d]' : 'bg-[var(--cm-subtle)] text-[#b91c1c]'
          }`}
        >
          {node.ready ? t('admin.ready') : t('admin.notReady')}
        </span>
      </td>
      <td className="px-4 py-3 text-[var(--cm-text)]">{node.roles.join(', ')}</td>
      <td className="px-4 py-3 text-[var(--cm-text)]">{node.internal_ip || '--'}</td>
      <td className="px-4 py-3 font-medium tabular-nums text-[var(--cm-ink)]">{node.pod_count}</td>
      <td className="px-4 py-3 whitespace-nowrap tabular-nums text-[var(--cm-text)]">
        {format(node.cpu.requested)} / {format(node.cpu.allocatable)} {node.cpu.unit}
      </td>
      <td className="px-4 py-3 whitespace-nowrap tabular-nums text-[var(--cm-text)]">
        {format(node.memory.requested)} / {format(node.memory.allocatable)} {node.memory.unit}
      </td>
      <td className="px-4 py-3 text-right">
        <button
          type="button"
          onClick={onToggle}
          className={`rounded-full border px-4 py-2 text-xs font-semibold transition-all ${
            expanded
              ? 'border-[#ef6b4a] bg-[#ef6b4a] text-white'
              : 'border-[var(--cm-border)] bg-white text-[var(--cm-muted)] hover:border-[#ef6b4a] hover:text-[#ef6b4a]'
          }`}
        >
          {expanded ? t('admin.collapse') : t('admin.viewDetails')}
        </button>
      </td>
    </tr>
  );
}

export default AdminDashboard;
