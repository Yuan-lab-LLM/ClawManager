import { useCallback, useEffect, useRef, useState } from "react";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight } from "lucide-react";
import ExpandableSearch from "../../components/ExpandableSearch";
import FilterSelect from "../../components/FilterSelect";
import ActionMenu from "../../components/ActionMenu";
import AdminLayout from "../../components/AdminLayout";
import { useI18n } from "../../contexts/I18nContext";
import { useRuntimeAdminWebSocket } from "../../hooks/useWebSocket";
import { runtimePoolService } from "../../services/runtimePoolService";
import { formatInstanceType } from "../../types/instance";
import type { RuntimePod, RuntimePodSummary } from "../../types/runtimePool";
import {
  isSchedulable,
  canManageScheduling,
  canManageSlot,
  canEvictPod,
  recoveryInstanceIds,
  runtimeError,
  summarizePod,
} from "../../lib/runtimeManagement";
import RuntimePodDetails from "./RuntimePodDetails";
import RuntimeActions from "./RuntimeActions";
import RuntimeRecoveryDialog from "./RuntimeRecoveryDialog";
import RuntimeMaintenanceDialog from "./RuntimeMaintenanceDialog";
import RuntimeBalanceDialog from "./RuntimeBalanceDialog";
import RuntimeAutoStopDialog from "./RuntimeAutoStopDialog";
import {
  buttonClass,
  primaryClass,
  ErrorMessage,
  Legend,
  Metric,
  SlotOverview,
  RuntimeDialog,
} from "./RuntimeManagementUI";
import "../../styles/runtime-pods.css";

