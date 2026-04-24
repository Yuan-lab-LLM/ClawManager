import React, { useEffect, useMemo, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import ConfirmDialog from '../../components/ConfirmDialog';
import { useI18n } from '../../contexts/I18nContext';
import { instanceService } from '../../services/instanceService';
import { adminInstanceService } from '../../services/adminInstanceService';
import { userService } from '../../services/userService';
import type { Instance } from '../../types/instance';
import type { User } from '../../types/user';

const InstanceManagementPage: React.FC = () => {
  const { t } = useI18n();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<'all' | Instance['status']>('all');
  const [typeFilter, setTypeFilter] = useState<'all' | Instance['type']>('all');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [pendingDeleteInstance, setPendingDeleteInstance] = useState<Instance | null>(null);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    try {
      setLoading(true);
      setError(null);
      const [instancesData, usersData] = await Promise.all([
        adminInstanceService.getInstances(1, 1000),
        userService.getUsers(1, 1000),
      ]);
      setInstances(instancesData.instances || []);
      setUsers(usersData.users || []);
    } catch (err: any) {
      setError(err.response?.data?.error || t('admin.failedToLoadInstances'));
    } finally {
      setLoading(false);
    }
  };

  const userMap = useMemo(() => {
    return new Map(users.map((user) => [user.id, user.username]));
  }, [users]);

  const filteredInstances = useMemo(() => {
    return instances.filter((instance) => {
      if (statusFilter !== 'all' && instance.status !== statusFilter) {
        return false;
      }
      if (typeFilter !== 'all' && instance.type !== typeFilter) {
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
        instance.os_type,
        instance.os_version,
        instance.pod_name || '',
        instance.pod_namespace || '',
        instance.pod_ip || '',
        username,
        String(instance.user_id),
      ].some((value) => value.toLowerCase().includes(query));
    });
  }, [instances, searchQuery, statusFilter, typeFilter, userMap]);

  const typeOptions = useMemo(() => {
    return Array.from(new Set(instances.map((instance) => instance.type))).sort();
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
    } catch (err: any) {
      const fallbackMap = {
        start: t('admin.failedToStartInstance'),
        stop: t('admin.failedToStopInstance'),
        restart: t('admin.failedToRestartInstance'),
        sync: t('admin.failedToSyncInstance'),
        delete: t('admin.failedToDeleteInstance'),
      } as const;
      setError(err.response?.data?.error || fallbackMap[action]);
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
        <div className="app-panel p-5 lg:flex lg:items-center lg:justify-between">
          <div>
            <h2 className="text-lg font-semibold text-[#171212]">{t('admin.globalInstances')}</h2>
            <p className="mt-1 text-sm text-[#8f8681]">
              {t('admin.globalInstancesDesc')}
            </p>
          </div>
          <div className="flex flex-col gap-3 sm:flex-row">
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={t('admin.instanceSearchPlaceholder')}
              className="app-input min-w-[280px]"
            />
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value as 'all' | Instance['status'])}
              className="app-input"
            >
              <option value="all">{t('admin.allStatuses')}</option>
              <option value="running">{t('status.running')}</option>
              <option value="stopped">{t('status.stopped')}</option>
              <option value="creating">{t('status.creating')}</option>
              <option value="error">{t('status.error')}</option>
              <option value="deleting">{t('status.deleting')}</option>
            </select>
            <select
              value={typeFilter}
              onChange={(e) => setTypeFilter(e.target.value as 'all' | Instance['type'])}
              className="app-input"
            >
              <option value="all">{t('admin.allTypes')}</option>
              {typeOptions.map((type) => (
                <option key={type} value={type}>
                  {type}
                </option>
              ))}
            </select>
            <button
              onClick={loadData}
              className="app-button-secondary"
            >
              {t('common.refresh')}
            </button>
          </div>
        </div>

        {error && (
          <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-red-700">
            {error}
          </div>
        )}

        <div className="app-panel">
          <div className="flex items-center justify-between border-b border-[#f1e7e1] px-5 py-4">
            <div className="text-sm text-[#8f8681]">
              {t('admin.showingInstances', { filtered: filteredInstances.length, total: instances.length })}
            </div>
          </div>

          {loading ? (
            <div className="px-5 py-12 text-center text-[#8f8681]">{t('admin.loadingInstances')}</div>
          ) : filteredInstances.length === 0 ? (
            <div className="px-5 py-12 text-center text-[#8f8681]">{t('admin.noFilteredInstances')}</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-[#f1e7e1]">
                <thead className="bg-[#fcfaf8]">
                  <tr>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('admin.instanceColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('admin.userColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('admin.typeOsColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('admin.resourcesColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('admin.k8sColumn')}</th>
                    <th className="px-5 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('common.status')}</th>
                    <th className="px-5 py-3 text-right text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('admin.actionsColumn')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#f7efe9]">
                  {filteredInstances.map((instance) => (
                    <tr key={instance.id} className="hover:bg-[#fffaf7]">
                      <td className="px-5 py-4 align-top">
                        <div className="font-medium text-[#171212]">{instance.name}</div>
                        <div className="mt-1 text-xs text-[#8f8681]">
                          {t('instances.instanceIdLabel')}: {instance.id} / {t('admin.updatedAtLabel')} {new Date(instance.updated_at).toLocaleString()}
                        </div>
                      </td>
                      <td className="px-5 py-4 align-top">
                        <div className="font-medium text-[#171212]">{userMap.get(instance.user_id) || `${t('admin.userColumn')} #${instance.user_id}`}</div>
                        <div className="mt-1 text-xs text-[#8f8681]">{t('admin.userIdLabel')}: {instance.user_id}</div>
                      </td>
                      <td className="px-5 py-4 align-top">
                        <div className="font-medium capitalize text-[#171212]">{instance.type}</div>
                        <div className="mt-1 text-xs text-[#8f8681]">{instance.os_type} {instance.os_version}</div>
                      </td>
                      <td className="px-5 py-4 align-top">
                        <div className="font-medium text-[#171212]">{formatResources(instance)}</div>
                        <div className="mt-1 text-xs text-[#8f8681]">
                          {t('admin.gpuLabel')}: {instance.gpu_enabled ? instance.gpu_count : 0}
                        </div>
                      </td>
                      <td className="px-5 py-4 align-top">
                        <div className="text-sm text-[#171212]">{instance.pod_name || '-'}</div>
                        <div className="mt-1 text-xs text-[#8f8681]">{instance.pod_namespace || '-'}</div>
                        <div className="mt-1 text-xs text-[#8f8681]">{instance.pod_ip || '-'}</div>
                      </td>
                      <td className="px-5 py-4 align-top">
                        <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium ${getStatusBadge(instance.status)}`}>
                          {t(`status.${instance.status}`)}
                        </span>
                      </td>
                      <td className="px-5 py-4 align-top">
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
                              className="rounded-md bg-[#fff2ea] px-3 py-1.5 text-xs font-medium text-[#ef6b4a] hover:bg-[#fde5db] disabled:opacity-50"
                            >
                              {t('common.restart')}
                            </button>
                          )}
                          <button
                            onClick={() => handleAction(instance, 'sync')}
                            disabled={actionLoading === `sync-${instance.id}`}
                            className="rounded-md bg-[#f3f0ed] px-3 py-1.5 text-xs font-medium text-[#5f5957] hover:bg-[#ebe3dd] disabled:opacity-50"
                          >
                            {t('common.refresh')}
                          </button>
                          <button
                            onClick={() => setPendingDeleteInstance(instance)}
                            disabled={actionLoading === `delete-${instance.id}`}
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

export default InstanceManagementPage;



