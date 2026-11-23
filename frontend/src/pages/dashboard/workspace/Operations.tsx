import { useLayoutEffect, useRef, useState } from 'react';
import { Badge, Button, Dropdown, MessageBar, MessageBarActions, MessageBarBody, Option, Skeleton, SkeletonItem, Spinner, Table, TableBody, TableCell, TableHeader, TableHeaderCell, TableRow, ToggleButton } from '@fluentui/react-components';
import { ArrowSyncCircle16Regular, CheckmarkCircle16Regular, ChevronLeft16Regular, ChevronRight16Regular, Clock16Regular, ErrorCircle16Regular, PlayCircle16Regular, QuestionCircle16Regular, Stop16Regular, WeatherMoon16Regular } from '@fluentui/react-icons';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { getWorkspaceUsage } from './service';
import { Chart } from './Chart';
import { agentStates, stateLabel, comparison, formatMoney, formatNumber, reportingDate, quotaRatio, quotaStatus, agentStateTotals } from './model';
import type { Dashboard, Range, UsageDashboard } from './types';
import type { WorkspaceText } from './text';
import { useI18n } from '../../../contexts/I18nContext';

export function CoverageNote({ source, text, timeZone }: { source: Dashboard['coverage']['usage']; text: WorkspaceText; timeZone: string }) {
 return <span className={`ws-coverage ws-${source.status}`}>{text[source.status]}{source.since ? ` · ${text.since} ${reportingDate(source.since, timeZone)}` : ''}</span>;
}
function MetricState({ loading, text }: { loading: boolean; text: WorkspaceText }) {
 return loading ? <Skeleton className="ws-metric-skeleton" aria-label={text.loading}>
  <SkeletonItem size={24} /><SkeletonItem size={64} /><SkeletonItem size={16} />
 </Skeleton> : <p className="ws-empty">{text.unavailable}</p>;
}

export function Operations({ data, instanceCount, userID, paused, locale, text, onDetails, loading, hidden, selectedPanel, workspaceError }: {
 data?: Dashboard; instanceCount: number | null; userID: number; paused: boolean; locale: string; text: WorkspaceText;
 onDetails: (panel: 'usage' | 'capacity', trigger: HTMLButtonElement, usage?: UsageDashboard) => void;
 loading: boolean;
 hidden: boolean;
 workspaceError: boolean;
 selectedPanel?: 'usage' | 'capacity';
}) {
 return <div className="ws-operations" hidden={hidden}>
  <section className="ws-panel ws-capacity-panel" hidden={!!selectedPanel && selectedPanel !== 'capacity'} aria-labelledby="ws-capacity-heading">
   <div className="ws-heading">
    <h2 id="ws-capacity-heading">{text.capacity}</h2>
    <span className="ws-note">{text.quota}</span>
    {data && <Button appearance="subtle" size="small" icon={<ChevronRight16Regular />} iconPosition="after"
     onClick={event => onDetails('capacity', event.currentTarget)}>{text.details}</Button>}
   </div>
   {data ? <Capacity data={data} instanceCount={instanceCount} locale={locale} text={text} /> : <MetricState loading={loading} text={text} />}
  </section>
  {data ? <UsageCard seed={data} userID={userID} paused={paused} locale={locale} text={text} workspaceError={workspaceError} hidden={!!selectedPanel && selectedPanel !== 'usage'}
   onDetails={(trigger, usage) => onDetails('usage', trigger, usage)} /> :
   <section className="ws-panel ws-usage-panel" hidden={!!selectedPanel && selectedPanel !== 'usage'} aria-labelledby="ws-usage-heading">
    <div className="ws-heading"><h2 id="ws-usage-heading">{text.usage}</h2></div>
    <MetricState loading={loading} text={text} />
   </section>}
 </div>;
}

