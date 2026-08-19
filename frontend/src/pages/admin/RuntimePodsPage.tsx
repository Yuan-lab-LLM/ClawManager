import React, { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, Pause, RefreshCw, Server } from "lucide-react";
import AdminLayout from "../../components/AdminLayout";
import { useI18n } from "../../contexts/I18nContext";
import { useRuntimeAdminWebSocket } from "../../hooks/useWebSocket";
import { runtimePoolService } from "../../services/runtimePoolService";
import { formatInstanceType } from "../../types/instance";
import type { RuntimePod, RuntimeType } from "../../types/runtimePool";

type RuntimeFilter = "all" | RuntimeType;

const FILTERS: Array<{ value: RuntimeFilter; labelKey?: string; label?: string }> = [
  { value: "all", labelKey: "common.all" },
  { value: "openclaw", label: "OpenClaw" },
  { value: "hermes", label: "Hermes" },
  { value: "opencode", label: "OpenCode" },
  { value: "deepseek-harness", label: "DeepSeek Harness" },
];

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function stateClass(pod: RuntimePod) {
  if (pod.draining || pod.state === "draining") {
    return "border-amber-200 bg-amber-50 text-amber-700";
  }
  if (pod.state === "ready") {
    return "border-emerald-200 bg-emerald-50 text-emerald-700";
  }
  if (pod.state === "unhealthy") {
    return "border-red-200 bg-red-50 text-red-700";
  }
  return "border-slate-200 bg-slate-50 text-slate-600";
}

function slotPercent(pod: RuntimePod) {
  if (!pod.capacity) {
    return 0;
  }
  return Math.min(100, Math.max(0, Math.round((pod.used_slots / pod.capacity) * 100)));
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs font-medium uppercase tracking-normal text-slate-500">{label}</div>
      <div className="mt-1 truncate text-sm font-medium text-slate-950">{value}</div>
    </div>
  );
}

function getErrorMessage(err: unknown, fallback: string) {
  const responseError = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
  if (responseError) {
    return responseError;
  }
  return err instanceof Error ? err.message : fallback;
}

