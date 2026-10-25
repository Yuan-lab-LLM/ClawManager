import axios from "axios";
import {useOwnerPortalVisit} from '../../hooks/useOwnerPortalVisit';
import { OwnerInstanceCard } from "../../components/OwnerInstanceCard";
import { expandedInstanceFromSearch } from "../../lib/ownerPortalNavigation";
import { OwnerDashboard } from "../../components/OwnerDashboard";
import {
  ArrowRight,
  Briefcase,
  Box,
  CheckCircle2,
  LogOut,
  House,
  Power,
  RefreshCw,
  Search,
  SlidersHorizontal,
  Sparkles,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useExpiringResourceRenewal } from "../../hooks/useExpiringResourceRenewal";
import { getIEIRuntimePresentation } from "../../lib/ieiRuntimeCatalog";
import {
  ieiSystemService,
  type IEISystemInstance,
  type IEISystemLifecycleOperation,
  type IEISystemSession,
} from "../../services/ieiSystemService";

const PAGE_SIZE = 100;

const EMPTY_STATE_RUNTIMES = [
  { type: "openclaw", description: "通用智能体工作台" },
  { type: "hermes", description: "自主研究与知识助手" },
  { type: "opencode", description: "开发者代码工作台" },
  { type: "deepseek-harness", description: "一切皆插件" },
] as const;

function statusClass(status: string) {
  switch (status.toLowerCase()) {
    case "running":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "creating":
    case "resetting":
    case "restarting":
      return "border-amber-200 bg-amber-50 text-amber-700";
    case "error":
      return "border-red-200 bg-red-50 text-red-700";
    default:
      return "border-slate-200 bg-slate-50 text-slate-600";
  }
}

function statusLabel(status: string) {
  switch (status.toLowerCase()) {
    case "running":
      return "运行中";
    case "creating":
      return "创建中";
    case "resetting":
      return "重置中";
    case "restarting":
      return "重启中";
    case "stopped":
      return "已停止";
    case "error":
      return "异常";
    default:
      return status;
  }
}

function InstanceTypeIcon({
  type,
  className = "h-full w-full",
}: {
  type: string;
  className?: string;
}) {
  const normalizedType = type.trim().toLowerCase();
  if (normalizedType === "openclaw") {
    return <img src="/openclaw.png" alt="OpenClaw" className={`${className} object-contain`} />;
  }
  if (normalizedType === "hermes") {
    return <img src="/hermes.png" alt="Hermes" className={`${className} object-contain`} />;
  }
  if (normalizedType === "opencode") {
    return <img src="/opencode.png" alt="OpenCode" className={`${className} object-contain`} />;
  }
  if (normalizedType === "deepseek-harness") {
    return (
      <img
        src="/deepseek-harness.svg"
        alt="DeepSeek Harness"
        className={`${className} object-contain`}
      />
    );
  }
  if (normalizedType === "workbuddy") {
    return <img src="/workbuddy.png" alt="WorkBuddy" className={`${className} object-contain`} />;
  }
  return <Box aria-label={type || "Instance"} className={`${className} text-slate-500`} />;
}

function errorMessage(error: unknown) {
  if (axios.isAxiosError(error) && typeof error.response?.data?.error === "string") {
    return error.response.data.error;
  }
  return "无法加载实例，请稍后重试。";
}

