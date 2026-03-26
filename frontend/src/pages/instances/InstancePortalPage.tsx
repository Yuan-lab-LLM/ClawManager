import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import UserLayout from '../../components/UserLayout';
import { useInstanceDesktopAccess } from '../../hooks/useInstanceDesktopAccess';
import { instanceService } from '../../services/instanceService';
import type { Instance } from '../../types/instance';
import { useI18n } from '../../contexts/I18nContext';

const InstancePortalPage: React.FC = () => {
  const { t } = useI18n();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const frameShellRef = useRef<HTMLElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);

  const resolveEmbedUrl = useCallback((url: string | null) => {
    if (!url) {
      return null;
    }

    if (/^https?:\/\//i.test(url)) {
      return url;
    }

    const explicitOrigin = import.meta.env.VITE_BACKEND_ORIGIN as string | undefined;
    if (explicitOrigin) {
      return new URL(url, explicitOrigin).toString();
    }

    if (window.location.port === '9002' && url.startsWith('/api/')) {
      return `${window.location.protocol}//${window.location.hostname}:9001${url}`;
    }

    return url;
  }, []);

  const loadInstances = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await instanceService.getInstances(1, 100);
      setInstances(data.instances);
      setSelectedId((currentSelectedId) => {
        if (currentSelectedId && data.instances.some((instance) => instance.id === currentSelectedId)) {
          return currentSelectedId;
        }

        const firstRunning = data.instances.find((instance) => instance.status === 'running');
        return firstRunning?.id ?? data.instances[0]?.id ?? null;
      });
    } catch (err: any) {
      setError(err.response?.data?.error || t('instances.failedToLoad'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    loadInstances();
  }, [loadInstances]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === frameShellRef.current);
    };

    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => {
      document.removeEventListener('fullscreenchange', handleFullscreenChange);
    };
  }, []);

  const selectedInstance = useMemo(
    () => instances.find((instance) => instance.id === selectedId) ?? null,
    [instances, selectedId],
  );

  const {
    embedUrl,
    expiresAt,
    loading: accessLoading,
    error: accessError,
    frameKey,
    reconnecting,
    refreshAccess,
    handleFrameLoad,
  } = useInstanceDesktopAccess({
    instanceId: selectedInstance?.id ?? null,
    isRunning: selectedInstance?.status === 'running',
    resolveEmbedUrl,
    failedMessage: t('instances.failedToGenerateAccessToken'),
  });

  const formatRemaining = () => {
    if (!expiresAt) {
      return '';
    }

    const diff = expiresAt.getTime() - Date.now();
    if (diff <= 0) {
      return t('instances.expired');
    }

    const minutes = Math.floor(diff / 60000);
    const seconds = Math.floor((diff % 60000) / 1000);
    return `${minutes}m ${seconds}s`;
  };

  const getStatusDot = (status: Instance['status']) => {
    switch (status) {
      case 'running':
        return 'bg-green-500';
      case 'creating':
        return 'bg-amber-500';
      case 'error':
        return 'bg-red-500';
      default:
        return 'bg-gray-400';
    }
  };

  const toggleFullscreen = async () => {
    const target = frameShellRef.current ?? iframeRef.current;
    if (!target) {
      return;
    }

    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      } else {
        await target.requestFullscreen();
      }
    } catch (fullscreenError) {
      console.error('Failed to toggle portal fullscreen', fullscreenError);
    }
  };

  return (
    <UserLayout title={t('instances.portalTitle')}>
      <div className="space-y-6">
        <div className="flex h-[calc(100vh-160px)] min-h-0 gap-4">
          <aside className="app-panel flex w-full max-w-[320px] flex-col">
            <div className="border-b border-[#f1e7e1] px-5 py-4">
              <h2 className="text-sm font-semibold uppercase tracking-[0.14em] text-[#8f8681]">{t('instances.portalWorkspace')}</h2>
            </div>
            <div className="flex-1 overflow-y-auto">
              {loading ? (
                <div className="p-6 text-sm text-[#8f8681]">{t('common.loading')}</div>
              ) : error ? (
                <div className="p-6 text-sm text-red-600">{error}</div>
              ) : instances.length === 0 ? (
                <div className="p-6 text-sm text-[#8f8681]">{t('instances.noInstances')}</div>
              ) : (
                <ul className="divide-y divide-[#f5ebe5]">
                  {instances.map((instance) => {
                    const isSelected = instance.id === selectedId;
                    const isRunning = instance.status === 'running';

                    return (
                      <li key={instance.id}>
                        <button
                          type="button"
                          onClick={() => setSelectedId(instance.id)}
                          className={`flex w-full items-start gap-3 px-5 py-4 text-left transition-colors ${
                            isSelected ? 'bg-[#fff7f3]' : 'hover:bg-[#fffaf7]'
                          }`}
                        >
                          <span className={`mt-1 h-2.5 w-2.5 flex-shrink-0 rounded-full ${getStatusDot(instance.status)}`} />
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center justify-between gap-3">
                              <p className={`truncate text-sm font-semibold ${isSelected ? 'text-[#dc2626]' : 'text-[#171212]'}`}>
                                {instance.name}
                              </p>
                              <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                                isRunning ? 'bg-green-100 text-green-800' : 'bg-[#f7ece6] text-[#8f5b4b]'
                              }`}>
                                {t(`status.${instance.status}`)}
                              </span>
                            </div>
                            <p className="mt-1 text-xs text-[#8f8681]">
                              {instance.os_type} {instance.os_version}
                            </p>
                            <p className="mt-2 text-xs text-[#8f8681]">
                              {instance.cpu_cores} {t('common.cpu')} · {instance.memory_gb} GB · {instance.disk_gb} GB
                            </p>
                          </div>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          </aside>

          <section
            ref={frameShellRef}
            className={`flex min-w-0 flex-1 flex-col overflow-hidden border border-[#1f2937] bg-[#111827] shadow-[0_30px_90px_-56px_rgba(17,24,39,0.9)] ${isFullscreen ? 'rounded-none' : 'rounded-[30px]'}`}
          >
            <div className="flex items-center justify-between border-b border-[#2b3443] bg-[#182131] px-4 py-3 text-white">
              <div className="min-w-0">
                <p className="truncate text-sm font-semibold">
                  {selectedInstance?.name || t('instances.portalSelectInstance')}
                </p>
                <p className="mt-1 text-xs text-[#aab4c4]">
                  {selectedInstance
                    ? accessLoading || reconnecting || !expiresAt
                      ? t('instances.generatingToken')
                      : `${t('instances.expiresIn')}: ${formatRemaining()}`
                    : t('instances.portalSelectInstanceSubtitle')}
                </p>
              </div>
              <div className="flex items-center gap-2">
                {selectedInstance && selectedInstance.status === 'running' && (
                  <button
                    onClick={() => refreshAccess({ forceReload: true })}
                    className="rounded-lg bg-[#243041] px-3 py-1.5 text-xs font-medium text-white hover:bg-[#31415a]"
                  >
                    {t('instances.refreshToken')}
                  </button>
                )}
                <button
                  type="button"
                  onClick={toggleFullscreen}
                  className="rounded-lg bg-[#243041] px-3 py-1.5 text-xs font-medium text-white hover:bg-[#31415a]"
                >
                  {isFullscreen ? (
                    <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  ) : (
                    <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" />
                    </svg>
                  )}
                </button>
              </div>
            </div>

            <div className="min-h-0 flex-1">
              {accessLoading && !embedUrl ? (
                <div className="flex h-full items-center justify-center text-sm text-[#d8dee8]">
                  {t('instances.generatingToken')}
                </div>
              ) : embedUrl ? (
                <iframe
                  key={frameKey}
                  ref={iframeRef}
                  src={embedUrl}
                  title={selectedInstance ? `${selectedInstance.name} portal` : 'desktop-portal'}
                  className="h-full w-full border-0"
                  allow="clipboard-read; clipboard-write; fullscreen; autoplay"
                  allowFullScreen
                  onLoad={() => handleFrameLoad(iframeRef.current)}
                  onError={() => refreshAccess({ forceReload: true, silent: true })}
                />
              ) : (
                <div className="flex h-full items-center justify-center px-8 text-center">
                  <div>
                    <h3 className="text-lg font-semibold text-white">
                      {selectedInstance ? t('instances.portalUnavailable') : t('instances.portalSelectInstance')}
                    </h3>
                    <p className="mt-2 text-sm text-[#b7c1cf]">
                      {accessError || (selectedInstance ? t('instances.portalUnavailableSubtitle') : t('instances.portalSelectInstanceSubtitle'))}
                    </p>
                  </div>
                </div>
              )}
            </div>
          </section>
        </div>
      </div>
    </UserLayout>
  );
};

export default InstancePortalPage;
