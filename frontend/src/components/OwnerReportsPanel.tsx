import {useCallback, useEffect, useImperativeHandle, useRef, useState, type RefObject} from 'react';
import {useSearchParams} from 'react-router-dom';
import {ChevronRight} from 'lucide-react';
import {ieiSystemService, type OwnerReport} from '../services/ieiSystemService';
import {UsageChart, ownerNumber} from './OwnerUsageCharts';
import {statisticsTime} from '../lib/ownerTaskState';
import {OwnerModal} from './OwnerModal';
import {getIEIRuntimePresentation} from '../lib/ieiRuntimeCatalog';
import {isOwnerSessionFailure} from '../lib/ownerReadNotice';

const dateFormat = new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'});
type ReportResult = {key: string; report?: OwnerReport; loading: boolean; error: string};

export function OwnerReportsPanel({open, show, close, refreshAction, entryOnly=false, onReportChange}: {
 open: boolean; show: () => void; close: () => void; refreshAction?: RefObject<(() => Promise<void>) | null>; entryOnly?: boolean; onReportChange?: (report: OwnerReport | null) => void;
}) {
 const [params,setParams] = useSearchParams();
 const kind = params.get('report_kind') === 'month' ? 'month' : 'week';
 const date = params.get('report_date') || dateFormat.format(new Date());
 const rawRevision = Number(params.get('report_revision'));
 const savedRevision = Number.isSafeInteger(rawRevision) && rawRevision>0 ? rawRevision : undefined;
 const requestKey = `${kind}:${date}:${savedRevision ?? ''}`;
 const sequence = useRef(0);
 const [result,setResult] = useState<ReportResult>();
 const [showDetails,setShowDetails] = useState(false);
 const [runtimePage,setRuntimePage] = useState(0);
 const active = result?.key === requestKey ? result : undefined;
 const report = active?.report;
 const loading = !active || active.loading;
 useEffect(()=>{onReportChange?.(report ?? null)},[report,onReportChange]);

 const setPeriod = (name: string, value: string) => {
  const updated = new URLSearchParams(window.location.search);
  if (value) updated.set(name,value); else updated.delete(name);
  if (name !== 'report_revision') updated.delete('report_revision');
  setParams(updated,{replace:true});
  setRuntimePage(0);
 };

 const refresh = useCallback(async (save = false) => {
  const request = ++sequence.current;
  setResult(previous => ({key:requestKey,report:previous?.key===requestKey ? previous.report : undefined,loading:true,error:''}));
  try {
   const loaded = await ieiSystemService.getReport(kind,date,save,save ? undefined : savedRevision);
   if (request !== sequence.current) return;
   setResult({key:requestKey,report:loaded,loading:false,error:''});
   return loaded;
  } catch (error) {
   if (request !== sequence.current) return;
   const message = isOwnerSessionFailure(error) ? '' : save ? '报告保存失败，原有快照保留。' : '报告暂时无法读取，请重试。';
   setResult(previous => ({key:requestKey,report:previous?.report,loading:false,error:message}));
  }
 },[date,kind,requestKey,savedRevision]);

 useEffect(()=>{void refresh();return()=>{sequence.current++}},[refresh]);
 useImperativeHandle(refreshAction,()=>async()=>{await refresh()},[refresh]);
 const saveSnapshot = async () => {
  const updated = await refresh(true);
  if (updated?.revision && updated.revision !== savedRevision) setPeriod('report_revision',String(updated.revision));
 };

 const periods = <div className="owner-compact-tabs" aria-label="报告周期">
  {(['week','month'] as const).map(value => <button type="button" key={value} aria-pressed={value===kind} onClick={()=>{if(value!==kind)setPeriod('report_kind',value)}}>{value==='week'?'周报':'月报'}</button>)}
 </div>;
 const title = report ? `${report.start.slice(0,10)} 至 ${report.end.slice(0,10)}` : '';
 const status = report?.ongoing ? '进行中' : report?.revision ? `快照 v${report.revision}` : '周期已结束 · 统计预览';

 const entry = <button type="button" className="owner-more-button" aria-haspopup="dialog" onClick={()=>{setShowDetails(false);show()}}>完整报告 <ChevronRight size={15} aria-hidden="true"/></button>;
 return <section className={entryOnly?'owner-report-entry':'owner-chart-card owner-report-card'} aria-label="周期报告">
  {entryOnly ? entry : <>
  <div className="owner-chart-head"><h3>周期报告</h3>{periods}</div>
  {report && <>
   <div className="owner-report-period"><strong>{title}</strong><span className="owner-title-badge">{status}</span></div>
   <dl className="owner-report-card-stats">
    <div><dt>期间 Token</dt><dd>{ownerNumber(report.counts.tokens ?? 0)}</dd></div>
    <div><dt>新增会话</dt><dd>{ownerNumber(report.counts.sessions ?? 0)}</dd></div>
    <div><dt>活跃天数</dt><dd>{report.active_days} 天</dd></div>
   </dl>
  </>}
  {active?.error && <p className="owner-metric-note" role="alert">{active.error}</p>}
  {loading && <p className="owner-metric-note" role="status">正在读取周期记录…</p>}
  <div className="owner-report-card-footer"><small>按北京时间统计</small><button type="button" className="owner-more-button" aria-haspopup="dialog" onClick={()=>{setShowDetails(false);show()}}>查看报告 <ChevronRight size={15} aria-hidden="true"/></button></div>
  </>}
  {open && <OwnerModal title="周期报告" onClose={close} wide>
   <div className="owner-reports">
    <section className="owner-report-tools">
     {periods}
     <label>周期 <input aria-label="报告日期" type="date" value={date} onChange={event=>{if(event.target.value)setPeriod('report_date',event.target.value)}}/></label>
     {!!(savedRevision || report?.saved_revisions?.length) && <label>版本 <select aria-label="报告版本" value={savedRevision ?? ''} onChange={event=>setPeriod('report_revision',event.target.value)}>
      <option value="">当前统计预览</option>
      {[...new Set([...(report?.saved_revisions ?? []),...(savedRevision ? [savedRevision] : [])])].map(value=><option key={value} value={value}>快照 v{value}</option>)}
     </select></label>}
     <button type="button" disabled={loading} onClick={()=>void refresh()}>刷新报告</button>
     {report && !report.ongoing && <button type="button" disabled={loading} onClick={()=>void saveSnapshot()} className="owner-report-save">保存 / 更新快照</button>}
    </section>
    {active?.error && <p role="alert" className="owner-alert">{active.error}</p>}
    {loading && <p role="status" className="owner-metric-note">正在读取周期记录…</p>}
    {report && <>
     <section className="owner-report-summary"><div className="owner-chart-head"><h3>{title} · {kind==='week'?'周度':'月度'}使用报告</h3><span className="owner-title-badge">{status}</span></div>
      <div className="owner-report-stats">{[['questions','有效提问'],['tokens','Total Token'],['sessions','新增会话']].map(([field,name])=>{
       const value=report.counts[field]??0;
       const previous=report.previous[field]??0;
       const comparison=report.comparison?.[field];
       const note=comparison?.status==='insufficient'?'上期覆盖未知，暂不比较':previous?`${value>=previous?'↑':'↓'} ${Math.abs((value-previous)/previous*100).toFixed(1)}% 较上期已采集记录`:comparison?(value?'上期已采集 0，本期新增':'上期已采集 0，本期持平'):'上期覆盖未知，暂不比较';
       return <div key={field}><p className="owner-metric-note">{name}</p><strong>{ownerNumber(value)}</strong><p className="owner-metric-note">{note}</p></div>;
      })}<div><p className="owner-metric-note">活跃天数</p><strong>{report.active_days} 天</strong><p className="owner-metric-note">期间使用 {report.instances} 个实例</p></div></div>
     </section>
     <div className="owner-report-body">
      <section className="owner-chart-card"><h3>周期用量走势</h3><p className="owner-chart-caption">统计快照 {statisticsTime(report.as_of)} · 未知日期不连线</p><UsageChart days={report.days} height={148}/></section>
      <section className="owner-report-runtime"><h3>运行时使用分布</h3><ul>{report.runtime.length ? report.runtime.slice(runtimePage*3,runtimePage*3+3).map(runtime=><li key={runtime.runtime}><div className="owner-detail-row"><span>{getIEIRuntimePresentation(runtime.runtime).name}</span><span>{ownerNumber(runtime.tokens)} Token · {runtime.questions} 次提问</span></div><progress max={Math.max(1,report.counts.tokens??0)} value={runtime.tokens} aria-label={`${getIEIRuntimePresentation(runtime.runtime).name} 用量`}/></li>) : <li>本周期尚无记录</li>}</ul>
       {report.runtime.length>3 && <nav className="owner-pager" aria-label="运行时分布分页"><button type="button" disabled={runtimePage===0} onClick={()=>setRuntimePage(page=>page-1)}>上一页</button><span>{runtimePage+1} / {Math.ceil(report.runtime.length/3)}</span><button type="button" disabled={(runtimePage+1)*3>=report.runtime.length} onClick={()=>setRuntimePage(page=>page+1)}>下一页</button></nav>}
      </section>
     </div>
     <footer className="owner-report-footer"><div><h3>周期摘要</h3><p>确认 {report.counts.questions??0} 次提问 · {report.active_days} 天活跃 · 期间使用过 {report.used_sessions} 个会话</p></div><button type="button" className="owner-more-button" aria-expanded={showDetails} onClick={()=>setShowDetails(value=>!value)}>查看完整说明</button></footer>
     {showDetails && <p className="owner-metric-note">本周期使用 {report.instances} 个实例，有记录日期 {report.observed_days} 天。{report.note} 报告时间 {statisticsTime(report.as_of)}。</p>}
    </>}
   </div>
  </OwnerModal>}
 </section>;
}
