import { Maximize2, Minimize2, PanelRightClose, PanelRightOpen, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../contexts/I18nContext";
import { useInstanceDesktopAccess } from "../hooks/useInstanceDesktopAccess";
import { clearHermesDashboardStorage, prepareHermesDashboardStorage } from "../lib/hermesDashboardStorage";
import { prepareOpenClawControlUIStorage } from "../lib/openclawControlStorage";
import type { InstanceAvailability } from "../types/instance";

interface InstanceServiceFrameProps {
  instanceId: number;
  instanceName: string;
  instanceType?: string;
  availability: InstanceAvailability;
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

export function InstanceServiceFrame({
  instanceId,
  instanceName,
  instanceType,
  availability,
  workspaceVisible,
  onWorkspaceVisibilityChange,
}: InstanceServiceFrameProps) {
  const { t } = useI18n();
  const isAvailable = availability === "available";
  const frameContainerRef = useRef<HTMLElement | null>(null);
  const [preparedFrame, setPreparedFrame] = useState<PreparedFrame | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const normalizedType = instanceType?.toLowerCase() ?? "";
  const isHermes = normalizedType === "hermes";
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
    isRunning: isAvailable,
    resolveEmbedUrl,
    failedMessage: "Failed to open instance service",
  });

  const handleRefresh = useCallback(() => {
    void refreshAccess({ forceReload: true });
  }, [refreshAccess]);

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
    if (!embedUrl) {
      setPreparedFrame(null);
      return;
    }

    let src = embedUrl;
    if (normalizedType === "openclaw") {
      src = prepareOpenClawControlUIStorage(instanceId, embedUrl);
    } else if (isHermes) {
      src = prepareHermesDashboardStorage(instanceId, embedUrl);
    }
    setPreparedFrame({ instanceId, embedUrl, src });
  }, [embedUrl, instanceId, isHermes, normalizedType]);

  useEffect(() => {
    if (!isHermes) {
      return;
    }
    return () => {
      clearHermesDashboardStorage();
    };
  }, [isHermes, instanceId]);

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
        <div className="min-w-0 truncate text-sm font-medium text-slate-950">
          {instanceName}
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

  if (!embedUrl || !frameSrc) {
    return renderFrameShell(
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 text-sm text-slate-600">
        <RefreshCw className={`h-5 w-5 ${loading || reconnecting ? "animate-spin" : ""}`} />
        {error || "Opening"}
      </div>,
    );
  }

  return renderFrameShell(
      <iframe
        key={isHermes ? `hermes-${instanceId}` : `frame-${instanceId}`}
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
