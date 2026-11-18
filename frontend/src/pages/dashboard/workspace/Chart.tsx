import { useEffect, useRef, useState } from 'react';
import { chartSegments, formatNumber, smoothPath } from './model';
import type { WorkspaceText } from './text';

type Series = { values: Array<number | null>; label: string; format: (value: number | null) => string; axisFormat?: (value: number) => string };
export function Chart({ values, times, label, locale, text, area = false, format = value => formatNumber(value, locale), secondary, showNote = true }: {
 values: Array<number | null>; times: string[]; label: string; locale: string; text: WorkspaceText; area?: boolean; format?: (value: number | null) => string; secondary?: Series; showNote?: boolean;
}) {
 const [active, setActive] = useState(0);
 const points = useRef<Array<SVGCircleElement | null>>([]);
 const chart = useRef<SVGSVGElement>(null);
 const [size, setSize] = useState({ width: 450, height: 180 });
 useEffect(() => {
  const observer = new ResizeObserver(([entry]) => {
   if (entry.contentRect.width > 0 && entry.contentRect.height > 0) setSize({ width: entry.contentRect.width, height: entry.contentRect.height });
  });
  if (chart.current) observer.observe(chart.current);
  return () => observer.disconnect();
 }, []);
 const series: Series[] = [{values,label,format}, ...(secondary ? [secondary] : [])];
 const known = times.flatMap((_, index) => series.some(row => row.values[index] !== null && Number.isFinite(row.values[index])) ? [index] : []);
 const selected = known.includes(active) ? active : known[0];
 const maxima = series.map(row => Math.max(0, ...row.values.filter((value): value is number => value !== null && Number.isFinite(value))) || 1);
 const axis = (row: Series, value: number) => row.axisFormat ? row.axisFormat(value) : row.format(value);
 const left = Math.min(96, Math.max(44, axis(series[0],maxima[0]).length * 7 + 6));
 const right = secondary ? Math.min(96, Math.max(48, axis(secondary,maxima[1]).length * 7 + 6)) : 16;
 const x = (index: number) => left + index / Math.max(1,times.length - 1) * Math.max(16,size.width - left - right);
 const y = (value: number, index: number) => size.height - 24 - value / maxima[index] * Math.max(16,size.height - 48);
 const date = (value: string) => new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', timeZone: 'Asia/Shanghai' }).format(new Date(value));
 const readout = (index: number) => series.map(row => `${row.label}: ${row.format(row.values[index] ?? null)}`).join(' · ');
 return <div className="ws-chart-wrap">
  <div className="ws-chart-readout"><span className="ws-chart-legends">{series.map((row,index) => <span key={index} className={index ? 'ws-chart-cost-label' : 'ws-chart-token-label'}>{row.label}</span>)}</span><output aria-live="polite">{selected !== undefined ? `${date(times[selected])} · ${readout(selected)}` : text.unavailable}</output></div>
  <svg ref={chart} className="ws-chart" viewBox={`0 0 ${size.width} ${size.height}`} role="group" aria-label={`${series.map(row => row.label).join(' / ')} · ${text.history}`}>
   {known.length > 0 && (size.height < 120 ? [0,1] : [0,.5,1]).map(ratio => <g key={ratio}>
    <line x1={left} x2={size.width - right} y1={y(maxima[0] * ratio,0)} y2={y(maxima[0] * ratio,0)} className="ws-chart-grid" />
    <text className="ws-chart-axis-left" x={left - 6} y={y(maxima[0] * ratio,0) - 4} textAnchor="end">{axis(series[0],maxima[0] * ratio)}</text>
    {secondary && <text className="ws-chart-axis-right" x={size.width - right + 6} y={y(maxima[1] * ratio,1) - 4}>{secondary.values.some(value=>value!==null&&Number.isFinite(value))?axis(secondary,maxima[1] * ratio):ratio===1?'—':''}</text>}
   </g>)}
   {series.map((row,seriesIndex) => <g key={seriesIndex} className={seriesIndex ? 'ws-chart-cost' : 'ws-chart-token'}>
    {chartSegments(row.values).map((segment,index) => {
     const path = smoothPath(segment.map(point => ({x:x(point.index),y:y(point.value,seriesIndex)})));
     return <g key={index}>
      {area && seriesIndex === 0 && segment.length > 1 && <path className="ws-chart-area" d={`${path} L ${x(segment[segment.length-1].index)} ${y(0,0)} L ${x(segment[0].index)} ${y(0,0)} Z`} aria-hidden="true" />}
      <path className="ws-chart-line" d={path} aria-hidden="true" />
     </g>;
    })}
    {row.values.map((value,index) => {
     if (value === null || !Number.isFinite(value)) return null;
     const keyboard = seriesIndex === 0 || values[index] === null || !Number.isFinite(values[index]);
     return <circle key={index} ref={element => {if(keyboard)points.current[index]=element;}} cx={x(index)} cy={y(value,seriesIndex)} r={selected === index ? 4 : 3} role="button" tabIndex={keyboard && selected === index ? 0 : -1} aria-label={`${date(times[index])}: ${readout(index)}`} onFocus={() => setActive(index)} onClick={() => setActive(index)} onKeyDown={event => {
      const position=known.indexOf(index);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? known.length-1 : event.key === 'ArrowRight' || event.key === 'ArrowDown' ? Math.min(known.length-1,position+1) : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? Math.max(0,position-1) : null;
      if(next!==null){event.preventDefault();points.current[known[next]]?.focus();}
      if(event.key==='Enter'||event.key===' '){event.preventDefault();setActive(index);}
     }}><title>{date(times[index])}: {readout(index)}</title></circle>;
    })}
   </g>)}
   {times.length > 0 && <><text x={left} y={size.height - 4}>{date(times[0])}</text><text x={size.width - right} y={size.height - 4} textAnchor="end">{date(times[times.length-1])}</text></>}
  </svg>
  {showNote && <p className="ws-note">{text.gap}</p>}
 </div>;
}
