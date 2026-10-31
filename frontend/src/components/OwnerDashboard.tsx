import {useCallback,useEffect,useImperativeHandle,useMemo,useState,useRef,type RefObject,type ReactNode} from 'react';
import {useSearchParams} from 'react-router-dom';
import {BarChart3,Award,CalendarDays,Users} from 'lucide-react';
import {ieiSystemService,type IEISystemInstance,type OwnerDashboardData,type OwnerGrowth,type UsageDay} from '../services/ieiSystemService';
import {OwnerContinueWork} from './OwnerContinueWork';
import {ActivityHeatmap,UsageChart,ownerNumber} from './OwnerUsageCharts';
import {GrowthSummary,OwnerGrowthPanel} from './OwnerGrowthPanel';
import {OwnerModal} from './OwnerModal';
import '../styles/owner-dashboard.css';
import {statisticsTime} from '../lib/ownerTaskState';
import {OwnerReportsPanel} from './OwnerReportsPanel';

export function OwnerDashboard({instances,onSelect,owner,refreshAction,teamContent}:{refreshAction?:RefObject<(()=>Promise<void>)|null>;instances:IEISystemInstance[];onSelect:(id:number)=>void;owner:string;teamContent?:ReactNode}){
 const [params,setParams]=useSearchParams();const tab=['overview','growth','reports','team'].includes(params.get('dashboard_tab')??'')?params.get('dashboard_tab')!:'overview';
 const range=['7d','30d','month'].includes(params.get('usage_range')??'')?params.get('usage_range')!:'7d';
 const [data,setData]=useState<OwnerDashboardData|null>(null);const [growth,setGrowth]=useState<OwnerGrowth|null>(null);const [error,setError]=useState('');const [growthError,setGrowthError]=useState('');const [loading,setLoading]=useState(true);const [detail,setDetail]=useState<UsageDay|null>(null);
 const [info,setInfo]=useState<{title:string;body:string}|null>(null);
 const setQuery=(key:string,value:string)=>setParams(old=>{const p=new URLSearchParams(old);p.set(key,value);return p},{replace:true});
 const sequence=useRef(0);const reportRefresh=useRef<(()=>Promise<void>)|null>(null);
 const fetchSnapshot=useCallback(async(visit=false)=>{
  const request=++sequence.current;setLoading(true);
  try{
   if(visit)await ieiSystemService.recordVisit().catch(()=>{});
   const d=await ieiSystemService.getDashboard(range);
   const g=d.growth??await ieiSystemService.getGrowth(false);
   if(request!==sequence.current)return;
   setData(d);setGrowth(g);setError('');setGrowthError('');
  }catch{if(request===sequence.current)setError('统计读取失败，实例仍可正常使用。')}
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
 useImperativeHandle(refreshAction,()=>async()=>{await Promise.all([fetchSnapshot(true),reportRefresh.current?.()])},[fetchSnapshot]);
 const recent=useMemo(()=>{const groups=new Map<string,IEISystemInstance>();[...instances].sort((a,b)=>Math.max(Date.parse(data?.recent_questions?.[b.id]?.at||'')||0,Date.parse(data?.last_activity[b.id]||'')||0)-Math.max(Date.parse(data?.recent_questions?.[a.id]?.at||'')||0,Date.parse(data?.last_activity[a.id]||'')||0)||Date.parse(b.created_at)-Date.parse(a.created_at)||b.id-a.id).forEach(i=>{if(!groups.has(i.type))groups.set(i.type,i)});return [...groups.values()]},[instances,data]);
 const dates=new Set(data?.trend.map(d=>d.date));const days=data?.insights?.days.filter(d=>dates.has(d.date))??[];
 const comparison=(metric:string)=>{const c=data?.insights?.comparison[metric];const label={day:'较昨日同期',week:'较上周同期',month:'较上月同期'}[c?.window?.kind??data?.insights?.window.kind??'day'];if(!c||c.status==='insufficient')return `暂无可比数据 · ${label}`;if(c.status==='flat')return `本期已采集 ${ownerNumber(c.current)} · 持平 · ${label}`;if(c.status==='new')return `本期已采集 ${ownerNumber(c.current)} · 上期 0 · ${label}`;return `本期 ${ownerNumber(c.current)} · ${c.delta>=0?'↑':'↓'} ${Math.abs(c.percent??0).toFixed(1)}% · ${label}`};
 const compareTitle=(metric:string)=>{const i=data?.insights,c=i?.comparison[metric];if(!i||!c)return '等待统计';const w=c.window??i.window;return `比较本期记录，不是累计总值。\n本期 ${new Date(w.start).toLocaleString('zh-CN')}—${new Date(w.end).toLocaleString('zh-CN')}：${c.current}\n上期 ${new Date(w.previous_start).toLocaleString('zh-CN')}—${new Date(w.previous_end).toLocaleString('zh-CN')}：${c.previous}\n仅按已采集记录比较，覆盖可能不完整。`};
 return <div className="owner-dashboard" data-dashboard-tab={tab}><header className="owner-bookmarks"><div role="tablist" aria-label="我的总览栏目">{[{id:'overview',name:'总览',Icon:BarChart3},{id:'growth',name:'我的成长',Icon:Award},{id:'reports',name:'周期报告',Icon:CalendarDays},...(teamContent?[{id:'team',name:'Team',Icon:Users}]:[])].map(({id,name,Icon})=><button id={`owner-tab-${id}`} role="tab" aria-selected={tab===id} aria-controls="owner-tab-panel" key={id} onClick={()=>setQuery('dashboard_tab',id)}><Icon size={15}/>{name}</button>)}</div></header>
 <h2 className="sr-only">我的总览</h2>{tab!=='team'&&data?.snapshot?.state==='initializing'&&<p role="status" className="owner-alert">统计整理中，历史用量尚未全部汇总；实例可正常使用。</p>}{tab!=='team'&&data?.snapshot?.state==='updating'&&<p role="status" className="owner-alert">部分统计正在整理，当前展示最近可用结果；实例可正常使用。</p>}{tab!=='team'&&(error||growthError)&&<p role="alert" className="owner-alert">{[error,growthError].filter(Boolean).join(' ')}</p>}
 <div id="owner-tab-panel" role="tabpanel" aria-labelledby={`owner-tab-${tab}`} className="owner-panel">
 {tab==='team'?teamContent||<p className="owner-alert">北向 Team 尚未启用</p>:tab==='growth'?<OwnerGrowthPanel growth={growth} owner={owner} onRefresh={refreshGrowth} loading={loading}/>:tab==='reports'?<OwnerReportsPanel refreshAction={reportRefresh}/>:<div className="owner-overview">
 <section data-testid="owner-overview-summary" className="owner-summary">
 <GrowthSummary growth={growth} owner={owner} embedded/>
 {[
 ['questions','有效提问',data?.summary.questions==null?'—':ownerNumber(data.summary.questions),'累计已确认人工提问'],
 ['tokens','Total Token',data?ownerNumber(data.summary.total_tokens):'—','累计已采集用量'],
 ['sessions','会话数',data?ownerNumber(data.summary.sessions):'—','累计有成功调用的会话'],
 ['streak','连续工作日活跃',data?.summary.workday_streak==null?'—':`${data.summary.workday_streak} 天`,'北京时间 · 周一至周五']
 ].map(([key,label,value,note])=><div key={key} className="owner-metric" data-testid={`metric-${key}`}><p className="owner-metric-label">{label}</p><p className="owner-metric-value tabular-nums">{value}</p><p className="owner-metric-note">{note}</p>{key!=='streak'&&<button title={compareTitle(key)} onClick={()=>setInfo({title:"同期比较口径",body:compareTitle(key)})} className="owner-comparison">{comparison(key)}</button>}</div>)}</section>
 <div className="owner-charts"><section className="owner-chart-card"><div className="owner-chart-head"><div><h3 className="text-base font-semibold">Token 使用趋势</h3></div><div className="flex rounded-lg bg-slate-50 p-1">{[['7d','7天'],['30d','30天'],['month','本月']].map(([value,label])=><button key={value} aria-pressed={range===value} onClick={()=>setQuery('usage_range',value)} className={`rounded-md px-2 py-1 text-sm ${range===value?'bg-white text-blue-600 shadow-sm':'text-slate-500'}`}>{label}</button>)}</div></div><p className="owner-chart-caption">周期合计 {ownerNumber(days.reduce((sum,d)=>sum+d.tokens,0))} Token · 快照 {statisticsTime(data?.insights?.as_of)}</p>{loading&&!data?<p role="status" className="py-16 text-center text-sm text-slate-500">正在读取统计…</p>:<UsageChart days={days} onDay={setDetail} fit/>}</section><ActivityHeatmap days={data?.insights?.days??[]} onDay={setDetail}/></div>
 <OwnerContinueWork instances={recent} data={data} onSelect={onSelect}/>
 </div>}
 </div>
 {detail&&<OwnerModal title="当日使用明细" onClose={()=>setDetail(null)}><h4 className="font-semibold">{detail.date}</h4>{detail.observed?<dl className="mt-4 grid grid-cols-2 gap-3">{[['有效提问',detail.questions],['新增会话',detail.sessions],['输入 Token',detail.prompt_tokens],['输出 Token',detail.completion_tokens]].map(([name,value])=><div key={name}><dt className="text-sm text-slate-500">{name}</dt><dd className="mt-1">{Number(value).toLocaleString()}</dd></div>)}</dl>:<p>这一天没有可确认记录，不能推断为零用量。</p>}</OwnerModal>}
 {info&&<OwnerModal title={info.title} onClose={()=>setInfo(null)}><p className="whitespace-pre-line">{info.body}</p></OwnerModal>}
 </div>
}
