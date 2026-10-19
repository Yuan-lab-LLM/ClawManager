import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useLocation } from "react-router-dom";
import { Monitor, MonitorPlay, Play, Plus, Search, Square, Trash2 } from "lucide-react";
import ConfirmDialog from "../../components/ConfirmDialog";
import { runtimeFailureSummary } from "../../lib/runtimeFailure";
import InstanceLifecycleBatches from "../../components/InstanceLifecycleBatches";
import { collectUnavailableLiteIds, pruneVisibleSelection } from "../../lib/unavailableInstanceSelection";
import UserLayout from "../../components/UserLayout";
import { useI18n } from "../../contexts/I18nContext";
import { instanceService } from "../../services/instanceService";
import {
  systemSettingsService,
  type SystemImageSetting,
} from "../../services/systemSettingsService";
import { teamService } from "../../services/teamService";
import {
  formatInstanceType,
  INSTANCE_TYPES,
  type Instance,
  type InstanceAvailability,
} from "../../types/instance";
import type { Team, TeamMember } from "../../types/team";

type AvailabilityFilter = "all" | InstanceAvailability;
type ModeFilter = "all" | Instance["instance_mode"];
type TeamMembership = {
  team: Team;
  member: TeamMember;
};
type LoadInstancesOptions = {
  silent?: boolean;
  refreshTeams?: boolean;
};

const INSTANCE_LIST_PAGE_SIZE = 100;
const TEAM_LIST_PAGE_SIZE = 100;

const runtimeImageKey = (item: SystemImageSetting) =>
  item.id
    ? "runtime-image-id:" + item.id
    : ["runtime-image", item.instance_type, item.runtime_type ?? "gateway", item.image].join(":");

const instanceTimeValue = (instance: Instance) => {
  const value = Date.parse(instance.created_at || "");
  return Number.isFinite(value) ? value : 0;
};

const sortInstances = (items: Instance[]) =>
  [...items].sort(
    (left, right) => instanceTimeValue(right) - instanceTimeValue(left) || right.id - left.id,
  );

const loadAllTeams = async () => {
  const firstPage = await teamService.listTeams(1, TEAM_LIST_PAGE_SIZE);
  const teams = [...(firstPage.teams || [])];
  const total = firstPage.total || teams.length;
  const totalPages = Math.ceil(total / TEAM_LIST_PAGE_SIZE);

  for (let page = 2; page <= totalPages; page += 1) {
    const nextPage = await teamService.listTeams(page, TEAM_LIST_PAGE_SIZE);
    teams.push(...(nextPage.teams || []));
  }

  return teams;
};

const loadTeamMemberships = async (strict = false) => {
  const teams = await loadAllTeams();
  const details = await Promise.all(
    teams.map((team) => strict ? teamService.getTeam(team.id) : teamService.getTeam(team.id).catch(() => null)),
  );
  const memberships = new Map<number, TeamMembership[]>();

  details.forEach((detail) => {
    if (!detail) {
      return;
    }
    detail.members.forEach((member) => {
      if (!member.instance_id) {
        return;
      }
      const current = memberships.get(member.instance_id) || [];
      memberships.set(member.instance_id, [
        ...current,
        {
          team: detail.team,
          member,
        },
      ]);
    });
  });

  return memberships;
};

function availabilityForStatus(status: string): InstanceAvailability {
  if (status === "running") {
    return "available";
  }
  if (status === "creating") {
    return "starting";
  }
  return "unavailable";
}

function availabilityLabel(availability: InstanceAvailability) {
  switch (availability) {
    case "available":
      return "Available";
    case "starting":
      return "Starting";
    default:
      return "Unavailable";
  }
}

function availabilityClass(availability: InstanceAvailability) {
  switch (availability) {
    case "available":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "starting":
      return "border-amber-200 bg-amber-50 text-amber-700";
    default:
      return "border-slate-200 bg-slate-50 text-slate-600";
  }
}

function modeLabel(mode: Instance["instance_mode"]) {
  return mode === "pro" ? "Pro" : "Lite";
}

function modeClass(mode: Instance["instance_mode"]) {
  return mode === "pro"
    ? "border-indigo-200 bg-indigo-50 text-indigo-700"
    : "border-sky-200 bg-sky-50 text-sky-700";
}

function isLiteInstance(instance: Instance) {
  if (instance.instance_mode === "lite" || instance.instance_mode === "pro") {
    return instance.instance_mode === "lite";
  }
  return instance.runtime_type === "gateway";
}

