import {CircleHelp, LoaderCircle, Unplug} from 'lucide-react';
import AgentStateIcon, {type AgentStateIconProps} from './AgentStateIcon';
import {getIEIRuntimePresentation, ownerRuntimeLogos} from '../lib/ieiRuntimeCatalog';
import {ownerAgentState} from '../lib/ownerAgentState';
import '../styles/owner-agent-state-icon.css';

const labels = {running:'运行中',sleeping:'自动休眠',stopped:'已停止',error:'运行异常',starting:'处理中',offline:'离线',unknown:'待确认'};

export function OwnerAgentStateIcon({type,status,stoppedReason,size=44,className='',decorative=false}: {
 type:string; status:string; stoppedReason?:string; size?:number; className?:string; decorative?:boolean;
}) {
 const state = ownerAgentState(status, stoppedReason);
 const extra = state==='starting' ? LoaderCircle : state==='offline' ? Unplug : state==='unknown' ? CircleHelp : null;
 const Extra = extra;
 return <span className={`owner-agent-icon ${className}`.trim()} data-owner-agent-state={state} style={{width:size,height:size}} aria-hidden={decorative || undefined}>
  <AgentStateIcon type={type as AgentStateIconProps['type']} state={Extra?'unknown':state} imageSrc={ownerRuntimeLogos[type]} size={size} label={`${getIEIRuntimePresentation(type).name} · ${labels[state]}`}/>
  {Extra && <span className="owner-agent-status-badge" aria-hidden="true"><Extra/></span>}
 </span>;
}
