import {ArrowRight,MoreHorizontal,Info,Power,RefreshCw} from 'lucide-react';
import {OwnerAgentStateIcon} from './OwnerAgentStateIcon';
import {useCallback,useEffect,useLayoutEffect,useRef,useState,type PointerEvent} from 'react';
import {createPortal} from 'react-dom';
import {Link,useLocation} from 'react-router-dom';
import {getIEIRuntimePresentation,ownerRuntimeLogos} from '../lib/ieiRuntimeCatalog';
import {compactDisplayTime} from '../lib/compactDisplayTime';
import {ownerInstanceURL} from '../lib/ownerPortalNavigation';
import type {IEISystemInstance} from '../services/ieiSystemService';
import '../styles/owner-instance-card.css';

const summaries:Record<string,{text:string;tags:string[]}>= {
 workbuddy:{text:'围绕文档、资料与待办持续协作，协助内容生成、信息归纳和日常事务推进。',tags:['文档生成','信息整理','事务跟进']},
 openclaw:{text:'从日常问答到多步骤执行，借助工具推进工作，适合灵活、多样的协作任务。',tags:['日常协作','工具调用','任务执行']},
 hermes:{text:'围绕复杂目标持续推理与研究，整理资料并保留阶段成果，适合需要深入推进的任务。',tags:['深度推理','资料研究','长程任务']},
 opencode:{text:'理解项目结构，完成代码修改、终端执行与测试验证，适合开发、调试和仓库维护。',tags:['仓库理解','代码修改','测试验证']},
 'deepseek-harness':{text:'按任务组合模型、工具与子代理，扩展执行流程，适合智能体能力与工作方式的实验。',tags:['插件组合','子代理协同','流程扩展']},
};
type Props={instance:IEISystemInstance;selected:boolean;menuOpen:boolean;status:string;pending:boolean;onMenu:()=>void;onCloseMenu:()=>void;onDetails:()=>void;onRestart:()=>void;onReset:()=>void};
export function OwnerInstanceCard({instance,selected,menuOpen,status,pending,onMenu,onCloseMenu,onDetails,onRestart,onReset}:Props){
 const {search,key}=useLocation();
 const runtime=getIEIRuntimePresentation(instance.type),name=instance.alias?.trim()||instance.name;
 const card=useRef<HTMLElement>(null);
 const popup=useRef<HTMLElement>(null);
 const menuButton=useRef<HTMLButtonElement>(null);
 const showTimer=useRef<number|undefined>(undefined);
 const hideTimer=useRef<number|undefined>(undefined);
 const [previewOpen,setPreviewOpen]=useState(false);
 const closePreview=useCallback(()=>{
  window.clearTimeout(showTimer.current);
  window.clearTimeout(hideTimer.current);
  setPreviewOpen(false);
 },[]);
 useEffect(()=>{
  closePreview();
  return ()=>{
   window.clearTimeout(showTimer.current);
   window.clearTimeout(hideTimer.current);
  };
 },[key,closePreview]);
 useEffect(()=>{
  if(menuOpen)closePreview();
 },[menuOpen,closePreview]);
 useLayoutEffect(()=>{
  const panel=popup.current;
  const anchor=card.current;
  if(!previewOpen||!panel||!anchor)return;
  const bounds=anchor.getBoundingClientRect();
  const {width,height}=panel.getBoundingClientRect();
  const viewportWidth=document.documentElement.clientWidth;
  const viewportHeight=document.documentElement.clientHeight;
  const beside=bounds.right+10+width+8<=viewportWidth;
  const left=beside?bounds.right+10:Math.max(8,Math.min(bounds.left,viewportWidth-width-8));
  const preferredTop=beside?bounds.top:bounds.bottom+8;
  const top=Math.max(8,Math.min(preferredTop,viewportHeight-height-8));
  const theme=getComputedStyle(anchor);
  for(const token of ['--instance-accent','--instance-edge','--instance-soft']){
   panel.style.setProperty(token,theme.getPropertyValue(token));
  }
  panel.style.left=left+'px';
  panel.style.top=top+'px';
  panel.dataset.side=beside?'right':'below';
  panel.style.setProperty('--instance-preview-arrow',Math.max(10,Math.min(bounds.top+bounds.height/2-top-4,height-16))+'px');
 },[previewOpen,name,status,instance.updated_at]);
 useEffect(()=>{
  if(!previewOpen)return;
  const onScroll=(event:Event)=>{if(!(event.target instanceof Node&&popup.current?.contains(event.target)))closePreview();};
  const onEscape=(event:KeyboardEvent)=>{
   if(event.key!=='Escape')return;
   if(popup.current?.contains(document.activeElement))menuButton.current?.focus();
   closePreview();
  };
  window.addEventListener('scroll',onScroll,true);
  window.addEventListener('resize',closePreview);
  document.addEventListener('keydown',onEscape);
  return ()=>{
   window.removeEventListener('scroll',onScroll,true);
   window.removeEventListener('resize',closePreview);
   document.removeEventListener('keydown',onEscape);
  };
 },[previewOpen,closePreview]);
 const beginPreview=(event:PointerEvent<HTMLElement>)=>{
  if(event.pointerType==='touch'||event.buttons!==0||menuOpen)return;
  window.clearTimeout(hideTimer.current);
  if(previewOpen)return;
  showTimer.current=window.setTimeout(()=>{
   if(card.current?.matches(':hover'))setPreviewOpen(true);
  },2000);
 };
 const scheduleClose=()=>{
  window.clearTimeout(showTimer.current);
  window.clearTimeout(hideTimer.current);
  if(previewOpen)hideTimer.current=window.setTimeout(()=>{
   if(!popup.current?.contains(document.activeElement))closePreview();
  },180);
 };
 const content=summaries[runtime.id]??{text:runtime.tagline,tags:runtime.capabilities.slice(0,3)};
 const canEnter=instance.status.toLowerCase()!=='deleting';
 const startStopped=instance.status.toLowerCase()==='stopped';
 const entryLabel=startStopped?'启动实例':'进入实例';
 const to=ownerInstanceURL(instance.id,search);
 const entryState={portalReturnSearch:search,startStopped};
 const logo=ownerRuntimeLogos[runtime.id];
 const visualStatus=pending?'starting':instance.status;
 const meta=<span className="instance-meta"><span className={`instance-status ${instance.status==='running'?'running':instance.status==='error'?'error':''}`}>● {status}</span><time dateTime={instance.updated_at} title={new Date(instance.updated_at).toLocaleString('zh-CN')}>{compactDisplayTime(instance.updated_at)}</time></span>;
 const entry=(compact=false)=>canEnter?startStopped&&pending?<button disabled className="instance-entry"><span>处理中</span></button>:<Link aria-label={`${entryLabel} ${name}`} to={to} state={entryState} onClick={closePreview} className={`instance-entry ${compact?'compact':''}`}><span>{entryLabel}</span><ArrowRight size={15}/></Link>:<button disabled className="instance-entry"><span>正在删除</span></button>;
 const identity=<>
   <span className="instance-avatar"><OwnerAgentStateIcon type={runtime.id} status={visualStatus} stoppedReason={instance.stopped_reason} size={44}/></span>
   <span className="instance-heading"><strong title={name}>{name}</strong><span className="instance-runtime-label">{runtime.name} · #{instance.id}</span></span>
 </>;
 return <><article ref={card} data-instance-id={instance.id} data-runtime={runtime.id} className={`portal-runtime-card ${selected?'is-selected':''}`}
  onPointerEnter={beginPreview} onPointerLeave={scheduleClose} onPointerDown={closePreview}
  onKeyDown={e=>{if(e.key==='Escape'){closePreview();if(menuOpen){onCloseMenu();menuButton.current?.focus();}}}}
  onBlur={e=>{if(menuOpen&&!e.currentTarget.contains(e.relatedTarget as Node))onCloseMenu()}}>
  {canEnter ? <Link className="instance-summary" aria-label={`打开实例工作区 ${name}`} aria-current={selected?"page":undefined} to={to} state={{portalReturnSearch:search}}>{identity}</Link> : <div className="instance-summary">{identity}</div>}
  <div className="instance-bottom-row">{meta}<div className="instance-hover-entry" data-testid="instance-quick-actions">{entry(true)}</div></div>
  <button ref={menuButton} type="button" className="instance-more" aria-label={`${name}的操作`} aria-haspopup="menu" aria-expanded={menuOpen} onClick={onMenu}><MoreHorizontal size={18}/></button>
  {menuOpen&&<div className="instance-menu" role="menu" aria-label="实例操作"><button type="button" role="menuitem" onClick={()=>{onDetails();onCloseMenu()}}><Info size={15}/>实例详情</button>{canEnter&&(startStopped&&pending?<button role="menuitem" disabled>处理中</button>:<Link role="menuitem" to={to} state={entryState}><ArrowRight size={15}/>{entryLabel}</Link>)}<button type="button" role="menuitem" disabled={pending||!['running','error'].includes(instance.status.toLowerCase())} onClick={onRestart}><Power size={15}/>重启实例</button><button type="button" role="menuitem" className="danger" disabled={pending||!['running','stopped','error'].includes(instance.status.toLowerCase())} onClick={onReset}><RefreshCw size={15}/>重置实例</button></div>}
 </article>
 {previewOpen&&createPortal(
  <section ref={popup} className="instance-detail-popover" aria-label={`${name}的实例简介`} data-testid="instance-hover-details"
   onPointerEnter={()=>window.clearTimeout(hideTimer.current)} onPointerLeave={scheduleClose}
   onBlur={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node)&&!e.currentTarget.matches(':hover'))closePreview();}}>
   <div className="instance-detail-surface">
    <header className="instance-detail-heading">
     {logo&&<OwnerAgentStateIcon className="instance-detail-watermark" type={runtime.id} status={visualStatus} stoppedReason={instance.stopped_reason} size={99} decorative/>}
     <div className="instance-detail-identity">
      <h3>{name}</h3>
      <span className="instance-runtime-label">{runtime.name} · #{instance.id}{instance.instance_mode==='pro'?' · Pro':instance.instance_mode==='lite'?' · Lite':''}</span>
      {meta}
     </div>
    </header>
    <div className="instance-description">
     <h4>{runtime.category}</h4>
     <p>{content.text}</p>
     <div className="instance-capabilities">{content.tags.map(tag=><span key={tag}>{tag}</span>)}</div>
     <footer>{entry()}</footer>
    </div>
   </div>
  </section>,document.body)}
 </>;
}
