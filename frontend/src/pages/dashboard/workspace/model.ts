import type { Agent, AgentState, Task } from './types.ts';
export interface Preferences { pins: number[] | null; read: string[] }
export const preferenceKey = (userID: number) => `cm:workspace:v1:${userID}`;
export function readPreferences(raw: string | null): Preferences {
 try {
  const value: unknown = JSON.parse(raw || 'null');
  if (!value || typeof value !== 'object') return { pins: null, read: [] };
  const { pins, read } = value as Record<string, unknown>;
  return {
   pins: Array.isArray(pins) ? [...new Set(pins.filter((id): id is number => Number.isSafeInteger(id) && id > 0))].slice(0, 6) : null,
   read: Array.isArray(read) ? [...new Set(read.filter((key): key is string => typeof key === 'string' && key.length < 1024))].slice(-1000) : [],
  };
 } catch { return { pins: null, read: [] }; }
}
export const validPins = (pins: number[] | null, agents: Pick<Agent, 'id'>[]) =>
 pins === null ? agents.slice(0, 6).map(agent => agent.id) : pins.filter(id => agents.some(agent => agent.id === id));
export const resultKey = (task: Task) => JSON.stringify([task.source, task.team_id ?? task.instance_id, task.id, task.finished_at || task.updated_at]);
export const agentRoute = (agent: Pick<Agent, 'id' | 'team_id'>) => agent.team_id ? `/teams/${agent.team_id}` : `/instances/${agent.id}`;
export function taskRoute(task: Task, agents: Agent[]): string | null {
 if (task.source === 'team') return agents.some(agent => agent.team_id === task.team_id) ? `/teams/${task.team_id}` : null;
 const agent = agents.find(agent => agent.id === task.instance_id);
 return agent ? agentRoute(agent) : null;
}
export type ResumeFilter = 'attention' | 'failed' | 'completed' | 'running';
export function resumeTasks(tasks: Task[], read: string[], filter: ResumeFilter): Task[] {
 const unread = (task: Task) => task.status === 'succeeded' && !read.includes(resultKey(task));
 return tasks.filter(task => filter === 'running' ? ['running', 'pending'].includes(task.status)
  : filter === 'failed' ? task.status === 'failed' : filter === 'completed' ? unread(task) : task.status === 'failed' || unread(task))
  .sort((a, b) => Number(b.status === 'failed') - Number(a.status === 'failed') || b.updated_at.localeCompare(a.updated_at));
}
export function chartSegments(values: Array<number | null>): Array<Array<{ index: number; value: number }>> {
 const segments: Array<Array<{ index: number; value: number }>> = [];
 let segment: Array<{ index: number; value: number }> = [];
 values.forEach((value, index) => {
  if (value === null || !Number.isFinite(value)) { segment = []; return; }
  if (!segment.length) segments.push(segment);
  segment.push({ index, value });
 });
 return segments;
}
export function formatNumber(value: number | null, locale: string): string {
 if (value === null || !Number.isFinite(value)) return '—';
 return new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(value);
}
export function formatMoney(value: number | null, currency: string, locale: string): string {
 if (value === null || !Number.isFinite(value)) return '—';
 const options: Intl.NumberFormatOptions = Math.abs(value) > 0 && Math.abs(value) < .01
  ? { maximumSignificantDigits: 3 } : { minimumFractionDigits: 2, maximumFractionDigits: 4 };
 try { return new Intl.NumberFormat(locale, { ...options, style: 'currency', currency, currencyDisplay: 'code' }).format(value); }
 catch { return `${new Intl.NumberFormat(locale, options).format(value)} ${currency}`; }
}
export function comparison(current: number | null, previous: number | null, complete: boolean): number | null {
 return complete && current !== null && previous !== null && previous > 0 ? (current - previous) / previous * 100 : null;
}
export const agentStates: AgentState[] = ['busy', 'idle', 'running', 'sleeping', 'starting', 'failed', 'offline', 'unknown'];
export function quotaGauge(assigned: number | null, limit: number | null) {
 const ratio = assigned !== null && Number.isFinite(assigned) && limit !== null && Number.isFinite(limit) && limit > 0 ? assigned / limit : null;
 return { ratio, needle: ratio === null ? null : Math.max(0, Math.min(1, ratio)) };
}
export function stateRing(counts: Partial<Record<AgentState, number>>) {
 const total = agentStates.reduce((sum, state) => sum + (counts[state] ?? 0), 0);
 let offset = 0;
 const running = (counts.busy ?? 0) + (counts.idle ?? 0) + (counts.running ?? 0);
 return { total, running, segments: agentStates.map(state => {
  const count = counts[state] ?? 0, share = total ? count / total : 0;
  const segment = { state, count, share, offset };
  offset += share;
  return segment;
 }) };
}
export const stateLabel = (state: AgentState, text: import('./text').WorkspaceText) => state === 'running' ? text.runtime : text[state];
export function usageRanking(rows: import('./types').Dashboard['agent_usage'], currency: string | null, available: boolean) {
 if (!available) return [];
 return rows.filter(row => currency === null || Object.hasOwn(row.costs, currency))
  .sort((a, b) => currency === null ? b.tokens - a.tokens : b.costs[currency] - a.costs[currency]);
}
export function reportingDate(value: string, timeZone: string, includeTime = false): string {
 const instant = new Date(value);
 const date = new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(instant);
 return includeTime ? `${date} ${new Intl.DateTimeFormat('en-GB', { timeZone, hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).format(instant)}` : date;
}
export function activityCounts(day: import('./types').Dashboard['activity'][number], source: import('./types').Coverage): {
 events: number | null; calls: number | null; tasks: number | null; status: import('./types').Coverage['status'];
} {
 if (source.status === 'unavailable') return { events: null, calls: null, tasks: null, status: 'unavailable' };
 return { events: day.events, calls: day.calls, tasks: day.tasks, status: source.status === 'partial' || !day.observed ? 'partial' : 'ready' };
}

// Horizontal tangents keep each segment within the range of its sampled endpoints.
export function smoothPath(points: Array<{ x: number; y: number }>): string {
 return points.map((point, index) => {
  if (!index) return `M ${point.x} ${point.y}`;
  const previous = points[index - 1], middle = (previous.x + point.x) / 2;
  return `C ${middle} ${previous.y} ${middle} ${point.y} ${point.x} ${point.y}`;
 }).join(' ');
}
