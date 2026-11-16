import { useEffect, useId, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Check, Pencil, X } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import { useAuthStore } from '../stores/authStore';
import { instanceService } from '../services/instanceService';
import type { Instance } from '../types/instance';
import { instanceWorkspaceText } from './instanceWorkspaceText';

export default function InstanceNameEditor({ instance, onRenamed }: { instance: Instance; onRenamed: (id: number, name: string) => void }) {
 const { t, locale } = useI18n();
 const text = instanceWorkspaceText(locale);
 const userId = useAuthStore(state => state.user?.id);
 const client = useQueryClient();
 const [editing,setEditing] = useState(false);
 const [draft,setDraft] = useState(instance.name);
 const [saving,setSaving] = useState(false);
 const [error,setError] = useState('');
 const input = useRef<HTMLInputElement>(null);
 const trigger = useRef<HTMLButtonElement>(null);
 const active = useRef(true);
 const pending = useRef(false);
 const errorId = useId();
 useEffect(() => {active.current=true;return()=>{active.current=false;};},[]);
 useEffect(() => {if(editing){input.current?.focus();input.current?.select();}},[editing]);
 const close = () => {setEditing(false);requestAnimationFrame(()=>trigger.current?.focus());};
 const save = async () => {
  if(pending.current)return;
  const name=draft.trim();
  if([...name].length<3||[...name].length>50){setError(text.length);input.current?.focus();return;}
  if(name===instance.name){close();return;}
  pending.current=true;setSaving(true);setError('');
  try {
   await instanceService.updateInstance(instance.id,{name});
   if(!active.current)return;
   onRenamed(instance.id,name);
   void client.invalidateQueries({queryKey:['instance-switcher',userId]});
   void client.invalidateQueries({queryKey:['instances','list',userId]});
   close();
  } catch(reason) {
   if(active.current){const message=(reason as {response?:{data?:{error?:string}}})?.response?.data?.error;setError(typeof message==='string'?message:text.failed);}
  } finally {pending.current=false;if(active.current)setSaving(false);}
 };
 return <div className="cm-instance-name">
  {editing?<form className="cm-instance-name-form" aria-label={text.rename} noValidate onSubmit={event=>{event.preventDefault();void save();}} onKeyDown={event=>{if(event.key==='Escape'&&!saving){event.preventDefault();close();}}}>
   <div><input ref={input} className="app-input" aria-label={text.name} aria-invalid={!!error} aria-describedby={error?errorId:undefined} value={draft} maxLength={100} readOnly={saving} onChange={event=>{setDraft(event.target.value);setError('');}} />
   <button type="submit" className="cm-icon-button" aria-label={t('common.save')} disabled={saving}><Check size={18} /></button><button type="button" className="cm-icon-button" aria-label={t('common.cancel')} disabled={saving} onClick={close}><X size={18} /></button></div>
   {error&&<p id={errorId} role="alert" className="cm-instance-name-error">{error}</p>}
  </form>:<h1 aria-label={instance.name}><button ref={trigger} type="button" className="cm-instance-name-trigger" aria-label={text.rename} title={text.rename} disabled={instance.status==='deleting'} onClick={()=>{setDraft(instance.name);setError('');setEditing(true);}}><span>{instance.name}</span><Pencil size={16} aria-hidden="true" /></button></h1>}
 </div>;
}
