import { Maximize2, MessageSquare, Minimize2, Monitor, PanelRightClose, PanelRightOpen, RefreshCw, ShieldAlert } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../contexts/I18nContext";
import { useInstanceDesktopAccess } from "../hooks/useInstanceDesktopAccess";
import { useRuntimeCertificateTrust } from "../hooks/useRuntimeCertificateTrust";
import { prepareOpenClawControlUIStorage } from "../lib/openclawControlStorage";
import { instanceService, type BrowserWorkerStatus } from "../services/instanceService";
import type { InstanceAvailability } from "../types/instance";
import { HermesLiteServiceFrame } from "./HermesLiteServiceFrame";

export interface InstanceServiceFrameProps {
  instanceId: number;
  instanceName: string;
  instanceType?: string;
  instanceMode: string;
  availability: InstanceAvailability;
  reloadToken?: number;
  openCodeInitialDirectory?: string;
  workspaceVisible?: boolean;
  onWorkspaceVisibilityChange?: (visible: boolean) => void;
}

function resolveEmbedUrl(url: string | null) {
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
  if (window.location.port === "9002" && url.startsWith("/api/")) {
    return `${window.location.protocol}//${window.location.hostname}:9001${url}`;
  }
  return url;
}

interface PreparedFrame {
  instanceId: number;
  embedUrl: string;
  src: string;
}

function encodeOpenCodeDirectory(directory: string) {
  const bytes = new TextEncoder().encode(directory);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return window
    .btoa(binary)
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/u, "");
}

function openCodeNewSessionUrl(
  entryUrl: string,
  directory: string | undefined,
) {
  const normalizedDirectory = directory?.trim().replaceAll("\\", "/");
  if (!normalizedDirectory?.startsWith("/")) {
    return entryUrl;
  }

  try {
    const parsed = new URL(entryUrl, window.location.href);
    const directorySlug = encodeOpenCodeDirectory(normalizedDirectory);
    const proxyOrOriginBase = parsed.pathname.endsWith("/")
      ? parsed.pathname
      : `${parsed.pathname}/`;
    parsed.pathname = `${proxyOrOriginBase}${directorySlug}/session`;
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return entryUrl;
  }
}

export function InstanceServiceFrame({
  instanceMode,
  ...props
}: InstanceServiceFrameProps) {
  if (props.instanceType?.toLowerCase() === "hermes" && instanceMode?.toLowerCase() === "lite") {
    return <HermesLiteServiceFrame {...props} instanceMode={instanceMode} />;
  }

  return <EmbeddedInstanceServiceFrame {...props} instanceMode={instanceMode} />;
}