type Dialog = {
  kind: "schedule" | "evict" | "events" | "history" | "maintenance" | "balance" | "auto-stop" | "recovery";
  pod?: RuntimePod;
  ids?: number[];
  evictionMode?: "recovery" | "orphan-recovery";
  verifiedRecoveryIds?: number[];
  retryOperationId?: string;
};
const types = ["all", "openclaw", "hermes", "opencode", "deepseek-harness"];
const emptySummary: Partial<RuntimePodSummary> = {};

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
  const filterDetails = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const media = matchMedia("(max-width: 1100px)");
    const sync = () => {if (filterDetails.current) filterDetails.current.open = !media.matches;};
    const outside = (event: PointerEvent) => {if (media.matches && filterDetails.current && !filterDetails.current.contains(event.target as Node)) filterDetails.current.open = false;};
    sync();media.addEventListener("change",sync);document.addEventListener("pointerdown",outside);
    return () => {media.removeEventListener("change",sync);document.removeEventListener("pointerdown",outside);};
  }, [selectedPod]);
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
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
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
  });
  const needsFullInventory = !!selectedPod || (!!dialog && dialog.kind !== "maintenance" && dialog.kind !== "balance" && dialog.kind !== "auto-stop");
  const fullQuery = useQuery({
    queryKey: ["runtime-pods", "all"],
    queryFn: ({ signal }) => runtimePoolService.listPods(undefined, signal),
    enabled: needsFullInventory,
    refetchInterval: 3000,
    refetchIntervalInBackground: true,
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
  useEffect(() => {
    if (isConnected) refresh();
  }, [isConnected, refresh]);
  const pods = query.data?.pods ?? [];
  const summary = query.data?.summary ?? emptySummary;
  const metricValue = (value?: number) => value ?? t("runtimePods.unknown");
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
    (dialog.evictionMode === "recovery" && dialog.verifiedRecoveryIds?.length ? dialog.pod : inventoryConfirmed ? undefined : dialog.pod)
  );
  if (dialog?.pod && inventoryConfirmed && !dialogPod) setDialog(null);
  const selectRuntimePod = (pod: RuntimePod) => setSelectedPod(pod);
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
            onRecovery={(ids) => setDialog({ kind: "evict", pod: activePod, ids, evictionMode: ids?.length || recoveryInstanceIds(activePod).length ? "recovery" : "orphan-recovery" })}
            onPause={() => void toggleScheduling(activePod)}
            onEvents={() => setDialog({ kind: "events", pod: activePod })}
            busy={busyPod === activePod.id}
          />
        ) : (
          <>
            <div className="runtime-metrics">
              <Metric
                compact
                label={t("runtimePods.online")}
                value={`${metricValue(summary.online)} / ${metricValue(summary.pods)}`}
                detail={`${metricValue(summary.schedulable)} ${t("runtimePods.enabled")}`}
              />
              <Metric
                compact
                label={t("runtimePods.agentReady")}
                value={`${metricValue(summary.ready)} / ${metricValue(summary.capacity)}`}
                detail={`${t("runtimePods.ready")} ${metricValue(summary.ready)} · ${t("runtimePods.unavailable")} ${metricValue(summary.unavailable)} · ${t("runtimePods.unknown")} ${metricValue(summary.unknown)}`}
                warning={(summary.unavailable ?? 0) > 0 || (summary.unknown ?? 0) > 0}
              />
              <Metric
                compact
                label={t("runtimePods.cardBound")}
                value={`${metricValue(summary.bound_instances)} / ${metricValue(summary.capacity)}`}
                detail={`${t("runtimePods.occupied")} ${metricValue(summary.occupied)} · ${metricValue(summary.running)} ${t("runtimePods.running")} · ${metricValue(summary.starting)} ${t("runtimePods.starting")} · ${metricValue(summary.migrating)} ${t("runtimePods.migrating")} · ${metricValue(summary.error)} ${t("runtimePods.error")}`}
              />
              <Metric
                compact
                label={t("runtimePods.incoming")}
                value={metricValue(summary.available)}
                detail={`${t("runtimePods.idle")} ${metricValue(summary.idle)} · ${t("runtimePods.releasing")} ${metricValue(summary.releasing)} · ${t("runtimePods.unknown")} ${metricValue(summary.unknown)}`}
              />
            </div>
            <div className="runtime-toolbar">
              <div className="runtime-filter-controls">
                <ExpandableSearch label={t("runtimePods.search")} placeholder={t("runtimePods.search")} value={search} onChange={setSearch} />
                <details ref={filterDetails} className="runtime-filters" open onKeyDown={event => {
                  if (event.key === "Escape" && matchMedia("(max-width: 1100px)").matches) {
                    event.preventDefault();event.stopPropagation();event.currentTarget.open = false;event.currentTarget.querySelector("summary")?.focus();
                  }
                }}><summary className="app-button-secondary">{t("instances.filters")}</summary><div className="runtime-filter-fields">
                <FilterSelect label={t("runtimePods.runtimeType")} value={filter} options={types.map(value => ({value, label: value === "all" ? t("common.all") : formatInstanceType(value)}))} onChange={value => {setFilter(value); setPage(1);}} />
                <FilterSelect label={t("runtimePods.allNodes")} value={node} options={[{value:"all",label:t("runtimePods.allNodes")}, ...(query.data?.nodes ?? []).map(value => ({value,label:value}))]} onChange={value => {setNode(value);setPage(1);}} />
                <FilterSelect label={t("runtimePods.allScheduling")} value={scheduling} options={[{value:"all",label:t("runtimePods.allScheduling")},{value:"enabled",label:t("runtimePods.enabled")},{value:"disabled",label:t("runtimePods.disabled")}]} onChange={value => {setScheduling(value);setPage(1);}} />
                </div></details>
              </div>
              <div className="runtime-toolbar-actions flex-wrap">
                <button type="button" className={`${buttonClass} !px-2 !py-1.5`} onClick={() => setDialog({ kind: "maintenance" })}>{t("runtimePods.maintenance")}</button>
                <ActionMenu label={t("runtimePods.cardActions")} items={[
                  {id:"recovery",label:t("runtimePods.recoveryTitle"),onSelect:() => setDialog({kind:"recovery"})},
                  {id:"auto-stop",label:t("autoStop.title"),onSelect:() => setDialog({kind:"auto-stop"})},
                  {id:"balance",label:t("runtimePods.balance"),onSelect:() => setDialog({kind:"balance"})},
                  {id:"history",label:t("runtimePods.history"),onSelect:() => setDialog({kind:"history"})},
                ]} />
                <button
                  type="button"
                  className={`${primaryClass} !px-2 !py-1.5`}
                  disabled={!(summary.bound_instances ?? summary.occupied)}
                  onClick={() => openSchedule()}
                >
                  {t("runtimePods.batch")}
                </button>
              </div>
            </div>
            <div className="runtime-inventory-status">
              <Legend compact />
            </div>
            <section className="runtime-inventory-panel cm-surface">
            {query.isLoading ? (
              <div className="p-10 text-center text-sm text-slate-500">
                {t("runtimePods.loading")}
              </div>
            ) : pods.length === 0 ? (
              <div className="p-10 text-center text-sm text-slate-500">
                {t(
                  summary.pods ? "runtimePods.noMatching" : "runtimePods.empty",
                )}
              </div>
            ) : (
            <div className="runtime-table-scroll">
              <table className="runtime-table" aria-label={t("runtimePods.title")} aria-busy={query.isFetching}>
                <thead><tr>{["runtimeName", "node", "typeLabel", "status", "capacity", "cardActions"].map(key => <th key={key} scope="col">{t(`runtimePods.${key}`)}</th>)}</tr></thead>
                <tbody>{pods.map(pod => {
                  const counts = summarizePod(pod);
                  const reported = pod.agent_reported !== false && !!pod.slots?.length;
                  const complete = reported && pod.slots!.length >= pod.capacity && pod.slots!.every(slot => slot.state !== "unknown");
                  const onlineState = pod.agent_reported === false ? "unknown" : ["ready", "draining"].includes(pod.state) ? "onlineState" : ["unhealthy", "deleted", "offline"].includes(pod.state) ? "offline" : "unknown";
                  const paused = pod.scheduling_enabled === false || !!pod.scheduling_paused;
                  const canOperate = pod.id > 0 && pod.agent_reported !== false;
                  return <tr key={`${pod.namespace}/${pod.pod_name}`}>
                    <td><button type="button" className="runtime-name" title={`${pod.namespace}/${pod.pod_name}`} onClick={() => selectRuntimePod(pod)}>{pod.pod_name}</button></td>
                    <td title={`${pod.node_name || t("runtimePods.unknown")} / ${pod.pod_ip || "—"}`}>{pod.node_name || t("runtimePods.unknown")}</td>
                    <td>{formatInstanceType(pod.runtime_type)}</td>
                    <td><span className={`runtime-status runtime-status-${onlineState === "onlineState" ? "ready" : onlineState}`}>{t(`runtimePods.${onlineState}`)}</span><span className="runtime-scheduling"> · {t(`runtimePods.${isSchedulable(pod) ? "enabled" : "disabled"}`)}</span></td>
                    <td><div className="runtime-capacity-cell">
                      <SlotOverview pod={pod} />
                      <span className="runtime-slot-count" title={`${t("runtimePods.agentReady")}: ${complete ? counts.ready : t("runtimePods.unknown")} · ${t("runtimePods.incoming")}: ${complete && pod.bindings_reported !== false ? counts.available : t("runtimePods.unknown")}`}>
                        {t("runtimePods.cardBound")} {metricValue(counts.bound)} / {pod.capacity}
                      </span>
                    </div></td>
                    <td><div className="runtime-row-actions">
                      <button type="button" className="runtime-view" aria-label={t("runtimePods.viewAgents", {count:pod.capacity})} onClick={() => selectRuntimePod(pod)}>{t("runtimePods.cardView")}</button>
                      <ActionMenu label={`${t("runtimePods.cardActions")}: ${pod.pod_name}`} items={[
                        {id:"events",label:t("runtimePods.events"),disabled:!canOperate,onSelect:() => setDialog({kind:"events",pod})},
                        {id:"recovery",label:t("runtimePods.faultRecovery"),disabled:pod.id <= 0 || !pod.pod_uid,onSelect:() => setDialog({kind:"evict",pod,evictionMode:recoveryInstanceIds(pod).length ? "recovery" : "orphan-recovery"})},
                        {id:"evict",label:t("runtimePods.evict"),disabled:!canEvictPod(pod),destructive:true,onSelect:() => setDialog({kind:"evict",pod})},
                        {id:"pause",label:t(`runtimePods.${paused ? "resume" : "pause"}`),disabled:!canManageScheduling(pod) || busyPod === pod.id,onSelect:() => void toggleScheduling(pod)},
                        {id:"manual",label:t("runtimePods.manual"),disabled:!canOperate || !pod.slots?.some(canManageSlot),onSelect:() => openSchedule(pod)},
                      ]} />
                    </div></td>
                  </tr>;
                })}</tbody>
              </table>
            </div>
            )}
            <div className="runtime-pagination">
              <span>{t("runtimePods.podCount", {count:query.data?.pagination.total ?? t("runtimePods.unknown"), shown:query.data ? pods.length : t("runtimePods.unknown")})}</span>
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
            </section>
          </>
        )}
        {dialog?.kind === "recovery" ? (
 <RuntimeRecoveryDialog pods={actionPods} onEvict={(pod, ids, verifiedRecoveryIds) => setDialog({ kind: "evict", pod, ids, verifiedRecoveryIds, evictionMode: "recovery" })} onClose={() => setDialog(null)} onChanged={refresh} />
 ) : dialog?.kind === "auto-stop" ? (
          <RuntimeAutoStopDialog onClose={() => setDialog(null)} />
        ) : dialog?.kind === "maintenance" ? (
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
            verifiedRecoveryIds={dialog.verifiedRecoveryIds}
            initialEvictionMode={dialog.evictionMode}
            retryOperationId={dialog.retryOperationId}
            pods={actionPods}
            onClose={() => setDialog(null)}
            onRetry={(ids, retryOperationId) => setDialog({ kind: "schedule", ids, retryOperationId })}
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
