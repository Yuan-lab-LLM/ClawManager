import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import AdminLayout from "../../components/AdminLayout";
import { useI18n } from "../../contexts/I18nContext";
import {
  adminService,
  type SessionUsageOverview,
  type SessionUsageOverviewItem,
} from "../../services/adminService";
import {
  downloadSessionUsageCsv,
  resolveSessionUsageSince,
  type SessionUsageTimeRange,
} from "../../utils/sessionUsageExport";

const PAGE_SIZE = 20;
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

const SessionUsageOverviewPage: React.FC = () => {
  const { t } = useI18n();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [page, setPage] = useState(1);
  const [timeRange, setTimeRange] = useState<SessionUsageTimeRange>("all");
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [overview, setOverview] = useState<SessionUsageOverview | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [search]);

  const since = useMemo(() => resolveSessionUsageSince(timeRange), [timeRange]);

  const loadOverview = useCallback(async (options?: { silent?: boolean }) => {
    if (!options?.silent) {
      setLoading(true);
    }
    setError("");
    try {
      const data = await adminService.getSessionUsageOverview({
        page,
        limit: PAGE_SIZE,
        search: debouncedSearch || undefined,
        since,
      });
      setOverview(data);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("sessionUsagePage.loadFailed"));
    } finally {
      if (!options?.silent) {
        setLoading(false);
      }
    }
  }, [debouncedSearch, page, since, t]);

  useEffect(() => {
    void loadOverview();
  }, [loadOverview]);

  useEffect(() => {
    setPage(1);
  }, [debouncedSearch, timeRange]);

  useEffect(() => {
    if (!autoRefresh) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadOverview({ silent: true });
    }, AUTO_REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [autoRefresh, loadOverview]);

  const totalPages = useMemo(() => {
    if (!overview) {
      return 1;
    }
    return Math.max(1, Math.ceil(overview.total / PAGE_SIZE));
  }, [overview]);

  const exportCsv = async () => {
    setExporting(true);
    try {
      const rows: SessionUsageOverviewItem[] = [];
      let pageCursor = 1;
      let total = 0;
      do {
        const batch = await adminService.getSessionUsageOverview({
          page: pageCursor,
          limit: 100,
          search: debouncedSearch || undefined,
          since,
        });
        rows.push(...batch.items);
        total = batch.total;
        pageCursor += 1;
      } while (rows.length < total);

      const header = [
        "instance_id",
        "instance_name",
        "instance_type",
        "user_id",
        "session_count",
        "total_tokens",
        "estimated_cost",
        "currency",
        "fallback_sessions",
      ];
      const csvRows = rows.map((item) =>
        [
          item.instance_id,
          item.instance_name,
          item.instance_type,
          item.user_id,
          item.summary.session_count,
          item.summary.total_tokens,
          item.summary.total_estimated_cost,
          item.summary.currency,
          item.compliance.fallback_session_count,
        ]
          .map((cell) => {
            const value = String(cell ?? "");
            return /[",\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value;
          })
          .join(","),
      );
      downloadSessionUsageCsv([header.join(","), ...csvRows].join("\n"), "session-usage-overview.csv");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("sessionUsagePage.exportFailed"));
    } finally {
      setExporting(false);
    }
  };

  return (
    <AdminLayout title={t("nav.sessionUsage")}>
      <div className="space-y-5">
        <section className="app-panel px-5 py-4">
          <p className="text-sm text-[#6f625b]">{t("sessionUsagePage.subtitle")}</p>
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t("sessionUsagePage.searchPlaceholder")}
              className="min-w-[220px] flex-1 rounded-md border border-[#ead8cf] px-3 py-2 text-sm"
            />
            <select
              value={timeRange}
              onChange={(event) => setTimeRange(event.target.value as SessionUsageTimeRange)}
              className="rounded-md border border-[#ead8cf] px-3 py-2 text-sm"
            >
              <option value="all">{t("instances.sessionUsage.timeRangeAll")}</option>
              <option value="24h">{t("instances.sessionUsage.timeRange24h")}</option>
              <option value="7d">{t("instances.sessionUsage.timeRange7d")}</option>
              <option value="30d">{t("instances.sessionUsage.timeRange30d")}</option>
            </select>
            <label className="inline-flex items-center gap-2 text-sm text-[#6f625b]">
              <input
                type="checkbox"
                checked={autoRefresh}
                onChange={(event) => setAutoRefresh(event.target.checked)}
              />
              {t("instances.sessionUsage.autoRefresh")}
            </label>
            <button
              type="button"
              className="rounded-md border border-[#ead8cf] px-3 py-2 text-sm"
              onClick={() => void loadOverview()}
            >
              {t("instances.sessionUsage.refresh")}
            </button>
            <button
              type="button"
              disabled={exporting}
              className="rounded-md border border-[#ead8cf] px-3 py-2 text-sm disabled:opacity-50"
              onClick={() => void exportCsv()}
            >
              {exporting ? t("instances.sessionUsage.exportCsvLoading") : t("instances.sessionUsage.exportCsv")}
            </button>
          </div>
        </section>

        {error && (
          <section className="app-panel px-5 py-4">
            <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-red-700">{error}</div>
          </section>
        )}

        {loading || !overview ? (
          <section className="app-panel px-6 py-12 text-sm text-[#8f8681]">
            {t("sessionUsagePage.loading")}
          </section>
        ) : (
          <>
            <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <MetricCard label={t("sessionUsagePage.totalTokens")} value={formatNumber(overview.summary.total_tokens)} />
              <MetricCard
                label={t("sessionUsagePage.estimatedCost")}
                value={formatCost(overview.summary.total_estimated_cost, overview.summary.currency)}
              />
              <MetricCard label={t("sessionUsagePage.sessions")} value={formatNumber(overview.summary.session_count)} />
              <MetricCard label={t("sessionUsagePage.instances")} value={formatNumber(overview.total)} />
            </section>

            <section className="app-panel overflow-x-auto px-4 py-4">
              {overview.items.length === 0 ? (
                <div className="py-10 text-center text-sm text-[#8f8681]">{t("sessionUsagePage.empty")}</div>
              ) : (
                <table className="min-w-full text-left text-sm">
                  <thead>
                    <tr className="border-b border-[#ead8cf] text-xs uppercase tracking-wide text-[#8f8681]">
                      <th className="px-2 py-2">{t("sessionUsagePage.instance")}</th>
                      <th className="px-2 py-2">{t("sessionUsagePage.user")}</th>
                      <th className="px-2 py-2">{t("sessionUsagePage.sessions")}</th>
                      <th className="px-2 py-2">{t("sessionUsagePage.totalTokens")}</th>
                      <th className="px-2 py-2">{t("sessionUsagePage.estimatedCost")}</th>
                      <th className="px-2 py-2">{t("sessionUsagePage.fallback")}</th>
                      <th className="px-2 py-2" />
                    </tr>
                  </thead>
                  <tbody>
                    {overview.items.map((item) => (
                      <tr key={item.instance_id} className="border-b border-[#f3ebe4]">
                        <td className="px-2 py-2">
                          <div className="font-medium text-[#171212]">{item.instance_name}</div>
                          <div className="text-xs text-[#8f8681]">
                            #{item.instance_id} · {item.instance_type}
                          </div>
                        </td>
                        <td className="px-2 py-2">{item.user_id}</td>
                        <td className="px-2 py-2">{formatNumber(item.summary.session_count)}</td>
                        <td className="px-2 py-2">{formatNumber(item.summary.total_tokens)}</td>
                        <td className="px-2 py-2">
                          {formatCost(item.summary.total_estimated_cost, item.summary.currency)}
                        </td>
                        <td className="px-2 py-2">
                          {item.compliance.has_fallback_sessions
                            ? t("sessionUsagePage.fallbackYes", {
                                count: item.compliance.fallback_session_count,
                              })
                            : t("sessionUsagePage.fallbackNo")}
                        </td>
                        <td className="px-2 py-2">
                          <Link
                            to={`/instances/${item.instance_id}`}
                            className="text-xs font-medium text-indigo-600 hover:underline"
                          >
                            {t("sessionUsagePage.viewInstance")}
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>

            {overview.total > PAGE_SIZE && (
              <div className="flex items-center justify-between text-xs text-[#8f8681]">
                <span>{t("instances.sessionUsage.pageSummary", { page, totalPages })}</span>
                <div className="flex gap-2">
                  <button
                    type="button"
                    disabled={page <= 1}
                    className="rounded border border-[#ead8cf] px-2 py-1 disabled:opacity-40"
                    onClick={() => setPage((current) => Math.max(1, current - 1))}
                  >
                    {t("instances.sessionUsage.prev")}
                  </button>
                  <button
                    type="button"
                    disabled={page >= totalPages}
                    className="rounded border border-[#ead8cf] px-2 py-1 disabled:opacity-40"
                    onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
                  >
                    {t("instances.sessionUsage.next")}
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </AdminLayout>
  );
};

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="app-panel px-4 py-4">
      <div className="text-xs uppercase tracking-wide text-[#8f8681]">{label}</div>
      <div className="mt-2 text-2xl font-semibold text-[#171212]">{value}</div>
    </div>
  );
}

export default SessionUsageOverviewPage;
