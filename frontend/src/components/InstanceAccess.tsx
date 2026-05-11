import { memo, useState, useEffect, useCallback, useRef } from "react";
import { OpenClawDesktopOverlay } from "./OpenClawDesktopOverlay";
import { useI18n } from "../contexts/I18nContext";
import { useInstanceDesktopAccess } from "../hooks/useInstanceDesktopAccess";
import { instanceService } from "../services/instanceService";

interface InstanceAccessProps {
  instanceId: number;
  instanceName: string;
  isRunning: boolean;
  showControlUiEntry?: boolean;
  overlay?: {
    gatewayStatus: string;
    canControl: boolean;
    actionLoading: string | null;
    onCommand: (command: "start" | "stop" | "restart" | "collect-system-info" | "health-check") => void;
  };
}

const desktopConnectPreferenceStore = new Map<number, boolean>();

function accessErrorMessage(error: unknown, fallback: string): string {
  if (typeof error === "object" && error !== null && "response" in error) {
    const response = (error as { response?: { data?: { error?: unknown } } })
      .response;
    if (typeof response?.data?.error === "string") {
      return response.data.error;
    }
  }

  return fallback;
}

const DesktopIframeSurface = memo(function DesktopIframeSurface({
  frameHeightClass,
  iframeRef,
  embedUrl,
  handleFrameLoad,
  handleFrameError,
  frameTitle,
}: {
  frameHeightClass: string;
  iframeRef: React.RefObject<HTMLIFrameElement | null>;
  embedUrl: string;
  handleFrameLoad: (frame: HTMLIFrameElement | null) => void;
  handleFrameError: () => void;
  frameTitle: string;
}) {
  return (
    <div className={frameHeightClass}>
      <iframe
        ref={iframeRef}
        src={embedUrl}
        title={frameTitle}
        className="w-full h-full border-0"
        allow="clipboard-read; clipboard-write; fullscreen; autoplay"
        onLoad={() => handleFrameLoad(iframeRef.current)}
        onError={handleFrameError}
      />
    </div>
  );
});

