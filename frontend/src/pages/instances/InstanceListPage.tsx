import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { Link } from 'react-router-dom';
import ConfirmDialog from '../../components/ConfirmDialog';
import UserLayout from '../../components/UserLayout';
import { instanceService } from '../../services/instanceService';
import { useInstanceStatusWebSocket } from '../../hooks/useWebSocket';
import type { Instance } from '../../types/instance';
import { useI18n } from '../../contexts/I18nContext';

type ViewMode = 'list' | 'card';
type StatusFilter = 'all' | 'running' | 'stopped' | 'creating' | 'deleting' | 'error';

const INSTANCE_FIELDS_TO_COMPARE: Array<keyof Instance> = [
  'id',
  'user_id',
  'name',
  'description',
  'type',
  'status',
  'cpu_cores',
  'memory_gb',
  'disk_gb',
  'gpu_enabled',
  'gpu_count',
  'os_type',
  'os_version',
  'image_registry',
  'image_tag',
  'storage_class',
  'mount_path',
  'pod_name',
  'pod_namespace',
  'pod_ip',
  'access_url',
  'openclaw_config_snapshot_id',
  'created_at',
  'updated_at',
  'started_at',
  'stopped_at',
];

const instancesEqual = (left: Instance, right: Instance) =>
  INSTANCE_FIELDS_TO_COMPARE.every((field) => left[field] === right[field]);

const mergeInstances = (current: Instance[], incoming: Instance[]) => {
  const currentById = new Map(current.map((instance) => [instance.id, instance]));

  return incoming.map((nextInstance) => {
    const existingInstance = currentById.get(nextInstance.id);
    return existingInstance && instancesEqual(existingInstance, nextInstance)
      ? existingInstance
      : nextInstance;
  });
};

interface InstanceItemProps {
  instance: Instance;
  actionLoading: number | null;
  deletingIds: number[];
  onStart: (id: number) => void;
  onStop: (id: number) => void;
  onRequestDelete: (id: number) => void;
  getStatusColor: (status: string) => string;
  getStatusIcon: (status: string) => React.ReactNode;
  getTypeIcon: (type: string) => string;
  t: (key: string, options?: Record<string, string | number>) => string;
}

