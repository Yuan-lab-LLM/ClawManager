import { useCallback, useEffect, useRef, useState } from 'react';
import { RotateCw, Wrench } from 'lucide-react';
import api from '../services/api';
import type { RuntimeUpgradeDetails, RuntimeRollout } from '../types/runtimePool';

type Info = { instance_id:number; generation:number; parent_rollout_id:number; target_image_ref:string; can_retry:boolean; can_rebuild:boolean; isolated?:boolean; reason:string; current?:RuntimeUpgradeDetails };
type Props = {instanceId:number;targetImage?:string;onRecovered?:()=>void;onUpgradeOthers?:()=>void};

// Upgrade repair only. No reset/delete API or replacement-visible instance ID.
export default function UpgradeRecoveryActions({instanceId,targetImage,onRecovered,onUpgradeOthers}:Props){
 const [info,setInfo]=useState<Info|null>(null);
 const [error,setError]=useState('');
 const [sending,setSending]=useState(false);
 const [expanded,setExpanded]=useState(false);
 const key=useRef<string|null>(null);
 const action=useRef('');
 const request=useRef(0);
 const callback=useRef(onRecovered);callback.current=onRecovered;
 const load=useCallback(async()=>{
  const version=++request.current;
  try{const response=await api.get(`/instances/${instanceId}/upgrade-recovery`);if(version===request.current){setInfo(response.data.data as Info);setError('')}}
  catch{if(version===request.current)setError('暂时无法检查升级恢复状态，请稍后重试。')}
 },[instanceId]);
 useEffect(()=>{setInfo(null);key.current=null;void load();return()=>{request.current++}},[load]);
 const active=info?.current && ['pending','running'].includes(info.current.rollout.status);
 useEffect(()=>{
  if(!active)return;
  const timer=window.setInterval(()=>void load(),3000);return()=>window.clearInterval(timer);
 },[active,load]);
 const lastStatus=useRef('');
 useEffect(()=>{
  const status=info?.current?.rollout.status??'';
  if(['pending','running'].includes(lastStatus.current)&&status==='finished')callback.current?.();
  lastStatus.current=status;
 },[info]);
 const submit=async(mode:'retry'|'rebuild')=>{
  if(!info)return;
  if(mode==='rebuild'&&!window.confirm('使用目标版本重建运行环境，保留文件、配置和历史数据。验证成功后替换旧环境，期间实例暂不可用。此操作不是清空数据的实例重置。确认继续？'))return;
  setSending(true);setError('');
  const signature=JSON.stringify([mode,info.parent_rollout_id,info.generation,targetImage?.trim()||info.target_image_ref]);
  if(action.current!==signature){key.current=null;action.current=signature}
  key.current??=crypto.randomUUID();
  try{
   const response=await api.post(`/instances/${instanceId}/upgrade-recovery`,{mode,parent_rollout_id:info.parent_rollout_id,generation:info.generation,target_image_ref:targetImage?.trim()||info.target_image_ref,idempotency_key:key.current,confirm_preserve:mode==='rebuild'});
   const r=response.data.data?.rollout as RuntimeRollout|undefined;
   if(!r)throw new Error('恢复响应不完整');
   key.current=null;await load();setExpanded(true);
  }catch(e){const reason=(e as {response?:{data?:{message?:string}}}).response?.data?.message;setError(reason||'提交未确认，请重新检查后再试；不要重复重启实例。')}
  finally{setSending(false)}
 };
 if(!info)return error?<div className="text-sm text-red-700">{error}<button type="button" className="ml-2 underline" onClick={()=>void load()}>重新检查</button></div>:null;
 if(!info.parent_rollout_id)return null;
 if(!active&&!info.can_retry&&!info.can_rebuild)return null;
 const items=info.current?.items.filter(item=>item.instance_id===instanceId)??[];
 const phase=info.current?.rollout.phase??'';
 return <div className="w-full rounded-lg border border-slate-200 bg-white p-3 text-sm text-slate-700" data-upgrade-recovery={instanceId}>
  <div className="font-medium">{active?'升级恢复处理中':info.isolated?'该实例已隔离，数据保留':info.current?.rollout.status==='finished'?'升级已通过验证':'该实例升级尚未完成'}</div>
  <p className="mt-1">{info.reason}</p>
  {active&&<p className="mt-1" role="status">{({maintenance:'检查并保护实例',image_rollout:'准备目标版本运行环境',compatibility_check:'检查数据与版本兼容性',session_migration:'保全数据并继续升级',gateway_restart:'启动并验证',postflight:'最终验证'} as Record<string,string>)[phase]??'正在检查执行结果'}，无需重复操作。</p>}
  <div className="mt-2 flex flex-wrap items-center gap-2">
   {info.can_retry&&<button type="button" className="app-button-secondary" disabled={sending||!!active} onClick={()=>void submit('retry')}><RotateCw className="h-4 w-4"/>重试升级</button>}
   {info.can_rebuild&&<button type="button" className="app-button-secondary" disabled={sending||!!active} onClick={()=>void submit('rebuild')}><Wrench className="h-4 w-4"/>保留数据重建升级</button>}
   {info.isolated&&onUpgradeOthers&&<button type="button" className="app-button-secondary" disabled={sending||!!active} onClick={onUpgradeOthers}>升级其他实例</button>}
   <button type="button" className="text-slate-600 underline" onClick={()=>{setExpanded(v=>!v);void load()}}>查看原因</button>
  </div>
  {error&&<p className="mt-2 text-red-700" role="alert">{error}</p>}
  {expanded&&<div className="mt-2 break-all border-t border-slate-100 pt-2">
   <div>目标：{targetImage?.trim()||info.target_image_ref}</div>
   {items.map(item=><div key={item.id}>{item.error_message||({verified:'该实例已通过升级验证',gateway_verified:'Gateway 已验证',migration_started:'数据处理尚未完成，原始数据保护仍保留'} as Record<string,string>)[item.state]||'请等待检查结果。'}{item.operation_last_error&&item.operation_last_error!==item.error_message&&<div>最近一次原因：{item.operation_last_error}</div>}</div>)}
   {info.current?.rollout.error_message&&<div>{info.current.rollout.error_message}</div>}
  </div>}
 </div>;
}
