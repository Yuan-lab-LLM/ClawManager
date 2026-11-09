import axios from "axios";
import { ArrowLeft, Maximize2, Minimize2, RefreshCw, RotateCw, ShieldAlert } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { ownerOverviewURL, ownerTeamOverviewURL } from "../../lib/ownerPortalNavigation";
import { WorkspaceFileManager } from "../../components/WorkspaceFileManager";
import { useExpiringResourceRenewal } from "../../hooks/useExpiringResourceRenewal";
import { useOwnerActivity } from "../../hooks/useOwnerActivity";
import { useRuntimeCertificateTrust } from "../../hooks/useRuntimeCertificateTrust";
import { refreshDedicatedRuntimeCookie } from "../../lib/instanceAccess";
import { prepareOpenClawControlUIStorage } from "../../lib/openclawControlStorage";
import {
  ieiSystemService,
  ieiSystemWorkspaceService,
  type IEISystemInstance,
  type IEISystemInstanceAccess,
} from "../../services/ieiSystemService";

// Preserve the personal branch's capability boundary: this Runtime cannot CAS-save text.
const workBuddyWorkspaceService = { ...ieiSystemWorkspaceService, readText: undefined, saveText: undefined };

function resolveEmbedUrl(url: string) {
  if (/^https?:\/\//i.test(url)) return url;
  const explicitOrigin = import.meta.env.VITE_BACKEND_ORIGIN as string | undefined;
  if (explicitOrigin) return new URL(url, explicitOrigin).toString();
  if (window.location.port === "9002" && url.startsWith("/api/")) {
    return `${window.location.protocol}//${window.location.hostname}:9001${url}`;
  }
  return url;
}

function errorMessage(error: unknown, fallback = "无法进入该实例，请稍后重试。") {
  if (axios.isAxiosError(error) && typeof error.response?.data?.error === "string") {
    return error.response.data.error;
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  return fallback;
}

const restartPollIntervalMs = 2_000;
const restartTimeoutMs = 180_000;

function wait(milliseconds: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds));
}

function instanceDisplayName(instance: IEISystemInstance) {
  return instance.alias?.trim() || instance.name;
}