export function AgentStates({ data, hidden, loading, locale, text, onDetails }: {
 data?: Dashboard; hidden: boolean; loading: boolean; locale: string; text: WorkspaceText;
 onDetails: (trigger: HTMLButtonElement) => void;
}) {
 const totals = data ? agentStateTotals(data.state_counts) : null;
 const statesAvailable = data?.coverage.instances.status !== 'unavailable';
 const stateNumber = (value: number) => formatNumber(statesAvailable ? value : null, locale);
 return <section className="ws-panel ws-agents-panel" hidden={hidden} aria-labelledby="ws-agents-heading">
   <div className="ws-heading">
    <h2 id="ws-agents-heading">{text.agents}</h2>
    {totals && <div className="ws-state-summary">
     <span className="ws-note">{text.runningTotal}</span>
     <strong>{stateNumber(totals.running)} / {stateNumber(totals.total)}</strong>
    </div>}
    {data && <Button appearance="subtle" size="small" icon={<ChevronRight16Regular />} iconPosition="after"
     onClick={event => onDetails(event.currentTarget)}>{text.details}</Button>}
   </div>
   {data ? <States data={data} stateNumber={stateNumber} text={text} /> : <MetricState loading={loading} text={text} />}
 </section>;
}

function UsageCard({ seed, userID, paused, locale, text, onDetails, hidden, workspaceError }: {
 seed: Dashboard; userID: number; paused: boolean; locale: string; text: WorkspaceText; hidden: boolean; workspaceError: boolean;
 onDetails: (trigger: HTMLButtonElement, data: UsageDashboard) => void;
}) {
 const [range, setRange] = useState<Range>('7d');
 const [currency, setCurrency] = useState<string | null>(seed.usage.currencies[0]?.currency ?? null);
 const query = useQuery({
  queryKey: ['workspace-usage', userID, range], queryFn: ({ signal }) => getWorkspaceUsage(range, signal),
  initialData: () => range === '7d' ? seed : undefined, placeholderData: keepPreviousData, retry: false, staleTime: 15_000, gcTime: 60_000,
  refetchOnWindowFocus: 'always', refetchOnReconnect: 'always', refetchInterval: paused ? false : 30_000, refetchIntervalInBackground: false,
 });
 const data = query.data;
 const currencyCode = currency ?? data?.usage.currencies[0]?.currency ?? 'USD';
 const codes = [...new Set([...(data?.usage.currencies.map(cost => cost.currency) ?? []), currencyCode])];
 return <section className="ws-panel ws-usage-panel" hidden={hidden} aria-labelledby="ws-usage-heading" aria-busy={query.isFetching}>
  <div className="ws-heading">
   <h2 id="ws-usage-heading">{text.usage}</h2>
   <div className="ws-range" role="group" aria-label={text.range}>
    {(['today', '7d', '30d'] as const).map(value => <ToggleButton key={value} appearance="subtle" size="small"
     checked={range === value} onClick={() => setRange(value)}>{text[value]}</ToggleButton>)}
   </div>
  </div>
  <div className="ws-usage-stage" data-pending={query.isFetching}>
   {query.isFetching && <div className="ws-usage-loading" role="status"><Spinner size="tiny" label={text.loadingUsage} /></div>}
   {query.isError && !workspaceError && <MessageBar intent="error" className="ws-error" role="alert">
    <MessageBarBody>{data ? text.stale : text.usageError}</MessageBarBody>
    <MessageBarActions><Button appearance="secondary" size="small" onClick={() => void query.refetch()}>{text.retry}</Button></MessageBarActions>
   </MessageBar>}
   {data ? <Usage data={data} locale={locale} text={text} currencyCode={currencyCode} />
    : !query.isError && <MetricState loading text={text} />}
  </div>
  <footer className="ws-operating-footer">
   <span>{text.distribution}</span>
   {codes.length > 1 && <Dropdown className="ws-cost-currency" aria-label={text.costCurrency} size="small"
    selectedOptions={[currencyCode]} value={currencyCode} onOptionSelect={(_, selection) => {
     if (selection.optionValue) setCurrency(selection.optionValue);
    }}>
    {codes.map(code => <Option key={code} value={code}>{code}</Option>)}
   </Dropdown>}
   <Button disabled={!data || query.isPlaceholderData} appearance="subtle" size="small" icon={<ChevronRight16Regular />}
    iconPosition="after" onClick={event => { if (data) onDetails(event.currentTarget, data); }}>{text.details}</Button>
  </footer>
 </section>;
}
function Usage({data,locale,text,currencyCode}: {data:UsageDashboard;locale:string;text:WorkspaceText;currencyCode:string}) {
 const n=(value:number|null)=>formatNumber(value,locale);
 const usageNumber=(value:number|null)=>n(data.coverage.usage.status==='unavailable'?null:value);
 const currencies=data.usage.currencies;
 const firstCurrency = currencies.find(cost => cost.currency === currencyCode);
 const visibleCurrencies = firstCurrency
  ? [firstCurrency, ...currencies.filter(cost => cost !== firstCurrency)].slice(0, 2)
  : currencies.slice(0, 2);
 const delta=comparison(data.usage.total_tokens,data.usage.previous_tokens,data.coverage.usage.status==='ready');
 return <div className="ws-operation-body">
  <div className="ws-metrics"><div><span>{text.tokens}</span><strong>{usageNumber(data.usage.total_tokens)}</strong><small>{text.input} {usageNumber(data.usage.prompt_tokens)} · {text.output} {usageNumber(data.usage.completion_tokens)}</small>
   {delta !== null && <small className="ws-usage-comparison">{`${delta > 0 ? '+' : ''}${n(delta)}% ${text.compare}`}</small>}
   </div><div className="ws-costs"><span>{currencies.some(cost=>!cost.complete)||data.coverage.costs.status==='partial'?text.costSubtotal:text.cost}</span>{visibleCurrencies.length?<>{visibleCurrencies.map(cost=><div key={cost.currency} title={`${cost.complete?text.ready:text.subtotal}${cost.estimated?` · ${text.estimated}`:''}`}><strong>{cost.estimated && cost.amount !== null ? '≈ ' : ''}{formatMoney(data.coverage.costs.status==='unavailable'?null:cost.amount,cost.currency,locale)}</strong></div>)}</>:<strong>—</strong>}</div></div>
  <Chart area showNote={false} values={data.usage_history.map(point=>data.coverage.usage.status==='unavailable'?null:point.tokens)} times={data.usage_history.map(point=>point.at)} label={text.tokens} locale={locale} timeZone={data.timezone} text={text}
   secondary={{values:data.usage_history.map(point=>data.coverage.costs.status==='unavailable'?null:point.costs[currencyCode]??null),label:`${text.cost} · ${currencyCode}`,format:value=>formatMoney(value,currencyCode,locale),axisFormat:value=>new Intl.NumberFormat(locale,{maximumSignificantDigits:3}).format(value)}} />

 </div>;
}
function Capacity({ data, instanceCount, locale, text }: { data: Dashboard; instanceCount: number | null; locale: string; text: WorkspaceText }) {
 const n = (value: number | null) => formatNumber(value, locale), capacity = data.capacity;
 const metrics = [
  {key: 'instances', value: instanceCount, max: capacity.max_instances, unit: ''},
  {key: 'cpu', value: capacity.allocated_cpu, max: capacity.max_cpu, unit: ''},
  {key: 'memory', value: capacity.allocated_memory_gb, max: capacity.max_memory_gb, unit: ' GB'},
  {key: 'storage', value: capacity.allocated_storage_gb, max: capacity.max_storage_gb, unit: ' GB'},
 ] as const;
 const percent = new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 });
 return <div className="ws-operation-body">
  <dl className="ws-capacity-values">{metrics.map(metric => {
   const isInstanceCount = metric.key === 'instances';
   const assigned = !isInstanceCount && data.coverage.instances.status === 'unavailable' ? null : metric.value;
   const ratio = quotaRatio(assigned, metric.max);
   const percentage = ratio === null ? '—' : percent.format(ratio);
   const status = isInstanceCount && metric.max === 0 && assigned !== null
    ? (assigned === 0 ? 'quotaReached' : 'quotaOver')
    : quotaStatus(ratio);
   const allocation = `${n(assigned)} / ${n(metric.max)}${metric.unit}`;
   return <div key={metric.key} data-resource={metric.key} data-quota-state={status}>
    <dt>
     <span>{metric.key === 'storage' ? text.workspaceStorage : text[metric.key]}</span>
     {status !== 'normal' && status !== 'unknown' && <span className="ws-capacity-status">{text[status]}</span>}
    </dt>
    <dd>
     {isInstanceCount ? <strong className="ws-capacity-count" title={allocation}>{allocation}</strong> : <>
      <strong className="ws-capacity-percent" title={percentage}>{percentage}</strong>
      <span className="ws-capacity-quota">{allocation}</span>
     </>}
    </dd>
   </div>;
  })}
  </dl>
 </div>;
}

