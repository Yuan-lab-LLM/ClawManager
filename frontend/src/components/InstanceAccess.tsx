import { useState, useEffect, useCallback, useRef } from 'react';
import { useI18n } from '../contexts/I18nContext';
import { useInstanceDesktopAccess } from '../hooks/useInstanceDesktopAccess';

interface InstanceAccessProps {
  instanceId: number;
  instanceName: string;
  isRunning: boolean;
}

export function InstanceAccess({ instanceId, instanceName, isRunning }: InstanceAccessProps) {
  const { t } = useI18n();
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [shouldConnect, setShouldConnect] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);
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

  const {
    embedUrl,
    expiresAt,
    loading,
    error,
    frameKey,
    reconnecting,
    refreshAccess,
    handleFrameLoad,
  } = useInstanceDesktopAccess({
    instanceId,
    isRunning: isRunning && shouldConnect,
    resolveEmbedUrl,
    failedMessage: t('instances.failedToGenerateAccessToken'),
  });

  useEffect(() => {
    setShouldConnect(false);
  }, [instanceId]);

  useEffect(() => {
    if (!isRunning) {
      setShouldConnect(false);
    }
  }, [isRunning]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === containerRef.current);
    };

    document.addEventListener('fullscreenchange', handleFullscreenChange);

    return () => {
      document.removeEventListener('fullscreenchange', handleFullscreenChange);
    };
  }, []);

  const toggleFullscreen = async () => {
    const fullscreenTarget = iframeRef.current ?? containerRef.current;
    if (!fullscreenTarget) {
      return;
    }

    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      } else {
        await fullscreenTarget.requestFullscreen();
      }
    } catch (fullscreenError) {
      console.error('Failed to toggle fullscreen', fullscreenError);
    }
  };

  const frameHeightClass = 'h-[calc(100vh-180px)] min-h-[780px] max-h-[1280px] md:h-[calc(100vh-160px)]';
  const showStartScreen = !embedUrl;

  const formatTimeRemaining = () => {
    if (!expiresAt) return '';
    const now = new Date();
    const diff = expiresAt.getTime() - now.getTime();
    if (diff <= 0) return t('instances.expired');
    const minutes = Math.floor(diff / 60000);
    const seconds = Math.floor((diff % 60000) / 1000);
    return `${minutes}m ${seconds}s`;
  };

  const handleConnect = () => {
    if (shouldConnect) {
      void refreshAccess({ forceReload: true });
      return;
    }

    setShouldConnect(true);
  };

  if (!isRunning) {
    return (
      <div className="app-panel border-dashed p-12 text-center">
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
            d="M13 10V3L4 14h7v7l9-11h-7z"
          />
        </svg>
        <h3 className="mt-2 text-sm font-medium text-gray-900">{t('instances.startTheInstance')}</h3>
        <p className="mt-1 text-sm text-gray-500">
          {t('instances.startToAccessDesktop')}
        </p>
      </div>
    );
  }

  if (showStartScreen) {
    return (
      <div className="relative overflow-hidden rounded-[28px] border border-[#1f2937] bg-[radial-gradient(circle_at_top,rgba(59,130,246,0.2),transparent_28%),linear-gradient(180deg,#111827_0%,#0f172a_100%)] shadow-[0_30px_90px_-56px_rgba(17,24,39,0.9)]">
        <div className={`${frameHeightClass} flex flex-col items-center justify-center px-8 text-center`}>
          <button
            type="button"
            onClick={handleConnect}
            disabled={loading}
            aria-label={t('instances.generateAccess')}
            className="group flex h-24 w-24 items-center justify-center rounded-full border border-white/20 bg-white/10 text-white backdrop-blur transition hover:scale-[1.03] hover:bg-white/16 disabled:cursor-wait disabled:opacity-70"
          >
            {loading ? (
              <span className="h-10 w-10 animate-spin rounded-full border-2 border-white/20 border-t-white" />
            ) : (
              <svg
                className="ml-1 h-11 w-11 transition-transform group-hover:translate-x-0.5"
                viewBox="0 0 24 24"
                fill="currentColor"
                aria-hidden="true"
              >
                <path d="M8 5.14v13.72L19 12 8 5.14z" />
              </svg>
            )}
          </button>

          <h3 className="mt-6 text-xl font-semibold text-white">{t('instances.readyToAccess')}</h3>
          <p className="mt-2 max-w-md text-sm leading-6 text-slate-300">
            {loading ? t('instances.generatingToken') : t('instances.generateAccessPrompt', { name: instanceName })}
          </p>
          {error && shouldConnect && (
            <p className="mt-4 max-w-md text-sm text-red-300">{error}</p>
          )}
          <p className="mt-4 text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">
            {loading ? t('instances.generatingToken') : t('instances.generateAccess')}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className={`relative overflow-hidden bg-[#111827] ${isFullscreen ? 'rounded-none' : 'rounded-[28px] border border-[#1f2937] shadow-[0_30px_90px_-56px_rgba(17,24,39,0.9)]'}`}
    >
      <div className="flex items-center justify-between px-4 py-3 bg-gray-800 text-white">
        <div className="flex items-center space-x-4">
          <span className="text-sm font-medium">{instanceName}</span>
          {expiresAt && (
            <span className="text-xs text-gray-400">
              {reconnecting ? t('instances.generatingToken') : `${t('instances.expiresIn')}: ${formatTimeRemaining()}`}
            </span>
          )}
        </div>
        <div className="flex items-center space-x-2">
          <button
            onClick={() => refreshAccess({ forceReload: true })}
            className="rounded-xl bg-[#243041] px-3 py-1 text-xs font-medium text-gray-300 hover:bg-[#31415a] hover:text-white"
          >
            {t('instances.refreshToken')}
          </button>
          <button
            onClick={toggleFullscreen}
            className="rounded-xl bg-[#243041] px-3 py-1 text-xs font-medium text-gray-300 hover:bg-[#31415a] hover:text-white"
          >
            {isFullscreen ? (
              <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            ) : (
              <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" />
              </svg>
            )}
          </button>
        </div>
      </div>

      <div className={frameHeightClass}>
        <iframe
          key={frameKey}
          ref={iframeRef}
          src={embedUrl || undefined}
          title={`${instanceName} Desktop`}
          className="w-full h-full border-0"
          allow="clipboard-read; clipboard-write; fullscreen; autoplay"
          allowFullScreen
          onLoad={() => handleFrameLoad(iframeRef.current)}
          onError={() => refreshAccess({ forceReload: true, silent: true })}
        />
      </div>
    </div>
  );
}