export function InstanceAccess({
  instanceId,
  instanceName,
  isRunning,
  showControlUiEntry = false,
  overlay,
}: InstanceAccessProps) {
  const { t } = useI18n();
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [shouldConnect, setShouldConnect] = useState(
    () => desktopConnectPreferenceStore.get(instanceId) ?? false,
  );
  const [controlUiOpening, setControlUiOpening] = useState(false);
  const [controlUiError, setControlUiError] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);

  const resolveEmbedUrl = useCallback((url: string | null) => {
    if (!url) {
      return null;
    }

    if (/^https?:\/\//i.test(url)) {
      return url;
    }

    const explicitOrigin = import.meta.env.VITE_BACKEND_ORIGIN as
      | string
      | undefined;
    if (explicitOrigin) {
      return new URL(url, explicitOrigin).toString();
    }

    if (window.location.port === "9002" && url.startsWith("/api/")) {
      return `${window.location.protocol}//${window.location.hostname}:9001${url}`;
    }

    return url;
  }, []);

  const {
    embedUrl,
    loading,
    error,
    reconnecting,
    refreshAccess,
    handleFrameLoad,
    handleFrameError,
  } = useInstanceDesktopAccess({
    instanceId,
    isRunning: isRunning && shouldConnect,
    accessMode: "desktop",
    retainSessionOnStop: shouldConnect,
    resolveEmbedUrl,
    failedMessage: t("instances.failedToGenerateAccessToken"),
  });

  useEffect(() => {
    setShouldConnect(desktopConnectPreferenceStore.get(instanceId) ?? false);
    setControlUiError(null);
    setControlUiOpening(false);
  }, [instanceId]);

  useEffect(() => {
    desktopConnectPreferenceStore.set(instanceId, shouldConnect);
  }, [instanceId, shouldConnect]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === containerRef.current);
    };

    document.addEventListener("fullscreenchange", handleFullscreenChange);

    return () => {
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
    };
  }, []);

  const toggleFullscreen = async () => {
    const fullscreenTarget = containerRef.current;
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
      console.error("Failed to toggle fullscreen", fullscreenError);
    }
  };

  const frameHeightClass = isFullscreen
    ? "min-h-0 flex-1"
    : "h-[54vh] min-h-[420px] max-h-[720px] md:h-[58vh] xl:h-[60vh]";
  const showStartScreen = !embedUrl;
  const hasDesktopSession =
    shouldConnect || Boolean(embedUrl) || loading || reconnecting;

  const handleConnect = () => {
    if (shouldConnect) {
      void refreshAccess({ forceReload: true });
      return;
    }

    setShouldConnect(true);
  };

  const handleOpenControlUi = async () => {
    if (!showControlUiEntry || !isRunning || controlUiOpening) {
      return;
    }

    try {
      setControlUiOpening(true);
      setControlUiError(null);
      const data = await instanceService.generateAccessToken(
        instanceId,
        "control-ui",
      );
      const controlUiUrl = instanceService.getControlUiChatUrl(
        data.proxy_url || data.access_url,
      );
      const resolvedControlUiUrl = resolveEmbedUrl(controlUiUrl);
      if (!resolvedControlUiUrl) {
        setControlUiError(t("instances.failedToOpenControlUi"));
        return;
      }
      window.open(resolvedControlUiUrl, "_blank", "noopener,noreferrer");
    } catch (error: unknown) {
      setControlUiError(
        accessErrorMessage(error, t("instances.failedToOpenControlUi")),
      );
    } finally {
      setControlUiOpening(false);
    }
  };

  if (!isRunning && !hasDesktopSession) {
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
        <h3 className="mt-2 text-sm font-medium text-gray-900">
          {t("instances.startTheInstance")}
        </h3>
        <p className="mt-1 text-sm text-gray-500">
          {t("instances.startToAccessDesktop")}
        </p>
      </div>
    );
  }

  if (showStartScreen) {
    return (
      <div
        ref={containerRef}
        className="relative overflow-hidden rounded-[28px] border border-[#1f2937] bg-[radial-gradient(circle_at_top,rgba(59,130,246,0.2),transparent_28%),linear-gradient(180deg,#111827_0%,#0f172a_100%)] shadow-[0_30px_90px_-56px_rgba(17,24,39,0.9)]"
      >
        {overlay ? (
          <OpenClawDesktopOverlay
            gatewayStatus={overlay.gatewayStatus}
            canControl={overlay.canControl}
            actionLoading={overlay.actionLoading}
            onCommand={overlay.onCommand}
          />
        ) : null}
        <div
          className={`${frameHeightClass} flex flex-col items-center justify-center px-8 text-center`}
        >
          <button
            type="button"
            onClick={handleConnect}
            disabled={loading}
            aria-label={t("instances.generateAccess")}
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

          <h3 className="mt-6 text-xl font-semibold text-white">
            {t("instances.readyToAccess")}
          </h3>
          <p className="mt-2 max-w-md text-sm leading-6 text-slate-300">
            {loading
              ? t("instances.generatingToken")
              : t("instances.generateAccessPrompt", { name: instanceName })}
          </p>
          {error && shouldConnect && (
            <p className="mt-4 max-w-md text-sm text-red-300">{error}</p>
          )}
          {controlUiError && (
            <p className="mt-3 max-w-md text-sm text-red-300">
              {controlUiError}
            </p>
          )}
          <p className="mt-4 text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">
            {loading
              ? t("instances.generatingToken")
              : t("instances.generateAccess")}
          </p>
          {showControlUiEntry ? (
            <button
              type="button"
              onClick={handleOpenControlUi}
              disabled={!isRunning || controlUiOpening}
              className="mt-5 rounded-xl border border-white/15 bg-white/10 px-4 py-2 text-sm font-medium text-white transition hover:bg-white/16 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {controlUiOpening
                ? t("instances.openingControlUi")
                : t("instances.openControlUi")}
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className={`relative overflow-hidden bg-[#111827] ${isFullscreen ? "flex h-screen flex-col rounded-none" : "rounded-[28px] border border-[#1f2937] shadow-[0_30px_90px_-56px_rgba(17,24,39,0.9)]"}`}
    >
      {overlay ? (
        <OpenClawDesktopOverlay
          gatewayStatus={overlay.gatewayStatus}
          canControl={overlay.canControl}
          actionLoading={overlay.actionLoading}
          onCommand={overlay.onCommand}
        />
      ) : null}
      <div className="flex items-center justify-between px-4 py-3 bg-gray-800 text-white">
        <div className="flex items-center space-x-4">
          <span className="text-sm font-medium">{instanceName}</span>
        </div>
        <div className="flex items-center space-x-2">
          {showControlUiEntry ? (
            <button
              type="button"
              onClick={handleOpenControlUi}
              disabled={!isRunning || controlUiOpening}
              className="rounded-xl bg-[#243041] px-3 py-1 text-xs font-medium text-gray-300 hover:bg-[#31415a] hover:text-white disabled:cursor-not-allowed disabled:opacity-50"
            >
              {controlUiOpening
                ? t("instances.openingControlUi")
                : t("instances.openControlUi")}
            </button>
          ) : null}
          <button
            onClick={() => refreshAccess({ forceReload: true })}
            className="rounded-xl bg-[#243041] px-3 py-1 text-xs font-medium text-gray-300 hover:bg-[#31415a] hover:text-white"
          >
            {t("instances.refreshToken")}
          </button>
          <button
            onClick={toggleFullscreen}
            className="rounded-xl bg-[#243041] px-3 py-1 text-xs font-medium text-gray-300 hover:bg-[#31415a] hover:text-white"
          >
            {isFullscreen ? (
              <svg
                className="w-4 h-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            ) : (
              <svg
                className="w-4 h-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4"
                />
              </svg>
            )}
          </button>
        </div>
      </div>

      <DesktopIframeSurface
        frameHeightClass={frameHeightClass}
        iframeRef={iframeRef}
        embedUrl={embedUrl}
        handleFrameLoad={handleFrameLoad}
        handleFrameError={handleFrameError}
        frameTitle={t("instances.desktopFrameTitle", { name: instanceName })}
      />
    </div>
  );
}
