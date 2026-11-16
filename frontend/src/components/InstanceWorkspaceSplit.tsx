import { useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { PanelLeft, PanelLeftOpen, X } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import type { Instance } from '../types/instance';
import InstanceSwitcher from './InstanceSwitcher';
import { instanceWorkspaceText } from './instanceWorkspaceText';
import './instance-workspace.css';
import { useAuthStore } from '../stores/authStore';

function readLayout(userId:number|undefined) {
 try { const value=JSON.parse(sessionStorage.getItem(`cm:instance-layout:v1:${userId}`)??'null');
  if(value&&Number.isFinite(value.list)&&value.list>=12&&value.list<=35&&Number.isFinite(value.tools)&&value.tools>=15&&typeof value.showList==='boolean'&&value.tools<=(value.showList?65-value.list:65))return value as {list:number;tools:number;showList:boolean};
 } catch { /* Storage is optional. */ }
 return {list:12,tools:20,showList:true};
}

export default function InstanceWorkspaceSplit({ instance, left, right, visible }: { instance: Instance; left: (listControls: ReactNode) => ReactNode; right: ReactNode; visible: boolean }) {
 const { t,locale } = useI18n();
 const text=instanceWorkspaceText(locale);
 const root=useRef<HTMLDivElement>(null);
 const pointer=useRef<number|null>(null);
 const [fullscreen,setFullscreen]=useState<Element|null>(null);
 useEffect(()=>{
  const changed=()=>setFullscreen(document.fullscreenElement&&root.current?.contains(document.fullscreenElement)?document.fullscreenElement:null);
  document.addEventListener('fullscreenchange',changed);return()=>document.removeEventListener('fullscreenchange',changed);
 },[]);
 const userId=useAuthStore(auth=>auth.user?.id);
 const [layout]=useState(()=>readLayout(userId));
 const [listShare,setListShare]=useState(layout.list);
 const [toolsShare,setToolsShare]=useState(layout.tools);
 const [listVisible,setListVisible]=useState(layout.showList);
 const previousListVisible=useRef(listVisible);
 useLayoutEffect(()=>{
  if(previousListVisible.current===listVisible)return;
  previousListVisible.current=listVisible;
  const target=listVisible
   ?(window.matchMedia('(min-width:1280px)').matches?'.cm-instance-list-pane [data-instance-list-hide]':'.cm-instance-picker-trigger')
   :'[data-instance-list-restore]';
  root.current?.querySelector<HTMLButtonElement>(target)?.focus({preventScroll:true});
 },[listVisible]);
 const toggleList=async()=>{
  if(!listVisible&&document.fullscreenElement){try{await document.exitFullscreen();}catch{return;}}
  if(!listVisible)setToolsShare(value=>Math.min(value,65-listShare));setListVisible(value=>!value);
 };
 useEffect(()=>{try{sessionStorage.setItem(`cm:instance-layout:v1:${userId}`,JSON.stringify({list:listShare,tools:toolsShare,showList:listVisible}));}catch{/* Storage is optional. */}},[userId,listShare,toolsShare,listVisible]);
 const [dragging,setDragging]=useState<'list'|'agent'|null>(null);
 const [pickerOpen,setPickerOpen]=useState(false);
 const picker=useRef<HTMLDialogElement>(null);
 const pickerTrigger=useRef<HTMLButtonElement>(null);
 const listId=useId(),agentId=useId(),toolsId=useId(),pickerTitle=useId();
 useEffect(()=>{
  if(!pickerOpen||!picker.current)return;
  const dialog=picker.current,trigger=pickerTrigger.current;dialog.showModal();
  return()=>{dialog.close();if(trigger?.isConnected)trigger.focus();};
 },[pickerOpen]);
 useEffect(()=>{
  const desktop=window.matchMedia('(min-width:1280px)');
  const changed=()=>{if(!desktop.matches){pointer.current=null;setDragging(null);}else setPickerOpen(false);};
  desktop.addEventListener('change',changed);return()=>desktop.removeEventListener('change',changed);
 },[]);
 const tools=visible?toolsShare:0;
 const list=listVisible?listShare:0;
 const agentShare=100-list-tools;
 const listMax=Math.min(35,100-tools-35);
 const reset=()=>{setListShare(12);setToolsShare(20);};
 const finish=()=>{pointer.current=null;setDragging(null);};
 const resize=(kind:'list'|'agent',value:number)=>{
  if(kind==='list'){const next=Math.min(listMax,Math.max(12,value));setListShare(next);setToolsShare(previous=>Math.min(previous,100-next-35));}
  else setToolsShare(100-list-Math.min(100-list-15,Math.max(35,value)));
 };
 const separator=(kind:'list'|'agent')=>{
  const value=kind==='list'?listShare:agentShare,min=kind==='list'?12:35,max=kind==='list'?listMax:100-list-15;
  return <div key={kind} role="separator" tabIndex={0} className="cm-instance-resizer" aria-label={kind==='list'?text.resizeList:t('instances.resizeWorkspace')} aria-orientation="vertical" aria-controls={kind==='list'?`${listId} ${agentId}`:`${agentId} ${toolsId}`} aria-valuemin={min} aria-valuemax={max} aria-valuenow={Math.round(value*100)/100}
   onDoubleClick={reset}
   onKeyDown={event=>{if(event.key==='Enter'){event.preventDefault();reset();return;}const next:Record<string,number>={ArrowLeft:value-2,ArrowRight:value+2,Home:min,End:max};if(event.key in next){event.preventDefault();resize(kind,next[event.key]);}}}
   onPointerDown={event=>{if(event.button!==0||!event.isPrimary)return;event.preventDefault();event.currentTarget.focus();pointer.current=event.pointerId;event.currentTarget.setPointerCapture(event.pointerId);setDragging(kind);}}
   onPointerMove={event=>{if(pointer.current!==event.pointerId||!root.current)return;const bounds=root.current.getBoundingClientRect(),gap=event.currentTarget.getBoundingClientRect().width,content=bounds.width-gap*((visible?1:0)+(listVisible?1:0));resize(kind,(event.clientX-bounds.left-gap*(kind==='list'||!listVisible?.5:1.5))/content*100-(kind==='list'?0:list));}}
   onPointerUp={event=>{if(pointer.current===event.pointerId){finish();event.currentTarget.releasePointerCapture(event.pointerId);}}} onPointerCancel={finish} onLostPointerCapture={finish}><span aria-hidden="true" /></div>;
 };
 const style:CSSProperties={gridTemplateColumns:`${listVisible?`minmax(0,${listShare}fr) var(--cm-space-group) `:''}minmax(0,${agentShare}fr)${visible?` var(--cm-space-group) minmax(0,${tools}fr)`:''}`};
 const restore=!listVisible&&<button type="button" data-instance-list-restore className={`app-button-secondary cm-instance-list-restore${fullscreen?' cm-instance-list-restore-fullscreen':''}`} aria-label={text.showList} title={text.showList} aria-controls={listId} aria-expanded={false} onClick={toggleList}><PanelLeftOpen size={18} aria-hidden="true"/><span className="cm-instance-list-restore-label">{text.showList}</span></button>;
 const listControls=<>{restore}<button ref={pickerTrigger} type="button" className="app-button-secondary cm-instance-picker-trigger" aria-haspopup="dialog" onClick={async()=>{if(document.fullscreenElement){try{await document.exitFullscreen();}catch{return;}}setPickerOpen(true);}}><PanelLeft size={18} />{text.list}</button></>;
 return <div ref={root} className="cm-instance-split" style={style} data-dragging={dragging!==null} data-tools={visible} data-list={listVisible}>
  <div id={listId} className="cm-instance-pane cm-instance-list-pane" data-instance-pane="list" hidden={!listVisible}><InstanceSwitcher current={instance} onHideList={toggleList} listId={listId} /></div>
  {listVisible&&separator('list')}
  <div id={agentId} className="cm-instance-pane cm-instance-agent-pane" data-instance-pane="agent">{left(listControls)}</div>
  {visible&&separator('agent')}
  <div id={toolsId} className="cm-instance-pane cm-instance-tools-pane" data-instance-pane="tools" hidden={!visible}>{right}</div>
  {dragging!==null&&<div className="cm-instance-drag-shield" aria-hidden="true" />}
  <dialog ref={picker} className="cm-confirm-dialog cm-instance-picker" aria-labelledby={pickerTitle} onKeyDownCapture={event=>{if(event.key==='Escape' && event.target instanceof Element && event.target.closest('dialog')===event.currentTarget){event.preventDefault();event.stopPropagation();setPickerOpen(false);}}} onCancel={event=>{if(event.target!==event.currentTarget)return;event.preventDefault();setPickerOpen(false);}}>
   <div className="cm-instance-picker-heading"><h2 id={pickerTitle}>{text.list}</h2><button type="button" className="cm-icon-button" aria-label={t('common.close')} onClick={()=>setPickerOpen(false)}><X size={18} /></button></div>
   {pickerOpen&&<InstanceSwitcher current={instance} onClose={()=>setPickerOpen(false)} />}
  </dialog>
 </div>;
}
