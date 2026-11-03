import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { ArrowLeft, ArrowRight, Check, ChevronUp, History, Plus, RefreshCw, Search, Settings2, Trash2, UserMinus, Users } from 'lucide-react';
import { OwnerTeamOperationError, withOwnerTeamIntent, pendingOwnerTeamIntent, recoverOwnerTeamIntent, ownerTeamService, ownerTeamWorkspaceAPI, waitForOwnerTeamOperation, type OwnerTeamSummary, type OwnerTeamCatalogEntry, type TeamOperation } from '../../services/ownerTeamService';
import type { OwnerTeamsState } from '../../hooks/useOwnerTeams';
import type { TeamDetails, TeamMember } from '../../types/team';
import type { CustomTeamTemplate } from '../../types/customTeamTemplate';
import TeamBadge from './TeamBadge';
import { OwnerTeamTemplates } from './OwnerTeamTemplates';
import { OwnerTeamDialog, useOwnerTeamDraftGuard } from './OwnerTeamDialog';
import { OwnerTeamNotifications, useOwnerTeamFeedback } from './OwnerTeamNotifications';
import { memberIcon } from '../../lib/teamIcons';
import { memberRailState } from './teamMemberPresentation';
import './owner-team-workspace.css';

const message = (error: unknown) => error instanceof Error ? error.message : '操作失败';
const teamURL = (id: number) => `/ieisystem/teams/${id}`;
const teamLabel = (team: OwnerTeamSummary) => team.status === 'creating' ? '创建中' : team.status !== 'running' || (team.fault_count ?? team.members.filter(m => m.status === 'failed' || m.availability === 'blocked' || m.availability === 'offline').length) > 0 ? '需处理' : team.available_count < team.member_count ? '待确认' : team.members.some(m => m.status === 'busy' || m.availability === 'busy') ? '执行中' : '空闲';
const memberState = (m: TeamMember) => memberRailState(m);

function MemberRow({ member, ownerTeamID }: { member: TeamMember; ownerTeamID: number }) {
  const state = memberState(member);
  return <div className="ot-member"><TeamBadge iconKey={memberIcon(member)} size="chat" /><div><strong>{member.display_name || member.member_key}</strong><p>{member.runtime_type} · {member.role} · <span data-state={state.tone}>{state.label}</span></p></div>{member.instance_id ? <Link to={`/ieisystem/instances/${member.instance_id}`} state={{ portalReturnSearch: window.location.search, portalReturnTeamID: ownerTeamID }}>进入 ↗</Link> : <span className="ot-muted">待创建</span>}</div>;
}

export function OwnerTeamSidebar({ state, onSelect }: { state: OwnerTeamsState; onSelect?: () => void }) {
  return <div className="ot-sidebar"><label className="ot-search"><Search size={15}/><input value={state.query} onChange={e => state.setQuery(e.target.value)} placeholder="搜索团队名称…" aria-label="搜索团队"/></label><div className="ot-card-list">{state.error && <p role="alert" className="ot-error">{state.error}<button className="ot-secondary" onClick={() => void state.refresh()}>重试</button></p>}
    {!state.teams.length && !state.loading && <p className="ot-empty">还没有 Team，可在右侧创建。</p>}
    {state.teams.map(team => {
      const expanded = state.expandedID === team.id;
      const toggle = () => { state.expand(team.id); onSelect?.(); };
      const entry = <Link aria-label={`进入团队 ${team.name}`} className="ot-card-entry" to={teamURL(team.id)} state={{ portalReturnSearch: window.location.search }}><span>进入团队</span><ArrowRight size={15}/></Link>;
      return <article key={team.id} data-team-id={team.id} data-expanded={expanded} className={`ot-team-card ${state.selectedID === team.id ? 'is-selected' : ''}`} onClick={e => { if ((e.target as HTMLElement).closest('a,button,input')) return; toggle(); }}>
        <div className="ot-card-watermark" aria-hidden="true"><img src="/team-expanded.png" alt=""/></div>
        <button className="ot-card-title" onClick={toggle} aria-expanded={expanded} aria-controls={`ot-members-${team.id}`}><span className="ot-card-avatar"><img src="/team-square.png" alt=""/></span><span className="ot-card-heading"><strong title={team.name}>{team.name}</strong><small>Team #{team.id} · {team.member_count} 名成员</small></span></button>
        <div className="ot-card-status"><div><span data-state={teamLabel(team) === '需处理' ? 'error' : ['创建中', '待确认'].includes(teamLabel(team)) ? 'waiting' : 'ready'}>● {teamLabel(team)}</span><span>{team.available_count}/{team.member_count} 可用</span></div>{!expanded && entry}</div>
        <div id={`ot-members-${team.id}`} hidden={!expanded} className="ot-card-members">{expanded && team.members.map(member => <MemberRow key={member.id} member={member} ownerTeamID={team.id}/>)}<footer><button onClick={toggle}>收起 <ChevronUp size={13}/></button>{entry}</footer></div>
      </article>;
    })}{state.hasMore && <button className="ot-secondary" onClick={state.loadMore}>加载更多</button>}{state.loading && !state.teams.length && <p role="status" className="ot-empty">正在读取 Team…</p>}
  </div></div>;
}

