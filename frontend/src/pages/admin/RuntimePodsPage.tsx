import { useCallback, useEffect, useRef, useState } from "react";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, RefreshCw, Search } from "lucide-react";
import AdminLayout from "../../components/AdminLayout";
import { useI18n } from "../../contexts/I18nContext";
import { useRuntimeAdminWebSocket } from "../../hooks/useWebSocket";
import { runtimePoolService } from "../../services/runtimePoolService";
import { formatInstanceType } from "../../types/instance";
import type { RuntimePod, RuntimePodSummary } from "../../types/runtimePool";
import {
  isSchedulable,
  canManageScheduling,
  runtimeError,
  summarizePod,
  formatCPU,
  formatMemory,
} from "../../lib/runtimeManagement";
import RuntimePodDetails from "./RuntimePodDetails";
import RuntimeActions from "./RuntimeActions";
import RuntimeMaintenanceDialog from "./RuntimeMaintenanceDialog";
import RuntimeBalanceDialog from "./RuntimeBalanceDialog";
import {
  buttonClass,
  primaryClass,
  inputClass,
  ErrorMessage,
  Legend,
  Metric,
  SlotGrid,
  RuntimeDialog,
} from "./RuntimeManagementUI";
import "../../styles/runtime-pods.css";

type Dialog = {
  kind: "schedule" | "evict" | "events" | "history" | "maintenance" | "balance";
  pod?: RuntimePod;
  ids?: number[];
};
const types = ["all", "openclaw", "hermes", "opencode", "deepseek-harness"];
const emptySummary: RuntimePodSummary = {
  pods: 0, online: 0, schedulable: 0, capacity: 0, ready: 0, occupied: 0,
  available: 0, running: 0, starting: 0, migrating: 0, error: 0, idle: 0, unavailable: 0,
};

