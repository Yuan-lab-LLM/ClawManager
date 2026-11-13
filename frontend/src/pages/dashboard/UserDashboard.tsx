import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { AlertCircle, CheckCircle2, Clock3, Monitor, Pin, X } from 'lucide-react';
import UserLayout from '../../components/UserLayout';
import { useAuth } from '../../contexts/AuthContext';
import { useI18n } from '../../contexts/I18nContext';
import { getWorkspace } from './workspace/service';
import { agentRoute, agentStates, stateLabel, reportingDate, activityCounts, formatMoney, formatNumber, preferenceKey, readPreferences, resultKey, resumeTasks, taskRoute, validPins } from './workspace/model';
import { Chart } from './workspace/Chart';
import type { Preferences, ResumeFilter } from './workspace/model';
import type { Dashboard, Task, UsageDashboard } from './workspace/types';
import { workspaceText } from './workspace/text';
import { CoverageNote, Operations } from './workspace/Operations';
import './workspace/workspace.css';

const agentLogos: Record<string, string | undefined> = {
 openclaw: '/openclaw.png', hermes: '/hermes.png', opencode: '/opencode.png',
 'deepseek-harness': '/deepseek-harness.svg', workbuddy: '/workbuddy.png',
 codex: '/codex.png', 'claude-code': '/claude-code.png',
};

function AgentTypeIcon({ type }: { type: string }) {
 const logo = agentLogos[type.trim().toLowerCase()];
 return typeof logo === 'string' ? <img src={logo} alt="" width={32} height={32} className="ws-agent-logo" /> : <Monitor size={32} className="ws-agent-logo" aria-hidden="true" />;
}