const RuntimePodsPage: React.FC = () => {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState<RuntimeFilter>("all");
  const [selectedPod, setSelectedPod] = useState<RuntimePod | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [drainingPodId, setDrainingPodId] = useState<number | null>(null);

  const podsQuery = useQuery({
    queryKey: ["runtime-pods", filter],
    queryFn: () => runtimePoolService.listPods(filter === "all" ? undefined : filter),
    refetchInterval: 3000,
  });
  const canQuerySelectedGateways = Boolean(
    selectedPod && selectedPod.agent_reported !== false && selectedPod.id > 0,
  );

  const gatewaysQuery = useQuery({
    queryKey: ["runtime-pod-gateways", selectedPod?.id],
    queryFn: () => runtimePoolService.listGateways(selectedPod?.id ?? 0),
    enabled: canQuerySelectedGateways,
    refetchInterval: canQuerySelectedGateways ? 3000 : false,
  });

  const invalidateRuntimePods = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["runtime-pods"] });
    if (selectedPod && selectedPod.agent_reported !== false && selectedPod.id > 0) {
      void queryClient.invalidateQueries({ queryKey: ["runtime-pod-gateways", selectedPod.id] });
    }
  }, [queryClient, selectedPod]);

  const { isConnected } = useRuntimeAdminWebSocket(invalidateRuntimePods);

  const drainPod = async (pod: RuntimePod) => {
    if (pod.agent_reported === false || pod.id <= 0) {
      return;
    }
    try {
      setActionError(null);
      setDrainingPodId(pod.id);
      await runtimePoolService.drainPod(pod.id);
      await queryClient.invalidateQueries({ queryKey: ["runtime-pods"] });
    } catch (err: unknown) {
      setActionError(getErrorMessage(err, t("runtimePods.drainFailed")));
    } finally {
      setDrainingPodId(null);
    }
  };

  const selectRuntimePod = (pod: RuntimePod) => {
    setSelectedPod(pod);
  };

  const handleRuntimePodCardKeyDown = (
    event: React.KeyboardEvent<HTMLElement>,
    pod: RuntimePod,
  ) => {
    if (event.key !== "Enter" && event.key !== " ") {
      return;
    }
    event.preventDefault();
    selectRuntimePod(pod);
  };

  const pods = podsQuery.data ?? [];

  return (
    <AdminLayout title={t('runtimePods.title')}>
      <div className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="inline-flex rounded-md border border-slate-200 bg-white p-1">
            {FILTERS.map((item) => (
              <button
                key={item.value}
                type="button"
                onClick={() => setFilter(item.value)}
                className={`rounded px-3 py-1.5 text-sm font-medium ${
                  filter === item.value
                    ? "bg-slate-900 text-white"
                    : "text-slate-600 hover:bg-slate-100 hover:text-slate-950"
                }`}
              >
                {item.labelKey ? t(item.labelKey) : item.label}
              </button>
            ))}
          </div>

          <div className="flex items-center gap-2 text-sm text-slate-500">
            <span
              className={`h-2 w-2 rounded-full ${isConnected ? "bg-emerald-500" : "bg-slate-300"}`}
            />
            <button
              type="button"
              className="cm-icon-button"
              title={t("common.refresh")}
              onClick={() => void podsQuery.refetch()}
            >
              <RefreshCw className={`h-4 w-4 ${podsQuery.isFetching ? "animate-spin" : ""}`} />
            </button>
          </div>
        </div>

        {actionError && (
          <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
            {actionError}
          </div>
        )}

        {podsQuery.isLoading ? (
          <section className="cm-surface px-4 py-10 text-center text-sm text-slate-500">
            {t("runtimePods.loading")}
          </section>
        ) : pods.length === 0 ? (
          <section className="cm-surface px-4 py-10 text-center text-sm text-slate-500">
            {t("runtimePods.empty")}
          </section>
        ) : (
          <section className="grid gap-3 lg:grid-cols-2">
            {pods.map((pod) => {
              const percent = slotPercent(pod);
              const isSelected = selectedPod?.id === pod.id;
              return (
                <article
                  key={pod.id}
                  role="button"
                  tabIndex={0}
                  aria-pressed={isSelected}
                  onClick={() => selectRuntimePod(pod)}
                  onKeyDown={(event) => handleRuntimePodCardKeyDown(event, pod)}
                  className={`cm-surface cursor-pointer p-4 transition hover:border-slate-300 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-400 ${
                    isSelected ? "border-slate-900 shadow-md" : ""
                  }`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 items-start gap-3">
                      <span className="mt-1 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-slate-100 text-slate-500">
                        <Server className="h-4 w-4" />
                      </span>
                      <div className="min-w-0">
                        <h2 className="truncate text-sm font-semibold text-slate-950">
                          {pod.pod_name}
                        </h2>
                        <p className="mt-1 truncate text-xs text-slate-500">
                          {pod.node_name || "-"} / {pod.pod_ip || "-"}
                        </p>
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <span
                        className={`inline-flex rounded-md border px-2 py-1 text-xs font-medium ${stateClass(pod)}`}
                      >
                        {pod.draining ? "draining" : pod.state}
                      </span>
                      <button
                        type="button"
                        className="cm-icon-button"
                        title={t("runtimePods.gateways")}
                        onClick={(event) => {
                          event.stopPropagation();
                          selectRuntimePod(pod);
                        }}
                      >
                        <Eye className="h-4 w-4" />
                      </button>
                      <button
                        type="button"
                        className="cm-icon-button"
                        title={t("runtimePods.drain")}
                        disabled={
                          pod.agent_reported === false ||
                          pod.id <= 0 ||
                          drainingPodId === pod.id ||
                          pod.draining
                        }
                        onClick={(event) => {
                          event.stopPropagation();
                          void drainPod(pod);
                        }}
                      >
                        <Pause className="h-4 w-4" />
                      </button>
                    </div>
                  </div>

                  <div className="mt-4 flex items-center justify-between gap-4">
                    <div>
                      <div className="text-xs font-medium uppercase tracking-normal text-slate-500">
                        {t("runtimePods.typeLabel")}
                      </div>
                      <div className="mt-1 text-sm font-medium text-slate-950">
                        {formatInstanceType(pod.runtime_type)}
                      </div>
                    </div>
                    <div className="min-w-[128px] text-right">
                      <div className="text-xs font-medium uppercase tracking-normal text-slate-500">
                        {t("runtimePods.slotsLabel")}
                      </div>
                      <div className="mt-1 text-sm font-medium text-slate-950">
                        {pod.used_slots} / {pod.capacity}
                      </div>
                    </div>
                  </div>

                  <div className="mt-3 h-2 overflow-hidden rounded bg-slate-100">
                    <div className="h-full rounded bg-slate-900" style={{ width: `${percent}%` }} />
                  </div>

                  <div className="mt-4 grid grid-cols-2 gap-4">
                    <Metric label={t("common.cpu")} value={`${(pod.cpu_millis_used / 1000).toFixed(2)} cores`} />
                    <Metric label={t("common.memory")} value={formatBytes(pod.memory_bytes_used)} />
                    <Metric label={t("common.disk")} value={formatBytes(pod.disk_bytes_used)} />
                    <Metric
                      label={t("runtimePods.networkLabel")}
                      value={`${formatBytes(pod.network_rx_bytes)} / ${formatBytes(pod.network_tx_bytes)}`}
                    />
                  </div>

                  <div className="mt-4 border-t border-slate-100 pt-3 text-xs text-slate-500">
                    {t("runtimePods.lastSeen", {
                      time: pod.last_seen_at ? new Date(pod.last_seen_at).toLocaleString() : "-",
                    })}
                  </div>
                </article>
              );
            })}
          </section>
        )}

        {selectedPod && (
          <section className="cm-surface overflow-hidden">
            <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
              <div>
                <h2 className="text-base font-semibold text-slate-950">{selectedPod.pod_name}</h2>
                <p className="text-sm text-slate-500">
                  {t("runtimePods.gatewayCount", { count: gatewaysQuery.data?.length ?? 0 })}
                </p>
              </div>
              <button
                type="button"
                className="app-button-secondary"
                onClick={() => setSelectedPod(null)}
              >
                {t("common.close")}
              </button>
            </div>
            <div className="divide-y divide-slate-100">
              {gatewaysQuery.isLoading ? (
                <div className="px-4 py-8 text-center text-sm text-slate-500">
                  {t("runtimePods.loading")}
                </div>
              ) : (gatewaysQuery.data ?? []).length === 0 ? (
                <div className="px-4 py-8 text-center text-sm text-slate-500">
                  {t("runtimePods.noGateways")}
                </div>
              ) : (
                (gatewaysQuery.data ?? []).map((gateway) => (
                  <div
                    key={gateway.id}
                    className="grid gap-3 px-4 py-3 text-sm md:grid-cols-[minmax(0,1fr)_88px_96px_96px_160px]"
                    >
                    <div className="min-w-0">
                      <div className="font-medium text-slate-950">
                        {t("runtimePods.instanceGateway", { id: gateway.instance_id })}
                      </div>
                      <div className="truncate font-mono text-xs text-slate-500">
                        {gateway.gateway_id || "-"}
                      </div>
                    </div>
                    <Metric label={t("runtimePods.portLabel")} value={String(gateway.gateway_port)} />
                    <Metric label={t("runtimePods.stateLabel")} value={gateway.state} />
                    <Metric label={t("runtimePods.generationLabel")} value={String(gateway.generation)} />
                    <Metric
                      label={t("runtimePods.lastHealthLabel")}
                      value={
                        gateway.last_health_at
                          ? new Date(gateway.last_health_at).toLocaleString()
                          : "-"
                      }
                    />
                  </div>
                ))
              )}
            </div>
          </section>
        )}
      </div>
    </AdminLayout>
  );
};

export default RuntimePodsPage;
