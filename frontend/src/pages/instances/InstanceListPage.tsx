import { formatWorkspaceUsage, workspaceUsageText } from "../../lib/workspaceUsage";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useLocation } from "react-router-dom";
import { Monitor, Play, Plus, RotateCw, Square, Trash2 } from "lucide-react";
import { useAuthStore } from "../../stores/authStore";
import ConfirmDialog from "../../components/ConfirmDialog";
import InstanceLifecycleStatus from "../../components/InstanceLifecycleStatus";
import InstanceAutoStopInfo from "../../components/InstanceAutoStopInfo";
import ActionMenu from "../../components/ActionMenu";
import ExpandableSearch from "../../components/ExpandableSearch";
import FilterSelect from "../../components/FilterSelect";
import { runtimeFailureSummary } from "../../lib/runtimeFailure";
import InstanceLifecycleBatches from "../../components/InstanceLifecycleBatches";
import { collectUnavailableLiteIds, pruneVisibleSelection } from "../../lib/unavailableInstanceSelection";
import UserLayout from "../../components/UserLayout";
import { useI18n } from "../../contexts/I18nContext";
import { instanceService } from "../../services/instanceService";

import { teamService } from "../../services/teamService";
import { formatInstanceType, type Instance, type BatchDeleteLiteInstancesResponse } from "../../types/instance";
import type { Team, TeamMember } from "../../types/team";
import {
  INSTANCE_STATUS_FILTERS,
  INSTANCE_TYPE_FILTERS,
  instanceStatusFilterQuery,
  type InstanceStatusFilter,
} from "../../lib/instanceListFilters";

type ModeFilter = "all" | Instance["instance_mode"];
type TeamMembership = {
  team: Team;
  member: TeamMember;
};
type LoadInstancesOptions = {
  refreshTeams?: boolean;
};

const INSTANCE_LIST_PAGE_SIZE = 100;
const TEAM_LIST_PAGE_SIZE = 100;

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

function modeLabel(mode: Instance["instance_mode"]) {
  return mode === "pro" ? "Pro" : "Lite";
}

function isLiteInstance(instance: Instance) {
  if (instance.instance_mode === "lite" || instance.instance_mode === "pro") {
    return instance.instance_mode === "lite";
  }
  return instance.runtime_type === "gateway";
}


function getErrorMessage(err: unknown, fallback: string) {
  const responseError = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
  if (responseError) {
    return responseError;
  }
  return err instanceof Error ? err.message : fallback;
}

interface InstanceListPageProps {
  embedded?: boolean;
  initialSearch?: string;
  onSelectInstance?: (instance: Instance) => void;
}

