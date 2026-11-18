import {useEffect, useRef, useState} from 'react';
import {Link, useNavigate, useParams} from 'react-router-dom';
import {Archive, CheckCircle2, CircleAlert, RefreshCw, Settings2, X} from 'lucide-react';
import AdminLayout from '../../../components/AdminLayout';
import {useI18n} from '../../../contexts/I18nContext';
import {systemBackupService as service, backupErrorCode, terminalBackupTask, type BackupReadiness, type BackupTask, type BackupConfigVersion, type BackupEvent, type BackupVerificationReport} from '../../../services/systemBackupService';
import {backupMessages, backupStatus} from './messages';
import BackupConfigFields from './BackupConfigFields';

const dependencyLabels: Record<string,string> = {
 controller:'独立备份控制器',schema_registry:'数据库结构登记',writer_registry:'写入来源清单',runtime_quiesce:'运行时停写',mysql_checkpoint:'数据库捕获',redis_checkpoint:'消息与协调数据',object_checkpoint:'对象存储',workspace_checkpoint:'工作区与产物',deployment_inventory:'部署资源清单',secret_capture:'敏感配置捕获',external_target:'外部备份存储',encryption_key:'加密密钥',signing_key:'签名密钥',catalog:'灾备目录',isolation:'恢复环境隔离',permissions:'最小权限',capacity:'容量与耗时',feature:'功能开关',
};

