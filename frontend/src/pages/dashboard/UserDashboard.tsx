import { useEffect, useRef, useState, type CSSProperties, type MouseEventHandler } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Checkbox, Dialog, DialogActions, DialogBody, DialogContent, DialogSurface, DialogTitle, Dropdown, MessageBar, MessageBarActions, MessageBarBody, Option, ProgressBar, Skeleton, SkeletonItem, Table, TableBody, TableCell, TableHeader, TableHeaderCell, TableRow, ToggleButton } from '@fluentui/react-components';
import { ArrowRight16Regular, CheckmarkCircle16Regular, ChevronRight16Regular, Clock16Regular, Dismiss20Regular, ErrorCircle16Regular, Pin20Regular, Window32Regular } from '@fluentui/react-icons';
import UserLayout from '../../components/UserLayout';
import { useAuth } from '../../contexts/AuthContext';
import { useI18n } from '../../contexts/I18nContext';
import { instanceService } from '../../services/instanceService';
import { getWorkspace } from './workspace/service';
import { agentRoute, agentStates, stateLabel, reportingDate, activityCounts, comparison, formatMoney, formatNumber, preferenceKey, readPreferences, resultKey, resumeTasks, taskRoute, validPins } from './workspace/model';
import { Chart } from './workspace/Chart';
import type { Preferences, ResumeFilter } from './workspace/model';
import type { Dashboard, Task, UsageDashboard } from './workspace/types';
import { workspaceText, type WorkspaceText } from './workspace/text';
import { AgentStates, CoverageNote, Operations } from './workspace/Operations';
import './workspace/workspace.css';

const agentLogos: Record<string, string | undefined> = {
 openclaw: '/openclaw.png', hermes: '/hermes.png', opencode: '/opencode.png',
 'deepseek-harness': '/deepseek-harness.svg', workbuddy: '/workbuddy.png',
 codex: '/codex.png', 'claude-code': '/claude-code.png',
};

function AgentTypeIcon({ type }: { type: string }) {
 const logo = agentLogos[type.trim().toLowerCase()];
 return typeof logo === 'string' ? <img src={logo} alt="" width={32} height={32} className="ws-agent-logo" /> : <Window32Regular className="ws-agent-logo" aria-hidden="true" />;
}

const workspaceSections = ['usage', 'capacity', 'agents', 'resume', 'pins', 'activity'] as const;
const workspaceGroups = ['operations', 'resume', 'activity', 'agents'] as const;
type WorkspaceSection = typeof workspaceSections[number];

type WorkspaceDialog = (
 | { kind: 'pins' | 'tasks' | 'recent' }
 | { kind: 'task'; task: Task; listTrigger?: HTMLElement }
 | { kind: 'details'; tab: 'usage' | 'capacity' | 'agents'; usage?: UsageDashboard }
) & { trigger?: HTMLElement | null };