const InstanceListPage: React.FC<InstanceListPageProps> = ({ embedded = false, initialSearch, onSelectInstance }) => {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const initialQuery = new URLSearchParams(initialSearch ?? (embedded ? "" : location.search));
  const detailState = { returnTo: location.pathname + location.search };
  const selectInstance = (instance: Instance) => {
    if (onSelectInstance) onSelectInstance(instance);
    else navigate(`/instances/${instance.id}`, { state: detailState });
  };
  const userId = useAuthStore((auth) => auth.isAuthenticated ? auth.user?.id : undefined);
  const [teamMemberships, setTeamMemberships] = useState<Map<number, TeamMembership[]>>(
    new Map(),
  );
  const [deletingIds, setDeletingIds] = useState<number[]>([]);
  const [actionLoadingIds, setActionLoadingIds] = useState<number[]>([]);
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null);
  const [statusFilter, setStatusFilter] = useState<InstanceStatusFilter>(() => {
    const status = INSTANCE_STATUS_FILTERS.find((value) => value === initialQuery.get("status"));
    if (status) return status;
    const availability = initialQuery.get("availability");
    return availability === "available" ? "running" : availability === "starting" ? "creating" : "all";
  });
  const [typeFilter, setTypeFilter] = useState<string>(
    () => INSTANCE_TYPE_FILTERS.find((value) => value === initialQuery.get("type")) ?? "all",
  );
  const [modeFilter, setModeFilter] = useState<ModeFilter>(() => {
    const mode = initialQuery.get("mode");
    return mode === "lite" || mode === "pro" ? mode : "all";
  });
  const filterDetails = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const media = window.matchMedia("(max-width: 1100px)");
    const sync = () => { if (filterDetails.current) filterDetails.current.open = !media.matches; };
    sync();
    const outside = (event: PointerEvent) => {
      if (media.matches && filterDetails.current && !filterDetails.current.contains(event.target as Node)) filterDetails.current.open = false;
    };
    media.addEventListener("change", sync);
    document.addEventListener("pointerdown", outside);
    return () => {media.removeEventListener("change", sync); document.removeEventListener("pointerdown", outside);};
  }, []);
  const [searchQuery, setSearchQuery] = useState(initialQuery.get("q") || "");
  const [debouncedSearchQuery, setDebouncedSearchQuery] = useState(initialQuery.get("q") || "");
  const [page, setPage] = useState(() => Math.max(1, Number(initialQuery.get("page")) || 1));
  const [selectedLiteIds, setSelectedLiteIds] = useState<number[]>([]);
  const [allUnavailableSelected, setAllUnavailableSelected] = useState(false);
  const [selectingUnavailable, setSelectingUnavailable] = useState(false);
  const [unavailableSelectionNotice, setUnavailableSelectionNotice] = useState("");
  const selectionRequest = useRef(0);
  useEffect(() => () => { selectionRequest.current++; }, []);
  const [batchDeleteLoading, setBatchDeleteLoading] = useState(false);
  const [batchDeleteResult, setBatchDeleteResult] = useState<BatchDeleteLiteInstancesResponse | null>(null);
  const [pendingBatchDelete, setPendingBatchDelete] = useState(false);

  const refreshTeamMemberships = useCallback(async () => {
    try {
      const memberships = await loadTeamMemberships();
      setTeamMemberships(memberships);
    } catch (teamError) {
      console.error("Failed to load team memberships", teamError);
    }
  }, []);

  const filters = {
    query: debouncedSearchQuery.trim() || undefined,
    type: typeFilter === "all" ? undefined : typeFilter,
    instance_mode: modeFilter === "all" ? undefined : modeFilter,
    ...instanceStatusFilterQuery(statusFilter),
  };
  const query = useQuery({
    queryKey: ["instances", "list", userId, page, filters],
    queryFn: ({ signal }) => instanceService.getInstances(page, INSTANCE_LIST_PAGE_SIZE, filters, signal),
    enabled: !!userId,
    // Keep rows during a filter change, but never inherit another user's cache.
    placeholderData: (previous, previousQuery) => previousQuery?.queryKey[2] === userId ? previous : undefined,
    refetchInterval: 5000,
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
    retry: 1,
  });
  const instances = useMemo(() => sortInstances(query.data?.instances ?? []), [query.data]);
  const total = query.data?.total ?? 0;
  const loading = query.isPending;
  const error = query.isError && !query.data ? getErrorMessage(query.error, t("instances.failedToLoad")) : null;
  const { refetch } = query;
  const loadInstances = useCallback(async (options?: LoadInstancesOptions) => {
    await refetch();
    if (options?.refreshTeams !== false) void refreshTeamMemberships();
  }, [refetch, refreshTeamMemberships]);
  useEffect(() => { if (userId) void refreshTeamMemberships(); }, [userId, refreshTeamMemberships]);
  useEffect(() => {
    if (!query.data || query.isPlaceholderData) return;
    const lastPage = Math.max(1, Math.ceil(total / INSTANCE_LIST_PAGE_SIZE));
    if (page > lastPage) setPage(lastPage);
  }, [query.data, query.isPlaceholderData, total, page]);
  useEffect(() => {
    if (searchQuery === debouncedSearchQuery) return;
    const timeoutId = window.setTimeout(() => {
      setPage(1);
      setDebouncedSearchQuery(searchQuery);
    }, 250);
    return () => window.clearTimeout(timeoutId);
  }, [searchQuery, debouncedSearchQuery]);
  useEffect(() => {
    if (embedded) return;
    const params = new URLSearchParams();
    if (searchQuery) params.set("q", searchQuery);
    if (typeFilter !== "all") params.set("type", typeFilter);
    if (modeFilter !== "all") params.set("mode", modeFilter);
    if (statusFilter !== "all") params.set("status", statusFilter);
    if (page > 1) params.set("page", String(page));
    const search = params.toString();
    if (location.search.replace(/^\?/, "") !== search) {
      navigate({ pathname: location.pathname, search }, { replace: true });
    }
  }, [embedded, searchQuery, typeFilter, modeFilter, statusFilter, page, navigate, location.pathname, location.search]);


  const filteredInstances = instances;
  const totalPages = Math.max(1, Math.ceil(total / INSTANCE_LIST_PAGE_SIZE));
  const resultFrom = total === 0 ? 0 : (page - 1) * INSTANCE_LIST_PAGE_SIZE + 1;
  const resultTo = total === 0 ? 0 : Math.min(page * INSTANCE_LIST_PAGE_SIZE, total);
  const filtersActive =
    searchQuery.trim() !== "" ||
    typeFilter !== "all" ||
    modeFilter !== "all" ||
    statusFilter !== "all";

  const clearFilters = useCallback(() => {
    setSearchQuery("");
    setDebouncedSearchQuery("");
    setTypeFilter("all");
    setModeFilter("all");
    setStatusFilter("all");
    setPage(1);
  }, []);

  const selectableLiteIds = useMemo(
    () =>
      filteredInstances
        .filter((instance) => isLiteInstance(instance) && instance.status !== "deleting")
        .map((instance) => instance.id),
    [filteredInstances],
  );

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
      setUnavailableSelectionNotice(ids.length ? `${t("instances.unavailableSelected", {count: ids.length})}${ids.length > 500 ? t("instances.selectionLimit") : ""}` : t("instances.noUnavailable"));
    } catch (err) {
      if (request === selectionRequest.current) setUnavailableSelectionNotice(getErrorMessage(err, t("instances.selectionFailed")));
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

  const handleBatchDeleteLite = useCallback(async () => {
    try {
      setBatchDeleteLoading(true);
      setBatchDeleteResult(null);
      const result = await instanceService.batchDeleteLiteInstances(selectedLiteIds);
      setBatchDeleteResult(result);
      setSelectedLiteIds(result.results.filter(item => item.status === "failed").map(item => item.instance_id));
      setPendingBatchDelete(false);
      await loadInstances();
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
        await loadInstances();
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
        setActionLoadingIds((ids) => ids.includes(id) ? ids : [...ids, id]);
        await instanceService.startInstance(id);
        await loadInstances({ refreshTeams: false });
      } catch (err: unknown) {
        alert(getErrorMessage(err, t("instances.failedToStart")));
      } finally {
        setActionLoadingIds((ids) => ids.filter((pendingId) => pendingId !== id));
      }
    },
    [loadInstances, t],
  );

  const handleRestart = useCallback(
    async (id: number) => {
      try {
        setActionLoadingIds((ids) => ids.includes(id) ? ids : [...ids, id]);
        await instanceService.restartInstance(id);
        await loadInstances({ refreshTeams: false });
      } catch (err: unknown) {
        alert(getErrorMessage(err, t("instances.failedToRestart")));
      } finally {
        setActionLoadingIds((ids) => ids.filter((pendingId) => pendingId !== id));
      }
    },
    [loadInstances, t],
  );

  const handleStop = useCallback(
    async (id: number) => {
      try {
        setActionLoadingIds((ids) => ids.includes(id) ? ids : [...ids, id]);
        await instanceService.stopInstance(id);
        await loadInstances({ refreshTeams: false });
      } catch (err: unknown) {
        alert(getErrorMessage(err, t("instances.failedToStop")));
      } finally {
        setActionLoadingIds((ids) => ids.filter((pendingId) => pendingId !== id));
      }
    },
    [loadInstances, t],
  );

  const content = (
    <>
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

      <InstanceLifecycleBatches selectedIds={selectedLiteIds} selectionBusy={selectingUnavailable} onChanged={() => void loadInstances()} selectionControl={<>
        <label className="flex items-center gap-2 text-sm text-slate-600" title={t("instances.unavailableSelectionHint")}>
          <input type="checkbox" checked={allUnavailableSelected || selectingUnavailable} onChange={e => void selectAllUnavailable(e.target.checked)} />
          {t(selectingUnavailable ? "instances.selectingUnavailable" : "instances.selectUnavailable")}
        </label>
        {unavailableSelectionNotice && <span role="status" className="text-xs text-slate-500">{unavailableSelectionNotice}</span>}
      </>} />
      <div className="cm-surface cm-instance-panel">
      <div className="cm-management-toolbar cm-instance-toolbar">
        <div className="cm-management-toolbar-filters">
          <ExpandableSearch value={searchQuery} onChange={setSearchQuery} label={t("instances.searchLabel")} placeholder={`ID / ${t("instances.searchPlaceholder")}`} />
          <details ref={filterDetails} className="cm-instance-filters" open onKeyDown={event => {
            if (event.key === "Escape" && window.matchMedia("(max-width: 1100px)").matches) {
              event.preventDefault(); event.stopPropagation(); event.currentTarget.open = false; event.currentTarget.querySelector("summary")?.focus();
            }
          }}>
            <summary className="app-button-secondary">{t("instances.filters")}</summary>
            <div className="cm-instance-filter-fields">
              <FilterSelect label={t("instances.instanceTypeFilter")} value={typeFilter} options={[{value: "all", label: t("instances.allTypes")}, ...INSTANCE_TYPE_FILTERS.map(type => ({value: type, label: formatInstanceType(type)}))]} onChange={value => {setTypeFilter(value); setPage(1);}} />
              <FilterSelect label={t("instances.instanceModeFilter")} value={modeFilter} options={[{value: "all", label: t("instances.allModes")}, {value: "lite", label: "Lite"}, {value: "pro", label: "Pro"}]} onChange={value => {setModeFilter(value as ModeFilter); setPage(1);}} />
              <FilterSelect label={t("instances.statusFilter")} value={statusFilter} options={[{value: "all", label: t("instances.allStatuses")}, ...INSTANCE_STATUS_FILTERS.map(status => ({value: status, label: t(`status.${status}`)}))]} onChange={value => {setStatusFilter(value as InstanceStatusFilter); setPage(1);}} />
              {filtersActive && <button type="button" onClick={clearFilters} className="app-button-secondary">{t("instances.clearFilters")}</button>}
            </div>
          </details>
        </div>
        <div className="cm-management-toolbar-actions">
          <Link to="/instances/new?batch=1" className="app-button-secondary"><Plus size={14} />{t("instances.batchCreateLite")}</Link>
          <Link to="/instances/new" className="app-button-primary"><Monitor size={14} />{t("instances.createInstance")}</Link>
        </div>
      </div>
      {selectedLiteCount > 0 && <div className="cm-instance-selection"><button type="button" onClick={() => setPendingBatchDelete(true)} disabled={batchDeleteLoading || selectingUnavailable} className="app-button-secondary cm-instance-danger"><Trash2 size={14} />{t("instances.batchDeleteLiteWithCount", {count: selectedLiteCount})}</button></div>}
      {batchDeleteResult && batchDeleteResult.failed > 0 && <details open role="alert" className="rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
        <summary>{t("instances.batchDeletePartial", { deleted: batchDeleteResult.deleted, failed: batchDeleteResult.failed })}</summary>
        <ul className="mt-2 max-h-48 overflow-y-auto break-words">
          {batchDeleteResult.results.filter(item => item.status === "failed").map(item => <li key={item.instance_id}>{item.name || `#${item.instance_id}`}: {item.error || t("instances.batchDeleteFailed")}</li>)}
        </ul>
      </details>}
      {query.isRefetchError && query.data && (
        <div role="status" className="rounded-md border border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-800">
          {t("instances.failedToLoad")}
        </div>
      )}


      {loading ? (
        <div className="flex h-64 items-center justify-center text-sm text-slate-500">
          {t("common.loading")}
        </div>
      ) : error ? (
        <div role="alert" className="px-4 py-3 text-sm text-red-700">
          {error} <button type="button" className="app-button-secondary" onClick={() => void refetch()}>{t("common.retry")}</button>
        </div>
      ) : instances.length === 0 && !filtersActive ? (
        <div className="p-10 text-center">
          <h3 className="text-sm font-medium text-slate-950">{t("instances.noInstances")}</h3>
          <div className="mt-5">
            <Link to="/instances/new" className="app-button-primary">
              {t("instances.createInstance")}
            </Link>
          </div>
        </div>
      ) : filteredInstances.length === 0 ? (
        <div className="p-10 text-center text-sm text-slate-500">
          {t("instances.noMatchingInstances")}
        </div>
      ) : (
        <div className="cm-management-table-wrap" tabIndex={0} aria-label={t("instances.listTitle")}>
          <table className="cm-management-table cm-instance-table">
            <thead className="text-left">
              <tr>
                <th className="w-[5%] px-4 py-3">
                  <input
                    type="checkbox"
                    checked={allVisibleLiteSelected}
                    disabled={selectableLiteIds.length === 0}
                    onChange={toggleAllVisibleLite}
                    className="h-4 w-4 rounded border-slate-300  disabled:opacity-40"
                    aria-label={t("instances.selectVisibleLite")}
                  />
                </th>
                <th style={{width: "23%"}}>{t("instances.nameAndId")}</th>
                <th style={{width: "16%"}}>{t("instances.type")}</th>
                <th style={{width: "6%"}}>{t("instances.modeLabel")}</th>
                <th style={{width: "19%"}}>{t("common.status")}</th>
                <th style={{width: "9%"}}>{t("instances.workspaceLabel")}</th>
                <th style={{width: "13%"}}>{t("instances.lastOnline")}</th>
                <th style={{width: "9%"}}>{t("instances.actionsLabel")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 bg-white">
              {filteredInstances.map((instance) => {
                const memberships = teamMemberships.get(instance.id) || [];
                const primaryMembership = memberships[0];
                const canSelectLite = isLiteInstance(instance) && instance.status !== "deleting";
                const selected = selectedLiteSet.has(instance.id);
                return (
                  <tr
                    key={instance.id}
                    tabIndex={0}
                    aria-selected={selected}
                    onClick={() => selectInstance(instance)}
                    onKeyDown={(event) => {
                      if (event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) {
                        event.preventDefault();
                        selectInstance(instance);
                      }
                    }}
                    className={`cursor-pointer cm-instance-row ${
                      primaryMembership ? "hover:bg-slate-50" : "hover:bg-slate-50"
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
                        className="h-4 w-4 rounded border-slate-300  disabled:opacity-30"
                        aria-label={t("instances.selectLiteInstance", { name: instance.name })}
                      />
                    </td>
                    <td>
                      <div className="cm-instance-identity">
                        <Link to={`/instances/${instance.id}`} state={detailState} onClick={event => { event.stopPropagation(); if (onSelectInstance) { event.preventDefault(); onSelectInstance(instance); } }} className="cm-management-identity cm-management-truncate" title={instance.name}>{instance.name}</Link>
                        <span className="cm-instance-id">#{instance.id}</span>
                        {primaryMembership && <Link to={`/teams/${primaryMembership.team.id}`} onClick={event => event.stopPropagation()} className="cm-management-truncate cm-instance-team" title={`${primaryMembership.team.name} / ${primaryMembership.member.display_name || primaryMembership.member.member_key} / ${primaryMembership.member.role}`}>{primaryMembership.team.name}{memberships.length > 1 ? ` (+${memberships.length - 1})` : ""}</Link>}
                      </div>
                    </td>
                    <td>{formatInstanceType(instance.type)}</td>
                    <td>{modeLabel(instance.instance_mode)}</td>
                    <td><div className="cm-instance-state">
                      <InstanceLifecycleStatus layout="inline" status={instance.status} availability={instance.availability} stoppedReason={instance.stopped_reason} />
                      {instance.status === "error" && <>
                        <button type="button" className="cm-instance-error cm-management-truncate" popoverTarget={`instance-error-${instance.id}`} aria-label={`${t("instances.errorDetails")}: ${instance.name}`} onClick={event => event.stopPropagation()}>{runtimeFailureSummary(instance)}</button>
                        <div id={`instance-error-${instance.id}`} popover="auto" role="dialog" aria-label={`${t("instances.errorDetails")}: ${instance.name}`} className="cm-management-popup cm-instance-error-popup" tabIndex={0} onToggle={event => {if (event.currentTarget.matches(":popover-open")) event.currentTarget.focus();}} onClick={event => event.stopPropagation()}>{instance.runtime_error_message || t("instances.noErrorReason")}</div>
                      </>}

                    </div></td>
                    <td className="cm-management-number" title={workspaceUsageText(locale, instance).description} aria-label={workspaceUsageText(locale, instance).description}>
                      {formatWorkspaceUsage(instance, workspaceUsageText(locale, instance).unavailable)}
                    </td>
                    <td><span className="cm-management-truncate block" title={instance.last_online_at ? new Date(instance.last_online_at).toLocaleString(locale) : undefined}>{instance.last_online_at ? new Date(instance.last_online_at).toLocaleString(locale) : t("instances.noOnlineRecord")}</span></td>
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
                            disabled={actionLoadingIds.includes(instance.id)}
                            className="cm-icon-button"
                            title={t("common.stop")}
                          >
                            <Square className="h-4 w-4" />
                          </button>
                        ) : instance.status === "stopped" ? (
                          <button
                            type="button"
                            onClick={() => void handleStart(instance.id)}
                            disabled={actionLoadingIds.includes(instance.id)}
                            className="cm-icon-button"
                            title={t("common.start")}
                          >
                            <Play className="h-4 w-4" />
                          </button>
                        ) : instance.status === "error" ? (
                          <button
                            type="button"
                            onClick={() => void handleRestart(instance.id)}
                            disabled={actionLoadingIds.includes(instance.id) || deletingIds.includes(instance.id)}
                            className="cm-icon-button"
                            title={t("common.restart")}
                            aria-label={`${t("common.restart")} ${instance.name}`}
                            aria-busy={actionLoadingIds.includes(instance.id)}
                          >
                            <RotateCw className={`h-4 w-4${actionLoadingIds.includes(instance.id) ? " animate-spin" : ""}`} />
                          </button>
                        ) : null}
                        <ActionMenu label={`${t("instances.actionsLabel")}: ${instance.name}`} items={[...(isLiteInstance(instance) ? [{id: "activity", label: t("instances.activityPolicy"), onSelect: () => {const dialog = document.getElementById(`instance-activity-${instance.id}`); if (dialog instanceof HTMLDialogElement) dialog.showModal();}}] : []), {id: "delete", label: t("common.delete"), destructive: true, disabled: deletingIds.includes(instance.id) || instance.status === "deleting" || actionLoadingIds.includes(instance.id), onSelect: () => setPendingDeleteId(instance.id)}]} />
                      </div>
                      {isLiteInstance(instance) && <dialog id={`instance-activity-${instance.id}`} aria-label={`${t("instances.activityPolicy")}: ${instance.name}`} className="cm-confirm-dialog cm-instance-activity-dialog">
                        <div className="flex items-center justify-between gap-3"><h2>{t("instances.activityPolicy")}</h2><button type="button" className="app-button-secondary" autoFocus onClick={event => event.currentTarget.closest("dialog")?.close()}>{t("common.close")}</button></div>
                        <InstanceAutoStopInfo instance={instance} />
                      </dialog>}
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
      {!loading && !error && total > 0 ? (
        <div className="cm-management-pagination">
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
      </div>
    </>
  );
  return embedded ? content : <UserLayout title={t("instances.listTitle")}>{content}</UserLayout>;
};

export default InstanceListPage;
