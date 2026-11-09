import {useEffect,useRef,useState} from 'react';
import {ArrowRight,ChevronLeft,ChevronRight} from 'lucide-react';
import type {IEISystemInstance,OwnerDashboardData} from '../services/ieiSystemService';
import {getIEIRuntimePresentation} from '../lib/ieiRuntimeCatalog';

const logos:Record<string,string>={openclaw:'/openclaw.png',hermes:'/hermes.png',opencode:'/opencode.png','deepseek-harness':'/deepseek-harness.svg'};
export function OwnerContinueWork({instances,data,onSelect}:{instances:IEISystemInstance[];data:OwnerDashboardData|null;onSelect:(id:number)=>void}){
 const ref=useRef<HTMLElement>(null);const [page,setPage]=useState(0);const [pageSize,setPageSize]=useState(5);
 useEffect(()=>{const el=ref.current;if(!el)return;const update=()=>{
  const w=el.clientWidth;const desktop=innerWidth>=1200&&innerHeight>=760;
  setPageSize(desktop?(w>=1100?5:w>=880?4:3):5);
 };const ro=new ResizeObserver(update);ro.observe(el);window.addEventListener('resize',update);update();return()=>{ro.disconnect();window.removeEventListener('resize',update)}},[]);
 const pages=Math.max(1,Math.ceil(instances.length/pageSize)),current=Math.min(page,pages-1),start=current*pageSize;
 return <section ref={ref} className="owner-continue"><header className="owner-continue-head"><h3>继续工作</h3>{pages>1&&<nav aria-label="继续工作分页"><button aria-label="上一组实例" disabled={current===0} onClick={()=>setPage(current-1)}><ChevronLeft size={16}/></button><span>{start+1}–{Math.min(start+pageSize,instances.length)} / {instances.length}</span><button aria-label="下一组实例" disabled={current+1>=pages} onClick={()=>setPage(current+1)}><ChevronRight size={16}/></button></nav>}</header>
 <div className="owner-continue-list" data-count={Math.min(pageSize,instances.length)}>{instances.slice(start,start+pageSize).map(i=>{
  const q=data?.recent_questions?.[i.id];const at=q?.at?new Date(q.at):null;
  return <button key={i.id} onClick={()=>onSelect(i.id)} aria-label={`进入实例 ${i.alias?.trim()||i.name}`}>
   <span className="owner-work-identity"><img src={logos[i.type]} alt="" className="owner-continue-logo"/><span className="min-w-0"><span className="owner-continue-name" title={i.alias?.trim()||i.name}>{i.alias?.trim()||i.name}</span><span className="owner-continue-type">{getIEIRuntimePresentation(i.type).name}</span></span></span>
   <span className="owner-work-question"><span>最近提问</span><span className={q?'':'owner-work-empty'} title={q?.text}>{q?.text||'暂无可展示的最近提问'}</span></span>
   <span className="owner-work-footer"><time dateTime={q?.at} title={at?.toLocaleString('zh-CN',{timeZone:'Asia/Shanghai'})+'（北京时间）'}>{at&&!Number.isNaN(at.getTime())?at.toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}):'暂无记录'}</time><span>进入实例 <ArrowRight size={13}/></span></span>
  </button>
 })}</div>{!instances.length&&<p className="py-5 text-sm text-slate-500">目前没有可访问的实例</p>}</section>
}
