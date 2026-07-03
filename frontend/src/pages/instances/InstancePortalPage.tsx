import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { OpenClawDesktopOverlay } from "../../components/OpenClawDesktopOverlay";
import { InstanceShellTerminal } from "../../components/InstanceShellTerminal";
import UserLayout from "../../components/UserLayout";
import { WorkspaceFileManager } from "../../components/WorkspaceFileManager";
import { useInstanceDesktopAccess } from "../../hooks/useInstanceDesktopAccess";
import { prepareOpenClawControlUIStorage } from "../../lib/openclawControlStorage";
import { instanceService } from "../../services/instanceService";
import type { Instance, InstanceRuntimeDetails } from "../../types/instance";
import { useI18n } from "../../contexts/I18nContext";

const PORTAL_RUNTIME_POLL_INTERVAL_MS = 10000;
const PORTAL_RUNTIME_BURST_POLL_INTERVAL_MS = 2500;
const PORTAL_RUNTIME_BURST_WINDOW_MS = 12000;

interface PreparedPortalFrame {
  instanceId: number;
  embedUrl: string;
  src: string;
}

function supportsWorkspace(instance: Instance) {
  return (
    instance.type === "openclaw" ||
    instance.type === "hermes" ||
    Boolean(instance.workspace_path)
  );
}

function workspaceInitialPath(instance: Instance, isPro: boolean) {
  const type = instance.type.trim().toLowerCase();
  if (type === "hermes") {
    return isPro ? ".hermes" : "home/.hermes";
  }
  if (type === "openclaw" && !isPro) {
    return "home/.openclaw";
  }
  return isPro ? "/config" : undefined;
}

function typeLabel(type: Instance["type"]) {
  return type === "hermes" ? "Hermes" : type === "openclaw" ? "OpenClaw" : type;
}

function modeLabel(mode: Instance["instance_mode"]) {
  return mode === "pro" ? "Pro" : "Lite";
}

function modeClass(mode: Instance["instance_mode"]) {
  return mode === "pro"
    ? "border-indigo-200 bg-indigo-50 text-indigo-700"
    : "border-sky-200 bg-sky-50 text-sky-700";
}

function instanceIdFromProxyUrl(url: string) {
  try {
    const parsed = new URL(url, window.location.href);
    const match = parsed.pathname.match(/\/api\/v1\/instances\/(\d+)\/proxy(?:\/|$)/);
    if (!match) {
      return null;
    }

    const instanceId = Number(match[1]);
    return Number.isFinite(instanceId) ? instanceId : null;
  } catch {
    return null;
  }
}

function portalEmbedUrlForInstance(instance: Instance | null, embedUrl: string | null) {
  if (!instance || !embedUrl) {
    return embedUrl;
  }

  const embedUrlInstanceId = instanceIdFromProxyUrl(embedUrl);
  if (embedUrlInstanceId !== null && embedUrlInstanceId !== instance.id) {
    return null;
  }

  return embedUrl;
}

