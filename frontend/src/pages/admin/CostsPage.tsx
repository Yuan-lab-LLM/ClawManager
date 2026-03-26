import React, { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import AdminLayout from '../../components/AdminLayout';
import { adminService, type CostOverview } from '../../services/adminService';
import { useI18n } from '../../contexts/I18nContext';

type TrendPoint = CostOverview['daily_trend'][number];

const CostsPage: React.FC = () => {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [overview, setOverview] = useState<CostOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);

  useEffect(() => {
    void loadOverview();
  }, [page, limit]);

  const loadOverview = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await adminService.getCostOverview({
        page,
        limit,
        search: search.trim() || undefined,
      });
      setOverview(data);
    } catch (err: any) {
        setError(err.response?.data?.error || t('costsPage.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  const applyFilters = async () => {
    setPage(1);
    try {
      setLoading(true);
      setError(null);
      const data = await adminService.getCostOverview({
        page: 1,
        limit,
        search: search.trim() || undefined,
      });
      setOverview(data);
    } catch (err: any) {
      setError(err.response?.data?.error || t('costsPage.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  const totalPages = Math.max(1, Math.ceil((overview?.total_recent_records || 0) / (overview?.limit || limit || 1)));

  return (
    <AdminLayout title={t('nav.costs')}>
      <div className="space-y-5">
        {error && (
          <section className="app-panel px-5 py-4">
            <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-red-700">
              {error}
            </div>
          </section>
        )}

        {loading || !overview ? (
          <section className="app-panel px-6 py-12 text-sm text-[#8f8681]">
            {t('costsPage.loading')}
          </section>
        ) : (
          <>
            <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <MetricCard label={t('costsPage.promptTokens')} value={overview.total_prompt_tokens.toLocaleString()} note={t('costsPage.promptTokensNote')} />
              <MetricCard label={t('costsPage.completionTokens')} value={overview.total_completion_tokens.toLocaleString()} note={t('costsPage.completionTokensNote')} />
              <MetricCard label={t('costsPage.estimatedSpend')} value={`${overview.total_estimated_cost.toFixed(8)} ${overview.currency}`} note={t('costsPage.estimatedSpendNote')} highlight />
              <MetricCard label={t('costsPage.internalCost')} value={`${overview.total_internal_cost.toFixed(8)} ${overview.currency}`} note={t('costsPage.internalCostNote')} />
            </section>

            <TrendCard points={overview.daily_trend} currency={overview.currency} />

            <section className="grid gap-5 xl:grid-cols-2">
              <AggregateTableCard
                title={t('costsPage.userTotals')}
                subtitle={t('costsPage.userTotalsSubtitle')}
                rows={overview.user_summary}
                currency={overview.currency}
              />
              <AggregateTableCard
                title={t('costsPage.instanceTotals')}
                subtitle={t('costsPage.instanceTotalsSubtitle')}
                rows={overview.instance_summary}
                currency={overview.currency}
              />
            </section>

            <section className="app-panel overflow-hidden">
              <div className="border-b border-[#f1e7e1] px-5 py-5">
                <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
                  <div>
                    <h3 className="text-lg font-semibold text-[#171212]">{t('costsPage.recentRecords')}</h3>
                    <p className="mt-1 text-sm text-[#8f8681]">
                      {t('costsPage.recentRecordsSubtitle')}
                    </p>
                  </div>

                  <div className="flex flex-col gap-3 lg:flex-row">
                    <input
                      type="text"
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                      placeholder={t('costsPage.searchPlaceholder')}
                      className="app-input min-w-[280px]"
                    />
                    <select
                      value={limit}
                      onChange={(event) => setLimit(Number(event.target.value))}
                      className="app-input"
                    >
                      <option value={10}>{t('costsPage.pageSize10')}</option>
                      <option value={20}>{t('costsPage.pageSize20')}</option>
                      <option value={50}>{t('costsPage.pageSize50')}</option>
                    </select>
                    <button onClick={applyFilters} className="app-button-secondary">
                      {t('costsPage.apply')}
                    </button>
                    <button onClick={loadOverview} className="app-button-secondary">
                      {t('common.refresh')}
                    </button>
                  </div>
                </div>
              </div>

              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-[#f1e7e1]">
                  <thead className="bg-[#fcfaf8]">
                    <tr>
                      <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.trace')}</th>
                      <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.user')}</th>
                      <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.instance')}</th>
                      <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.model')}</th>
                      <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.tokens')}</th>
                      <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.spend')}</th>
                      <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.recorded')}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[#f7efe9]">
                    {overview.recent_records.map((record) => (
                      <tr
                        key={record.id}
                        className="cursor-pointer transition-colors hover:bg-[#fffaf7]"
                        onClick={() => navigate(`/admin/ai-audit?trace=${encodeURIComponent(record.trace_id)}`)}
                      >
                        <td className="px-4 py-4 text-sm text-[#171212]">
                          <Link
                            to={`/admin/ai-audit?trace=${encodeURIComponent(record.trace_id)}`}
                            className="font-medium text-[#b46c50] hover:text-[#171212] hover:underline"
                            onClick={(event: React.MouseEvent) => event.stopPropagation()}
                          >
                            {record.trace_id}
                          </Link>
                        </td>
                        <td className="px-4 py-4 text-sm text-[#171212]">{record.username || '-'}</td>
                        <td className="px-4 py-4 text-sm text-[#171212]">
                          <div>{record.instance_name || '-'}</div>
                          {record.instance_id && (
                            <div className="mt-1 text-xs text-[#8f8681]">{t('costsPage.instanceId', { id: record.instance_id })}</div>
                          )}
                        </td>
                        <td className="px-4 py-4">
                          <div className="font-medium text-[#171212]">{record.model_name}</div>
                          <div className="mt-1 text-xs text-[#8f8681]">{record.provider_type}</div>
                        </td>
                        <td className="px-4 py-4 text-sm text-[#171212]">{record.total_tokens.toLocaleString()}</td>
                        <td className="px-4 py-4 text-sm text-[#171212]">
                          <div>{record.estimated_cost.toFixed(8)} {record.currency}</div>
                          <div className="mt-1 text-xs text-[#8f8681]">{t('costsPage.internalValue', { value: record.internal_cost.toFixed(8), currency: record.currency })}</div>
                        </td>
                        <td className="px-4 py-4 text-sm text-[#8f8681]">
                          <div>{new Date(record.recorded_at).toLocaleString()}</div>
                          <Link
                            to={`/admin/ai-audit?trace=${encodeURIComponent(record.trace_id)}`}
                            className="mt-1 inline-flex text-xs font-medium text-[#b46c50] hover:text-[#171212] hover:underline"
                            onClick={(event: React.MouseEvent) => event.stopPropagation()}
                          >
                            {t('costsPage.openInAudit')}
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="flex flex-col gap-3 border-t border-[#f1e7e1] px-5 py-4 text-sm text-[#8f8681] sm:flex-row sm:items-center sm:justify-between">
                <div>
                  {t('costsPage.showing', {
                    from: (overview.page - 1) * overview.limit + 1,
                    to: Math.min(overview.page * overview.limit, overview.total_recent_records),
                    total: overview.total_recent_records,
                  })}
                </div>
                <div className="flex items-center gap-3">
                  <button
                    type="button"
                    onClick={() => setPage((current) => Math.max(1, current - 1))}
                    disabled={page <= 1}
                    className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {t('admin.prev')}
                  </button>
                  <span>
                    {t('admin.pageSummary', { page, total: totalPages })}
                  </span>
                  <button
                    type="button"
                    onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
                    disabled={page >= totalPages}
                    className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {t('admin.nextPage')}
                  </button>
                </div>
              </div>
            </section>
          </>
        )}
      </div>
    </AdminLayout>
  );
};

const AggregateTableCard: React.FC<{
  title: string;
  subtitle: string;
  rows: Array<{
    label: string;
    meta?: string;
    total_tokens: number;
    estimated_cost: number;
    internal_cost: number;
  }>;
  currency: string;
}> = ({ title, subtitle, rows, currency }) => {
  const { t } = useI18n();
  const safeRows = rows ?? [];

  return (
  <section className="app-panel overflow-hidden">
    <div className="border-b border-[#f1e7e1] px-5 py-4">
      <h3 className="text-lg font-semibold text-[#171212]">{title}</h3>
      <p className="mt-1 text-sm text-[#8f8681]">{subtitle}</p>
    </div>
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-[#f1e7e1]">
        <thead className="bg-[#fcfaf8]">
          <tr>
            <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.name')}</th>
            <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.tokens')}</th>
            <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.estimated')}</th>
            <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('costsPage.internal')}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-[#f7efe9]">
          {safeRows.map((row) => (
            <tr key={`${row.label}-${row.meta || ''}`} className="hover:bg-[#fffaf7]">
              <td className="px-4 py-4 text-sm text-[#171212]">
                <div className="font-medium">{row.label}</div>
                {row.meta && <div className="mt-1 text-xs text-[#8f8681]">{row.meta}</div>}
              </td>
              <td className="px-4 py-4 text-sm text-[#171212]">{row.total_tokens.toLocaleString()}</td>
              <td className="px-4 py-4 text-sm text-[#171212]">{row.estimated_cost.toFixed(8)} {currency}</td>
              <td className="px-4 py-4 text-sm text-[#171212]">{row.internal_cost.toFixed(8)} {currency}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  </section>
  );
};

const MetricCard: React.FC<{
  label: string;
  value: string;
  note: string;
  highlight?: boolean;
}> = ({ label, value, note, highlight }) => (
  <div className={`rounded-2xl border px-5 py-5 ${highlight ? 'border-[#f0c9bc] bg-[linear-gradient(135deg,#fff4ee_0%,#fffaf8_100%)]' : 'border-[#eadfd8] bg-white'}`}>
    <div className="text-xs font-semibold uppercase tracking-[0.14em] text-[#8f8681]">{label}</div>
    <div className="mt-4 text-3xl font-semibold tracking-[-0.05em] text-[#171212]">{value}</div>
    <div className="mt-2 text-sm text-[#8f8681]">{note}</div>
  </div>
);

const TrendCard: React.FC<{
  points: TrendPoint[];
  currency: string;
}> = ({ points, currency }) => {
  const { t } = useI18n();
  const safePoints = points ?? [];
  const values = safePoints.map((point) => point.estimated_cost);
  const maxValue = Math.max(...values, 0);
  const chartWidth = 760;
  const chartHeight = 220;
  const padding = 20;
  const innerWidth = chartWidth - padding * 2;
  const innerHeight = chartHeight - padding * 2;

  const coordinates = safePoints.map((point, index) => {
    const x = safePoints.length > 1 ? padding + (innerWidth * index) / (safePoints.length - 1) : chartWidth / 2;
    const ratio = maxValue > 0 ? point.estimated_cost / maxValue : 0;
    const y = padding + innerHeight - innerHeight * ratio;
    return { x, y, point };
  });

  const linePath = coordinates.map((entry, index) => `${index === 0 ? 'M' : 'L'} ${entry.x} ${entry.y}`).join(' ');
  const areaPath = coordinates.length === 0
    ? ''
    : `${linePath} L ${coordinates[coordinates.length - 1].x} ${chartHeight - padding} L ${coordinates[0].x} ${chartHeight - padding} Z`;

  return (
    <section className="app-panel p-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="text-xs font-semibold uppercase tracking-[0.14em] text-[#8f8681]">{t('costsPage.dailyTrend')}</div>
          <h3 className="mt-2 text-xl font-semibold text-[#171212]">{t('costsPage.lastSevenDays')}</h3>
          <p className="mt-1 text-sm text-[#8f8681]">{t('costsPage.dailyTrendSubtitle')}</p>
        </div>
        <div className="rounded-full border border-[#eadfd8] bg-[#fffaf7] px-3 py-1 text-xs font-semibold uppercase tracking-[0.14em] text-[#b46c50]">
          {t('costsPage.sevenDayView')}
        </div>
      </div>

      <div className="mt-5 rounded-2xl border border-[#f1e7e1] bg-[#fffaf7] p-4">
        {safePoints.length === 0 ? (
          <div className="text-sm text-[#8f8681]">{t('costsPage.noTrendData')}</div>
        ) : (
          <>
            <svg viewBox={`0 0 ${chartWidth} ${chartHeight}`} className="h-64 w-full">
              <defs>
                <linearGradient id="dailySpendFill" x1="0" x2="0" y1="0" y2="1">
                  <stop offset="0%" stopColor="#ef6b4a" stopOpacity="0.30" />
                  <stop offset="100%" stopColor="#ef6b4a" stopOpacity="0.03" />
                </linearGradient>
              </defs>
              <path d={areaPath} fill="url(#dailySpendFill)" />
              <path d={linePath} fill="none" stroke="#ef6b4a" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" />
              {coordinates.map(({ x, y, point }) => (
                <g key={point.day}>
                  <circle cx={x} cy={y} r="4.5" fill="#fff" stroke="#ef6b4a" strokeWidth="2" />
                </g>
              ))}
            </svg>

            <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              {safePoints.map((point) => (
                <div key={point.day} className="rounded-xl border border-[#f1e7e1] bg-white px-3 py-3">
                  <div className="text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{point.day.slice(5)}</div>
                  <div className="mt-2 text-sm font-semibold text-[#171212]">{point.estimated_cost.toFixed(6)} {currency}</div>
                  <div className="mt-1 text-xs text-[#8f8681]">{point.total_tokens.toLocaleString()} tokens</div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </section>
  );
};

export default CostsPage;
