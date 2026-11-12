import { useState } from 'react';
import { Power } from 'lucide-react';
import type { TeamRuntimeState } from '../../types/team';

export function teamPowerLabel(state?: TeamRuntimeState) {
  if (!state || state.state === 'running') return '';
  const labels: Record<string,string> = { stopped: '已停止', stopping: '正在停止', starting: '正在启动', recovery_required: '启停待处理' };
  const progress = ['starting','stopping'].includes(state.state) ? ` · ${state.progress}/${state.total}` : '';
  return `${labels[state.state] || state.state}${state.reason === 'idle_timeout' && state.state === 'stopped' ? ' · 自动休眠' : ''}${progress}`;
}

export default function TeamPowerControl({ state, start, stop, onChange, className = 'ot-secondary' }: {
  state?: TeamRuntimeState; start: () => Promise<TeamRuntimeState>; stop?: () => Promise<TeamRuntimeState>;
  onChange: () => void | Promise<void>; className?: string;
}) {
  const [busy,setBusy] = useState(false), [error,setError] = useState('');
  const active = !state || state.state === 'running';
  const transition = state?.state === 'starting' || state?.state === 'stopping';
  if (active && !stop) return null;
  const run = async (continueStop = false) => {
    if (busy || transition) return;
    if ((active || (continueStop && state?.desired_state !== 'stopped')) && !window.confirm('停止整个团队？有未完成任务时不会执行停止。')) return;
    setBusy(true); setError('');
    try { await (active || continueStop ? stop!() : start()); await onChange(); }
    catch (e) { const response = (e as {response?: {data?: {message?: string; error?: string}}})?.response?.data; setError(response?.message || response?.error || (e instanceof Error ? e.message : '操作结果暂未确认，请刷新查看原操作')); }
    finally { setBusy(false); }
  };
  return <span className="inline-flex max-w-full flex-wrap items-center gap-2">
    <button type="button" className={className} disabled={busy || transition} onClick={() => void run()}><span>{busy ? '正在提交…' : transition ? teamPowerLabel(state) : active ? '停止团队' : '启动团队'}</span>{className.includes('ot-card-entry') && <Power size={14}/>}</button>
    {state?.state === "recovery_required" && stop && <button type="button" className={className} disabled={busy} onClick={() => void run(true)}>{state.desired_state === "stopped" ? "核实并继续停止" : "停止团队"}</button>}
    {error && <span role="alert" className="text-xs text-red-600">{error}</span>}
  </span>;
}
