import {useCallback,useEffect,useMemo,useState,useRef,type RefObject,type ReactNode} from 'react';
import {useSearchParams} from 'react-router-dom';
import {ChartNoAxesCombined} from 'lucide-react';
import {ieiSystemService,type IEISystemInstance,type OwnerDashboardData,type OwnerGrowth,type OwnerReport,type UsageDay} from '../services/ieiSystemService';
import {getIEIRuntimePresentation} from '../lib/ieiRuntimeCatalog';
import {OwnerContinueWork} from './OwnerContinueWork';
import {ActivityHeatmap,UsageChart,ownerNumber} from './OwnerUsageCharts';
import {GrowthSummary,OwnerGrowthDetails,OwnerGrowthTasks} from './OwnerGrowthWorkspace';
import {OwnerModal} from './OwnerModal';
import '../styles/owner-dashboard.css';
import {statisticsTime} from '../lib/ownerTaskState';
import {OwnerReportsPanel} from './OwnerReportsPanel';
import {isOwnerSessionFailure,isPersistentReadFailure,nextReadFailure,type ReadFailure} from '../lib/ownerReadNotice';
import {ownerDashboardSection} from '../lib/ownerPortalNavigation';
import '../styles/owner-dashboard-overview.css';

export function OwnerDashboard({instances,onSelect,owner,refreshAction,teamContent}:{refreshAction?:RefObject<(()=>Promise<void>)|null>;instances:IEISystemInstance[];onSelect:(id:number)=>void;owner:string;teamContent?:ReactNode}){
 const [params,setParams]=useSearchParams();const {tab,detail:dashboardDetail}=ownerDashboardSection(params);
 const range=['7d','30d','month'].includes(params.get('usage_range')??'')?params.get('usage_range')!:'7d';
 const [data,setData]=useState<OwnerDashboardData|null>(null);const [growth,setGrowth]=useState<OwnerGrowth|null>(null);const [error,setError]=useState('');const [growthError,setGrowthError]=useState('');const [loading,setLoading]=useState(true);const [detail,setDetail]=useState<UsageDay|null>(null);
 const [dataRange,setDataRange]=useState(range);
 const [report,setReport]=useState<OwnerReport|null>(null);
 const [info,setInfo]=useState<{title:string;body:string}|null>(null);
 const setQuery=(key:string,value:string)=>setParams(old=>{const p=new URLSearchParams(old);p.set(key,value);return p},{replace:true});
 const changeDetail=(detail:'growth'|'report'|'tasks'|null)=>setParams(previous=>{
  const next=new URLSearchParams(previous);
  next.set('view','overview');
  next.set('dashboard_tab','overview');
  if(detail)next.set('dashboard_detail',detail);else next.delete('dashboard_detail');
  return next;
 },{replace:true});
 const sequence=useRef(0);const reportRefresh=useRef<(()=>Promise<void>)|null>(null);
 const readFailure=useRef<ReadFailure|null>(null);const hasSnapshot=useRef(false);
 const fetchSnapshot=useCallback(async(visit=false)=>{
  const request=++sequence.current;setLoading(true);
  try{
   if(visit)await ieiSystemService.recordVisit().catch(e=>{if(isOwnerSessionFailure(e))throw e});
   const d=await ieiSystemService.getDashboard(range);
   if(request!==sequence.current)return;
   hasSnapshot.current=true;readFailure.current=null;
   setData(d);setDataRange(range);setError('');
   try{
    const g=d.growth??await ieiSystemService.getGrowth(false);
    if(request===sequence.current){setGrowth(g);setGrowthError('')}
   }catch(error){if(request===sequence.current&&!isOwnerSessionFailure(error))setGrowthError('成长数据暂时无法更新。')}
  }catch(e){if(request===sequence.current&&!isOwnerSessionFailure(e)){
   readFailure.current=nextReadFailure(readFailure.current,Date.now());
   setError(!hasSnapshot.current?'统计暂时无法读取，请稍后刷新。':isPersistentReadFailure(readFailure.current)?'统计暂时无法更新，已保留上次结果，请稍后刷新。':'');
  }}
  finally{if(request===sequence.current)setLoading(false)}
 },[range]);
 useEffect(()=>{void fetchSnapshot(true);const visible=()=>{if(document.visibilityState==='visible')void fetchSnapshot(true)};document.addEventListener('visibilitychange',visible);return()=>{sequence.current++;document.removeEventListener('visibilitychange',visible)}},[fetchSnapshot]);
 useEffect(()=>{
  const advertised=Date.parse(data?.snapshot?.next_refresh_at??'');
  const fallback=(Math.floor(Date.now()/600000)+1)*600000+5000;
  const due=Number.isFinite(advertised)&&advertised>Date.now()?advertised:fallback;
  const timer=window.setTimeout(()=>{if(document.visibilityState==='visible')void fetchSnapshot()},Math.max(1000,due-Date.now()));
  return()=>clearTimeout(timer);
 },[data?.snapshot?.next_refresh_at,fetchSnapshot]);
 const refreshGrowth=()=>{void fetchSnapshot(true)};
 useEffect(()=>{if(!refreshAction)return;const run=async()=>{await Promise.all([fetchSnapshot(true),reportRefresh.current?.()])};refreshAction.current=run;return()=>{if(refreshAction.current===run)refreshAction.current=null}},[fetchSnapshot,refreshAction]);
 const recent=useMemo(()=>{const groups=new Map<string,IEISystemInstance>();[...instances].sort((a,b)=>Math.max(Date.parse(data?.recent_questions?.[b.id]?.at||'')||0,Date.parse(data?.last_activity[b.id]||'')||0)-Math.max(Date.parse(data?.recent_questions?.[a.id]?.at||'')||0,Date.parse(data?.last_activity[a.id]||'')||0)||Date.parse(b.created_at)-Date.parse(a.created_at)||b.id-a.id).forEach(i=>{if(!groups.has(i.type))groups.set(i.type,i)});return [...groups.values()]},[instances,data]);
 const dates=new Set(data?.trend.map(d=>d.date));const days=data?.insights?.days.filter(d=>dates.has(d.date))??[];
 const observed=days.filter(day=>day.observed);
 const observedTotal=(key:'tokens'|'questions'|'sessions')=>observed.length?ownerNumber(observed.reduce((sum,day)=>sum+day[key],0)):'—';
 const comparison=(metric:string)=>{const c=data?.insights?.comparison[metric];const label={day:'较昨日同期',week:'较上周同期',month:'较上月同期'}[c?.window?.kind??data?.insights?.window.kind??'day'];if(!c||c.status==='insufficient')return `暂无可比数据 · ${label}`;if(c.status==='flat')return `本期已采集 ${ownerNumber(c.current)} · 持平 · ${label}`;if(c.status==='new')return `本期已采集 ${ownerNumber(c.current)} · 上期 0 · ${label}`;return `本期 ${ownerNumber(c.current)} · ${c.delta>=0?'↑':'↓'} ${Math.abs(c.percent??0).toFixed(1)}% · ${label}`};
 const compareTitle=(metric:string)=>{const i=data?.insights,c=i?.comparison[metric];if(!i||!c)return '等待统计';const w=c.window??i.window;return `比较本期记录，不是累计总值。\n本期 ${new Date(w.start).toLocaleString('zh-CN')}—${new Date(w.end).toLocaleString('zh-CN')}：${c.current}\n上期 ${new Date(w.previous_start).toLocaleString('zh-CN')}—${new Date(w.previous_end).toLocaleString('zh-CN')}：${c.previous}\n仅按已采集记录比较，覆盖可能不完整。`};
 return <div className="owner-dashboard" data-dashboard-tab={tab}>
 <h2 className="sr-only">我的总览</h2>{tab!=='team'&&(error||growthError?<p role="alert" className="owner-alert">{error||growthError}</p>:data?.snapshot?.state==='deferred'?<p role="status" className="owner-alert">部分统计更新延迟，当前展示最近可用结果；实例可正常使用。</p>:data?.snapshot?.state==='initializing'?<p role="status" className="owner-alert owner-initializing">历史统计正在初始化，已完成部分正常展示。</p>:null)}
 <div id="owner-tab-panel" className="owner-panel">
 {tab==='team'?teamContent||<p className="owner-alert">北向 Team 尚未启用</p>:<div className="owner-overview owner-integrated-overview">
 <div className="owner-overview-top"><section data-testid="owner-overview-summary" className="owner-summary">
 <GrowthSummary growth={growth} owner={owner} openGrowth={()=>changeDetail('growth')}/>
 {[
 ['questions','有效提问',data?.summary.questions==null?'—':ownerNumber(data.summary.questions),'累计已确认人工提问'],
 ['tokens','Total Token',data?ownerNumber(data.summary.total_tokens):'—','累计已采集用量'],
 ['sessions','会话数',data?ownerNumber(data.summary.sessions):'—','累计有成功调用的会话'],
 ['streak','连续访问',data?.visits?`${data.visits.streak} 天`:'—',data?.visits_unavailable?'访问统计暂不可用':data?.visits?.provisional?'北京时间 · 日历未完整，暂定':'北京时间 · 休息日保护连签']
 ].map(([key,label,value,note])=><div key={key} className="owner-metric" data-testid={`metric-${key}`}><p className="owner-metric-label" title={note}>{label}</p><p className="owner-metric-value tabular-nums">{value}</p>{key!=='streak'&&<button aria-label={`${label}同期比较`} title={`${note}\n${compareTitle(key)}`} onClick={()=>setInfo({title:"同期比较口径",body:`${note}\n${comparison(key)}\n${compareTitle(key)}`})} className="owner-comparison">{comparison(key)}</button>}</div>)}</section>
 <ActivityHeatmap visits={data?.visits} days={data?.insights?.days??[]} onDay={setDetail}/></div>
 <div className="owner-overview-grid">
  <section className="owner-chart-card owner-token-card"><div className="owner-chart-head"><h3><ChartNoAxesCombined size={16} aria-hidden="true"/>用量与周期报告</h3><div className="owner-compact-tabs" aria-label="趋势范围">{[['7d','7天'],['30d','30天'],['month','本月']].map(([value,label])=><button type="button" key={value} aria-pressed={range===value} onClick={()=>setQuery('usage_range',value)}>{label}</button>)}</div></div>
   <div className="owner-usage-period"><small>{days.length?`${days[0].date.slice(5).replace('-','/')}–${days[days.length-1].date.slice(5).replace('-','/')}`:'暂无周期记录'}</small><OwnerReportsPanel entryOnly onReportChange={setReport} refreshAction={reportRefresh} open={dashboardDetail==='report'} show={()=>changeDetail('report')} close={()=>changeDetail(null)}/></div>
   <dl className="owner-usage-stats">{[['tokens','Token 用量'],['questions','有效提问'],['sessions','新增会话']].map(([key,label])=><div key={key}><dt>{label}</dt><dd>{observedTotal(key as 'tokens'|'questions'|'sessions')}</dd></div>)}</dl>
   <div className="owner-usage-chart-label"><span>Token 每日用量</span><small>缺失记录不连线</small></div>
   {loading&&!data?<p role="status" className="owner-metric-note">正在读取统计…</p>:<UsageChart days={days} onDay={setDetail} fit maxTicks={4}/>}
   {report && <section className="owner-usage-distribution" aria-label="报告运行时用量"><header><strong>运行时用量</strong><small>{report.kind==='month'?'月报':'周报'} {report.start.slice(5,10)}–{report.end.slice(5,10)}</small></header><div>{report.runtime.length?report.runtime.map(item=><div key={item.runtime}><span title={getIEIRuntimePresentation(item.runtime).name}>{getIEIRuntimePresentation(item.runtime).name}</span><strong>{ownerNumber(item.tokens)}<small>{report.counts.tokens?`${Math.round(item.tokens/report.counts.tokens*100)}%`:'—'}</small></strong></div>):<p className="owner-metric-note">本期暂无运行时用量记录</p>}</div></section>}
   <p className="owner-chart-caption">{data && dataRange!==range && <>显示上次范围（{dataRange==='7d'?'7天':dataRange==='30d'?'30天':'本月'}） · </>}数据快照 {statisticsTime(data?.insights?.as_of)}{data?.snapshot?.last_batch_at&&<> · 最近汇总批次 {statisticsTime(data.snapshot.last_batch_at)}</>}</p>
  </section>
  <OwnerGrowthTasks growth={growth} loading={loading} open={dashboardDetail==='tasks'} show={()=>changeDetail('tasks')} close={()=>changeDetail(null)}/>
 </div>
 <OwnerContinueWork instances={recent} data={data} onSelect={onSelect}/>
 {dashboardDetail==='growth' && <OwnerGrowthDetails growth={growth} onRefresh={refreshGrowth} loading={loading} onClose={()=>changeDetail(null)}/>}
 </div>}
 </div>
 {detail&&<OwnerModal title="当日使用明细" onClose={()=>setDetail(null)}><h4 className="font-semibold">{detail.date}</h4>{detail.observed?<dl className="mt-4 grid grid-cols-2 gap-3">{[['有效提问',detail.questions],['新增会话',detail.sessions],['输入 Token',detail.prompt_tokens],['输出 Token',detail.completion_tokens]].map(([name,value])=><div key={name}><dt className="text-sm text-slate-500">{name}</dt><dd className="mt-1">{Number(value).toLocaleString()}</dd></div>)}</dl>:<p>这一天没有可确认记录，不能推断为零用量。</p>}</OwnerModal>}
 {info&&<OwnerModal title={info.title} onClose={()=>setInfo(null)}><p className="whitespace-pre-line">{info.body}</p></OwnerModal>}
 </div>
}