export default function RuntimePodsPage() {
  const { t } = useI18n();
  const client = useQueryClient();
  const [filter, setFilter] = useState("all");
  const [node, setNode] = useState("all");
  const [scheduling, setScheduling] = useState("all");
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [page, setPage] = useState(1);
  const [selectedPod, setSelectedPod] = useState<RuntimePod | null>(null);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [actionError, setActionError] = useState<string>();
  const [busyPod, setBusyPod] = useState<number>();
  const [menuPosition, setMenuPosition] = useState({ left: 0, top: 0 });
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const closeMenus = (event: Event) => {
      if (event.target instanceof Element && event.target.closest(".runtime-card-popover")) return;
      document.querySelectorAll<HTMLElement>(".runtime-card-popover:popover-open").forEach(menu => menu.hidePopover());
    };
    window.addEventListener("resize", closeMenus);
    window.addEventListener("scroll", closeMenus, true);
    return () => {
      window.removeEventListener("resize", closeMenus);
      window.removeEventListener("scroll", closeMenus, true);
    };
  }, []);
  useEffect(() => {
    if (search.trim() === debouncedSearch) return;
    const timer = setTimeout(() => {
      setDebouncedSearch(search.trim());
      setPage(1);
    }, 250);
    return () => clearTimeout(timer);
  }, [search, debouncedSearch]);
  const params = {
    page, page_size: 16, runtime_type: filter, node, scheduling, search: debouncedSearch,
  };
  const query = useQuery({
    queryKey: ["runtime-pods", "page", params],
    queryFn: ({ signal }) => runtimePoolService.listPodsPage(params, signal),
    enabled: !selectedPod,
    placeholderData: keepPreviousData,
    refetchInterval: 3000,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
  });
  const needsFullInventory = !!selectedPod || (!!dialog && dialog.kind !== "maintenance" && dialog.kind !== "balance");
  const fullQuery = useQuery({
    queryKey: ["runtime-pods", "all"],
    queryFn: ({ signal }) => runtimePoolService.listPods(undefined, signal),
    enabled: needsFullInventory,
    refetchInterval: 3000,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
  });
  const refresh = useCallback(() => {
    if (refreshTimer.current !== null) return;
    refreshTimer.current = setTimeout(() => {
      refreshTimer.current = null;
      void client.invalidateQueries({ queryKey: ["runtime-pods"] }, { cancelRefetch: false });
      void client.invalidateQueries({ queryKey: ["runtime-scheduling"] }, { cancelRefetch: false });
    }, 300);
  }, [client]);
  useEffect(() => () => {
    if (refreshTimer.current !== null) clearTimeout(refreshTimer.current);
  }, []);
  const { isConnected } = useRuntimeAdminWebSocket(refresh);
  const pods = query.data?.pods ?? [];
  const summary = query.data?.summary ?? emptySummary;
  const pages = Math.max(1, query.data?.pagination.total_pages ?? 1);
  if (query.data && !query.isPlaceholderData && page > pages) setPage(pages);
  const actionPods = fullQuery.data ?? pods;
  const inventoryConfirmed = fullQuery.isSuccess && !fullQuery.isFetching;
  const activePod =
    selectedPod &&
    (actionPods.find(
      (pod) =>
        pod.namespace === selectedPod.namespace &&
        pod.pod_name === selectedPod.pod_name,
    ) ?? (inventoryConfirmed ? null : selectedPod));
  if (selectedPod && inventoryConfirmed && !activePod) setSelectedPod(null);
  const dialogPod = dialog?.pod && (
    actionPods.find((pod) => pod.id === dialog.pod!.id) ??
    (inventoryConfirmed ? undefined : dialog.pod)
  );
  if (dialog?.pod && inventoryConfirmed && !dialogPod) setDialog(null);
  const selectRuntimePod = (pod: RuntimePod) => setSelectedPod(pod);
  const handleRuntimePodCardKeyDown = (
    event: React.KeyboardEvent<HTMLElement>,
    pod: RuntimePod,
  ) => {
    if (
      event.target !== event.currentTarget ||
      !["Enter", " "].includes(event.key)
    )
      return;
    event.preventDefault();
    selectRuntimePod(pod);
  };
  const toggleScheduling = async (pod: RuntimePod) => {
    try {
      setActionError(undefined);
      setBusyPod(pod.id);
      await runtimePoolService.setScheduling(
        pod.id,
        pod.scheduling_enabled === false || !!pod.scheduling_paused,
      );
      refresh();
    } catch (err) {
      setActionError(runtimeError(err));
    } finally {
      setBusyPod(undefined);
    }
  };
  const openSchedule = (pod?: RuntimePod, ids?: number[]) =>
    setDialog({ kind: "schedule", pod, ids });
  const stateCounts = (pod: RuntimePod) => {
    const summary = summarizePod(pod);
    return (
      [
        "running",
        "starting",
        "migrating",
        "error",
        "idle",
        "unavailable",
      ] as const
    )
      .filter((key) => summary[key] > 0)
      .map((key) => `${summary[key]} ${t(`runtimePods.${key}`)}`)
      .join(" · ");
  };
  return (
    <AdminLayout title={t("runtimePods.title")} fullHeight={!activePod}>
      <div className={activePod ? "space-y-4" : "runtime-overview"}>
        {actionError && <ErrorMessage>{actionError}</ErrorMessage>}
        {query.isError && (
          <ErrorMessage>
            {t("runtimePods.loadFailed")}: {runtimeError(query.error)}{" "}
            <button
              type="button"
              className="ml-2 underline"
              onClick={() => void query.refetch()}
            >
              {t("runtimePods.retry")}
            </button>
          </ErrorMessage>
        )}
        {activePod && fullQuery.isError && <ErrorMessage>{runtimeError(fullQuery.error)}</ErrorMessage>}
        {activePod ? (
          <RuntimePodDetails
            key={activePod.pod_name}
            pod={activePod}
            onBack={() => setSelectedPod(null)}
            onSchedule={(ids) => openSchedule(activePod, ids)}
            onPause={() => void toggleScheduling(activePod)}
            onEvents={() => setDialog({ kind: "events", pod: activePod })}
            busy={busyPod === activePod.id}
          />
        ) : (
          <>
            <div className="runtime-toolbar">
              <div className="runtime-types">
                {types.map((type) => (
                  <button key={type} type="button" aria-pressed={filter === type}
                    onClick={() => { setFilter(type); setPage(1); }}
                    className={`rounded px-2 py-1.5 text-xs font-medium ${filter === type ? "bg-slate-900 text-white" : "text-slate-500 hover:bg-slate-100"}`}>
                    {type === "all" ? t("common.all") : formatInstanceType(type)}
                  </button>
                ))}
              </div>
              <div className="runtime-toolbar-actions flex-wrap">
                <button type="button" className={`${buttonClass} !px-2 !py-1.5`} onClick={() => setDialog({ kind: "maintenance" })}>{t("runtimePods.maintenance")}</button>
                <button type="button" className={`${buttonClass} !px-2 !py-1.5`} onClick={() => setDialog({ kind: "balance" })}>{t("runtimePods.balance")}</button>
                <button
                  type="button"
                  className={`${buttonClass} !px-2 !py-1.5`}
                  onClick={() => setDialog({ kind: "history" })}
                >
                  {t("runtimePods.history")}
                </button>
                <button
                  type="button"
                  className={`${primaryClass} !px-2 !py-1.5`}
                  disabled={!summary.occupied}
                  onClick={() => openSchedule()}
                >
                  {t("runtimePods.batch")}
                </button>
              </div>
            </div>
            <div className="runtime-metrics">
              <Metric
                compact
                label={t("runtimePods.online")}
                value={`${summary.online} / ${summary.pods}`}
                detail={`${summary.schedulable} ${t("runtimePods.enabled")} · ${summary.pods - summary.schedulable} ${t("runtimePods.disabled")}`}
              />
              <Metric
                compact
                label={t("runtimePods.agentReady")}
                value={`${summary.ready} / ${summary.capacity}`}
                detail={`${t("runtimePods.ready")} ${summary.ready} · ${t("runtimePods.unavailable")} ${summary.unavailable}`}
                warning={summary.unavailable > 0}
              />
              <Metric
                compact
                label={t("runtimePods.occupied")}
                value={`${summary.occupied} / ${summary.capacity}`}
                detail={`${summary.running} ${t("runtimePods.running")} · ${summary.starting} ${t("runtimePods.starting")} · ${summary.migrating} ${t("runtimePods.migrating")} · ${summary.error} ${t("runtimePods.error")}`}
              />
              <Metric
                compact
                label={t("runtimePods.incoming")}
                value={summary.available}
                detail={`${t("runtimePods.idle")} ${summary.idle} · ${t("runtimePods.unavailable")} ${summary.unavailable}`}
              />
            </div>
            <div className="runtime-filters">
              <div className="relative min-w-0 flex-1">
                <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-slate-400" />
                <input
                  className={`${inputClass} w-full !py-1.5 !pl-8 !text-xs`}
                  aria-label={t("runtimePods.search")}
                  placeholder={t("runtimePods.search")}
                  value={search}
                  onChange={(event) => {
                    setSearch(event.target.value);
                  }}
                />
              </div>
              <select
                className={`${inputClass} !py-1.5 !text-xs`}
                aria-label={t("runtimePods.allNodes")}
                value={node}
                onChange={(event) => {
                  setNode(event.target.value);
                  setPage(1);
                }}
              >
                <option value="all">{t("runtimePods.allNodes")}</option>
                {(query.data?.nodes ?? []).map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
              </select>
              <select
                className={`${inputClass} !py-1.5 !text-xs`}
                aria-label={t("runtimePods.allScheduling")}
                value={scheduling}
                onChange={(event) => {
                  setScheduling(event.target.value);
                  setPage(1);
                }}
              >
                <option value="all">{t("runtimePods.allScheduling")}</option>
                <option value="enabled">{t("runtimePods.enabled")}</option>
                <option value="disabled">{t("runtimePods.disabled")}</option>
              </select>
            </div>
            <div className="runtime-inventory-status">
              <span>
                {t("runtimePods.podCount", {
                  count: query.data?.pagination.total ?? 0,
                  shown: pods.length,
                })}
              </span>
              <div className="flex flex-wrap items-center gap-3">
                <Legend compact />
                <button
                  type="button"
                  className="inline-flex items-center gap-1.5"
                  onClick={() => void query.refetch({ cancelRefetch: false })}
                  title={t("runtimePods.refresh")}
                >
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${isConnected ? "bg-emerald-500" : "bg-slate-300"}`}
                  />
                  <RefreshCw
                    className={`h-3 w-3 ${query.isFetching ? "animate-spin" : ""}`}
                  />
                  {query.data?.updated_at
                    ? t("runtimePods.updated", {
                        time: new Date(
                          query.data.updated_at,
                        ).toLocaleTimeString(),
                      })
                    : t("runtimePods.refresh")}
                </button>
              </div>
            </div>
            {query.isLoading ? (
              <div className="cm-surface p-10 text-center text-sm text-slate-500">
                {t("runtimePods.loading")}
              </div>
            ) : pods.length === 0 ? (
              <div className="cm-surface p-10 text-center text-sm text-slate-500">
                {t(
                  summary.pods ? "runtimePods.noMatching" : "runtimePods.empty",
                )}
              </div>
            ) : (
            <section className="runtime-card-grid" aria-busy={query.isFetching}>
                {pods.map((pod) => {
                  const summary = summarizePod(pod);
                  const paused =
                    pod.scheduling_enabled === false || !!pod.scheduling_paused;
                  const warning =
                    summary.unavailable > 0 ||
                    summary.error > 0 ||
                    !isSchedulable(pod);
                  const canOperate = pod.id > 0 && pod.agent_reported !== false;
                  return (
                    <article
                      key={`${pod.namespace}/${pod.pod_name}`}
                      role="button"
                      tabIndex={0}
                      aria-label={pod.pod_name}
                      aria-pressed={selectedPod?.id === pod.id}
                      onClick={() => selectRuntimePod(pod)}
                      onKeyDown={(event) =>
                        handleRuntimePodCardKeyDown(event, pod)
                      }
                      className={`cm-surface runtime-card cursor-pointer transition hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-500 ${warning ? "!border-amber-200" : ""}`}
                    >
                      <div className="runtime-card-heading">
                        <span className="min-w-0 truncate font-semibold text-slate-900">{formatInstanceType(pod.runtime_type)}</span>
                        <span className={`ml-auto shrink-0 ${isSchedulable(pod) ? "text-emerald-600" : "text-amber-600"}`}>{t(`runtimePods.${isSchedulable(pod) ? "enabled" : "disabled"}`)}</span>
                      </div>
                      <div className="runtime-card-identity">
                        <h2 className="min-w-0 truncate font-normal" aria-label={pod.pod_name} title={pod.pod_name}>{pod.pod_name.split("-").slice(-2).join("-")}</h2>
                        <span className="min-w-0 truncate text-slate-500" title={`${t("runtimePods.node")}: ${pod.node_name || "—"} / ${pod.pod_ip || "—"}`}>{pod.node_name || t("runtimePods.unknown")}</span>
                      </div>
                      <p className="runtime-card-resources tabular-nums text-slate-600" title={t("runtimePods.capacityHelp")}>
                        <span>CPU {formatCPU(pod.cpu_millis_used)}/{formatCPU(pod.cpu_millis_limit)}</span>
                        <span>{t("runtimePods.memory")} {formatMemory(pod.memory_bytes_used)}/{formatMemory(pod.memory_bytes_limit)}</span>
                      </p>
                      <SlotGrid compact pod={pod} onSelect={() => selectRuntimePod(pod)} />
                      <div className="runtime-card-actions" onClick={event => event.stopPropagation()}>
                        <span className="mr-auto min-w-0 truncate tabular-nums text-slate-600" title={`Agent ${summary.ready}/${pod.capacity} · ${stateCounts(pod)}`}>
                          {t("runtimePods.cardUsed")} {summary.occupied}/{pod.capacity} · {t("runtimePods.cardFree")} {summary.available}
                        </span>
                        <button type="button" className="shrink-0 text-slate-600 hover:text-slate-950" aria-label={t("runtimePods.viewAgents", { count: pod.capacity })} title={t("runtimePods.viewAgents", { count: pod.capacity })} onClick={() => selectRuntimePod(pod)}>{t("runtimePods.cardView")}</button>
                        <button type="button" className="runtime-card-menu-trigger" popoverTarget={`runtime-actions-${pod.namespace}-${pod.pod_name}`} aria-label={t("runtimePods.cardActions")} onClick={event => {
                          const rect = event.currentTarget.getBoundingClientRect();
                          setMenuPosition({ left: Math.max(8, Math.min(rect.right - 184, window.innerWidth - 192)), top: Math.max(8, Math.min(rect.top > 152 ? rect.top - 144 : rect.bottom + 4, window.innerHeight - 146)) });
                        }}>{t("runtimePods.cardActions")} ▾</button>
                        <div id={`runtime-actions-${pod.namespace}-${pod.pod_name}`} popover="auto" className="runtime-card-popover" style={menuPosition}>
                          <button type="button" disabled={!canOperate} onClick={event => { event.currentTarget.closest<HTMLElement>("[popover]")?.hidePopover(); setDialog({ kind: "events", pod }); }}>{t("runtimePods.events")}</button>
                          <button type="button" className="text-red-600" disabled={!canOperate || !summary.occupied} onClick={event => { event.currentTarget.closest<HTMLElement>("[popover]")?.hidePopover(); setDialog({ kind: "evict", pod }); }}>{t("runtimePods.evict")}</button>
                          <button type="button" disabled={!canManageScheduling(pod) || busyPod === pod.id} onClick={event => { event.currentTarget.closest<HTMLElement>("[popover]")?.hidePopover(); void toggleScheduling(pod); }}>{t(`runtimePods.${paused ? "resume" : "pause"}`)}</button>
                          <button type="button" disabled={!canOperate || !summary.occupied} onClick={event => { event.currentTarget.closest<HTMLElement>("[popover]")?.hidePopover(); openSchedule(pod); }}>{t("runtimePods.manual")}</button>
                        </div>
                      </div>
                    </article>
                  );
                })}
              </section>
            )}
            <div className="runtime-pagination">
              <p className="max-w-3xl text-[10px] leading-4 text-slate-500">
                {t("runtimePods.pauseHelp")}
              </p>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  className={`${buttonClass} !px-2 !py-1`}
                  aria-label={t("runtimePods.previous")}
                  disabled={page <= 1 || query.isPlaceholderData}
                  onClick={() => setPage(page - 1)}
                >
                  <ChevronLeft className="h-3 w-3" />
                </button>
                <span className="text-xs tabular-nums text-slate-600">
                  {page} / {pages}
                </span>
                <button
                  type="button"
                  className={`${buttonClass} !px-2 !py-1`}
                  aria-label={t("runtimePods.next")}
                  disabled={page >= pages || query.isPlaceholderData}
                  onClick={() => setPage(page + 1)}
                >
                  <ChevronRight className="h-3 w-3" />
                </button>
              </div>
            </div>
          </>
        )}
        {dialog?.kind === "maintenance" ? (
          <RuntimeMaintenanceDialog onClose={() => setDialog(null)} onChanged={refresh} onViewScheduling={() => setDialog({ kind: "history" })} />
        ) : dialog?.kind === "balance" ? (
          <RuntimeBalanceDialog onClose={() => setDialog(null)} onSubmitted={() => { refresh(); setDialog({ kind: "history" }); }} />
        ) : dialog && needsFullInventory && !fullQuery.data ? (
          <RuntimeDialog title={t(`runtimePods.${dialog.kind === "history" ? "history" : dialog.kind === "evict" ? "evictTitle" : "manual"}`)} onClose={() => setDialog(null)}>
            {fullQuery.isError ? <ErrorMessage>{runtimeError(fullQuery.error)} <button type="button" onClick={() => void fullQuery.refetch({ cancelRefetch: false })}>{t("runtimePods.retry")}</button></ErrorMessage> : <p role="status">{t("runtimePods.loading")}</p>}
          </RuntimeDialog>
        ) : dialog && (
          <RuntimeActions
            key={`${dialog.kind}-${dialog.pod?.id ?? "all"}`}
            kind={dialog.kind}
            pod={dialogPod}
            initialIds={dialog.ids}
            pods={actionPods}
            onClose={() => setDialog(null)}
            onRetry={(ids) => setDialog({ kind: "schedule", ids })}
            onSubmitted={() => {
              refresh();
              setDialog({ kind: "history" });
            }}
          />
        )}
      </div>
    </AdminLayout>
  );
}