const InstancePortalPage: React.FC = () => {
  const { t } = useI18n();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [shouldConnect, setShouldConnect] = useState(false);
  const [runtimeDetails, setRuntimeDetails] =
    useState<InstanceRuntimeDetails | null>(null);
  const [runtimeActionLoading, setRuntimeActionLoading] = useState<
    string | null
  >(null);
  const [runtimeBurstUntil, setRuntimeBurstUntil] = useState<number>(0);
  const [preparedPortalFrame, setPreparedPortalFrame] =
    useState<PreparedPortalFrame | null>(null);
  const frameShellRef = useRef<HTMLElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const pendingConnectInstanceIdRef = useRef<number | null>(null);

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

  const loadInstances = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await instanceService.getInstances(1, 100);
      setInstances(data.instances);
      setSelectedId((currentSelectedId) => {
        if (
          currentSelectedId &&
          data.instances.some((instance) => instance.id === currentSelectedId)
        ) {
          return currentSelectedId;
        }

        const firstRunning = data.instances.find(
          (instance) => instance.status === "running",
        );
        return firstRunning?.id ?? data.instances[0]?.id ?? null;
      });
    } catch (err: any) {
      setError(err.response?.data?.error || t("instances.failedToLoad"));
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

    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => {
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
    };
  }, []);

  const selectedInstance = useMemo(
    () => instances.find((instance) => instance.id === selectedId) ?? null,
    [instances, selectedId],
  );
  const selectedInstanceId = selectedInstance?.id ?? null;
  const selectedInstanceStatus = selectedInstance?.status ?? null;
  const selectedRuntimeType = selectedInstance?.runtime_type ?? "desktop";
  const isShellPortal = selectedRuntimeType === "shell";
  const isProPortal = Boolean(
    selectedInstance && selectedInstance.instance_mode === "pro",
  );
  const canShowRuntimeOverlay = Boolean(
    selectedInstance?.type === "openclaw" && isProPortal,
  );

  const {
    embedUrl,
    loading: accessLoading,
    error: accessError,
    refreshAccess,
    handleFrameLoad,
    handleFrameError,
  } = useInstanceDesktopAccess({
    instanceId: selectedInstance?.id ?? null,
    isRunning:
      selectedInstance?.status === "running" && shouldConnect && !isShellPortal,
    retainSessionOnStop: shouldConnect,
    resolveEmbedUrl,
    failedMessage: t("instances.failedToGenerateAccessToken"),
  });

  const portalEmbedUrl = useMemo(
    () => portalEmbedUrlForInstance(selectedInstance, embedUrl),
    [embedUrl, selectedInstance],
  );
  const portalFrameSrc =
    preparedPortalFrame?.instanceId === selectedInstanceId &&
    preparedPortalFrame.embedUrl === portalEmbedUrl
      ? preparedPortalFrame.src
      : null;

  useEffect(() => {
    if (!selectedInstance || !portalEmbedUrl) {
      setPreparedPortalFrame(null);
      return;
    }

    const src =
      selectedInstance.type === "openclaw"
        ? prepareOpenClawControlUIStorage(selectedInstance.id, portalEmbedUrl)
        : portalEmbedUrl;
    setPreparedPortalFrame({
      instanceId: selectedInstance.id,
      embedUrl: portalEmbedUrl,
      src,
    });
  }, [portalEmbedUrl, selectedInstance]);

  const loadRuntimeDetails = useCallback(async (instanceId: number) => {
    try {
      const data = await instanceService.getRuntimeDetails(instanceId);
      setRuntimeDetails(data);
    } catch (runtimeError) {
      console.error("Failed to load portal runtime details", runtimeError);
      setRuntimeDetails(null);
    }
  }, []);

  useEffect(() => {
    if (!selectedInstance || !canShowRuntimeOverlay) {
      setRuntimeDetails(null);
      return;
    }

    void loadRuntimeDetails(selectedInstance.id);

    const runtimePollInterval =
      runtimeBurstUntil > Date.now()
        ? PORTAL_RUNTIME_BURST_POLL_INTERVAL_MS
        : PORTAL_RUNTIME_POLL_INTERVAL_MS;

    const timer = window.setInterval(() => {
      if (document.hidden) {
        return;
      }
      void loadRuntimeDetails(selectedInstance.id);
    }, runtimePollInterval);

    return () => {
      window.clearInterval(timer);
    };
  }, [canShowRuntimeOverlay, loadRuntimeDetails, runtimeBurstUntil, selectedInstance]);

  useEffect(() => {
    if (runtimeBurstUntil <= Date.now()) {
      return;
    }

    const timeout = window.setTimeout(() => {
      setRuntimeBurstUntil(0);
    }, runtimeBurstUntil - Date.now());

    return () => {
      window.clearTimeout(timeout);
    };
  }, [runtimeBurstUntil]);

  const getStatusDot = (status: Instance["status"]) => {
    switch (status) {
      case "running":
        return "bg-green-500";
      case "creating":
        return "bg-amber-500";
      case "error":
        return "bg-red-500";
      default:
        return "bg-gray-400";
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
      console.error("Failed to toggle portal fullscreen", fullscreenError);
    }
  };

  const requestAccess = useCallback(() => {
    if (selectedInstanceId && selectedInstanceStatus === "running") {
      const nextInstanceId = selectedInstanceId;
      pendingConnectInstanceIdRef.current = nextInstanceId;
      setShouldConnect(false);
      window.setTimeout(() => {
        if (pendingConnectInstanceIdRef.current === nextInstanceId) {
          setShouldConnect(true);
        }
      }, 0);
    }
  }, [selectedInstanceId, selectedInstanceStatus]);

  const retryAccess = () => {
    if (!selectedInstance || selectedInstance.status !== "running") {
      return;
    }

    if (shouldConnect && portalEmbedUrl) {
      void refreshAccess({ forceReload: true });
      return;
    }

    requestAccess();
  };

  useEffect(() => {
    if (!selectedInstanceId || selectedInstanceStatus !== "running" || isShellPortal) {
      return;
    }

    requestAccess();
  }, [isShellPortal, requestAccess, selectedInstanceId, selectedInstanceStatus]);

  const handleRuntimeCommand = async (
    command:
      | "start"
      | "stop"
      | "restart"
      | "collect-system-info"
      | "health-check",
  ) => {
    if (!selectedInstance) {
      return;
    }

    try {
      setRuntimeActionLoading(`runtime-${command}`);
      setRuntimeBurstUntil(Date.now() + PORTAL_RUNTIME_BURST_WINDOW_MS);
      await instanceService.createRuntimeCommand(selectedInstance.id, command);
      await loadRuntimeDetails(selectedInstance.id);
      window.setTimeout(() => {
        void loadRuntimeDetails(selectedInstance.id);
      }, 800);
      window.setTimeout(() => {
        void loadRuntimeDetails(selectedInstance.id);
      }, 2000);
      window.setTimeout(() => {
        void loadRuntimeDetails(selectedInstance.id);
      }, 5000);
    } catch (runtimeError) {
      console.error("Failed to queue portal runtime command", runtimeError);
    } finally {
      setRuntimeActionLoading(null);
    }
  };

  const playerStatusText = !selectedInstance
    ? t("instances.portalSelectInstanceSubtitle")
    : isShellPortal && selectedInstance.status === "running"
      ? t("instances.shellReady")
      : portalEmbedUrl
      ? t("instances.readyToAccess")
      : selectedInstance.status === "running"
        ? accessLoading && shouldConnect
          ? t("instances.generatingToken")
          : t("instances.readyToAccess")
        : t("instances.instanceMustBeRunning");

  return (
    <UserLayout title={t("instances.portalTitle")}>
      <div className="space-y-6">
        <div className="flex h-[calc(100vh-160px)] min-h-0 gap-4">
          <aside className="app-panel flex w-full max-w-[320px] flex-col">
            <div className="border-b border-[#f1e7e1] px-5 py-4">
              <h2 className="text-sm font-semibold uppercase tracking-[0.14em] text-[#8f8681]">
                {t("instances.portalWorkspace")}
              </h2>
            </div>
            <div className="flex-1 overflow-y-auto">
              {loading ? (
                <div className="p-6 text-sm text-[#8f8681]">
                  {t("common.loading")}
                </div>
              ) : error ? (
                <div className="p-6 text-sm text-red-600">{error}</div>
              ) : instances.length === 0 ? (
                <div className="p-6 text-sm text-[#8f8681]">
                  {t("instances.noInstances")}
                </div>
              ) : (
                <ul className="divide-y divide-[#f5ebe5]">
                  {instances.map((instance) => {
                    const isSelected = instance.id === selectedId;
                    const isRunning = instance.status === "running";

                    return (
                      <li key={instance.id}>
                        <button
                          type="button"
                          onClick={() => {
                            if (instance.id !== selectedId) {
                              pendingConnectInstanceIdRef.current = null;
                              setShouldConnect(false);
                            }
                            setSelectedId(instance.id);
                          }}
                          className={`flex w-full items-start gap-3 px-5 py-4 text-left transition-colors ${
                            isSelected ? "bg-[#fff7f3]" : "hover:bg-[#fffaf7]"
                          }`}
                        >
                          <span
                            className={`mt-1 h-2.5 w-2.5 flex-shrink-0 rounded-full ${getStatusDot(instance.status)}`}
                          />
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center justify-between gap-3">
                              <div className="flex min-w-0 items-center gap-2">
                                <p
                                  className={`truncate text-sm font-semibold ${isSelected ? "text-[#dc2626]" : "text-[#171212]"}`}
                                >
                                  {instance.name}
                                </p>
                                <span
                                  className={`inline-flex shrink-0 rounded-md border px-1.5 py-0.5 text-[10px] font-semibold ${modeClass(
                                    instance.instance_mode,
                                  )}`}
                                >
                                  {modeLabel(instance.instance_mode)}
                                </span>
                              </div>
                              <span
                                className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                                  isRunning
                                    ? "bg-green-100 text-green-800"
                                    : "bg-[#f7ece6] text-[#8f5b4b]"
                                }`}
                              >
                                {t(`status.${instance.status}`)}
                              </span>
                            </div>
                            <p className="mt-1 text-xs text-[#8f8681]">
                              {typeLabel(instance.type)} {instance.os_version}
                            </p>
                            <p className="mt-2 text-xs text-[#8f8681]">
                              {instance.cpu_cores} {t("common.cpu")} /{" "}
                              {instance.memory_gb} GB / {instance.disk_gb} GB
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

          <div
            data-portal-mode={isProPortal ? "pro" : "lite"}
            className={`grid min-w-0 flex-1 gap-4 ${
              isProPortal
                ? "min-h-0 overflow-y-auto xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)] xl:grid-rows-[minmax(0,1fr)] xl:overflow-hidden"
                : "min-h-0 overflow-hidden xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)]"
            }`}
          >
            <div
              className={
                isProPortal
                  ? "min-h-[420px] min-w-0 overflow-hidden xl:min-h-0"
                  : "min-h-0 min-w-0"
              }
            >
              <section
                ref={frameShellRef}
                className={`flex h-full min-w-0 flex-col overflow-hidden border border-[#1f2937] bg-[#111827] shadow-[0_30px_90px_-56px_rgba(17,24,39,0.9)] ${
                  isFullscreen ? "rounded-none" : isProPortal ? "rounded-[30px]" : "rounded-lg"
                }`}
              >
            <div className="flex items-center justify-between border-b border-[#2b3443] bg-[#182131] px-4 py-3 text-white">
              <div className="min-w-0">
                <p className="truncate text-sm font-semibold">
                  {selectedInstance?.name ||
                    t("instances.portalSelectInstance")}
                </p>
                <p className="mt-1 text-xs text-[#aab4c4]">
                  {playerStatusText}
                </p>
              </div>
              <div className="flex items-center gap-2">
                {selectedInstance &&
                  selectedInstance.status === "running" &&
                  portalEmbedUrl &&
                  !isShellPortal && (
                    <button
                      onClick={() => refreshAccess({ forceReload: true })}
                      className="rounded-lg bg-[#243041] px-3 py-1.5 text-xs font-medium text-white hover:bg-[#31415a]"
                    >
                      {t("instances.refreshToken")}
                    </button>
                  )}
                {(portalEmbedUrl ||
                  (isShellPortal &&
                    selectedInstance?.status === "running")) && (
                  <button
                    type="button"
                    onClick={toggleFullscreen}
                    className="rounded-lg bg-[#243041] px-3 py-1.5 text-xs font-medium text-white hover:bg-[#31415a]"
                  >
                    {isFullscreen ? (
                      <svg
                        className="h-4 w-4"
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
                        className="h-4 w-4"
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
                )}
              </div>
            </div>

            <div className="min-h-0 flex-1">
              {selectedInstance &&
              isShellPortal &&
              selectedInstance.status === "running" ? (
                <InstanceShellTerminal
                  instanceId={selectedInstance.id}
                  instanceName={selectedInstance.name}
                  isRunning={selectedInstance.status === "running"}
                  heightClassName="h-full min-h-0 max-h-none"
                  className="rounded-none border-0 shadow-none"
                />
              ) : portalFrameSrc ? (
                <div className="relative h-full">
                  {canShowRuntimeOverlay && (
                    <OpenClawDesktopOverlay
                      gatewayStatus={
                        runtimeDetails?.runtime?.openclaw_status || "unknown"
                      }
                      canControl={selectedInstance?.status === "running"}
                      actionLoading={runtimeActionLoading}
                      onCommand={handleRuntimeCommand}
                    />
                  )}
                  <iframe
                    ref={iframeRef}
                    src={portalFrameSrc}
                    title={
                      selectedInstance
                        ? `${selectedInstance.name} portal`
                        : "desktop-portal"
                    }
                    className="h-full w-full border-0"
                    allow="clipboard-read; clipboard-write; fullscreen; autoplay"
                    allowFullScreen
                    onLoad={() => handleFrameLoad(iframeRef.current)}
                    onError={handleFrameError}
                  />
                </div>
              ) : selectedInstance &&
                selectedInstance.status === "running" &&
                !isShellPortal ? (
                <div className="relative flex h-full items-center justify-center bg-[radial-gradient(circle_at_top,rgba(59,130,246,0.18),transparent_26%),linear-gradient(180deg,#111827_0%,#0f172a_100%)] px-8 text-center">
                  {canShowRuntimeOverlay && (
                    <OpenClawDesktopOverlay
                      gatewayStatus={
                        runtimeDetails?.runtime?.openclaw_status || "unknown"
                      }
                      canControl={selectedInstance.status === "running"}
                      actionLoading={runtimeActionLoading}
                      onCommand={handleRuntimeCommand}
                    />
                  )}
                  <div className="flex max-w-md flex-col items-center">
                    <button
                      type="button"
                      onClick={retryAccess}
                      onPointerUp={retryAccess}
                      disabled={accessLoading}
                      aria-label={t("instances.generateAccess")}
                      className="group flex h-24 w-24 items-center justify-center rounded-full border border-white/20 bg-white/10 text-white backdrop-blur transition hover:scale-[1.03] hover:bg-white/16 disabled:cursor-wait disabled:opacity-70"
                    >
                      {accessLoading ? (
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
                    <p className="mt-2 max-w-md text-sm leading-6 text-[#b7c1cf]">
                      {accessLoading
                        ? t("instances.generatingToken")
                        : accessError ||
                          t("instances.generateAccessPrompt", {
                            name: selectedInstance.name,
                          })}
                    </p>
                    <p className="mt-4 text-xs font-semibold uppercase tracking-[0.24em] text-slate-400">
                      {accessLoading
                        ? t("instances.generatingToken")
                        : t("instances.generateAccess")}
                    </p>
                  </div>
                </div>
              ) : (
                <div className="flex h-full items-center justify-center px-8 text-center">
                  <div>
                    <h3 className="text-lg font-semibold text-white">
                      {selectedInstance
                        ? t("instances.portalUnavailable")
                        : t("instances.portalSelectInstance")}
                    </h3>
                    <p className="mt-2 text-sm text-[#b7c1cf]">
                      {selectedInstance
                        ? accessError ||
                          t("instances.portalUnavailableSubtitle")
                        : t("instances.portalSelectInstanceSubtitle")}
                    </p>
                  </div>
                </div>
              )}
            </div>
              </section>
            </div>

            {selectedInstance && supportsWorkspace(selectedInstance) ? (
              <div
                className={`min-w-0 ${
                  isProPortal ? "min-h-[420px] xl:h-full xl:min-h-0" : "h-full min-h-[420px]"
                }`}
              >
                {isProPortal ? (
                  <WorkspaceFileManager
                    instanceId={selectedInstance.id}
                    initialPath={workspaceInitialPath(selectedInstance, isProPortal)}
                  />
                ) : (
                  <WorkspaceFileManager
                    instanceId={selectedInstance.id}
                    initialPath={workspaceInitialPath(selectedInstance, isProPortal)}
                  />
                )}
              </div>
            ) : (
              <div
                className={`cm-surface flex min-w-0 items-center justify-center text-sm text-slate-500 ${
                  isProPortal ? "min-h-[420px]" : "h-full min-h-[420px]"
                }`}
              >
                {selectedInstance
                  ? "No workspace"
                  : t("instances.portalSelectInstanceSubtitle")}
              </div>
            )}
          </div>
        </div>
      </div>
    </UserLayout>
  );
};

export default InstancePortalPage;