function useTeamOperation(owner: string, onDone: () => Promise<void>) {
  const [operation, setOperation] = useState<TeamOperation | null>(() => { try { const raw = sessionStorage.getItem(`owner-team-operation:${owner}`); return raw ? JSON.parse(raw) : null; } catch { return null; } });
  const operationOwner = useRef(owner);
  useEffect(() => { if (operationOwner.current !== owner) { operationOwner.current = owner; setOperation(null); setError(''); } }, [owner]);
  const [error, setError] = useState('');
  const [watch, setWatch] = useState(0);
  const done = useRef(onDone); done.current = onDone;
  useEffect(() => {
    if (!operation?.operation_id || !owner || operationOwner.current !== owner) return;
    sessionStorage.setItem(`owner-team-operation:${owner}`, JSON.stringify(operation));
    const controller = new AbortController(); setError('');
    void waitForOwnerTeamOperation(operation.operation_id, controller.signal).then(async result => {
      if (controller.signal.aborted) return;
      setOperation(result); sessionStorage.removeItem(`owner-team-operation:${owner}`); await done.current();
    }).catch(error => { if (!controller.signal.aborted) { if (error instanceof OwnerTeamOperationError) setOperation(error.operation); setError(message(error)); } });
    return () => controller.abort();
  }, [operation?.operation_id, owner, watch]);
  return { operation: operationOwner.current === owner ? operation : null, error, retryFailed: async () => { if (!operation) return; try { const next = await ownerTeamService.retryOperation(operation.operation_id, operation.attempt_count || 0); setError(''); setOperation(next); setWatch(n => n + 1); } catch (error) { setError(message(error)); } }, begin: (op: TeamOperation) => { setError(''); setOperation(op); }, retryRead: () => setWatch(n => n + 1), dismiss: () => { setOperation(null); setError(''); sessionStorage.removeItem(`owner-team-operation:${owner}`); } };
}

export function OwnerTeamDashboard({ state }: { state: OwnerTeamsState }) {
  return <OwnerTeamNotifications key={state.owner} owner={state.owner}><OwnerTeamDashboardContent state={state}/></OwnerTeamNotifications>;
}

