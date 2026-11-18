import type { ResourceKey, ResourceSample, Slice } from './types.ts';
const keys: ResourceKey[] = ['cpu', 'memory', 'disk'];
export function percentOf(
  value: number | null | undefined,
  total: number | null | undefined,
): number | null {
  return typeof value === 'number' &&
    Number.isFinite(value) &&
    value >= 0 &&
    typeof total === 'number' &&
    Number.isFinite(total) &&
    total > 0
    ? (value / total) * 100
    : null;
}
export function resourceSample(
  source: Record<
    ResourceKey,
    {
      used?: number | null;
      capacity: number;
      requested: number;
      allocatable: number;
    }
  >,
  at: string,
): ResourceSample {
  return {
    at,
    ...Object.fromEntries(
      keys.map((key) => [
        key,
        {
          actual: percentOf(source[key].used, source[key].capacity),
          requested: percentOf(source[key].requested, source[key].allocatable),
        },
      ]),
    ),
  } as ResourceSample;
}
export function distribution<T extends { type: string }>(
  rows: T[],
  field: keyof T,
): Slice[] {
  const known = rows.filter(
      (row) =>
        typeof row[field] === 'number' &&
        Number.isFinite(row[field]) &&
        Number(row[field]) > 0,
    ),
    total = known.reduce((sum, row) => sum + Number(row[field]), 0);
  return known.map((row) => ({
    key: row.type,
    name: row.type,
    count: Number(row[field]),
    share: (Number(row[field]) / total) * 100,
  }));
}
export function modelDistribution(
  rows: Array<{ name: string; tokens: number }>,
  other: string,
  limit = 6,
): Slice[] {
  const sorted = rows
    .filter((row) => Number.isFinite(row.tokens) && row.tokens > 0)
    .toSorted((a, b) => b.tokens - a.tokens || a.name.localeCompare(b.name));
  const visible =
    sorted.length > limit
      ? [
          ...sorted.slice(0, limit - 1),
          {
            name: other,
            tokens: sorted
              .slice(limit - 1)
              .reduce((sum, row) => sum + row.tokens, 0),
          },
        ]
      : sorted;
  const total = visible.reduce((sum, row) => sum + row.tokens, 0);
  return visible.map((row, i) => ({
    key: String(i),
    name: row.name,
    count: row.tokens,
    share: (row.tokens / total) * 100,
  }));
}
export function appendSample(
  previous: ResourceSample[],
  sample: ResourceSample,
): ResourceSample[] {
  const at = Date.parse(sample.at);
  if (!Number.isFinite(at)) return previous;
  const latest = previous.at(-1);
  if (latest && Date.parse(latest.at) > at) return previous;
  if (latest?.at === sample.at) return [...previous.slice(0, -1), sample];
  const kept = previous.filter(
    (point) =>
      Date.parse(point.at) > at - 30 * 60 * 1000 && Date.parse(point.at) < at,
  );
  const last = kept.at(-1);
  if (last && at - Date.parse(last.at) > 90_000) {
    const missing = { actual: null, requested: null };
    kept.push({
      at: new Date((at + Date.parse(last.at)) / 2).toISOString(),
      cpu: missing,
      memory: missing,
      disk: missing,
    });
  }
  return [...kept, sample].slice(-121);
}
export function timeFraction(times: string[], index: number): number {
  const first = Date.parse(times[0]),
    last = Date.parse(times.at(-1)!),
    at = Date.parse(times[index]);
  return last > first && Number.isFinite(at)
    ? Math.max(0, Math.min(1, (at - first) / (last - first)))
    : 0;
}