function Workspace({ userID, username }: { userID: number; username: string }) {
 const { locale } = useI18n();
 const text = workspaceText(locale);
 const [group, setGroup] = useState('operations');
 const [filter, setFilter] = useState<ResumeFilter>('attention');
 const [hidden, setHidden] = useState(document.hidden);
 const [preferences, setPreferences] = useState<Preferences>(() => {
  try { return readPreferences(localStorage.getItem(preferenceKey(userID))); } catch { return { pins: null, read: [] }; }
 });
 const [draft, setDraft] = useState<number[]>([]);
 const [storageError, setStorageError] = useState(false);
 const [task, setTask] = useState<Task | null>(null);
 const [usageDetails, setUsageDetails] = useState<UsageDashboard | null>(null);
 const [detailsTab, setDetailsTab] = useState<'usage' | 'capacity' | 'agents'>('usage');
 const [activityIndex, setActivityIndex] = useState(364);
 const pinDialog = useRef<HTMLDialogElement>(null);
 const taskDialog = useRef<HTMLDialogElement>(null);
 const detailsDialog = useRef<HTMLDialogElement>(null);
 const pinTrigger = useRef<HTMLButtonElement>(null);
 const taskTrigger = useRef<HTMLButtonElement | null>(null);
 const detailsTrigger = useRef<HTMLElement | null>(null);
 const resumeHeading = useRef<HTMLHeadingElement>(null);
 const heatPoints = useRef<Array<HTMLButtonElement | null>>([]);
 const query = useQuery({
  queryKey: ['workspace', userID], queryFn: ({ signal }) => getWorkspace('7d', signal),
  retry: false, staleTime: 15_000, gcTime: 0, refetchOnWindowFocus: 'always', refetchOnReconnect: 'always',
  refetchInterval: hidden ? false : 30_000, refetchIntervalInBackground: false,
 });
 useEffect(() => {
  const visibility = () => setHidden(document.hidden);
  document.addEventListener('visibilitychange', visibility);
  return () => document.removeEventListener('visibilitychange', visibility);
 }, []);
 const savePreferences = (next: Preferences) => {
  try { localStorage.setItem(preferenceKey(userID), JSON.stringify(next)); setPreferences(next); setStorageError(false); return true; }
  catch { setStorageError(true); return false; }
 };
 const data = query.data;
 const n = (value: number | null) => formatNumber(value, locale);
 const date = (value: string) => new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: 'Asia/Shanghai' }).format(new Date(value));
 const counts = data ? { failed: resumeTasks(data.tasks, preferences.read, 'failed').length, completed: resumeTasks(data.tasks, preferences.read, 'completed').length, running: resumeTasks(data.tasks, preferences.read, 'running').length } : null;
 const taskCount = (value: number) => n(data?.coverage.tasks.status === 'unavailable' ? null : value);
 const pins = data ? validPins(preferences.pins, data.instances) : [];
 const retained = (preferences.pins || []).filter(id => !data?.instances.some(agent => agent.id === id));
 const tasks = data ? resumeTasks(data.tasks, preferences.read, filter) : [];
 const closeTask = () => { taskDialog.current?.close(); if (taskTrigger.current?.isConnected) taskTrigger.current.focus(); else resumeHeading.current?.focus(); };
 const taskDestination = task && data ? taskRoute(task, data.instances) : null;
 const activity = data?.activity || [];
 const selectedDay = activity[Math.min(activityIndex, activity.length - 1)];
 const selectedCounts = selectedDay && data ? activityCounts(selectedDay, data.coverage.activity) : null;
 return <UserLayout title="Workspace" fillHeight>
  <div className={`ws-dashboard ${hidden ? 'ws-paused' : ''}`}>
   <header className="ws-toolbar"><div className="ws-welcome"><span className="ws-avatar" aria-hidden="true">{username.slice(0, 2).toLocaleUpperCase(locale)}</span><div><h1 title={`${text.welcome} ${username}`}><span className="ws-greeting">{text.welcome}</span> <strong>{username}</strong></h1></div></div></header>
   <nav className="ws-groups" aria-label={text.selectGroup}>{(['work', 'operations', 'activity'] as const).map(value => <button key={value} aria-pressed={group === value} onClick={() => setGroup(value)}>{text[value]}</button>)}</nav>
   {query.isError && <div role="alert" className="ws-error">{data ? text.stale : text.error}<button onClick={() => void query.refetch()}>{text.retry}</button></div>}
   <div className="ws-grid" data-group={group} aria-busy={query.isFetching}>
    <Operations data={data} userID={userID} paused={hidden} locale={locale} text={text} loading={query.isPending} onDetails={(tab, usage) => { setUsageDetails(usage ?? null); detailsTrigger.current = document.activeElement as HTMLElement; setDetailsTab(tab); detailsDialog.current?.showModal(); }} />
    {data ? <>
    <section className="ws-resume ws-panel" aria-labelledby="ws-resume-heading"><div className="ws-heading"><h2 id="ws-resume-heading" ref={resumeHeading} tabIndex={-1}>{text.resume}</h2></div>
     <div className="ws-recent-list">{data.recent_agents?.length ? data.recent_agents.map(agent => <Link key={agent.id} to={agentRoute(agent)} title={agent.name}><AgentTypeIcon type={agent.type} /><span><strong>{agent.name}</strong><small>{agent.type} · {agent.instance_mode} · {stateLabel(agent.state, text)}</small></span><span className="ws-recent-time"><time dateTime={agent.last_used_at}>{date(agent.last_used_at)}</time><small>{text[agent.activity_kind === 'model' ? 'calls' : agent.activity_kind === 'chat' ? 'chat' : agent.activity_kind === 'team' ? 'team' : 'task']}</small></span></Link>) : <p className="ws-empty">{data.coverage.recent?.status === 'unavailable' ? text.unavailable : text.noRecent}<Link to="/instances">{text.allAgents}</Link></p>}</div>

     <details className="ws-task-summary"><summary>{text.tasks} · {taskCount(counts!.failed)} {text.failed} · {taskCount(counts!.completed)} {text.completed} · {taskCount(counts!.running)} {text.running}</summary>
     <button className="ws-text" aria-pressed={filter === 'attention'} onClick={() => setFilter('attention')}>{text.attention} {taskCount(counts!.failed + counts!.completed)}</button>
     <div className="ws-counters" role="group" aria-label={text.resume}>{(['failed', 'completed', 'running'] as const).map(value => <button key={value} aria-pressed={filter === value} onClick={() => setFilter(value)}><strong>{taskCount(counts![value])}</strong><span>{text[value]}</span></button>)}</div>
     <div className="ws-resume-list">{tasks.length ? tasks.map(item => <button className={`ws-task ws-task-${item.status}`} key={resultKey(item)} onClick={event => { taskTrigger.current = event.currentTarget; setTask(item); setStorageError(false); taskDialog.current?.showModal(); }}>
      {item.status === 'failed' ? <AlertCircle size={15} /> : item.status === 'succeeded' ? <CheckCircle2 size={15} /> : <Clock3 size={15} className="ws-busy" />}
      <span className="ws-task-copy"><strong>{item.title || item.id}</strong><small>{item.agent_name} · {item.source === 'team' ? text.team : text.task}{item.status === 'pending' ? ` · ${text.pending}` : ''}</small>{['running', 'pending'].includes(item.status) && (item.progress === null ? <small>{text.progressUnknown}</small> : <progress max={100} value={Math.min(100, Math.max(0, item.progress))} aria-label={`${item.title}: ${item.progress}%`} />)}</span>
      <time dateTime={item.updated_at}>{date(item.updated_at)}</time>
     </button>) : <p className="ws-empty">{data.coverage.tasks.status === 'unavailable' ? text.unavailable : text.empty}</p>}</div>

     </details>
    </section>
    <section className="ws-pins ws-panel" aria-labelledby="ws-pins-heading"><div className="ws-heading"><h2 id="ws-pins-heading">{text.pins}</h2><button ref={pinTrigger} className="ws-text" onClick={() => { setDraft(preferences.pins ?? pins); setStorageError(false); pinDialog.current?.showModal(); }}>{text.manage}</button></div>
     {pins.length ? <div className="ws-pin-grid" data-count={pins.length}>{pins.map(id => {
      const agent = data.instances.find(agent => agent.id === id)!;
      return <Link key={id} to={agentRoute(agent)} className="ws-pin" title={`${agent.name} · ${agent.team_id ? text.openTeam : text.openAgent}`}><div><AgentTypeIcon type={agent.type} /><strong>{agent.name}</strong><Pin size={12} /></div><p><i className={`ws-dot ws-${agent.state}`} /><span>{stateLabel(agent.state, text)}</span><span>{agent.type} · {agent.instance_mode}</span></p></Link>;
     })}</div> : <div className="ws-empty">{data.instances_total === 0 && data.coverage.instances.status === 'ready' ? text.emptyAgents : text.emptyPins}{data.instances_total === 0 && <Link to="/instances/new">{text.create}</Link>}</div>}
     {retained.length > 0 && <p className="ws-note">{text.retainedPins}</p>}{data.instances_truncated && <p className="ws-note">{text.cap}</p>}
    </section>
    </> : <section className="ws-resume ws-panel"><p className="ws-empty" role="status">{query.isPending ? text.loading : text.unavailable}</p></section>}
    {data ? <section className="ws-activity ws-panel" aria-labelledby="ws-activity-heading"><div className="ws-activity-intro"><h2 id="ws-activity-heading">{text.activity}</h2><div className="ws-heat-legend">{text.less}<i /><i /><i />{text.more}</div></div><div className="ws-heat-wrap">{activity.length ? <><div className="ws-heat" role="group" aria-label={text.activity}>{activity.map((day, index) => {
      const counts = activityCounts(day, data.coverage.activity);
      const coverage = counts.status === 'ready' ? '' : ` · ${text[counts.status]}`;
      return <button key={day.date} ref={el => { heatPoints.current[index] = el; }} tabIndex={day === selectedDay ? 0 : -1} data-level={counts.status === 'unavailable' || !day.observed ? 'unknown' : day.events === 0 ? 0 : day.events < 3 ? 1 : day.events < 10 ? 2 : 3} aria-label={`${day.date}: ${n(counts.events)} ${text.events}, ${n(counts.calls)} ${text.calls}, ${n(counts.tasks)} ${text.tasks}${coverage}`} onFocus={() => setActivityIndex(index)} onClick={() => setActivityIndex(index)} onKeyDown={event => {
       const rows = getComputedStyle(event.currentTarget.parentElement!).gridTemplateRows.split(' ').length;
       const next = event.key === 'ArrowRight' ? index + rows : event.key === 'ArrowLeft' ? index - rows : event.key === 'ArrowDown' ? index + 1 : event.key === 'ArrowUp' ? index - 1 : event.key === 'Home' ? 0 : event.key === 'End' ? activity.length - 1 : null;
       if (next !== null) { event.preventDefault(); heatPoints.current[Math.max(0, Math.min(activity.length - 1, next))]?.focus(); }
      }}><title>{day.date}: {n(counts.events)} {text.events}{coverage}</title></button>;
     })}</div><output aria-live="polite">{selectedDay && selectedCounts && `${selectedDay.date} · ${n(selectedCounts.calls)} ${text.calls} · ${n(selectedCounts.tasks)} ${text.tasks}${selectedCounts.status === 'ready' ? '' : ` · ${text[selectedCounts.status]}`}`}</output></> : <p className="ws-empty">{data.coverage.activity.status === 'unavailable' ? text.unavailable : text.noActivity}</p>}</div></section> : <section className="ws-activity ws-panel"><p className="ws-empty" role="status">{query.isPending ? text.loading : text.unavailable}</p></section>}
   </div>
   <dialog ref={pinDialog} className="ws-dialog" aria-labelledby="ws-pin-title" onClose={() => pinTrigger.current?.focus()}><div className="ws-dialog-heading"><h2 id="ws-pin-title">{text.manage}</h2><button aria-label={text.close} onClick={() => pinDialog.current?.close()}><X size={18} /></button></div><p>{text.maxPins}</p><div className="ws-pin-options">{data?.instances.map(agent => <label key={agent.id}><input type="checkbox" checked={draft.includes(agent.id)} disabled={!draft.includes(agent.id) && draft.length >= 6} onChange={event => setDraft(event.target.checked ? [...draft, agent.id] : draft.filter(id => id !== agent.id))} /><span><strong>{agent.name}</strong><small>{agent.type} · {stateLabel(agent.state, text)}</small></span></label>)}</div>{retained.length > 0 && <p>{text.retainedPins}</p>}{data?.instances_truncated && <p>{text.cap}</p>}{storageError && <p className="ws-error" role="alert">{text.saveError}</p>}<div className="ws-dialog-actions"><button onClick={() => setDraft([])}>{text.clear}</button><span>{draft.length} / 6</span><button onClick={() => pinDialog.current?.close()}>{text.cancel}</button><button className="ws-primary" onClick={() => { if (savePreferences({ ...preferences, pins: draft })) pinDialog.current?.close(); }}>{text.save}</button></div></dialog>
   <dialog ref={taskDialog} className="ws-dialog" aria-labelledby="ws-task-title" onCancel={event => { event.preventDefault(); closeTask(); }}><div className="ws-dialog-heading"><h2 id="ws-task-title">{text.result}</h2><button aria-label={text.close} onClick={closeTask}><X size={18} /></button></div>{task && <><h3>{task.title || task.id}</h3><p>{task.agent_name} · {text[task.status === 'running' ? 'running' : task.status]} · {date(task.finished_at || task.updated_at)}</p><p className="ws-result">{task.error_message || task.result_excerpt || text.noResult}</p>{task.progress !== null && <progress aria-label={`${task.title}: ${task.progress}%`} max={100} value={task.progress} />}{!taskDestination && <p>{text.archived}</p>}{storageError && <p role="alert" className="ws-error">{text.saveError}</p>}<div className="ws-dialog-actions">{task.status === 'succeeded' && <button disabled={preferences.read.includes(resultKey(task))} onClick={() => savePreferences({ ...preferences, read: [...preferences.read, resultKey(task)].slice(-1000) })}>{preferences.read.includes(resultKey(task)) ? text.read : text.markRead}</button>}<Link className="ws-primary" to={taskDestination || (task.source === 'team' ? '/teams' : '/instances')}>{taskDestination ? task.source === 'team' ? text.openTeam : text.openAgent : task.source === 'team' ? text.allTeams : text.allAgents}</Link></div></>}</dialog>
   <dialog ref={detailsDialog} className="ws-dialog ws-details-dialog" aria-labelledby="ws-details-title" onClose={() => detailsTrigger.current?.focus()}><div className="ws-dialog-heading"><h2 id="ws-details-title">{detailsTab === 'usage' ? text.distribution : text[detailsTab]}</h2><button aria-label={text.close} onClick={() => detailsDialog.current?.close()}><X size={18} /></button></div>{data && <OperationDetails data={detailsTab === 'usage' && usageDetails ? { ...data, ...usageDetails, coverage: { ...data.coverage, ...usageDetails.coverage } } : data} locale={locale} tab={detailsTab} />}</dialog>
  </div>
 </UserLayout>;
}
function OperationDetails({ data, locale, tab }: { data: Dashboard; locale: string; tab: 'usage' | 'capacity' | 'agents' }) {
 const text = workspaceText(locale);
 const n = (value: number | null) => formatNumber(value, locale);
 if (tab === 'agents') return <><CoverageNote source={data.coverage.instances} text={text} timeZone={data.timezone} /><p>{text.stateHint}</p><ObservationDetails key={tab} data={data} locale={locale} tab={tab} />{data.instances_truncated && <p>{text.cap}</p>}<div className="ws-table-wrap"><table><thead><tr><th>{text.agent}</th><th>{text.state}</th><th>{text.cpu}</th><th>{text.memory}</th></tr></thead><tbody>{data.instances.map(agent => <tr key={agent.id}><td><Link to={agentRoute(agent)}>{agent.name}</Link></td><td>{stateLabel(agent.state, text)}</td><td>{n(agent.cpu_cores)}</td><td>{n(agent.memory_gb)} GB</td></tr>)}</tbody></table></div></>;
 if (tab === 'capacity') return <><CoverageNote source={data.coverage.capacity} text={text} timeZone={data.timezone} /><p>{text.capacityHint}</p><ObservationDetails key={tab} data={data} locale={locale} tab={tab} /><div className="ws-table-wrap"><table><thead><tr><th>{text.capacity}</th><th>{text.quota}</th></tr></thead><tbody><tr><td>{text.instances}</td><td>{n(data.instances_total)} / {n(data.capacity.max_instances)}</td></tr><tr><td>{text.cpu}</td><td>{n(data.capacity.allocated_cpu)} / {n(data.capacity.max_cpu)}</td></tr><tr><td>{text.memory}</td><td>{n(data.capacity.allocated_memory_gb)} / {n(data.capacity.max_memory_gb)} GB</td></tr><tr><td>{text.storage}</td><td>{n(data.capacity.allocated_storage_gb)} / {n(data.capacity.max_storage_gb)} GB</td></tr>{data.capacity.runtimes.map(runtime => <tr key={runtime.type}><td>{runtime.type} · {text.slots}</td><td>{n(data.coverage.capacity.status === 'unavailable' ? null : runtime.available)}</td></tr>)}</tbody></table></div></>;
 return <><p>{reportingDate(data.window.start, data.timezone, true)} – {reportingDate(data.window.end, data.timezone, true)} · {data.timezone} · {text.exclusiveEnd}</p><CoverageNote source={data.coverage.usage} text={text} timeZone={data.timezone} /><p>{text.costHint}</p>{data.usage.currencies.map(cost => <p key={cost.currency}>{formatMoney(data.coverage.costs.status === 'unavailable' ? null : cost.amount, cost.currency, locale)} · {data.coverage.costs.status === 'unavailable' ? text.unavailable : cost.complete ? text.ready : text.subtotal}{cost.estimated ? ` · ${text.estimated}` : ''}</p>)}<div className="ws-table-wrap"><table><thead><tr><th>{text.agent}</th><th>{text.tokens}</th>{data.usage.currencies.map(cost => <th key={cost.currency}>{cost.currency}</th>)}</tr></thead><tbody>{data.agent_usage.map(row => {
  const agent = data.instances.find(agent => agent.id === row.instance_id);
  return <tr key={row.instance_id}><td>{agent ? <Link to={agentRoute(agent)}>{agent.name}</Link> : text.archivedUsage}</td><td>{formatNumber(data.coverage.usage.status === 'unavailable' ? null : row.tokens, locale)}</td>{data.usage.currencies.map(cost => <td key={cost.currency}>{formatMoney(data.coverage.costs.status === 'unavailable' ? null : row.costs[cost.currency] ?? null, cost.currency, locale)}</td>)}</tr>;
 })}</tbody></table></div></>;
}
function ObservationDetails({data, locale, tab}: {data: Dashboard; locale: string; tab: 'capacity' | 'agents'}) {
 const text = workspaceText(locale);
 const options = tab === 'agents' ? agentStates.map(value => ({value, label: stateLabel(value,text)})) : [{value:'allocated_cpu',label:text.cpu},{value:'allocated_memory_gb',label:text.memory},{value:'eligible_slots',label:text.slots}];
 const [metric,setMetric] = useState(options[0].value);
 const source = data.coverage.observations;
 const values = data.observations.map(point => source.status === 'unavailable' ? null : point[metric as keyof Omit<typeof point,'at'>]);
 return <div className="ws-observation-details"><p>{text[data.range]} · {text.observation}</p><div className="ws-chart-controls"><select aria-label={text.selectMetric} value={metric} onChange={event => setMetric(event.target.value)}>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select><CoverageNote source={source} text={text} timeZone={data.timezone} /></div><Chart key={metric} values={values} times={data.observations.map(point => point.at)} label={options.find(option => option.value === metric)!.label} locale={locale} text={text} /></div>;
}
export default function UserDashboard() {
 const { user } = useAuth();
 // Remount account preferences, dialogs and local view state synchronously on identity changes.
 return user ? <Workspace key={user.id} userID={Number(user.id)} username={String(user.username || '')} /> : null;
}