function Workspace({ userID }: { userID: number }) {
 const { locale } = useI18n();
 const text = workspaceText(locale);
 const dashboard = useRef<HTMLDivElement>(null);
 const [size, setSize] = useState({ width: Infinity, height: Infinity });
 const [section, setSection] = useState<WorkspaceSection>('usage');
 const queryClient = useQueryClient();

 useEffect(() => {
  const element = dashboard.current;
  if (!element) return;
  const observer = new ResizeObserver(([entry]) => {
   const { width, height } = entry.contentRect;
   setSize({ width, height });
  });
  observer.observe(element);
  return () => observer.disconnect();
 }, []);
 const [filter, setFilter] = useState<ResumeFilter>('attention');
 const [hidden, setHidden] = useState(document.hidden);
 const [preferences, setPreferences] = useState<Preferences>(() => {
  try { return readPreferences(localStorage.getItem(preferenceKey(userID))); } catch { return { pins: null, read: [] }; }
 });
 const [draft, setDraft] = useState<number[]>([]);
 const [storageError, setStorageError] = useState(false);
 const [dialog, setDialog] = useState<WorkspaceDialog | null>(null);
 const resumeHeading = useRef<HTMLHeadingElement>(null);
 const viewPicker = useRef<HTMLButtonElement>(null);
 const dialogClose = useRef<HTMLButtonElement>(null);
 const openDialog = (next: WorkspaceDialog, trigger: HTMLElement | null) => {
  setStorageError(false);
  setDialog({ ...next, trigger });
 };
 const closeDialog = () => {
  if (dialog?.kind === 'task' && dialog.listTrigger) {
   const target = dialog.listTrigger;
   setDialog({ kind: 'tasks', trigger: dialog.trigger });
   requestAnimationFrame(() => {
    if (target?.isConnected) target.focus();
    else dialogClose.current?.focus();
   });
   return;
  }
  setDialog(null);
  requestAnimationFrame(() => {
   const target = dialog?.trigger;
   if (target?.isConnected && target.getClientRects().length) target.focus();
   else if (viewPicker.current) viewPicker.current.focus();
   else resumeHeading.current?.focus();
  });
 };
 const refreshOptions = {
  retry: false, staleTime: 15_000, gcTime: 0, refetchOnWindowFocus: 'always', refetchOnReconnect: 'always',
  refetchInterval: hidden ? false : 30_000, refetchIntervalInBackground: false,
 } as const;
 const query = useQuery({
  ...refreshOptions,
  queryKey: ['workspace', userID], queryFn: ({ signal }) => getWorkspace('7d', signal),
 });
 // The account inventory and the creation quota count all instances owned by the account.
 const instanceCountQuery = useQuery({
  ...refreshOptions,
  queryKey: ['workspace-instance-count', userID],
  queryFn: instanceService.getSummary,
  select: summary => summary.total,
 });
 const instanceCount = instanceCountQuery.isError ? null : instanceCountQuery.data ?? null;
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
 const usableHeight = size.height - (query.isError ? (size.width < 540 ? 72 : 44) : 0);
 const layout = size.width >= 940 && usableHeight >= 668 ? 'dashboard' : usableHeight >= 744 ? 'groups' : 'section';
 const compact = layout === 'dashboard' && usableHeight < 832;
 const previewLimit = layout === 'dashboard' && usableHeight >= 790 ? 3 : 2;
 const n = (value: number | null) => formatNumber(value, locale);
 const timeZone = data?.timezone ?? 'Asia/Shanghai';
 const dateTime = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone });
 const rowTime = new Intl.DateTimeFormat(locale, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone });
 const date = (value: string) => dateTime.format(new Date(value));
 const followUp = data ? {
  failed: resumeTasks(data.tasks, preferences.read, 'failed'),
  completed: resumeTasks(data.tasks, preferences.read, 'completed'),
  running: resumeTasks(data.tasks, preferences.read, 'running'),
 } : null;
 const taskCount = (value: number) => n(data?.coverage.tasks.status === 'unavailable' ? null : value);
 const pins = data ? validPins(preferences.pins, data.instances) : [];
 const hasRetainedPins = (preferences.pins || []).some(id => !data?.instances.some(agent => agent.id === id));
 const taskGroups = followUp ? [followUp.running, followUp.failed, followUp.completed] : [];
 const previewTasks = [...taskGroups.flatMap(group => group.slice(0, 1)), ...taskGroups.flatMap(group => group.slice(1, 3))].slice(0, previewLimit);
 const filteredTasks = followUp ? filter === 'attention' ? [...followUp.failed, ...followUp.completed] : followUp[filter] : [];
 const recentAgents = data?.recent_agents ?? [];
 const task = dialog?.kind === 'task' ? dialog.task : null;
 const taskDestination = task && data ? taskRoute(task, data.instances) : null;
 const showTaskList = dialog?.kind === 'tasks' || dialog?.kind === 'task' && dialog.listTrigger;
 const dialogTitle = dialog?.kind === 'pins' ? text.manage : dialog?.kind === 'task' ? text.result
  : dialog?.kind === 'tasks' ? text.viewTasks : dialog?.kind === 'recent' ? text.recent
  : dialog?.kind === 'details' ? dialog.tab === 'usage' ? text.distribution : text[dialog.tab] : '';
 const operationPanel = section === 'usage' || section === 'capacity' ? section : undefined;
 const group = section === 'activity' || section === 'agents' ? section : operationPanel ? 'operations' : 'resume';
 const showOperations = layout === 'dashboard' || (layout === 'groups' ? group === 'operations' : Boolean(operationPanel));
 const showAgents = layout === 'dashboard' || section === 'agents';
 const showWork = layout === 'dashboard' || (layout === 'groups' ? group === 'resume' : section === 'resume' || section === 'pins');
 const showActivity = layout === 'dashboard' || (layout === 'groups' ? group === 'activity' : section === 'activity');
 const pickerOptions = layout === 'groups' ? workspaceGroups : workspaceSections;
 const visibleSection = layout === 'section' ? section : undefined;
 const currentView = layout === 'groups' ? group : section;

 return <UserLayout fillHeight scrollableMain>
  <div ref={dashboard} data-layout={layout} data-compact={compact} className={`ws-dashboard ${hidden ? 'ws-paused' : ''}`}>
   {query.isError && <MessageBar intent="error" className="ws-error" role="alert">
    <MessageBarBody>{data ? text.stale : text.error}</MessageBarBody>
    <MessageBarActions><Button appearance="secondary" size="small" onClick={() => {
     void query.refetch();
     void instanceCountQuery.refetch();
     void queryClient.refetchQueries({ queryKey: ['workspace-usage', userID], type: 'active' });
    }}>{text.retry}</Button></MessageBarActions>
   </MessageBar>}
   {layout !== 'dashboard' && <Dropdown ref={viewPicker} className="ws-view-picker" aria-label={text.selectGroup} value={text[currentView]}
    selectedOptions={[currentView]} onOptionSelect={(_, selection) => {
     if (layout === 'groups') {
      const next = workspaceGroups.find(value => value === selection.optionValue);
      if (next) setSection(next === 'operations' ? operationPanel ?? 'usage' : next);
     } else {
      const next = workspaceSections.find(value => value === selection.optionValue);
      if (next) setSection(next);
     }
    }}>
    {pickerOptions.map(value => <Option key={value} value={value}>{text[value]}</Option>)}
   </Dropdown>}
   <div className="ws-grid" aria-busy={query.isFetching}>
    <Operations data={data} instanceCount={instanceCount} userID={userID} paused={hidden} locale={locale} text={text} loading={query.isPending}
     hidden={!showOperations} workspaceError={query.isError} selectedPanel={layout === 'section' ? operationPanel : undefined}
     onDetails={(tab, trigger, usage) => openDialog({ kind: 'details', tab, usage }, trigger)} />
    <AgentStates data={data} hidden={!showAgents} loading={query.isPending} locale={locale} text={text}
     onDetails={trigger => openDialog({ kind: 'details', tab: 'agents' }, trigger)} />
    <div className="ws-work" hidden={!showWork}>
    <section className="ws-resume ws-panel" hidden={!!visibleSection && visibleSection !== 'resume'} aria-labelledby="ws-resume-heading">
     <div className="ws-heading">
      <h2 id="ws-resume-heading" ref={resumeHeading} tabIndex={-1}>{text.resume}</h2>
      {followUp && <Button appearance="subtle" size="small" icon={<ChevronRight16Regular />}
       className="ws-task-toggle" iconPosition="after" aria-haspopup="dialog"
       onClick={event => openDialog({ kind: 'tasks' }, event.currentTarget)}>{text.viewTasks}</Button>}
     </div>
     {data && followUp ? <div className="ws-resume-columns">
      <div>
       <h3>{text.followUp}</h3>
       <div className="ws-resume-list">
        {previewTasks.length ? previewTasks.map(item => <TaskRow key={item.key} item={item} text={text} dateTime={rowTime}
         onClick={event => openDialog({ kind: 'task', task: item }, event.currentTarget)} />)
         : <p className="ws-empty">{data.coverage.tasks.status === 'unavailable' ? text.unavailable : text.empty}</p>}
       </div>
      </div>
      <div>
       <div className="ws-list-heading">
        <h3>{text.recent}</h3>
        {recentAgents.length > previewLimit && <Button appearance="subtle" size="small" icon={<ChevronRight16Regular />}
         aria-label={text.viewRecent} title={text.viewRecent} aria-haspopup="dialog"
         onClick={event => openDialog({ kind: 'recent' }, event.currentTarget)} />}
       </div>
       <div className="ws-recent-list">
        {recentAgents.length ? recentAgents.slice(0, previewLimit).map(agent => <RecentAgentLink key={agent.id} agent={agent} text={text} dateTime={rowTime} />)
         : <div className="ws-empty">{data.coverage.recent?.status === 'unavailable' ? text.unavailable : text.noRecent}<Link to="/instances">{text.allAgents}</Link></div>}
       </div>
      </div>
     </div> : <WorkspacePanelState loading={query.isPending} text={text} />}
    </section>
    <section className="ws-pins ws-panel" hidden={!!visibleSection && visibleSection !== 'pins'} aria-labelledby="ws-pins-heading">
     <div className="ws-heading">
      <h2 id="ws-pins-heading">{text.pins}</h2>
      {data && data.instances.length > 0 && <Button appearance="subtle" size="small" icon={<Pin20Regular />}
       onClick={event => { setDraft(preferences.pins ?? pins); openDialog({ kind: 'pins' }, event.currentTarget); }}>{text.manage}</Button>}
     </div>
     {data ? <>
      {pins.length ? <ul className="ws-pin-grid" data-count={pins.length}>{pins.map(id => {
       const agent = data.instances.find(agent => agent.id === id)!;
       return <li key={id}><Card as="div" appearance="filled" className="ws-pin">
        <Link to={agentRoute(agent)} title={agent.name}>
         <AgentTypeIcon type={agent.type} />
         <span className="ws-row-copy"><strong>{agent.name}</strong>
          <small><i className={`ws-dot ws-state-${agent.state}`} aria-hidden="true" />{stateLabel(agent.state, text)} · {agent.type} · {agent.instance_mode}</small>
         </span>
        </Link>
       </Card></li>;
      })}</ul> : <div className="ws-empty">
       {data.instances_total === 0 && data.coverage.instances.status === 'ready' ? <>
        <span>{text.emptyAgents}</span><Link className="ws-create" to="/instances/new">{text.create}</Link>
       </> : data.coverage.instances.status === 'unavailable' ? text.unavailable : text.emptyPins}
      </div>}
      {hasRetainedPins && <p className="ws-note">{text.retainedPins}</p>}
      {data.instances_truncated && <p className="ws-note">{text.cap}</p>}
     </> : <WorkspacePanelState loading={query.isPending} text={text} />}
    </section>
    </div>
    <Activity data={data} loading={query.isPending} locale={locale} text={text} hidden={!showActivity} />
   </div>
  </div>
  <Dialog open={Boolean(dialog)} onOpenChange={(_, change) => { if (!change.open) closeDialog(); }}>
   <DialogSurface className={`ws-dialog ${dialog?.kind === 'details' ? 'ws-details-dialog' : ''}`}>
    <DialogBody>
     <DialogTitle action={<Button ref={dialogClose} appearance="subtle" icon={<Dismiss20Regular />} aria-label={text.close} onClick={closeDialog} />}>{dialogTitle}</DialogTitle>
     <DialogContent>
      {showTaskList && followUp && <div hidden={dialog?.kind !== 'tasks'}>
       <div className="ws-counters" role="group" aria-label={text.viewTasks}>
        {(['attention', 'running', 'failed', 'completed'] as const).map(value => <ToggleButton key={value} appearance="subtle" size="small"
         checked={filter === value} onClick={() => setFilter(value)}>{text[value]} <strong>{taskCount(value === 'attention' ? followUp.failed.length + followUp.completed.length : followUp[value].length)}</strong></ToggleButton>)}
       </div>
       <div className="ws-resume-list">
        {filteredTasks.length ? filteredTasks.map(item => <TaskRow key={item.key} item={item} text={text} dateTime={rowTime} onClick={event => {
          setStorageError(false);
          setDialog({ kind: 'task', task: item, listTrigger: event.currentTarget, trigger: dialog?.trigger });
          requestAnimationFrame(() => dialogClose.current?.focus());
         }} />)
         : <p className="ws-empty">{data?.coverage.tasks.status === 'unavailable' ? text.unavailable : text.empty}</p>}
       </div>
      </div>}
      {dialog?.kind === 'recent' && <div className="ws-recent-list">{recentAgents.map(agent =>
       <RecentAgentLink key={agent.id} agent={agent} text={text} dateTime={rowTime} />)}</div>}
      {dialog?.kind === 'pins' && <>
       <p>{text.maxPins}</p>
       <div className="ws-pin-options">{data?.instances.map(agent => <Checkbox key={agent.id}
        checked={draft.includes(agent.id)} disabled={!draft.includes(agent.id) && draft.length >= 6}
        onChange={(_, change) => setDraft(change.checked === true ? [...draft, agent.id] : draft.filter(id => id !== agent.id))}
        label={<span className="ws-row-copy"><strong>{agent.name}</strong><small>{agent.type} · {stateLabel(agent.state, text)}</small></span>} />)}</div>
       {hasRetainedPins && <p>{text.retainedPins}</p>}{data?.instances_truncated && <p>{text.cap}</p>}
      </>}
      {task && <>
       <h3>{task.title || task.id}</h3><p>{task.agent_name} · {text[task.status]} · {date(task.finished_at || task.updated_at)}</p>
       <p className="ws-result">{task.error_message || task.result_excerpt || text.noResult}</p>
       {task.progress !== null && <ProgressBar aria-label={`${task.title}: ${task.progress}%`} max={100} value={task.progress} />}
       {!taskDestination && <p>{text.archived}</p>}
      </>}
      {dialog?.kind === 'details' && data && <OperationDetails key={dialog.tab}
       data={dialog.tab === 'usage' && dialog.usage ? { ...data, ...dialog.usage, coverage: { ...data.coverage, ...dialog.usage.coverage } } : data}
       instanceCount={instanceCount} locale={locale} tab={dialog.tab} />}
      {storageError && <MessageBar intent="error" role="alert"><MessageBarBody>{text.saveError}</MessageBarBody></MessageBar>}
     </DialogContent>
     {dialog?.kind === 'pins' && <DialogActions fluid className="ws-pin-actions">
      <Button appearance="subtle" onClick={() => setDraft([])}>{text.clear}</Button><output>{draft.length} / 6</output>
      <Button onClick={closeDialog}>{text.cancel}</Button>
      <Button appearance="primary" onClick={() => { if (savePreferences({ ...preferences, pins: draft })) closeDialog(); }}>{text.save}</Button>
     </DialogActions>}
     {task && <DialogActions>
      {task.status === 'succeeded' && <Button disabled={preferences.read.includes(resultKey(task))}
       onClick={() => savePreferences({ ...preferences, read: [...preferences.read, resultKey(task)].slice(-1000) })}>
       {preferences.read.includes(resultKey(task)) ? text.read : text.markRead}
      </Button>}
      <Link className="app-button-primary" to={taskDestination || (task.source === 'team' ? '/teams' : '/instances')}>
       {taskDestination ? task.source === 'team' ? text.openTeam : text.openAgent : task.source === 'team' ? text.allTeams : text.allAgents}
       <ArrowRight16Regular aria-hidden="true" />
      </Link>
     </DialogActions>}
    </DialogBody>
   </DialogSurface>
  </Dialog>
 </UserLayout>;
}

