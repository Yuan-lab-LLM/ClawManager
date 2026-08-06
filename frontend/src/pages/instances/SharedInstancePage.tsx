import axios from "axios";
import { Maximize2, Minimize2, PanelRightClose, PanelRightOpen, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { WorkspaceFileManager } from "../../components/WorkspaceFileManager";
import { prepareOpenClawControlUIStorage } from "../../lib/openclawControlStorage";
import {
  createSharedWorkspaceService,
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
  const frameContainerRef = useRef<HTMLElement | null>(null);
  const [session, setSession] = useState<SharedInstanceSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [frameVersion, setFrameVersion] = useState(0);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [workspaceVisible, setWorkspaceVisible] = useState(true);

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
    const timer = window.setInterval(() => {
      void loadSession({ background: true });
    }, 45 * 60 * 1000);
    return () => window.clearInterval(timer);
  }, [loadSession]);

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

  const workspaceService = useMemo(
    () =>
      session
        ? createSharedWorkspaceService(code, session.csrf_token)
        : null,
    [code, session],
  );

  const frameSrc = useMemo(() => {
    if (!session?.access_url) {
      return "";
    }
    const url = resolveEmbedUrl(session.access_url);
    return session.instance.type.toLowerCase() === "openclaw"
      ? prepareOpenClawControlUIStorage(session.instance.id, url)
      : url;
  }, [session]);

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
          ? "max-xl:grid-rows-[minmax(420px,1fr)_minmax(360px,0.8fr)] xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)]"
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
                  title={workspaceVisible ? "Hide file browser" : "Show file browser"}
                  aria-label={workspaceVisible ? "Hide file browser" : "Show file browser"}
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
          <iframe
            key={`${frameSrc}:${frameVersion}`}
            title={`${session.instance.name} service`}
            src={frameSrc}
            className="min-h-0 w-full flex-1 border-0 bg-white"
            scrolling="no"
            allow="clipboard-read; clipboard-write; fullscreen; autoplay"
            referrerPolicy="no-referrer"
          />
        </section>

        {workspaceVisible && (canShowWorkspace ? (
          <div className="min-h-0 min-w-0">
            <WorkspaceFileManager
              instanceId={session.instance.id}
              initialPath={session.workspace_root === "/config" ? "/config" : undefined}
              service={workspaceService}
              workspaceKey={`share:${code}`}
              canWrite={session.workspace_access === "write"}
            />
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
