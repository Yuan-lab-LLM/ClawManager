import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { AlertCircle, Info } from 'lucide-react';
import AdminLayout from '../../components/AdminLayout';
import { useI18n } from '../../contexts/I18nContext';
import { useAuth } from '../../contexts/AuthContext';
import api from '../../services/api';
import { formatInstanceType } from '../../types/instance';
import {
  appendSample,
  distribution,
  modelDistribution,
  percentOf,
  resourceSample,
} from './overview/model';
import {
  colors,
  count,
  Gauge,
  percent,
  Pie,
  Trend,
  typeColor,
} from './overview/Charts';
import type { Series } from './overview/Charts';
import NodesDialog from './overview/NodesDialog';
import { overviewText } from './overview/text';
import type {
  AdminOverview,
  ClusterOverview,
  ResourceKey,
  ResourceSample,
} from './overview/types';
import './overview/overview.css';

function Overview({ userID }: { userID: number }) {
  const { locale, t } = useI18n(),
    text = overviewText(locale),
    [metric, setMetric] = useState<'tokens' | ResourceKey>('tokens'),
    [currency, setCurrency] = useState(''),
    [samples, setSamples] = useState<ResourceSample[]>([]),
    nodes = useRef<HTMLDialogElement>(null),
    nodeTrigger = useRef<HTMLButtonElement>(null);
  const common = {
    refetchInterval: 30_000,
    refetchOnWindowFocus: 'always' as const,
    refetchOnReconnect: 'always' as const,
    retry: 1,
    staleTime: 15_000,
  };
  const overview = useQuery({
    ...common,
    queryKey: ['admin-overview', userID],
    queryFn: async ({ signal }) =>
      (await api.get<{ data: AdminOverview }>('/admin/dashboard', { signal }))
        .data.data,
  });
  const resources = useQuery({
    ...common,
    queryKey: ['admin-overview-resources', userID],
    queryFn: async ({ signal }) =>
      (
        await api.get<{ data: ClusterOverview }>(
          '/system-settings/cluster-resources',
          { signal },
        )
      ).data.data,
  });
  useEffect(() => {
    if (resources.data)
      setSamples((previous) =>
        appendSample(
          previous,
          resourceSample(
            resources.data,
            new Date(resources.dataUpdatedAt).toISOString(),
          ),
        ),
      );
  }, [resources.data, resources.dataUpdatedAt]);
  const data = overview.data,
    cluster = resources.data,
    types = data?.types ?? [],
    podRows =
      data?.coverage.runtimes?.status === 'unavailable'
        ? []
        : distribution(types, 'runtime_pods'),
    instanceRows =
      data?.coverage.instances?.status === 'unavailable'
        ? []
        : distribution(types, 'instances'),
    models = modelDistribution(data?.models ?? [], text.other);
  const costRows = data?.usage.currencies ?? [],
    selectedCost =
      costRows.find((row) => row.currency === currency) ?? costRows[0],
    costUnavailable = data?.coverage.costs?.status === 'unavailable',
    costPartial = selectedCost && !selectedCost.complete;
  const costs =
    selectedCost && !costUnavailable
      ? new Intl.NumberFormat(locale, {
          maximumFractionDigits: 4,
          minimumFractionDigits: 2,
        }).format(selectedCost.amount)
      : '—';
  const n = (v: number | null | undefined, compact = false) =>
      count(v, locale, compact),
    p = (v: number | null | undefined) => percent(v, locale),
    activeUsers = percentOf(data?.active_users, data?.total_users),
    activeInstances = percentOf(data?.active_instances, data?.total_instances);
  const error = overview.isError || resources.isError,
    hasGood = !!data || !!cluster,
    conversationsPartial = data?.coverage.conversations?.status === 'partial';
  const sampleRows = samples.length
    ? samples
    : cluster
      ? [
          resourceSample(
            cluster,
            new Date(resources.dataUpdatedAt).toISOString(),
          ),
        ]
      : [];
  const times =
    metric === 'tokens'
      ? (data?.history ?? []).map((point) => point.at)
      : sampleRows.map((point) => point.at);
  const series: Series[] =
    metric === 'tokens'
      ? [
          {
            label: text.tokens,
            values: (data?.history ?? []).map((point) => point.tokens),
            format: (v) => n(v, true),
            className: 'ao-token',
          },
          {
            label: conversationsPartial
              ? text.confirmedConversations
              : text.conversations,
            values: (data?.history ?? []).map((point) => point.conversations),
            format: (v) => n(v, true),
            className: 'ao-conversation',
          },
        ]
      : [
          {
            label: text.actual,
            values: sampleRows.map((point) => point[metric].actual),
            format: p,
            className: 'ao-token',
          },
          {
            label: text.requested,
            values: sampleRows.map((point) => point[metric].requested),
            format: p,
            className: 'ao-request',
          },
        ];
  const missingActual =
      cluster &&
      (['cpu', 'memory', 'disk'] as const).some(
        (key) => cluster[key].used === null || cluster[key].used === undefined,
      ),
    podsTotal =
      data?.coverage.runtimes?.status === 'unavailable'
        ? null
        : types.reduce((sum, row) => sum + row.runtime_pods, 0),
    instancesTotal =
      data?.coverage.instances?.status === 'unavailable'
        ? null
        : types.reduce((sum, row) => sum + row.instances, 0),
    online = types.reduce((sum, row) => sum + row.ready_pods, 0);
  const typeName = (key: string) => formatInstanceType(key);
  return (
    <AdminLayout title={t('admin.dashboardTitle')} fullHeight>
      <div
        className="ao-dashboard"
        data-error={error || undefined}
        aria-busy={overview.isPending || resources.isPending}
      >
        {error && (
          <div className="ao-error" role="alert">
            <AlertCircle size={16} />
            <span>{hasGood ? text.stale : text.error}</span>
            <button
              className="ao-link"
              onClick={() => {
                void overview.refetch();
                void resources.refetch();
              }}
            >
              {text.retry}
            </button>
          </div>
        )}
        <section className="ao-overview" aria-label={text.overview}>
          <div
            className="ao-panel ao-metrics-primary"
            role="group"
            aria-label={text.usage}
          >
            <article className="ao-stat" title={text.costScope}>
              <span>{costPartial ? text.subtotal : text.cost}</span>
              <strong className="ao-cost">
                {costs}{' '}
                {costRows.length > 1 ? (
                  <select
                    aria-label={text.currency}
                    value={selectedCost?.currency ?? ''}
                    onChange={(event) => setCurrency(event.target.value)}
                  >
                    {costRows.map((row) => (
                      <option key={row.currency} value={row.currency}>
                        {row.currency}
                      </option>
                    ))}
                  </select>
                ) : (
                  <small>{selectedCost?.currency}</small>
                )}
              </strong>
              {costPartial && (
                <small className="ao-coverage-label">{text.partial}</small>
              )}
            </article>
            <article className="ao-stat">
              <span>{text.tokens}</span>
              <strong>{n(data?.usage.total_tokens, true)}</strong>
              <small title={text.conversationScope}>
                {conversationsPartial
                  ? text.confirmedConversations
                  : text.conversations}{' '}
                {n(data?.usage.conversations, true)}
              </small>
            </article>
            <article className="ao-stat" title={text.activeScope}>
              <span>{text.activeUsers}</span>
              <strong>
                {n(data?.active_users, true)}{' '}
                <small>/ {n(data?.total_users, true)}</small>
              </strong>
              <div className="ao-kpi-track">
                <i style={{ width: `${Math.min(100, activeUsers ?? 0)}%` }} />
              </div>
            </article>
            <article className="ao-stat" title={text.activeScope}>
              <span>{text.activeInstances}</span>
              <strong>
                {n(data?.active_instances, true)}{' '}
                <small>/ {n(data?.total_instances, true)}</small>
              </strong>
              <div className="ao-kpi-track">
                <i
                  style={{ width: `${Math.min(100, activeInstances ?? 0)}%` }}
                />
              </div>
            </article>
          </div>
          <div
            className="ao-panel ao-metrics-health"
            role="group"
            aria-label={text.health}
          >
            <article className="ao-stat">
              <span>
                <i
                  className={`ao-status-dot ${cluster && cluster.ready_nodes !== cluster.node_count ? 'ao-dot-warning' : ''}`}
                />
                {text.nodesReady}
              </span>
              <strong>
                {n(cluster?.ready_nodes)}{' '}
                <small>/ {n(cluster?.node_count)}</small>
              </strong>
            </article>
            <article className="ao-stat" title={text.qualityScope}>
              <span>{text.success}</span>
              <strong className="ao-good">{p(data?.usage.success_rate)}</strong>
            </article>
            <article className="ao-stat">
              <span>{text.latency}</span>
              <strong>
                {n(
                  data?.usage.average_response_ms === null ||
                    data?.usage.average_response_ms === undefined
                    ? null
                    : data.usage.average_response_ms / 1000,
                )}{' '}
                <small>{text.seconds}</small>
              </strong>
            </article>
            <article className="ao-stat" title={text.sleepingScope}>
              <span>{text.sleeping}</span>
              <strong className="ao-warning">
                {n(data?.idle_instances)} <small>/ {n(data?.idle_total)}</small>
              </strong>
            </article>
          </div>
        </section>
        <div className="ao-main-row">
          <section
            className="ao-panel ao-trend-panel"
            aria-labelledby="ao-trend-title"
          >
            <div className="ao-card-head">
              <h2 id="ao-trend-title">
                {metric === 'tokens' ? text.tokenTrend : text[metric]}
              </h2>
              <div
                className="ao-trend-tabs"
                role="group"
                aria-label={text.range}
              >
                {(['tokens', 'cpu', 'memory', 'disk'] as const).map((key) => (
                  <button
                    key={key}
                    aria-pressed={metric === key}
                    onClick={() => setMetric(key)}
                  >
                    {key === 'tokens'
                      ? `Token / ${text.conversations}`
                      : text[key]}
                  </button>
                ))}
              </div>
            </div>
            <span
              className="ao-period"
              title={
                data
                  ? `${data.window.start} – ${data.window.end} · ${data.timezone}`
                  : undefined
              }
            >
              {metric === 'tokens'
                ? `${text.today} · Asia/Shanghai`
                : text.observing}
              {metric === 'tokens' && conversationsPartial && (
                <span title={text.conversationScope}> · {text.partial}</span>
              )}
            </span>
            <Trend
              key={metric}
              series={series}
              times={times}
              locale={locale}
              sharedAxis={metric !== 'tokens'}
              empty={
                overview.isPending
                  ? text.loading
                  : metric !== 'tokens' && sampleRows.length < 2
                    ? text.collecting
                    : text.noData
              }
            />
          </section>
          <section
            className="ao-panel ao-resource-panel"
            aria-labelledby="ao-resources-title"
          >
            <div className="ao-card-head">
              <h2 id="ao-resources-title">{text.resources}</h2>
              <div className="ao-gauge-legend">
                <span className="ao-token">
                  <i />
                  {text.actualLegend}
                </span>
                <span className="ao-request">
                  <i />
                  {text.requestsLegend}
                </span>
              </div>
              <button
                className="ao-link"
                ref={nodeTrigger}
                onClick={() => {
                  nodes.current?.showModal();
                }}
              >
                {text.nodeDetails}
              </button>
            </div>
            {(missingActual || resources.isError) && (
              <p className="ao-source-note">
                <Info size={13} />
                {cluster ? text.noActual : text.resourceUnavailable}
              </p>
            )}
            <div className="ao-resource-grid">
              {(['cpu', 'memory', 'disk'] as const).map((key) => (
                <Gauge
                  key={key}
                  value={cluster?.[key]}
                  label={text[key]}
                  unit={key === 'cpu' ? text.cores : 'GiB'}
                  locale={locale}
                  text={text}
                />
              ))}
            </div>
            <span className="ao-source-help" title={text.diskScope}>
              {text.actual} / {text.physical} · {text.requested} /{' '}
              {text.allocatable}
            </span>
          </section>
        </div>
        <div className="ao-distribution-row">
          <section
            className="ao-panel ao-types-panel"
            aria-labelledby="ao-types-title"
          >
            <div className="ao-card-head">
              <h2 id="ao-types-title">{text.typeDistribution}</h2>
              <Link className="ao-link" to="/admin/runtime-pods">
                {text.manage}
              </Link>
            </div>
            <div className="ao-type-totals">
              <span>
                {text.runtime} <strong>{data ? n(podsTotal) : '—'}</strong>
              </span>
              <span>
                {text.instances}{' '}
                <strong>{data ? n(instancesTotal) : '—'}</strong>
              </span>
              <span
                className={online === (podsTotal ?? -1) ? '' : 'ao-warning'}
                title={data?.coverage.runtimes?.note}
              >
                {text.ready}{' '}
                {data && podsTotal !== null
                  ? `${n(online)} / ${n(podsTotal)}`
                  : '—'}
                {data?.coverage.runtimes?.status === 'partial'
                  ? ' · ' + text.partial
                  : ''}
              </span>
            </div>
            <div className="ao-type-content">
              <div className="ao-type-pies">
                <figure>
                  <figcaption>{text.runtime}</figcaption>
                  <Pie
                    rows={podRows.map((row) => ({
                      ...row,
                      name: typeName(row.key),
                    }))}
                    label={text.runtime}
                    locale={locale}
                    color={(row) => typeColor(row.key)}
                    empty={
                      overview.isPending
                        ? text.loading
                        : data?.coverage.runtimes?.status === 'unavailable'
                          ? text.unavailable
                          : text.emptyRuntime
                    }
                  />
                </figure>
                <figure>
                  <figcaption>{text.instances}</figcaption>
                  <Pie
                    rows={instanceRows.map((row) => ({
                      ...row,
                      name: typeName(row.key),
                    }))}
                    label={text.instances}
                    locale={locale}
                    color={(row) => typeColor(row.key)}
                    empty={
                      overview.isPending
                        ? text.loading
                        : data?.coverage.instances?.status === 'unavailable'
                          ? text.unavailable
                          : text.emptyInstances
                    }
                  />
                </figure>
              </div>
              <div className="ao-type-table">
                <div className="ao-type-table-head">
                  <span>{text.type}</span>
                  <span>{text.runtime}</span>
                  <span>{text.instances}</span>
                </div>
                {types.map((row) => (
                  <div className="ao-type-table-row" key={row.type}>
                    <span
                      className="ao-type-name"
                      tabIndex={0}
                      title={typeName(row.type)}
                    >
                      <i style={{ background: typeColor(row.type) }} />
                      {typeName(row.type)}
                    </span>
                    <span
                      title={`${n(podsTotal === null ? null : row.runtime_pods)} · ${p(percentOf(row.runtime_pods, podsTotal))}`}
                    >
                      <strong>
                        {n(podsTotal === null ? null : row.runtime_pods)}
                      </strong>{' '}
                      <small>{p(percentOf(row.runtime_pods, podsTotal))}</small>
                    </span>
                    <span
                      title={`${n(instancesTotal === null ? null : row.instances)} · ${p(percentOf(row.instances, instancesTotal))}`}
                    >
                      <strong>
                        {n(instancesTotal === null ? null : row.instances)}
                      </strong>{' '}
                      <small>
                        {p(percentOf(row.instances, instancesTotal))}
                      </small>
                    </span>
                  </div>
                ))}
                {!types.length && (
                  <p className="ao-empty">
                    {overview.isPending ? text.loading : text.noData}
                  </p>
                )}
              </div>
            </div>
          </section>
          <section
            className="ao-panel ao-models-panel"
            aria-labelledby="ao-models-title"
          >
            <div className="ao-card-head">
              <h2 id="ao-models-title">{text.modelDistribution}</h2>
              <span>{text.modelScope}</span>
            </div>
            <div className="ao-model-content">
              <Pie
                rows={models}
                label={text.modelDistribution}
                locale={locale}
                color={(_, i) => colors[i % colors.length]}
                empty={
                  data?.coverage.usage?.status === 'unavailable'
                    ? text.unavailable
                    : overview.isPending
                      ? text.loading
                      : text.emptyModels
                }
              />
              <div className="ao-model-legend">
                {models.map((row, index) => (
                  <div key={row.key}>
                    <span tabIndex={0} title={row.name}>
                      <i
                        style={{ background: colors[index % colors.length] }}
                      />
                      {row.name}
                    </span>
                    <strong title={n(row.count)}>{n(row.count, true)}</strong>
                    <small>{p(Math.round(row.share * 10) / 10)}</small>
                  </div>
                ))}
              </div>
            </div>
          </section>
        </div>
        <NodesDialog
          dialog={nodes}
          trigger={nodeTrigger}
          data={cluster}
          text={text}
          locale={locale}
        />
      </div>
    </AdminLayout>
  );
}
export default function AdminDashboard() {
  const { user } = useAuth();
  return user ? <Overview key={user.id} userID={Number(user.id)} /> : null;
}