function RecentAgentLink({ agent, text, dateTime }: { agent: Dashboard['recent_agents'][number]; text: WorkspaceText; dateTime: Intl.DateTimeFormat }) {
 const source = text[agent.activity_kind === 'model' ? 'calls' : agent.activity_kind === 'chat' ? 'chat' : agent.activity_kind === 'team' ? 'team' : 'task'];
 return <Link to={agentRoute(agent)} title={agent.name} className="ws-work-row">
  <AgentTypeIcon type={agent.type} />
  <span className="ws-row-copy"><strong>{agent.name}</strong><small>{agent.type} · {source} · {stateLabel(agent.state, text)}</small></span>
  <EntryTime value={agent.last_used_at} dateTime={dateTime} />
 </Link>;
}

function EntryTime({ value, dateTime }: { value: string; dateTime: Intl.DateTimeFormat }) {
 const formatted = dateTime.format(new Date(value));
 return <time dateTime={value} title={formatted}>{formatted}</time>;
}

function TaskRow({ item, text, dateTime, onClick }: {
 item: Task; text: WorkspaceText; dateTime: Intl.DateTimeFormat; onClick: MouseEventHandler<HTMLButtonElement>;
}) {
 const inProgress = item.status === 'running' || item.status === 'pending';
 const status = text[item.status];
 return <button type="button" className={`ws-work-row ws-task ws-task-${item.status}`} onClick={onClick}>
  {item.status === 'failed' ? <ErrorCircle16Regular /> : item.status === 'succeeded' ? <CheckmarkCircle16Regular /> : <Clock16Regular className="ws-busy" />}
  <span className="ws-row-copy">
   <strong title={item.title || item.id}>{item.title || item.id}</strong>
   <small>{status} · {item.agent_name}{inProgress && <> · <span>{item.progress === null ? text.progressUnknown : `${item.progress}%`}</span></>}</small>
  </span>
  <EntryTime value={item.updated_at} dateTime={dateTime} />
 </button>;
}