function newIdempotencyKey() {
  if (typeof crypto.randomUUID === "function") return `iei-${crypto.randomUUID()}`;
  return `iei-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function operationIsPending(
  operation: IEISystemLifecycleOperation | undefined,
  instanceStatus: string | undefined,
) {
  if (!operation) return false;
  const operationStatus = operation.status.toLowerCase();
  if (["queued", "processing", "batch_pending"].includes(operationStatus)) return true;
  return operationStatus === "succeeded" && instanceStatus?.toLowerCase() === "creating";
}

function lifecycleDisplayStatus(
  instance: IEISystemInstance,
  operation: IEISystemLifecycleOperation | undefined,
) {
  if (!operationIsPending(operation, instance.status)) return instance.status;
  return operation?.action === "reset" ? "resetting" : "restarting";
}

function installNoReferrerPolicy() {
  const existing = document.querySelector<HTMLMetaElement>('meta[name="referrer"]');
  const previous = existing?.content;
  const meta = existing ?? document.createElement("meta");
  if (!existing) {
    meta.name = "referrer";
    document.head.appendChild(meta);
  }
  meta.content = "no-referrer";
  return () => {
    if (existing) meta.content = previous ?? "";
    else meta.remove();
  };
}

function formatTime(value?: string) {
  if (!value) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function instanceDisplayName(instance: IEISystemInstance) {
  return instance.alias?.trim() || instance.name;
}

function selectedInstanceIDFromURL() {
  const value = Number(new URLSearchParams(window.location.search).get("selected_instance_id"));
  return Number.isInteger(value) && value > 0 ? value : null;
}

export default function IEISystemListInstancesPage() {
  const initialized = useRef(false);
  const [session, setSession] = useState<IEISystemSession | null>(null);
  useOwnerPortalVisit(session?.owner);
  const [expandedInstanceID, setExpandedInstanceID] = useState<number|null>(()=>expandedInstanceFromSearch(window.location.search));
  const [instances, setInstances] = useState<IEISystemInstance[]>([]);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [view, setView] = useState(() => {
    const saved=new URLSearchParams(window.location.search).get('view');
    return saved==='overview'?'overview':saved==='instance'||selectedInstanceIDFromURL()?'instance':'overview';
  });
  const [query, setQuery] = useState(() => new URLSearchParams(window.location.search).get('q') || '');
  const [runtimeFilter, setRuntimeFilter] = useState(() => new URLSearchParams(window.location.search).get('runtime') || 'all');
  const [showFilters, setShowFilters] = useState(false);
  const [menuInstanceID, setMenuInstanceID] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const restoreExpandedCard = useRef(expandedInstanceID);
  useEffect(() => {
    if (loading || !restoreExpandedCard.current) return;
    const card = document.querySelector(`[data-instance-id="${restoreExpandedCard.current}"]`);
    if (!card) return;
    card.scrollIntoView({block:'nearest'});
    restoreExpandedCard.current = null;
  }, [loading, instances]);
  const [refreshing,setRefreshing]=useState(false);
  const [refreshError,setRefreshError]=useState('');
  const refreshInFlight=useRef(false);
  const dashboardRefresh=useRef<(()=>Promise<void>)|null>(null);
  const [lifecycleOperations, setLifecycleOperations] = useState<
    Record<number, IEISystemLifecycleOperation>
  >({});
  const [lifecycleErrors, setLifecycleErrors] = useState<Record<number, string>>({});
  const [lifecycleWarnings, setLifecycleWarnings] = useState<Record<number, string>>({});
  const [error, setError] = useState<string | null>(null);

  const loadInstances = useCallback(async (preferredID?: number | null) => {
    const first = await ieiSystemService.listInstances(1, PAGE_SIZE);
    const items = [...(first.instances ?? [])];
    const pages = Math.ceil(first.total / PAGE_SIZE);
    for (let page = 2; page <= pages; page += 1) {
      const next = await ieiSystemService.listInstances(page, PAGE_SIZE);
      items.push(...(next.instances ?? []));
    }
    setInstances(items);
    setSelectedID((current) => {
      const candidate = preferredID ?? current;
      return candidate && items.some((instance) => instance.id === candidate)
        ? candidate
        : (items[0]?.id ?? null);
    });
  }, []);

  const refreshPortal=async()=>{
    if(refreshInFlight.current)return;
    refreshInFlight.current=true;setRefreshing(true);setRefreshError('');
    try{
      const results=await Promise.allSettled([
        ieiSystemService.getSession().then(setSession),
        loadInstances(),
        dashboardRefresh.current?.()
      ]);
      if(results.some(r=>r.status==='rejected'))setRefreshError('部分数据刷新失败，已保留原内容，请重试。');
    }finally{refreshInFlight.current=false;setRefreshing(false)}
  };

  const initialize = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams(window.location.search);
      const token = params.get("token")?.trim();
      const preferredID = selectedInstanceIDFromURL();
      if (token) {
        params.delete("token");
        const queryString = params.toString();
        window.history.replaceState(
          {},
          "",
          `${window.location.pathname}${queryString ? `?${queryString}` : ""}${window.location.hash}`,
        );
      }
      const nextSession = token
        ? await ieiSystemService.exchangeSession(token)
        : await ieiSystemService.getSession();
      setSession(nextSession);
      await loadInstances(preferredID);
    } catch (loadError) {
      setSession(null);
      setInstances([]);
      setSelectedID(null);
      setError(errorMessage(loadError));
    } finally {
      setLoading(false);
    }
  }, [loadInstances]);

  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    void initialize();
  }, [initialize]);

  const renewSession = useCallback(async () => {
    setSession(await ieiSystemService.refreshSession());
  }, []);
  useExpiringResourceRenewal({
    expiresAt: session?.expires_at,
    renew: renewSession,
  });

  useEffect(() => installNoReferrerPolicy(), []);

  useEffect(() => {
    if (!initialized.current) return;
    const params = new URLSearchParams(window.location.search);
    params.set('view', view);
    if(expandedInstanceID)params.set('expanded_instance_id',String(expandedInstanceID));else params.delete('expanded_instance_id');
    if (query) params.set('q', query); else params.delete('q');
    if (runtimeFilter !== 'all') params.set('runtime', runtimeFilter); else params.delete('runtime');
    if (selectedID) params.set("selected_instance_id", String(selectedID));
    else params.delete("selected_instance_id");
    params.delete("token");
    const queryString = params.toString();
    window.history.replaceState(
      {},
      "",
      `${window.location.pathname}${queryString ? `?${queryString}` : ""}${window.location.hash}`,
    );
  }, [selectedID, view, query, runtimeFilter, expandedInstanceID]);

  const runtimeOptions = useMemo(() => {
    const types = [...new Set(instances.map((instance) => instance.type.trim().toLowerCase()))];
    return types.map((type) => ({ type, label: getIEIRuntimePresentation(type).name }));
  }, [instances]);

  const visibleInstances = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return instances.filter((instance) => {
      const matchesRuntime = runtimeFilter === "all" || instance.type.toLowerCase() === runtimeFilter;
      const matchesQuery =
        !normalizedQuery ||
        (instance.alias?.toLowerCase().includes(normalizedQuery) ?? false) ||
        instance.name.toLowerCase().includes(normalizedQuery) ||
        String(instance.id).includes(normalizedQuery) ||
        getIEIRuntimePresentation(instance.type).name.toLowerCase().includes(normalizedQuery);
      return matchesRuntime && matchesQuery;
    });
  }, [instances, query, runtimeFilter]);

  const selectedInstance =
    instances.find((instance) => instance.id === selectedID) ??
    visibleInstances[0] ??
    instances[0] ??
    null;
  const selectedRuntime = selectedInstance
    ? getIEIRuntimePresentation(selectedInstance.type)
    : null;
  const selectedOperation = selectedInstance
    ? lifecycleOperations[selectedInstance.id]
    : undefined;
  const selectedLifecyclePending = selectedInstance
    ? operationIsPending(selectedOperation, selectedInstance.status)
    : false;
  const selectedDisplayStatus = selectedInstance
    ? lifecycleDisplayStatus(selectedInstance, selectedOperation)
    : "";
  const selectedLifecycleError = selectedInstance
    ? lifecycleErrors[selectedInstance.id]
    : undefined;
  const selectedLifecycleWarning = selectedInstance
    ? lifecycleWarnings[selectedInstance.id]
    : undefined;

  const selectedInstanceID = selectedInstance?.id;
  const selectedInstanceStatus = selectedInstance?.status;
  useEffect(() => {
    if (!selectedInstanceID) return;
    let cancelled = false;
    void ieiSystemService
      .getLatestLifecycleOperation(selectedInstanceID)
      .then((operation) => {
        if (cancelled || !operation) return;
        const relevant = operationIsPending(operation, selectedInstanceStatus) ||
          operation.status === "failed" ||
          operation.error_code === "OLD_INSTANCE_CLEANUP_PENDING";
        if (!relevant) return;
        setLifecycleOperations((current) => ({ ...current, [selectedInstanceID]: operation }));
        if (operation.status === "failed") {
          setLifecycleErrors((current) => ({
            ...current,
            [selectedInstanceID]: operation.error_message || "实例操作失败，工作区数据已保留。",
          }));
        }
        if (operation.error_code === "OLD_INSTANCE_CLEANUP_PENDING") {
          setLifecycleWarnings((current) => ({
            ...current,
            [selectedInstanceID]: "新实例已可用；旧实例已从您的门户移除，后台正在等待管理员清理。",
          }));
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [selectedInstanceID, selectedInstanceStatus]);

  const pendingLifecycleOperations = useMemo(
    () => Object.entries(lifecycleOperations)
      .map(([sourceID, operation]) => ({ sourceID: Number(sourceID), operation }))
      .filter(({ sourceID, operation }) => {
        const instance = instances.find((item) => item.id === sourceID);
        return operationIsPending(operation, instance?.status);
      }),
    [instances, lifecycleOperations],
  );
  const pendingLifecycleSignature = pendingLifecycleOperations
    .map(({ sourceID, operation }) => `${sourceID}:${operation.operation_id}:${operation.status}`)
    .sort()
    .join("|");
  const pendingLifecycleOperationsRef = useRef(pendingLifecycleOperations);
  pendingLifecycleOperationsRef.current = pendingLifecycleOperations;

  useEffect(() => {
    const pending = pendingLifecycleOperationsRef.current;
    if (pending.length === 0) return;

    let cancelled = false;
    const refresh = async () => {
      const results = await Promise.allSettled(
        pending.map(({ operation }) => ieiSystemService.getLifecycleOperation(operation.operation_id)),
      );
      if (cancelled) return;
      let preferredReplacementID: number | null = null;
      setLifecycleOperations((current) => {
        const next = { ...current };
        results.forEach((result, index) => {
          if (result.status === "fulfilled") {
            const sourceID = pending[index].sourceID;
            const operation = result.value;
            if (
              operation.status === "succeeded" &&
              operation.action === "reset" &&
              operation.instance_id &&
              operation.instance_id !== sourceID
            ) {
              delete next[sourceID];
              next[operation.instance_id] = operation;
              preferredReplacementID = operation.instance_id;
            } else {
              next[sourceID] = operation;
            }
          }
        });
        return next;
      });
      results.forEach((result, index) => {
        if (result.status === "fulfilled" && result.value.status === "failed") {
          const instanceID = pending[index].sourceID;
          setLifecycleErrors((current) => ({
            ...current,
            [instanceID]: result.value.error_message || "实例操作失败，工作区数据已保留。",
          }));
        }
        if (
          result.status === "fulfilled" &&
          result.value.status === "succeeded" &&
          result.value.error_code === "OLD_INSTANCE_CLEANUP_PENDING" &&
          result.value.instance_id
        ) {
          setLifecycleWarnings((current) => ({
            ...current,
            [result.value.instance_id!]: "新实例已可用；旧实例已从您的门户移除，后台正在等待管理员清理。",
          }));
        }
      });
      await loadInstances(preferredReplacementID).catch(() => undefined);
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 1500);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [pendingLifecycleSignature, loadInstances]);
  const handleLogout = async () => {
    await ieiSystemService.clearSession().catch(() => undefined);
    setSession(null);
    setInstances([]);
    setSelectedID(null);
    setError("访问会话已退出，请从智慧协作平台重新进入。");
  };

  const handleRestart = async (target = selectedInstance) => {
    if (!target || operationIsPending(lifecycleOperations[target.id], target.status) || !["running", "error"].includes(target.status.toLowerCase())) return;
    if (!window.confirm(`确认重启实例“${instanceDisplayName(target)}”？工作区数据会保留。`)) return;
    const instanceID = target.id;
    setMenuInstanceID(null);
    setLifecycleErrors((current) => {
      const next = { ...current };
      delete next[instanceID];
      return next;
    });
    setLifecycleWarnings((current) => {
      const next = { ...current };
      delete next[instanceID];
      return next;
    });
    try {
      const operation = await ieiSystemService.restartInstance(instanceID, newIdempotencyKey());
      setLifecycleOperations((current) => ({ ...current, [instanceID]: operation }));
      await loadInstances();
    } catch (actionError) {
      setLifecycleErrors((current) => ({ ...current, [instanceID]: errorMessage(actionError) }));
    }
  };

  const handleReset = async (target = selectedInstance) => {
    if (!target || operationIsPending(lifecycleOperations[target.id], target.status)) return;
    const status = target.status.toLowerCase();
    if (!["running", "stopped", "error"].includes(status)) return;
    if (
      !window.confirm(
        `重置实例“${instanceDisplayName(target)}”将永久删除其中的全部文件、配置、技能、任务和会话。\n\n系统不会自动备份，请先下载需要保留的数据。是否继续？`,
      )
    ) return;
    if (
      !window.confirm(
        `最后确认：重置成功后，实例“${instanceDisplayName(target)}”的原数据无法恢复。\n\n确定清空全部数据并重新初始化实例吗？`,
      )
    ) return;
    const instanceID = target.id;
    setMenuInstanceID(null);
    setLifecycleErrors((current) => {
      const next = { ...current };
      delete next[instanceID];
      return next;
    });
    setLifecycleWarnings((current) => {
      const next = { ...current };
      delete next[instanceID];
      return next;
    });
    try {
      const operation = await ieiSystemService.resetInstance(instanceID, newIdempotencyKey());
      setLifecycleOperations((current) => ({ ...current, [instanceID]: operation }));
      await loadInstances();
    } catch (actionError) {
      setLifecycleErrors((current) => ({ ...current, [instanceID]: errorMessage(actionError) }));
    }
  };

  return (
    <main className={`flex min-h-screen flex-col bg-[#f4f7fb] text-slate-900 ${view==='overview'?'portal-overview':''}`}>
      <header className="shrink-0 border-b border-slate-200 bg-white/95 shadow-[0_1px_16px_rgba(15,23,42,0.025)] backdrop-blur">
        <div className="flex min-h-[64px] flex-wrap items-center justify-between gap-3 px-4 py-2 sm:flex-nowrap sm:gap-6 sm:px-7 lg:px-9">
          <div className="flex min-w-0 shrink-0 items-center gap-4">
            <img
              src="/inspur-information.png"
              alt="浪潮信息"
              className="h-auto w-[145px] object-contain object-left"
            />
            <div className="hidden h-8 w-px bg-slate-200 sm:block" />
            <div className="hidden whitespace-nowrap text-sm font-medium tracking-[0.08em] text-slate-400 sm:block">
              智慧协作门户
            </div>
          </div>
          {session ? (
            <div className="flex items-center gap-2 sm:gap-4">
              <div className="hidden min-w-0 text-right lg:block">
                <div className="flex items-center justify-end gap-2 text-sm font-semibold text-slate-800">
                  <CheckCircle2 className="h-4 w-4 text-emerald-500" />
                  已验证所有者
                </div>
                <p className="mt-1 max-w-64 truncate text-sm text-slate-500">{session.owner}</p>
              </div>
              <div className="hidden h-8 w-px bg-slate-200 lg:block" />
              {instances.length>0&&<button type="button" onClick={() => {setView('overview');setMenuInstanceID(null);}} aria-pressed={view==='overview'} className="inline-flex h-8 items-center gap-2 rounded-lg border border-slate-200 bg-white px-4 text-sm font-semibold text-slate-700 transition hover:border-blue-300 hover:text-blue-700">
                <House className="h-4 w-4" />我的总览
              </button>}
              <button
                type="button"
                className="inline-flex h-8 items-center gap-2 rounded-lg border border-slate-200 bg-white px-4 text-sm font-semibold text-slate-700 transition hover:border-blue-300 hover:text-blue-700 disabled:opacity-60"
                onClick={() => void refreshPortal()}
                disabled={loading||refreshing}
              >
                <RefreshCw className={`h-4 w-4 ${loading||refreshing ? "animate-spin" : ""}`} />
                刷新
              </button>
              <button
                type="button"
                className="inline-flex h-8 items-center gap-2 rounded-lg border border-slate-200 bg-white px-4 text-sm font-semibold text-slate-700 transition hover:border-red-200 hover:text-red-600"
                onClick={() => void handleLogout()}
              >
                <LogOut className="h-4 w-4" />
                退出
              </button>
            </div>
          ) : null}
        </div>
      </header>

      {refreshError&&<p role="alert" className="fixed right-4 top-16 z-30 rounded-lg border border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-800">{refreshError}</p>}
      <section className="portal-content min-h-0 flex-1 p-4 lg:p-5">
        {error ? (
          <div className="mx-auto mt-16 max-w-lg rounded-2xl border border-red-200 bg-white p-7 text-center shadow-sm">
            <h2 className="text-lg font-semibold">访问验证失败</h2>
            <p className="mt-2 text-sm leading-6 text-slate-600">{error}</p>
          </div>
        ) : loading ? (
          <div className="flex h-[65vh] items-center justify-center text-sm text-slate-600">
            <RefreshCw className="mr-2 h-5 w-5 animate-spin" />
            正在验证身份并加载实例…
          </div>
        ) : instances.length === 0 ? (
          <div className="relative mx-auto flex min-h-[calc(100vh-132px)] max-w-6xl items-center justify-center overflow-hidden rounded-2xl border border-blue-100 bg-[#f3f7ff] px-5 py-10 text-center shadow-[0_12px_35px_rgba(15,23,42,0.035)] sm:px-8">
            <div className="pointer-events-none absolute -left-24 -top-24 h-64 w-64 rounded-full bg-blue-100/60" />
            <div className="pointer-events-none absolute -bottom-36 -right-28 h-72 w-72 rounded-full bg-blue-100/60" />
            <div className="relative flex w-full max-w-4xl flex-col items-center">
              <div className="flex h-[70px] w-[70px] items-center justify-center rounded-2xl bg-white text-blue-600 shadow-[0_12px_28px_rgba(37,99,235,0.10)]">
                <Briefcase className="h-7 w-7" />
              </div>
              <h2 className="mt-5 text-2xl font-semibold tracking-tight text-[#10203b]">
                工作空间尚未分配
              </h2>
              <p className="mt-2 text-sm leading-6 text-slate-500">
                实例由智慧协作平台统一分配，分配完成后会自动出现在这里。
              </p>
              <button
                type="button"
                className="mt-6 inline-flex h-8 items-center gap-2 rounded-lg bg-gradient-to-r from-blue-700 to-blue-600 px-5 text-sm font-semibold text-white shadow-[0_9px_20px_rgba(37,99,235,0.18)] transition hover:from-blue-800 hover:to-blue-700 disabled:opacity-60"
                onClick={() => void refreshPortal()}
                disabled={loading||refreshing}
              >
                <RefreshCw className={`h-4 w-4 ${loading||refreshing ? "animate-spin" : ""}`} />
                重新检查
              </button>
              <div className="mt-6 flex flex-col items-center gap-2 text-xs text-slate-500 sm:flex-row sm:gap-6">
                <span className="inline-flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4 text-emerald-500" />
                  所有者身份已验证
                </span>
                <span className="inline-flex items-center gap-2">
                  <RefreshCw className="h-4 w-4 text-slate-400" />
                  实例状态自动同步
                </span>
              </div>

              <div className="mt-9 w-full rounded-2xl border border-blue-100 bg-white/80 px-4 pb-3 pt-4 shadow-[0_9px_24px_rgba(60,96,149,0.05)] sm:px-5">
                <div className="flex flex-col items-start justify-between gap-1 px-1 pb-3 text-left sm:flex-row sm:items-center">
                  <h3 className="text-sm font-semibold text-slate-800">支持的工作空间</h3>
                  <p className="text-xs text-slate-400">分配后将在门户中自动显示</p>
                </div>
                <div className="grid grid-cols-1 border-t border-slate-100 sm:grid-cols-2 md:grid-cols-5">
                  {EMPTY_STATE_RUNTIMES.map((runtime, index) => {
                    const presentation = getIEIRuntimePresentation(runtime.type);
                    return (
                      <div
                        key={runtime.type}
                        className={`flex min-w-0 items-center gap-3 border-slate-100 px-3 py-4 text-left md:min-h-[114px] md:flex-col md:justify-center md:gap-0 md:border-l md:px-1 md:text-center ${
                          index === 0 ? "md:border-l-0" : ""
                        } ${index > 0 ? "border-t sm:border-t-0" : ""}`}
                      >
                        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border border-slate-200 bg-white p-1.5">
                          <InstanceTypeIcon type={runtime.type} />
                        </div>
                        <div className="min-w-0 md:mt-2 md:w-full">
                          <p
                            className={`whitespace-nowrap font-medium text-slate-700 ${
                              runtime.type === "deepseek-harness" ? "text-xs" : "text-[13px]"
                            }`}
                          >
                            {presentation.name}
                          </p>
                          <p className="mt-1 text-[11px] leading-4 text-slate-400">
                            {runtime.description}
                          </p>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          </div>
        ) : selectedInstance && selectedRuntime ? (
          <div className={view==='overview'?'portal-overview-grid grid items-start gap-3 lg:grid-cols-[320px_minmax(0,1fr)] xl:grid-cols-[350px_minmax(0,1fr)]':'grid gap-4 lg:h-[calc(100vh-132px)] lg:min-h-[640px] lg:grid-cols-[minmax(280px,330px)_minmax(460px,1fr)_minmax(280px,320px)] 2xl:grid-cols-[minmax(320px,380px)_minmax(560px,1fr)_minmax(320px,360px)]'}>
            <aside className={`portal-instance-sidebar flex flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_12px_35px_rgba(15,23,42,0.045)] lg:min-h-0 ${view==='overview'?'max-h-[360px] lg:max-h-[calc(100vh-132px)]':'min-h-[680px]'}`}>
              <div className="flex items-center justify-between border-b border-slate-100 px-5 py-5">
                <h2 className="text-base font-bold text-[#14213a]">我的实例 · {instances.length}</h2>
                <button
                  type="button"
                  className={`cm-icon-button h-9 w-9 ${showFilters ? "border-blue-200 bg-blue-50 text-blue-600" : ""}`}
                  title="筛选运行时"
                  onClick={() => setShowFilters((current) => !current)}
                >
                  <SlidersHorizontal className="h-4 w-4" />
                </button>
              </div>
              <div className="space-y-3 px-4 py-4">
                <label className="relative block">
                  <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
                  <input
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder="搜索实例名称或 ID…"
                    className="h-11 w-full rounded-lg border border-slate-200 bg-slate-50/60 pl-10 pr-3 text-sm outline-none transition placeholder:text-slate-400 focus:border-blue-300 focus:bg-white focus:ring-2 focus:ring-blue-100"
                  />
                </label>
                {showFilters ? (
                  <select
                    aria-label="筛选运行时"
                    value={runtimeFilter}
                    onChange={(event) => setRuntimeFilter(event.target.value)}
                    className="h-10 w-full rounded-lg border border-slate-200 bg-white px-3 text-sm text-slate-700 outline-none focus:border-blue-300"
                  >
                    <option value="all">全部 Runtime</option>
                    {runtimeOptions.map((option) => (
                      <option key={option.type} value={option.type}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                ) : null}
              </div>
              <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 pb-4">
                {visibleInstances.length === 0 ? (
                  <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">
                    没有符合条件的实例
                  </div>
                ) : (
                  visibleInstances.map((instance) => {
                    const selected = view === 'instance' && instance.id === selectedInstance.id;
                    const displayStatus = lifecycleDisplayStatus(
                      instance,
                      lifecycleOperations[instance.id],
                    );
                    return (
                      <div key={instance.id}>
                        <OwnerInstanceCard instance={instance} expanded={expandedInstanceID===instance.id} selected={selected} status={statusLabel(displayStatus)} pending={operationIsPending(lifecycleOperations[instance.id],instance.status)} menuOpen={menuInstanceID===instance.id}
                          onToggle={()=>{setExpandedInstanceID(current=>current===instance.id?null:instance.id);setMenuInstanceID(null)}}
                          onMenu={()=>setMenuInstanceID(current=>current===instance.id?null:instance.id)} onCloseMenu={()=>setMenuInstanceID(null)}
                          onDetails={()=>{setSelectedID(instance.id);setView('instance')}}
                          onRestart={()=>void handleRestart(instance)} onReset={()=>void handleReset(instance)}/>
                      {lifecycleErrors[instance.id]&&<p role="alert" className="px-2 py-1 text-xs text-rose-600">{lifecycleErrors[instance.id]}</p>}
                      </div>
                    );
                  })
                )}
              </div>
            </aside>

            {view==='overview' ? <div className="portal-dashboard-host min-w-0"><OwnerDashboard refreshAction={dashboardRefresh} owner={session?.owner??''} instances={instances} onSelect={id=>{if(instances.some(i=>i.id===id)){setSelectedID(id);setView('instance');}}}/></div> : <>
            <section className="flex min-h-[680px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_12px_35px_rgba(15,23,42,0.045)] lg:min-h-0">
              <div className="border-b border-slate-100 px-6 py-5">
                <h2 className="text-base font-bold text-[#14213a]">运行时说明</h2>
              </div>
              <div className="flex-1 overflow-y-auto p-6">
                {selectedLifecycleError ? (
                  <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                    {selectedLifecycleError}
                  </div>
                ) : null}
                {selectedLifecycleWarning ? (
                  <div className="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
                    {selectedLifecycleWarning}
                  </div>
                ) : null}
                {selectedLifecyclePending ? (
                  <div className="mb-4 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-sm text-blue-700">
                    正在{selectedOperation?.action === "reset" ? "重置" : "重启"}实例。页面会持续同步状态，期间无法进入该实例。
                  </div>
                ) : null}
                <div className="flex flex-col gap-5 border-b border-slate-100 pb-6 sm:flex-row sm:items-center">
                  <div
                    className={`flex h-24 w-24 shrink-0 items-center justify-center rounded-2xl border ${selectedRuntime.theme.border} ${selectedRuntime.theme.accentSoft} p-4`}
                  >
                    <InstanceTypeIcon type={selectedInstance.type} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-3">
                      <h1 className="truncate text-2xl font-bold tracking-tight text-[#10203b]">
                        {instanceDisplayName(selectedInstance)}
                      </h1>
                      <span
                        className={`rounded-full border px-2.5 py-1 text-xs font-semibold ${statusClass(selectedDisplayStatus)}`}
                      >
                        {statusLabel(selectedDisplayStatus)}
                      </span>
                    </div>
                    <p className="mt-2 text-base text-slate-500">
                      {selectedRuntime.name} · #{selectedInstance.id}
                    </p>
                    <p
                      className={`mt-2 flex items-center gap-2 text-sm font-medium ${selectedRuntime.theme.accent}`}
                    >
                      <span className={`h-2 w-2 rounded-full ${selectedRuntime.theme.dot}`} />
                      {selectedRuntime.category}
                    </p>
                  </div>
                  <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                  <button
                    type="button"
                    onClick={() => void handleReset()}
                    disabled={
                      selectedLifecyclePending ||
                      !["running", "stopped", "error"].includes(selectedInstance.status.toLowerCase())
                    }
                    className="inline-flex h-12 items-center justify-center gap-2 rounded-lg border border-rose-200 bg-rose-50 px-5 text-sm font-semibold text-rose-700 transition hover:border-rose-300 hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <RefreshCw className={`h-4 w-4 ${selectedLifecyclePending && selectedOperation?.action === "reset" ? "animate-spin" : ""}`} />
                    重置实例
                  </button>
                  <button
                    type="button"
                    onClick={() => void handleRestart()}
                  disabled={selectedLifecyclePending || !["running", "error"].includes(selectedInstance.status.toLowerCase())}
                    className="inline-flex h-12 items-center justify-center gap-2 rounded-lg border border-blue-200 bg-blue-50 px-5 text-sm font-semibold text-blue-700 transition hover:border-blue-300 hover:bg-blue-100 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <Power className={`h-4 w-4 ${selectedLifecyclePending && selectedOperation?.action === "restart" ? "animate-pulse" : ""}`} />
                    重启实例
                  </button>
                  {selectedInstance.status.toLowerCase() === "deleting" ? (
                    <button
                      type="button"
                      disabled
                      className="inline-flex h-12 shrink-0 cursor-not-allowed items-center justify-center gap-2 rounded-lg bg-slate-300 px-6 text-sm font-semibold text-white"
                    >
                      进入实例
                      <ArrowRight className="h-4 w-4" />
                    </button>
                  ) : (
                    <Link
                      to={`/ieisystem/instances/${selectedInstance.id}`}
                      state={{portalReturnSearch:window.location.search}}
                      className="inline-flex h-12 shrink-0 items-center justify-center gap-2 rounded-lg bg-gradient-to-r from-blue-700 to-blue-600 px-6 text-sm font-semibold text-white shadow-[0_10px_24px_rgba(37,99,235,0.22)] transition hover:-translate-y-0.5 hover:from-blue-800 hover:to-blue-700"
                    >
                      进入实例
                      <ArrowRight className="h-4 w-4" />
                    </Link>
                  )}
                  </div>
                </div>

                <div className={`mt-6 overflow-hidden rounded-2xl border ${selectedRuntime.theme.border}`}>
                  <div className={`border-b ${selectedRuntime.theme.border} ${selectedRuntime.theme.accentSoft} px-6 py-5`}>
                    <div className="flex flex-wrap items-center gap-3">
                      <span className={`text-xs font-bold uppercase tracking-[0.18em] ${selectedRuntime.theme.accent}`}>
                        {selectedRuntime.name}
                      </span>
                      {selectedRuntime.badge ? (
                        <span className="rounded-full bg-gradient-to-r from-teal-500 to-cyan-500 px-2.5 py-1 text-[10px] font-bold tracking-wider text-white">
                          {selectedRuntime.badge}
                        </span>
                      ) : null}
                    </div>
                    <h3 className="mt-2 text-xl font-bold leading-snug text-[#10203b]">
                      {selectedRuntime.tagline}
                    </h3>
                  </div>

                  <div className="grid divide-y divide-slate-100 bg-white xl:grid-cols-5 xl:divide-x xl:divide-y-0">
                    <div className="px-6 py-5 xl:col-span-3">
                      <h3 className="text-sm font-bold text-slate-900">运行时定位</h3>
                      <p className="mt-3 text-sm leading-7 text-slate-600">
                        {selectedRuntime.positioning}
                      </p>
                    </div>
                    <div className="px-6 py-5 xl:col-span-2">
                      <h3 className="text-sm font-bold text-slate-900">为什么选择它</h3>
                      <p className="mt-3 text-sm leading-7 text-slate-600">
                        {selectedRuntime.selectionGuide}
                      </p>
                    </div>
                  </div>
                </div>

                <div className="mt-5 rounded-2xl border border-slate-200 p-6">
                  <div className="flex items-center justify-between gap-4">
                    <h3 className="text-sm font-bold text-slate-900">核心能力</h3>
                    <span className="text-xs text-slate-400">面向实际任务的能力组合</span>
                  </div>
                  <ul className="mt-4 grid gap-3 sm:grid-cols-2 2xl:grid-cols-3">
                    {selectedRuntime.capabilities.map((capability, index) => (
                      <li
                        key={capability}
                        className="flex min-h-14 items-center gap-3 rounded-xl border border-slate-100 bg-slate-50/70 px-4 py-3 text-sm font-medium text-slate-700"
                      >
                        <span
                          className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${selectedRuntime.theme.accentSoft} text-[11px] font-bold ${selectedRuntime.theme.accent}`}
                        >
                          {index + 1}
                        </span>
                        {capability}
                      </li>
                    ))}
                  </ul>
                </div>

                <div className="mt-5 rounded-2xl border border-slate-200 bg-slate-50/50 p-6">
                  <h3 className="text-sm font-bold text-slate-900">适用场景</h3>
                  <p className="mt-3 text-sm leading-7 text-slate-600">
                    {selectedRuntime.scenarios}
                  </p>
                  {selectedRuntime.notice ? (
                    <div className="mt-4 flex gap-3 rounded-xl border border-teal-200 bg-teal-50 p-3.5 text-xs leading-5 text-teal-800">
                      <Sparkles className="mt-0.5 h-4 w-4 shrink-0" />
                      {selectedRuntime.notice}
                    </div>
                  ) : null}
                </div>

                <div className="mt-6">
                  <h3 className="text-sm font-bold text-slate-900">能力标签</h3>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {selectedRuntime.capabilities.map((capability) => (
                      <span
                        key={capability}
                        className={`rounded-md border ${selectedRuntime.theme.border} ${selectedRuntime.theme.accentSoft} px-3 py-1.5 text-xs font-medium ${selectedRuntime.theme.accent}`}
                      >
                        {capability}
                      </span>
                    ))}
                  </div>
                </div>
              </div>
            </section>

            <aside className="flex min-h-[680px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_12px_35px_rgba(15,23,42,0.045)] lg:min-h-0">
              <div className="border-b border-slate-100 px-5 py-5">
                <h2 className="text-base font-bold text-[#14213a]">实例信息</h2>
              </div>
              <div className="flex-1 overflow-y-auto px-5 py-6">
                <div className="text-center">
                  <div
                    className={`mx-auto flex h-16 w-16 items-center justify-center rounded-2xl border ${selectedRuntime.theme.border} ${selectedRuntime.theme.accentSoft} p-3`}
                  >
                    <InstanceTypeIcon type={selectedInstance.type} />
                  </div>
                  <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
                    <h3 className="max-w-full truncate text-lg font-bold text-[#10203b]">
                      {instanceDisplayName(selectedInstance)}
                    </h3>
                  </div>
                  <span
                    className={`mt-3 inline-flex rounded-full border px-2.5 py-1 text-xs font-semibold ${statusClass(selectedInstance.status)}`}
                  >
                    {statusLabel(selectedInstance.status)}
                  </span>
                </div>

                <div className="mt-6 overflow-hidden rounded-xl border border-slate-200">
                  <dl className="divide-y divide-slate-100 px-4">
                    {[
                      [selectedInstance.alias?.trim() ? "实例别名" : "实例名称", instanceDisplayName(selectedInstance)],
                      ...(selectedInstance.alias?.trim() ? [["技术名称", selectedInstance.name]] : []),
                      ["实例 ID", `#${selectedInstance.id}`],
                      ["运行时", selectedRuntime.name],
                      ["运行时类型", selectedRuntime.category],
                      ["状态", statusLabel(selectedInstance.status)],
                      ["更新时间", formatTime(selectedInstance.updated_at)],
                    ].map(([label, value]) => (
                      <div key={label} className="py-3 text-sm">
                        <dt className="text-xs text-slate-400">{label}</dt>
                        <dd className="mt-1 break-words font-medium text-slate-700">{value}</dd>
                      </div>
                    ))}
                  </dl>
                </div>

                <div className="mt-7">
                  <h4 className="text-sm font-bold text-slate-900">实例描述</h4>
                  <p className="mt-2 text-sm leading-7 text-slate-600">
                    {selectedInstance.description?.trim() || "暂未填写实例描述。"}
                  </p>
                </div>
              </div>
            </aside>
            </>}
          </div>
        ) : null}
      </section>
    </main>
  );
}
