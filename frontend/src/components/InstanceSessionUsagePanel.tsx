import React, { useCallback, useEffect, useMemo, useState } from "react";
import { BarChart3, ChevronDown, ChevronUp, Download, RefreshCw, Search } from "lucide-react";
import { useI18n } from "../contexts/I18nContext";
import InstanceCollapsiblePanel from "./InstanceCollapsiblePanel";
import { instanceService } from "../services/instanceService";
import type {
  InstanceSessionUsageDetail,
  InstanceSessionUsageItem,
  InstanceSessionUsageResult,
} from "../types/instance";
import {
  buildSessionUsageCsv,
  downloadSessionUsageCsv,
  resolveSessionUsageSince,
  type SessionUsageTimeRange,
} from "../utils/sessionUsageExport";

type Props = {
  instanceId: number;
  instanceType: string;
  onPanelExpandedChange?: (expanded: boolean) => void;
};

const PAGE_SIZE = 10;
const AUTO_REFRESH_MS = 15000;

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value);
}

function formatCost(value: number, currency: string) {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: currency || "USD",
    maximumFractionDigits: 4,
  }).format(value);
}

function formatDateTime(value: string | undefined, locale: string) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(locale);
}

export default function InstanceSessionUsagePanel({
  instanceId,
  instanceType,
  onPanelExpandedChange,
}: Props) {
  const { t, locale } = useI18n();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [page, setPage] = useState(1);
  const [timeRange, setTimeRange] = useState<SessionUsageTimeRange>("all");
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [result, setResult] = useState<InstanceSessionUsageResult | null>(null);
  const [expandedSessionId, setExpandedSessionId] = useState<string | null>(null);
  const [detail, setDetail] = useState<InstanceSessionUsageDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [panelExpanded, setPanelExpanded] = useState(false);

  const handlePanelExpandedChange = useCallback(
    (expanded: boolean) => {
      setPanelExpanded(expanded);
      onPanelExpandedChange?.(expanded);
    },
    [onPanelExpandedChange],
  );

  const supported = instanceType === "openclaw" || instanceType === "hermes";
  const since = useMemo(() => resolveSessionUsageSince(timeRange), [timeRange]);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [search]);

  const loadDetail = useCallback(
    async (sessionId: string, options?: { silent?: boolean }) => {
      if (!options?.silent) {
        setDetailLoading(true);
      }
      setDetailError("");
      try {
        const data = await instanceService.getInstanceSessionUsageDetail(instanceId, sessionId, {
          since,
        });
        setDetail(data);
      } catch (err) {
        setDetail(null);
        setDetailError(err instanceof Error ? err.message : t("instances.sessionUsage.detailLoadFailed"));
      } finally {
        if (!options?.silent) {
          setDetailLoading(false);
        }
      }
    },
    [instanceId, since, t],
  );

  const loadUsage = useCallback(async (options?: { silent?: boolean }) => {
    if (!supported) {
      return;
    }
    if (!options?.silent) {
      setLoading(true);
    }
    setError("");
    try {
      const data = await instanceService.getInstanceSessionUsage(instanceId, {
        page,
        limit: PAGE_SIZE,
        search: debouncedSearch || undefined,
        since,
      });
      setResult(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("instances.sessionUsage.loadFailed"));
    } finally {
      if (!options?.silent) {
        setLoading(false);
      }
    }
  }, [debouncedSearch, instanceId, page, since, supported, t]);

  useEffect(() => {
    void loadUsage();
  }, [loadUsage]);

  useEffect(() => {
    setPage(1);
  }, [debouncedSearch, timeRange]);

  useEffect(() => {
    setExpandedSessionId(null);
    setDetail(null);
    setDetailError("");
  }, [debouncedSearch, timeRange]);

  useEffect(() => {
    if (!autoRefresh || !supported || !panelExpanded) {
      return;
    }
    const timer = window.setInterval(() => {
      void (async () => {
        await loadUsage({ silent: true });
        if (expandedSessionId) {
          await loadDetail(expandedSessionId, { silent: true });
        }
      })();
    }, AUTO_REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [autoRefresh, expandedSessionId, loadDetail, loadUsage, panelExpanded, supported]);

  const totalPages = useMemo(() => {
    if (!result) {
      return 1;
    }
    return Math.max(1, Math.ceil(result.total / PAGE_SIZE));
  }, [result]);

  const exportCsv = async () => {
    setExporting(true);
    try {
      const items: InstanceSessionUsageItem[] = [];
      let pageCursor = 1;
      let total = 0;
      let currency = "USD";
      do {
        const batch = await instanceService.getInstanceSessionUsage(instanceId, {
          page: pageCursor,
          limit: 100,
          search: debouncedSearch || undefined,
          since,
        });
        items.push(...batch.items);
        total = batch.total;
        currency = batch.summary.currency || currency;
        pageCursor += 1;
      } while (items.length < total);

      const csv = buildSessionUsageCsv(items, currency);
      downloadSessionUsageCsv(csv, `instance-${instanceId}-session-usage.csv`);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("instances.sessionUsage.exportFailed"));
    } finally {
      setExporting(false);
    }
  };

  const toggleDetail = async (item: InstanceSessionUsageItem) => {
    if (expandedSessionId === item.session_id) {
      setExpandedSessionId(null);
      setDetail(null);
      setDetailError("");
      return;
    }
    setExpandedSessionId(item.session_id);
    setDetail(null);
    await loadDetail(item.session_id);
  };

  const sessionPanelSummary = result
    ? t("instances.sessionUsage.panelSummary", {
        sessions: formatNumber(result.summary.session_count),
        tokens: formatNumber(result.summary.total_tokens),
        cost: formatCost(result.summary.total_estimated_cost, result.summary.currency),
      })
    : loading
      ? t("instances.sessionUsage.loading")
      : t("instances.sessionUsage.empty");

  if (!supported) {
    return null;
  }

  return (
    <InstanceCollapsiblePanel
      storageKey={`clawmanager.instance-panel.session-usage.${instanceId}`}
      title={t("instances.sessionUsage.title")}
      icon={<BarChart3 className="h-4 w-4 text-indigo-600" />}
      summary={sessionPanelSummary}
      onExpandedChange={handlePanelExpandedChange}
      headerActions={
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[180px] flex-1 sm:max-w-xs">
            <Search className="pointer-events-none absolute left-2 top-2.5 h-4 w-4 text-slate-400" />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t("instances.sessionUsage.sessionKey")}
              className="w-full rounded-md border border-slate-200 py-2 pl-8 pr-3 text-sm"
            />
          </div>
          <select
            value={timeRange}
            onChange={(event) => setTimeRange(event.target.value as SessionUsageTimeRange)}
            className="rounded-md border border-slate-200 px-2 py-2 text-sm"
          >
            <option value="all">{t("instances.sessionUsage.timeRangeAll")}</option>
            <option value="24h">{t("instances.sessionUsage.timeRange24h")}</option>
            <option value="7d">{t("instances.sessionUsage.timeRange7d")}</option>
            <option value="30d">{t("instances.sessionUsage.timeRange30d")}</option>
          </select>
          <label className="inline-flex items-center gap-1 text-xs text-slate-600">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(event) => setAutoRefresh(event.target.checked)}
            />
            {t("instances.sessionUsage.autoRefresh")}
          </label>
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 py-2 text-xs text-slate-700"
            onClick={() => void loadUsage()}
          >
            <RefreshCw className="h-3.5 w-3.5" />
            {t("instances.sessionUsage.refresh")}
          </button>
          <button
            type="button"
            disabled={exporting}
            className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 py-2 text-xs text-slate-700 disabled:opacity-50"
            onClick={() => void exportCsv()}
          >
            <Download className="h-3.5 w-3.5" />
            {exporting ? t("instances.sessionUsage.exportCsvLoading") : t("instances.sessionUsage.exportCsv")}
          </button>
        </div>
      }
    >
      <p className="text-xs text-slate-500">{t("instances.sessionUsage.gatewayOnlyNotice")}</p>

      {result?.compliance.has_fallback_sessions && (
        <div className="mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
          {t("instances.sessionUsage.fallbackWarning")}
        </div>
      )}

      {result && (
        <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div className="rounded-md border border-slate-200 px-3 py-2">
            <div className="text-xs text-slate-500">{t("instances.sessionUsage.total")}</div>
            <div className="text-lg font-semibold text-slate-950">
              {formatNumber(result.summary.total_tokens)}
            </div>
          </div>
          <div className="rounded-md border border-slate-200 px-3 py-2">
            <div className="text-xs text-slate-500">{t("instances.sessionUsage.cost")}</div>
            <div className="text-lg font-semibold text-slate-950">
              {formatCost(result.summary.total_estimated_cost, result.summary.currency)}
            </div>
          </div>
          <div className="rounded-md border border-slate-200 px-3 py-2">
            <div className="text-xs text-slate-500">{t("instances.sessionUsage.sessions")}</div>
            <div className="text-lg font-semibold text-slate-950">
              {formatNumber(result.summary.session_count)}
            </div>
          </div>
          <div className="rounded-md border border-slate-200 px-3 py-2">
            <div className="text-xs text-slate-500">{t("instances.sessionUsage.summary")}</div>
            <div className="text-sm font-medium text-slate-900">
              {formatNumber(result.summary.total_prompt_tokens)} /{" "}
              {formatNumber(result.summary.total_completion_tokens)}
            </div>
          </div>
        </div>
      )}

      {error && (
        <div className="mb-3 flex items-center justify-between gap-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" className="text-xs font-medium underline" onClick={() => void loadUsage()}>
            {t("instances.sessionUsage.retry")}
          </button>
        </div>
      )}

      {loading ? (
        <div className="rounded-md border border-dashed border-slate-200 px-3 py-8 text-center text-sm text-slate-500">
          {t("instances.sessionUsage.loading")}
        </div>
      ) : !result || result.items.length === 0 ? (
        <div className="rounded-md border border-dashed border-slate-200 px-3 py-8 text-center text-sm text-slate-500">
          {t("instances.sessionUsage.empty")}
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="min-w-full text-left text-sm">
            <thead>
              <tr className="border-b border-slate-200 text-xs uppercase tracking-wide text-slate-500">
                <th className="px-2 py-2">{t("instances.sessionUsage.sessionKey")}</th>
                <th className="px-2 py-2">{t("instances.sessionUsage.prompt")}</th>
                <th className="px-2 py-2">{t("instances.sessionUsage.completion")}</th>
                <th className="px-2 py-2">{t("instances.sessionUsage.total")}</th>
                <th className="px-2 py-2">{t("instances.sessionUsage.cost")}</th>
                <th className="px-2 py-2">{t("instances.sessionUsage.invocations")}</th>
                <th className="px-2 py-2">{t("instances.sessionUsage.lastActive")}</th>
                <th className="px-2 py-2" />
              </tr>
            </thead>
            <tbody>
              {result.items.map((item) => {
                const expanded = expandedSessionId === item.session_id;
                return (
                  <React.Fragment key={item.session_id}>
                    <tr className="border-b border-slate-100">
                      <td className="px-2 py-2">
                        <div className="font-medium text-slate-900">{item.session_key}</div>
                        {item.title && <div className="text-xs text-slate-500">{item.title}</div>}
                      </td>
                      <td className="px-2 py-2">{formatNumber(item.prompt_tokens)}</td>
                      <td className="px-2 py-2">{formatNumber(item.completion_tokens)}</td>
                      <td className="px-2 py-2">{formatNumber(item.total_tokens)}</td>
                      <td className="px-2 py-2">{formatCost(item.estimated_cost, item.currency)}</td>
                      <td className="px-2 py-2">{formatNumber(item.invocation_count)}</td>
                      <td className="px-2 py-2">{formatDateTime(item.last_seen_at, locale)}</td>
                      <td className="px-2 py-2">
                        <button
                          type="button"
                          className="inline-flex items-center gap-1 text-xs text-indigo-600"
                          onClick={() => void toggleDetail(item)}
                        >
                          {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                          {t("instances.sessionUsage.detail")}
                        </button>
                      </td>
                    </tr>
                    {expanded && (
                      <tr className="bg-slate-50">
                        <td colSpan={8} className="px-3 py-3">
                          {detailLoading ? (
                            <div className="text-xs text-slate-500">{t("instances.sessionUsage.detailLoading")}</div>
                          ) : detail ? (
                            <div className="grid gap-4 lg:grid-cols-2">
                              <div>
                                <div className="mb-2 text-xs font-semibold uppercase text-slate-500">
                                  {t("instances.sessionUsage.modelBreakdown")}
                                </div>
                                <div className="space-y-1">
                                  {detail.model_breakdown.map((row) => (
                                    <div
                                      key={row.label}
                                      className="flex items-center justify-between rounded border border-slate-200 bg-white px-2 py-1 text-xs"
                                    >
                                      <span>{row.label}</span>
                                      <span>
                                        {formatNumber(row.total_tokens)} tokens ·{" "}
                                        {formatCost(row.estimated_cost, detail.currency)}
                                      </span>
                                    </div>
                                  ))}
                                </div>
                              </div>
                              <div>
                                <div className="mb-2 text-xs font-semibold uppercase text-slate-500">
                                  {t("instances.sessionUsage.recentTraces")}
                                </div>
                                <div className="space-y-1">
                                  {detail.recent_traces.map((trace) => (
                                    <div
                                      key={`${trace.trace_id}-${trace.created_at}`}
                                      className="rounded border border-slate-200 bg-white px-2 py-1 text-xs"
                                    >
                                      <div className="font-medium text-slate-900">{trace.trace_id}</div>
                                      <div className="text-slate-500">
                                        {trace.requested_model} · {trace.status} ·{" "}
                                        {formatNumber(trace.total_tokens)} tokens
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            </div>
                          ) : (
                            <div className="text-xs text-red-600">
                              {detailError || t("instances.sessionUsage.detailEmpty")}
                            </div>
                          )}
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {result && result.total > PAGE_SIZE && (
        <div className="mt-3 flex items-center justify-between text-xs text-slate-500">
          <span>
            {t("instances.sessionUsage.pageSummary", { page, totalPages })}
          </span>
          <div className="flex gap-2">
            <button
              type="button"
              disabled={page <= 1}
              className="rounded border border-slate-200 px-2 py-1 disabled:opacity-40"
              onClick={() => setPage((current) => Math.max(1, current - 1))}
            >
              {t("instances.sessionUsage.prev")}
            </button>
            <button
              type="button"
              disabled={page >= totalPages}
              className="rounded border border-slate-200 px-2 py-1 disabled:opacity-40"
              onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
            >
              {t("instances.sessionUsage.next")}
            </button>
          </div>
        </div>
      )}
    </InstanceCollapsiblePanel>
  );
}