export default function BackupsPage() {
 const {locale}=useI18n();const text=backupMessages(locale);const {id}=useParams();const navigate=useNavigate();
 const [health,setHealth]=useState<BackupReadiness>();const [permissions,setPermissions]=useState<string[]>([]);
 const [tasks,setTasks]=useState<BackupTask[]>([]);const [task,setTask]=useState<BackupTask>();const [events,setEvents]=useState<BackupEvent[]>([]);
 const [verification,setVerification]=useState<BackupVerificationReport>();
 const [loading,setLoading]=useState(true);const [error,setError]=useState('');const [busy,setBusy]=useState(false);const [reload,setReload]=useState(0);
 const [config,setConfig]=useState<BackupConfigVersion>();const [showConfig,setShowConfig]=useState(false);const [notice,setNotice]=useState('');
 const [reason,setReason]=useState('');const [proof,setProof]=useState('');const [approval,setApproval]=useState('');
 useEffect(()=>{setProof('');setApproval('')},[config,reason,showConfig]);
 const requestKey=useRef<string|null>(null);
 const mutationKey=useRef<{intent:string;key:string}|undefined>(undefined);
 const keyFor=(intent:string)=>{if(mutationKey.current?.intent!==intent)mutationKey.current={intent,key:crypto.randomUUID()};return mutationKey.current.key;};
 const configDialog=useRef<HTMLElement|null>(null);
 useEffect(()=>{
  if(!showConfig)return;
  const previous=document.activeElement instanceof HTMLElement?document.activeElement:null;
  configDialog.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus();
  return()=>previous?.focus();
 },[showConfig]);
 const canOperate=permissions.includes('system_backup.operate');const canConfigure=permissions.includes('system_backup.configure');
 const errorText=(code:string)=> code==='permission_denied'?text.auth:code==='not_ready'?text.bootstrap:code.startsWith('strong_auth_')?text.mfa:code==='artifact_unavailable'?text.unavailable:['version_conflict','state_conflict','commit_started','idempotency_conflict'].includes(code)?text.conflict:text.error;

 useEffect(()=>{
  const abort=new AbortController();let timer: ReturnType<typeof setTimeout>|undefined;let unchanged=0;let previous='';
  setLoading(true);setTask(undefined);setEvents([]);setVerification(undefined);setError('');
  const poll=async()=>{
   let delay=5000;let again=true;
   try {
    const overview=await service.overview(abort.signal);
    if(abort.signal.aborted)return;setHealth(overview.readiness);setPermissions(overview.permissions);
    let fingerprint='';
    if(id){
     const [detail,activity]=await Promise.all([service.get(id,abort.signal),service.events(id,abort.signal)]);
     if(abort.signal.aborted)return;setTask(detail);setEvents(activity);
     if(detail.task_type==='artifact_verify'&&detail.status==='succeeded'){
      const report=await service.verificationReport(detail.public_id,abort.signal);if(abort.signal.aborted)return;setVerification(report);
     }
     fingerprint=JSON.stringify([detail,activity]);again=!terminalBackupTask(detail);
    } else {
     const items=await service.list(abort.signal);if(abort.signal.aborted)return;setTasks(items);fingerprint=JSON.stringify([overview,items]);again=items.some(t=>!terminalBackupTask(t));
    }
    unchanged=fingerprint===previous?unchanged+1:0;previous=fingerprint;delay=unchanged>=5?15000:5000;setError('');
   } catch(e) {
    if(abort.signal.aborted)return;const code=backupErrorCode(e);setError(code);
    const response=(e as {response?:{status?:number;headers?:Record<string,string>}}).response;
    again=!!response&&[429,503].includes(response.status??0);
    const retry=Number(response?.headers?.['retry-after']);delay=Number.isFinite(retry)&&retry>0?Math.max(5000,retry*1000):15000;
   } finally {if(!abort.signal.aborted){setLoading(false);if(again)timer=setTimeout(poll,delay+Math.floor(Math.random()*500));}}
  };void poll();return()=>{abort.abort();if(timer)clearTimeout(timer)};
 },[id,reload]);

 const create=async()=>{if(busy)return;setBusy(true);setError('');requestKey.current??=crypto.randomUUID();try{const t=await service.create(requestKey.current,'backup');requestKey.current=null;navigate(`/admin/backups/${t.public_id}`)}catch(e){setError(backupErrorCode(e))}finally{setBusy(false)}};
 const openConfig=async()=>{setBusy(true);try{setConfig(await service.config());setShowConfig(true)}catch(e){setError(backupErrorCode(e))}finally{setBusy(false)}};
 const saveConfig=async()=>{if(!config||busy)return;setBusy(true);try{const saved=await service.saveConfig(config.version,config.config,keyFor(JSON.stringify(['config',config,reason])),reason,proof);setConfig(saved);setShowConfig(false);setNotice(text.saved);setReload(x=>x+1)}catch(e){setError(backupErrorCode(e))}finally{setBusy(false)}};
 const prepareApproval=async()=>{if(!config||busy)return;setBusy(true);setError('');try{const binding=await service.configAuthorization({expected_version:config.version,config:config.config,...(reason?{change_reason:reason}:{})},keyFor(JSON.stringify(['config',config,reason])));setApproval(JSON.stringify(binding,null,2))}catch(e){setError(backupErrorCode(e))}finally{setBusy(false)}};
 const cancel=async()=>{if(!task||busy)return;setBusy(true);try{setTask(await service.cancel(task,keyFor(JSON.stringify(['cancel',task.public_id,task.row_version]))));setReload(x=>x+1)}catch(e){setError(backupErrorCode(e))}finally{setBusy(false)}};
 const date=(value:string)=>new Date(value).toLocaleString(locale);
 return <AdminLayout title={text.title}>
  <div className="space-y-6 pb-8">
   <header className="flex flex-wrap items-start justify-between gap-4">
    <div><div className="flex items-center gap-3"><Archive className="h-6 w-6 text-slate-600"/><h1 className="text-2xl font-semibold text-slate-900">{text.title}</h1></div><p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">{text.intro}</p></div>
    <div className="flex flex-wrap gap-2"><button type="button" className="app-button-secondary inline-flex items-center gap-2" onClick={()=>setReload(x=>x+1)} disabled={loading}><RefreshCw className="h-4 w-4"/>{text.refresh}</button><button type="button" className="app-button-secondary inline-flex items-center gap-2" onClick={openConfig} disabled={busy||!permissions.includes('system_backup.read')}><Settings2 className="h-4 w-4"/>{text.config}</button><button type="button" className="app-button-primary" onClick={create} disabled={busy||!health?.can_create||!canOperate}>{busy?text.creating:text.create}</button></div>
   </header>
   {error&&<div role="alert" className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800">{errorText(error)} <span className="ml-2 text-xs">({error})</span></div>}
   {notice&&<div role="status" className="rounded-xl border border-blue-100 bg-blue-50 p-4 text-sm text-blue-800">{notice}</div>}
   {loading&&!health&&<p role="status" className="text-sm text-slate-500">{text.loading}</p>}
   {health&&<div className="grid gap-4 sm:grid-cols-2">
    <section className="rounded-xl border border-slate-200 bg-white p-5"><p className="text-sm text-slate-500">{text.points}</p><p className="mt-3 text-2xl font-semibold text-slate-900">{health.recovery_point_count>0?health.recovery_point_count:text.noPoints}</p><p className="mt-2 text-xs text-slate-500">{text.active}</p></section>
    <section className="rounded-xl border border-slate-200 bg-white p-5"><div className="flex items-center gap-2 text-sm font-medium">{health.can_create?<CheckCircle2 className="h-5 w-5 text-green-600"/>:<CircleAlert className="h-5 w-5 text-amber-600"/>}{!health.enabled?text.disabled:health.can_create?text.ready:text.notReady}</div><p className="mt-3 text-sm leading-6 text-slate-500">{text.scope}</p><p className="mt-2 text-xs text-slate-400">{text.version}: {health.config_version}</p></section>
   </div>}
   {id?<>
    <Link to="/admin/backups" className="inline-block text-sm text-blue-700 hover:underline">← {text.back}</Link>
    {verification&&<section aria-label={text.reportTitle} className="rounded-xl border border-green-200 bg-green-50 p-5"><h2 className="font-semibold text-green-900">{text.reportTitle}</h2><p className="mt-2 text-sm text-green-900">{`${text.reportParts}: ${verification.parts_verified}`}</p><p className="mt-2 break-all text-xs text-green-800">{verification.backup_id}</p><p className="mt-3 text-sm text-slate-700">{text.reportNote}</p></section>}
    {task&&<section className="rounded-xl border border-slate-200 bg-white p-5"><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-lg font-semibold">{backupStatus(task.task_type,locale)} · {backupStatus(task.status,locale)}</h2>{task.can_cancel&&canOperate&&<button type="button" className="app-button-secondary" disabled={busy} onClick={cancel}>{text.cancel}</button>}</div><p className="mt-2 break-all font-mono text-xs text-slate-500">{task.public_id}</p><dl className="mt-5 grid gap-4 text-sm sm:grid-cols-2"><div><dt className="text-slate-500">{text.created}</dt><dd className="mt-1">{date(task.created_at)}</dd></div><div><dt className="text-slate-500">{text.deadline}</dt><dd className="mt-1">{date(task.deadline_at)}</dd></div><div><dt className="text-slate-500">{text.eligibility}</dt><dd className="mt-1">{task.acceptance_eligible?text.eligible:text.ineligible}</dd></div><div><dt className="text-slate-500">{text.purpose}</dt><dd className="mt-1">{task.task_purpose==='functional_test'?text.functional:task.task_purpose}</dd></div>{task.failure_category!=='none'&&<div><dt className="text-slate-500">{text.failure}</dt><dd className="mt-1 text-red-700">{task.failure_category}</dd></div>}</dl></section>}
    <section className="rounded-xl border border-slate-200 bg-white p-5"><h2 className="font-semibold">{text.events}</h2>{events.length===0?<p className="mt-4 text-sm text-slate-500">{text.noEvents}</p>:<ol className="mt-4 space-y-3">{events.map(e=><li key={e.id} className="border-l-2 border-slate-200 pl-4 text-sm"><time className="text-xs text-slate-400">{date(e.created_at)}</time><p className="mt-1">{e.event_type}</p><pre className="mt-1 overflow-auto whitespace-pre-wrap break-all text-xs text-slate-500">{JSON.stringify(e.details,null,2)}</pre></li>)}</ol>}</section>
   </>:<>
    {health&&<section className="rounded-xl border border-slate-200 bg-white p-5"><h2 className="font-semibold">{text.dependencies}</h2><div className="mt-4 grid gap-x-8 gap-y-3 sm:grid-cols-2 xl:grid-cols-3">{health.checks.map(c=><div key={c.name} className="flex items-center justify-between gap-3 text-sm"><span className="text-slate-600">{locale==='zh'?dependencyLabels[c.name]??c.name:c.name.replaceAll('_',' ')}</span><span title={c.code} className={c.passed?'text-green-700':'text-amber-700'}>{c.passed?text.passed:text.pending}</span></div>)}</div></section>}
    <section className="overflow-hidden rounded-xl border border-slate-200 bg-white"><h2 className="px-5 pt-5 font-semibold">{text.tasks}</h2>{tasks.length===0?<div className="px-5 py-10 text-center text-sm text-slate-500">{text.empty}</div>:<div className="mt-4 overflow-x-auto"><table className="w-full text-left text-sm"><thead className="bg-slate-50 text-xs text-slate-500"><tr><th className="px-5 py-3">{text.type}</th><th>{text.status}</th><th>{text.eligibility}</th><th>{text.created}</th><th className="px-5">{text.details}</th></tr></thead><tbody>{tasks.map(t=><tr key={t.public_id} className="border-t border-slate-100"><td className="px-5 py-4">{backupStatus(t.task_type,locale)}</td><td>{backupStatus(t.status,locale)}</td><td>{t.acceptance_eligible?text.eligible:text.ineligible}</td><td className="whitespace-nowrap">{date(t.created_at)}</td><td className="px-5"><Link className="text-blue-700 hover:underline" to={`/admin/backups/${t.public_id}`}>{text.details}</Link></td></tr>)}</tbody></table></div>}</section>
   </>}
   {showConfig&&config&&<div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/30 p-4" onKeyDown={e=>{if(e.key==='Escape'&&!busy)setShowConfig(false);if(e.key==='Tab'){const controls=Array.from(configDialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],[tabindex="0"]')??[]);const first=controls[0],last=controls.at(-1);if(!first){e.preventDefault();return;}if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus()}}}}><section ref={configDialog} role="dialog" aria-modal="true" aria-labelledby="backup-config-title" className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-2xl bg-white p-6 shadow-xl"><div className="flex items-center justify-between"><h2 id="backup-config-title" className="text-lg font-semibold">{text.config}</h2><button type="button" aria-label={text.close} onClick={()=>setShowConfig(false)} disabled={busy}><X className="h-5 w-5"/></button></div><p className="mt-1 text-xs text-slate-400">{text.version}: {config.version}</p><div className="mt-5 space-y-5"><label className="flex items-center gap-3 text-sm"><input type="checkbox" checked={config.config.enabled} disabled={!canConfigure||busy} onChange={e=>setConfig({...config,config:{...config.config,enabled:e.target.checked}})}/>{text.enabled}</label><label className="block text-sm">{text.retention}<input className="mt-2 w-full rounded-lg border border-slate-200 px-3 py-2" type="number" min={1} max={365} disabled={!canConfigure||busy} value={config.config.retention_seconds/86400} onChange={e=>setConfig({...config,config:{...config.config,retention_seconds:Number(e.target.value)*86400}})}/></label><label className="block text-sm">{text.minimum}<input className="mt-2 w-full rounded-lg border border-slate-200 px-3 py-2" type="number" min={1} max={100} disabled={!canConfigure||busy} value={config.config.minimum_recovery_points} onChange={e=>setConfig({...config,config:{...config.config,minimum_recovery_points:Number(e.target.value)}})}/></label><BackupConfigFields config={config.config} onChange={value=>setConfig({...config,config:value})} disabled={!canConfigure||busy} securityAdmin={permissions.includes('system_backup.security_configure')} text={text}/><label className="block text-sm">{text.changeReason}<textarea className="mt-2 w-full rounded-lg border border-slate-200 px-3 py-2" value={reason} maxLength={512} disabled={!canConfigure||busy} onChange={e=>setReason(e.target.value)}/></label>{permissions.includes('system_backup.security_configure')&&<details><summary className="cursor-pointer text-sm font-medium">{text.prepareAuth}</summary><p className="mt-3 text-sm text-slate-500">{text.approvalHelp}</p><button type="button" className="app-button-secondary mt-3" disabled={busy} onClick={prepareApproval}>{text.prepareAuth}</button>{approval&&<label className="mt-3 block text-sm">{text.approvalRequest}<textarea className="mt-2 w-full rounded-lg border border-slate-200 p-3 font-mono text-xs" readOnly rows={8} value={approval}/></label>}<label className="mt-3 block text-sm">{text.approvalProof}<input type="password" autoComplete="off" className="mt-2 w-full rounded-lg border border-slate-200 px-3 py-2" maxLength={8192} value={proof} disabled={busy} onChange={e=>setProof(e.target.value)}/></label></details>}{error&&<p role="alert" className="text-sm text-red-700">{errorText(error)}</p>}<div className="flex justify-end gap-2"><button type="button" className="app-button-secondary" onClick={()=>setShowConfig(false)} disabled={busy}>{text.close}</button>{canConfigure&&<button type="button" className="app-button-primary" disabled={busy} onClick={saveConfig}>{text.save}</button>}</div></div></section></div>}
  </div>
 </AdminLayout>;
}
