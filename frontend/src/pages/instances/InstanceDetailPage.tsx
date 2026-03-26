import React, { useEffect, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import ConfirmDialog from '../../components/ConfirmDialog';
import { InstanceAccess } from '../../components/InstanceAccess';
import UserLayout from '../../components/UserLayout';
import { instanceService } from '../../services/instanceService';
import type { Instance, InstanceStatus } from '../../types/instance';
import { useI18n } from '../../contexts/I18nContext';

type TabType = 'overview' | 'access';

function statusStyle(status: string) {
  switch (status) {
    case 'running':
      return {
        shell: 'border-[#bde8ca] bg-[#edfdf2] text-[#177245]',
        dot: 'bg-[#22c55e]',
        soft: 'bg-[linear-gradient(180deg,#eefcf3_0%,#f9fffb_100%)] border-[#cdeed7]',
      };
    case 'stopped':
      return {
        shell: 'border-[#d9e0e7] bg-[#f6f8fb] text-[#556070]',
        dot: 'bg-[#94a3b8]',
        soft: 'bg-[linear-gradient(180deg,#f8fafc_0%,#ffffff_100%)] border-[#dde3ea]',
      };
    case 'creating':
      return {
        shell: 'border-[#f6df9f] bg-[#fff8dd] text-[#9a6a00]',
        dot: 'bg-[#eab308]',
        soft: 'bg-[linear-gradient(180deg,#fffbed_0%,#ffffff_100%)] border-[#f3e1b7]',
      };
    case 'error':
      return {
        shell: 'border-[#f2c2c2] bg-[#fff0f0] text-[#b42318]',
        dot: 'bg-[#ef4444]',
        soft: 'bg-[linear-gradient(180deg,#fff3f3_0%,#ffffff_100%)] border-[#f2d0d0]',
      };
    default:
      return {
        shell: 'border-[#d9e0e7] bg-[#f6f8fb] text-[#556070]',
        dot: 'bg-[#94a3b8]',
        soft: 'bg-[linear-gradient(180deg,#f8fafc_0%,#ffffff_100%)] border-[#dde3ea]',
      };
  }
}

const InstanceDetailPage: React.FC = () => {
  const { t } = useI18n();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [instance, setInstance] = useState<Instance | null>(null);
  const [status, setStatus] = useState<InstanceStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<TabType>('overview');
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (id) {
      loadInstance(parseInt(id));
    }
  }, [id]);

  useEffect(() => {
    if (!id || instance?.status !== 'creating') {
      return;
    }

    const instanceId = parseInt(id);
    const intervalId = window.setInterval(() => {
      loadInstance(instanceId);
    }, 5000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [id, instance?.status]);

  const loadInstance = async (instanceId: number) => {
    try {
      setLoading(true);
      setError(null);
      const [instanceData, statusData] = await Promise.all([
        instanceService.getInstance(instanceId),
        instanceService.getInstanceStatus(instanceId)
      ]);
      setInstance(instanceData);
      setStatus(statusData);
    } catch (err: any) {
      setError(err.response?.data?.error || t('instances.failedToLoad'));
    } finally {
      setLoading(false);
    }
  };

  const handleAction = async (action: string) => {
    if (!instance) return;

    try {
      setActionLoading(action);
      switch (action) {
        case 'start':
          await instanceService.startInstance(instance.id);
          break;
        case 'stop':
          await instanceService.stopInstance(instance.id);
          break;
        case 'restart':
          await instanceService.restartInstance(instance.id);
          break;
        case 'delete':
          await instanceService.deleteInstance(instance.id);
          setShowDeleteDialog(false);
          navigate('/instances');
          return;
      }
      await loadInstance(instance.id);
    } catch (err: any) {
      alert(err.response?.data?.error || t(`instances.failedTo${action.charAt(0).toUpperCase()}${action.slice(1)}`));
    } finally {
      setActionLoading(null);
    }
  };

  const handleExportOpenClaw = async () => {
    if (!instance) return;

    try {
      setActionLoading('export-openclaw');
      const blob = await instanceService.exportOpenClawWorkspace(instance.id);
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `${instance.name || 'openclaw-workspace'}.openclaw.tar.gz`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.URL.revokeObjectURL(url);
    } catch (err: any) {
      alert(err.response?.data?.error || t('instances.exportOpenClaw'));
    } finally {
      setActionLoading(null);
    }
  };

  const handleImportOpenClaw = async (file?: File | null) => {
    if (!instance || !file) return;

    try {
      setActionLoading('import-openclaw');
      await instanceService.importOpenClawWorkspace(instance.id, file);
      alert(t('instances.importOpenClaw'));
    } catch (err: any) {
      alert(err.response?.data?.error || t('instances.importOpenClaw'));
    } finally {
      if (importInputRef.current) {
        importInputRef.current.value = '';
      }
      setActionLoading(null);
    }
  };

  if (loading) {
    return (
      <UserLayout>
        <div className="flex min-h-[60vh] items-center justify-center">
          <div className="text-lg text-gray-600">{t('instances.loadingInstance')}</div>
        </div>
      </UserLayout>
    );
  }

  if (error || !instance) {
    return (
      <UserLayout>
        <div className="flex min-h-[60vh] items-center justify-center">
          <div className="text-center">
            <p className="mb-4 text-red-600">{error || t('instances.instanceNotFound')}</p>
            <button
              onClick={() => navigate('/instances')}
              className="text-indigo-600 hover:text-indigo-800"
            >
              {t('instances.backToInstances')}
            </button>
          </div>
        </div>
      </UserLayout>
    );
  }

  const currentStatusStyle = statusStyle(instance.status);
  const timelineItems = [
    {
      label: t('instances.created'),
      value: new Date(instance.created_at).toLocaleString(),
      dot: 'bg-[#6366f1]',
    },
    instance.started_at ? {
      label: t('instances.lastStarted'),
      value: new Date(instance.started_at).toLocaleString(),
      dot: 'bg-[#22c55e]',
    } : null,
    instance.stopped_at ? {
      label: t('instances.lastStopped'),
      value: new Date(instance.stopped_at).toLocaleString(),
      dot: 'bg-[#94a3b8]',
    } : null,
  ].filter(Boolean) as Array<{ label: string; value: string; dot: string }>;

  return (
    <UserLayout>
      <ConfirmDialog
        open={showDeleteDialog}
        title={t('common.delete')}
        message={t('instances.confirmDelete')}
        confirmLabel={t('common.delete')}
        cancelLabel={t('common.cancel')}
        destructive
        loading={actionLoading === 'delete'}
        onCancel={() => setShowDeleteDialog(false)}
        onConfirm={() => handleAction('delete')}
      />

      <div className="space-y-6">
        <section className="app-panel px-5 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center">
              <div>
                <h1 className="text-2xl font-bold text-gray-900">{instance.name}</h1>
                <div className="mt-1 flex items-center">
                  <span className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-semibold ${currentStatusStyle.shell}`}>
                    <span className={`mr-2 h-2 w-2 rounded-full ${currentStatusStyle.dot}`} />
                    {t(`status.${instance.status}`)}
                  </span>
                  <span className="ml-3 text-sm text-gray-500">{t('instances.instanceIdLabel')}: {instance.id}</span>
                </div>
              </div>
            </div>
            <div className="flex space-x-2">
              {instance.status === 'running' ? (
                <button
                  onClick={() => handleAction('stop')}
                  disabled={actionLoading === 'stop'}
                  className="rounded-2xl border border-transparent bg-yellow-100 px-4 py-2 text-sm font-medium text-yellow-700 hover:bg-yellow-200 disabled:opacity-50"
                >
                  {actionLoading === 'stop' ? `${t('common.stop')}...` : t('common.stop')}
                </button>
              ) : instance.status === 'stopped' && (
                <button
                  onClick={() => handleAction('start')}
                  disabled={actionLoading === 'start'}
                  className="rounded-2xl border border-transparent bg-green-100 px-4 py-2 text-sm font-medium text-green-700 hover:bg-green-200 disabled:opacity-50"
                >
                  {actionLoading === 'start' ? `${t('common.start')}...` : t('common.start')}
                </button>
              )}

              <button
                onClick={() => handleAction('restart')}
                disabled={actionLoading === 'restart'}
                className="app-button-secondary disabled:opacity-50"
              >
                {actionLoading === 'restart' ? `${t('common.restart')}...` : t('common.restart')}
              </button>

              <button
                onClick={() => setShowDeleteDialog(true)}
                disabled={actionLoading === 'delete'}
                className="rounded-2xl border border-transparent bg-red-100 px-4 py-2 text-sm font-medium text-red-700 hover:bg-red-200 disabled:opacity-50"
              >
                {actionLoading === 'delete' ? `${t('common.delete')}...` : t('common.delete')}
              </button>
            </div>
          </div>
        </section>

        {/* Tabs */}
        <div className="mb-6 border-b border-[#eadfd8]">
          <nav className="-mb-px flex space-x-8">
            <button
              onClick={() => setActiveTab('overview')}
              className={`py-4 px-1 border-b-2 font-medium text-sm ${
                activeTab === 'overview'
                  ? 'border-indigo-500 text-indigo-600'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
              }`}
            >
              {t('common.overview')}
            </button>
            <button
              onClick={() => setActiveTab('access')}
              className={`py-4 px-1 border-b-2 font-medium text-sm ${
                activeTab === 'access'
                  ? 'border-indigo-500 text-indigo-600'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
              }`}
            >
              {t('instances.desktopAccess')}
            </button>
          </nav>
        </div>

        {/* Tab Content */}
        {activeTab === 'access' && (
          <div className="mb-6">
            <InstanceAccess
              instanceId={instance.id}
              instanceName={instance.name}
              isRunning={instance.status === 'running'}
            />
          </div>
        )}

        {activeTab === 'overview' && (
        <div className="space-y-6">
          <section className="app-panel-warm relative overflow-hidden px-6 py-6 sm:px-7">
            <div className="pointer-events-none absolute left-0 top-0 h-40 w-40 rounded-full bg-[radial-gradient(circle,rgba(239,107,74,0.14),transparent_68%)] blur-2xl" />
            <div className="pointer-events-none absolute bottom-0 right-0 h-32 w-32 rounded-full bg-[radial-gradient(circle,rgba(59,130,246,0.12),transparent_70%)] blur-2xl" />
            <div className="relative grid gap-6 xl:grid-cols-[1.5fr_0.9fr]">
              <div>
                <div className="flex flex-wrap items-center gap-3">
                  <span className={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] ${currentStatusStyle.shell}`}>
                    <span className={`mr-2 h-2 w-2 rounded-full ${currentStatusStyle.dot}`} />
                    {instance.status}
                  </span>
                  <span className="rounded-full border border-[#ead8cf] bg-white/75 px-3 py-1 text-xs font-semibold uppercase tracking-[0.14em] text-[#8f776b]">
                    {instance.type}
                  </span>
                  <span className="text-sm text-[#8b7a70]">Instance ID {instance.id}</span>
                </div>

                <div className="mt-5 max-w-3xl">
                  <h2 className="text-[2rem] font-semibold leading-[1.02] tracking-[-0.045em] text-[#1d1713] sm:text-[2.4rem]">
                    {instance.os_type} {instance.os_version}
                  </h2>
                </div>

                <div className="mt-5 grid gap-4 sm:grid-cols-3">
                  <MetricTile label={t('instances.cpuCores')} value={`${instance.cpu_cores}`} hint={t('instances.allocatedCompute')} />
                  <MetricTile label={t('common.memory')} value={`${instance.memory_gb} GB`} hint={t('instances.reservedRam')} />
                  <MetricTile label={t('common.disk')} value={`${instance.disk_gb} GB`} hint={t('instances.persistentDisk')} />
                </div>
              </div>

              <aside className="space-y-4">
                <div className={`rounded-[28px] border p-5 shadow-[0_24px_70px_-50px_rgba(72,44,24,0.42)] ${currentStatusStyle.soft}`}>
                  <div className="flex items-start justify-between gap-4">
                    <div>
                      <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-[#b46c50]">{t('instances.access')}</p>
                      <h3 className="mt-2 text-[1.6rem] font-semibold leading-none tracking-[-0.04em] text-[#1d1713]">
                        {instance.status === 'running' ? t('instances.openDesktop') : t('instances.startToAccess')}
                      </h3>
                    </div>
                    <span className={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold ${currentStatusStyle.shell}`}>
                      <span className={`mr-2 h-2 w-2 rounded-full ${currentStatusStyle.dot}`} />
                      {t(`status.${instance.status}`)}
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => setActiveTab('access')}
                    className={`mt-5 flex w-full items-center justify-between rounded-[22px] border px-5 py-4 text-left transition-all ${
                      instance.status === 'running'
                        ? 'border-[#bde8ca] bg-white/88 text-[#166534] hover:-translate-y-0.5 hover:shadow-[0_18px_36px_-28px_rgba(34,197,94,0.45)]'
                        : 'border-[#e6ded7] bg-white/82 text-[#5f5a57] hover:border-[#ef6b4a] hover:text-[#1d1713]'
                    }`}
                  >
                    <div>
                      <p className="text-base font-semibold">{t('instances.desktopAccess')}</p>
                      <p className="mt-1 text-sm text-[#7a6d66]">
                        {instance.status === 'running' ? t('instances.launchLiveSession') : t('instances.desktopAvailableAfterStart')}
                      </p>
                    </div>
                    <svg className="h-6 w-6 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 7l5 5m0 0l-5 5m5-5H6" />
                    </svg>
                  </button>
                </div>

                <div className="app-panel px-5 py-5">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{t('instances.timeline')}</p>
                  <div className="mt-4 space-y-4">
                    {timelineItems.map((item) => (
                      <div key={item.label} className="flex items-start gap-3">
                        <span className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full ${item.dot}`} />
                        <div className="min-w-0">
                          <p className="text-sm font-medium text-[#1d1713]">{item.label}</p>
                          <p className="mt-1 text-sm text-[#7a6d66]">{item.value}</p>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </aside>
            </div>
          </section>

          <div className="grid gap-6 xl:grid-cols-[1.5fr_0.9fr]">
            <div className="space-y-6">
              <section className="app-panel p-6">
                <div className="flex items-end justify-between gap-4">
                  <div>
                    <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{t('instances.instanceDossier')}</p>
                    <h2 className="mt-2 text-[1.5rem] font-semibold tracking-[-0.03em] text-[#1d1713]">{t('instances.basicInformation')}</h2>
                  </div>
                </div>
                <dl className="mt-6 grid gap-x-6 gap-y-5 sm:grid-cols-2">
                  <DetailItem label={t('common.type')} value={instance.type} />
                  <DetailItem label={t('instances.instanceImage')} value={instance.image_registry ? `${instance.image_registry}${instance.image_tag ? `:${instance.image_tag}` : ''}` : `${instance.os_type} ${instance.os_version}`} />
                  <DetailItem label={t('common.createdAt')} value={new Date(instance.created_at).toLocaleString()} />
                  <DetailItem label={t('common.lastUpdated')} value={new Date(instance.updated_at).toLocaleString()} />
                  {instance.description && (
                    <div className="sm:col-span-2">
                      <DetailItem label={t('common.description')} value={instance.description} />
                    </div>
                  )}
                </dl>
              </section>

              <section className="app-panel p-6">
                <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{t('instances.resourceConfiguration')}</p>
                <h2 className="mt-2 text-[1.5rem] font-semibold tracking-[-0.03em] text-[#1d1713]">{t('instances.allocatedProfile')}</h2>
                <div className="mt-6 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                  <ResourceStatCard value={`${instance.cpu_cores}`} label={t('instances.cpuCores')} tone="coral" />
                  <ResourceStatCard value={`${instance.memory_gb} GB`} label={t('instances.memoryReserved')} tone="blue" />
                  <ResourceStatCard value={`${instance.disk_gb} GB`} label={t('instances.persistentStorage')} tone="amber" />
                  <ResourceStatCard value={`${instance.gpu_enabled ? instance.gpu_count : 0}`} label={t('instances.gpuAttached')} tone="slate" />
                </div>
              </section>

              {status && (
                <section className="app-panel p-6">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{t('instances.kubernetesSection')}</p>
                  <h2 className="mt-2 text-[1.5rem] font-semibold tracking-[-0.03em] text-[#1d1713]">{t('instances.kubernetesStatus')}</h2>
                  <dl className="mt-6 grid gap-x-6 gap-y-5 sm:grid-cols-2">
                    {status.pod_name && <DetailItem label={t('instances.podName')} value={status.pod_name} />}
                    {status.pod_namespace && <DetailItem label={t('instances.namespace')} value={status.pod_namespace} />}
                    {status.pod_ip && <DetailItem label={t('instances.podIp')} value={status.pod_ip} />}
                    {status.pod_status && <DetailItem label={t('instances.podStatus')} value={status.pod_status} />}
                  </dl>
                </section>
              )}
            </div>

            <aside className="space-y-6">
              <section className="app-panel p-6">
                <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{t('instances.storage')}</p>
                <h2 className="mt-2 text-[1.5rem] font-semibold tracking-[-0.03em] text-[#1d1713]">{t('instances.volumeMapping')}</h2>
                <div className="mt-5 space-y-4">
                  <StorageRow label={t('instances.storageClass')} value={instance.storage_class || t('instances.defaultStorageClass')} />
                  <StorageRow label={t('instances.mountPath')} value={instance.mount_path} mono />
                </div>
              </section>

              {instance.type === 'openclaw' && (
                <section className="app-panel p-6">
                  <div className="flex items-start justify-between gap-4">
                    <div>
                      <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{t('instances.workspaceSection')}</p>
                      <h2 className="mt-2 text-[1.45rem] font-semibold tracking-[-0.03em] text-[#1d1713]">{t('instances.openClawWorkspace')}</h2>
                      <p className="mt-2 text-sm leading-6 text-[#7a6d66]">
                        {t('instances.openClawWorkspaceDesc')}
                      </p>
                    </div>
                    <span className={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold ${currentStatusStyle.shell}`}>
                      <span className={`mr-2 h-2 w-2 rounded-full ${currentStatusStyle.dot}`} />
                      {instance.status === 'running' ? t('instances.workspaceReady') : t('instances.workspacePaused')}
                    </span>
                  </div>

                  <div className="mt-5 grid gap-3">
                    <button
                      type="button"
                      onClick={handleExportOpenClaw}
                      disabled={instance.status !== 'running' || actionLoading === 'export-openclaw' || actionLoading === 'import-openclaw'}
                      className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {actionLoading === 'export-openclaw' ? t('instances.exportingOpenClaw') : t('instances.exportOpenClaw')}
                    </button>
                    <input
                      ref={importInputRef}
                      type="file"
                      accept=".tar.gz,.tgz,application/gzip,application/x-gzip,application/octet-stream"
                      className="hidden"
                      onChange={(e) => handleImportOpenClaw(e.target.files?.[0] || null)}
                    />
                    <button
                      type="button"
                      onClick={() => importInputRef.current?.click()}
                      disabled={instance.status !== 'running' || actionLoading === 'export-openclaw' || actionLoading === 'import-openclaw'}
                      className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {actionLoading === 'import-openclaw' ? t('instances.importingOpenClaw') : t('instances.importOpenClaw')}
                    </button>
                  </div>
                </section>
              )}
            </aside>
          </div>
        </div>
        )}
      </div>
    </UserLayout>
  );
};

function MetricTile({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-[24px] border border-[#ead8cf] bg-white/78 p-4 shadow-[0_24px_60px_-44px_rgba(72,44,24,0.42)]">
      <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">{label}</p>
      <p className="mt-3 whitespace-nowrap text-[1.9rem] font-semibold leading-none tracking-[-0.04em] text-[#1d1713] tabular-nums">
        {value}
      </p>
      <p className="mt-3 text-sm text-[#7a6d66]">{hint}</p>
    </div>
  );
}

function DetailItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[20px] border border-[#efe2d8] bg-[#fffaf7] px-4 py-4">
      <dt className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">{label}</dt>
      <dd className="mt-2 text-sm font-medium leading-6 text-[#1d1713]">{value}</dd>
    </div>
  );
}

function ResourceStatCard({
  value,
  label,
  tone,
}: {
  value: string;
  label: string;
  tone: 'coral' | 'blue' | 'amber' | 'slate';
}) {
  const accent = {
    coral: 'from-[#fff3ee] to-[#fffdfb] border-[#f3d4c7] text-[#ef6b4a]',
    blue: 'from-[#f3f7ff] to-[#ffffff] border-[#d8e4ff] text-[#3b82f6]',
    amber: 'from-[#fff8ec] to-[#ffffff] border-[#f1dfb3] text-[#d59a22]',
    slate: 'from-[#f5f7fb] to-[#ffffff] border-[#dfe6ef] text-[#5b6478]',
  }[tone];

  return (
    <div className={`rounded-[24px] border bg-gradient-to-br px-5 py-5 shadow-[0_20px_50px_-42px_rgba(72,44,24,0.42)] ${accent}`}>
      <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{label}</p>
      <p className="mt-4 whitespace-nowrap text-[2rem] font-semibold leading-none tracking-[-0.05em] text-[#1d1713] tabular-nums">
        {value}
      </p>
    </div>
  );
}

function StorageRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-[20px] border border-[#efe2d8] bg-[#fffaf7] px-4 py-4">
      <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">{label}</p>
      <p className={`mt-2 text-sm font-medium leading-6 text-[#1d1713] ${mono ? 'break-all font-mono text-[13px]' : ''}`}>{value}</p>
    </div>
  );
}

export default InstanceDetailPage;
