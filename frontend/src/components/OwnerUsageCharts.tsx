import {useEffect,useRef,useState} from 'react';
import type {UsageDay} from '../services/ieiSystemService';
export const ownerNumber=(n:number)=>new Intl.NumberFormat('zh-CN',{notation:n>=10000?'compact':'standard',maximumFractionDigits:2}).format(n);
export const ownerCard='rounded-2xl border border-slate-200 bg-white p-4';
export function UsageChart({days,onDay,label='Token',height:fixedHeight=176,fit=false,maxTicks=6}:{days:UsageDay[];onDay?:(day:UsageDay)=>void;label?:string;height?:number;fit?:boolean;maxTicks?:number}){
 const [autoHeight,setAutoHeight]=useState(176);const height=fit?autoHeight:fixedHeight;
 const ref=useRef<HTMLDivElement>(null);const [width,setWidth]=useState(600);const [selected,setSelected]=useState<number|null>(null);
 useEffect(()=>{const el=ref.current;if(!el)return;const observer=new ResizeObserver(()=>{setWidth(Math.max(200,el.clientWidth));if(fit)setAutoHeight(Math.max(100,el.clientHeight))});observer.observe(el);return()=>observer.disconnect()},[fit]);
 const max=Math.max(1,...days.map(d=>d.tokens));const magnitude=10**Math.floor(Math.log10(max/3));const step=Math.max(1,Math.ceil(max/3/magnitude)*magnitude);const top=step*3;
 const left=44,right=12,bottom=height-25,upper=14;const x=(i:number)=>left+i*(width-left-right)/Math.max(1,days.length-1);const y=(n:number)=>bottom-n/top*(bottom-upper);
 const segments:UsageDay[][]=[];days.forEach(d=>{if(d.observed){if(!segments.length||!segments[segments.length-1].length)segments.push([]);segments[segments.length-1].push(d)}else if(segments.length&&segments[segments.length-1].length)segments.push([])});
 const active=selected==null?null:days[selected];const stride=days.length<=7?1:Math.ceil(days.length/maxTicks);
 return <div ref={ref} className={`relative min-w-0 ${fit?'owner-chart-fit':''}`} data-testid="usage-chart">
  <svg width="100%" height={height} role="img" aria-label={`${label} 每日趋势`} onPointerLeave={()=>setSelected(null)} onPointerMove={event=>{const box=event.currentTarget.getBoundingClientRect();setSelected(Math.max(0,Math.min(days.length-1,Math.round((event.clientX-box.left-left)/(width-left-right)*Math.max(1,days.length-1)))))}}>
   {[0,1,2,3].map(i=><g key={i}><line x1={left} x2={width-right} y1={y(i*step)} y2={y(i*step)} stroke="#e9eff8"/><text x={left-7} y={y(i*step)+3} textAnchor="end" fontSize="12" fill="#718096">{ownerNumber(i*step)}</text></g>)}
   {segments.filter(s=>s.length>0).map((segment,i)=>{const points=segment.map(d=>`${x(days.indexOf(d))},${y(d.tokens)}`).join(' ');return <g key={i}><polygon points={`${x(days.indexOf(segment[0]))},${bottom} ${points} ${x(days.indexOf(segment[segment.length-1]))},${bottom}`} fill="#dbeafe" opacity="0.65"/><polyline points={points} stroke="#2874ed" strokeWidth="2.3" fill="none" strokeLinejoin="round"/></g>})}
   {active&&<line x1={x(selected!)} x2={x(selected!)} y1={upper} y2={bottom} stroke="#93b9f7" strokeDasharray="3 3"/>}
   {days.map((d,i)=><g key={d.date}>{d.observed&&<circle cx={x(i)} cy={y(d.tokens)} r={i===selected?5:3.5} fill="white" stroke="#2874ed" strokeWidth="2" tabIndex={0} role="button" aria-label={`${d.date} ${d.tokens} ${label}`} onFocus={()=>setSelected(i)} onBlur={()=>setSelected(null)} onClick={()=>onDay?.(d)} onKeyDown={e=>{
    if(e.key==='Enter'||e.key===' '){e.preventDefault();onDay?.(d)}
    if(e.key==='ArrowRight'||e.key==='ArrowLeft'){
     e.preventDefault();const points=Array.from(ref.current?.querySelectorAll<SVGCircleElement>('circle')??[]);const at=points.indexOf(e.currentTarget);
     points[Math.max(0,Math.min(points.length-1,at+(e.key==='ArrowRight'?1:-1)))]?.focus();
    }
   }}/ >}{(i%stride===0||i===days.length-1)&&<text x={x(i)} y={height-6} textAnchor={i===days.length-1?'end':'middle'} fontSize="12" fill="#718096">{d.date.slice(5).replace('-','/')}</text>}</g>)}
  </svg>
  {active&&<div role="tooltip" className="pointer-events-none absolute right-2 top-0 z-10 rounded-lg border border-blue-100 bg-white/95 px-3 py-2 text-[13px] shadow-lg"><strong>{active.date}</strong>{active.observed?<><p>{label}：{active.tokens.toLocaleString()}</p>{label==='Token'&&<><p>输入 {active.prompt_tokens.toLocaleString()} / 输出 {active.completion_tokens.toLocaleString()}</p><p>有效提问 {active.questions} · 新增会话 {active.sessions}</p></>}</>:<p>该日无已确认记录，覆盖未知</p>}</div>}
  {!fit&&<p className="min-h-4 text-[12px] text-slate-500">悬停查看具体值{onDay?' · 点击数据点查看当日明细':''} · 缺失数据不连线</p>}
 </div>
}
export function ActivityHeatmap({days,onDay}:{days:UsageDay[];onDay:(day:UsageDay)=>void}){
 const [hover,setHover]=useState<UsageDay|null>(null);const offset=days.length?(new Date(days[0].date+'T00:00:00+08:00').getUTCDay()+6)%7:0;
 // UTC weekday at midnight Shanghai is the previous date; use date-only UTC.
 const pad=days.length?(new Date(days[0].date+'T00:00:00Z').getUTCDay()+6)%7:offset;
 const cells:(UsageDay|null)[]=[...Array(pad).fill(null),...days];while(cells.length%7)cells.push(null);
 const colors=['bg-slate-100','bg-teal-100','bg-teal-300','bg-teal-500'];const active=days.filter(d=>d.questions>0).length;
 const first=days.find(d=>d.observed)?.date;
 const inRange=(d:UsageDay)=>d.in_statistical_range??(!!first&&d.date>=first);
 const description=(d:UsageDay)=>d.observed?`${d.questions} 次提问 · ${ownerNumber(d.tokens)} Token`:inRange(d)?'暂无提问记录（仅表示平台已采集数据）':'统计开始前';
 return <section className={`owner-chart-card owner-heatmap relative`}><h3 className="text-base font-semibold">近 90 天活跃度</h3><div className="mt-2 flex items-baseline justify-between"><p className="text-xl font-medium">{active} <span className="text-sm">天活跃</span></p><span className="text-[13px] text-slate-500">有记录 {days.filter(d=>d.observed).length}/90 天</span></div>
 <div className="owner-heatmap-grid"><div className="grid h-[122px] grid-rows-7 text-[12px] text-slate-500">{['一','','三','','五','','日'].map((d,i)=><span key={i}>{d}</span>)}</div><div><div className="grid grid-flow-col grid-rows-7 gap-[3px]">{cells.map((d,i)=>d?<button key={d.date} aria-label={`${d.date} ${description(d)}`} onMouseEnter={()=>setHover(d)} onMouseLeave={()=>setHover(null)} onFocus={()=>setHover(d)} onBlur={()=>setHover(null)} onClick={()=>onDay(d)} className={`h-[14px] w-[14px] rounded-[3px] outline-offset-2 hover:ring-2 hover:ring-blue-400 ${!inRange(d)?'border border-dashed border-slate-200 bg-white':colors[d.questions===0?0:d.questions<3?1:d.questions<10?2:3]}`}/>:<span key={`pad-${i}`} className="h-[14px] w-[14px]"/>)}</div><div className="mt-2 flex justify-between text-[13px] text-slate-500">{[...new Set(days.map(d=>d.date.slice(0,7)))].map(m=><span key={m}>{Number(m.slice(5))}月</span>)}</div></div></div>
 <div className="mt-auto flex items-center justify-end gap-1 text-[13px] text-slate-500"><span>少</span>{colors.map(c=><i key={c} className={`h-2.5 w-2.5 rounded-sm ${c}`}/>)}<span>多 · 空白：统计前</span></div>
 {hover&&<div role="tooltip" className="pointer-events-none absolute left-4 top-10 z-10 rounded-lg border bg-white/95 p-2 text-sm shadow-lg">{hover.date}<p>{description(hover)}</p></div>}
 </section>
}
