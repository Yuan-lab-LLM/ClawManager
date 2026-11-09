import {ArrowRight,ChevronUp,MoreHorizontal,Info,Power,RefreshCw,Box} from 'lucide-react';
import {Link} from 'react-router-dom';
import {getIEIRuntimePresentation} from '../lib/ieiRuntimeCatalog';
import {compactDisplayTime} from '../lib/compactDisplayTime';
import type {IEISystemInstance} from '../services/ieiSystemService';
import '../styles/owner-instance-card.css';

const logos:Record<string,string>={openclaw:'/openclaw.png',hermes:'/hermes-card.png',opencode:'/opencode-card.svg','deepseek-harness':'/deepseek-harness.svg'};
const summaries:Record<string,{text:string;tags:string[]}>= {
 openclaw:{text:'从日常问答到多步骤执行，借助工具推进工作，适合灵活、多样的协作任务。',tags:['日常协作','工具调用','任务执行']},
 hermes:{text:'围绕复杂目标持续推理与研究，整理资料并保留阶段成果，适合需要深入推进的任务。',tags:['深度推理','资料研究','长程任务']},
 opencode:{text:'理解项目结构，完成代码修改、终端执行与测试验证，适合开发、调试和仓库维护。',tags:['仓库理解','代码修改','测试验证']},
 'deepseek-harness':{text:'按任务组合模型、工具与子代理，扩展执行流程，适合智能体能力与工作方式的实验。',tags:['插件组合','子代理协同','流程扩展']},
};
type Props={instance:IEISystemInstance;expanded:boolean;selected:boolean;menuOpen:boolean;status:string;pending:boolean;onToggle:()=>void;onMenu:()=>void;onCloseMenu:()=>void;onDetails:()=>void;onRestart:()=>void;onReset:()=>void};
export function OwnerInstanceCard({instance,expanded,selected,menuOpen,status,pending,onToggle,onMenu,onCloseMenu,onDetails,onRestart,onReset}:Props){
 const runtime=getIEIRuntimePresentation(instance.type),name=instance.alias?.trim()||instance.name;
 const content=summaries[runtime.id]??{text:runtime.tagline,tags:runtime.capabilities.slice(0,3)};
 const canEnter=instance.status.toLowerCase()!=='deleting';
 const logo=logos[runtime.id];
 const meta=<span className="instance-meta"><span className={`instance-status ${instance.status==='running'?'running':instance.status==='error'?'error':''}`}>● {status}</span><time dateTime={instance.updated_at} title={new Date(instance.updated_at).toLocaleString('zh-CN')}>{compactDisplayTime(instance.updated_at)}</time></span>;
 const entry=(compact=false)=>canEnter?<Link aria-label={`进入实例 ${name}`} to={`/ieisystem/instances/${instance.id}`} state={{portalReturnSearch:window.location.search}} className={`instance-entry ${compact?'compact':''}`}><span>进入实例</span><ArrowRight size={15}/></Link>:<button disabled className="instance-entry"><span>正在删除</span></button>;
 return <article data-instance-id={instance.id} data-runtime={runtime.id} data-expanded={expanded} className={`portal-runtime-card ${selected?'is-selected':''}`} onClick={e=>{if(!(e.target as Element).closest('button,a'))onToggle()}} onKeyDown={e=>{if(e.key==='Escape')onCloseMenu()}} onBlur={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node))onCloseMenu()}}>
  <div className="instance-watermark-frame" aria-hidden="true">{logo&&<img className="instance-watermark" src={logo} alt=""/>}</div>
  <button type="button" className="instance-summary" aria-expanded={expanded} aria-controls={`instance-extra-${instance.id}`} onClick={onToggle}>
   <span className="instance-avatar">{logo?<img src={logo} alt=""/>:<Box size={38}/>}</span>
   <span className="instance-heading"><strong title={name}>{name}</strong><span className="instance-runtime-label">{runtime.name} · #{instance.id}</span>{expanded&&meta}</span>
  </button>
  {!expanded&&<div className="instance-bottom-row">{meta}<div className="instance-hover-entry" data-testid="instance-quick-actions">{entry(true)}</div></div>}
  <button type="button" className="instance-more" aria-label={`${name}的操作`} aria-haspopup="menu" aria-expanded={menuOpen} onClick={onMenu}><MoreHorizontal size={18}/></button>
  <div className="instance-expansion" id={`instance-extra-${instance.id}`} inert={!expanded}><div><div className="instance-description"><h4>{runtime.category}</h4><p>{content.text}</p><div className="instance-capabilities">{content.tags.map(tag=><span key={tag}>{tag}</span>)}</div><footer><button type="button" onClick={onToggle}>收起 <ChevronUp size={13}/></button>{entry()}</footer></div></div></div>
  {menuOpen&&<div className="instance-menu" role="menu" aria-label="实例操作"><button type="button" role="menuitem" onClick={()=>{onDetails();onCloseMenu()}}><Info size={15}/>实例详情</button>{canEnter&&<Link role="menuitem" to={`/ieisystem/instances/${instance.id}`} state={{portalReturnSearch:window.location.search}}><ArrowRight size={15}/>进入实例</Link>}<button type="button" role="menuitem" disabled={pending||!['running','error'].includes(instance.status.toLowerCase())} onClick={onRestart}><Power size={15}/>重启实例</button><button type="button" role="menuitem" className="danger" disabled={pending||!['running','stopped','error'].includes(instance.status.toLowerCase())} onClick={onReset}><RefreshCw size={15}/>重置实例</button></div>}
 </article>;
}