function WorkspacePanelState({ loading, text }: { loading: boolean; text: WorkspaceText }) {
 return loading ? <Skeleton className="ws-work-skeleton" aria-label={text.loading}><SkeletonItem /><SkeletonItem /></Skeleton>
  : <p className="ws-empty">{text.unavailable}</p>;
}

function Activity({ data, loading, locale, text, hidden }: { data?: Dashboard; loading: boolean; locale: string; text: WorkspaceText; hidden: boolean }) {
 const [activityIndex, setActivityIndex] = useState<number | null>(null);
 const heatPoints = useRef<Array<HTMLButtonElement | null>>([]);
 const activity = data?.activity ?? [];
 const firstDate = activity[0]?.date;
 const leadingDays = firstDate ? (new Date(firstDate).getUTCDay() + 6) % 7 : 0;
 const weeks = Math.ceil((activity.length + leadingDays) / 7);
 const month = new Intl.DateTimeFormat(locale, { month: 'short', timeZone: 'UTC' });
 const weekday = new Intl.DateTimeFormat(locale, { weekday: 'short', timeZone: 'UTC' });
 const months = activity.flatMap((day, index) => day.date.endsWith('-01')
  ? [{ date: day.date, column: Math.floor((index + leadingDays) / 7) + 1, label: month.format(new Date(day.date)) }] : []);
 const selectedDay = activity[Math.min(activityIndex ?? activity.length - 1, activity.length - 1)];
 const n = (value: number | null) => formatNumber(value, locale);
 const breakdown = (day: Dashboard['activity'][number], counts: ReturnType<typeof activityCounts>) => {
  const coverage = counts.status === 'ready' ? '' : ` · ${text[counts.status]}`;
  return `${day.date}: ${n(counts.events)} ${text.events}, ${n(counts.calls)} ${text.calls}, ${n(counts.tasks)} ${text.tasks}${coverage}`;
 };
 return <section hidden={hidden} className="ws-activity ws-panel" aria-labelledby="ws-activity-heading">
  <div className="ws-heading">
   <h2 id="ws-activity-heading">{text.activity}</h2>
   {data && selectedDay && <output className="ws-heat-readout" aria-live="polite">{breakdown(selectedDay, activityCounts(selectedDay, data.coverage.activity))}</output>}
   {activity.length > 0 && <div className="ws-heat-legend" aria-label={text.events} role="group">
    {['0', '1–2', '3–9', '10+'].map((label, level) => <span key={label}><i data-level={level} aria-hidden="true" />{label}</span>)}
    <span><i data-level="unknown" aria-hidden="true" />{text.unknown}</span>
   </div>}
  </div>
  {data ? activity.length ? <div className="ws-heat-wrap">
   <div className="ws-heat-frame">
    <div className="ws-heat-plot" style={{ '--ws-weeks': weeks } as CSSProperties}>
    <div className="ws-heat-months" aria-hidden="true">
     {months.map(({ date, column, label }) => <span key={date} style={{ gridColumnStart: column }} data-end={column > weeks - 3}>{label}</span>)}
    </div>
    <div className="ws-heat-weekdays" aria-hidden="true">
     {[0, 2, 4].map(day => <span key={day} style={{ gridRowStart: day + 1 }}>{weekday.format(new Date(Date.UTC(2024, 0, day + 1)))}</span>)}
    </div>
    <div className="ws-heat" role="group" aria-label={text.activity}>{activity.map((day, index) => {
     const counts = activityCounts(day, data.coverage.activity);
     const label = breakdown(day, counts);
     return <button key={day.date} type="button" ref={el => { heatPoints.current[index] = el; }}
      tabIndex={day === selectedDay ? 0 : -1} aria-pressed={day === selectedDay}
      style={index === 0 ? { gridRowStart: leadingDays + 1 } : undefined}
      data-level={counts.status === 'unavailable' || !day.observed ? 'unknown' : day.events === 0 ? 0 : day.events < 3 ? 1 : day.events < 10 ? 2 : 3}
      aria-label={label} title={label}
      onFocus={() => setActivityIndex(index)} onClick={() => setActivityIndex(index)} onKeyDown={event => {
       const rows = getComputedStyle(event.currentTarget.parentElement!).gridTemplateRows.split(' ').length;
       const next = event.key === 'ArrowRight' ? index + rows : event.key === 'ArrowLeft' ? index - rows : event.key === 'ArrowDown' ? index + 1 : event.key === 'ArrowUp' ? index - 1 : event.key === 'Home' ? 0 : event.key === 'End' ? activity.length - 1 : null;
       if (next !== null) {
        event.preventDefault();
        heatPoints.current[Math.max(0, Math.min(activity.length - 1, next))]?.focus();
       }
      }} />;
    })}</div>
    </div>
   </div>
  </div> : <p className="ws-empty">{data.coverage.activity.status === 'unavailable' ? text.unavailable : text.noActivity}</p>
  : <WorkspacePanelState loading={loading} text={text} />}
 </section>;
}

