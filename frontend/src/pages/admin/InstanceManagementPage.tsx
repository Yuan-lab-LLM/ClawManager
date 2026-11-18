import React, { useCallback, useEffect, useMemo, useState, useId, useRef } from 'react';
import AdminLayout from '../../components/AdminLayout';
import ConfirmDialog from '../../components/ConfirmDialog';
import ExpandableSearch from '../../components/ExpandableSearch';
import InstanceAutoStopInfo, { InstanceAutoStopControl } from '../../components/InstanceAutoStopInfo';
import { useI18n } from '../../contexts/I18nContext';
import { instanceService } from '../../services/instanceService';
import { adminInstanceService } from '../../services/adminInstanceService';
import { userService } from '../../services/userService';
import { formatInstanceType, type Instance } from '../../types/instance';
import { INSTANCE_STATUS_FILTERS, type InstanceStatusFilter } from '../../lib/instanceListFilters';
import type { User } from '../../types/user';
import { getInstanceDisplayStatus } from '../../lib/instanceStatus';

function InstanceDetailsPopover(props: React.ComponentProps<typeof InstanceAutoStopControl> & { ownerName?: string }) {
  const { t } = useI18n();
  const id = useId();
  const panel = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const anchor = useRef<DOMRect | null>(null);
  useEffect(() => {
    const dismiss = () => {
      if (!panel.current?.matches(':popover-open')) return;
      panel.current.hidePopover();
      trigger.current?.focus({ preventScroll: true });
    };
    const scroll = (event: Event) => {
      if (!panel.current?.matches(':popover-open')) return;
      if (event.target instanceof Node && panel.current?.contains(event.target)) return;
      const rect = trigger.current?.getBoundingClientRect();
      if (rect && anchor.current && (rect.top !== anchor.current.top || rect.left !== anchor.current.left)) dismiss();
    };
    window.addEventListener('resize', dismiss);
    document.addEventListener('scroll', scroll, true);
    return () => { window.removeEventListener('resize', dismiss); document.removeEventListener('scroll', scroll, true); };
  }, []);
  const { instance } = props;
  return <>
    <button ref={trigger} type="button" popoverTarget={id} className="ml-2 rounded px-2 py-1 text-xs text-[var(--cm-primary)]" onClick={event => {
      event.stopPropagation();
      const rect = event.currentTarget.getBoundingClientRect();
      anchor.current = rect;
      if (panel.current) {
        panel.current.style.left = `${Math.max(8, Math.min(rect.left, window.innerWidth - 336))}px`;
        panel.current.style.top = `${Math.max(8, Math.min(rect.bottom + 4, window.innerHeight - 320))}px`;
      }
    }}>{t('instances.details')}</button>
    <div ref={panel} id={id} popover="auto" className="fixed inset-auto m-0 max-h-[min(300px,80dvh)] w-80 max-w-[calc(100vw-16px)] overflow-auto whitespace-normal rounded-lg border border-[var(--cm-border)] bg-[var(--cm-surface)] p-3 text-[var(--cm-text)] shadow-lg" onClick={event => event.stopPropagation()}>
      <dl className="space-y-2 text-xs">
        <div><dt className="text-[var(--cm-muted)]">{t('admin.instanceColumn')}</dt><dd className="break-all">{instance.name} · #{instance.id}</dd></div>
        <div><dt className="text-[var(--cm-muted)]">{t('admin.userColumn')}</dt><dd className="break-all">{props.ownerName || '—'} · #{instance.user_id}</dd></div>
        <div><dt className="text-[var(--cm-muted)]">{t('admin.k8sColumn')}</dt><dd className="break-all">{[instance.pod_name, instance.pod_namespace, instance.pod_ip].filter(Boolean).join(' / ') || '—'}</dd></div>
        <div><dt className="text-[var(--cm-muted)]">{t('admin.updatedAtLabel')}</dt><dd>{new Date(instance.updated_at).toLocaleString()}</dd></div>
      </dl>
      <InstanceAutoStopInfo instance={props.instance} />
      <InstanceAutoStopControl {...props} />
    </div>
  </>;
}

