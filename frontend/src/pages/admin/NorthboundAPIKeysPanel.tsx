import { useEffect, useRef, useState } from 'react';
import { KeyRound, Plus, Search, X } from 'lucide-react';
import { northboundSettingsService as service, type NorthboundAPIKey, type NorthboundAPIKeyList, type NorthboundAPIKeyMutation, type NorthboundCallerPolicy } from '../../services/northboundSettingsService';
import type { User } from '../../types/user';

const input = 'w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-sm focus:border-red-400 focus:outline-none focus:ring-2 focus:ring-red-100';
const button = 'rounded-lg border border-slate-200 px-3 py-2 text-sm hover:bg-slate-50 disabled:opacity-40';
const expiresDefault = () => new Date(Date.now()+90*86400000).toISOString().slice(0,10);
const failure = (e: unknown) => (e as {response?: {data?: {error?: string}}}).response?.data?.error || '操作失败，请刷新后重试';

export default function NorthboundAPIKeysPanel({users, callers, scopes, onDirty}: {users: User[]; callers: NorthboundCallerPolicy[]; scopes: string[]; onDirty: (dirty: boolean) => void}) {
 const [uid,setUID]=useState(0);
 const [list,setList]=useState<NorthboundAPIKeyList|null>(null);
 const [query,setQuery]=useState('');
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState('');
 const [secret,setSecret]=useState('');
 const [edit,setEdit]=useState<NorthboundAPIKey|null|undefined>(undefined);
 const [name,setName]=useState('');
 const [selected,setSelected]=useState<string[]>([]);
 const [expires,setExpires]=useState(expiresDefault);
 const [enabled,setEnabled]=useState(true);
 const [currentTime,setCurrentTime]=useState(()=>Date.now());
 useEffect(()=>{const timer=window.setInterval(()=>setCurrentTime(Date.now()),60000);return()=>window.clearInterval(timer)},[]);
 const dialog=useRef<HTMLDialogElement>(null);
 const policy=callers.find(p=>p.user_id===uid);
 const allowed=policy?.enabled ? policy.scopes : [];
 const selectable=scopes.filter(s=>allowed.includes(s));
 useEffect(()=>{let active=true;setList(null);setError('');if(!uid)return;
  service.getAPIKeys(uid).then(v=>{if(active)setList(v)}).catch(e=>{if(active)setError(failure(e))});return()=>{active=false};
 },[uid,callers]);
 useEffect(()=>{onDirty(edit!==undefined || !!secret);return()=>onDirty(false)},[edit,secret,onDirty]);
 useEffect(()=>{if(edit!==undefined || secret)dialog.current?.showModal();else dialog.current?.close()},[edit,secret]);
 const close=()=>{if(edit!==undefined && !window.confirm('关闭将放弃本次编辑，是否继续？'))return;setEdit(undefined);setSecret('')};
 const open=(key:NorthboundAPIKey|null)=>{setEdit(key);setName(key?.name||'');setSelected(key?.scopes||[]);setExpires(key?.expires_at.slice(0,10)||expiresDefault());setEnabled(key?.enabled??true);setError('')};
 const mutate=async(action:NorthboundAPIKeyMutation['action'],key?:NorthboundAPIKey)=>{
  if(!list)return;
  if(['disable','revoke','rotate','remove'].includes(action) && !window.confirm(action==='rotate'?'生成替代 Key，旧 Key 仍然有效。确认继续？':`确认${action==='disable'?'停用':action==='remove'?'移除已撤销的配置记录':'永久撤销'} ${key?.name}？`))return;
  setBusy(true);setError('');
  try{
   const result=await service.mutateAPIKey({user_id:uid,version:list.version,action,id:key?.id||edit?.id,name,scopes:selected,enabled,expires_at:new Date((action==='rotate'?expiresDefault():expires)+'T23:59:59Z').toISOString()});
   setList({user_id:result.user_id,version:result.version,keys:result.keys});setEdit(undefined);setSecret(result.secret||'');
  }catch(e){setError(failure(e))}finally{setBusy(false)}
 };
 const visible=list?.keys.filter(k=>`${k.name} ${k.id}`.toLowerCase().includes(query.toLowerCase()))||[];
 return <section className="space-y-5">
  <div className="rounded-2xl border border-red-100 bg-gradient-to-r from-red-50 to-white p-5"><div className="flex items-center gap-2 font-semibold text-slate-900"><KeyRound size={19}/>独立凭据 · 精确授权</div><p className="mt-2 text-sm leading-6 text-slate-600">直接携带 X-API-Key 调用，无需挑战登录。每次请求使用一把 Key；权限不叠加，原 Bearer 方式保持不变。</p></div>
  <div className="grid gap-3 md:grid-cols-[240px_1fr_auto]"><select aria-label="Key 所属账号" className={input} disabled={busy} value={uid} onChange={e=>setUID(Number(e.target.value))}><option value={0}>选择所属账号</option>{users.map(u=><option key={u.id} value={u.id}>{u.username}</option>)}</select><div className="relative"><Search className="absolute left-3 top-3 text-slate-400" size={17}/><input aria-label="搜索 API Key" placeholder="搜索 Key 名称或标识…" className={`${input} pl-10`} value={query} onChange={e=>setQuery(e.target.value)}/></div><button disabled={!list||!policy?.enabled||busy} onClick={()=>open(null)} className="flex items-center gap-2 rounded-xl bg-red-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-40"><Plus size={16}/>创建 Key</button></div>
  {uid>0&&<p className="text-sm text-slate-500">同一账号下的 Key 共享实例归属范围，接口权限独立。不会自动新建账号；需要数据隔离时请使用不同账号。</p>}
  {error&&<div role="alert" className="rounded-xl bg-red-50 p-3 text-sm text-red-700">{error}<button className="ml-3 underline" disabled={busy} onClick={()=>{setError('');service.getAPIKeys(uid).then(setList).catch(e=>setError(failure(e)))}}>刷新 Key 配置</button></div>}
   <div className="overflow-x-auto rounded-2xl border border-slate-200"><table className="w-full text-left text-sm"><thead className="bg-slate-50 text-xs text-slate-500"><tr>{['名称 / 标识','权限','状态 / 有效期','操作'].map(t=><th key={t} className="px-4 py-3 font-medium">{t}</th>)}</tr></thead><tbody className="divide-y divide-slate-100">{visible.map(k=>{const expired=new Date(k.expires_at).getTime()<=currentTime;return <tr key={k.id}><td className="px-4 py-4"><div className="font-medium">{k.name}</div><code className="text-xs text-slate-400">{k.id}</code></td><td className="max-w-sm px-4 py-4"><div className="flex flex-wrap gap-1">{k.scopes.map(s=><span title={s} key={s} className={`rounded-md px-2 py-1 text-xs ${k.effective_scopes.includes(s)?'bg-red-50 text-red-700':'bg-slate-100 text-slate-400 line-through'}`}>{s}</span>)}</div></td><td className="whitespace-nowrap px-4 py-4"><span className={k.revoked_at||!k.enabled||expired?'text-slate-400':'text-emerald-600'}>{k.revoked_at?'已撤销':expired?'已过期':!k.enabled?'已停用':'已启用'}</span><div className="mt-1 text-xs text-slate-400">{new Date(k.expires_at).toLocaleDateString()}</div></td><td className="px-4 py-4"><div className="flex flex-wrap gap-2">{k.revoked_at?<button className={button} disabled={busy} onClick={()=>void mutate('remove',k)}>移除记录</button>:<><button className={button} disabled={busy} onClick={()=>open(k)}>编辑</button><button className={button} disabled={busy||!k.enabled} onClick={()=>void mutate('disable',k)}>停用</button><button className={button} disabled={busy} onClick={()=>void mutate('rotate',k)}>轮换</button><button className={`${button} text-red-600`} disabled={busy} onClick={()=>void mutate('revoke',k)}>撤销</button></>}</div></td></tr>})}</tbody></table>{visible.length===0&&<div className="p-10 text-center text-sm text-slate-400">{!uid?'先选择账号，再管理它的 API Key':!list?'尚未加载 Key 配置':'暂无匹配 Key。创建后完整密钥只显示一次。'}</div>}</div>
  <dialog ref={dialog} onCancel={e=>{e.preventDefault();if(!busy)close()}} className="w-[min(720px,95vw)] rounded-2xl border border-slate-200 p-0 shadow-2xl backdrop:bg-slate-950/35">
   <div className="flex items-center justify-between border-b px-6 py-4"><h3 className="text-lg font-semibold">{secret?'请立即保存密钥':edit?'编辑 API Key':'创建 API Key'}</h3><button aria-label="关闭" disabled={busy} onClick={close}><X size={20}/></button></div>
   {secret?<div className="space-y-4 p-6"><p className="text-sm text-amber-700">完整密钥仅此次显示，关闭后无法找回。不要发送到群聊、日志或提交到 Git。</p><textarea readOnly aria-label="新 API Key" className={`${input} h-28 font-mono`} value={secret}/><pre className="overflow-auto rounded-xl bg-slate-950 p-4 text-xs text-white">{'GET /api/northbound/v1/pro-instances\nX-API-Key: <复制上方密钥>'}</pre><button className={button} onClick={()=>setSecret('')}>已安全保存，关闭</button></div>:<form className="space-y-5 p-6" onSubmit={e=>{e.preventDefault();void mutate(edit?'update':'create')}}>
    {error&&<p role="alert" className="text-sm text-red-600">{error}</p>}
    <label className="block text-sm">调用方名称<input required maxLength={80} className={`${input} mt-2`} value={name} onChange={e=>setName(e.target.value)}/></label>
    <div className="grid grid-cols-2 gap-4"><label className="text-sm">有效至（UTC）<input type="date" required className={`${input} mt-2`} value={expires} onChange={e=>setExpires(e.target.value)}/></label><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={enabled} onChange={e=>setEnabled(e.target.checked)}/>启用</label></div>
    <p className="text-xs text-slate-500">未获账号授权的权限不可选择。划掉的权限不会生效；重置和删除会丢失实例数据。</p>
    <div className="flex flex-wrap items-center gap-2"><button type="button" className={button} disabled={busy||selectable.length===0||selectable.every(s=>selected.includes(s))} onClick={()=>setSelected(v=>Array.from(new Set([...v,...selectable])))}>全选可授权权限</button><button type="button" className={button} disabled={busy||selected.length===0} onClick={()=>setSelected([])}>取消全选</button><span className="text-xs text-slate-500">全选包含可授权的重置、删除权限，保存后生效。</span></div>
    {['Lite','Pro','分享链接','用量统计'].map(group=><fieldset className="rounded-xl border p-3" key={group}><legend className="px-2 text-sm font-medium">{group}</legend><div className="grid gap-2 sm:grid-cols-2">{scopes.filter(s=>group==='用量统计'?s==='owners:usage:read':group==='分享链接'?s.includes('share-link'):group==='Pro'?s.startsWith('pro-'):s.startsWith('lite-')&&!s.includes('share-link')).map(s=><label key={s} className={`flex items-center gap-2 text-xs ${allowed.includes(s)?'text-slate-700':'text-slate-400'}`}><input type="checkbox" checked={selected.includes(s)} disabled={busy||(!allowed.includes(s)&&!selected.includes(s))} onChange={e=>setSelected(v=>e.target.checked?[...v,s]:v.filter(x=>x!==s))}/>{s}{/:(delete|reset)$/.test(s)&&<span className="text-red-600">危险</span>}</label>)}</div>{group==='用量统计'&&<p className="mt-2 text-xs leading-5 text-slate-500">读取本账号下指定 owner 的 Token、会话、有效提问、活跃度及趋势统计；不返回最近提问内容，不授予实例操作权限，也不影响门户展示。</p>}</fieldset>)}
    <div className="flex justify-end gap-2"><button type="button" disabled={busy} className={button} onClick={close}>取消</button><button disabled={busy||selected.length===0} className="rounded-xl bg-red-600 px-5 py-2.5 text-sm text-white disabled:opacity-40">{busy?'保存中…':'保存 Key'}</button></div>
   </form>}
  </dialog>
 </section>;
}