const stateBadges = {
 busy: { Icon: Clock16Regular, color: 'brand' },
 idle: { Icon: CheckmarkCircle16Regular, color: 'success' },
 running: { Icon: PlayCircle16Regular, color: 'brand' },
 sleeping: { Icon: WeatherMoon16Regular, color: 'informative' },
 starting: { Icon: ArrowSyncCircle16Regular, color: 'warning' },
 failed: { Icon: ErrorCircle16Regular, color: 'danger' },
 offline: { Icon: Stop16Regular, color: 'subtle' },
 unknown: { Icon: QuestionCircle16Regular, color: 'subtle' },
} as const;

function States({ data, stateNumber, text }: { data: Dashboard; stateNumber: (count: number) => string; text: WorkspaceText }) {
 const { t } = useI18n();
 const content = useRef<HTMLDivElement>(null);
 const [pageSize, setPageSize] = useState(agentStates.length);
 const [offset, setOffset] = useState(0);
 useLayoutEffect(() => {
  const element = content.current;
  if (!element) return;
  const update = () => {
   const height = element.clientHeight;
   if (!height) return;
   const rowHeight = 36;
   const paginationHeight = 48;
   const fullHeight = (agentStates.length + 1) * rowHeight;
   setPageSize(height >= fullHeight ? agentStates.length : Math.max(1, Math.floor((height - paginationHeight) / rowHeight) - 1));
  };
  update();
  const observer = new ResizeObserver(update);
  observer.observe(element);
  return () => observer.disconnect();
 }, []);
 const pages = Math.ceil(agentStates.length / pageSize);
 const page = Math.min(Math.floor(offset / pageSize), pages - 1);
 const changePage = (next: number) => setOffset(next * pageSize);
 return <div className="ws-state-content" ref={content}>
 <Table id="ws-state-counts" size="small" className="ws-states-table" aria-labelledby="ws-agents-heading">
  <TableHeader><TableRow>
   <TableHeaderCell scope="col">{text.state}</TableHeaderCell>
   <TableHeaderCell scope="col" className="ws-table-number">{text.count}</TableHeaderCell>
  </TableRow></TableHeader>
  <TableBody>
   {agentStates.slice(page * pageSize, (page + 1) * pageSize).map(state => {
    const label = stateLabel(state, text);
    const value = data.state_counts[state] ?? 0;
    const count = stateNumber(value);
    const { Icon, color } = stateBadges[state];
    const populated = value > 0 && data.coverage.instances.status !== 'unavailable';
    return <TableRow key={state}>
     <TableCell title={label}><span className="ws-state-label">
      <Badge appearance="tint" color={color} shape="rounded" size="large" icon={<Icon />} aria-hidden="true" />
      <span>{label}</span>
     </span></TableCell>
     <TableCell className="ws-table-number" title={count} aria-label={`${label} ${count}`} data-state={state}
      data-populated={populated || undefined}>{count}</TableCell>
    </TableRow>;
   })}
  </TableBody>
 </Table>
 {pages > 1 && <div className="ws-state-pagination">
  <span className="ws-note" role="status">{t('workspaceFileManager.pageStatus', { current: page + 1, total: pages })}</span>
  <Button appearance="subtle" icon={<ChevronLeft16Regular />} aria-label={t('workspaceFileManager.previousPage')}
   aria-controls="ws-state-counts" disabledFocusable={page === 0} onClick={() => changePage(page - 1)} />
  <Button appearance="subtle" icon={<ChevronRight16Regular />} aria-label={t('workspaceFileManager.nextPage')}
   aria-controls="ws-state-counts" disabledFocusable={page === pages - 1} onClick={() => changePage(page + 1)} />
 </div>}
 </div>;
}
