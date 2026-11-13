import { useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { getWorkspaceUsage } from './service';
import { Link } from 'react-router-dom';
import { Chart } from './Chart';
import { agentRoute, stateLabel, comparison, formatMoney, formatNumber, usageRanking, reportingDate, quotaGauge, stateRing } from './model';
import type { Dashboard, Range, UsageDashboard } from './types';
import type { WorkspaceText } from './text';

export function CoverageNote({ source, text, timeZone }: { source: Dashboard['coverage']['usage']; text: WorkspaceText; timeZone: string }) {
 return <span className={`ws-coverage ws-${source.status}`}>{text[source.status]}{source.since ? ` · ${text.since} ${reportingDate(source.since, timeZone)}` : ''}</span>;
}
export function Operations({ data, userID, paused, locale, text, onDetails, loading }: {
 data?: Dashboard; userID: number; paused: boolean; locale: string; text: WorkspaceText; onDetails: (panel: 'usage' | 'capacity' | 'agents', usage?: UsageDashboard) => void; loading: boolean;
}) {
 return <div className="ws-operations">
  {data ? <UsageCard seed={data} userID={userID} paused={paused} locale={locale} text={text} onDetails={usage => onDetails('usage',usage)} /> : <section className="ws-panel ws-usage-panel"><div className="ws-heading"><h2>{text.usage}</h2></div><div className="ws-loading" role="status">{loading ? text.loading : text.unavailable}</div></section>}
  <section className="ws-panel ws-capacity-panel" aria-labelledby="ws-capacity-heading">
   <div className="ws-heading"><h2 id="ws-capacity-heading">{text.capacity}</h2><span className="ws-note">{text.quota}</span></div>
   {data ? <Capacity data={data} locale={locale} text={text} /> : <div className="ws-loading" role="status">{loading ? text.loading : text.unavailable}</div>}
   <footer className="ws-operating-footer"><span /><button disabled={!data} className="ws-text" onClick={() => onDetails('capacity')}>{text.details}</button></footer>
  </section>
  <section className="ws-panel ws-agents-panel" aria-labelledby="ws-agents-heading">
   <div className="ws-heading"><h2 id="ws-agents-heading">{text.agents}</h2></div>
   {data ? <States data={data} locale={locale} text={text} /> : <div className="ws-loading" role="status">{loading ? text.loading : text.unavailable}</div>}
   <footer className="ws-operating-footer"><span /><button disabled={!data} className="ws-text" onClick={() => onDetails('agents')}>{text.details}</button></footer>
  </section>
 </div>;
}
function UsageCard({seed,userID,paused,locale,text,onDetails}: {seed:Dashboard;userID:number;paused:boolean;locale:string;text:WorkspaceText;onDetails:(data:UsageDashboard)=>void}) {
 const [range,setRange]=useState<Range>('7d');
 const [currency,setCurrency]=useState<string|null>(seed.usage.currencies[0]?.currency ?? null);
 const query=useQuery({queryKey:['workspace-usage',userID,range],queryFn:({signal})=>getWorkspaceUsage(range,signal),
  initialData:()=>range==='7d'?seed:undefined,placeholderData:keepPreviousData,retry:false,staleTime:15_000,gcTime:60_000,
  refetchOnWindowFocus:'always',refetchOnReconnect:'always',refetchInterval:paused?false:30_000,refetchIntervalInBackground:false});
 const data=query.data;
 return <section className="ws-panel ws-usage-panel" aria-labelledby="ws-usage-heading" aria-busy={query.isFetching}>
  <div className="ws-heading"><h2 id="ws-usage-heading">{text.usage}</h2><div className="ws-range" role="group" aria-label={text.range}>{(['today','7d','30d'] as Range[]).map(value=><button key={value} aria-pressed={range===value} onClick={()=>setRange(value)}>{text[value]}</button>)}</div></div>
  <div className="ws-usage-stage" data-pending={query.isFetching}>
   {query.isFetching && <div className="ws-usage-loading" role="status"><span />{text.loadingUsage}</div>}
   {query.isError && <div role="alert" className="ws-error">{data?text.stale:text.usageError}<button onClick={()=>void query.refetch()}>{text.retry}</button></div>}
   {data ? <Usage data={data} agents={seed.instances} locale={locale} text={text} currencyCode={currency ?? data.usage.currencies[0]?.currency ?? 'USD'} onCurrency={setCurrency} /> : !query.isError && <div className="ws-loading" role="status">{text.loadingUsage}</div>}
  </div>
  <footer className="ws-operating-footer"><span>{text.distribution}</span><button disabled={!data||query.isPlaceholderData} className="ws-text" onClick={()=>data&&onDetails(data)}>{text.details}</button></footer>
 </section>;
}
function Usage({data,agents,locale,text,currencyCode,onCurrency}: {data:UsageDashboard;agents:Dashboard['instances'];locale:string;text:WorkspaceText;currencyCode:string;onCurrency:(value:string)=>void}) {
 const n=(value:number|null)=>formatNumber(value,locale);
 const usageNumber=(value:number|null)=>n(data.coverage.usage.status==='unavailable'?null:value);
 const currencies=data.usage.currencies;
 const delta=comparison(data.usage.total_tokens,data.usage.previous_tokens,data.coverage.usage.status==='ready');
 const ranks=usageRanking(data.agent_usage,null,data.coverage.usage.status!=='unavailable').slice(0,1);
 const peak=Math.max(1,...ranks.map(row=>row.tokens));
 const codes=[...new Set([...currencies.map(cost=>cost.currency),currencyCode])];
 return <div className="ws-operation-body">
  <div className="ws-metrics"><div><strong>{usageNumber(data.usage.total_tokens)}</strong><small>{text.input} {usageNumber(data.usage.prompt_tokens)} · {text.output} {usageNumber(data.usage.completion_tokens)}</small></div><div className="ws-costs"><span>{currencies.some(cost=>!cost.complete)||data.coverage.costs.status==='partial'?text.costSubtotal:text.cost}</span>{currencies.length?<>{currencies.map(cost=><div key={cost.currency} title={`${cost.complete?text.ready:text.subtotal}${cost.estimated?` · ${text.estimated}`:''}`}><strong>{cost.estimated && cost.amount !== null ? '≈ ' : ''}{formatMoney(data.coverage.costs.status==='unavailable'?null:cost.amount,cost.currency,locale)}</strong></div>)}</>:<strong>—</strong>}</div></div>
  <div className="ws-chart-controls">{delta!==null&&<span className="ws-usage-comparison">{`${delta>0?'+':''}${n(delta)}% ${text.compare}`}</span>}{codes.length>1&&<select aria-label={text.costCurrency} value={currencyCode} onChange={event=>onCurrency(event.target.value)}>{codes.map(code=><option key={code} value={code}>{code}</option>)}</select>}</div>
  <Chart area showNote={false} values={data.usage_history.map(point=>data.coverage.usage.status==='unavailable'?null:point.tokens)} times={data.usage_history.map(point=>point.at)} label={text.tokens} locale={locale} text={text}
   secondary={{values:data.usage_history.map(point=>data.coverage.costs.status==='unavailable'?null:point.costs[currencyCode]??null),label:`${text.cost} · ${currencyCode}`,format:value=>formatMoney(value,currencyCode,locale),axisFormat:value=>new Intl.NumberFormat(locale,{maximumSignificantDigits:3}).format(value)}} />
  <div className="ws-operating-detail">{ranks.map(row=>{const agent=agents.find(agent=>agent.id===row.instance_id);return <div key={row.instance_id} className="ws-rank">{agent?<Link title={agent.name} to={agentRoute(agent)}>{agent.name}</Link>:<span title={text.archivedUsage}>{text.archivedUsage}</span>}<span className="ws-track"><i style={{width:`${row.tokens/peak*100}%`}} /></span><span>{n(row.tokens)}</span></div>;})}</div>
 </div>;
}
function Capacity({ data, locale, text }: { data: Dashboard; locale: string; text: WorkspaceText }) {
 const n = (value: number | null) => formatNumber(value, locale), capacity = data.capacity;
 const metrics = [
  {key: 'cpu', value: capacity.allocated_cpu, max: capacity.max_cpu, unit: ''},
  {key: 'memory', value: capacity.allocated_memory_gb, max: capacity.max_memory_gb, unit: ' GB'},
  {key: 'storage', value: capacity.allocated_storage_gb, max: capacity.max_storage_gb, unit: ' GB'},
 ] as const;
 return <div className="ws-operation-body">
  <div className="ws-gauges">{metrics.map(metric => {
   const assigned = data.coverage.instances.status === 'unavailable' ? null : metric.value;
   const {ratio, needle} = quotaGauge(assigned, metric.max);
   return <div key={metric.key} className="ws-gauge" data-resource={metric.key} role="img" aria-label={`${text[metric.key]} · ${text.quota}: ${n(assigned)} / ${n(metric.max)}${metric.unit}`}>
    <span className="ws-gauge-label">{text[metric.key]}</span>
    <svg viewBox="0 0 180 125" aria-hidden="true">
     <path className="ws-gauge-track" d="M 20 96 A 70 70 0 0 1 160 96" />
     {needle !== null && <g className="ws-gauge-zones">
      <path className="ws-gauge-green" d="M 20 96 A 70 70 0 0 1 160 96" pathLength="100" strokeDasharray="64 100" />
      <path className="ws-gauge-amber" d="M 20 96 A 70 70 0 0 1 160 96" pathLength="100" strokeDasharray="17 100" strokeDashoffset="-66" />
      <path className="ws-gauge-red" d="M 20 96 A 70 70 0 0 1 160 96" pathLength="100" strokeDasharray="15 100" strokeDashoffset="-85" />
     </g>}
     {Array.from({length:11},(_,index) => <line key={index} x1="29" y1="96" x2={index % 5 === 0 ? 38 : 34} y2="96" className="ws-gauge-tick" transform={`rotate(${index * 18} 90 96)`} />)}
     <text x="16" y="118">0</text><text x="164" y="118" textAnchor="end">{n(metric.max)}</text>
     {needle !== null && <g className="ws-needle" style={{transform:`rotate(${needle * 180}deg)`}}><path d="M 90 93 L 42 96 L 90 99 Z" /><circle cx="90" cy="96" r="4" /></g>}
     <text x="90" y="122" className={`ws-gauge-percent ${ratio !== null && ratio > 1 ? 'ws-over-quota' : ''}`} textAnchor="middle">{ratio === null ? '—' : `${n(ratio * 100)}%`}</text>
    </svg>
    <strong>{n(assigned)}<span> / {n(metric.max)}{metric.unit}</span></strong>
   </div>;
  })}</div>
  <div className="ws-slots"><span>{text.slots}</span><strong>{n(data.coverage.capacity.status === 'unavailable' ? null : capacity.eligible_slots)}</strong></div>
 </div>;
}
function States({ data, locale, text }: { data: Dashboard; locale: string; text: WorkspaceText }) {
 const ring = stateRing(data.state_counts), available = data.coverage.instances.status !== 'unavailable';
 const n = (value: number) => formatNumber(available ? value : null, locale);
 return <div className="ws-operation-body">
  <div className="ws-ring" data-wide={ring.total >= 100} role="img" aria-label={`${text.agents}: ${text.runningTotal} ${n(ring.running)} / ${n(ring.total)}; ${ring.segments.map(part => `${stateLabel(part.state,text)} ${n(part.count)}`).join('; ')}`}>
   <svg viewBox="0 0 160 160" aria-hidden="true"><circle className="ws-ring-track" cx="80" cy="80" r="62" />{available && ring.segments.map(part => <circle key={part.state} className={`ws-ring-segment ws-state-${part.state}`} cx="80" cy="80" r="62" pathLength="100" strokeDasharray={`${part.share * 100} 100`} strokeDashoffset={-part.offset * 100} transform="rotate(-90 80 80)" />)}</svg>
   <div><strong>{n(ring.running)} / {n(ring.total)}</strong><span>{text.runningTotal}</span></div>
  </div>
  <div className="ws-state-legend">{ring.segments.map(part => <div key={part.state}><i className={`ws-dot ws-state-${part.state}`} /><span>{stateLabel(part.state,text)}</span><strong>{n(part.count)}</strong></div>)}</div>
 </div>;
}
