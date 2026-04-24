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
    shell: 'from-[rgba(255,255,255,0.96)] via-[rgba(255,251,247,0.96)] to-[rgba(255,255,255,0.92)] border-[#ead8cf]',
    line: 'bg-[#ef6b4a]',
    glow: 'shadow-[0_24px_70px_-52px_rgba(72,44,24,0.5)]',
  },
  amber: {
    shell: 'from-[rgba(255,255,255,0.96)] via-[rgba(255,251,247,0.96)] to-[rgba(255,255,255,0.92)] border-[#ead8cf]',
    line: 'bg-[#d59a22]',
    glow: 'shadow-[0_24px_70px_-52px_rgba(72,44,24,0.5)]',
  },
  blue: {
    shell: 'from-[rgba(255,255,255,0.96)] via-[rgba(255,251,247,0.96)] to-[rgba(255,255,255,0.92)] border-[#ead8cf]',
    line: 'bg-[#3b82f6]',
    glow: 'shadow-[0_24px_70px_-52px_rgba(72,44,24,0.5)]',
  },
  slate: {
    shell: 'from-[rgba(255,255,255,0.96)] via-[rgba(255,251,247,0.96)] to-[rgba(255,255,255,0.92)] border-[#ead8cf]',
    line: 'bg-[#5b6478]',
    glow: 'shadow-[0_24px_70px_-52px_rgba(72,44,24,0.5)]',
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
          userService.getUsers(1, 1000),
          adminInstanceService.getInstances(1, 1000),
          adminService.getClusterResources(),
        ]);

        const instances = instancesData.instances || [];
        const runningInstances = instances.filter((instance) => instance.status === 'running').length;
        const totalStorageGB = instances.reduce((sum, instance) => sum + instance.disk_gb, 0);

        setStats({
          totalUsers: usersData.total || (usersData.users || []).length,
          totalInstances: instancesData.total || instances.length,
          runningInstances,
          totalStorageGB,
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
        <div className="mb-6 rounded-3xl border border-red-200 bg-[linear-gradient(135deg,#fff0ec_0%,#fffaf8_100%)] px-5 py-4 text-red-700 shadow-[0_24px_60px_-40px_rgba(220,38,38,0.45)]">
          {error}
        </div>
      )}

      <section className="relative overflow-hidden rounded-[32px] border border-[#ead8cf] bg-[radial-gradient(circle_at_top_left,#fff8f0_0%,#fff6f0_22%,#fbf4ef_45%,#f5efe9_100%)] px-6 py-6 shadow-[0_30px_90px_-48px_rgba(132,85,52,0.45)] sm:px-8">
        <div className="pointer-events-none absolute inset-y-0 right-0 w-[40%] bg-[radial-gradient(circle_at_center,rgba(239,107,74,0.16),transparent_58%)]" />
        <div className="pointer-events-none absolute left-0 top-0 h-36 w-36 rounded-full bg-[radial-gradient(circle,rgba(213,154,34,0.18),transparent_70%)] blur-2xl" />

        <div className="relative grid gap-6 xl:grid-cols-[1.3fr_0.9fr]">
          <div>
            <div className="inline-flex items-center rounded-full border border-[#f0d4c6] bg-white/75 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-[#b46c50] backdrop-blur">
              {t('admin.clusterCommand')}
            </div>
            <div className="mt-4 max-w-3xl">
              <h2 className="text-[1.85rem] font-semibold leading-[1.02] tracking-[-0.045em] text-[#1d1713] sm:text-[2.65rem] lg:text-[3rem]">
                {t('admin.clusterHeroTitle')}
              </h2>
              <p className="mt-4 max-w-2xl text-[15px] leading-7 text-[#7a6d66]">
                {t('admin.clusterHeroDesc')}
              </p>
            </div>

            <div className="mt-7 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <HeroStat
                label={t('admin.totalUsers')}
                value={loading ? '--' : `${stats.totalUsers}`}
                hint={t('admin.accountsWithAccess')}
              />
              <HeroStat
                label={t('admin.totalInstances')}
                value={loading ? '--' : `${stats.totalInstances}`}
                hint={t('admin.provisionedDesktops')}
              />
              <HeroStat
                label={t('admin.runningInstances')}
                value={loading ? '--' : `${stats.runningInstances}`}
                hint={t('admin.currentlyOnline')}
                highlight
              />
              <HeroStat
                label={t('admin.totalStorage')}
                value={loading ? '--' : `${stats.totalStorageGB} GB`}
                hint={t('admin.allocatedToInstances')}
              />
            </div>
          </div>

          <div className="relative">
            <div className="rounded-[28px] border border-[#ead8cf] bg-white/80 p-5 shadow-[0_24px_70px_-48px_rgba(72,44,24,0.6)] backdrop-blur">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-[#b46c50]">{t('admin.readiness')}</p>
                  <h3 className="mt-2 text-[2.35rem] font-semibold leading-none tracking-[-0.05em] text-[#1d1713] tabular-nums">
                    {loading || !clusterResources ? '--' : `${clusterResources.ready_nodes}/${clusterResources.node_count}`}
                  </h3>
                  <p className="mt-2 min-h-[40px] text-[13px] leading-5 text-[#7a6d66]">
                    {loading || !clusterResources ? t('admin.clusterStatusPending') : t('admin.nodesHealthySchedulable', { percent: readyRatio })}
                  </p>
                </div>
                <div className="rounded-2xl bg-[#fff4ee] px-3 py-2 text-right">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#c27252]">{t('admin.hotNode')}</p>
                  <p className="mt-1 text-sm font-semibold text-[#1d1713]">{highestPressureNode?.name || '--'}</p>
                </div>
              </div>

              <div className="mt-5 h-3 overflow-hidden rounded-full bg-[#efe7e1]">
                <div
                  className="h-full rounded-full bg-[linear-gradient(90deg,#ef6b4a_0%,#d59a22_100%)] transition-all"
                  style={{ width: `${readyRatio}%` }}
                />
              </div>

              <div className="mt-5 grid grid-cols-2 gap-3">
                <MiniSignal
                  label={t('admin.cpuRequested')}
                  value={loading || !clusterResources ? '--' : `${formatValue(clusterResources.cpu.requested)} cores`}
                />
                <MiniSignal
                  label={t('admin.memoryRequested')}
                  value={loading || !clusterResources ? '--' : `${formatValue(clusterResources.memory.requested)} GiB`}
                />
                <MiniSignal
                  label={t('admin.diskAllocated')}
                  value={loading || !clusterResources ? '--' : `${formatValue(clusterResources.disk.requested)} GiB`}
                />
                <MiniSignal
                  label={t('admin.pressureFocus')}
                  value={highestPressureNode ? t('admin.pressureFocusMem', { percent: formatValue(usagePercent(highestPressureNode.memory), 0) }) : '--'}
                />
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="mt-8 grid gap-4 lg:grid-cols-3">
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

      <section className="mt-10">
        <div className="mb-5 flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[#b46c50]">{t('admin.clusterResources')}</p>
            <h2 className="mt-2 text-2xl font-semibold text-[#1d1713]">{t('admin.capacityBoard')}</h2>
          </div>
          <div className="rounded-full border border-[#ead8cf] bg-white/80 px-4 py-2 text-sm text-[#7a6d66] shadow-sm">
            {loading || !clusterResources ? t('admin.waitingForClusterData') : t('admin.nodesReadySummary', { ready: clusterResources.ready_nodes, total: clusterResources.node_count })}
          </div>
        </div>

        <div className="grid gap-5 xl:grid-cols-4">
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

      <section className="mt-10 overflow-hidden rounded-[30px] border border-[#ead8cf] bg-[linear-gradient(180deg,rgba(255,255,255,0.88)_0%,rgba(255,250,246,0.96)_100%)] shadow-[0_30px_80px_-52px_rgba(60,42,28,0.6)]">
        <div className="border-b border-[#efe2da] px-6 py-5">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[#b46c50]">{t('admin.nodeMatrix')}</p>
              <h2 className="mt-2 text-2xl font-semibold text-[#1d1713]">{t('admin.infrastructureTable')}</h2>
            </div>
            <p className="max-w-xl text-[13px] leading-5 text-[#7a6d66]">
              {t('admin.infrastructureTableDesc')}
            </p>
          </div>
        </div>

        <div className="overflow-x-auto">
          <table className="min-w-full">
            <thead>
              <tr className="border-b border-[#f0e3db] bg-[linear-gradient(180deg,#fffaf6_0%,#fff6f1_100%)] text-left text-[11px] font-semibold uppercase tracking-[0.18em] text-[#9d7d6e]">
                <th className="px-6 py-4">{t('admin.node')}</th>
                <th className="px-6 py-4">{t('admin.health')}</th>
                <th className="px-6 py-4">{t('admin.role')}</th>
                <th className="px-6 py-4">{t('admin.internalIp')}</th>
                <th className="px-6 py-4">{t('admin.pods')}</th>
                <th className="px-6 py-4">{t('admin.cpuLoad')}</th>
                <th className="px-6 py-4">{t('admin.memoryLoad')}</th>
                <th className="px-6 py-4 text-right">{t('admin.inspect')}</th>
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
                    <tr className="border-b border-[#f2e8e2] bg-[linear-gradient(180deg,#fffdfb_0%,#fff7f2_100%)]">
                      <td colSpan={8} className="px-6 py-6">
                        <div className="grid items-start gap-4 xl:grid-cols-[0.95fr_1.8fr]">
                          <div className="rounded-[24px] border border-[#ebdad0] bg-white/85 p-5 shadow-[0_18px_50px_-42px_rgba(72,44,24,0.7)]">
                            <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[#b46c50]">{t('admin.nodeDossier')}</p>
                            <dl className="mt-4 space-y-4">
                              <DetailRow label={t('admin.kubeletVersion')} value={node.kubelet_version || '--'} />
                              <DetailRow label={t('admin.roles')} value={node.roles.join(', ')} />
                              <DetailRow label={t('admin.podCount')} value={`${node.pod_count}`} />
                              <DetailRow label={t('admin.diskSummary')} value={`${formatValue(node.disk.requested)} / ${formatValue(node.disk.allocatable)} ${node.disk.unit}`} />
                            </dl>
                          </div>

                          <div className="self-start rounded-[24px] border border-[#ebdad0] bg-white/75 p-5 shadow-[0_18px_50px_-42px_rgba(72,44,24,0.58)]">
                            <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[#b46c50]">{t('admin.resourceProfile')}</p>
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
          <div className="px-6 py-16 text-center text-sm text-[#8f8681]">
            {t('admin.noNodeData')}
          </div>
        )}

        {!loading && allNodes.length > 0 && (
          <div className="flex flex-col gap-4 border-t border-[#f0e3db] px-6 py-5 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm text-[#7a6d66]">
              {t('admin.showingNodes', { from: (currentPage - 1) * pageSize + 1, to: Math.min(currentPage * pageSize, allNodes.length), total: allNodes.length })}
            </p>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setCurrentPage((page) => Math.max(1, page - 1))}
                disabled={currentPage === 1}
                className="rounded-full border border-[#ead7cd] bg-white px-4 py-2 text-xs font-semibold uppercase tracking-[0.14em] text-[#a05f46] transition-all hover:border-[#ef6b4a] hover:text-[#ef6b4a] disabled:cursor-not-allowed disabled:opacity-40"
              >
                {t('admin.prev')}
              </button>
              <div className="rounded-full border border-[#ead7cd] bg-[#fff8f3] px-4 py-2 text-xs font-semibold uppercase tracking-[0.14em] text-[#a05f46]">
                {t('admin.pageSummary', { page: currentPage, total: totalPages })}
              </div>
              <button
                type="button"
                onClick={() => setCurrentPage((page) => Math.min(totalPages, page + 1))}
                disabled={currentPage === totalPages}
                className="rounded-full border border-[#ead7cd] bg-white px-4 py-2 text-xs font-semibold uppercase tracking-[0.14em] text-[#a05f46] transition-all hover:border-[#ef6b4a] hover:text-[#ef6b4a] disabled:cursor-not-allowed disabled:opacity-40"
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
    <div
      className={`rounded-[24px] border p-4 shadow-[0_24px_60px_-44px_rgba(72,44,24,0.55)] backdrop-blur ${
        highlight
          ? 'border-[#f3c7b7] bg-[linear-gradient(135deg,rgba(255,241,235,0.98)_0%,rgba(255,255,255,0.95)_100%)]'
          : 'border-[#ead8cf] bg-white/78'
      }`}
    >
      <div className="grid min-h-[122px] grid-rows-[40px_minmax(0,1fr)_32px]">
        <p className="text-[13px] leading-5 text-[#8c7b72]">{label}</p>
        <p
          className={`self-center whitespace-nowrap text-[2rem] font-semibold leading-none tracking-[-0.05em] tabular-nums ${
            highlight ? 'text-[#dc2626]' : 'text-[#1d1713]'
          }`}
        >
          {value}
        </p>
        <p className="text-[10px] font-semibold uppercase leading-4 tracking-[0.16em] text-[#b09d93]">{hint}</p>
      </div>
    </div>
  );
}

function MiniSignal({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-[#eee1d8] bg-[#fffaf7] px-4 py-3">
      <div className="grid min-h-[92px] grid-rows-[28px_minmax(0,1fr)]">
        <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{label}</p>
        <p className="self-end whitespace-nowrap text-[1.75rem] font-semibold leading-none tracking-[-0.04em] text-[#1d1713] tabular-nums">{value}</p>
      </div>
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
    coral: 'border-[#ead8cf] text-[#ef6b4a] after:bg-[radial-gradient(circle,rgba(239,107,74,0.14),transparent_66%)]',
    amber: 'border-[#ead8cf] text-[#d59a22] after:bg-[radial-gradient(circle,rgba(213,154,34,0.14),transparent_66%)]',
    blue: 'border-[#ead8cf] text-[#3b82f6] after:bg-[radial-gradient(circle,rgba(59,130,246,0.14),transparent_66%)]',
  }[tone];

  return (
    <Link
      to={to}
      className={`group relative overflow-hidden rounded-[28px] border bg-[linear-gradient(180deg,rgba(255,255,255,0.98)_0%,rgba(255,249,245,0.94)_100%)] p-6 shadow-[0_24px_70px_-52px_rgba(72,44,24,0.5)] transition-all duration-300 hover:-translate-y-1 hover:shadow-[0_30px_80px_-52px_rgba(72,44,24,0.64)] after:absolute after:-right-8 after:top-[-24px] after:h-24 after:w-24 after:rounded-full ${accents}`}
    >
      <div className="relative flex items-start gap-4">
        <div className="rounded-2xl border border-[#f1e2d9] bg-white/90 p-3 shadow-[0_12px_30px_-24px_rgba(72,44,24,0.5)]">
          <svg className="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={iconPath} />
          </svg>
        </div>
        <div>
          <h3 className="text-[1.15rem] font-semibold leading-6 text-[#1d1713]">{title}</h3>
          <p className="mt-1 text-[13px] leading-5 text-[#7a6d66]">{description}</p>
          <p className="mt-4 text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b46c50]">{openLabel}</p>
        </div>
      </div>
    </Link>
  );
}

function ClusterMetricCard({ title, eyebrow, value, subtitle, progress, tone }: ClusterMetricCardProps) {
  const { t } = useI18n();
  const style = toneStyles[tone];

  return (
    <div className={`overflow-hidden rounded-[28px] border bg-[linear-gradient(135deg,var(--tw-gradient-from),var(--tw-gradient-via),var(--tw-gradient-to))] p-5 ${style.shell} ${style.glow}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-h-[54px]">
          <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-[#ad8f80]">{eyebrow}</p>
          <h3 className="mt-2 text-[1.35rem] font-semibold leading-6 text-[#1d1713]">{title}</h3>
        </div>
        <div className="rounded-full border border-[#efe2d9] bg-white/85 px-3 py-1 text-xs font-semibold text-[#7a6d66] shadow-[0_10px_24px_-20px_rgba(72,44,24,0.55)]">
          {Math.round(progress)}%
        </div>
      </div>
      <div className="mt-4 rounded-[22px] border border-[#f1e2d8] bg-[linear-gradient(180deg,rgba(255,255,255,0.92)_0%,rgba(255,249,245,0.88)_100%)] px-4 py-4">
        <p className="whitespace-nowrap text-[1.5rem] font-semibold leading-none tracking-[-0.04em] text-[#111827] tabular-nums sm:text-[1.65rem]">
          {value}
        </p>
        <div className="mt-4 h-2.5 overflow-hidden rounded-full bg-white/90">
          <div className={`h-full rounded-full transition-all ${style.line}`} style={{ width: `${progress}%` }} />
        </div>
        <div className="mt-4 flex items-end justify-between gap-3">
          <p className="text-[13px] leading-5 text-[#7a6d66]">{subtitle}</p>
          <p className="shrink-0 text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">{t('admin.usage')}</p>
        </div>
      </div>
    </div>
  );
}

function NodeResourceBar({ label, summary, format }: NodeResourceBarProps) {
  const { t } = useI18n();
  const percent = summary.allocatable > 0 ? Math.min((summary.requested / summary.allocatable) * 100, 100) : 0;

  return (
    <div className="rounded-[22px] border border-[#efe1d8] bg-[#fffaf7] p-4">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-semibold text-[#1d1713]">{label}</span>
        <span className="rounded-full bg-white px-2.5 py-1 text-[11px] font-semibold text-[#8a766a]">
          {Math.round(percent)}%
        </span>
      </div>
      <p className="mt-3 whitespace-nowrap text-[1.05rem] font-semibold leading-none text-[#111827] tabular-nums">
        {format(summary.requested)} / {format(summary.allocatable)} {summary.unit}
      </p>
      <p className="mt-2 text-[13px] leading-5 text-[#8c7b72]">{t('admin.capacityGiB', { value: format(summary.capacity) }).replace('GiB', summary.unit)}</p>
      <div className="mt-4 h-2.5 overflow-hidden rounded-full bg-[#eadfd8]">
        <div
          className="h-full rounded-full bg-[linear-gradient(90deg,#ef6b4a_0%,#d59a22_100%)]"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="border-b border-[#f3e7df] pb-3 last:border-b-0 last:pb-0">
      <dt className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">{label}</dt>
      <dd className="mt-1 text-sm font-medium text-[#1d1713]">{value}</dd>
    </div>
  );
}

function NodeSummaryRow({ node, expanded, onToggle, format }: NodeSummaryRowProps) {
  const { t } = useI18n();
  return (
    <tr className="border-b border-[#f2e8e2] text-sm transition-colors hover:bg-[#fffaf6]">
      <td className="px-6 py-5">
        <div>
          <p className="font-semibold text-[#1d1713]">{node.name}</p>
          <p className="mt-1 text-xs uppercase tracking-[0.14em] text-[#b09d93]">{t('admin.node')}</p>
        </div>
      </td>
      <td className="px-6 py-5">
        <span
          className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${
            node.ready ? 'bg-[#dcfce7] text-[#15803d]' : 'bg-[#fee2e2] text-[#b91c1c]'
          }`}
        >
          {node.ready ? t('admin.ready') : t('admin.notReady')}
        </span>
      </td>
      <td className="px-6 py-5 text-[#7a6d66]">{node.roles.join(', ')}</td>
      <td className="px-6 py-5 text-[#7a6d66]">{node.internal_ip || '--'}</td>
      <td className="px-6 py-5 font-medium tabular-nums text-[#1d1713]">{node.pod_count}</td>
      <td className="px-6 py-5 whitespace-nowrap tabular-nums text-[#7a6d66]">
        {format(node.cpu.requested)} / {format(node.cpu.allocatable)} {node.cpu.unit}
      </td>
      <td className="px-6 py-5 whitespace-nowrap tabular-nums text-[#7a6d66]">
        {format(node.memory.requested)} / {format(node.memory.allocatable)} {node.memory.unit}
      </td>
      <td className="px-6 py-5 text-right">
        <button
          type="button"
          onClick={onToggle}
          className={`rounded-full border px-4 py-2 text-xs font-semibold uppercase tracking-[0.14em] transition-all ${
            expanded
              ? 'border-[#ef6b4a] bg-[#ef6b4a] text-white'
              : 'border-[#ead7cd] bg-white text-[#a05f46] hover:border-[#ef6b4a] hover:text-[#ef6b4a]'
          }`}
        >
          {expanded ? t('admin.collapse') : t('admin.viewDetails')}
        </button>
      </td>
    </tr>
  );
}

export default AdminDashboard;