function OwnerTeamDashboardContent({ state }: { state: OwnerTeamsState }) {
  const feedback = useOwnerTeamFeedback();
  const [params, setParams] = useSearchParams();
  const section = params.get('team_section') === 'templates' ? 'templates' : 'teams';
  const setSection = (value: 'teams' | 'templates') => { const next = new URLSearchParams(window.location.search); next.set('team_section', value); next.delete('token'); setParams(next, { replace: true }); };
  const [loadedDetails, setDetails] = useState<TeamDetails | null>(null);
  const details = loadedDetails?.team.id === state.selectedID ? loadedDetails : null;
  const [detailError, setDetailError] = useState('');
  const [create, setCreate] = useState(false);
  const [manage, setManage] = useState(false);
  const [narrowDetail, setNarrowDetail] = useState(false);
  const operation = useTeamOperation(state.owner, state.refresh);
  const pendingIntent = pendingOwnerTeamIntent(state.owner);
  const reportOperation = (op: TeamOperation, error = '') => {
    const failed = op.status === 'failed' || !!error;
    feedback.notify({ id: op.operation_id, operationID: op.operation_id, attemptCount: op.attempt_count, terminal: ['succeeded', 'failed'].includes(op.status), kind: failed ? 'error' : op.status === 'succeeded' ? 'success' : 'pending', title: failed ? '操作尚未完成' : op.status === 'succeeded' ? '操作已完成' : '操作进行中', message: error || op.error_message || (op.status === 'succeeded' ? '后台已确认完成，可继续查看和管理。' : '可以继续浏览，关闭通知不会取消后台操作。'), actions: failed ? [{ label: '重新检查进度', onClick: operation.retryRead }, ...(op.status === 'failed' && op.team_id ? [{ label: '重试原操作', onClick: () => void operation.retryFailed() }] : [])] : undefined });
  };
  useEffect(() => {
    if (operation.operation) reportOperation(operation.operation, operation.error);
  // Stable business transitions, not notification/history renders, produce feedback.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [operation.operation?.operation_id, operation.operation?.status, operation.operation?.attempt_count, operation.error, feedback.notify]);
  useEffect(() => {
    feedback.setOperationHandler(op => { operation.begin(op); reportOperation(op); });
    return () => feedback.setOperationHandler(null);
  });
  useEffect(() => {
    if (!pendingIntent) return;
    feedback.notify({ id: `unconfirmed:${pendingIntent}`, kind: 'warning', title: '上次提交尚未确认', message: '原提交已保留，请先检查进度，避免重复创建。', actions: [{ label: '恢复提交并检查进度', onClick: () => { void recoverOwnerTeamIntent(state.owner, pendingIntent).then(operation.begin).catch(error => feedback.notify({ id: `unconfirmed:${pendingIntent}`, kind: 'error', title: '暂时无法确认提交', message: message(error) })); } }] });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingIntent, state.owner, feedback.notify]);
  useEffect(() => {
    let active = true; setDetails(null); setDetailError('');
    if (!state.selectedID) return;
    const refresh = async () => { try { const details = await ownerTeamWorkspaceAPI.getTeam(state.selectedID!); if (active) { setDetails(details); setDetailError(''); } } catch (error) { if (active) setDetailError(message(error)); } };
    void refresh(); const timer = window.setInterval(() => { if (!document.hidden) void refresh(); }, 5000);
    return () => { active = false; clearInterval(timer); };
  }, [state.selectedID, state.owner]);
  const selected = state.teams.find(t => t.id === state.selectedID) || (details && details.team.id === state.selectedID ? { ...details.team, members: details.members, member_count: details.members.length, available_count: details.members.filter(m => ['idle','busy'].includes(m.availability || '')).length } : undefined);
  return <section className="ot-workspace"><header className="ot-heading"><div><h2>团队空间</h2><p>{state.total} 个团队 · 创建、管理与协作</p></div><div className="ot-actions"><button type="button" className="ot-secondary" onClick={feedback.openHistory}><History size={15}/>操作记录</button><button className="ot-primary" disabled={!state.capabilities?.can_create} title={state.capabilities?.create_disabled_reason} onClick={() => setCreate(true)}><Plus size={16}/> 新建 Team</button></div></header>

    {!state.capabilities?.can_create && <p className="ot-note">{state.capabilities?.create_disabled_reason || '当前暂不能创建团队；已有团队不受影响。'}</p>}
    <div className="ot-tabs" role="tablist" aria-label="团队空间"><button role="tab" aria-selected={section === 'teams'} onClick={() => setSection('teams')}>我的 Team</button><button role="tab" aria-selected={section === 'templates'} onClick={() => setSection('templates')}>团队模板</button></div>

    {section === 'templates' ? <OwnerTeamTemplates key={state.owner} owner={state.owner} canGenerate={state.capabilities?.can_create === true} onOperation={operation.begin}/> : <div className={`ot-master-detail ${narrowDetail ? 'show-detail' : ''}`}>
      <div className="ot-master"><label className="ot-search"><Search size={14}/><input aria-label="筛选团队" placeholder="搜索团队…" value={state.query} onChange={e => state.setQuery(e.target.value)}/></label>{state.error && <p role="alert" className="ot-error">{state.error}</p>}{state.teams.map(t => <button key={t.id} aria-pressed={state.selectedID === t.id} className="ot-master-item" onClick={() => { state.select(t.id); setNarrowDetail(true); }}><strong>{t.name}</strong><span>#{t.id} · {t.available_count}/{t.member_count} 可用</span><small>{teamLabel(t)}</small></button>)}{state.hasMore && <button onClick={state.loadMore}>加载更多</button>}</div>
      <div className="ot-detail"><button className="ot-mobile-back ot-secondary" onClick={() => setNarrowDetail(false)}><ArrowLeft size={14}/>团队列表</button>{detailError ? <p role="alert" className="ot-error">{detailError}</p> : selected ? <>
        <div className="ot-detail-title"><div className="ot-team-icon"><Users size={22}/></div><div><h3>{selected.name}</h3><p>Team #{selected.id}</p></div></div><p className="ot-description">{selected.description || '协作完成团队任务，沉淀共享产物。'}</p><p className="ot-muted">{selected.member_count} 名成员 · {selected.available_count}/{selected.member_count} 可用 · Leader 统筹</p>
        <div className="ot-actions"><Link className="ot-primary" to={teamURL(selected.id)} state={{ portalReturnSearch: window.location.search }}>进入团队对话 <ArrowRight size={15}/></Link><button className="ot-secondary" onClick={() => setManage(true)}><Settings2 size={15}/>管理</button><button className="ot-secondary" aria-label="刷新团队" onClick={() => void state.refresh()}><RefreshCw size={15}/>刷新</button></div>
        <div className="ot-task-summary"><small>当前任务</small><p>{details?.tasks?.find(t => ['pending', 'dispatched', 'running', 'waiting'].includes(t.status))?.payload?.prompt as string || '暂无执行中的任务'}</p></div>
        <h4>团队成员</h4>{(details?.members || selected.members).map(m => <MemberRow key={m.id} member={m} ownerTeamID={selected.id}/>)}
        {manage && details && <OwnerTeamManage key={selected.id} details={details} onClose={() => setManage(false)} onChange={state.refresh} onOperation={operation.begin}/>}
      </> : <div className="ot-empty"><Users size={32}/><h3>创建你的第一个 Team</h3><p>选择模板，或先生成自定义团队。</p><button className="ot-primary" disabled={!state.capabilities?.can_create} onClick={() => setCreate(true)}>新建 Team</button></div>}</div>
    </div>}
    {create && <OwnerTeamCreate owner={state.owner} onClose={() => setCreate(false)} onOperation={op => { operation.begin(op); setCreate(false); void state.refresh(); }}/>}
  </section>;
}

function OwnerTeamCreate({ owner, onClose, onOperation }: { owner: string; onClose: () => void; onOperation: (op: TeamOperation) => void }) {
  const [catalog, setCatalog] = useState<OwnerTeamCatalogEntry[]>([]); const [custom, setCustom] = useState<CustomTeamTemplate[]>([]);
  const [selected, setSelected] = useState(''); const [name, setName] = useState(''); const [description, setDescription] = useState(''); const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  const [memberRuntimes, setMemberRuntimes] = useState<Record<string, string>>({});
  const [memberModes, setMemberModes] = useState<Record<string, string>>({});
  const submitting = useRef(false);
  const dirty = !!name || !!description || Object.keys(memberRuntimes).length > 0 || Object.keys(memberModes).length > 0;
  useOwnerTeamDraftGuard(dirty || busy);
  useEffect(() => { const controller = new AbortController(); void Promise.all([ownerTeamService.catalog(controller.signal), ownerTeamService.templates(controller.signal)]).then(([catalog, custom]) => { if (controller.signal.aborted) return; setCatalog(catalog.templates); setCustom(custom.templates); setSelected(catalog.templates[0] ? `builtin:${catalog.templates[0].key}` : ''); }).catch(error => { if (!controller.signal.aborted) setError(message(error)); }); return () => controller.abort(); }, []);
  const builtin = catalog.find(t => selected === `builtin:${t.key}`); const template = custom.find(t => selected === `custom:${t.id}`);
  const members = builtin?.members || template?.spec.members.map(m => ({ member_id: m.memberId, name: m.displayName, role: m.role, is_leader: m.isLeader, runtime_type: 'openclaw', instance_mode: 'lite' })) || [];
  return <OwnerTeamDialog title="新建 Team" busy={busy} dirty={dirty} onClose={onClose}><form onSubmit={e => { e.preventDefault(); if (submitting.current || !members.length) return; submitting.current = true; setBusy(true); setError(''); void withOwnerTeamIntent(owner, 'create', { name, description, template_key: builtin?.key, template_id: template?.id, revision: builtin?.revision || template!.revision, members: members.map(m => ({ member_id: m.member_id, runtime_type: m.is_leader ? 'openclaw' : memberRuntimes[m.member_id] || m.runtime_type, instance_mode: memberModes[m.member_id] || m.instance_mode })) }, (data, key) => ownerTeamService.create(data, key)).then(onOperation).catch(error => setError(message(error))).finally(() => { submitting.current = false; setBusy(false); }); }}>
    <div className="ot-form-scroll"><label>团队名称<input required maxLength={100} disabled={busy} value={name} onChange={e => setName(e.target.value)}/></label><label>团队模板<select disabled={busy} value={selected} onChange={e => { setSelected(e.target.value); setMemberRuntimes({}); setMemberModes({}); }}><optgroup label="内置模板">{catalog.map(t => <option key={t.key} value={`builtin:${t.key}`}>{t.name} · {t.members.length} 人</option>)}</optgroup><optgroup label="我的自定义模板">{custom.map(t => <option key={t.id} value={`custom:${t.id}`}>{t.name} · {t.resolved_member_count} 人</option>)}</optgroup></select></label><label>说明<textarea disabled={busy} value={description} maxLength={2000} onChange={e => setDescription(e.target.value)}/></label>
    <div className="ot-create-members">{members.map(m => <div key={m.member_id}><strong>{m.name}</strong><span>{m.role}</span><select aria-label={`${m.name} Runtime`} disabled={busy || m.is_leader} value={m.is_leader ? 'openclaw' : memberRuntimes[m.member_id] || m.runtime_type} onChange={e => setMemberRuntimes(v => ({ ...v, [m.member_id]: e.target.value }))}><option value="openclaw">OpenClaw</option><option value="hermes">Hermes</option></select><select aria-label={`${m.name}模式`} disabled={busy} value={memberModes[m.member_id] || m.instance_mode} onChange={e => setMemberModes(v => ({ ...v, [m.member_id]: e.target.value }))}><option value="lite">Lite</option><option value="pro">Pro</option></select></div>)}</div>{error && <p role="alert" className="ot-error">{error}</p>}</div><footer><button type="button" disabled={busy} className="ot-secondary" onClick={() => { if (!dirty || window.confirm('有尚未提交的团队配置，确认关闭？')) onClose(); }}>取消</button><button className="ot-primary" disabled={busy || !members.length}>{busy ? '正在提交…' : '创建团队'}</button></footer></form></OwnerTeamDialog>;
}

function OwnerTeamManage({ details, onClose, onChange, onOperation }: { details: TeamDetails; onClose: () => void; onChange: () => Promise<void>; onOperation: (op: TeamOperation) => void }) {
  const [name, setName] = useState(details.team.name || ''), [description, setDescription] = useState(details.team.description || '');
  const [error, setError] = useState(''), [notice, setNotice] = useState(''), [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState({ name: details.team.name || '', description: details.team.description || '' });
  const lock = useRef(false), dirty = name !== saved.name || description !== saved.description;
  useOwnerTeamDraftGuard(dirty || busy);
  const run = async (action: () => Promise<void>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(''); setNotice(''); try { await action(); } catch (error) { setError(message(error)); } finally { lock.current = false; setBusy(false); } };
  return <OwnerTeamDialog title="管理 Team" dirty={dirty} busy={busy} onClose={onClose}><form onSubmit={e => { e.preventDefault(); void run(async () => { await ownerTeamService.update(details.team.id, { name, description }); setSaved({ name, description }); await onChange(); setNotice('团队设置已保存。'); }); }}>
    <div className="ot-form-scroll"><label>团队名称<input required maxLength={100} disabled={busy} value={name} onChange={e => setName(e.target.value)}/></label><label>团队说明<textarea maxLength={2000} disabled={busy} value={description} onChange={e => setDescription(e.target.value)}/></label>
    <div className="ot-section-title"><h4>成员管理</h4><span>{details.members.length} 名成员</span></div>
    {details.members.map(m => <div key={m.id} className="ot-remove-member"><TeamBadge iconKey={memberIcon(m)} size="chat"/><span>{m.display_name}</span>{m.member_key === details.leader_member_id ? <small className="ot-tag">Leader</small> : <button type="button" className="ot-secondary ot-danger-text" disabled={busy} onClick={() => { if (window.confirm(`移除“${m.display_name}”会删除该成员实例及数据，请先保全需要的文件。确认继续？`)) void run(async () => { const op = await ownerTeamService.removeMember(details.team.id, m.member_key); onOperation(op); await onChange(); }); }}><UserMinus size={14}/>移除成员</button>}</div>)}
    <section className="ot-danger-zone"><h4>删除团队</h4><p>删除成员实例及共享数据，操作无法撤销。</p><button type="button" className="ot-danger" disabled={busy} onClick={() => { if (window.confirm('删除整个 Team 会删除成员实例和共享数据，无法撤销。确认继续？')) void run(async () => { const op = await ownerTeamService.removeTeam(details.team.id); onOperation(op); onClose(); await onChange(); }); }}><Trash2 size={14}/>删除 Team 与数据</button></section>
    {error && <p role="alert" className="ot-error">{error}</p>}{notice && <p role="status" className="ot-inline-status">{notice}</p>}</div>
    <footer><button type="button" className="ot-secondary" disabled={busy} onClick={() => { if (!dirty || window.confirm('有尚未保存的修改，确认关闭？')) onClose(); }}>关闭</button><button className="ot-primary" disabled={busy}><Check size={15}/>{busy ? '正在保存…' : '保存设置'}</button></footer>
  </form></OwnerTeamDialog>;
}
