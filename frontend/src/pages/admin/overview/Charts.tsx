import { useEffect, useRef, useState } from 'react';
import { chartSegments, smoothPath } from '../../dashboard/workspace/model';
import { percentOf, timeFraction } from './model';
import type { ResourceReading, Slice } from './types';
import type { OverviewText } from './text';
export const typeOrder = ['openclaw', 'hermes', 'opencode', 'deepseek-harness'];
export const colors = [
  'var(--cm-primary)',
  'var(--cm-data-starting)',
  'var(--cm-success)',
  'var(--cm-data-busy)',
  'var(--cm-data-offline)',
  'var(--cm-data-cost)',
];
export function typeColor(type: string) {
  const known = typeOrder.indexOf(type),
    hash = Array.from(type).reduce(
      (sum, character) => sum + character.charCodeAt(0),
      0,
    );
  return colors[known >= 0 ? known : 4 + (hash % 2)];
}
export function count(
  value: number | null | undefined,
  locale: string,
  compact = false,
): string {
  return typeof value === 'number' && Number.isFinite(value)
    ? new Intl.NumberFormat(locale, {
        maximumFractionDigits: compact ? 1 : 2,
        ...(compact ? { notation: 'compact' as const } : {}),
      }).format(value)
    : '—';
}
export function percent(value: number | null | undefined, locale: string) {
  return typeof value === 'number' && Number.isFinite(value)
    ? count(value, locale) + '%'
    : '—';
}
export function Pie({
  rows,
  label,
  locale,
  color,
  empty,
}: {
  rows: Slice[];
  label: string;
  locale: string;
  color: (row: Slice, index: number) => string;
  empty: string;
}) {
  if (!rows.length)
    return (
      <div className="ao-pie-empty" role="status">
        {empty}
      </div>
    );
  let start = -Math.PI / 2;
  return (
    <svg
      className="ao-pie"
      viewBox="0 0 240 240"
      role="img"
      aria-label={`${label}: ${rows.map((row) => `${row.name} ${count(row.count, locale)} (${percent(row.share, locale)})`).join('; ')}`}
    >
      {rows.map((row, index) => {
        const end = start + (row.share / 100) * Math.PI * 2,
          mid = (start + end) / 2,
          x = 120 + 108 * Math.cos(start),
          y = 120 + 108 * Math.sin(start),
          ex = 120 + 108 * Math.cos(end),
          ey = 120 + 108 * Math.sin(end);
        start = end;
        return (
          <g key={row.key}>
            {row.share >= 99.999 ? (
              <circle cx="120" cy="120" r="108" fill={color(row, index)}>
                <title>
                  {row.name}: {count(row.count, locale)} ·{' '}
                  {percent(row.share, locale)}
                </title>
              </circle>
            ) : (
              <path
                d={`M 120 120 L ${x} ${y} A 108 108 0 ${row.share > 50 ? 1 : 0} 1 ${ex} ${ey} Z`}
                fill={color(row, index)}
              >
                <title>
                  {row.name}: {count(row.count, locale)} ·{' '}
                  {percent(row.share, locale)}
                </title>
              </path>
            )}
            {row.share >= 10 && (
              <text x={120 + 70 * Math.cos(mid)} y={120 + 70 * Math.sin(mid)}>
                {percent(Math.round(row.share * 10) / 10, locale)}
              </text>
            )}
          </g>
        );
      })}
    </svg>
  );
}
export function Gauge({
  value,
  label,
  unit,
  locale,
  text,
}: {
  value?: ResourceReading;
  label: string;
  unit: string;
  locale: string;
  text: OverviewText;
}) {
  const actual = percentOf(value?.used, value?.capacity),
    request = percentOf(value?.requested, value?.allocatable),
    limit = (n: number) => Math.max(0, Math.min(100, n));
  const angle =
      Math.PI + ((request === null ? 0 : limit(request)) / 100) * Math.PI,
    mark = { x: 90 + Math.cos(angle) * 54, y: 96 + Math.sin(angle) * 54 };
  return (
    <article className="ao-resource" aria-label={label}>
      <h3>{label}</h3>
      <svg
        className="ao-gauge"
        viewBox="0 0 180 125"
        role="img"
        aria-label={`${label}: ${text.actual} ${percent(actual, locale)}, ${text.requested} ${percent(request, locale)}`}
      >
        <path className="ao-gauge-track" d="M20 96 A70 70 0 0 1 160 96" />
        <path
          className="ao-gauge-zone ao-green"
          d="M20 96 A70 70 0 0 1 160 96"
          pathLength="100"
          strokeDasharray="64 100"
        />
        <path
          className="ao-gauge-zone ao-amber"
          d="M20 96 A70 70 0 0 1 160 96"
          pathLength="100"
          strokeDasharray="17 100"
          strokeDashoffset="-66"
        />
        <path
          className="ao-gauge-zone ao-red"
          d="M20 96 A70 70 0 0 1 160 96"
          pathLength="100"
          strokeDasharray="15 100"
          strokeDashoffset="-85"
        />
        <path
          className="ao-gauge-request-track"
          d="M36 96 A54 54 0 0 1 144 96"
        />
        {Array.from({ length: 11 }, (_, i) => (
          <line
            key={i}
            className="ao-gauge-tick"
            x1="29"
            y1="96"
            x2={i % 5 ? 34 : 38}
            y2="96"
            transform={`rotate(${i * 18} 90 96)`}
          />
        ))}
        {actual !== null && (
          <g
            className="ao-gauge-needle"
            transform={`rotate(${limit(actual) * 1.8} 90 96)`}
          >
            <path d="M90 92 L43 96 L90 100Z" />
            <circle cx="90" cy="96" r="4" />
          </g>
        )}
        {request !== null && (
          <path
            className="ao-gauge-marker"
            d={`M ${mark.x} ${mark.y - 5} L ${mark.x + 4} ${mark.y} L ${mark.x} ${mark.y + 5} L ${mark.x - 4} ${mark.y}Z`}
          >
            <title>
              {text.requested} {percent(request, locale)}
            </title>
          </path>
        )}
        <text
          className={
            actual !== null && actual > 85
              ? 'ao-gauge-value ao-hot'
              : 'ao-gauge-value'
          }
          x="90"
          y="123"
          textAnchor="middle"
        >
          {percent(actual, locale)}
        </text>
      </svg>
      <div className="ao-gauge-readings">
        {(['actual', 'requested'] as const).map((kind) => {
          const measured = kind === 'actual',
            ratio = measured ? actual : request,
            v = measured ? value?.used : value?.requested,
            den = measured ? value?.capacity : value?.allocatable;
          return (
            <div
              key={kind}
              role={ratio === null ? 'img' : 'meter'}
              aria-label={`${label} ${text[kind]}`}
              aria-valuemin={ratio === null ? undefined : 0}
              aria-valuemax={ratio === null ? undefined : 100}
              aria-valuenow={ratio === null ? undefined : limit(ratio)}
              aria-valuetext={`${percent(ratio, locale)}: ${count(v, locale)} / ${count(den, locale)} ${unit}`}
            >
              <span className={`ao-reading-label ao-${kind}`}>
                <i />
                {text[kind]}
              </span>
              <span>
                <strong>{count(v, locale)}</strong> / {count(den, locale)}{' '}
                {unit}
                {!measured && <b>{percent(ratio, locale)}</b>}
              </span>
            </div>
          );
        })}
      </div>
    </article>
  );
}
export interface Series {
  label: string;
  values: Array<number | null>;
  format: (value: number | null) => string;
  className: string;
}
export function Trend({
  series,
  times,
  locale,
  sharedAxis = false,
  empty,
}: {
  series: Series[];
  times: string[];
  locale: string;
  sharedAxis?: boolean;
  empty: string;
}) {
  const wrap = useRef<HTMLDivElement>(null),
    refs = useRef<Array<SVGGElement | null>>([]),
    [size, setSize] = useState({ width: 600, height: 220 }),
    [active, setActive] = useState(-1);
  useEffect(() => {
    const observer = new ResizeObserver(([entry]) => {
      if (entry.contentRect.width && entry.contentRect.height)
        setSize({
          width: entry.contentRect.width,
          height: entry.contentRect.height,
        });
    });
    if (wrap.current) observer.observe(wrap.current);
    return () => observer.disconnect();
  }, []);
  const maxima = series.map((row) =>
    Math.max(
      1,
      ...row.values.filter(
        (v): v is number => v !== null && Number.isFinite(v),
      ),
    ),
  );
  if (sharedAxis)
    maxima.fill(Math.max(20, Math.ceil(Math.max(...maxima) / 20) * 20));
  const known = times.flatMap((_, index) =>
      series.some(
        (row) =>
          row.values[index] !== null && Number.isFinite(row.values[index]),
      )
        ? [index]
        : [],
    ),
    selected = known.includes(active) ? active : known.at(-1);
  const left = Math.min(
      84,
      Math.max(38, series[0].format(maxima[0]).length * 6.5),
    ),
    right =
      !sharedAxis && series[1]
        ? Math.min(84, Math.max(38, series[1].format(maxima[1]).length * 6.5))
        : 18;
  const x = (index: number) =>
      left +
      timeFraction(times, index) * Math.max(10, size.width - left - right),
    y = (v: number, s: number) =>
      size.height - 26 - (v / maxima[s]) * Math.max(16, size.height - 50);
  const date = (at: string) =>
    new Intl.DateTimeFormat(locale, {
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
      timeZone: 'Asia/Shanghai',
    }).format(new Date(at));
  const readout = (i: number) =>
    series
      .map((row) => `${row.label} ${row.format(row.values[i] ?? null)}`)
      .join(' · ');
  return (
    <div className="ao-trend-body">
      <div className="ao-trend-legend">
        {series.map((row) => (
          <span className={row.className} key={row.label}>
            <i />
            {row.label}
          </span>
        ))}
      </div>
      <div className="ao-plot" ref={wrap}>
        {!known.length && (
          <p className="ao-chart-empty" role="status">
            {empty}
          </p>
        )}
        <svg
          className="ao-trend-svg"
          viewBox={`0 0 ${size.width} ${size.height}`}
          role="group"
          aria-label={series.map((row) => row.label).join(' / ')}
        >
          {known.length > 0 &&
            [0, 0.5, 1].map((ratio) => (
              <g key={ratio}>
                <line
                  className="ao-chart-grid"
                  x1={left}
                  x2={size.width - right}
                  y1={y(maxima[0] * ratio, 0)}
                  y2={y(maxima[0] * ratio, 0)}
                />
                <text
                  className={`ao-chart-axis ${series[0].className}`}
                  x={left - 6}
                  y={y(maxima[0] * ratio, 0) + 4}
                  textAnchor="end"
                >
                  {series[0].format(maxima[0] * ratio)}
                </text>
                {!sharedAxis && series[1] && (
                  <text
                    className={`ao-chart-axis ${series[1].className}`}
                    x={size.width - right + 6}
                    y={y(maxima[0] * ratio, 0) + 4}
                  >
                    {series[1].values.some((v) => v !== null)
                      ? series[1].format(maxima[1] * ratio)
                      : ratio === 1
                        ? '—'
                        : ''}
                  </text>
                )}
              </g>
            ))}
          {series.map((row, si) => (
            <g className={row.className} key={row.label}>
              {chartSegments(row.values).map((segment, index) => {
                const path = smoothPath(
                  segment.map((p) => ({ x: x(p.index), y: y(p.value, si) })),
                );
                return (
                  <g key={index}>
                    {si === 0 && segment.length > 1 && (
                      <path
                        className="ao-chart-area"
                        d={`${path} L ${x(segment.at(-1)!.index)} ${y(0, si)} L ${x(segment[0].index)} ${y(0, si)} Z`}
                      />
                    )}
                    <path className="ao-chart-line" d={path} />
                  </g>
                );
              })}
              {row.values.map((v, i) =>
                v === null ? null : (
                  <circle
                    key={i}
                    className="ao-chart-dot"
                    cx={x(i)}
                    cy={y(v, si)}
                    r={selected === i ? 4 : 2.5}
                  />
                ),
              )}
            </g>
          ))}
          {known.map((i) => (
            <g
              key={i}
              ref={(el) => {
                refs.current[i] = el;
              }}
              className="ao-chart-point"
              role="button"
              tabIndex={i === selected ? 0 : -1}
              aria-label={`${date(times[i])} · ${readout(i)}`}
              onFocus={() => setActive(i)}
              onPointerOver={() => setActive(i)}
              onClick={() => setActive(i)}
              onKeyDown={(event) => {
                const position = known.indexOf(i),
                  next =
                    event.key === 'Home'
                      ? 0
                      : event.key === 'End'
                        ? known.length - 1
                        : event.key === 'ArrowRight'
                          ? Math.min(known.length - 1, position + 1)
                          : event.key === 'ArrowLeft'
                            ? Math.max(0, position - 1)
                            : null;
                if (next !== null) {
                  event.preventDefault();
                  setActive(known[next]);
                  refs.current[known[next]]?.focus();
                }
              }}
            >
              {selected === i && (
                <line
                  className="ao-crosshair"
                  x1={x(i)}
                  x2={x(i)}
                  y1="20"
                  y2={size.height - 26}
                />
              )}
              <rect
                x={i === 0 ? left : (x(i - 1) + x(i)) / 2}
                y="20"
                width={Math.max(
                  1,
                  (i === times.length - 1
                    ? size.width - right
                    : (x(i) + x(i + 1)) / 2) -
                    (i === 0 ? left : (x(i - 1) + x(i)) / 2),
                )}
                height={Math.max(20, size.height - 46)}
              />
              <title>
                {date(times[i])} · {readout(i)}
              </title>
            </g>
          ))}
          {times.length > 0 && (
            <>
              <text className="ao-chart-axis" x={left} y={size.height - 5}>
                {date(times[0])}
              </text>
              <text
                className="ao-chart-axis"
                x={size.width - right}
                y={size.height - 5}
                textAnchor="end"
              >
                {date(times.at(-1)!)}
              </text>
            </>
          )}
        </svg>
      </div>
      <output className="ao-trend-readout" aria-live="polite">
        {selected === undefined
          ? empty
          : `${date(times[selected])} · ${readout(selected)}`}
      </output>
    </div>
  );
}
