import { useEffect, useRef, useState } from 'react';
import { chartSegments, formatNumber, smoothPath } from './model';
import type { WorkspaceText } from './text';

type Series = { values: Array<number | null>; label: string; format: (value: number | null) => string; axisFormat?: (value: number) => string };

function roundAxisLimit(value: number) {
 const magnitude = Math.abs(value);
 if (magnitude === 0) return 0;
 const step = 10 ** Math.floor(Math.log10(magnitude));
 const ceiling = Math.ceil(magnitude / step - 1e-9) * step;
 return Math.sign(value) * Math.max(magnitude, ceiling);
}

export function Chart({ values, times, label, locale, timeZone, text, area = false, format = value => formatNumber(value, locale), secondary, showNote = true }: {
 values: Array<number | null>; times: string[]; label: string; locale: string; timeZone: string; text: WorkspaceText;
 area?: boolean; format?: (value: number | null) => string; secondary?: Series; showNote?: boolean;
}) {
 const [active, setActive] = useState<number | null>(null);
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
 const selected = active !== null && known.includes(active) ? active : known.at(-1);
 const domains = series.map(row => {
  const samples = row.values.filter((value): value is number => value !== null && Number.isFinite(value));
  const min = secondary ? roundAxisLimit(Math.min(0, ...samples)) : Math.min(0, ...samples);
  const max = secondary ? roundAxisLimit(Math.max(0, ...samples)) : Math.max(0, ...samples);
  return { min, max: min === max ? min + 1 : max, count: samples.length };
 });
 const axis = (row: Series, value: number) => row.axisFormat ? row.axisFormat(value) : row.format(value);
 const axisLabelWidth = (row: Series, index: number) =>
  7 * Math.max(axis(row, domains[index].min).length, axis(row, domains[index].max).length) + 6;
 const left = Math.min(96, Math.max(44, axisLabelWidth(series[0],0)));
 const right = secondary ? Math.min(96, Math.max(48, axisLabelWidth(secondary,1))) : 16;
 const x = (index: number) => left + index / Math.max(1,times.length - 1) * Math.max(16,size.width - left - right);
 const compact = size.height < 100;
 const plotTop = compact ? 14 : 24;
 const plotBottom = size.height - (compact ? 18 : 24);
 const plotHeight = Math.max(1, plotBottom - plotTop);
 const y = (value: number, index: number) => {
  const { min, max } = domains[index];
  return plotBottom - (value - min) / (max - min) * plotHeight;
 };
 const axisValue = (ratio: number, index: number) => domains[index].min + (domains[index].max - domains[index].min) * ratio;
 const day = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone });
 const time = new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', timeZone });
 const dateTime = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone });
 const date = (value: string) => dateTime.format(new Date(value));
 const first = times[0] ? new Date(times[0]) : null;
 const last = times.length ? new Date(times[times.length - 1]) : null;
 const xLabel = first && last && day.format(first) === day.format(last) ? time : day;
 const readout = (index: number) => series.map(row => `${row.label}: ${row.format(row.values[index] ?? null)}`).join(' · ');
 const currentReading = selected !== undefined ? `${date(times[selected])} · ${readout(selected)}` : text.unavailable;
 return <div className="ws-chart-wrap">
  <div className="ws-chart-readout"><span className="ws-chart-legends">{series.map((row,index) => {
   const single = index === 1 && domains[index].count === 1;
   return <span key={index} className={index ? 'ws-chart-cost-label' : 'ws-chart-token-label'} data-single={single || undefined}>
    {row.label}{single && <small>{text.singleSample}</small>}
   </span>;
  })}</span><output aria-live="polite" title={currentReading}>{currentReading}</output></div>
  <svg ref={chart} className="ws-chart" viewBox={`0 0 ${size.width} ${size.height}`} role="group" aria-label={`${series.map(row => row.label).join(' / ')} · ${text.history}`}>
   {known.length > 0 && (plotHeight < 72 ? [0,1] : [0,.5,1]).map(ratio => {
    const offset = plotBottom - ratio * plotHeight;
    return <g key={ratio}>
     <line x1={left} x2={size.width - right} y1={offset} y2={offset} className="ws-chart-grid" />
     {series.map((row,index) => (domains[index].count || ratio === 1) && <text key={index}
      className={index ? 'ws-chart-axis-right' : 'ws-chart-axis-left'}
      x={index ? size.width - right + 6 : left - 6} y={Math.max(10,offset - 4)} textAnchor={index ? 'start' : 'end'}>
      {domains[index].count ? axis(row,axisValue(ratio,index)) : '—'}
     </text>)}
    </g>;
   })}
   {series.map((row,seriesIndex) => <g key={seriesIndex} className={seriesIndex ? 'ws-chart-cost' : 'ws-chart-token'}>
    {chartSegments(row.values).filter(segment => segment.length > 1).map((segment,index) => {
     const path = smoothPath(segment.map(point => ({x:x(point.index),y:y(point.value,seriesIndex)})));
     return <g key={index}>
      {area && seriesIndex === 0 && <path className="ws-chart-area" d={`${path} L ${x(segment[segment.length-1].index)} ${y(0,0)} L ${x(segment[0].index)} ${y(0,0)} Z`} aria-hidden="true" />}
      <path className="ws-chart-line" d={path} aria-hidden="true" />
     </g>;
    })}
    {row.values.map((value,index) => {
     if (value === null || !Number.isFinite(value)) return null;
     const keyboard = seriesIndex === 0 || values[index] === null || !Number.isFinite(values[index]);
     const radius = (seriesIndex ? 5 : 3) + (selected === index ? 1 : 0);
     return <circle key={index} ref={element => {if(keyboard)points.current[index]=element;}} cx={x(index)} cy={y(value,seriesIndex)} r={radius} role="button" tabIndex={keyboard && selected === index ? 0 : -1} aria-label={`${date(times[index])}: ${readout(index)}`} onFocus={() => setActive(index)} onClick={() => setActive(index)} onKeyDown={event => {
      const position=known.indexOf(index);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? known.length-1 : event.key === 'ArrowRight' || event.key === 'ArrowDown' ? Math.min(known.length-1,position+1) : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? Math.max(0,position-1) : null;
      if(next!==null){event.preventDefault();points.current[known[next]]?.focus();}
      if(event.key==='Enter'||event.key===' '){event.preventDefault();setActive(index);}
     }}><title>{date(times[index])}: {readout(index)}</title></circle>;
    })}
   </g>)}
   {first && last && <>
    <text x={left} y={size.height - 4}>{xLabel.format(first)}</text>
    {times.length > 1 && <text x={size.width - right} y={size.height - 4} textAnchor="end">{xLabel.format(last)}</text>}
   </>}
  </svg>
  {showNote && <p className="ws-note">{text.gap}</p>}
 </div>;
}
