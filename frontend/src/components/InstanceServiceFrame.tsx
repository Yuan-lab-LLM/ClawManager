import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { useInstanceDesktopAccess } from "../hooks/useInstanceDesktopAccess";
import { clearHermesDashboardStorage, prepareHermesDashboardStorage } from "../lib/hermesDashboardStorage";
import { resolveHermesDashboardUrl } from "../lib/hermesDashboardAuth";
import { prepareOpenClawControlUIStorage } from "../lib/openclawControlStorage";
import type { InstanceAvailability } from "../types/instance";
import { InstanceShellTerminal } from "./InstanceShellTerminal";
import { HermesLiteServiceFrame } from "./HermesLiteServiceFrame";
import { InstanceServiceFrameShell } from "./InstanceServiceFrameShell";

export interface InstanceServiceFrameProps {
  instanceId: number;
  instanceName: string;
  instanceType?: string;
  instanceMode?: string;
  availability: InstanceAvailability;
  reloadToken?: number;
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

export function InstanceServiceFrame(props: InstanceServiceFrameProps) {
  if (props.instanceType?.toLowerCase() === "hermes" && props.instanceMode?.toLowerCase() === "lite") {
    return (
      <HermesLiteServiceFrame
        key={props.instanceId}
        {...props}
        renderClassic={() => <ClassicInstanceServiceFrame {...props} />}
      />
    );
  }
  return <ClassicInstanceServiceFrame {...props} />;
}

function ClassicInstanceServiceFrame({
  instanceId,
  instanceName,
  instanceType,
  instanceMode,
  availability,
  reloadToken = 0,
  workspaceVisible,
  onWorkspaceVisibilityChange,
}: InstanceServiceFrameProps) {
  const isAvailable = availability === "available";
  const [preparedFrame, setPreparedFrame] = useState<PreparedFrame | null>(null);
  const normalizedType = instanceType?.toLowerCase() ?? "";
  const isHermes = normalizedType === "hermes";
  const isHermesLite = isHermes && instanceMode?.toLowerCase() === "lite";
  const resolveServiceEmbedUrl = useCallback((url: string | null) => isHermesLite
    ? resolveHermesDashboardUrl(url, instanceId, window.location.origin)
    : resolveEmbedUrl(url), [isHermesLite, instanceId]);
  const isOpenCodeLite =
    normalizedType === "opencode" && instanceMode?.toLowerCase() === "lite";
  const {
    embedUrl,
    frameRevision,
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
    sameOriginAccess: isHermesLite,
    resolveEmbedUrl: resolveServiceEmbedUrl,
    failedMessage: "Failed to open instance service",
  });

  const handleRefresh = useCallback(() => {
    void refreshAccess({ forceReload: true });
  }, [refreshAccess]);

  useEffect(() => {
    let cancelled = false;
    if (!embedUrl) {
      queueMicrotask(() => {
        if (!cancelled) setPreparedFrame(null);
      });
      return () => {
        cancelled = true;
      };
    }

    let src = embedUrl;
    if (normalizedType === "openclaw") {
      src = prepareOpenClawControlUIStorage(instanceId, embedUrl);
    } else if (isHermes) {
      src = prepareHermesDashboardStorage(instanceId, embedUrl);
    }
    queueMicrotask(() => {
      if (!cancelled) setPreparedFrame({ instanceId, embedUrl, src });
    });
    return () => {
      cancelled = true;
    };
  }, [embedUrl, instanceId, isHermes, normalizedType]);

  useEffect(() => {
    if (!isHermes) {
      return;
    }
    return () => {
      clearHermesDashboardStorage();
    };
  }, [isHermes, instanceId]);

  const frameSrc =
    preparedFrame?.instanceId === instanceId && preparedFrame.embedUrl === embedUrl
      ? preparedFrame.src
      : null;

  const renderFrameShell = (content: ReactNode) => (
    <InstanceServiceFrameShell
      instanceName={instanceName}
      onRefresh={isAvailable ? handleRefresh : undefined}
      refreshing={reconnecting}
      workspaceVisible={workspaceVisible}
      onWorkspaceVisibilityChange={onWorkspaceVisibilityChange}
    >
      {content}
    </InstanceServiceFrameShell>
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

  if (isOpenCodeLite) {
    return (
      <InstanceShellTerminal
        instanceId={instanceId}
        instanceName={instanceName}
        isRunning={isAvailable}
        autoConnect
        heightClassName="h-full min-h-0 max-h-none"
        className="h-full"
      />
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
        key={
          isHermes
            ? `hermes-${instanceId}-${reloadToken}-${frameRevision}`
            : `frame-${instanceId}-${reloadToken}-${frameRevision}`
        }
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