const InstanceManagementPage: React.FC = () => {
  const { t } = useI18n();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<InstanceStatusFilter>('all');
  const [typeFilter, setTypeFilter] = useState<'all' | Instance['type']>('all');
  const [modeFilter, setModeFilter] = useState<'all' | Instance['instance_mode']>('all');
  const [runtimeFilter, setRuntimeFilter] = useState<'all' | Instance['runtime_type']>('all');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [pendingDeleteInstance, setPendingDeleteInstance] = useState<Instance | null>(null);
  const pendingLoad = useRef(false);
  const refreshAfterLoad = useRef(false);

  const loadData = useCallback(async (options?: { silent?: boolean }): Promise<void> => {
    if (pendingLoad.current) {
      if (!options?.silent) refreshAfterLoad.current = true;
      return;
    }
    pendingLoad.current = true;
    if (!options?.silent) setLoading(true);
    try {
      do {
        refreshAfterLoad.current = false;
        setError(null);
        try {
          const [instancesData, usersData] = await Promise.all([
            adminInstanceService.getInstances(1, 1000),
            userService.getUsers(1, 1000),
          ]);
          setInstances(instancesData.instances || []);
          setUsers(usersData.users || []);
        } catch (err: unknown) {
          setError(getAdminErrorMessage(err, t('admin.failedToLoadInstances')));
        }
      } while (refreshAfterLoad.current);
    } finally {
      pendingLoad.current = false;
      if (!options?.silent) {
        setLoading(false);
      }
    }
  }, [t]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  useEffect(() => {
    const refresh = () => { if (document.visibilityState !== 'hidden') void loadData({ silent: true }); };
    const transient = instances.some((instance) => instance.status === 'creating' || instance.status === 'deleting');
    const intervalId = window.setInterval(refresh, transient ? 5000 : 30000);
    window.addEventListener('focus', refresh);
    window.addEventListener('online', refresh);

    return () => {
      window.clearInterval(intervalId);
      window.removeEventListener('focus', refresh);
      window.removeEventListener('online', refresh);
    };
  }, [instances, loadData]);

  const userMap = useMemo(() => {
    return new Map(users.map((user) => [user.id, user.username]));
  }, [users]);

  const filteredInstances = useMemo(() => {
    return instances.filter((instance) => {
      if (statusFilter !== 'all' && getInstanceDisplayStatus(instance.status, instance.stopped_reason) !== statusFilter) {
        return false;
      }
      if (typeFilter !== 'all' && instance.type !== typeFilter) {
        return false;
      }
      if (modeFilter !== 'all' && instance.instance_mode !== modeFilter) {
        return false;
      }
      if (runtimeFilter !== 'all' && instance.runtime_type !== runtimeFilter) {
        return false;
      }
      if (!searchQuery) {
        return true;
      }

      const query = searchQuery.toLowerCase();
      const username = userMap.get(instance.user_id)?.toLowerCase() || '';
      return [
        instance.name,
        instance.type,
        instance.instance_mode,
        instance.runtime_type,
        instance.os_type,
        instance.os_version,
        instance.pod_name || '',
        instance.pod_namespace || '',
        instance.pod_ip || '',
        username,
        String(instance.user_id),
      ].some((value) => value.toLowerCase().includes(query));
    });
  }, [instances, modeFilter, runtimeFilter, searchQuery, statusFilter, typeFilter, userMap]);

  const typeOptions = useMemo(() => {
    return Array.from(new Set(instances.map((instance) => instance.type))).sort();
  }, [instances]);

  const modeStats = useMemo(() => {
    return instances.reduce(
      (stats, instance) => {
        const mode = instance.instance_mode === 'pro' ? 'pro' : 'lite';
        stats[mode].total += 1;
        if (instance.status === 'running') {
          stats[mode].running += 1;
        }
        if (instance.status === 'creating') {
          stats[mode].creating += 1;
        }
        return stats;
      },
      {
        lite: { total: 0, running: 0, creating: 0 },
        pro: { total: 0, running: 0, creating: 0 },
      },
    );
  }, [instances]);

  const handleAction = async (instance: Instance, action: 'start' | 'stop' | 'restart' | 'delete' | 'sync') => {
    const actionKey = `${action}-${instance.id}`;
    try {
      setActionLoading(actionKey);
      if (action === 'start') {
        await instanceService.startInstance(instance.id);
      } else if (action === 'stop') {
        await instanceService.stopInstance(instance.id);
      } else if (action === 'restart') {
        await instanceService.restartInstance(instance.id);
      } else if (action === 'delete') {
        await instanceService.deleteInstance(instance.id);
        setPendingDeleteInstance(null);
      } else if (action === 'sync') {
        await instanceService.forceSyncInstance(instance.id);
      }

      await loadData();
    } catch (err: unknown) {
      const fallbackMap = {
        start: t('admin.failedToStartInstance'),
        stop: t('admin.failedToStopInstance'),
        restart: t('admin.failedToRestartInstance'),
        sync: t('admin.failedToSyncInstance'),
        delete: t('admin.failedToDeleteInstance'),
      } as const;
      setError(getAdminErrorMessage(err, fallbackMap[action]));
    } finally {
      setActionLoading(null);
    }
  };

  const getStatusBadge = (status: Instance['status']) => {
    switch (status) {
      case 'running':
        return 'bg-green-100 text-green-800';
      case 'stopped':
        return 'bg-gray-100 text-gray-700';
      case 'creating':
        return 'bg-yellow-100 text-yellow-800';
      case 'error':
        return 'bg-red-100 text-red-700';
      case 'deleting':
        return 'bg-orange-100 text-orange-700';
      default:
        return 'bg-gray-100 text-gray-700';
    }
  };

  const getModeBadge = (mode: Instance['instance_mode']) => {
    return mode === 'pro'
      ? 'border-indigo-200 bg-indigo-50 text-indigo-700'
      : 'border-sky-200 bg-sky-50 text-sky-700';
  };

  const formatMode = (mode: Instance['instance_mode']) => {
    return mode === 'pro' ? 'Pro' : 'Lite';
  };

  const formatRuntime = (runtimeType: Instance['runtime_type']) => {
    if (runtimeType === 'gateway') {
      return 'Gateway';
    }
    if (runtimeType === 'shell') {
      return 'Shell';
    }
    return 'Desktop';
  };

  const formatResources = (instance: Instance) => {
    return `${instance.cpu_cores} CPU / ${instance.memory_gb} GB / ${instance.disk_gb} GB`;
  };

  return (
    <AdminLayout title={t('admin.instanceManagement')}>
      <ConfirmDialog
        open={pendingDeleteInstance !== null}
        title={t('common.delete')}
        message={
          pendingDeleteInstance
            ? t('admin.confirmDeleteInstance', { name: pendingDeleteInstance.name })
            : ''
        }
        confirmLabel={t('common.delete')}
        cancelLabel={t('common.cancel')}
        destructive
        loading={
          pendingDeleteInstance !== null &&
          actionLoading === `delete-${pendingDeleteInstance.id}`
        }
        onCancel={() => setPendingDeleteInstance(null)}
        onConfirm={() => {
          if (pendingDeleteInstance) {
            handleAction(pendingDeleteInstance, 'delete');
          }
        }}
      />

      <div className="space-y-6">
        <div className="app-panel p-4">
          <div className="flex flex-wrap items-center gap-2">
            <ExpandableSearch value={searchQuery} onChange={setSearchQuery} label={t('common.search')} placeholder={t('admin.instanceSearchPlaceholder')} />
            <select
              aria-label={t("instances.statusFilter")}
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value as InstanceStatusFilter)}
              className="app-input"
            >
              <option value="all">{t('admin.allStatuses')}</option>
              {INSTANCE_STATUS_FILTERS.map((status) => (
                <option key={status} value={status}>{t(`status.${status}`)}</option>
              ))}
            </select>
            <select
              aria-label={t("instances.instanceTypeFilter")}
              value={typeFilter}
              onChange={(e) => setTypeFilter(e.target.value as 'all' | Instance['type'])}
              className="app-input"
            >
              <option value="all">{t('admin.allTypes')}</option>
              {typeOptions.map((type) => (
                <option key={type} value={type}>
                  {formatInstanceType(type)}
                </option>
              ))}
            </select>
            <select
              value={modeFilter}
              onChange={(e) => setModeFilter(e.target.value as 'all' | Instance['instance_mode'])}
              className="app-input"
            >
              <option value="all">All modes</option>
              <option value="lite">Lite</option>
              <option value="pro">Pro</option>
            </select>
            <select
              value={runtimeFilter}
              onChange={(e) => setRuntimeFilter(e.target.value as 'all' | Instance['runtime_type'])}
              className="app-input"
            >
              <option value="all">All backends</option>
              <option value="gateway">Gateway</option>
              <option value="desktop">Desktop</option>
              <option value="shell">Shell</option>
            </select>

          </div>
        </div>

        <div className="grid gap-3 md:grid-cols-2">
          {(['lite', 'pro'] as const).map((mode) => {
            const stats = modeStats[mode];
            return (
              <section key={mode} className="app-panel p-4">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold text-[var(--cm-ink)]">{formatMode(mode)}</div>
                    <div className="mt-1 text-xs text-[var(--cm-muted)]">
                      {mode === 'lite' ? 'Gateway runtime' : 'Desktop deployment'}
                    </div>
                  </div>
                  <span className={`inline-flex rounded-md border px-2 py-1 text-xs font-medium ${getModeBadge(mode)}`}>
                    {stats.running} running
                  </span>
                </div>
                <div className="mt-3 grid grid-cols-3 gap-3 text-sm">
                  <div>
                    <div className="text-xs text-[var(--cm-muted)]">Total</div>
                    <div className="mt-1 font-semibold text-[var(--cm-ink)]">{stats.total}</div>
                  </div>
                  <div>
                    <div className="text-xs text-[var(--cm-muted)]">Running</div>
                    <div className="mt-1 font-semibold text-[var(--cm-ink)]">{stats.running}</div>
                  </div>
                  <div>
                    <div className="text-xs text-[var(--cm-muted)]">Creating</div>
                    <div className="mt-1 font-semibold text-[var(--cm-ink)]">{stats.creating}</div>
                  </div>
                </div>
              </section>
            );
          })}
        </div>

        {error && (
          <div role="alert" className="flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-red-700">
            {error}
            <button type="button" className="app-button-secondary" onClick={() => void loadData()}>{t('common.retry')}</button>
          </div>
        )}

        <div className="app-panel">
          <div className="flex items-center justify-between border-b border-[var(--cm-border)] px-5 py-4">
            <div className="text-sm text-[var(--cm-muted)]">
              {t('admin.showingInstances', { filtered: filteredInstances.length, total: instances.length })}
            </div>
          </div>

          {loading ? (
            <div className="px-5 py-12 text-center text-[var(--cm-muted)]">{t('admin.loadingInstances')}</div>
          ) : filteredInstances.length === 0 ? (
            <div className="px-5 py-12 text-center text-[var(--cm-muted)]">{t('admin.noFilteredInstances')}</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[1200px] divide-y divide-[var(--cm-border)] whitespace-nowrap text-sm">
                <thead className="bg-[var(--cm-subtle)]">
                  <tr>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[var(--cm-muted)]">{t('admin.instanceColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[var(--cm-muted)]">{t('admin.userColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[var(--cm-muted)]">{t('admin.typeOsColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[var(--cm-muted)]">{t('admin.resourcesColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[var(--cm-muted)]">{t('admin.k8sColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[var(--cm-muted)]">{t('common.status')}</th>
                    <th className="px-5 py-3 text-right text-xs font-semibold uppercase tracking-[0.12em] text-[var(--cm-muted)]">{t('admin.actionsColumn')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--cm-border)]">
                  {filteredInstances.map((instance) => (
                    <tr key={instance.id} className="hover:bg-[var(--cm-subtle)]">
                      <td className="h-11 px-4 py-1 align-middle">
                        <div className="flex items-center gap-2">
                          <span className="max-w-48 truncate font-medium text-[var(--cm-ink)]" tabIndex={0} title={instance.name}>{instance.name}</span>
                          <span className="text-xs text-[var(--cm-muted)]" title={`${t('admin.updatedAtLabel')} ${new Date(instance.updated_at).toLocaleString()}`}>#{instance.id}</span>
                        </div>
                      </td>
                      <td className="h-11 px-4 py-1 align-middle">
                        <div className="flex items-center gap-2">
                          <span className="max-w-36 truncate font-medium text-[var(--cm-ink)]" tabIndex={0} title={userMap.get(instance.user_id)}>{userMap.get(instance.user_id) || `${t('admin.userColumn')} #${instance.user_id}`}</span>
                          <span className="text-xs text-[var(--cm-muted)]">#{instance.user_id}</span>
                        </div>
                      </td>
                      <td className="h-11 px-4 py-1 align-middle">
                        <div className="inline-flex items-center gap-2">
                          <span className="font-medium capitalize text-[var(--cm-ink)]">{instance.type}</span>
                          <span className={`inline-flex rounded-md border px-2 py-0.5 text-xs font-medium ${getModeBadge(instance.instance_mode)}`}>
                            {formatMode(instance.instance_mode)}
                          </span>
                        </div>
                        <span className="ml-2 text-xs text-[var(--cm-muted)]">{instance.os_type} {instance.os_version}</span>
                        <span className="ml-2 text-xs text-[var(--cm-muted)]">{formatRuntime(instance.runtime_type)}</span>
                      </td>
                      <td className="h-11 px-4 py-1 align-middle">
                        <span className="font-medium text-[var(--cm-ink)]">{formatResources(instance)}</span>
                        <span className="ml-2 text-xs text-[var(--cm-muted)]">{t('admin.gpuLabel')}: {instance.gpu_enabled ? instance.gpu_count : 0}</span>
                      </td>
                      <td className="h-11 px-4 py-1 align-middle">
                        <span className="inline-block max-w-48 truncate align-middle text-xs text-[var(--cm-text)]" tabIndex={0} title={[instance.instance_mode === 'lite' ? 'Gateway binding' : 'Deployment', instance.pod_name || '-', instance.pod_namespace || '-', instance.pod_ip || '-'].join(' / ')}>{instance.pod_name || (instance.instance_mode === 'lite' ? 'Gateway binding' : 'Deployment')}</span>
                      </td>
                      <td className="h-11 px-4 py-1 align-middle">
                        <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium ${getStatusBadge(instance.status)}`}>
                          {t(`status.${getInstanceDisplayStatus(instance.status, instance.stopped_reason)}`)}
                        </span>
                        <InstanceDetailsPopover instance={instance} ownerName={userMap.get(instance.user_id)} onChanged={activity => setInstances(current => current.map(item => item.id === instance.id ? { ...item, last_gateway_activity_at: activity.last_gateway_activity_at, monitoring_since: activity.monitoring_since, auto_stop_disabled: activity.auto_stop_disabled, stopped_reason: activity.stopped_reason, auto_stop_blocker: activity.auto_stop_blocker, auto_stop_phase: activity.stop_phase } : item))} />
                      </td>
                      <td className="h-11 px-4 py-1 align-middle">
                        <div className="flex justify-end gap-2">
                          {instance.status === 'stopped' && (
                            <button
                              onClick={() => handleAction(instance, 'start')}
                              disabled={actionLoading === `start-${instance.id}`}
                              className="rounded-md bg-green-100 px-3 py-1.5 text-xs font-medium text-green-700 hover:bg-green-200 disabled:opacity-50"
                            >
                              {t('common.start')}
                            </button>
                          )}
                          {instance.status === 'running' && (
                            <button
                              onClick={() => handleAction(instance, 'stop')}
                              disabled={actionLoading === `stop-${instance.id}`}
                              className="rounded-md bg-yellow-100 px-3 py-1.5 text-xs font-medium text-yellow-700 hover:bg-yellow-200 disabled:opacity-50"
                            >
                              {t('common.stop')}
                            </button>
                          )}
                          {(instance.status === 'running' || instance.status === 'stopped') && (
                            <button
                              onClick={() => handleAction(instance, 'restart')}
                              disabled={actionLoading === `restart-${instance.id}`}
                              className="rounded-md bg-[var(--cm-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--cm-primary)] hover:bg-[var(--cm-hover)] disabled:opacity-50"
                            >
                              {t('common.restart')}
                            </button>
                          )}
                          <button
                            onClick={() => handleAction(instance, 'sync')}
                            disabled={actionLoading === `sync-${instance.id}` || instance.status === 'deleting'}
                            className="rounded-md bg-[var(--cm-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--cm-text)] hover:bg-[var(--cm-subtle)] disabled:opacity-50"
                          >
                            {t('common.refresh')}
                          </button>
                          <button
                            onClick={() => setPendingDeleteInstance(instance)}
                            disabled={actionLoading === `delete-${instance.id}` || instance.status === 'deleting'}
                            className="rounded-md bg-red-50 px-3 py-1.5 text-xs font-medium text-red-600 hover:bg-red-100 disabled:opacity-50"
                          >
                            {t('common.delete')}
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </AdminLayout>
  );
};

function getAdminErrorMessage(err: unknown, fallback: string) {
  const responseError = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
  if (responseError) {
    return responseError;
  }
  return err instanceof Error ? err.message : fallback;
}

export default InstanceManagementPage;