export default function IEISystemInstancePage() {
  const { id = "" } = useParams<{ id: string }>();
  const instanceID = Number(id);
  const location = useLocation();
  const returnSearch = location.state?.portalReturnSearch;
  const returnTeamID = Number(location.state?.portalReturnTeamID);
  const listReturnURL = Number.isSafeInteger(returnTeamID) && returnTeamID>0 ? ownerTeamOverviewURL(returnTeamID,typeof returnSearch==='string'?returnSearch:'') : ownerOverviewURL(instanceID,typeof returnSearch==='string'?returnSearch:'');
  const frameContainerRef = useRef<HTMLElement | null>(null);
  const [instance, setInstance] = useState<IEISystemInstance | null>(null);
  const [access, setAccess] = useState<IEISystemInstanceAccess | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [runtimeError, setRuntimeError] = useState<string | null>(null);
  const requestRef = useRef(0);
  const [frameVersion, setFrameVersion] = useState(0);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [restartDialogOpen, setRestartDialogOpen] = useState(false);
  const [restartInProgress, setRestartInProgress] = useState(false);
  const [restartNotice, setRestartNotice] = useState<string | null>(null);
  const [restartError, setRestartError] = useState<string | null>(null);
  const mountedRef = useRef(true);
  const {onFrameLoad,activityNote}=useOwnerActivity(instanceID,instance?.status==='running'&&!restartInProgress);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const openInstance = useCallback(async () => {
    const request = ++requestRef.current;
    if (!Number.isInteger(instanceID) || instanceID <= 0) {
      setError("实例编号无效。");
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    setRuntimeError(null);
    try {
      // Both calls validate the dedicated IEI session and owner relationship.
      const nextInstance = await ieiSystemService.getInstance(instanceID);
      if (request !== requestRef.current || !mountedRef.current) return;
      setInstance(nextInstance);
      setAccess(null);
      try {
        const nextAccess = await ieiSystemService.generateAccess(instanceID);
        if (request !== requestRef.current || !mountedRef.current) return;
        setAccess(nextAccess);
        setFrameVersion((version) => version + 1);
      } catch (accessError) {
        if (request !== requestRef.current || !mountedRef.current) return;
        if (axios.isAxiosError(accessError) && [401,403,404].includes(accessError.response?.status ?? 0)) {
          setInstance(null);
          setError("访问验证失效，请返回列表重新登录或确认实例权限。");
        } else {
          setRuntimeError(nextInstance.status === "stopped" ? "实例已停止，运行界面暂不可用。" : nextInstance.status === "error" ? "实例运行异常，暂时无法连接运行界面。" : "暂时无法连接运行界面，请稍后重新连接。");
        }
      }
    } catch (openError) {
      if (request !== requestRef.current || !mountedRef.current) return;
      setInstance(null);
      setAccess(null);
      setError(errorMessage(openError));
    } finally {
      if (request === requestRef.current && mountedRef.current) setLoading(false);
    }
  }, [instanceID]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void openInstance();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [openInstance]);

  const renewAccess = useCallback(async () => {
    await ieiSystemService.refreshSession();
    const nextAccess = await ieiSystemService.generateAccess(instanceID);
    await refreshDedicatedRuntimeCookie(resolveEmbedUrl(nextAccess.access_url));
    if (!mountedRef.current) return;
    // Updating the iframe URL would reload the running agent UI. The fresh
    // cookie is installed in the background, so keep the existing src while
    // advancing the local expiry and access metadata.
    setAccess((current) =>
      current ? { ...nextAccess, access_url: current.access_url } : nextAccess,
    );
  }, [instanceID]);

  useExpiringResourceRenewal({
    expiresAt: access?.expires_at,
    renew: renewAccess,
  });

  useEffect(() => {
    const existing = document.querySelector<HTMLMetaElement>('meta[name="referrer"]');
    const previous = existing?.content;
    const meta = existing ?? document.createElement("meta");
    if (!existing) {
      meta.name = "referrer";
      document.head.appendChild(meta);
    }
    meta.content = "no-referrer";
    return () => {
      if (existing) existing.content = previous ?? "";
      else meta.remove();
    };
  }, []);

  useEffect(() => {
    const handleFullscreen = () => setIsFullscreen(document.fullscreenElement === frameContainerRef.current);
    document.addEventListener("fullscreenchange", handleFullscreen);
    return () => document.removeEventListener("fullscreenchange", handleFullscreen);
  }, []);

  const accessFrameUrl = useMemo(() => {
    if (!instance || !access?.access_url) return "";
    const url = resolveEmbedUrl(access.access_url);
    return instance.type.toLowerCase() === "openclaw"
      ? prepareOpenClawControlUIStorage(instance.id, url)
      : url;
  }, [access, instance]);
  const normalizedType = instance?.type.trim().toLowerCase() ?? "";
  const requiresRuntimeCertificateTrust =
    normalizedType === "opencode" || normalizedType === "deepseek-harness";
  const {
    frameUrl: frameSrc,
    checkingCertificate,
    certificateConfirmationRequired,
    confirmCertificate,
  } = useRuntimeCertificateTrust(
    accessFrameUrl || null,
    requiresRuntimeCertificateTrust,
  );

  const handleFullscreen = () => {
    const element = frameContainerRef.current;
    if (!element) return;
    if (document.fullscreenElement === element) void document.exitFullscreen();
    else void element.requestFullscreen().catch(() => undefined);
  };

  const waitForRestartRecovery = useCallback(async (operationID: string) => {
    const deadline = Date.now() + restartTimeoutMs;
    while (Date.now() < deadline) {
      await wait(restartPollIntervalMs);
      if (!mountedRef.current) return;

      const operation = await ieiSystemService.getLifecycleOperation(operationID);
      if (operation.status === "failed") {
        throw new Error(operation.error_message || "实例重启失败，工作区数据已保留。");
      }
      const nextInstance = await ieiSystemService.getInstance(instanceID);
      if (!mountedRef.current) return;
      setInstance(nextInstance);

      const status = nextInstance.status.trim().toLowerCase();
      if (operation.status === "succeeded" && status === "running") {
        const nextAccess = await ieiSystemService.generateAccess(instanceID);
        if (!mountedRef.current) return;
        setAccess(nextAccess);
        setRuntimeError(null);
        setFrameVersion((version) => version + 1);
        return;
      }
      if (status === "failed" || status === "stopped" || status === "deleting") {
        throw new Error("实例未能恢复到运行状态。");
      }
    }
    throw new Error("实例仍在重启，请稍后点击刷新访问。");
  }, [instanceID]);

  const handleRestart = async () => {
    if (!instance || restartInProgress) return;
    setRestartDialogOpen(false);
    setRestartInProgress(true);
    setRestartError(null);
    setRestartNotice("正在重启实例，服务会短暂中断，恢复后将自动重新连接。");
    try {
      const operation = await ieiSystemService.restartInstance(instance.id);
      await waitForRestartRecovery(operation.operation_id);
      if (mountedRef.current) {
        setRestartNotice("实例已完成重启并重新连接。");
      }
    } catch (restartFailure) {
      if (mountedRef.current) {
        setRestartNotice(null);
        setRestartError(errorMessage(restartFailure, "实例重启失败，请稍后重试。"));
      }
    } finally {
      if (mountedRef.current) setRestartInProgress(false);
    }
  };

  if (loading && (!instance || instance.id !== instanceID)) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-slate-100 text-sm text-slate-600">
        <RefreshCw className="mr-2 h-5 w-5 animate-spin" />
        正在验证并进入实例…
      </main>
    );
  }

  if (!instance || instance.id !== instanceID || error) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-slate-100 p-6">
        <section className="w-full max-w-md rounded-xl border border-red-200 bg-white p-6 text-center shadow-sm">
          <h1 className="text-lg font-semibold text-slate-950">实例不可访问</h1>
          <p className="mt-2 text-sm leading-6 text-slate-600">{error ?? "访问验证未通过。"}</p>
          <div className="mt-5 flex justify-center gap-2">
            <Link className="app-button-secondary" to={listReturnURL}>
              <ArrowLeft className="h-4 w-4" /> 返回我的总览
            </Link>
            <button type="button" className="app-button-primary" onClick={() => void openInstance()}>
              <RefreshCw className="h-4 w-4" /> 重试
            </button>
          </div>
        </section>
      </main>
    );
  }

  const canShowWorkspace = instance.workspace_available ?? access?.workspace_available ?? false;
  const isWorkBuddyLite = instance.type.trim().toLowerCase() === "workbuddy" && instance.runtime_type === "gateway";

  return (
    <main className="flex h-screen min-h-[560px] flex-col overflow-hidden bg-slate-100">
      <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b border-slate-200 bg-white px-4">
        <div className="flex min-w-0 items-center gap-3">
          <Link className="cm-icon-button shrink-0" aria-label="返回我的总览" title="返回我的总览" to={listReturnURL}>
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="min-w-0">
            <h1 className="truncate text-base font-semibold text-slate-950">{instanceDisplayName(instance)}</h1>
            <p className="truncate text-[10px] text-slate-500" title={activityNote}>浪潮信息安全访问 · {instance.owner} · {activityNote}</p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            type="button"
            className="app-button-secondary"
            disabled={restartInProgress || loading || !["running", "error"].includes(instance.status.trim().toLowerCase())}
            onClick={() => {
              setRestartError(null);
              setRestartDialogOpen(true);
            }}
          >
            <RotateCw className={`h-4 w-4 ${restartInProgress ? "animate-spin" : ""}`} />
            {restartInProgress ? "正在重启" : "重启实例"}
          </button>
          <span className="inline-flex items-center gap-2 rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700">
            <span className="h-2 w-2 rounded-full bg-emerald-400" />
            已验证
          </span>
        </div>
      </header>

      {(restartNotice || restartError) && (
        <div
          className={`shrink-0 border-b px-4 py-2 text-sm ${
            restartError
              ? "border-red-200 bg-red-50 text-red-700"
              : "border-amber-200 bg-amber-50 text-amber-800"
          }`}
          role="status"
        >
          <div className="flex items-center gap-2">
            <RotateCw className={`h-4 w-4 shrink-0 ${restartInProgress ? "animate-spin" : ""}`} />
            <span>{restartError ?? restartNotice}</span>
          </div>
        </div>
      )}

      <section className="grid min-h-0 flex-1 gap-4 p-4 max-xl:grid-rows-[minmax(420px,1fr)_minmax(360px,0.8fr)] xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)]">
        <section
          ref={frameContainerRef}
          className="cm-surface relative flex min-h-0 min-w-0 flex-col overflow-hidden bg-white"
          style={isFullscreen ? { height: "100vh", width: "100vw", borderRadius: 0 } : undefined}
        >
          <div className="flex h-12 shrink-0 items-center justify-between border-b border-slate-200 px-3">
            <span className="min-w-0 truncate text-sm font-medium text-slate-950">{instanceDisplayName(instance)}</span>
            <div className="flex shrink-0 items-center gap-2">
              <button type="button" className="cm-icon-button" title="刷新访问" onClick={() => void openInstance()}>
                <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
              </button>
              <button type="button" className="cm-icon-button" title={isFullscreen ? "退出全屏" : "全屏"} onClick={handleFullscreen}>
                {isFullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
              </button>
            </div>
          </div>
          {restartInProgress && (
            <div className="absolute inset-x-0 bottom-0 top-12 z-20 flex items-center justify-center bg-white/95 px-6 text-center backdrop-blur-sm">
              <div>
                <RotateCw className="mx-auto h-8 w-8 animate-spin text-blue-600" />
                <p className="mt-4 text-base font-semibold text-slate-950">实例正在重启</p>
                <p className="mt-1 text-sm leading-6 text-slate-600">恢复运行后将自动重新连接，无需刷新页面。</p>
              </div>
            </div>
          )}
          {runtimeError ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-4 p-6 text-center" role="status">
              <ShieldAlert className="h-9 w-9 text-amber-500" />
              <p className="text-base font-semibold text-slate-900">{runtimeError}</p>
              <p className="max-w-md text-sm leading-6 text-slate-600">工作区独立于运行界面加载。可在右侧检查并修改文件；保存后如需生效，请确认配置并手动重启。</p>
              <button className="app-button-secondary" disabled={loading} onClick={() => void openInstance()}>重新连接</button>
            </div>
          ) : frameSrc ? (
            <iframe
              key={`${frameSrc}:${frameVersion}`}
              onLoad={event=>onFrameLoad(event.currentTarget)}
              title={`${instanceDisplayName(instance)} service`}
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
                <p className="text-sm font-semibold text-slate-900">需要确认运行时 HTTPS 证书</p>
                <p className="mt-1 max-w-lg text-sm leading-6 text-slate-600">
                  请继续完成一次浏览器证书确认，确认后会自动返回当前实例页面。
                </p>
              </div>
              <button
                type="button"
                className="app-button-primary"
                onClick={confirmCertificate}
              >
                继续确认
              </button>
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-slate-600">
              <RefreshCw className="mr-2 h-5 w-5 animate-spin" />
              {checkingCertificate ? "正在检查 HTTPS 证书…" : "正在进入实例…"}
            </div>
          )}
        </section>

        {canShowWorkspace ? (
          <div className="min-h-0 min-w-0">
            <WorkspaceFileManager
              instanceId={instance.id}
              key={instance.id}
              initialPath={isWorkBuddyLite ? "/home" : (instance.workspace_root ?? access?.workspace_root) === "/config" ? "/config" : undefined}
              service={isWorkBuddyLite ? workBuddyWorkspaceService : ieiSystemWorkspaceService}
              workspaceKey={`iei:${instance.owner}:${instance.id}`}
              canWrite={!restartInProgress && !["creating", "deleting"].includes(instance.status)}
              localeOverride="zh"
            />
          </div>
        ) : (
          <section className="cm-surface flex min-h-[360px] items-center justify-center p-6 text-center text-sm text-slate-500">
            该实例没有可用的工作区。
          </section>
        )}
      </section>

      {restartDialogOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/40 p-4" role="presentation">
          <section
            className="w-full max-w-md rounded-xl border border-slate-200 bg-white p-6 shadow-2xl"
            role="dialog"
            aria-modal="true"
            aria-labelledby="iei-restart-title"
          >
            <h2 id="iei-restart-title" className="text-lg font-semibold text-slate-950">确认重启实例？</h2>
            <p className="mt-3 text-sm leading-6 text-slate-600">
              重启期间实例会暂时不可访问，当前运行中的任务和会话可能中断；工作区及持久化数据不会删除。
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="app-button-secondary" onClick={() => setRestartDialogOpen(false)}>
                取消
              </button>
              <button type="button" className="app-button-primary" onClick={() => void handleRestart()}>
                <RotateCw className="h-4 w-4" /> 确认重启
              </button>
            </div>
          </section>
        </div>
      )}
    </main>
  );
}
