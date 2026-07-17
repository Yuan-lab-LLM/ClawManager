import { Maximize2, Minimize2, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useInstanceDesktopAccess } from "../hooks/useInstanceDesktopAccess";
import { prepareOpenClawControlUIStorage } from "../lib/openclawControlStorage";
import type { InstanceAvailability } from "../types/instance";

interface InstanceServiceFrameProps {
  instanceId: number;
  instanceName: string;
  instanceType?: string;
  availability: InstanceAvailability;
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
}: InstanceServiceFrameProps) {
  const isAvailable = availability === "available";
  const frameContainerRef = useRef<HTMLElement | null>(null);
  const [preparedFrame, setPreparedFrame] = useState<PreparedFrame | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
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

    const src =
      instanceType?.toLowerCase() === "openclaw"
        ? prepareOpenClawControlUIStorage(instanceId, embedUrl)
        : embedUrl;
    setPreparedFrame({ instanceId, embedUrl, src });
  }, [embedUrl, instanceId, instanceType]);

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
      className="cm-surface flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-white max-xl:min-h-[360px]"
      style={isFullscreen ? { height: "100vh", width: "100vw", borderRadius: 0 } : undefined}
    >
      <div className="flex h-12 items-center justify-between border-b border-slate-200 px-3">
        <div className="min-w-0 truncate text-sm font-medium text-slate-950">
          {instanceName}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {isAvailable && (
            <button
              type="button"
              onClick={handleRefresh}
              className="cm-icon-button"
              title="Refresh"
            >
              <RefreshCw className={`h-4 w-4 ${reconnecting ? "animate-spin" : ""}`} />
            </button>
          )}
          <button
            type="button"
            onClick={handleFullscreen}
            className="cm-icon-button"
            title={isFullscreen ? "Exit fullscreen" : "Fullscreen"}
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
