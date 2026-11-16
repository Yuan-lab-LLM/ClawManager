import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../contexts/I18nContext";
import { hermesDesktopRenewalDelay, hermesSessionRoute, isHermesDesktopFrameMessage, readHermesSessionRoute, rememberHermesSession, resolveHermesDesktopRendererUrl, resolveHermesDesktopView } from "../lib/hermesDesktopFrame";
import { useAuthStore } from "../stores/authStore";
import { attachNativePreviewBridge } from "../lib/nativePreviewBridge";
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
  let unavailableContent: ReactNode = null;
  if (view.mode === "pending") {
    unavailableContent = (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-slate-600" role="status">
        <RefreshCw className="h-5 w-5 animate-spin" />
        <p>{t("hermesDesktop.checking")}</p>
      </div>
    );
  } else if (view.mode === "unavailable") {
    unavailableContent = (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-slate-600" role="alert">
        <p>{t(`hermesDesktop.${view.reason ?? "capabilityUnknown"}`)}</p>
        <button type="button" disabled={capability.isFetching} onClick={() => void capability.refetch()} className="rounded-md border border-slate-200 px-3 py-1.5 disabled:opacity-50">{t("hermesDesktop.retryCapability")}</button>
      </div>
    );
  }
  return (
    <HermesDesktopFrame {...props} unavailableContent={unavailableContent}
      capabilityRefreshing={view.mode === "pending" || capability.isFetching} />
  );
}

type FrameState = {
  key: string;
  src: string | null;
  status: "opening" | "ready" | "error";
  error?: string;
};

function HermesDesktopFrame({
  instanceId,
  instanceName,
  headerTitle,
  headerActions,
  headerEndActions,
  reloadToken = 0,
  workspaceVisible,
  onWorkspaceVisibilityChange,
  onOpenWorkbenchPreview,
  unavailableContent,
  capabilityRefreshing,
}: InstanceServiceFrameProps & { unavailableContent?: ReactNode; capabilityRefreshing: boolean }) {
  const { t } = useI18n();
  const frameRef = useRef<HTMLIFrameElement | null>(null);
  const userId = useAuthStore((auth) => auth.isAuthenticated ? auth.user?.id : undefined);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<FrameState | null>(null);
  const enabled = !unavailableContent;
  const [wasEnabled, setWasEnabled] = useState(enabled);
  if (wasEnabled !== enabled) {
    setWasEnabled(enabled);
    if (!enabled) setState(null);
  }
  const launchKey = `${userId ?? "shared"}-${instanceId}-${reloadToken}-${revision}`;
  const current = state?.key === launchKey ? state : null;
  const src = current?.src ?? null;

  useEffect(() => {
    const frame = frameRef.current;
    if (!frame || !src) return;
    return rememberHermesSession(frame, window.localStorage, userId, instanceId);
  }, [src, launchKey, userId, instanceId]);

  useEffect(() => {
    const frame = frameRef.current;
    if (!frame || !src || !onOpenWorkbenchPreview) return;
    return attachNativePreviewBridge(frame, src, onOpenWorkbenchPreview);
  }, [src, launchKey, onOpenWorkbenchPreview]);

  useEffect(() => {
    if (!enabled) return;
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
          // Activation can fail after a successful capability probe. Use the
          // same fixed reason mapping, rather than claiming the Runtime lacks
          // Desktop Web support when its upstream authentication failed.
          const rejected = resolveHermesDesktopView({ instanceId, instanceAvailable: true, pending: false, failed: false, capability: response });
          setState({ key: launchKey, src: null, status: "error", error: rejected.reason ?? "capabilityUnknown" });
          return;
        }
        expiresAt = Date.parse(response.expires_at!);
        if (!renew) {
          let route = readHermesSessionRoute(window.localStorage, userId, instanceId);
          if (route === null) {
            try {
              const recent = await hermesDesktopService.recentSession(instanceId, controller.signal);
              if (recent) route = hermesSessionRoute(`#/${encodeURIComponent(recent)}`);
            } catch { /* Optional selection recovery must not block a usable renderer. */ }
          }
          if (disposed) return;
          setState({ key: launchKey, src: nextSrc + (route ?? ""), status: "opening" });
        }
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
  }, [enabled, instanceId, launchKey, userId]);

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
      headerTitle={headerTitle}
      headerActions={headerActions}
      headerEndActions={headerEndActions}
      onRefresh={enabled ? refresh : undefined}
      refreshing={enabled ? opening : capabilityRefreshing}
      workspaceVisible={workspaceVisible}
      onWorkspaceVisibilityChange={onWorkspaceVisibilityChange}
    >
      {unavailableContent ?? <div className="relative flex min-h-0 flex-1 flex-col">
        {src && (
          <iframe
            ref={frameRef}
            key={launchKey}
            title={`${instanceName} Hermes Desktop Web`}
            src={src}
            className="cm-instance-native-content min-h-0 w-full flex-1 border-0 bg-white"
            data-ready={current?.status === "ready"}
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
      </div>}
    </InstanceServiceFrameShell>
  );
}
