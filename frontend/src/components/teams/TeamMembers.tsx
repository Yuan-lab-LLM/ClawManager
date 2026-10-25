import { useEffect, useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Link } from "react-router-dom";
import { ArrowUpRight, ChevronLeft, ChevronRight, MonitorUp, Users, X } from "lucide-react";
import TeamBadge from './TeamBadge';
import { memberIcon } from '../../lib/teamIcons';
import type { Team, TeamMember } from "../../types/team";
import { memberRailState, memberRuntime, memberSchedule, memberSummary, memberHasRuntimeFault } from "./teamMemberPresentation";
import "./team-members.css";

function Avatar({ member, index, leaderKey }: { member: TeamMember; index: number; leaderKey?: string }) {
  return <TeamBadge iconKey={memberIcon(member,leaderKey)} ordinal={index + 1} className="team-roster-avatar" />;
}

export function MemberEntry({ member, compact = false }: { member: TeamMember; compact?: boolean }) {
  return member.instance_id ? <Link to={`/instances/${member.instance_id}`} target="_blank" rel="noreferrer" className={compact ? "team-roster-quick-entry" : "team-roster-entry"} aria-label={`进入${member.display_name}实例 #${member.instance_id}`}>
    {compact && <MonitorUp size={13} />}<span>{compact ? "进入" : "进入实例"}</span>{!compact && <ArrowUpRight size={13} />}
  </Link> : <span className="team-roster-no-instance">{compact ? "待创建" : "实例待创建"}</span>;
}

export function MemberPager({ page, pages, onPage }: { page: number; pages: number; onPage: (page: number) => void }) {
  if (pages <= 1) return null;
  return <nav className="team-roster-pager" aria-label="成员分页"><button type="button" disabled={page === 0} onClick={() => onPage(page - 1)} aria-label="上一页成员"><ChevronLeft size={16} /></button><span aria-live="polite">{page + 1} / {pages}</span><button type="button" disabled={page === pages - 1} onClick={() => onPage(page + 1)} aria-label="下一页成员"><ChevronRight size={16} /></button></nav>;
}

type RosterProps = { members: TeamMember[]; start: number; leaderKey?: string; onDetails: (id: number) => void };

export function MemberRail({ members, start, leaderKey, onOpen, onDetails, total, expanded, panelId, pager }: RosterProps & { onOpen: () => void; total: number; expanded: boolean; panelId: string; pager: ReactNode }) {
  return <>
    <button type="button" className="team-roster-rail-heading" aria-label="展开成员与团队配置" aria-expanded={expanded} aria-controls={panelId} onClick={onOpen}><Users size={18} /><span>成员 <small>{total}</small></span></button>
    {members.map((member, index) => {
      const state = memberRailState(member);
      return <div key={member.id} className="team-roster-mini" data-member-id={member.id} data-tone={state.tone}>
        <button type="button" className="team-roster-mini-person" onClick={() => onDetails(member.id)} title={`${member.display_name} · ${member.member_key} · 调度 ${memberSchedule(member.status).label} · Runtime ${memberRuntime(member.availability, member.runtime_status).label}`} aria-label={`查看${member.display_name}成员详情`}>
          <Avatar member={member} index={start + index} leaderKey={leaderKey} />
          <span className="team-roster-mini-name">{member.display_name || member.member_key}</span>
        </button>
        <div className="team-roster-mini-footer"><span className="team-roster-mini-state"><i />{state.label}</span><MemberEntry member={member} compact /></div>
      </div>;
    })}
    {pager}
  </>;
}

export function MemberCards({ members, start, leaderKey, onDetails }: RosterProps) {
  return <div className="team-roster-cards">
    {members.length === 0 && <p className="team-roster-empty">暂无成员</p>}
    {members.map((member, index) => {
      const schedule = memberSchedule(member.status), runtime = memberRuntime(member.availability, member.runtime_status);
      return <article key={member.id} className="team-roster-card" data-member-id={member.id} data-tone={memberRailState(member).tone}>
        <div className="team-roster-card-heading"><Avatar member={member} index={start + index} leaderKey={leaderKey} /><div className="team-roster-person"><strong title={member.display_name}>{member.display_name || member.member_key}</strong><div className="team-roster-meta">{member.runtime_type || "openclaw"} · {member.instance_mode || "lite"}{member.instance_id ? ` · #${member.instance_id}` : ""}</div></div><MemberEntry member={member} /></div>
        <div className="team-roster-status"><span data-tone={schedule.tone}>调度 · {schedule.label}</span><span data-tone={runtime.tone}>运行 · {runtime.label}</span><button type="button" onClick={() => onDetails(member.id)} aria-label={`查看${member.display_name}完整详情`}>详情<ChevronRight size={13} /></button></div>
        <div className="team-roster-summary" data-error={memberHasRuntimeFault(member)} title={memberSummary(member)}>{memberSummary(member)}</div>
      </article>;
    })}
  </div>;
}

// A native top-layer modal avoids the chat header/panel stacking-context bug.
// Long descriptions scroll only here; the member roster itself never scrolls.
export function MemberInfoDialog({ member, team, renderDetails, onClose }: { member?: TeamMember; team: Team; renderDetails: (member: TeamMember) => ReactNode; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const dialog = ref.current;
    const trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialog?.showModal();
    return () => { dialog?.close(); if (trigger?.isConnected && trigger.getClientRects().length) trigger.focus(); };
  }, []);
  return createPortal(<dialog ref={ref} className="team-member-dialog" data-team-member-dialog aria-labelledby={titleId} onCancel={onClose} onKeyDown={event => {
    if (event.key !== "Tab") return;
    const controls = [...event.currentTarget.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input, textarea, select, summary')].filter(node => node.getClientRects().length);
    const first = controls[0], last = controls[controls.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  }} onClick={event => { if (event.target === ref.current) { const r = ref.current!.getBoundingClientRect(); if (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom) onClose(); } }}>
    <header><div><small>{member ? "成员详情" : "团队配置"}</small><h2 id={titleId}>{member?.display_name || team.name}</h2></div>{member && <MemberEntry member={member} />}<button type="button" onClick={onClose} aria-label="关闭成员详情"><X size={18} /></button></header>
    <div className="team-member-dialog-body">{member ? <>
      <dl><dt>成员标识</dt><dd>{member.member_key}</dd><dt>角色</dt><dd>{member.role}</dd><dt>Runtime</dt><dd>{member.runtime_type || "openclaw"} · {member.instance_mode || "lite"}</dd><dt>调度状态</dt><dd>{memberSchedule(member.status).label} ({member.status})</dd><dt>运行状态</dt><dd>{memberRuntime(member.availability, member.runtime_status).label} ({member.availability || "unknown"})</dd></dl>
      {memberHasRuntimeFault(member) && <p className="team-roster-error">{member.blocked_reason}</p>}
      {member.last_summary && <section><h3>最近摘要</h3><p>{member.last_summary}</p></section>}
      {renderDetails(member)}
    </> : <dl><dt>Team</dt><dd>#{team.id}</dd><dt>协作方式</dt><dd>{team.communication_mode}</dd><dt>共享目录</dt><dd>{team.shared_mount_path}</dd>{team.description && <><dt>说明</dt><dd>{team.description}</dd></>}</dl>}</div>
  </dialog>, document.body);
}