function formatBytes(value?: number) {
  if (!value || value <= 0) {
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

function getErrorMessage(err: unknown, fallback: string) {
  const responseError = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
  if (responseError) {
    return responseError;
  }
  return err instanceof Error ? err.message : fallback;
}

const InstanceListPage: React.FC = () => {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const initialQuery = new URLSearchParams(location.search);
  const detailState = { returnTo: location.pathname + location.search };
  const [instances, setInstances] = useState<Instance[]>([]);
  const [teamMemberships, setTeamMemberships] = useState<Map<number, TeamMembership[]>>(
    new Map(),
  );
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [deletingIds, setDeletingIds] = useState<number[]>([]);
  const [actionLoading, setActionLoading] = useState<number | null>(null);
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null);
  const [availabilityFilter, setAvailabilityFilter] = useState<AvailabilityFilter>(() => (["available", "starting", "unavailable"].includes(initialQuery.get("availability") || "") ? initialQuery.get("availability") : "all") as AvailabilityFilter);
  const [typeFilter, setTypeFilter] = useState(initialQuery.get("type") || "all");
  const [modeFilter, setModeFilter] = useState<ModeFilter>(() => (["lite", "pro"].includes(initialQuery.get("mode") || "") ? initialQuery.get("mode") : "all") as ModeFilter);
  const [searchQuery, setSearchQuery] = useState(initialQuery.get("q") || "");
  const [debouncedSearchQuery, setDebouncedSearchQuery] = useState(initialQuery.get("q") || "");
  const [page, setPage] = useState(() => Math.max(1, Number(initialQuery.get("page")) || 1));
  const [total, setTotal] = useState(0);
  const [selectedLiteIds, setSelectedLiteIds] = useState<number[]>([]);
  const [allUnavailableSelected, setAllUnavailableSelected] = useState(false);
  const [selectingUnavailable, setSelectingUnavailable] = useState(false);
  const [unavailableSelectionNotice, setUnavailableSelectionNotice] = useState("");
  const selectionRequest = useRef(0);
  useEffect(() => () => { selectionRequest.current++; }, []);
  const [batchCreateOpen, setBatchCreateOpen] = useState(false);
  const [batchCreatePrefix, setBatchCreatePrefix] = useState("lite-openclaw");
  const [batchCreateCount, setBatchCreateCount] = useState(3);
  const [batchCreateStartIndex, setBatchCreateStartIndex] = useState(1);
  const [batchCreateType, setBatchCreateType] = useState<
    "openclaw" | "hermes" | "opencode" | "deepseek-harness"
  >("openclaw");
  const [batchCreateImageKey, setBatchCreateImageKey] = useState("");
  const [runtimeImageSettings, setRuntimeImageSettings] = useState<SystemImageSetting[]>([]);
  const [batchCreateLoading, setBatchCreateLoading] = useState(false);
  const [batchCreateSummary, setBatchCreateSummary] = useState<string | null>(null);
  const [batchDeleteLoading, setBatchDeleteLoading] = useState(false);
  const [pendingBatchDelete, setPendingBatchDelete] = useState(false);

  const refreshTeamMemberships = useCallback(async () => {
    try {
      const memberships = await loadTeamMemberships();
      setTeamMemberships(memberships);
    } catch (teamError) {
      console.error("Failed to load team memberships", teamError);
    }
  }, []);

  const loadInstances = useCallback(
    async (options?: LoadInstancesOptions) => {
      try {
        if (!options?.silent) {
          setLoading(true);
        }
        setError(null);
        const data = await instanceService.getInstances(page, INSTANCE_LIST_PAGE_SIZE, {
          query: debouncedSearchQuery.trim() || undefined,
          type: typeFilter === "all" ? undefined : typeFilter,
          instance_mode: modeFilter === "all" ? undefined : modeFilter,
          availability: availabilityFilter === "all" ? undefined : availabilityFilter,
        });
        setInstances(sortInstances(data.instances || []));
        setTotal(data.total || 0);
        if ((data.instances || []).length === 0 && data.total > 0 && page > 1) {
          setPage(Math.max(1, Math.ceil(data.total / INSTANCE_LIST_PAGE_SIZE)));
        }
        if (options?.refreshTeams !== false) {
          void refreshTeamMemberships();
        }
      } catch (err: unknown) {
        setError(getErrorMessage(err, t("instances.failedToLoad")));
      } finally {
        if (!options?.silent) {
          setLoading(false);
        }
      }
    },
    [availabilityFilter, debouncedSearchQuery, modeFilter, page, refreshTeamMemberships, t, typeFilter],
  );

  useEffect(() => {
    void loadInstances();
  }, [loadInstances]);
  useEffect(() => {
    if (searchQuery === debouncedSearchQuery) return;
    const timeoutId = window.setTimeout(() => {
      setPage(1);
      setDebouncedSearchQuery(searchQuery);
    }, 250);
    return () => window.clearTimeout(timeoutId);
  }, [searchQuery, debouncedSearchQuery]);
  useEffect(() => {
    const params = new URLSearchParams();
    if (searchQuery) params.set("q", searchQuery);
    if (typeFilter !== "all") params.set("type", typeFilter);
    if (modeFilter !== "all") params.set("mode", modeFilter);
    if (availabilityFilter !== "all") params.set("availability", availabilityFilter);
    if (page > 1) params.set("page", String(page));
    const search = params.toString();
    if (location.search.replace(/^\?/, "") !== search) {
      navigate({ pathname: location.pathname, search }, { replace: true });
    }
  }, [searchQuery, typeFilter, modeFilter, availabilityFilter, page, navigate, location.pathname, location.search]);
  useEffect(() => {
    let cancelled = false;
    systemSettingsService
      .getImageSettings()
      .then((items) => {
        if (!cancelled) {
          setRuntimeImageSettings(items.filter((item) => item.is_enabled !== false));
        }
      })
      .catch((imageError) => {
        console.error("Failed to load runtime images", imageError);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!instances.some((instance) => instance.status === "creating" || instance.status === "deleting")) {
      return;
    }

    const intervalId = window.setInterval(() => {
      void loadInstances({ silent: true, refreshTeams: false });
    }, 5000);

    return () => window.clearInterval(intervalId);
  }, [instances, loadInstances]);

  const filteredInstances = instances;
  const typeOptions = useMemo(() => {
    const values = new Set(INSTANCE_TYPES.map((item) => item.id));
    runtimeImageSettings.forEach((item) => values.add(item.instance_type));
    instances.forEach((instance) => values.add(instance.type));
    return Array.from(values).sort((left, right) =>
      formatInstanceType(left).localeCompare(formatInstanceType(right), locale),
    );
  }, [instances, locale, runtimeImageSettings]);
  const totalPages = Math.max(1, Math.ceil(total / INSTANCE_LIST_PAGE_SIZE));
  const resultFrom = total === 0 ? 0 : (page - 1) * INSTANCE_LIST_PAGE_SIZE + 1;
  const resultTo = total === 0 ? 0 : Math.min(page * INSTANCE_LIST_PAGE_SIZE, total);
  const filtersActive =
    searchQuery.trim() !== "" ||
    typeFilter !== "all" ||
    modeFilter !== "all" ||
    availabilityFilter !== "all";

  const clearFilters = useCallback(() => {
    setSearchQuery("");
    setDebouncedSearchQuery("");
    setTypeFilter("all");
    setModeFilter("all");
    setAvailabilityFilter("all");
    setPage(1);
  }, []);

  const batchRuntimeImageOptions = useMemo(
    () =>
      runtimeImageSettings.filter(
        (item) =>
          item.instance_type === batchCreateType &&
          (item.runtime_type ?? "gateway") === "gateway",
      ),
    [batchCreateType, runtimeImageSettings],
  );
  const selectedBatchRuntimeImage =
    batchRuntimeImageOptions.find((item) => runtimeImageKey(item) === batchCreateImageKey) ?? null;
  const selectableLiteIds = useMemo(
    () =>
      filteredInstances
        .filter((instance) => isLiteInstance(instance) && instance.status !== "deleting")
        .map((instance) => instance.id),
    [filteredInstances],
  );

  useEffect(() => {
    if (
      batchCreateImageKey &&
      !batchRuntimeImageOptions.some((item) => runtimeImageKey(item) === batchCreateImageKey)
    ) {
      setBatchCreateImageKey("");
    }
  }, [batchCreateImageKey, batchRuntimeImageOptions]);
  const selectedLiteSet = useMemo(() => new Set(selectedLiteIds), [selectedLiteIds]);
  const selectedLiteCount = selectedLiteIds.length;
  const allVisibleLiteSelected =
    selectableLiteIds.length > 0 && selectableLiteIds.every((id) => selectedLiteSet.has(id));

  useEffect(() => {
    setSelectedLiteIds((ids) => pruneVisibleSelection(ids, instances));
  }, [instances]);

  const selectAllUnavailable = async (checked: boolean) => {
    const request = ++selectionRequest.current;
    setAllUnavailableSelected(false);
    setSelectingUnavailable(checked);
    setUnavailableSelectionNotice("");
    if (!checked) { setSelectedLiteIds([]); return; }
    try {
      const memberships = await loadTeamMemberships(true);
      const ids = await collectUnavailableLiteIds(instanceService.getInstances, new Set(memberships.keys()), () => request !== selectionRequest.current);
      if (request !== selectionRequest.current) return;
      setSelectedLiteIds(ids);
      setAllUnavailableSelected(ids.length > 0);
      setUnavailableSelectionNotice(ids.length ? `已跨页选中 ${ids.length} 个不可用 Lite 实例；不受当前搜索、类型筛选限制。${ids.length > 500 ? "单次批量重启/重置最多选择 500 个，请减少选择。" : ""}` : "没有可选的不可用 Lite 实例。");
    } catch (err) {
      if (request === selectionRequest.current) setUnavailableSelectionNotice(getErrorMessage(err, "全选失败，原选择已保留，请重试。"));
    } finally {
      if (request === selectionRequest.current) setSelectingUnavailable(false);
    }
  };

  const toggleLiteSelection = useCallback((id: number) => {
    selectionRequest.current++;
    setSelectingUnavailable(false);
    setAllUnavailableSelected(false);
    setUnavailableSelectionNotice("");
    setSelectedLiteIds((ids) =>
      ids.includes(id) ? ids.filter((selectedId) => selectedId !== id) : [...ids, id],
    );
  }, []);

  const toggleAllVisibleLite = useCallback(() => {
    selectionRequest.current++;
    setSelectingUnavailable(false);
    setAllUnavailableSelected(false);
    setUnavailableSelectionNotice("");
    setSelectedLiteIds((ids) => {
      const visible = new Set(selectableLiteIds);
      if (selectableLiteIds.length > 0 && selectableLiteIds.every((id) => ids.includes(id))) {
        return ids.filter((id) => !visible.has(id));
      }
      return Array.from(new Set([...ids, ...selectableLiteIds]));
    });
  }, [selectableLiteIds]);

  const handleBatchCreateLite = useCallback(async () => {
    try {
      setBatchCreateLoading(true);
      setBatchCreateSummary(null);
      const result = await instanceService.batchCreateLiteInstances({
        name_prefix: batchCreatePrefix.trim(),
        count: batchCreateCount,
        start_index: batchCreateStartIndex,
        template: {
          type: batchCreateType,
          mode: "lite",
          instance_mode: "lite",
          runtime_type: "gateway",
          os_type: batchCreateType,
          os_version: "latest",
          image_registry: selectedBatchRuntimeImage?.image,
          gpu_enabled: false,
          gpu_count: 0,
        },
      });
      setBatchCreateSummary(
        t("instances.batchCreateSummary", {
          created: result.created,
          failed: result.failed,
        }),
      );
      await loadInstances({ silent: true });
      if (result.failed === 0) {
        setBatchCreateOpen(false);
      }
    } catch (err: unknown) {
      setBatchCreateSummary(getErrorMessage(err, t("instances.batchCreateFailed")));
    } finally {
      setBatchCreateLoading(false);
    }
  }, [
    batchCreateCount,
    batchCreatePrefix,
    batchCreateStartIndex,
    batchCreateType,
    selectedBatchRuntimeImage,
    loadInstances,
    t,
  ]);

  const handleBatchDeleteLite = useCallback(async () => {
    try {
      setBatchDeleteLoading(true);
      const result = await instanceService.batchDeleteLiteInstances(selectedLiteIds);
      if (result.failed > 0) {
        alert(t("instances.batchDeletePartial", { deleted: result.deleted, failed: result.failed }));
      }
      setSelectedLiteIds([]);
      setPendingBatchDelete(false);
      await loadInstances({ silent: true });
    } catch (err: unknown) {
      alert(getErrorMessage(err, t("instances.batchDeleteFailed")));
    } finally {
      setBatchDeleteLoading(false);
    }
  }, [loadInstances, selectedLiteIds, t]);
  const handleDelete = useCallback(
    async (id: number) => {
      try {
        setDeletingIds((prevIds) => [...prevIds, id]);
        await instanceService.deleteInstance(id);
        setPendingDeleteId(null);
        await loadInstances({ silent: true });
      } catch (err: unknown) {
        alert(getErrorMessage(err, t("instances.failedToDelete")));
      } finally {
        setDeletingIds((prevIds) => prevIds.filter((deletingId) => deletingId !== id));
      }
    },
    [loadInstances, t],
  );

  const handleStart = useCallback(
    async (id: number) => {
      try {
        setActionLoading(id);
        await instanceService.startInstance(id);
        await loadInstances({ silent: true, refreshTeams: false });
      } catch (err: unknown) {
        alert(getErrorMessage(err, t("instances.failedToStart")));
      } finally {
        setActionLoading(null);
      }
    },
    [loadInstances, t],
  );

  const handleStop = useCallback(
    async (id: number) => {
      try {
        setActionLoading(id);
        await instanceService.stopInstance(id);
        await loadInstances({ silent: true, refreshTeams: false });
      } catch (err: unknown) {
        alert(getErrorMessage(err, t("instances.failedToStop")));
      } finally {
        setActionLoading(null);
      }
    },
    [loadInstances, t],
  );

  return (
    <UserLayout title={t("instances.listTitle")}>
      <ConfirmDialog
        open={pendingDeleteId !== null}
        title={t("common.delete")}
        message={t("instances.confirmDelete")}
        confirmLabel={t("common.delete")}
        cancelLabel={t("common.cancel")}
        destructive
        loading={pendingDeleteId !== null && deletingIds.includes(pendingDeleteId)}
        onCancel={() => setPendingDeleteId(null)}
        onConfirm={() => {
          if (pendingDeleteId !== null) {
            void handleDelete(pendingDeleteId);
          }
        }}
      />

      <ConfirmDialog
        open={pendingBatchDelete}
        title={t("instances.batchDeleteLite")}
        message={t("instances.batchDeleteConfirm", { count: selectedLiteCount })}
        confirmLabel={t("instances.batchDeleteLite")}
        cancelLabel={t("common.cancel")}
        destructive
        loading={batchDeleteLoading}
        onCancel={() => setPendingBatchDelete(false)}
        onConfirm={() => void handleBatchDeleteLite()}
      />

      {batchCreateOpen ? (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(15,23,42,0.45)] px-4"
          onClick={(event) => {
            if (event.target === event.currentTarget && !batchCreateLoading) {
              setBatchCreateOpen(false);
            }
          }}
        >
          <div className="w-full max-w-lg rounded-lg border border-slate-200 bg-white p-6 shadow-xl">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h3 className="text-lg font-semibold text-slate-950">
                  {t("instances.batchCreateLite")}
                </h3>
                <p className="mt-1 text-sm text-slate-500">
                  {t("instances.batchCreateLiteSubtitle")}
                </p>
              </div>
            </div>

            <div className="mt-5 space-y-4">
              <div>
                <div className="mb-2 text-sm font-medium text-slate-700">
                  {t("instances.batchMode")}
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    className="rounded-md border border-sky-300 bg-sky-50 px-3 py-2 text-left text-sm font-medium text-sky-700"
                  >
                    {t("instances.batchModeLite")}
                  </button>
                  <button
                    type="button"
                    disabled
                    className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-left text-sm font-medium text-slate-400 disabled:cursor-not-allowed"
                    title={t("instances.batchModeProDisabled")}
                  >
                    {t("instances.batchModePro")}
                  </button>
                </div>
              </div>

              <label className="block text-sm font-medium text-slate-700">
                {t("instances.batchNamePrefix")}
                <input
                  type="text"
                  value={batchCreatePrefix}
                  onChange={(event) => setBatchCreatePrefix(event.target.value)}
                  className="app-input mt-1 w-full"
                  placeholder="lite-openclaw"
                />
              </label>

              <div className="grid gap-4 sm:grid-cols-2">
                <label className="block text-sm font-medium text-slate-700">
                  {t("instances.batchCount")}
                  <input
                    type="number"
                    min={1}
                    max={100}
                    value={batchCreateCount}
                    onChange={(event) => setBatchCreateCount(Number(event.target.value))}
                    className="app-input mt-1 w-full"
                  />
                </label>
                <label className="block text-sm font-medium text-slate-700">
                  {t("instances.batchStartIndex")}
                  <input
                    type="number"
                    min={1}
                    max={9999}
                    value={batchCreateStartIndex}
                    onChange={(event) => setBatchCreateStartIndex(Number(event.target.value))}
                    className="app-input mt-1 w-full"
                  />
                </label>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <label className="block text-sm font-medium text-slate-700">
                  {t("instances.type")}
                  <select
                    value={batchCreateType}
                    onChange={(event) =>
                      setBatchCreateType(
                        event.target.value as
                          | "openclaw"
                          | "hermes"
                          | "opencode"
                          | "deepseek-harness",
                      )
                    }
                    className="app-input mt-1 w-full"
                  >
                    <option value="openclaw">OpenClaw</option>
                    <option value="hermes">Hermes</option>
                    <option value="opencode">OpenCode</option>
                    <option value="deepseek-harness">DeepSeek Harness</option>
                  </select>
                </label>
                <label className="block text-sm font-medium text-slate-700">
                  {t("instances.batchImage")}
                  <select
                    value={batchCreateImageKey}
                    onChange={(event) => setBatchCreateImageKey(event.target.value)}
                    className="app-input mt-1 w-full"
                  >
                    <option value="">{t("instances.batchDefaultImage")}</option>
                    {batchRuntimeImageOptions.map((item) => (
                      <option key={runtimeImageKey(item)} value={runtimeImageKey(item)}>
                        {item.display_name || item.image}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
            </div>
            <div className="mt-4 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-600">
              {t("instances.batchCreatePreview", {
                first: `${batchCreatePrefix || "lite"}-${String(batchCreateStartIndex || 1).padStart(3, "0")}`,
                last: `${batchCreatePrefix || "lite"}-${String((batchCreateStartIndex || 1) + Math.max(batchCreateCount - 1, 0)).padStart(3, "0")}`,
              })}
            </div>

            {batchCreateSummary ? (
              <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-700">
                {batchCreateSummary}
              </div>
            ) : null}

            <div className="mt-6 flex justify-end gap-3">
              <button
                type="button"
                onClick={() => setBatchCreateOpen(false)}
                disabled={batchCreateLoading}
                className="app-button-secondary"
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                onClick={() => void handleBatchCreateLite()}
                disabled={batchCreateLoading}
                className="app-button-primary"
              >
                <Plus className="h-4 w-4" />
                {batchCreateLoading ? t("instances.batchCreating") : t("instances.batchCreateLite")}
              </button>
            </div>
          </div>
        </div>
      ) : null}
      <InstanceLifecycleBatches selectedIds={selectedLiteIds} selectionBusy={selectingUnavailable} onChanged={() => void loadInstances({ silent: true })} selectionControl={<>
        <label className="flex items-center gap-2 text-sm text-slate-600" title="跨所有页面选择当前账号的不可用独立 Lite 实例；排除删除中、清理残留及团队实例，不受当前筛选限制。">
          <input type="checkbox" checked={allUnavailableSelected || selectingUnavailable} onChange={e => void selectAllUnavailable(e.target.checked)} />
          {selectingUnavailable ? "正在跨页获取…（取消勾选可中止）" : "选中所有不可用实例"}
        </label>
        {unavailableSelectionNotice && <span role="status" className="text-xs text-slate-500">{unavailableSelectionNotice}</span>}
      </>} />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap gap-2">
          <Link to="/instances/new" className="app-button-primary self-start">
            <Monitor className="h-4 w-4" />
            {t("instances.createInstance")}
          </Link>
          <button
            type="button"
            onClick={() => {
              setBatchCreateSummary(null);
              setBatchCreateOpen(true);
            }}
            className="app-button-secondary self-start"
          >
            <Plus className="h-4 w-4" />
            {t("instances.batchCreateLite")}
          </button>
          <button
            type="button"
            onClick={() => setPendingBatchDelete(true)}
            disabled={selectedLiteCount === 0 || batchDeleteLoading || selectingUnavailable}
            className="app-button-secondary self-start border-red-200 text-red-600 hover:border-red-300 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Trash2 className="h-4 w-4" />
            {selectedLiteCount > 0
              ? t("instances.batchDeleteLiteWithCount", { count: selectedLiteCount })
              : t("instances.batchDeleteLite")}
          </button>
          <Link to="/portal" className="app-button-secondary self-start">
            <MonitorPlay className="h-4 w-4" />
            {t("instances.portalView")}
          </Link>
        </div>

        <div className="flex flex-col gap-2 xl:flex-row xl:flex-wrap xl:justify-end">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-slate-400" />
            <input
              type="text"
              placeholder={`ID / ${t("instances.searchPlaceholder")}`}
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              className="app-input w-full pl-9 sm:w-64"
            />
          </div>
          <select
            value={typeFilter}
            onChange={(event) => {
              setTypeFilter(event.target.value);
              setPage(1);
            }}
            className="app-input"
            aria-label={t("instances.instanceTypeFilter")}
          >
            <option value="all">{t("instances.allTypes")}</option>
            {typeOptions.map((type) => (
              <option key={type} value={type}>
                {formatInstanceType(type)}
              </option>
            ))}
          </select>
          <select
            value={modeFilter}
            onChange={(event) => {
              setModeFilter(event.target.value as ModeFilter);
              setPage(1);
            }}
            className="app-input"
            aria-label={t("instances.instanceModeFilter")}
          >
            <option value="all">{t("instances.allModes")}</option>
            <option value="lite">Lite</option>
            <option value="pro">Pro</option>
          </select>
          <select
            value={availabilityFilter}
            onChange={(event) => {
              setAvailabilityFilter(event.target.value as AvailabilityFilter);
              setPage(1);
            }}
            className="app-input"
            aria-label={t("instances.availabilityFilter")}
          >
            <option value="all">{t("instances.allAvailability")}</option>
            <option value="available">Available</option>
            <option value="starting">Starting</option>
            <option value="unavailable">Unavailable</option>
          </select>
          {filtersActive ? (
            <button type="button" onClick={clearFilters} className="app-button-secondary">
              {t("instances.clearFilters")}
            </button>
          ) : null}
        </div>
      </div>

      {!loading && !error && total > 0 ? (
        <div className="flex items-center justify-between text-xs text-slate-500">
          <span>
            {t("instances.showingResults", {
              filtered: `${resultFrom}-${resultTo}`,
              total,
            })}
          </span>
          <span>
            {t("instances.pageOf", { page, totalPages })}
          </span>
        </div>
      ) : null}

      {loading ? (
        <div className="flex h-64 items-center justify-center text-sm text-slate-500">
          {t("common.loading")}
        </div>
      ) : error ? (
        <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
        </div>
      ) : instances.length === 0 && !filtersActive ? (
        <div className="cm-surface p-10 text-center">
          <h3 className="text-sm font-medium text-slate-950">{t("instances.noInstances")}</h3>
          <div className="mt-5">
            <Link to="/instances/new" className="app-button-primary">
              {t("instances.createInstance")}
            </Link>
          </div>
        </div>
      ) : filteredInstances.length === 0 ? (
        <div className="cm-surface p-10 text-center text-sm text-slate-500">
          {t("instances.noMatchingInstances")}
        </div>
      ) : (
        <div className="cm-surface overflow-x-auto">
          <table className="w-full min-w-[820px] table-fixed divide-y divide-slate-200 text-sm">
            <thead className="bg-slate-50 text-left text-xs font-medium uppercase tracking-normal text-slate-500">
              <tr>
                <th className="w-[5%] px-4 py-3">
                  <input
                    type="checkbox"
                    checked={allVisibleLiteSelected}
                    disabled={selectableLiteIds.length === 0}
                    onChange={toggleAllVisibleLite}
                    className="h-4 w-4 rounded border-slate-300 text-red-600 focus:ring-red-500 disabled:opacity-40"
                    aria-label={t("instances.selectVisibleLite")}
                  />
                </th>
                <th className="w-[28%] px-4 py-3">Instance</th>
                <th className="w-[13%] px-4 py-3">Type</th>
                <th className="w-[20%] px-4 py-3">Information</th>
                <th className="w-[16%] px-4 py-3">Availability</th>
                <th className="w-[10%] px-4 py-3">Workspace</th>
                <th className="w-[10%] px-4 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 bg-white">
              {filteredInstances.map((instance) => {
                const availability = availabilityForStatus(instance.status);
                const memberships = teamMemberships.get(instance.id) || [];
                const primaryMembership = memberships[0];
                const canSelectLite = isLiteInstance(instance) && instance.status !== "deleting";
                const selected = selectedLiteSet.has(instance.id);
                return (
                  <tr
                    key={instance.id}
                    role="link"
                    tabIndex={0}
                    onClick={() => navigate(`/instances/${instance.id}`, { state: detailState })}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        navigate(`/instances/${instance.id}`, { state: detailState });
                      }
                    }}
                    className={`cursor-pointer focus:outline-none focus-visible:bg-slate-50 ${
                      primaryMembership ? "bg-sky-50/30 hover:bg-sky-50/60" : "hover:bg-slate-50"
                    }`}
                  >
                    <td
                      className="px-4 py-3"
                      onClick={(event) => event.stopPropagation()}
                      onKeyDown={(event) => event.stopPropagation()}
                    >
                      <input
                        type="checkbox"
                        checked={selected}
                        disabled={!canSelectLite}
                        onChange={() => toggleLiteSelection(instance.id)}
                        className="h-4 w-4 rounded border-slate-300 text-red-600 focus:ring-red-500 disabled:opacity-30"
                        aria-label={t("instances.selectLiteInstance", { name: instance.name })}
                      />
                    </td>
                    <td className="max-w-[280px] px-4 py-3">
                      <Link
                        to={`/instances/${instance.id}`}
                        state={detailState}
                        onClick={(event) => event.stopPropagation()}
                        className="block truncate font-medium text-slate-950 hover:text-red-700"
                      >
                        {instance.name}
                      </Link>
                      <div className="mt-1 truncate text-xs text-slate-500">
                        {t("instances.createdAt", {
                          time: instance.created_at
                            ? new Date(instance.created_at).toLocaleString(locale)
                            : "-",
                        })}
                      </div>
                      {primaryMembership && <Link
                        to={`/teams/${primaryMembership.team.id}`}
                        title={`${primaryMembership.team.name} / ${primaryMembership.member.display_name || primaryMembership.member.member_key} / ${primaryMembership.member.role}`}
                        onClick={(event) => event.stopPropagation()}
                        className="mt-1 block truncate text-xs text-violet-700 hover:underline"
                      >Team: {primaryMembership.team.name}{memberships.length > 1 ? ` (+${memberships.length - 1})` : ""}</Link>}
                    </td>
                    <td className="px-4 py-3 text-slate-600">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <span className="truncate">{formatInstanceType(instance.type)}</span>
                        <span
                          className={`inline-flex shrink-0 rounded-md border px-2 py-0.5 text-xs font-medium ${modeClass(
                            instance.instance_mode,
                          )}`}
                        >
                          {modeLabel(instance.instance_mode)}
                        </span>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-slate-600">
                      <div className="text-xs">ID: {instance.id}</div>
                      <div className="mt-1 text-xs text-slate-500">最后在线：{instance.last_online_at ? new Date(instance.last_online_at).toLocaleString(locale) : "暂无记录"}</div>
                      {instance.status === "error" && <div tabIndex={0}
                        className="group relative mt-1 text-xs text-red-600"
                      ><div className="truncate">错误：{runtimeFailureSummary(instance)}</div>
                        <div role="tooltip" className="absolute left-0 top-full z-50 hidden max-h-64 w-80 max-w-[75vw] overflow-auto whitespace-pre-wrap break-words rounded border border-red-100 bg-white p-3 text-slate-700 shadow-lg group-hover:block group-focus-within:block">{instance.runtime_error_message || "原因未记录"}</div>
                      </div>}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`inline-flex rounded-md border px-2 py-1 text-xs font-medium ${availabilityClass(
                          availability,
                        )}`}
                      >
                        {availabilityLabel(availability)}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-slate-600">
                      {formatBytes(instance.workspace_usage_bytes)}
                    </td>
                    <td
                      className="px-4 py-3"
                      onClick={(event) => event.stopPropagation()}
                      onKeyDown={(event) => event.stopPropagation()}
                    >
                      <div className="flex justify-end gap-2">
                        {instance.status === "running" ? (
                          <button
                            type="button"
                            onClick={() => void handleStop(instance.id)}
                            disabled={actionLoading === instance.id}
                            className="cm-icon-button"
                            title={t("common.stop")}
                          >
                            <Square className="h-4 w-4" />
                          </button>
                        ) : instance.status === "stopped" ? (
                          <button
                            type="button"
                            onClick={() => void handleStart(instance.id)}
                            disabled={actionLoading === instance.id}
                            className="cm-icon-button"
                            title={t("common.start")}
                          >
                            <Play className="h-4 w-4" />
                          </button>
                        ) : null}
                        <button
                          type="button"
                          onClick={() => setPendingDeleteId(instance.id)}
                          disabled={deletingIds.includes(instance.id) || instance.status === "deleting"}
                          className="cm-icon-button border-red-200 text-red-600 hover:border-red-300 hover:bg-red-50 hover:text-red-700"
                          title={t("common.delete")}
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {totalPages > 1 ? (
            <div className="flex items-center justify-end gap-2 border-t border-slate-200 px-4 py-3">
              <button
                type="button"
                onClick={() => setPage((current) => Math.max(1, current - 1))}
                disabled={page <= 1}
                className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {t("instances.previous")}
              </button>
              <button
                type="button"
                onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
                disabled={page >= totalPages}
                className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {t("instances.nextPage")}
              </button>
            </div>
          ) : null}
        </div>
      )}
    </UserLayout>
  );
};

export default InstanceListPage;