function EmbeddedInstanceServiceFrame({
  instanceId,
  instanceName,
  instanceType,
  instanceMode,
  availability,
  reloadToken = 0,
  openCodeInitialDirectory,
  workspaceVisible,
  onWorkspaceVisibilityChange,
}: InstanceServiceFrameProps) {
  const { t } = useI18n();
  const isAvailable = availability === "available";
  const frameContainerRef = useRef<HTMLElement | null>(null);
  const [preparedFrame, setPreparedFrame] = useState<PreparedFrame | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [activeView, setActiveView] = useState<"chat" | "browser">("chat");
  const [browserWorker, setBrowserWorker] = useState<BrowserWorkerStatus | null>(null);
  const [browserFrameUrl, setBrowserFrameUrl] = useState<string | null>(null);
  const [browserLoading, setBrowserLoading] = useState(false);
  const [browserError, setBrowserError] = useState<string | null>(null);
  const [browserReloadToken, setBrowserReloadToken] = useState(0);
  const normalizedType = instanceType?.toLowerCase() ?? "";
  const browserWorkerCandidate =
    normalizedType === "openclaw" && instanceMode?.toLowerCase() === "lite";
  const {
    embedUrl: accessEmbedUrl,
    loading,
    error,
    reconnecting,
    refreshAccess,
    handleFrameLoad,
    handleFrameError,
  } = useInstanceDesktopAccess({
    instanceId,
    isRunning: isAvailable,
    reloadOnAccessRefresh: normalizedType === "deepseek-harness",
    resolveEmbedUrl,
    failedMessage: "Failed to open instance service",
  });
  const requiresRuntimeCertificateTrust =
    normalizedType === "opencode" || normalizedType === "deepseek-harness";
  const {
    frameUrl: embedUrl,
    checkingCertificate,
    certificateConfirmationRequired,
    confirmCertificate,
  } = useRuntimeCertificateTrust(
    accessEmbedUrl,
    requiresRuntimeCertificateTrust,
  );

  useEffect(() => {
    let cancelled = false;
    if (!browserWorkerCandidate || !isAvailable) {
      queueMicrotask(() => {
        if (!cancelled) setBrowserWorker(null);
      });
      return () => {
        cancelled = true;
      };
    }
    void instanceService
      .getBrowserWorker(instanceId)
      .then((status) => {
        if (!cancelled) setBrowserWorker(status);
      })
      .catch(() => {
        if (!cancelled) setBrowserWorker(null);
      });
    return () => {
      cancelled = true;
    };
  }, [browserWorkerCandidate, instanceId, isAvailable, reloadToken]);

  const openBrowserWorker = useCallback(async (force = false) => {
    setActiveView("browser");
    if ((!force && browserFrameUrl) || browserLoading) return;
    setBrowserLoading(true);
    setBrowserError(null);
    try {
      const access = await instanceService.generateBrowserWorkerAccess(instanceId);
      const nextURL = resolveEmbedUrl(access.access_url ?? null);
      if (!access.available || !nextURL) {
        throw new Error(access.reason || "Browser Worker is unavailable");
      }
      setBrowserWorker(access);
      setBrowserFrameUrl(nextURL);
    } catch (requestError) {
      setBrowserError(
        requestError instanceof Error
          ? requestError.message
          : "Browser Worker is unavailable",
      );
    } finally {
      setBrowserLoading(false);
    }
  }, [browserFrameUrl, browserLoading, instanceId]);

  const handleRefresh = useCallback(() => {
    if (activeView === "browser") {
      if (browserFrameUrl) {
        setBrowserReloadToken((current) => current + 1);
      } else {
        void openBrowserWorker();
      }
      return;
    }
    void refreshAccess({ forceReload: true });
  }, [activeView, browserFrameUrl, openBrowserWorker, refreshAccess]);

  const handleFullscreen = useCallback(() => {
    const element = frameContainerRef.current;
    if (!element) {
      return;
    }
    if (document.fullscreenElement === element) {
      void document.exitFullscreen();
      return;
    }
    const request = element.requestFullscreen();
    void request.catch(() => undefined);
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (!embedUrl) {
      queueMicrotask(() => {
        if (!cancelled) {
          setPreparedFrame(null);
        }
      });
      return () => {
        cancelled = true;
      };
    }

    let src = embedUrl;
    if (normalizedType === "openclaw") {
      src = prepareOpenClawControlUIStorage(instanceId, embedUrl);
    } else if (normalizedType === "opencode") {
      // The root route is OpenCode's global landing page. Its directory-scoped
      // /session route selects the managed project and opens a blank chat.
      src = openCodeNewSessionUrl(embedUrl, openCodeInitialDirectory);
    }
    queueMicrotask(() => {
      if (!cancelled) {
        setPreparedFrame({ instanceId, embedUrl, src });
      }
    });
    return () => {
      cancelled = true;
    };
  }, [embedUrl, instanceId, normalizedType, openCodeInitialDirectory]);

  useEffect(() => {
    const handleChange = () => {
      setIsFullscreen(document.fullscreenElement === frameContainerRef.current);
    };
    document.addEventListener("fullscreenchange", handleChange);
    return () => document.removeEventListener("fullscreenchange", handleChange);
  }, []);

  const frameSrc =
    preparedFrame?.instanceId === instanceId && preparedFrame.embedUrl === embedUrl
      ? preparedFrame.src
      : null;

  const renderFrameShell = (content: ReactNode) => (
    <section
      ref={frameContainerRef}
      className="cm-surface relative isolate flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-white max-xl:min-h-[360px]"
      style={isFullscreen ? { height: "100vh", width: "100vw", borderRadius: 0 } : undefined}
    >
      <div className="relative z-20 flex h-12 shrink-0 items-center justify-between border-b border-slate-200 bg-white px-3">
        <div className="flex min-w-0 items-center gap-3">
          <div className="min-w-0 truncate text-sm font-medium text-slate-950">
            {instanceName}
          </div>
          {browserWorker?.enabled && (
            <div className="flex shrink-0 rounded-md bg-slate-100 p-0.5" role="tablist">
              <button
                type="button"
                role="tab"
                aria-selected={activeView === "chat"}
                onClick={() => setActiveView("chat")}
                className={`flex items-center gap-1 rounded px-2 py-1 text-xs ${activeView === "chat" ? "bg-white text-slate-950 shadow-sm" : "text-slate-600"}`}
                title="OpenClaw"
              >
                <MessageSquare className="h-3.5 w-3.5" />
                OpenClaw
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeView === "browser"}
                onClick={() => void openBrowserWorker()}
                className={`flex items-center gap-1 rounded px-2 py-1 text-xs ${activeView === "browser" ? "bg-white text-slate-950 shadow-sm" : "text-slate-600"}`}
                title={browserWorker.reason || t("skillHubPage.tags.browser")}
              >
                <Monitor className="h-3.5 w-3.5" />
                {t("skillHubPage.tags.browser")}
              </button>
            </div>
          )}
        </div>
        <div className="relative z-20 flex shrink-0 items-center gap-2">
          {typeof workspaceVisible === "boolean" && onWorkspaceVisibilityChange && (
            <button
              type="button"
              onClick={() => onWorkspaceVisibilityChange(!workspaceVisible)}
              className="cm-icon-button"
              title={workspaceVisible ? t("instances.hideWorkspace") : t("instances.showWorkspace")}
              aria-label={workspaceVisible ? t("instances.hideWorkspace") : t("instances.showWorkspace")}
            >
              {workspaceVisible ? <PanelRightClose className="h-4 w-4" /> : <PanelRightOpen className="h-4 w-4" />}
            </button>
          )}
          {isAvailable && (
            <button
              type="button"
              onClick={handleRefresh}
              className="cm-icon-button"
              title={t("common.refresh")}
              aria-label={t("common.refresh")}
            >
              <RefreshCw className={`h-4 w-4 ${reconnecting ? "animate-spin" : ""}`} />
            </button>
          )}
          <button
            type="button"
            onClick={handleFullscreen}
            className="cm-icon-button"
            title={isFullscreen ? t("instances.exitFullscreen") : t("instances.enterFullscreen")}
            aria-label={isFullscreen ? t("instances.exitFullscreen") : t("instances.enterFullscreen")}
          >
            {isFullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </button>
        </div>
      </div>
      {content}
    </section>
  );

  if (availability === "starting") {
    return renderFrameShell(
      <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-slate-600">
        Starting
      </div>,
    );
  }

  if (!isAvailable) {
    return renderFrameShell(
      <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-slate-600">
        Unavailable
      </div>,
    );
  }

  if (activeView === "browser" && browserWorker?.enabled) {
    return renderFrameShell(
      <div className="relative flex min-h-0 flex-1 flex-col bg-slate-950">
        {browserFrameUrl && (
          <iframe
            key={`browser-worker-${instanceId}-${browserReloadToken}`}
            title={`${instanceName} Browser Worker`}
            src={browserFrameUrl}
            className="min-h-0 w-full flex-1 border-0 bg-slate-950"
            scrolling="no"
            allow="clipboard-read; clipboard-write; fullscreen"
            onLoad={() => setBrowserLoading(false)}
            onError={() => {
              setBrowserLoading(false);
              setBrowserError("Browser Worker connection failed");
            }}
          />
        )}
        {(!browserFrameUrl || browserLoading || browserError) && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-white p-6 text-center text-sm text-slate-600">
            {browserLoading && <RefreshCw className="h-5 w-5 animate-spin" />}
            <p>{browserError || (browserLoading ? "Opening browser" : browserWorker.reason || "Browser Worker is unavailable")}</p>
            {browserError && (
              <button
                type="button"
                onClick={() => {
                  setBrowserFrameUrl(null);
                  setBrowserError(null);
                  void openBrowserWorker(true);
                }}
                className="rounded-md border border-slate-200 px-3 py-1.5"
              >
                {t("common.refresh")}
              </button>
            )}
          </div>
        )}
      </div>,
    );
  }

  if (!embedUrl || !frameSrc) {
    if (certificateConfirmationRequired) {
      return renderFrameShell(
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <ShieldAlert className="h-8 w-8 text-amber-500" />
          <div>
            <p className="text-sm font-semibold text-slate-900">
              {t("instances.certificateConfirmationRequired")}
            </p>
            <p className="mt-1 max-w-lg text-sm leading-6 text-slate-600">
              {t("instances.certificateConfirmationDescription")}
            </p>
          </div>
          <button
            type="button"
            className="app-button-primary"
            onClick={confirmCertificate}
          >
            {t("instances.continueCertificateConfirmation")}
          </button>
        </div>,
      );
    }

    return renderFrameShell(
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 text-sm text-slate-600">
        <RefreshCw className={`h-5 w-5 ${loading || reconnecting ? "animate-spin" : ""}`} />
        {checkingCertificate
          ? t("instances.checkingCertificate")
          : error || "Opening"}
      </div>,
    );
  }

  return renderFrameShell(
      <iframe
        key={`frame-${instanceId}-${reloadToken}`}
        title={`${instanceName} service`}
        src={frameSrc}
        className="min-h-0 w-full flex-1 border-0 bg-white"
        scrolling="no"
        allow="clipboard-read; clipboard-write; fullscreen; autoplay"
        onLoad={(event) => handleFrameLoad(event.currentTarget)}
        onError={handleFrameError}
      />,
  );
}
