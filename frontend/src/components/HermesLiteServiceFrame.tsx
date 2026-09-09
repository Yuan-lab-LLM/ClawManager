import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useI18n } from "../contexts/I18nContext";
import { hermesDesktopRenewalDelay, isHermesDesktopFrameMessage, resolveHermesDesktopRendererUrl, resolveHermesDesktopView } from "../lib/hermesDesktopFrame";
import { hermesDesktopService } from "../services/hermesDesktopService";
import type { InstanceServiceFrameProps } from "./InstanceServiceFrame";
import { InstanceServiceFrameShell } from "./InstanceServiceFrameShell";

type HermesLiteServiceFrameProps = InstanceServiceFrameProps;

export function HermesLiteServiceFrame(props: HermesLiteServiceFrameProps) {
  const { t } = useI18n();
  const capability = useQuery({
    queryKey: ["hermes-desktop-capability", props.instanceId, props.reloadToken],
    queryFn: ({ signal }) => hermesDesktopService.probe(props.instanceId, signal),
    enabled: props.availability === "available",
    retry: false,
    staleTime: 30_000,
  });
  const view = resolveHermesDesktopView({
    instanceId: props.instanceId,
    instanceAvailable: props.availability === "available",
    pending: capability.isPending,
    failed: capability.isError,
    capability: capability.data,
  });
  if (view.mode === "pending") {
    return (
      <InstanceServiceFrameShell
        instanceName={props.instanceName}
        refreshing
        workspaceVisible={props.workspaceVisible}
        onWorkspaceVisibilityChange={props.onWorkspaceVisibilityChange}
      >
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-slate-600" role="status">
          <RefreshCw className="h-5 w-5 animate-spin" />
          <p>{t("hermesDesktop.checking")}</p>
        </div>
      </InstanceServiceFrameShell>
    );
  }
  if (view.mode === "unavailable") {
    return (
      <InstanceServiceFrameShell
        instanceName={props.instanceName}
        refreshing={capability.isFetching}
        workspaceVisible={props.workspaceVisible}
        onWorkspaceVisibilityChange={props.onWorkspaceVisibilityChange}
      >
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-slate-600" role="alert">
          <p>{t(`hermesDesktop.${view.reason ?? "capabilityUnknown"}`)}</p>
          <button type="button" disabled={capability.isFetching} onClick={() => void capability.refetch()} className="rounded-md border border-slate-200 px-3 py-1.5 disabled:opacity-50">{t("hermesDesktop.retryCapability")}</button>
        </div>
      </InstanceServiceFrameShell>
    );
  }
  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="min-h-0 flex-1"><HermesDesktopFrame {...props} /></div>
    </div>
  );
}

type FrameState = {
  key: string;
  src: string | null;
  status: "opening" | "ready" | "error";
  error?: "unavailable" | "connectionFailed" | "startupTimeout";
};

function HermesDesktopFrame({
  instanceId,
  instanceName,
  reloadToken = 0,
  workspaceVisible,
  onWorkspaceVisibilityChange,
}: InstanceServiceFrameProps) {
  const { t } = useI18n();
  const frameRef = useRef<HTMLIFrameElement | null>(null);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<FrameState | null>(null);
  const launchKey = `${instanceId}-${reloadToken}-${revision}`;
  const current = state?.key === launchKey ? state : null;
  const src = current?.src ?? null;

  useEffect(() => {
    const controller = new AbortController();
    let disposed = false;
    let renewalTimer: number | undefined;
    let expiresAt = 0;
    let pending = false;

    const bootstrap = async (renew: boolean) => {
      if (pending || disposed) return;
      pending = true;
      try {
        const response = await hermesDesktopService.bootstrap(instanceId, controller.signal);
        if (disposed) return;
        const nextSrc = resolveHermesDesktopRendererUrl(response.renderer_url, instanceId, window.location.origin);
        const renewalDelay = hermesDesktopRenewalDelay(response.expires_at);
        if (!response.available || response.instance_id !== instanceId || !nextSrc || renewalDelay === null) {
          setState({ key: launchKey, src: null, status: "error", error: "unavailable" });
          return;
        }
        expiresAt = Date.parse(response.expires_at!);
        if (!renew) setState({ key: launchKey, src: nextSrc, status: "opening" });
        window.clearTimeout(renewalTimer);
        renewalTimer = window.setTimeout(() => void bootstrap(true), renewalDelay);
      } catch {
        if (!disposed) setState({ key: launchKey, src: null, status: "error", error: "connectionFailed" });
      } finally {
        pending = false;
      }
    };

    const renewOnFocus = () => {
      if (!document.hidden && expiresAt > 0 && expiresAt - Date.now() < 60_000) void bootstrap(true);
    };
    void bootstrap(false);
    window.addEventListener("focus", renewOnFocus);
    document.addEventListener("visibilitychange", renewOnFocus);
    return () => {
      disposed = true;
      controller.abort();
      window.clearTimeout(renewalTimer);
      window.removeEventListener("focus", renewOnFocus);
      document.removeEventListener("visibilitychange", renewOnFocus);
    };
  }, [instanceId, launchKey]);

  useEffect(() => {
    if (!src) return;
    const timer = window.setTimeout(() => {
      setState((previous) => previous?.key === launchKey && previous.status === "opening"
        ? { key: launchKey, src: null, status: "error", error: "startupTimeout" }
        : previous);
    }, 45_000);
    const receiveMessage = (event: MessageEvent) => {
      if (!isHermesDesktopFrameMessage(event, frameRef.current?.contentWindow ?? null, instanceId, window.location.origin)) return;
      window.clearTimeout(timer);
      setState((previous) => {
        if (previous?.key !== launchKey) return previous;
        return event.data.type === "clawmanager:hermes-desktop:ready"
          ? { ...previous, status: "ready" }
          : { key: launchKey, src: null, status: "error", error: "connectionFailed" };
      });
    };
    window.addEventListener("message", receiveMessage);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("message", receiveMessage);
    };
  }, [instanceId, launchKey, src]);

  const refresh = () => setRevision((previous) => previous + 1);
  const failed = current?.status === "error";
  const opening = !current || current.status === "opening";

  return (
    <InstanceServiceFrameShell
      instanceName={instanceName}
      onRefresh={refresh}
      refreshing={opening}
      workspaceVisible={workspaceVisible}
      onWorkspaceVisibilityChange={onWorkspaceVisibilityChange}
    >
      <div className="relative flex min-h-0 flex-1 flex-col">
        {src && (
          <iframe
            ref={frameRef}
            key={launchKey}
            title={`${instanceName} Hermes Desktop Web`}
            src={src}
            className="min-h-0 w-full flex-1 border-0 bg-white"
            allow="clipboard-write; fullscreen"
            onError={() => setState({ key: launchKey, src: null, status: "error", error: "connectionFailed" })}
          />
        )}
        {(opening || failed) && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-white p-6 text-center text-sm text-slate-600" role={failed ? "alert" : "status"}>
            {opening && <RefreshCw className="h-5 w-5 animate-spin" />}
            <p>{t(`hermesDesktop.${current?.error ?? "opening"}`)}</p>
            {failed && <button type="button" onClick={refresh} className="rounded-md border border-slate-200 px-3 py-1.5">{t("common.refresh")}</button>}
          </div>
        )}
      </div>
    </InstanceServiceFrameShell>
  );
}
