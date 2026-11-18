import { allowBrowserInstance } from "../../lib/browserOrigin";
import axios from "axios";
import { Maximize2, Minimize2, PanelRightClose, PanelRightOpen, RefreshCw, ShieldAlert } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { InstanceWorkbench } from "../../components/workbench/InstanceWorkbench";
import { WorkbenchServicesProvider } from "../../components/workbench/WorkbenchServices";
import { workbenchReducer, MAX_WORKBENCH_TABS, type WorkbenchEvent } from "../../components/workbench/state";
import { readWorkbenchView, writeWorkbenchView, sharedWorkbenchViewKey } from "../../components/workbench/persistence";
import { attachNativePreviewBridge } from "../../lib/nativePreviewBridge";
import { workbenchError } from "../../services/instanceWorkbenchService";
import { WorkspaceFileManager } from "../../components/WorkspaceFileManager";
import { useRuntimeCertificateTrust } from "../../hooks/useRuntimeCertificateTrust";
import { prepareOpenClawControlUIStorage } from "../../lib/openclawControlStorage";
import {
  createSharedWorkspaceService,
  createSharedWorkbenchService,
  getSharedInstanceSession,
  type SharedInstanceSession,
} from "../../services/sharedInstanceService";

function resolveEmbedUrl(url: string) {
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

function shareEntryPath(code: string) {
  return `/s/${encodeURIComponent(code)}/`;
}

export default function SharedInstancePage() {
  const { code = "" } = useParams<{ code: string }>();
  return <SharedInstanceContent key={code} code={code} />;
}

function SharedInstanceContent({ code }: { code: string }) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const frameContainerRef = useRef<HTMLElement | null>(null);
  const [session, setSession] = useState<SharedInstanceSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [frameVersion, setFrameVersion] = useState(0);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const generation = session?.instance.runtime_generation ?? 0;
  const workbenchKey = sharedWorkbenchViewKey(session?.workbench_scope, session?.instance.id);
  const restored = useMemo(() => readWorkbenchView(workbenchKey, generation), [workbenchKey, generation]);
  const [view, setView] = useState(() => ({ key: workbenchKey, value: restored }));
  const currentView = view.key === workbenchKey ? view.value : restored;
  const viewRef = useRef(currentView);
  const scopeRef = useRef(workbenchKey);
  viewRef.current = currentView;
  scopeRef.current = workbenchKey;
  const workspaceVisible = currentView.visible;
  const setWorkspaceVisible = useCallback((value: boolean | ((visible: boolean) => boolean)) => {
    if (scopeRef.current !== workbenchKey) return;
    const next = { ...viewRef.current, visible: typeof value === "function" ? value(viewRef.current.visible) : value };
    viewRef.current = next;
    setView({ key: workbenchKey, value: next });
    writeWorkbenchView(workbenchKey, next);
  }, [workbenchKey]);
  const onWorkbenchEvent = useCallback((event: WorkbenchEvent) => {
    if (scopeRef.current !== workbenchKey) return;
    const previous = viewRef.current;
    const state = workbenchReducer(previous.state, event);
    if (state === previous.state) return;
    const next = { ...previous, state };
    viewRef.current = next;
    setView({ key: workbenchKey, value: next });
    writeWorkbenchView(workbenchKey, next);
  }, [workbenchKey]);

  const loadSession = useCallback(
    async (options?: { reloadFrame?: boolean; background?: boolean }) => {
      if (!code) {
        setError("Invalid share link");
        setLoading(false);
        return;
      }
      if (!options?.background) {
        setLoading(true);
      }
      try {
        const next = await getSharedInstanceSession(code);
        setSession(next);
        setError(null);
        if (options?.reloadFrame) {
          setFrameVersion((version) => version + 1);
        }
      } catch (err: unknown) {
        if (axios.isAxiosError(err) && err.response?.status === 401) {
          window.location.replace(shareEntryPath(code));
          return;
        }
        const message =
          axios.isAxiosError(err) && typeof err.response?.data?.error === "string"
            ? err.response.data.error
            : "Unable to open the shared instance";
        if (!options?.background) {
          setError(message);
        }
      } finally {
        if (!options?.background) {
          setLoading(false);
        }
      }
    },
    [code],
  );

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadSession();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadSession]);

  useEffect(() => {
    if (!session) {
      return;
    }
    const expiresAt = Date.parse(session.session_expires_at);
    const renewalDelay = Number.isFinite(expiresAt)
      ? Math.max(30_000, Math.min(45 * 60 * 1000, expiresAt - Date.now() - 60_000))
      : 45 * 60 * 1000;
    const timer = window.setTimeout(() => {
      void loadSession({ background: true });
    }, renewalDelay);
    return () => window.clearTimeout(timer);
  }, [loadSession, session]);

  useEffect(() => {
    const previousTitle = document.title;
    const existingReferrer = document.querySelector<HTMLMetaElement>('meta[name="referrer"]');
    const previousReferrer = existingReferrer?.content;
    const referrerMeta = existingReferrer ?? document.createElement("meta");
    if (!existingReferrer) {
      referrerMeta.name = "referrer";
      document.head.appendChild(referrerMeta);
    }
    referrerMeta.content = "no-referrer";
    document.title = session?.instance.name
      ? `${session.instance.name} · Shared instance`
      : "Shared instance";
    return () => {
      document.title = previousTitle;
      if (existingReferrer) {
        existingReferrer.content = previousReferrer ?? "";
      } else {
        referrerMeta.remove();
      }
    };
  }, [session?.instance.name]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === frameContainerRef.current);
    };
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, []);

  const csrfToken = session?.csrf_token;
  const shareScope = session?.workbench_scope;
  const workspaceAccess = session?.workspace_access;
  const workspaceService = useMemo(
    () =>
      csrfToken
        ? createSharedWorkspaceService(code, csrfToken)
        : null,
    [code, csrfToken],
  );

  const workbenchService = useMemo(() => csrfToken ? createSharedWorkbenchService(code, csrfToken) : null, [code, csrfToken]);
  const services = useMemo(() => workbenchService && workspaceService ? ({
    service: workbenchService, workspace: workspaceService,
    scope: `share:${shareScope ?? code}`,
    workspaceKey: `share:${shareScope ?? code}`,
    canWrite: workspaceAccess === "write",
  }) : null, [shareScope, workspaceAccess, workbenchService, workspaceService, code]);
  const canUseWorkbench = !!workbenchKey && generation > 0 && session?.instance.instance_mode === "lite" && session?.instance.runtime_type === "gateway";

  const accessFrameUrl = useMemo(() => {
    if (!session?.access_url || !allowBrowserInstance(session.instance.id, session)) {
      return "";
    }
    const url = resolveEmbedUrl(session.access_url);
    return session.instance.type.toLowerCase() === "openclaw"
      ? prepareOpenClawControlUIStorage(session.instance.id, url)
      : url;
  }, [session]);
  const normalizedType = session?.instance.type.trim().toLowerCase() ?? "";
  const requiresRuntimeCertificateTrust =
    normalizedType === "opencode" || normalizedType === "deepseek-harness";
  const {
    frameUrl: frameSrc,
    checkingCertificate,
    certificateConfirmationRequired,
    certificateOrigin,
    confirmCertificate,
  } = useRuntimeCertificateTrust(
    accessFrameUrl || null,
    requiresRuntimeCertificateTrust,
  );

  const openPreview = useCallback(async (address: string, isCurrent: () => boolean) => {
    if (!session || !workbenchService || !canUseWorkbench || session.workspace_access === "none" || !isCurrent() || scopeRef.current !== workbenchKey) return;
    const state = viewRef.current.state;
    if (state.generation !== generation) return;
    const existing = state.tabs.find(tab => tab.kind === "browser" && tab.address === address && !tab.stale);
    setWorkspaceVisible(true);
    onWorkbenchEvent({ type: "panel", panel: "preview" });
    if (existing) { onWorkbenchEvent({ type: "tab.select", id: existing.id }); return; }
    if (state.tabs.length >= MAX_WORKBENCH_TABS) return;
    const id = crypto.randomUUID();
    onWorkbenchEvent({ type: "browser.new", id });
    onWorkbenchEvent({ type: "browser.navigate", id, address, url: "", previewPending: true, generation });
    const current = () => isCurrent() && scopeRef.current === workbenchKey && viewRef.current.state.generation === generation && viewRef.current.state.tabs.some(tab => tab.id === id && tab.address === address);
    const discardPending = () => {
      if (scopeRef.current === workbenchKey && viewRef.current.state.generation === generation && viewRef.current.state.tabs.some(tab => tab.id === id && tab.address === address && tab.previewPending)) onWorkbenchEvent({ type: "tab.remove", id });
    };
    try {
      const result = await workbenchService.preview(session.instance.id, generation, address);
      if (!current() || result.generation !== generation) { discardPending(); return; }
      onWorkbenchEvent({ type: "browser.navigate", id, address, url: result.url, viewer_url: result.viewer_url, credentialless: result.credentialless, generation });
    } catch (error) {
      if (current()) onWorkbenchEvent({ type: "browser.navigate", id, address, url: "", previewError: workbenchError(error), generation });
      else discardPending();
    }
  }, [session, workbenchService, canUseWorkbench, workbenchKey, generation, setWorkspaceVisible, onWorkbenchEvent]);
  useEffect(() => {
    if (!frameSrc || !frameRef.current || !canUseWorkbench || session?.workspace_access === "none") return;
    return attachNativePreviewBridge(frameRef.current, frameSrc, (address, current) => void openPreview(address, current));
  }, [frameSrc, frameVersion, canUseWorkbench, session?.workspace_access, openPreview]);

  const handleFullscreen = useCallback(() => {
    const element = frameContainerRef.current;
    if (!element) {
      return;
    }
    if (document.fullscreenElement === element) {
      void document.exitFullscreen();
      return;
    }
    void element.requestFullscreen().catch(() => undefined);
  }, []);

  if (loading && !session) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-slate-100 text-sm text-slate-600">
        <RefreshCw className="mr-2 h-5 w-5 animate-spin" />
        Opening shared instance
      </main>
    );
  }

  if (!session || error) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-slate-100 p-6">
        <section className="w-full max-w-md rounded-xl border border-red-200 bg-white p-6 text-center shadow-sm">
          <h1 className="text-lg font-semibold text-slate-950">Shared instance unavailable</h1>
          <p className="mt-2 text-sm text-slate-600">{error ?? "The share link is not available."}</p>
          <button
            type="button"
            className="app-button-secondary mt-5"
            onClick={() => void loadSession({ reloadFrame: true })}
          >
            <RefreshCw className="h-4 w-4" />
            Retry
          </button>
        </section>
      </main>
    );
  }

  const canShowWorkspace =
    session.workspace_available &&
    session.workspace_access !== "none" &&
    workspaceService !== null;

  return (
    <main className="flex h-screen min-h-[560px] flex-col overflow-hidden bg-slate-100">
      <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b border-slate-200 bg-white px-4">
        <div className="min-w-0">
          <h1 className="truncate text-base font-semibold text-slate-950">
            {session.instance.name}
          </h1>
          <p className="text-xs text-slate-500">Shared instance</p>
        </div>
        <span className="inline-flex shrink-0 items-center gap-2 rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700">
          <span className="h-2 w-2 rounded-full bg-emerald-400" />
          Available
        </span>
      </header>

      <section className={`grid min-h-0 flex-1 gap-4 p-4 ${
        canShowWorkspace && workspaceVisible
          ? "max-xl:grid-rows-[minmax(420px,1fr)_minmax(360px,0.8fr)] xl:grid-cols-[minmax(0,1fr)_minmax(360px,1fr)]"
          : "grid-cols-1"
      }`}>
        <section
          ref={frameContainerRef}
          className="cm-surface flex min-h-0 min-w-0 flex-col overflow-hidden bg-white"
          style={isFullscreen ? { height: "100vh", width: "100vw", borderRadius: 0 } : undefined}
        >
          <div className="flex h-12 shrink-0 items-center justify-between border-b border-slate-200 px-3">
            <div className="min-w-0 truncate text-sm font-medium text-slate-950">
              {session.instance.name}
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {canShowWorkspace && (
                <button
                  type="button"
                  className="cm-icon-button"
                  title={workspaceVisible ? "Hide workspace" : "Show workspace"}
                  aria-label={workspaceVisible ? "Hide workspace" : "Show workspace"}
                  onClick={() => setWorkspaceVisible((visible) => !visible)}
                >
                  {workspaceVisible ? <PanelRightClose className="h-4 w-4" /> : <PanelRightOpen className="h-4 w-4" />}
                </button>
              )}
              <button
                type="button"
                className="cm-icon-button"
                title="Refresh"
                onClick={() => void loadSession({ reloadFrame: true })}
              >
                <RefreshCw className="h-4 w-4" />
              </button>
              <button
                type="button"
                className="cm-icon-button"
                title={isFullscreen ? "Exit fullscreen" : "Fullscreen"}
                onClick={handleFullscreen}
              >
                {isFullscreen ? (
                  <Minimize2 className="h-4 w-4" />
                ) : (
                  <Maximize2 className="h-4 w-4" />
                )}
              </button>
            </div>
          </div>
          {frameSrc ? (
            <iframe
              ref={frameRef}
              key={`${frameSrc}:${frameVersion}`}
              title={`${session.instance.name} service`}
              src={frameSrc}
              className="min-h-0 w-full flex-1 border-0 bg-white"
              scrolling="no"
              allow="clipboard-read; clipboard-write; fullscreen; autoplay"
              referrerPolicy="no-referrer"
            />
          ) : certificateConfirmationRequired ? (
            <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
              <ShieldAlert className="h-8 w-8 text-amber-500" />
              <div>
                <p className="text-sm font-semibold text-slate-900">
                  Confirm the runtime HTTPS certificate
                </p>
                <p className="mt-1 max-w-lg break-words text-sm leading-6 text-slate-600">
                  Your browser has not approved the certificate for {certificateOrigin}. Open
                  confirmation in a new tab, review the browser warning, then close that tab and
                  return here. If no tab opens, allow pop-ups for ClawManager and try again.
                </p>
              </div>
              <button
                type="button"
                className="app-button-primary"
                onClick={confirmCertificate}
              >
                Confirm certificate in new tab
              </button>
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-slate-600">
              <RefreshCw className="mr-2 h-5 w-5 animate-spin" />
              {checkingCertificate ? "Checking HTTPS certificate..." : "Opening shared instance..."}
            </div>
          )}
        </section>

        {workspaceVisible && (canShowWorkspace ? (
          <div className="min-h-0 min-w-0">
            {canUseWorkbench && services ? <WorkbenchServicesProvider value={services}>
              <InstanceWorkbench
                key={workbenchKey}
                instanceId={session.instance.id}
                runtimeGeneration={generation}
                isRunning={session.instance.status === "running"}
                visible={workspaceVisible}
                sharedState={currentView.state}
                onWorkbenchEvent={onWorkbenchEvent}
              />
            </WorkbenchServicesProvider> : <WorkspaceFileManager
              instanceId={session.instance.id}
              initialPath={session.workspace_root === "/config" ? "/config" : undefined}
              service={workspaceService}
              workspaceKey={`share:${shareScope ?? code}`}
              canWrite={session.workspace_access === "write"}
            />}
          </div>
        ) : (
          <section className="cm-surface flex min-h-[360px] items-center justify-center p-6 text-center text-sm text-slate-500">
            {session.workspace_access === "none"
              ? "Workspace files are not included in this share link."
              : "This instance does not expose a workspace."}
          </section>
        ))}
      </section>
    </main>
  );
}
