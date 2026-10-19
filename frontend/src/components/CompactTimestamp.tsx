import {useId,useState} from 'react';
import {createPortal} from 'react-dom';
import {compactDisplayTime,fullDisplayTime} from '../lib/compactDisplayTime';

export function CompactTimestamp({value,locale,className=''}:{value?:string;locale?:string;className?:string}) {
 const id=useId();const [position,setPosition]=useState<{left:number;top:number}|null>(null);
 const show=(element:HTMLElement)=>{const r=element.getBoundingClientRect();setPosition({left:Math.max(8,Math.min(r.left,window.innerWidth-290)),top:Math.max(8,r.top-38)})};
 const full=fullDisplayTime(value,locale);
 return <><time className={className} tabIndex={0} aria-label={full} aria-describedby={position?id:undefined} title={full} onMouseEnter={e=>show(e.currentTarget)} onMouseLeave={e=>{if(document.activeElement!==e.currentTarget)setPosition(null)}} onFocus={e=>show(e.currentTarget)} onBlur={e=>{if(!e.currentTarget.matches(':hover'))setPosition(null)}} onKeyDown={e=>{if(e.key==='Escape')setPosition(null)}}>{compactDisplayTime(value)}</time>{position&&createPortal(<span id={id} role="tooltip" className="pointer-events-none fixed z-50 rounded-md border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700 shadow-lg" style={position}>{full}</span>,document.body)}</>;
}
