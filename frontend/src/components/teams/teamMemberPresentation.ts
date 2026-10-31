import type { TeamMember } from "../../types/team";

export function memberSchedule(status: string) {
  const labels: Record<string, string> = { creating: "创建中", idle: "空闲", busy: "执行中", failed: "失败", offline: "离线", deleting: "删除中", deleted: "已删除" };
  return { label: labels[status] || status || "待确认", tone: status === "busy" ? "active" : status === "failed" ? "error" : "neutral" };
}

export function memberIsStarting(member: Pick<TeamMember, 'status' | 'runtime_status' | 'runtime_observation'>) {
  if (member.runtime_observation) return member.runtime_observation.state === 'starting';
  return !['failed', 'deleting', 'deleted'].includes(member.status) && ['creating', 'starting', 'pending'].includes((member.runtime_status || '').trim().toLowerCase());
}

export function memberHasRuntimeFault(member: TeamMember) {
  // Compatibility for old startup projections: the structured lifecycle
  // status, not an arbitrary error message, identifies a normal transition.
  return !!member.blocked_reason && !(memberIsStarting(member) && member.blocked_reason.trim().startsWith('Runtime unavailable: '));
}

export function memberRuntime(availability?: string, runtimeStatus?: string) {
  if (['creating', 'starting', 'pending'].includes((runtimeStatus || '').trim().toLowerCase())) return { label: '启动中', tone: 'neutral' };
  const labels: Record<string, string> = { idle: "空闲", busy: "忙碌", blocked: "受阻", offline: "离线", unknown: "待确认" };
  const value = availability || "unknown";
  return { label: labels[value] || value, tone: value === "blocked" || value === "offline" ? "error" : value === "busy" ? "active" : value === "idle" ? "good" : "unknown" };
}

// The rail must not let an idle scheduling projection hide a runtime failure.
export function memberRailState(member: TeamMember) {
  if (memberHasRuntimeFault(member)) return { label: "运行异常", tone: "error" };
  if (member.status === "failed" || member.status === "deleting" || member.status === "deleted") return memberSchedule(member.status);
  if (member.runtime_observation) {
    const state = member.runtime_observation.state;
    if (state === 'starting') return { label: '启动中', tone: 'neutral' };
    if (state === 'offline') return { label: member.runtime_observation.reason === 'collaboration_offline' ? '协作离线' : '实例离线', tone: 'error' };
    if (state === 'unknown') {
      if (member.runtime_observation.reason === 'collaboration_heartbeat_missing') return { label: '协作心跳待检查', tone: 'error' };
      if (member.runtime_observation.reason === 'presence_unavailable') return { label: '在线状态暂不可查', tone: 'unknown' };
      return { label: member.runtime_observation.reason === 'collaboration_unconfirmed' ? '协作待确认' : '待确认', tone: 'unknown' };
    }
    return member.availability === 'busy' ? { label: '执行中', tone: 'active' } : { label: '空闲', tone: 'good' };
  }
  if (memberIsStarting(member)) return { label: '启动中', tone: 'neutral' };
  if (member.status === 'creating') return memberSchedule(member.status);
  if (!member.availability || !["idle", "busy"].includes(member.availability)) return memberRuntime(member.availability);
  if (member.availability === "busy") return { label: "运行忙碌", tone: "active" };
  return memberSchedule(member.status);
}

// Reserve the larger of expanded cards (128px + 10px gap) and the rail
// (164px including a two-line name, missing-instance label and 18px vertical padding).
// Header/padding allowance is 82px; paginated layout uses a 110px allowance.
// Pagination (not scrolling or smaller text) handles short windows and larger teams.
export function memberPageSize(availableHeight: number, count: number) {
  const visible = Math.min(3, Math.max(1, count));
  if (count <= 3 && availableHeight >= 82 + visible * 164 - 10) return visible;
  return Math.max(1, Math.min(3, Math.floor((availableHeight - 110) / 164)));
}

export function memberSummary(member: TeamMember) {
  if (memberIsStarting(member) && !memberHasRuntimeFault(member)) return '实例启动中，正在确认运行状态。';
  return member.blocked_reason || member.runtime_intent || member.last_summary || "暂无工作摘要";
}
