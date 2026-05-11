import React, { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import ConfirmDialog from "../../components/ConfirmDialog";
import { InstanceAccess } from "../../components/InstanceAccess";
import UserLayout from "../../components/UserLayout";
import { useI18n } from "../../contexts/I18nContext";
import { instanceService } from "../../services/instanceService";
import { skillService } from "../../services/skillService";
import type {
  AgentInfo,
  Instance,
  InstanceRuntimeCommand,
  InstanceRuntimeDetails,
  InstanceStatus,
  RuntimeStatus,
} from "../../types/instance";
import type { InstanceSkill, Skill } from "../../types/skill";

const META_POLL_INTERVAL_MS = 8000;
const RUNTIME_POLL_INTERVAL_MS = 5000;
const RUNTIME_BURST_POLL_INTERVAL_MS = 1000;
const RUNTIME_BURST_WINDOW_MS = 15000;
const METRIC_WINDOW_MS = 5 * 60 * 1000;
const INSTANCE_SKILL_PAGE_SIZE = 5;

type TimelineItem = {
  id: string;
  title: string;
  detail: string;
  timestamp: number;
  stampLabel: string;
  tone: string;
  section: string;
};

type MetricSample = {
  ts: number;
  value: number;
};

type MetricHistory = {
  cpu: MetricSample[];
  memory: MetricSample[];
  disk: MetricSample[];
  networkDown: MetricSample[];
  networkUp: MetricSample[];
};

type MetricCurve = {
  label: string;
  value: string;
  detail: string;
  accent: string;
  points: MetricSample[];
  secondaryAccent?: string;
  secondaryPoints?: MetricSample[];
  legend?: Array<{ label: string; accent: string }>;
  preNormalized?: boolean;
};

type TranslateFn = (
  key: string,
  variables?: Record<string, string | number>,
) => string;

function statusStyle(status: string) {
  switch (status) {
    case "running":
    case "online":
    case "ready":
      return {
        shell: "border-[#bde8ca] bg-[#edfdf2] text-[#177245]",
        dot: "bg-[#22c55e]",
      };
    case "stopped":
    case "offline":
      return {
        shell: "border-[#d9e0e7] bg-[#f6f8fb] text-[#556070]",
        dot: "bg-[#94a3b8]",
      };
    case "creating":
    case "starting":
    case "configuring":
    case "pending":
      return {
        shell: "border-[#f6df9f] bg-[#fff8dd] text-[#9a6a00]",
        dot: "bg-[#eab308]",
      };
    case "error":
    case "failed":
    case "crashed":
      return {
        shell: "border-[#f2c2c2] bg-[#fff0f0] text-[#b42318]",
        dot: "bg-[#ef4444]",
      };
    default:
      return {
        shell: "border-[#d9e0e7] bg-[#f6f8fb] text-[#556070]",
        dot: "bg-[#94a3b8]",
      };
  }
}

function skillRiskLabel(t: TranslateFn, riskLevel?: string | null) {
  switch ((riskLevel || "").toLowerCase()) {
    case "none":
      return t("instances.skillRiskNone");
    case "low":
      return t("instances.skillRiskLow");
    case "medium":
      return t("instances.skillRiskMedium");
    case "high":
      return t("instances.skillRiskHigh");
    default:
      return t("instances.skillRiskUnknown");
  }
}

function skillSourceLabel(t: TranslateFn, sourceType?: string | null) {
  switch ((sourceType || "").toLowerCase()) {
    case "uploaded":
      return t("instances.skillSourceUploaded");
    case "discovered":
      return t("instances.skillSourceDiscovered");
    default:
      return sourceType || t("instances.notAvailable");
  }
}

const InstanceDetailPage: React.FC = () => {
  const { t, locale } = useI18n();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const instanceId = id ? Number(id) : null;

  const [instance, setInstance] = useState<Instance | null>(null);
  const [status, setStatus] = useState<InstanceStatus | null>(null);
  const [runtimeDetails, setRuntimeDetails] =
    useState<InstanceRuntimeDetails | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [metaRefreshing, setMetaRefreshing] = useState(false);
  const [runtimeRefreshing, setRuntimeRefreshing] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [runtimeBurstUntil, setRuntimeBurstUntil] = useState<number>(0);
  const [metricSessionStartedAt, setMetricSessionStartedAt] = useState<number>(
    () => Date.now(),
  );
  const [metricHistory, setMetricHistory] = useState<MetricHistory>({
    cpu: [],
    memory: [],
    disk: [],
    networkDown: [],
    networkUp: [],
  });
  const [instanceSkills, setInstanceSkills] = useState<InstanceSkill[]>([]);
  const [availableSkills, setAvailableSkills] = useState<Skill[]>([]);
  const [selectedSkillId, setSelectedSkillId] = useState<number | "">("");
  const [instanceSkillPage, setInstanceSkillPage] = useState(1);
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const timelineScrollRef = useRef<HTMLDivElement | null>(null);
  const timelineItemRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const lastNetworkCounterRef = useRef<{
    ts: number;
    down: number | null;
    up: number | null;
  } | null>(null);

  const fetchMeta = useCallback(
    async (targetInstanceId: number, options?: { background?: boolean }) => {
      const background = options?.background ?? false;
      if (background) {
        setMetaRefreshing(true);
      }

      try {
        const [instanceData, statusData] = await Promise.all([
          instanceService.getInstance(targetInstanceId),
          instanceService.getInstanceStatus(targetInstanceId),
        ]);
        setInstance(instanceData);
        setStatus(statusData);
        setError(null);
      } catch (err: any) {
        if (!background) {
          setError(err.response?.data?.error || t("instances.failedToLoad"));
        } else {
          console.error("Failed to refresh instance metadata", err);
        }
      } finally {
        if (background) {
          setMetaRefreshing(false);
        }
      }
    },
    [t],
  );

  const fetchRuntime = useCallback(
    async (targetInstanceId: number, options?: { background?: boolean }) => {
      const background = options?.background ?? false;
      if (background) {
        setRuntimeRefreshing(true);
      }

      try {
        const runtimeData =
          await instanceService.getRuntimeDetails(targetInstanceId);
        setRuntimeDetails(runtimeData);
        setError(null);
      } catch (err: any) {
        if (!background) {
          setError(err.response?.data?.error || t("instances.failedToLoad"));
        } else {
          console.error("Failed to refresh runtime details", err);
        }
      } finally {
        if (background) {
          setRuntimeRefreshing(false);
        }
      }
    },
    [t],
  );

  const runtimePollInterval =
    runtimeBurstUntil > Date.now()
      ? RUNTIME_BURST_POLL_INTERVAL_MS
      : RUNTIME_POLL_INTERVAL_MS;

  useEffect(() => {
    setMetricSessionStartedAt(Date.now());
    setMetricHistory({
      cpu: [],
      memory: [],
      disk: [],
      networkDown: [],
      networkUp: [],
    });
    lastNetworkCounterRef.current = null;
  }, [instanceId]);

  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId)) {
      setError(t("instances.instanceNotFound"));
      setLoading(false);
      return;
    }

    let disposed = false;

    const hydrate = async () => {
      setLoading(true);
      try {
        const [instanceData, statusData, runtimeData] = await Promise.all([
          instanceService.getInstance(instanceId),
          instanceService.getInstanceStatus(instanceId),
          instanceService.getRuntimeDetails(instanceId),
        ]);
        if (disposed) {
          return;
        }
        setInstance(instanceData);
        setStatus(statusData);
        setRuntimeDetails(runtimeData);
        setError(null);
      } catch (err: any) {
        if (disposed) {
          return;
        }
        setError(err.response?.data?.error || t("instances.failedToLoad"));
      } finally {
        if (!disposed) {
          setLoading(false);
        }
      }
    };

    void hydrate();

    return () => {
      disposed = true;
    };
  }, [instanceId, t]);

  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId)) {
      return;
    }
    const loadSkills = async () => {
      try {
        const [instanceSkillItems, reusableSkills] = await Promise.all([
          skillService.listInstanceSkills(instanceId),
          skillService.listSkills(),
        ]);
        setInstanceSkills(instanceSkillItems);
        setAvailableSkills(
          reusableSkills.filter(
            (item) =>
              item.status === "active" &&
              item.risk_level !== "medium" &&
              item.risk_level !== "high",
          ),
        );
      } catch (skillError) {
        console.error("Failed to load skill data", skillError);
      }
    };
    void loadSkills();
  }, [instanceId]);

  useEffect(() => {
    setInstanceSkillPage(1);
  }, [instanceSkills.length]);

  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId)) {
      return;
    }

    const metaTimer = window.setInterval(() => {
      if (document.hidden) {
        return;
      }
      void fetchMeta(instanceId, { background: true });
    }, META_POLL_INTERVAL_MS);

    return () => {
      window.clearInterval(metaTimer);
    };
  }, [fetchMeta, instanceId]);

  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId)) {
      return;
    }

    const runtimeTimer = window.setInterval(() => {
      if (document.hidden) {
        return;
      }
      void fetchRuntime(instanceId, { background: true });
    }, runtimePollInterval);

    return () => {
      window.clearInterval(runtimeTimer);
    };
  }, [fetchRuntime, instanceId, runtimePollInterval]);

  useEffect(() => {
    if (runtimeBurstUntil <= Date.now()) {
      return;
    }

    const timeout = window.setTimeout(() => {
      setRuntimeBurstUntil(0);
    }, runtimeBurstUntil - Date.now());

    return () => {
      window.clearTimeout(timeout);
    };
  }, [runtimeBurstUntil]);

  const instanceSkillTotalPages = Math.max(
    1,
    Math.ceil(instanceSkills.length / INSTANCE_SKILL_PAGE_SIZE),
  );
  const currentInstanceSkillPage = Math.min(
    instanceSkillPage,
    instanceSkillTotalPages,
  );
  const paginatedInstanceSkills = instanceSkills.slice(
    (currentInstanceSkillPage - 1) * INSTANCE_SKILL_PAGE_SIZE,
    currentInstanceSkillPage * INSTANCE_SKILL_PAGE_SIZE,
  );

  useEffect(() => {
    const snapshot = extractMetricSnapshot(runtimeDetails?.runtime?.system_info);
    if (!snapshot) {
      return;
    }

    const ts = Date.now();
    const previousNetwork = lastNetworkCounterRef.current;
    let networkDownSample: number | null = null;
    let networkUpSample: number | null = null;

    if (
      previousNetwork &&
      snapshot.networkDownTotal !== null &&
      snapshot.networkUpTotal !== null
    ) {
      const elapsedSeconds = Math.max((ts - previousNetwork.ts) / 1000, 1);
      if (
        previousNetwork.down !== null &&
        snapshot.networkDownTotal >= previousNetwork.down
      ) {
        networkDownSample =
          (snapshot.networkDownTotal - previousNetwork.down) / elapsedSeconds;
      }
      if (
        previousNetwork.up !== null &&
        snapshot.networkUpTotal >= previousNetwork.up
      ) {
        networkUpSample =
          (snapshot.networkUpTotal - previousNetwork.up) / elapsedSeconds;
      }
    }

    lastNetworkCounterRef.current = {
      ts,
      down: snapshot.networkDownTotal,
      up: snapshot.networkUpTotal,
    };

    setMetricHistory((current) => ({
      cpu: appendMetricSample(current.cpu, snapshot.cpuPercent, ts),
      memory: appendMetricSample(current.memory, snapshot.memoryPercent, ts),
      disk: appendMetricSample(current.disk, snapshot.diskPercent, ts),
      networkDown: appendMetricSample(current.networkDown, networkDownSample, ts),
      networkUp: appendMetricSample(current.networkUp, networkUpSample, ts),
    }));
  }, [runtimeDetails]);

  const refreshAll = useCallback(async () => {
    if (!instanceId) {
      return;
    }
    await Promise.all([
      fetchMeta(instanceId, { background: true }),
      fetchRuntime(instanceId, { background: true }),
    ]);
  }, [fetchMeta, fetchRuntime, instanceId]);

  const handleAction = async (action: string) => {
    if (!instance) return;

    try {
      setActionLoading(action);
      switch (action) {
        case "start":
          await instanceService.startInstance(instance.id);
          break;
        case "stop":
          await instanceService.stopInstance(instance.id);
          break;
        case "restart":
          await instanceService.restartInstance(instance.id);
          break;
        case "delete":
          await instanceService.deleteInstance(instance.id);
          setShowDeleteDialog(false);
          navigate("/instances");
          return;
        default:
          return;
      }
      await refreshAll();
    } catch (err: any) {
      alert(
        err.response?.data?.error ||
          t(
            `instances.failedTo${action.charAt(0).toUpperCase()}${action.slice(1)}`,
          ),
      );
    } finally {
      setActionLoading(null);
    }
  };

  const handleRuntimeCommand = async (
    command:
      | "start"
      | "stop"
      | "restart"
      | "collect-system-info"
      | "health-check",
  ) => {
    if (!instance) return;

    try {
      setActionLoading(`runtime-${command}`);
      setRuntimeBurstUntil(Date.now() + RUNTIME_BURST_WINDOW_MS);
      await instanceService.createRuntimeCommand(instance.id, command);
      await fetchRuntime(instance.id, { background: true });
      window.setTimeout(() => {
        void fetchRuntime(instance.id, { background: true });
      }, 800);
      window.setTimeout(() => {
        void fetchRuntime(instance.id, { background: true });
      }, 2000);
      window.setTimeout(() => {
        void fetchRuntime(instance.id, { background: true });
      }, 5000);
    } catch (err: any) {
      alert(
        err.response?.data?.error ||
          t("instances.runtimeCommandFailed", { command }),
      );
    } finally {
      setActionLoading(null);
    }
  };

  const handleExportOpenClaw = async () => {
    if (!instance) return;

    try {
      setActionLoading("export-openclaw");
      const blob = await instanceService.exportOpenClawWorkspace(instance.id);
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `${instance.name || "openclaw-workspace"}.openclaw.tar.gz`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.URL.revokeObjectURL(url);
    } catch (err: any) {
      alert(err.response?.data?.error || t("instances.exportOpenClaw"));
    } finally {
      setActionLoading(null);
    }
  };

  const handleImportOpenClaw = async (file?: File | null) => {
    if (!instance || !file) return;

    try {
      setActionLoading("import-openclaw");
      await instanceService.importOpenClawWorkspace(instance.id, file);
      await fetchRuntime(instance.id, { background: true });
      alert(t("instances.importOpenClaw"));
    } catch (err: any) {
      alert(err.response?.data?.error || t("instances.importOpenClaw"));
    } finally {
      if (importInputRef.current) {
        importInputRef.current.value = "";
      }
      setActionLoading(null);
    }
  };

  const refreshSkills = useCallback(async () => {
    if (!instanceId || Number.isNaN(instanceId)) {
      return;
    }
    const items = await skillService.listInstanceSkills(instanceId);
    setInstanceSkills(items);
  }, [instanceId]);

  const handleAttachSkill = async () => {
    if (!instanceId || Number.isNaN(instanceId) || selectedSkillId === "") {
      return;
    }
    try {
      setActionLoading("attach-skill");
      await skillService.attachSkillToInstance(instanceId, Number(selectedSkillId));
      setSelectedSkillId("");
      await refreshSkills();
      setRuntimeBurstUntil(Date.now() + RUNTIME_BURST_WINDOW_MS);
    } catch (err: any) {
      alert(err.response?.data?.error || t("instances.failedToAttachSkill"));
    } finally {
      setActionLoading(null);
    }
  };

  const handleRemoveSkill = async (skillId: number) => {
    if (!instanceId || Number.isNaN(instanceId)) {
      return;
    }
    try {
      setActionLoading(`remove-skill-${skillId}`);
      await skillService.removeSkillFromInstance(instanceId, skillId);
      await refreshSkills();
      setRuntimeBurstUntil(Date.now() + RUNTIME_BURST_WINDOW_MS);
    } catch (err: any) {
      alert(err.response?.data?.error || t("instances.failedToRemoveSkill"));
    } finally {
      setActionLoading(null);
    }
  };

  if (loading) {
    return (
      <UserLayout>
        <div className="flex min-h-[60vh] items-center justify-center">
          <div className="text-lg text-gray-600">
            {t("instances.loadingInstance")}
          </div>
        </div>
      </UserLayout>
    );
  }

  if (error || !instance) {
    return (
      <UserLayout>
        <div className="flex min-h-[60vh] items-center justify-center">
          <div className="text-center">
            <p className="mb-4 text-red-600">
              {error || t("instances.instanceNotFound")}
            </p>
            <button
              onClick={() => navigate("/instances")}
              className="text-indigo-600 hover:text-indigo-800"
            >
              {t("instances.backToInstances")}
            </button>
          </div>
        </div>
      </UserLayout>
    );
  }

  const runtime = runtimeDetails?.runtime;
  const agent = runtimeDetails?.agent;
  const commands = runtimeDetails?.commands ?? [];
  const effectiveInstanceStatus = status?.status || instance.status;
  const systemInfo = asRecord(runtime?.system_info);
  const runtimeSummary = asRecord(runtime?.summary);
  const cpuInfo = asRecord(systemInfo?.cpu);
  const memoryInfo = asRecord(systemInfo?.memory);
  const diskInfo = asRecord(systemInfo?.disk);
  const networkInfo = asRecord(systemInfo?.network);
  const osInfo = asRecord(systemInfo?.os);
  const openclawStats = asRecord(runtimeSummary?.openclaw_stats);
  const runtimeStats = asRecord(runtimeSummary?.stats);
  const skillCount = firstNumber(
    openclawStats?.skill_count,
    openclawStats?.skills_count,
    runtimeStats?.skill_count,
    runtimeStats?.skills_count,
    runtimeSummary?.skill_count,
    runtimeSummary?.skills_count,
  );
  const agentCount = firstNumber(
    openclawStats?.agent_count,
    openclawStats?.agents_count,
    runtimeStats?.agent_count,
    runtimeStats?.agents_count,
    runtimeSummary?.agent_count,
    runtimeSummary?.agents_count,
  );
  const channelCount = firstNumber(
    openclawStats?.channel_count,
    openclawStats?.channels_count,
    runtimeStats?.channel_count,
    runtimeStats?.channels_count,
    runtimeSummary?.channel_count,
    runtimeSummary?.channels_count,
  );
  const currentStatusStyle = statusStyle(effectiveInstanceStatus);
  const gatewayStatus = runtime?.openclaw_status || "unknown";
  const metricCurves = buildMetricCurves({
    cpuInfo,
    memoryInfo,
    diskInfo,
    networkInfo,
    metricHistory,
    sessionStartedAt: metricSessionStartedAt,
    t,
  });
  const timelineItems = buildTimelineItems(
    instance,
    status,
    runtime,
    agent,
    commands,
    locale,
    t,
  );

  const canControlGateway = effectiveInstanceStatus === "running";

  return (
    <UserLayout>
      <ConfirmDialog
        open={showDeleteDialog}
        title={t("common.delete")}
        message={t("instances.confirmDelete")}
        confirmLabel={t("common.delete")}
        cancelLabel={t("common.cancel")}
        destructive
        loading={actionLoading === "delete"}
        onCancel={() => setShowDeleteDialog(false)}
        onConfirm={() => handleAction("delete")}
      />

      <div className="space-y-6">
        <section className="app-panel overflow-hidden px-5 py-5">
          <div className="flex flex-col gap-5 xl:flex-row xl:items-start xl:justify-between">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-3">
                <span
                  className={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold ${currentStatusStyle.shell}`}
                >
                  <span
                    className={`mr-2 h-2 w-2 rounded-full ${currentStatusStyle.dot}`}
                  />
                  {t(`status.${effectiveInstanceStatus}`)}
                </span>
                <span className="rounded-full border border-[#dbe4f0] bg-[#f8fbff] px-3 py-1 text-xs font-semibold uppercase tracking-[0.14em] text-[#8f776b]">
                  {instance.type}
                </span>
                <span className="text-sm text-[#7a6d66]">
                  {t("instances.instanceIdLabel")}: {instance.id}
                </span>
              </div>
              <h1 className="mt-4 text-[2.2rem] font-semibold leading-none tracking-[-0.05em] text-[#1d1713]">
                {instance.name}
              </h1>
              {instance.description && (
                <p className="mt-3 max-w-3xl text-sm leading-6 text-[#7a6d66]">
                  {instance.description}
                </p>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
                <RefreshState
                  active={metaRefreshing || runtimeRefreshing}
                  label={t("instances.live")}
                />
              {effectiveInstanceStatus === "running" ? (
                <button
                  onClick={() => handleAction("stop")}
                  disabled={actionLoading === "stop"}
                  className="rounded-2xl border border-transparent bg-yellow-100 px-4 py-2 text-sm font-medium text-yellow-700 hover:bg-yellow-200 disabled:opacity-50"
                >
                  {actionLoading === "stop"
                    ? `${t("common.stop")}...`
                    : t("common.stop")}
                </button>
              ) : effectiveInstanceStatus === "stopped" ? (
                <button
                  onClick={() => handleAction("start")}
                  disabled={actionLoading === "start"}
                  className="rounded-2xl border border-transparent bg-green-100 px-4 py-2 text-sm font-medium text-green-700 hover:bg-green-200 disabled:opacity-50"
                >
                  {actionLoading === "start"
                    ? `${t("common.start")}...`
                    : t("common.start")}
                </button>
              ) : null}
              <button
                onClick={() => handleAction("restart")}
                disabled={actionLoading === "restart"}
                className="app-button-secondary disabled:opacity-50"
              >
                {actionLoading === "restart"
                  ? `${t("common.restart")}...`
                  : t("common.restart")}
              </button>
              <button
                onClick={() => setShowDeleteDialog(true)}
                disabled={actionLoading === "delete"}
                className="rounded-2xl border border-transparent bg-red-100 px-4 py-2 text-sm font-medium text-red-700 hover:bg-red-200 disabled:opacity-50"
              >
                {actionLoading === "delete"
                  ? `${t("common.delete")}...`
                  : t("common.delete")}
              </button>
            </div>
          </div>
        </section>

        <div className="grid gap-6 xl:grid-cols-[minmax(0,1.48fr)_470px] 2xl:grid-cols-[minmax(0,1.55fr)_520px]">
          <div className="space-y-6">
            <section className="overflow-hidden rounded-[34px] border border-[#dbe4f0] bg-[linear-gradient(180deg,#fbf5ef_0%,#f6ece4_100%)] p-3 shadow-[0_34px_90px_-62px_rgba(30,64,175,0.5)]">
              <div className="relative">
                <InstanceAccess
                  instanceId={instance.id}
                  instanceName={instance.name}
                  isRunning={effectiveInstanceStatus === "running"}
                  showControlUiEntry={instance.type === "openclaw"}
                  overlay={
                    instance.type === "openclaw"
                      ? {
                          gatewayStatus,
                          canControl: canControlGateway,
                          actionLoading,
                          onCommand: handleRuntimeCommand,
                        }
                      : undefined
                  }
                />
              </div>
            </section>

            {instance.type === "openclaw" && (
              <section className="app-panel px-5 py-5">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                  <div className="max-w-xl">
                    <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">
                      {t("instances.workspaceSection")}
                    </p>
                    <h2 className="mt-2 text-[1.35rem] font-semibold tracking-[-0.03em] text-[#1d1713]">
                      {t("instances.openClawWorkspace")}
                    </h2>
                    <p className="mt-2 text-sm leading-6 text-[#7a6d66]">
                      {t("instances.openClawWorkspaceDesc")}
                    </p>
                  </div>

                  <div className="grid w-full gap-3 lg:max-w-[320px]">
                    <button
                      type="button"
                      onClick={handleExportOpenClaw}
                      disabled={
                        effectiveInstanceStatus !== "running" ||
                        actionLoading === "export-openclaw" ||
                        actionLoading === "import-openclaw"
                      }
                      className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {actionLoading === "export-openclaw"
                        ? t("instances.exportingOpenClaw")
                        : t("instances.exportOpenClaw")}
                    </button>
                    <input
                      ref={importInputRef}
                      type="file"
                      accept=".tar.gz,.tgz,application/gzip,application/x-gzip,application/octet-stream"
                      className="hidden"
                      onChange={(e) =>
                        handleImportOpenClaw(e.target.files?.[0] || null)
                      }
                    />
                    <button
                      type="button"
                      onClick={() => importInputRef.current?.click()}
                      disabled={
                        effectiveInstanceStatus !== "running" ||
                        actionLoading === "export-openclaw" ||
                        actionLoading === "import-openclaw"
                      }
                      className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {actionLoading === "import-openclaw"
                        ? t("instances.importingOpenClaw")
                        : t("instances.importOpenClaw")}
                    </button>
                  </div>
                </div>
              </section>
            )}

            <section className="app-panel px-6 py-6">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
                <div>
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">
                    {t("instances.kubernetesSection")}
                  </p>
                  <h2 className="mt-2 text-[1.5rem] font-semibold tracking-[-0.03em] text-[#1d1713]">
                    {t("instances.kubernetesStatusTitle")}
                  </h2>
                </div>
                <RefreshState active={metaRefreshing} label={t("instances.infrastructureReady")} />
              </div>

              <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                <DetailCard
                  label={t("instances.podName")}
                  value={status?.pod_name || instance.pod_name || t("instances.notAvailable")}
                />
                <DetailCard
                  label={t("instances.namespace")}
                  value={status?.pod_namespace || instance.pod_namespace || t("instances.notAvailable")}
                />
                <DetailCard
                  label={t("instances.podStatus")}
                  value={status?.pod_status || status?.status || t("instances.notAvailable")}
                />
                <DetailCard
                  label={t("instances.podIp")}
                  value={status?.pod_ip || instance.pod_ip || t("instances.notAvailable")}
                />
                <DetailCard label={t("common.type")} value={instance.type} />
                <DetailCard
                  label={t("instances.instanceImage")}
                  value={
                    instance.image_registry
                      ? `${instance.image_registry}${instance.image_tag ? `:${instance.image_tag}` : ""}`
                      : `${instance.os_type} ${instance.os_version}`
                  }
                />
                <DetailCard
                  label={t("instances.storageClass")}
                  value={
                    instance.storage_class || t("instances.defaultStorageClass")
                  }
                />
                <DetailCard
                  label={t("instances.mountPath")}
                  value={instance.mount_path}
                  mono
                />
              </div>
            </section>

            {instance.type === "openclaw" && (
              <section className="app-panel px-6 py-6">
                <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
                  <div>
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">
                      {t("instances.skillsSection")}
                    </p>
                    <h2 className="mt-2 text-[1.5rem] font-semibold tracking-[-0.03em] text-[#1d1713]">
                      {t("instances.skillManagement")}
                    </h2>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <select
                      value={selectedSkillId}
                      onChange={(event) => setSelectedSkillId(event.target.value ? Number(event.target.value) : "")}
                      className="app-input min-w-[220px]"
                    >
                      <option value="">{t("instances.selectSkill")}</option>
                      {availableSkills.map((skill) => (
                        <option key={skill.id} value={skill.id}>
                          {skill.name} ({skill.skill_key})
                        </option>
                      ))}
                    </select>
                    <button
                      type="button"
                      onClick={handleAttachSkill}
                      disabled={selectedSkillId === "" || actionLoading === "attach-skill"}
                      className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {actionLoading === "attach-skill" ? t("instances.installingSkill") : t("instances.installSkill")}
                    </button>
                  </div>
                </div>
                <div className="mt-5 space-y-3">
                  {instanceSkills.length === 0 ? (
                    <div className="rounded-[22px] border border-dashed border-[#e7d9d1] bg-[#f8fbff] px-5 py-6 text-sm text-[#7a6d66]">
                      {t("instances.noSkillsReported")}
                    </div>
                  ) : (
                    <>
                      {paginatedInstanceSkills.map((item) => (
                        <div
                          key={`${item.skill_id}-${item.id}`}
                          className="rounded-[22px] border border-[#efe2d8] bg-[#f8fbff] px-5 py-4 shadow-[0_20px_40px_-36px_rgba(30,64,175,0.42)]"
                        >
                          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                            <div>
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="text-base font-semibold text-[#1d1713]">
                                  {item.skill?.name || t("instances.skillFallback", { id: item.skill_id })}
                                </span>
                                <span className="rounded-full border border-[#dbe4f0] bg-white px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f776b]">
                                  {skillSourceLabel(t, item.source_type)}
                                </span>
                                <span className="rounded-full border border-[#dbe4f0] bg-white px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f776b]">
                                  {skillRiskLabel(t, item.skill?.risk_level)}
                                </span>
                              </div>
                              <p className="mt-2 text-sm text-[#6f6158]">
                                {item.skill?.skill_key || item.skill_id}
                                {item.install_path ? ` · ${item.install_path}` : ""}
                                {item.last_seen_at ? ` · ${t("instances.lastSeenAt", { value: formatDateTime(item.last_seen_at, locale, t) })}` : ""}
                              </p>
                            </div>
                            <button
                              type="button"
                              onClick={() => handleRemoveSkill(item.skill_id)}
                              disabled={actionLoading === `remove-skill-${item.skill_id}`}
                              className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm font-medium text-red-700 hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              {actionLoading === `remove-skill-${item.skill_id}` ? t("instances.removingSkill") : t("instances.removeSkill")}
                            </button>
                          </div>
                        </div>
                      ))}
                      {instanceSkillTotalPages > 1 ? (
                        <div className="flex flex-col gap-3 border-t border-[#f0e4dc] pt-4 sm:flex-row sm:items-center sm:justify-between">
                          <div className="text-sm text-[#8a7b72]">
                            {t("instances.skillPagination", {
                              from: (currentInstanceSkillPage - 1) * INSTANCE_SKILL_PAGE_SIZE + 1,
                              to: Math.min(
                                currentInstanceSkillPage * INSTANCE_SKILL_PAGE_SIZE,
                                instanceSkills.length,
                              ),
                              total: instanceSkills.length,
                            })}
                          </div>
                          <div className="flex items-center gap-2">
                            <button
                              type="button"
                              onClick={() =>
                                setInstanceSkillPage((current) =>
                                  Math.max(1, current - 1),
                                )
                              }
                              disabled={currentInstanceSkillPage <= 1}
                              className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              {t("instances.previous")}
                            </button>
                            <div className="min-w-[76px] text-center text-sm font-medium text-[#5f5957]">
                              {currentInstanceSkillPage} / {instanceSkillTotalPages}
                            </div>
                            <button
                              type="button"
                              onClick={() =>
                                setInstanceSkillPage((current) =>
                                  Math.min(instanceSkillTotalPages, current + 1),
                                )
                              }
                              disabled={currentInstanceSkillPage >= instanceSkillTotalPages}
                              className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              {t("instances.nextPage")}
                            </button>
                          </div>
                        </div>
                      ) : null}
                    </>
                  )}
                </div>
              </section>
            )}

            <section className="app-panel px-6 py-6">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
                <div>
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">
                    {t("instances.timeline")}
                  </p>
                  <h2 className="mt-2 text-[1.5rem] font-semibold tracking-[-0.03em] text-[#1d1713]">
                    {t("instances.timelineSubtitle")}
                  </h2>
                </div>
                <RefreshState
                  active={runtimeRefreshing}
                  label={t("instances.timelineEvents", { count: timelineItems.length })}
                />
              </div>

              <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1fr)_86px]">
                <div
                  ref={timelineScrollRef}
                  className="max-h-[560px] overflow-y-auto pr-2"
                >
                  <div className="space-y-4">
                    {timelineItems.length === 0 ? (
                      <div className="rounded-[24px] border border-dashed border-[#e7d9d1] bg-[#f8fbff] px-5 py-8 text-sm text-[#7a6d66]">
                        {t("instances.noRuntimeActivity")}
                      </div>
                    ) : (
                      timelineItems.map((item) => (
                        <div
                          key={item.id}
                          ref={(node) => {
                            timelineItemRefs.current[item.id] = node;
                          }}
                          className="rounded-[26px] border border-[#efe2d8] bg-[#f8fbff] px-5 py-5 shadow-[0_20px_40px_-36px_rgba(30,64,175,0.42)]"
                        >
                          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                            <div className="min-w-0">
                              <div className="flex flex-wrap items-center gap-2">
                                <span
                                  className={`h-2.5 w-2.5 rounded-full ${item.tone}`}
                                />
                                <p className="text-base font-semibold text-[#1d1713]">
                                  {item.title}
                                </p>
                                <span className="rounded-full border border-[#dbe4f0] bg-white px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f776b]">
                                  {item.section}
                                </span>
                              </div>
                              <p className="mt-2 text-sm leading-6 text-[#6f6158]">
                                {item.detail}
                              </p>
                            </div>
                            <p className="shrink-0 text-xs font-medium uppercase tracking-[0.14em] text-[#9d8a80]">
                              {item.stampLabel}
                            </p>
                          </div>
                        </div>
                      ))
                    )}
                  </div>
                </div>

                <div className="rounded-[24px] border border-[#efe2d8] bg-[#f8fbff] px-3 py-4">
                  <p className="text-center text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">
                    {t("instances.minimap")}
                  </p>
                  <div className="mt-4 flex max-h-[500px] flex-col items-center gap-3 overflow-y-auto">
                    {timelineItems.map((item, index) => (
                      <button
                        key={item.id}
                        type="button"
                        title={`${item.title} · ${item.stampLabel}`}
                        onClick={() =>
                          timelineItemRefs.current[item.id]?.scrollIntoView({
                            behavior: "smooth",
                            block: "center",
                          })
                        }
                        className="group flex w-full flex-col items-center gap-1 rounded-[18px] px-2 py-2 hover:bg-white"
                      >
                        <span
                          className={`h-3 w-3 rounded-full ${item.tone} transition-transform group-hover:scale-110`}
                        />
                        <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[#9d8a80]">
                          {index + 1}
                        </span>
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            </section>
          </div>

          <aside className="space-y-5 xl:sticky xl:top-6 xl:self-start">
            <section className="app-panel-warm px-5 py-5">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#2563eb]">
                    {t("instances.runtimeSummary")}
                  </p>
                  <h2 className="mt-2 text-[1.55rem] font-semibold tracking-[-0.04em] text-[#1d1713]">
                    {t("instances.agentReportedStatus")}
                  </h2>
                </div>
                <RefreshState active={runtimeRefreshing} label={t("instances.fresh")} />
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-2">
                {metricCurves.map((metric) => (
                  <CurveMetricCard key={metric.label} metric={metric} />
                ))}
              </div>

              <div className="mt-4 grid grid-cols-2 gap-3">
                <SummaryMetricCard
                  label={t("instances.operatingSystemShort")}
                  value={formatMetricValue(
                    firstValue(
                      osInfo?.os_release,
                      osInfo?.kernel,
                      systemInfo?.hostname,
                      agent?.host_info?.hostname,
                    ),
                    t,
                  )}
                />
                <SummaryMetricCard
                  label={t("instances.openClawShort")}
                  value={formatMetricValue(runtime?.openclaw_version, t)}
                />
                <SummaryMetricCard
                  label={t("instances.skillsSection")}
                  value={formatCountValue(skillCount, t)}
                />
                <SummaryMetricCard
                  label={t("instances.agents")}
                  value={formatCountValue(agentCount, t)}
                />
                <SummaryMetricCard
                  label={t("instances.channels")}
                  value={formatCountValue(channelCount, t)}
                />
              </div>

              <div className="mt-5 rounded-[24px] border border-[#dbe4f0] bg-white/82 p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge
                    label={t("instances.agentStatusLabel", { status: agent?.status || runtime?.agent_status || "offline" })}
                    status={agent?.status || runtime?.agent_status || "offline"}
                  />
                  <StatusBadge
                    label={t("instances.gatewayStatusLabel", { status: gatewayStatus })}
                    status={gatewayStatus}
                  />
                </div>
                <dl className="mt-4 grid gap-3 text-sm text-[#4d4039]">
                  <MetaRow label={t("instances.agentId")} value={agent?.agent_id || t("instances.notAvailable")} />
                  <MetaRow
                    label={t("instances.agentVersion")}
                    value={agent?.agent_version || t("instances.notAvailable")}
                  />
                  <MetaRow
                    label={t("instances.protocol")}
                    value={agent?.protocol_version || t("instances.notAvailable")}
                  />
                  <MetaRow
                    label={t("instances.lastHeartbeat")}
                    value={formatDateTime(agent?.last_heartbeat_at, locale, t)}
                  />
                  <MetaRow
                    label={t("instances.lastReport")}
                    value={formatDateTime(runtime?.last_reported_at, locale, t)}
                  />
                  <MetaRow
                    label={t("instances.podIp")}
                    value={status?.pod_ip || instance.pod_ip || t("instances.notAvailable")}
                  />
                </dl>
              </div>
            </section>
          </aside>
        </div>
      </div>
    </UserLayout>
  );
};

function SummaryMetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[20px] border border-[#dbe4f0] bg-white/82 px-4 py-3.5">
      <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">
        {label}
      </p>
      <p className="mt-2.5 text-sm font-semibold leading-6 text-[#1d1713]">
        {value}
      </p>
    </div>
  );
}

function CurveMetricCard({ metric }: { metric: MetricCurve }) {
  return (
    <div className="overflow-hidden rounded-[22px] border border-[#dbe4f0] bg-white/84 shadow-[0_16px_34px_-28px_rgba(30,64,175,0.3)]">
      <div className="flex items-start justify-between gap-4 px-4 pb-2.5 pt-3.5">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">
            {metric.label}
          </p>
          <p className="mt-1.5 text-[15px] font-semibold text-[#1d1713]">
            {metric.value}
          </p>
        </div>
        <span
          className="mt-1 h-2.5 w-2.5 rounded-full"
          style={{ backgroundColor: metric.accent }}
        />
      </div>
      <div className="px-3">
        <Sparkline
          points={metric.points}
          accent={metric.accent}
          secondaryPoints={metric.secondaryPoints}
          secondaryAccent={metric.secondaryAccent}
          preNormalized={metric.preNormalized}
        />
      </div>
      {metric.legend && metric.legend.length > 0 ? (
        <div className="flex items-center gap-3 px-4 pt-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-[#9b877c]">
          {metric.legend.map((item) => (
            <span key={item.label} className="inline-flex items-center gap-1.5">
              <span
                className="h-2 w-2 rounded-full"
                style={{ backgroundColor: item.accent }}
              />
              {item.label}
            </span>
          ))}
        </div>
      ) : null}
      <p className="px-4 pb-3.5 pt-1.5 text-[11px] leading-5 text-[#7a6d66]">
        {metric.detail}
      </p>
    </div>
  );
}

function buildSparklinePath(
  points: MetricSample[],
  chartLeft: number,
  chartBottom: number,
  chartWidth: number,
  chartHeight: number,
  options?: {
    preNormalized?: boolean;
  },
) {
  const normalized = options?.preNormalized
    ? points.map((point) => Math.max(0.04, Math.min(point.value / 100, 1)))
    : normalizePoints(points.map((point) => point.value));
  if (!points.length) {
    return {
      normalized: [],
      path: "",
      areaPath: "",
    };
  }
  const path = normalized
    .map((point, index) => {
      const x =
        chartLeft +
        ((points[index]?.ts ?? 0) / Math.max(METRIC_WINDOW_MS, 1)) * chartWidth;
      const y = chartBottom - point * chartHeight;
      return `${index === 0 ? "M" : "L"} ${x} ${y}`;
    })
    .join(" ");

  return {
    normalized,
    path,
    areaPath: `${path} L ${chartLeft + chartWidth} ${chartBottom} L ${chartLeft} ${chartBottom} Z`,
  };
}

function Sparkline({
  points,
  accent,
  secondaryPoints,
  secondaryAccent,
  preNormalized = false,
}: {
  points: MetricSample[];
  accent: string;
  secondaryPoints?: MetricSample[];
  secondaryAccent?: string;
  preNormalized?: boolean;
}) {
  const width = 300;
  const height = 96;
  const chartLeft = 24;
  const chartRight = width - 6;
  const chartTop = 10;
  const chartBottom = height - 18;
  const chartWidth = chartRight - chartLeft;
  const chartHeight = chartBottom - chartTop;
  const primaryLine = buildSparklinePath(
    points,
    chartLeft,
    chartBottom,
    chartWidth,
    chartHeight,
    { preNormalized },
  );
  const secondaryLine =
    secondaryPoints && secondaryPoints.length > 0
      ? buildSparklinePath(
          secondaryPoints,
          chartLeft,
          chartBottom,
          chartWidth,
          chartHeight,
          { preNormalized },
        )
      : null;
  const gradientId = `spark-${accent.replace("#", "")}`;
  const secondaryGradientId = secondaryAccent
    ? `spark-${secondaryAccent.replace("#", "")}`
    : null;
  const xLabels = buildXAxisLabels();
  const yTicks = [
    { label: "100", value: 1 },
    { label: "50", value: 0.5 },
    { label: "0", value: 0 },
  ];

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      className="h-[92px] w-full"
      preserveAspectRatio="none"
    >
      <defs>
        <linearGradient id={gradientId} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor={accent} stopOpacity="0.34" />
          <stop offset="100%" stopColor={accent} stopOpacity="0.02" />
        </linearGradient>
        {secondaryAccent && secondaryGradientId ? (
          <linearGradient id={secondaryGradientId} x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stopColor={secondaryAccent} stopOpacity="0.18" />
            <stop offset="100%" stopColor={secondaryAccent} stopOpacity="0.01" />
          </linearGradient>
        ) : null}
      </defs>
      {yTicks.map((tick) => {
        const y = chartBottom - tick.value * chartHeight;
        return (
          <g key={tick.label}>
            <line
              x1={chartLeft}
              x2={chartRight}
              y1={y}
              y2={y}
              stroke="rgba(143,122,112,0.12)"
              strokeWidth="1"
            />
            <text
              x={chartLeft - 6}
              y={y + 3}
              textAnchor="end"
              fontSize="8"
              fill="#b09d93"
            >
              {tick.label}
            </text>
          </g>
        );
      })}
      <line
        x1={chartLeft}
        x2={chartRight}
        y1={chartBottom}
        y2={chartBottom}
        stroke="rgba(143,122,112,0.18)"
        strokeWidth="1.1"
      />
      <line
        x1={chartLeft}
        x2={chartLeft}
        y1={chartTop}
        y2={chartBottom}
        stroke="rgba(143,122,112,0.18)"
        strokeWidth="1.1"
      />
      <path d={primaryLine.areaPath} fill={`url(#${gradientId})`} stroke="none" />
      {secondaryLine && secondaryAccent && secondaryGradientId ? (
        <path
          d={secondaryLine.areaPath}
          fill={`url(#${secondaryGradientId})`}
          stroke="none"
        />
      ) : null}
      <path
        d={primaryLine.path}
        fill="none"
        stroke={accent}
        strokeWidth="3"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      {secondaryLine && secondaryAccent ? (
        <path
          d={secondaryLine.path}
          fill="none"
          stroke={secondaryAccent}
          strokeWidth="2.6"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeDasharray="5 4"
          opacity="1"
        />
      ) : null}
      {xLabels.map((label) => {
        const x =
          chartLeft + (label.offsetMs / Math.max(METRIC_WINDOW_MS, 1)) * chartWidth;
        return (
          <g key={label.label}>
            <line
              x1={x}
              x2={x}
              y1={chartBottom}
              y2={chartBottom + 3}
              stroke="rgba(143,122,112,0.18)"
              strokeWidth="1"
            />
            <text
              x={x}
              y={height - 5}
              textAnchor="middle"
              fontSize="8"
              fill="#b09d93"
            >
              {label.label}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

function buildXAxisLabels() {
  return [
    { label: "0", offsetMs: 0 },
    { label: "1m", offsetMs: 1 * 60 * 1000 },
    { label: "2m", offsetMs: 2 * 60 * 1000 },
    { label: "3m", offsetMs: 3 * 60 * 1000 },
    { label: "4m", offsetMs: 4 * 60 * 1000 },
    { label: "5m", offsetMs: 5 * 60 * 1000 },
  ];
}

function DetailCard({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="rounded-[22px] border border-[#efe2d8] bg-[#f8fbff] px-4 py-4">
      <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#b09d93]">
        {label}
      </p>
      <p
        className={`mt-3 text-sm font-medium leading-6 text-[#1d1713] ${mono ? "break-all font-mono text-[13px]" : ""}`}
      >
        {value}
      </p>
    </div>
  );
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-[#f1e5dd] pb-3 last:border-b-0 last:pb-0">
      <dt className="text-[#8f7a70]">{label}</dt>
      <dd className="text-right font-medium text-[#1d1713]">{value}</dd>
    </div>
  );
}

function StatusBadge({ label, status }: { label: string; status: string }) {
  const style = statusStyle(status);
  return (
    <span
      className={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold ${style.shell}`}
    >
      <span className={`mr-2 h-2 w-2 rounded-full ${style.dot}`} />
      {label}
    </span>
  );
}

function RefreshState({ active, label }: { active: boolean; label: string }) {
  return (
    <span
      className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.14em] transition-colors duration-300 ${
        active
          ? "border-[#d9e8f9] bg-[#f5f9ff] text-[#6581a4]"
          : "border-[#dbe4f0] bg-white/82 text-[#7a6d66]"
      }`}
    >
      <span
        className={`h-2.5 w-2.5 rounded-full transition-colors duration-300 ${
          active ? "bg-[#93c5fd]" : "bg-[#22c55e]"
        }`}
      />
      {label}
    </span>
  );
}

function buildTimelineItems(
  instance: Instance,
  status: InstanceStatus | null,
  runtime: RuntimeStatus | undefined,
  agent: AgentInfo | undefined,
  commands: InstanceRuntimeCommand[],
  locale: string,
  t: TranslateFn,
): TimelineItem[] {
  const items: TimelineItem[] = [];

  items.push({
    id: `instance-created-${instance.id}`,
    title: t("instances.timelineInstanceCreated"),
    detail: t("instances.timelineInstanceCreatedDetail", {
      name: instance.name,
      type: instance.type,
    }),
    timestamp: new Date(instance.created_at).getTime(),
    stampLabel: formatDateTime(instance.created_at, locale, t),
    tone: "bg-[#6366f1]",
    section: t("instances.timelineSectionInstance"),
  });

  if (instance.started_at) {
    items.push({
      id: `instance-started-${instance.id}`,
      title: t("instances.timelineInstanceStarted"),
      detail: t("instances.timelineInstanceStartedDetail"),
      timestamp: new Date(instance.started_at).getTime(),
      stampLabel: formatDateTime(instance.started_at, locale, t),
      tone: "bg-[#22c55e]",
      section: t("instances.timelineSectionInfra"),
    });
  }

  if (instance.stopped_at) {
    items.push({
      id: `instance-stopped-${instance.id}`,
      title: t("instances.timelineInstanceStopped"),
      detail: t("instances.timelineInstanceStoppedDetail"),
      timestamp: new Date(instance.stopped_at).getTime(),
      stampLabel: formatDateTime(instance.stopped_at, locale, t),
      tone: "bg-[#94a3b8]",
      section: t("instances.timelineSectionInfra"),
    });
  }

  if (agent?.registered_at) {
    items.push({
      id: `agent-registered-${instance.id}`,
      title: t("instances.timelineAgentRegistered"),
      detail: t("instances.timelineAgentRegisteredDetail", {
        agentId: agent.agent_id,
        protocol: agent.protocol_version,
      }),
      timestamp: new Date(agent.registered_at).getTime(),
      stampLabel: formatDateTime(agent.registered_at, locale, t),
      tone: "bg-[#3b82f6]",
      section: t("instances.timelineSectionAgent"),
    });
  }

  if (runtime?.last_reported_at) {
    items.push({
      id: `runtime-report-${instance.id}`,
      title: t("instances.timelineRuntimeReported"),
      detail: t("instances.timelineRuntimeReportedDetail", {
        gatewayStatus: runtime.openclaw_status,
        infraStatus: runtime.infra_status,
      }),
      timestamp: new Date(runtime.last_reported_at).getTime(),
      stampLabel: formatDateTime(runtime.last_reported_at, locale, t),
      tone: "bg-[#f59e0b]",
      section: t("instances.timelineSectionRuntime"),
    });
  }

  if (status?.pod_status && status.created_at) {
    items.push({
      id: `pod-status-${instance.id}`,
      title: t("instances.timelinePodStatusObserved"),
      detail: t("instances.timelinePodStatusObservedDetail", {
        status: status.pod_status,
      }),
      timestamp: new Date(status.created_at).getTime(),
      stampLabel: formatDateTime(status.created_at, locale, t),
      tone: "bg-[#a855f7]",
      section: t("instances.timelineSectionKubernetes"),
    });
  }

  commands.forEach((command) => {
    const commandTime =
      command.finished_at ||
      command.started_at ||
      command.dispatched_at ||
      command.issued_at;
    items.push({
      id: `command-${command.id}`,
      title: formatCommandTitle(command.command_type, locale),
      detail: command.error_message
        ? t("instances.timelineCommandWithError", {
            status: command.status,
            error: command.error_message,
          })
        : t("instances.timelineCommandWithIdempotency", {
            status: command.status,
            key: command.idempotency_key,
          }),
      timestamp: new Date(commandTime).getTime(),
      stampLabel: formatDateTime(commandTime, locale, t),
      tone: commandTone(command.status),
      section: t("instances.timelineSectionCommand"),
    });
  });

  return items
    .filter((item) => Number.isFinite(item.timestamp))
    .sort((left, right) => right.timestamp - left.timestamp);
}

function formatDateTime(
  value: string | undefined,
  locale: string,
  t: TranslateFn,
) {
  if (!value) {
    return t("instances.notAvailable");
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(locale);
}

function formatCommandTitle(commandType: string, locale: string) {
  return commandType
    .replace(/_/g, " ")
    .replace(/\b\w/g, (char) => char.toLocaleUpperCase(locale));
}

function commandTone(status: string) {
  switch (status) {
    case "succeeded":
      return "bg-[#22c55e]";
    case "failed":
    case "timed_out":
      return "bg-[#ef4444]";
    case "running":
    case "dispatched":
      return "bg-[#3b82f6]";
    default:
      return "bg-[#f59e0b]";
  }
}

function firstValue(...values: unknown[]) {
  return values.find((value) => {
    if (value === null || value === undefined) {
      return false;
    }
    if (typeof value === "string") {
      return value.trim() !== "";
    }
    return true;
  });
}

function firstNumber(...values: unknown[]): number | null {
  for (const value of values) {
    const parsed = getNumber(value);
    if (parsed !== null) {
      return parsed;
    }
  }
  return null;
}

function formatCountValue(value: unknown, t: TranslateFn): string {
  if (Array.isArray(value)) {
    return `${value.length}`;
  }
  if (typeof value === "number") {
    return `${value}`;
  }
  if (typeof value === "string" && value.trim() !== "") {
    return value;
  }
  if (value && typeof value === "object") {
    const count = (value as Record<string, unknown>).count;
    if (typeof count === "number") {
      return `${count}`;
    }
  }
  return t("instances.notAvailable");
}

function asRecord(value: unknown): Record<string, any> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return undefined;
  }
  return value as Record<string, any>;
}

function getNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string") {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }
  return null;
}

function getArray(value: unknown): any[] {
  return Array.isArray(value) ? value : [];
}

function bytesToGB(value: number | null, t: TranslateFn): string {
  if (value === null) {
    return t("instances.notAvailable");
  }
  return `${(value / 1024 / 1024 / 1024).toFixed(1)} GB`;
}

function percentLabel(value: number | null, t: TranslateFn): string {
  if (value === null) {
    return t("instances.notAvailable");
  }
  return `${Math.round(value)}%`;
}

function buildMetricCurves({
  cpuInfo,
  memoryInfo,
  diskInfo,
  networkInfo,
  metricHistory,
  sessionStartedAt,
  t,
}: {
  cpuInfo?: Record<string, any>;
  memoryInfo?: Record<string, any>;
  diskInfo?: Record<string, any>;
  networkInfo?: Record<string, any>;
  metricHistory: MetricHistory;
  sessionStartedAt: number;
  t: TranslateFn;
}): MetricCurve[] {
  const cores = getNumber(cpuInfo?.cores) || 1;
  const cpuLoad = asRecord(cpuInfo?.load);
  const cpuPoints = [
    Math.min(((getNumber(cpuLoad?.["15m"]) || 0) / cores) * 100, 100),
    Math.min(((getNumber(cpuLoad?.["5m"]) || 0) / cores) * 100, 100),
    Math.min(((getNumber(cpuLoad?.["1m"]) || 0) / cores) * 100, 100),
  ];
  const cpuCurrent = cpuPoints[cpuPoints.length - 1] ?? 0;

  const memTotal = getNumber(memoryInfo?.mem_total_bytes);
  const memAvailable = getNumber(memoryInfo?.mem_available_bytes);
  const memUsedPercent =
    memTotal && memAvailable !== null
      ? ((memTotal - memAvailable) / memTotal) * 100
      : null;

  const diskTotal = getNumber(diskInfo?.root_total_bytes);
  const diskFree = getNumber(diskInfo?.root_free_bytes);
  const diskUsedPercent =
    diskTotal && diskFree !== null
      ? ((diskTotal - diskFree) / diskTotal) * 100
      : null;

  const interfaces = [
    ...getArray(networkInfo?.interfaces),
    ...getArray(networkInfo?.devices),
    ...getArray(networkInfo?.links),
  ];
  const activeInterfaces = interfaces.filter((item) => {
    const record = asRecord(item);
    return Boolean(
      record?.up ??
      record?.is_up ??
      (typeof record?.status === "string" &&
        record.status.toLowerCase() === "up"),
    );
  }).length;
  const addressCounts = interfaces.map((item) => {
    const record = asRecord(item);
    return (
      getArray(record?.addresses).length ||
      getArray(record?.addr).length ||
      getArray(record?.ips).length ||
      0
    );
  });
  const inlineAddresses =
    getArray(networkInfo?.addresses).length ||
    getArray(networkInfo?.ip_addresses).length ||
    (networkInfo?.primary_ip || networkInfo?.primary_ipv4 ? 1 : 0);
  const totalAddresses =
    addressCounts.reduce((sum, count) => sum + count, 0) + inlineAddresses;
  const interfaceCount =
    interfaces.length ||
    getNumber(networkInfo?.interface_count) ||
    getNumber(networkInfo?.interfaces_count) ||
    0;
  const networkTraffic = aggregateNetworkTraffic(networkInfo, interfaces);
  const cpuSeries = toVisibleSeries(metricHistory.cpu, sessionStartedAt);
  const memorySeries = toVisibleSeries(metricHistory.memory, sessionStartedAt);
  const diskSeries = toVisibleSeries(metricHistory.disk, sessionStartedAt);
  const networkDownSeriesRaw = toVisibleSeries(
    metricHistory.networkDown,
    sessionStartedAt,
  );
  const networkUpSeriesRaw = toVisibleSeries(metricHistory.networkUp, sessionStartedAt);
  const networkPeak = Math.max(
    ...networkDownSeriesRaw.map((sample) => sample.value),
    ...networkUpSeriesRaw.map((sample) => sample.value),
    1,
  );
  const networkDownSeries = networkDownSeriesRaw.map((sample) => ({
    ...sample,
    value: (sample.value / networkPeak) * 100,
  }));
  const networkUpSeries = networkUpSeriesRaw.map((sample) => ({
    ...sample,
    value: (sample.value / networkPeak) * 100,
  }));
  const networkBase =
    interfaceCount > 0
      ? Math.min((activeInterfaces / interfaceCount) * 100, 100)
      : 0;
  const addressBase = Math.min(totalAddresses * 16, 100);

  return [
    {
      label: t("instances.metricCpu"),
      value: percentLabel(cpuCurrent, t),
      detail: t("instances.metricCpuDetail", {
        cores,
        load1: formatNumber(getNumber(cpuLoad?.["1m"]), t),
        load5: formatNumber(getNumber(cpuLoad?.["5m"]), t),
        load15: formatNumber(getNumber(cpuLoad?.["15m"]), t),
      }),
      accent: "#f97316",
      points: cpuSeries,
    },
    {
      label: t("instances.metricMemory"),
      value: percentLabel(memUsedPercent, t),
      detail: t("instances.metricMemoryDetail", {
        used: bytesToGB(
          memTotal !== null && memAvailable !== null ? memTotal - memAvailable : null,
          t,
        ),
        total: bytesToGB(memTotal, t),
      }),
      accent: "#3b82f6",
      points: memorySeries,
    },
    {
      label: t("instances.metricDisk"),
      value: percentLabel(diskUsedPercent, t),
      detail: t("instances.metricDiskDetail", {
        free: bytesToGB(diskFree, t),
        total: bytesToGB(diskTotal, t),
      }),
      accent: "#d97706",
      points: diskSeries,
    },
    {
      label: t("instances.metricNetwork"),
      value:
        formatTrafficPair(networkTraffic, t) ||
        `${activeInterfaces}/${interfaceCount || 0}`,
      detail:
        formatTrafficDetail(networkTraffic, t) ||
        t("instances.metricNetworkDetail", {
          addresses: totalAddresses,
          interfaces: interfaceCount,
        }),
      accent: "#14b8a6",
      secondaryAccent:
        networkTraffic.down !== null || networkTraffic.up !== null
          ? "#3b82f6"
          : undefined,
      secondaryPoints:
        networkTraffic.down !== null || networkTraffic.up !== null
          ? networkUpSeries
          : undefined,
      legend:
        networkTraffic.down !== null || networkTraffic.up !== null
          ? [
              { label: t("instances.metricLegendDown"), accent: "#14b8a6" },
              { label: t("instances.metricLegendUp"), accent: "#3b82f6" },
            ]
          : undefined,
      preNormalized:
        networkTraffic.down !== null || networkTraffic.up !== null,
      points:
        networkTraffic.down !== null || networkTraffic.up !== null
          ? networkDownSeries
          : [
              ...toVisibleSeries(metricHistory.networkDown, sessionStartedAt),
              {
                ts: Math.min(METRIC_WINDOW_MS, Date.now() - sessionStartedAt),
                value: Math.max(networkBase, addressBase, 6),
              },
            ],
    },
  ];
}

function aggregateNetworkTraffic(
  networkInfo: Record<string, any> | undefined,
  interfaces: any[],
) {
  const nonLoopbackInterfaces = interfaces.filter((item) => {
    const record = asRecord(item);
    return `${record?.name || ""}`.toLowerCase() !== "lo";
  });
  const preferredInterfaces =
    nonLoopbackInterfaces.length > 0 ? nonLoopbackInterfaces : interfaces;

  const interfaceTotalsDown = preferredInterfaces
    .map((item) =>
      firstNumber(
        asRecord(item)?.rx_bytes,
        asRecord(item)?.bytes_recv,
        asRecord(item)?.receive_bytes,
        asRecord(item)?.ingress_bytes,
      ),
    )
    .filter((value): value is number => value !== null);
  const interfaceTotalsUp = preferredInterfaces
    .map((item) =>
      firstNumber(
        asRecord(item)?.tx_bytes,
        asRecord(item)?.bytes_sent,
        asRecord(item)?.transmit_bytes,
        asRecord(item)?.egress_bytes,
      ),
    )
    .filter((value): value is number => value !== null);

  if (interfaceTotalsDown.length > 0 || interfaceTotalsUp.length > 0) {
    return {
      down: interfaceTotalsDown.length
        ? interfaceTotalsDown.reduce((sum, value) => sum + value, 0)
        : null,
      up: interfaceTotalsUp.length
        ? interfaceTotalsUp.reduce((sum, value) => sum + value, 0)
        : null,
    };
  }

  const directDown = firstNumber(
    networkInfo?.rx_rate_bps,
    networkInfo?.rx_bps,
    networkInfo?.rx_bytes_per_sec,
    networkInfo?.rx_rate,
    networkInfo?.download_bps,
    networkInfo?.download_rate_bps,
    networkInfo?.download_rate,
    networkInfo?.inbound_bps,
    networkInfo?.inbound_rate_bps,
    networkInfo?.inbound_rate,
    networkInfo?.ingress_bps,
    networkInfo?.ingress_rate_bps,
    networkInfo?.ingress_rate,
    networkInfo?.receive_bps,
    networkInfo?.receive_rate_bps,
    networkInfo?.receive_rate,
    networkInfo?.rx_bytes,
    networkInfo?.download_bytes,
    networkInfo?.inbound_bytes,
    networkInfo?.ingress_bytes,
    networkInfo?.receive_bytes,
    networkInfo?.bytes_recv,
  );
  const directUp = firstNumber(
    networkInfo?.tx_rate_bps,
    networkInfo?.tx_bps,
    networkInfo?.tx_bytes_per_sec,
    networkInfo?.tx_rate,
    networkInfo?.upload_bps,
    networkInfo?.upload_rate_bps,
    networkInfo?.upload_rate,
    networkInfo?.outbound_bps,
    networkInfo?.outbound_rate_bps,
    networkInfo?.outbound_rate,
    networkInfo?.egress_bps,
    networkInfo?.egress_rate_bps,
    networkInfo?.egress_rate,
    networkInfo?.transmit_bps,
    networkInfo?.transmit_rate_bps,
    networkInfo?.transmit_rate,
    networkInfo?.tx_bytes,
    networkInfo?.upload_bytes,
    networkInfo?.outbound_bytes,
    networkInfo?.egress_bytes,
    networkInfo?.transmit_bytes,
    networkInfo?.bytes_sent,
  );

  if (directDown !== null || directUp !== null) {
    return { down: directDown, up: directUp };
  }

  const perInterfaceDown = preferredInterfaces
    .map((item) =>
      firstNumber(
        asRecord(item)?.rx_rate_bps,
        asRecord(item)?.rx_bps,
        asRecord(item)?.rx_bytes_per_sec,
        asRecord(item)?.rx_rate,
        asRecord(item)?.download_bps,
        asRecord(item)?.download_rate_bps,
        asRecord(item)?.download_rate,
        asRecord(item)?.ingress_bps,
        asRecord(item)?.ingress_rate_bps,
        asRecord(item)?.ingress_rate,
        asRecord(item)?.receive_bps,
        asRecord(item)?.receive_rate_bps,
        asRecord(item)?.receive_rate,
        asRecord(item)?.rx_bytes,
        asRecord(item)?.ingress_bytes,
        asRecord(item)?.receive_bytes,
        asRecord(item)?.bytes_recv,
      ),
    )
    .filter((value): value is number => value !== null);
  const perInterfaceUp = preferredInterfaces
    .map((item) =>
      firstNumber(
        asRecord(item)?.tx_rate_bps,
        asRecord(item)?.tx_bps,
        asRecord(item)?.tx_bytes_per_sec,
        asRecord(item)?.tx_rate,
        asRecord(item)?.upload_bps,
        asRecord(item)?.upload_rate_bps,
        asRecord(item)?.upload_rate,
        asRecord(item)?.egress_bps,
        asRecord(item)?.egress_rate_bps,
        asRecord(item)?.egress_rate,
        asRecord(item)?.transmit_bps,
        asRecord(item)?.transmit_rate_bps,
        asRecord(item)?.transmit_rate,
        asRecord(item)?.tx_bytes,
        asRecord(item)?.egress_bytes,
        asRecord(item)?.transmit_bytes,
        asRecord(item)?.bytes_sent,
      ),
    )
    .filter((value): value is number => value !== null);

  return {
    down: perInterfaceDown.length
      ? perInterfaceDown.reduce((sum, value) => sum + value, 0)
      : null,
    up: perInterfaceUp.length
      ? perInterfaceUp.reduce((sum, value) => sum + value, 0)
      : null,
  };
}

function normalizePoints(points: number[]): number[] {
  const safe = points.map((point) => Math.max(0.04, Math.min(point / 100, 1)));
  const max = Math.max(...safe, 0.04);
  return safe.map((point) => Math.max(point / max, 0.08));
}

function formatNumber(value: number | null, t: TranslateFn): string {
  if (value === null) {
    return t("instances.notAvailable");
  }
  return value.toFixed(2);
}

function formatBytesCompact(value: number | null, t: TranslateFn): string {
  if (value === null) {
    return t("instances.notAvailable");
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

function formatTrafficPair(traffic: {
  down: number | null;
  up: number | null;
}, t: TranslateFn) {
  if (traffic.down === null && traffic.up === null) {
    return "";
  }
  return `↓ ${formatBytesCompact(traffic.down, t)} ↑ ${formatBytesCompact(traffic.up, t)}`;
}

function formatTrafficDetail(traffic: {
  down: number | null;
  up: number | null;
}, t: TranslateFn) {
  if (traffic.down === null && traffic.up === null) {
    return "";
  }
  return t("instances.metricTrafficDetail", {
    inbound: formatBytesCompact(traffic.down, t),
    outbound: formatBytesCompact(traffic.up, t),
  });
}

function appendMetricSample(
  samples: MetricSample[],
  value: number | null,
  ts: number,
): MetricSample[] {
  if (value === null || !Number.isFinite(value)) {
    return trimMetricSamples(samples, ts);
  }
  return trimMetricSamples(
    [
      ...samples,
      {
        ts,
        value: Math.max(0, value),
      },
    ],
    ts,
  );
}

function trimMetricSamples(samples: MetricSample[], ts: number): MetricSample[] {
  const cutoff = ts - METRIC_WINDOW_MS;
  return samples.filter((sample) => sample.ts >= cutoff);
}

function toVisibleSeries(
  samples: MetricSample[],
  sessionStartedAt: number,
): MetricSample[] {
  const now = Date.now();
  const windowStart = Math.max(sessionStartedAt, now - METRIC_WINDOW_MS);
  return samples
    .filter((sample) => sample.ts >= windowStart)
    .map((sample) => ({
      ts: Math.max(0, Math.min(sample.ts - windowStart, METRIC_WINDOW_MS)),
      value: Math.max(0, Math.min(sample.value, 100)),
    }));
}

function extractMetricSnapshot(systemInfoValue: unknown) {
  const systemInfo = asRecord(systemInfoValue);
  if (!systemInfo) {
    return null;
  }

  const cpuInfo = asRecord(systemInfo.cpu);
  const cpuLoad = asRecord(cpuInfo?.load);
  const cores = getNumber(cpuInfo?.cores) || 1;
  const cpuPercent = Math.min(
    (((getNumber(cpuLoad?.["1m"]) || 0) / cores) * 100) || 0,
    100,
  );

  const memoryInfo = asRecord(systemInfo.memory);
  const memTotal = getNumber(memoryInfo?.mem_total_bytes);
  const memAvailable = getNumber(memoryInfo?.mem_available_bytes);
  const memoryPercent =
    memTotal && memAvailable !== null
      ? ((memTotal - memAvailable) / memTotal) * 100
      : null;

  const diskInfo = asRecord(systemInfo.disk);
  const diskTotal = getNumber(diskInfo?.root_total_bytes);
  const diskFree = getNumber(diskInfo?.root_free_bytes);
  const diskPercent =
    diskTotal && diskFree !== null
      ? ((diskTotal - diskFree) / diskTotal) * 100
      : null;

  const networkInfo = asRecord(systemInfo.network);
  const interfaces = [
    ...getArray(networkInfo?.interfaces),
    ...getArray(networkInfo?.devices),
    ...getArray(networkInfo?.links),
  ];
  const networkTraffic = aggregateNetworkTraffic(networkInfo, interfaces);

  return {
    cpuPercent,
    memoryPercent,
    diskPercent,
    networkDownTotal: networkTraffic.down,
    networkUpTotal: networkTraffic.up,
  };
}

function formatMetricValue(value: unknown, t: TranslateFn): string {
  if (value === null || value === undefined) {
    return t("instances.notAvailable");
  }
  if (typeof value === "string") {
    return value.trim() || t("instances.notAvailable");
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return `${value}`;
  }
  if (Array.isArray(value)) {
    if (value.length === 0) {
      return t("instances.notAvailable");
    }
    return value
      .map((item) => formatMetricValue(item, t))
      .filter((item) => item !== t("instances.notAvailable"))
      .join(", ");
  }

  const record = value as Record<string, unknown>;
  const label = firstValue(
    record.label,
    record.value,
    record.total,
    record.total_gb,
    record.model,
    record.name,
    record.version,
    record.hostname,
    record.primary_ip,
  );

  if (label !== undefined) {
    return formatMetricValue(label, t);
  }

  try {
    return JSON.stringify(record);
  } catch {
    return t("instances.notAvailable");
  }
}

export default InstanceDetailPage;