const InstanceCardItem = React.memo(({
  instance,
  actionLoading,
  deletingIds,
  onStart,
  onStop,
  onRequestDelete,
  getStatusColor,
  getStatusIcon,
  getTypeIcon,
  t,
}: InstanceItemProps) => (
  <div className="app-panel transition-shadow duration-200 hover:shadow-[0_30px_80px_-52px_rgba(72,44,24,0.62)]">
    <div className="p-6">
      <div className="flex items-start justify-between">
        <div className="flex items-center">
          <span className="text-2xl mr-3">{getTypeIcon(instance.type)}</span>
          <div>
            <h3 className="text-lg font-semibold text-gray-900">
              {instance.name}
            </h3>
            <p className="text-sm text-gray-500">{instance.type}</p>
          </div>
        </div>
        <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-[11px] leading-5 font-medium ${getStatusColor(instance.status)}`}>
          {getStatusIcon(instance.status)}
          {t(`status.${instance.status}`)}
        </span>
      </div>

      <div className="mt-4 grid grid-cols-3 gap-4 text-center">
        <div className="bg-gray-50 rounded p-2">
          <p className="text-xs text-gray-500">{t('common.cpu')}</p>
          <p className="text-sm font-semibold text-gray-900">{instance.cpu_cores}</p>
        </div>
        <div className="bg-gray-50 rounded p-2">
          <p className="text-xs text-gray-500">{t('common.memory')}</p>
          <p className="text-sm font-semibold text-gray-900">{instance.memory_gb} GB</p>
        </div>
        <div className="bg-gray-50 rounded p-2">
          <p className="text-xs text-gray-500">{t('common.disk')}</p>
          <p className="text-sm font-semibold text-gray-900">{instance.disk_gb} GB</p>
        </div>
      </div>

      {instance.description && (
        <p className="mt-4 text-sm text-gray-600 line-clamp-2">{instance.description}</p>
      )}

      <div className="mt-4 text-xs text-gray-500">
        {instance.os_type} {instance.os_version}
      </div>
    </div>

    <div className="flex items-center justify-between border-t border-[#f1e7e1] bg-[rgba(255,248,245,0.82)] px-6 py-4">
      <div className="flex space-x-2">
        {instance.status === 'running' ? (
          <button
            onClick={() => onStop(instance.id)}
            disabled={actionLoading === instance.id}
            className="inline-flex items-center px-3 py-1.5 border border-transparent text-xs font-medium rounded text-yellow-700 bg-yellow-100 hover:bg-yellow-200 focus:outline-none disabled:opacity-50"
          >
            {actionLoading === instance.id ? `${t('common.stop')}...` : t('common.stop')}
          </button>
        ) : instance.status === 'stopped' ? (
          <button
            onClick={() => onStart(instance.id)}
            disabled={actionLoading === instance.id}
            className="inline-flex items-center px-3 py-1.5 border border-transparent text-xs font-medium rounded text-green-700 bg-green-100 hover:bg-green-200 focus:outline-none disabled:opacity-50"
          >
            {actionLoading === instance.id ? `${t('common.start')}...` : t('common.start')}
          </button>
        ) : null}
      </div>
      <div className="flex space-x-2">
        <Link
          to={`/instances/${instance.id}`}
          className="inline-flex items-center px-3 py-1.5 border border-gray-300 text-xs font-medium rounded text-gray-700 bg-white hover:bg-gray-50 focus:outline-none"
        >
          {t('instances.details')}
        </Link>
        <button
          onClick={() => onRequestDelete(instance.id)}
          disabled={deletingIds.includes(instance.id) || instance.status === 'deleting'}
          className="inline-flex items-center px-3 py-1.5 border border-transparent text-xs font-medium rounded text-red-700 bg-red-100 hover:bg-red-200 focus:outline-none disabled:opacity-50"
        >
          {deletingIds.includes(instance.id) ? `${t('common.delete')}...` : t('common.delete')}
        </button>
      </div>
    </div>
  </div>
));

const InstanceListItem = React.memo(({
  instance,
  actionLoading,
  deletingIds,
  onStart,
  onStop,
  onRequestDelete,
  getStatusColor,
  getStatusIcon,
  getTypeIcon,
  t,
}: InstanceItemProps) => (
  <li className="px-4 py-4 hover:bg-[#fffaf6] sm:px-6">
    <div className="flex items-center justify-between">
      <div className="flex-1 min-w-0">
        <div className="flex items-center">
          <span className="text-xl mr-2">{getTypeIcon(instance.type)}</span>
          <h3 className="text-lg font-medium text-[#dc2626] truncate">
            {instance.name}
          </h3>
          <span className={`ml-3 inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getStatusColor(instance.status)}`}>
            {getStatusIcon(instance.status)}
            {t(`status.${instance.status}`)}
          </span>
        </div>
        <div className="mt-1 flex items-center text-sm text-gray-500">
          <span className="mr-4">{instance.type}</span>
          <span className="mr-4">{instance.os_type} {instance.os_version}</span>
          <span className="mr-4">{instance.cpu_cores} {t('common.cpu')}</span>
          <span className="mr-4">{instance.memory_gb} GB {t('common.memory')}</span>
          <span>{instance.disk_gb} GB {t('common.disk')}</span>
        </div>
        {instance.description && (
          <p className="mt-1 text-sm text-gray-500">{instance.description}</p>
        )}
      </div>
      <div className="ml-4 flex items-center space-x-2">
        {instance.status === 'running' ? (
          <button
            onClick={() => onStop(instance.id)}
            disabled={actionLoading === instance.id}
            className="inline-flex items-center px-3 py-1.5 border border-transparent text-xs font-medium rounded text-yellow-700 bg-yellow-100 hover:bg-yellow-200 focus:outline-none disabled:opacity-50"
          >
            {actionLoading === instance.id ? `${t('common.stop')}...` : t('common.stop')}
          </button>
        ) : instance.status === 'stopped' ? (
          <button
            onClick={() => onStart(instance.id)}
            disabled={actionLoading === instance.id}
            className="inline-flex items-center px-3 py-1.5 border border-transparent text-xs font-medium rounded text-green-700 bg-green-100 hover:bg-green-200 focus:outline-none disabled:opacity-50"
          >
            {actionLoading === instance.id ? `${t('common.start')}...` : t('common.start')}
          </button>
        ) : null}

        <Link
          to={`/instances/${instance.id}`}
          className="inline-flex items-center px-3 py-1.5 border border-gray-300 text-xs font-medium rounded text-gray-700 bg-white hover:bg-gray-50 focus:outline-none"
        >
          {t('instances.details')}
        </Link>

        <button
          onClick={() => onRequestDelete(instance.id)}
          disabled={deletingIds.includes(instance.id) || instance.status === 'deleting'}
          className="inline-flex items-center px-3 py-1.5 border border-transparent text-xs font-medium rounded text-red-700 bg-red-100 hover:bg-red-200 focus:outline-none disabled:opacity-50"
        >
          {deletingIds.includes(instance.id) ? `${t('common.delete')}...` : t('common.delete')}
        </button>
      </div>
    </div>
  </li>
));

const InstanceListPage: React.FC = () => {
  const { t } = useI18n();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [deletingIds, setDeletingIds] = useState<number[]>([]);
  const [actionLoading, setActionLoading] = useState<number | null>(null);
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null);
  
  // View and filter states
  const [viewMode, setViewMode] = useState<ViewMode>('list');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [searchQuery, setSearchQuery] = useState('');

  const loadInstances = useCallback(async (options?: { silent?: boolean }) => {
    try {
      if (!options?.silent) {
        setLoading(true);
      }
      setError(null);
      const data = await instanceService.getInstances();
      setInstances((prevInstances) => mergeInstances(prevInstances, data.instances));
    } catch (err: any) {
      setError(err.response?.data?.error || t('instances.failedToLoad'));
    } finally {
      if (!options?.silent) {
        setLoading(false);
      }
    }
  }, [t]);

  useEffect(() => {
    void loadInstances();
  }, [loadInstances]);

  useEffect(() => {
    if (!instances.some((instance) => instance.status === 'creating' || instance.status === 'deleting')) {
      return;
    }

    const intervalId = window.setInterval(() => {
      void loadInstances({ silent: true });
    }, 5000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [instances, loadInstances]);

  // Handle WebSocket status updates
  const handleStatusUpdate = useCallback((update: { instance_id: number; status: string; pod_name?: string; pod_ip?: string }) => {
    setInstances(prevInstances => 
      prevInstances.map(instance => 
        instance.id === update.instance_id
          ? (() => {
              const nextInstance = {
                ...instance,
                status: update.status as Instance['status'],
                pod_name: update.pod_name,
                pod_ip: update.pod_ip,
              };

              return instancesEqual(instance, nextInstance) ? instance : nextInstance;
            })()
          : instance
      )
    );
  }, []);

  // Setup WebSocket listener
  const { isConnected } = useInstanceStatusWebSocket(handleStatusUpdate);

  // Filter instances based on search and status
  const filteredInstances = useMemo(() => {
    return instances.filter(instance => {
      // Status filter
      if (statusFilter !== 'all' && instance.status !== statusFilter) {
        return false;
      }
      
      // Search filter
      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        const matchesName = instance.name.toLowerCase().includes(query);
        const matchesType = instance.type.toLowerCase().includes(query);
        const matchesOS = instance.os_type.toLowerCase().includes(query);
        return matchesName || matchesType || matchesOS;
      }
      
      return true;
    });
  }, [instances, statusFilter, searchQuery]);

  const handleDelete = useCallback(async (id: number) => {
    try {
      setDeletingIds((prevIds) => [...prevIds, id]);
      await instanceService.deleteInstance(id);
      setInstances((prevInstances) =>
        prevInstances.map((instance) =>
          instance.id === id ? { ...instance, status: 'deleting' } : instance,
        ),
      );
      setPendingDeleteId(null);
      await loadInstances({ silent: true });
    } catch (err: any) {
      alert(err.response?.data?.error || t('instances.failedToDelete'));
    } finally {
      setDeletingIds((prevIds) => prevIds.filter((deletingId) => deletingId !== id));
    }
  }, [loadInstances, t]);

  const handleStart = useCallback(async (id: number) => {
    try {
      setActionLoading(id);
      await instanceService.startInstance(id);
      await loadInstances({ silent: true });
    } catch (err: any) {
      alert(err.response?.data?.error || t('instances.failedToStart'));
    } finally {
      setActionLoading(null);
    }
  }, [loadInstances, t]);

  const handleStop = useCallback(async (id: number) => {
    try {
      setActionLoading(id);
      await instanceService.stopInstance(id);
      await loadInstances({ silent: true });
    } catch (err: any) {
      alert(err.response?.data?.error || t('instances.failedToStop'));
    } finally {
      setActionLoading(null);
    }
  }, [loadInstances, t]);

  const handleRequestDelete = useCallback((id: number) => {
    setPendingDeleteId(id);
  }, []);

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running':
        return 'bg-green-100 text-green-800';
      case 'stopped':
        return 'bg-gray-100 text-gray-800';
      case 'creating':
        return 'bg-yellow-100 text-yellow-800';
      case 'deleting':
        return 'bg-orange-100 text-orange-800';
      case 'error':
        return 'bg-red-100 text-red-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'running':
        return (
          <svg className="w-2 h-2 mr-1.5 text-green-600" fill="currentColor" viewBox="0 0 8 8">
            <circle cx="4" cy="4" r="3" />
          </svg>
        );
      case 'stopped':
        return (
          <svg className="w-2 h-2 mr-1.5 text-gray-600" fill="currentColor" viewBox="0 0 8 8">
            <circle cx="4" cy="4" r="3" />
          </svg>
        );
      case 'creating':
        return (
          <svg className="animate-spin w-3 h-3 mr-1.5 text-yellow-600" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
          </svg>
        );
      case 'deleting':
        return (
          <svg className="animate-spin w-3 h-3 mr-1.5 text-orange-600" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
          </svg>
        );
      default:
        return null;
    }
  };

  const getTypeIcon = (type: string) => {
    switch (type) {
      case 'ubuntu':
        return 'UB';
      case 'debian':
        return 'DB';
      case 'centos':
        return 'CE';
      case 'openclaw':
        return 'OC';
      case 'hermes':
        return 'HM';
      default:
        return 'VM';
    }
  };

  // Card View Component
  const CardView = () => (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      {filteredInstances.map((instance) => (
        <InstanceCardItem
          key={instance.id}
          instance={instance}
          actionLoading={actionLoading}
          deletingIds={deletingIds}
          onStart={handleStart}
          onStop={handleStop}
          onRequestDelete={handleRequestDelete}
          getStatusColor={getStatusColor}
          getStatusIcon={getStatusIcon}
          getTypeIcon={getTypeIcon}
          t={t}
        />
      ))}
    </div>
  );

  // List View Component
  const ListView = () => (
    <div className="app-panel overflow-hidden">
      <ul className="divide-y divide-[#f1e7e1]">
        {filteredInstances.map((instance) => (
          <InstanceListItem
            key={instance.id}
            instance={instance}
            actionLoading={actionLoading}
            deletingIds={deletingIds}
            onStart={handleStart}
            onStop={handleStop}
            onRequestDelete={handleRequestDelete}
            getStatusColor={getStatusColor}
            getStatusIcon={getStatusIcon}
            getTypeIcon={getTypeIcon}
            t={t}
          />
        ))}
      </ul>
    </div>
  );

  return (
    <UserLayout title={t('instances.listTitle')}>
      <ConfirmDialog
        open={pendingDeleteId !== null}
        title={t('common.delete')}
        message={t('instances.confirmDelete')}
        confirmLabel={t('common.delete')}
        cancelLabel={t('common.cancel')}
        destructive
        loading={pendingDeleteId !== null && deletingIds.includes(pendingDeleteId)}
        onCancel={() => setPendingDeleteId(null)}
        onConfirm={() => {
          if (pendingDeleteId !== null) {
            handleDelete(pendingDeleteId);
          }
        }}
      />

      {/* Page Description and Connection Status */}
      <div className="mb-6 flex items-center justify-between">
        <p className="text-gray-600">
          {t('userDashboard.subtitle')}
        </p>
        <div className="flex items-center text-sm">
          <span className="mr-2 text-gray-500">{t('instances.realTimeUpdates')}:</span>
          <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
            isConnected 
              ? 'bg-green-100 text-green-800' 
              : 'bg-gray-100 text-gray-800'
          }`}>
            <span className={`w-2 h-2 rounded-full mr-1.5 ${
              isConnected ? 'bg-green-500 animate-pulse' : 'bg-gray-400'
            }`} />
            {isConnected ? t('instances.connected') : t('instances.disconnected')}
          </span>
        </div>
      </div>

      {/* Toolbar */}
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-3 sm:flex-row">
          <Link to="/instances/new" className="app-button-primary">
            <svg className="w-5 h-5 mr-2" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            {t('instances.createInstance')}
          </Link>
          <Link
            to="/portal"
            className="inline-flex items-center px-4 py-2 rounded-xl border border-[#eadfd8] bg-white text-sm font-medium text-[#5f5957] shadow-sm hover:bg-[#fff8f5]"
          >
            <svg className="mr-2 h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L6 20.75V17H4a2 2 0 01-2-2V5a2 2 0 012-2h16a2 2 0 012 2v10a2 2 0 01-2 2h-2v3.75L14.25 17h-4.5z" />
            </svg>
            {t('instances.portalView')}
          </Link>
        </div>

        {/* Search and Filter */}
        <div className="flex flex-col sm:flex-row gap-3">
          {/* Search Input */}
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
              <svg className="h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </div>
            <input
              type="text"
              placeholder={t('instances.searchPlaceholder')}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="block w-full sm:w-64 pl-10 pr-3 py-2 border border-[#eadfd8] rounded-xl leading-5 bg-white placeholder-[#9c938e] focus:outline-none focus:placeholder-[#9c938e] focus:ring-1 focus:ring-[#f3d2c2] focus:border-[#ef4444] sm:text-sm"
            />
          </div>

          {/* Status Filter */}
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
            className="block w-full sm:w-auto pl-3 pr-10 py-2 text-base border-[#eadfd8] focus:outline-none focus:ring-[#f3d2c2] focus:border-[#ef4444] sm:text-sm rounded-xl"
          >
            <option value="all">{t('status.all')}</option>
            <option value="running">{t('status.running')}</option>
            <option value="stopped">{t('status.stopped')}</option>
            <option value="creating">{t('status.creating')}</option>
            <option value="deleting">{t('status.deleting')}</option>
            <option value="error">{t('status.error')}</option>
          </select>

          {/* View Mode Toggle */}
          <div className="flex rounded-xl shadow-sm" role="group">
            <button
              onClick={() => setViewMode('list')}
              className={`relative inline-flex items-center px-4 py-2 rounded-l-xl border text-sm font-medium focus:outline-none ${
                viewMode === 'list'
                  ? 'bg-[#ef4444] text-white border-[#ef4444]'
                  : 'bg-white text-gray-700 border-[#eadfd8] hover:bg-[#fff8f5]'
              }`}
            >
              <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>
            <button
              onClick={() => setViewMode('card')}
              className={`relative inline-flex items-center px-4 py-2 rounded-r-xl border text-sm font-medium focus:outline-none ${
                viewMode === 'card'
                  ? 'bg-[#ef4444] text-white border-[#ef4444]'
                  : 'bg-white text-gray-700 border-[#eadfd8] hover:bg-[#fff8f5]'
              }`}
            >
              <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
              </svg>
            </button>
          </div>
        </div>
      </div>

      {/* Results Count */}
      {(searchQuery || statusFilter !== 'all') && (
        <div className="mb-4 text-sm text-gray-600">
          {t('instances.showingResults', { filtered: filteredInstances.length, total: instances.length })}
          {searchQuery && ` • ${t('common.search')}: "${searchQuery}"`}
          {statusFilter !== 'all' && ` • ${t('common.status')}: ${t(`status.${statusFilter}`)}`}
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center h-64">
          <div className="text-lg text-gray-600">{t('common.loading')}</div>
        </div>
      ) : error ? (
        <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded">
          {error}
          <button 
            onClick={() => setError(null)}
            className="float-right text-red-500 hover:text-red-700"
          >
            ×
          </button>
        </div>
      ) : instances.length === 0 ? (
        <div className="bg-white shadow rounded-lg p-12 text-center">
          <svg 
            className="mx-auto h-12 w-12 text-gray-400" 
            fill="none" 
            viewBox="0 0 24 24" 
            stroke="currentColor"
          >
            <path 
              strokeLinecap="round" 
              strokeLinejoin="round" 
              strokeWidth={2} 
              d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" 
            />
          </svg>
          <h3 className="mt-2 text-sm font-medium text-gray-900">{t('instances.noInstances')}</h3>
          <p className="mt-1 text-sm text-gray-500">
            {t('instances.noInstancesSubtitle')}
          </p>
          <div className="mt-6">
            <Link
              to="/instances/new"
              className="inline-flex items-center px-4 py-2 border border-transparent shadow-sm text-sm font-medium rounded-xl text-white bg-[#ef4444] hover:bg-[#dc2626]"
            >
              {t('instances.createInstance')}
            </Link>
          </div>
        </div>
      ) : filteredInstances.length === 0 ? (
        <div className="bg-white shadow rounded-lg p-12 text-center">
          <svg 
            className="mx-auto h-12 w-12 text-gray-400" 
            fill="none" 
            viewBox="0 0 24 24" 
            stroke="currentColor"
          >
            <path 
              strokeLinecap="round" 
              strokeLinejoin="round" 
              strokeWidth={2} 
              d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" 
            />
          </svg>
          <h3 className="mt-2 text-sm font-medium text-gray-900">{t('instances.noMatchingInstances')}</h3>
          <p className="mt-1 text-sm text-gray-500">
            {t('instances.noMatchingInstancesSubtitle')}
          </p>
          <div className="mt-6">
            <button
              onClick={() => {
                setSearchQuery('');
                setStatusFilter('all');
              }}
              className="inline-flex items-center px-4 py-2 border border-gray-300 shadow-sm text-sm font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
            >
              {t('instances.clearFilters')}
            </button>
          </div>
        </div>
      ) : (
        viewMode === 'list' ? <ListView /> : <CardView />
      )}
    </UserLayout>
  );
};

export default InstanceListPage;