function OperationDetails({ data, instanceCount, locale, tab }: { data: Dashboard; instanceCount: number | null; locale: string; tab: 'usage' | 'capacity' | 'agents' }) {
 const text = workspaceText(locale);
 const n = (value: number | null) => formatNumber(value, locale);
 if (tab === 'agents') return <>
  <CoverageNote source={data.coverage.instances} text={text} timeZone={data.timezone} />
  <p>{text.stateHint}</p>
  <ObservationDetails key={tab} data={data} locale={locale} tab={tab} />
  {data.instances_truncated && <p>{text.cap}</p>}
  <div className="ws-table-wrap">
   <Table className="ws-instance-state-table" aria-label={text.agents}>
    <TableHeader><TableRow>
     <TableHeaderCell scope="col">{text.agent}</TableHeaderCell>
     <TableHeaderCell scope="col">{text.state}</TableHeaderCell>
     <TableHeaderCell scope="col">{text.cpu}</TableHeaderCell>
     <TableHeaderCell scope="col">{text.memory}</TableHeaderCell>
    </TableRow></TableHeader>
    <TableBody>{data.instances.map(agent => {
     const state = stateLabel(agent.state, text);
     return <TableRow key={agent.id}>
      <TableCell><Link to={agentRoute(agent)} title={agent.name}>{agent.name}</Link></TableCell>
      <TableCell><span title={state}>{state}</span></TableCell>
      <TableCell>{n(agent.cpu_cores)}</TableCell>
      <TableCell>{n(agent.memory_gb)} GB</TableCell>
     </TableRow>;
    })}</TableBody>
   </Table>
  </div>
 </>;
 if (tab === 'capacity') return <>
  <CoverageNote source={data.coverage.instances} text={text} timeZone={data.timezone} />
  <p>{text.capacityHint}</p>
  <ObservationDetails key={tab} data={data} locale={locale} tab={tab} />
  <div className="ws-table-wrap"><table>
   <thead><tr><th>{text.capacity}</th><th>{text.quota}</th></tr></thead>
   <tbody>
    <tr><td>{text.instances}</td><td>{n(instanceCount)} / {n(data.capacity.max_instances)}</td></tr>
    <tr><td>{text.cpu}</td><td>{n(data.capacity.allocated_cpu)} / {n(data.capacity.max_cpu)}</td></tr>
    <tr><td>{text.memory}</td><td>{n(data.capacity.allocated_memory_gb)} / {n(data.capacity.max_memory_gb)} GB</td></tr>
    <tr><td>{text.workspaceStorage}</td><td>{n(data.capacity.allocated_storage_gb)} / {n(data.capacity.max_storage_gb)} GB</td></tr>
   </tbody>
  </table></div>
 </>;
 const usageValue = (value: number | null) => n(data.coverage.usage.status === 'unavailable' ? null : value);
 const delta = comparison(data.usage.total_tokens, data.usage.previous_tokens, data.coverage.usage.status === 'ready');
 return <><p>{text.input} {usageValue(data.usage.prompt_tokens)} · {text.output} {usageValue(data.usage.completion_tokens)}
  {delta !== null && ` · ${delta > 0 ? '+' : ''}${n(delta)}% ${text.compare}`}</p><p>{reportingDate(data.window.start, data.timezone, true)} – {reportingDate(data.window.end, data.timezone, true)} · {data.timezone} · {text.exclusiveEnd}</p><CoverageNote source={data.coverage.usage} text={text} timeZone={data.timezone} /><p>{text.costHint}</p>{data.usage.currencies.map(cost => <p key={cost.currency}>{formatMoney(data.coverage.costs.status === 'unavailable' ? null : cost.amount, cost.currency, locale)} · {data.coverage.costs.status === 'unavailable' ? text.unavailable : cost.complete ? text.ready : text.subtotal}{cost.estimated ? ` · ${text.estimated}` : ''}</p>)}<div className="ws-table-wrap"><table><thead><tr><th>{text.agent}</th><th>{text.tokens}</th>{data.usage.currencies.map(cost => <th key={cost.currency}>{cost.currency}</th>)}</tr></thead><tbody>{data.agent_usage.map(row => {
  const agent = data.instances.find(agent => agent.id === row.instance_id);
  return <tr key={row.instance_id}><td>{agent ? <Link to={agentRoute(agent)}>{agent.name}</Link> : text.archivedUsage}</td><td>{formatNumber(data.coverage.usage.status === 'unavailable' ? null : row.tokens, locale)}</td>{data.usage.currencies.map(cost => <td key={cost.currency}>{formatMoney(data.coverage.costs.status === 'unavailable' ? null : row.costs[cost.currency] ?? null, cost.currency, locale)}</td>)}</tr>;
 })}</tbody></table></div></>;
}
function ObservationDetails({data, locale, tab}: {data: Dashboard; locale: string; tab: 'capacity' | 'agents'}) {
 const text = workspaceText(locale);
 const options = tab === 'agents' ? agentStates.map(value => ({value, label: stateLabel(value,text)})) : [{value:'allocated_cpu',label:text.cpu},{value:'allocated_memory_gb',label:text.memory}];
 const [metric,setMetric] = useState(options[0].value);
 const source = data.coverage.observations;
 const values = data.observations.map(point => source.status === 'unavailable' ? null : point[metric as keyof Omit<typeof point,'at'>]);
 return <div className="ws-observation-details"><p>{text[data.range]} · {text.observation}</p><div className="ws-chart-controls"><Dropdown aria-label={text.selectMetric} size="small" selectedOptions={[metric]} value={options.find(option => option.value === metric)?.label}
  onOptionSelect={(_, selection) => { if (selection.optionValue) setMetric(selection.optionValue); }}>
   {options.map(option => <Option key={option.value} value={option.value}>{option.label}</Option>)}
  </Dropdown><CoverageNote source={source} text={text} timeZone={data.timezone} /></div><Chart key={metric} values={values} times={data.observations.map(point => point.at)} label={options.find(option => option.value === metric)!.label} locale={locale} timeZone={data.timezone} text={text} /></div>;
}
export default function UserDashboard() {
 const { user } = useAuth();
 // Remount account preferences, dialogs and local view state synchronously on identity changes.
 return user ? <Workspace key={user.id} userID={Number(user.id)} /> : null;
}
