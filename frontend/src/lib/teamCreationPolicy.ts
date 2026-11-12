import type { CustomTeamTemplate } from '../types/customTeamTemplate';

export const TEAM_LEADER_RUNTIME = 'openclaw' as const;
export const TEAM_INSTANCE_MODE = 'lite' as const;
export const TEAM_WORKER_RUNTIMES = ['openclaw', 'hermes', 'opencode', 'deepseek-harness'] as const;
export const TEAM_CREATION_NOTICE = 'Leader 固定为 OpenClaw Lite；Worker 可选 OpenClaw、Hermes、OpenCode、DeepSeek Harness，均使用 Lite。';

export function teamRuntimeOptions(leader: boolean, advertised?: readonly string[]) {
  const options = leader ? [TEAM_LEADER_RUNTIME] : [...TEAM_WORKER_RUNTIMES];
  return options.filter(value => !advertised || advertised.includes(value));
}

export function reservedTeamLeaderRole(role: string) {
  return ['leader', 'team-leader'].includes(role.trim().toLowerCase().replaceAll('_', '-').replaceAll(' ', '-'));
}

export function teamMemberCreationError(member: { isLeader: boolean; role: string; runtimeType: string; instanceMode: string }) {
  if (!member.isLeader && reservedTeamLeaderRole(member.role)) return 'Worker 角色与 Leader 身份冲突，请先修订模板。';
  if (member.instanceMode !== TEAM_INSTANCE_MODE) return 'Team 成员仅支持 Lite，请重新确认配置。';
  if (!teamRuntimeOptions(member.isLeader).includes(member.runtimeType as typeof TEAM_WORKER_RUNTIMES[number])) return member.isLeader ? 'Leader 必须使用 OpenClaw Lite。' : '不支持的 Worker Runtime。';
  return '';
}

export function customTemplateCreationError(template: CustomTeamTemplate) {
  if (template.creation_error) return template.creation_error;
  const members = template.spec.members;
  if (members.length < 2 || members.length > 6 || (template.requested_member_count != null && members.length !== template.requested_member_count)) return '自定义 Team 必须为 2～6 人，且与指定人数一致。';
  const ids = new Set<string>();
  for (let index = 0; index < members.length; index++) {
    const member = members[index];
    const id = member.memberId.trim().toLowerCase().replaceAll('_', '-').replaceAll(' ', '-');
    if (!id || ids.has(id)) return '模板成员标识为空或重复，请先修订模板。';
    ids.add(id);
    if (index === 0 ? !member.isLeader : member.isLeader || reservedTeamLeaderRole(member.role)) return '模板必须有且只有首位 Leader，请先修订模板。';
  }
  return '';
}

export function teamRuntimeLabel(type: string) {
  return ({ openclaw: 'OpenClaw', hermes: 'Hermes', opencode: 'OpenCode', 'deepseek-harness': 'DeepSeek Harness' } as Record<string,string>)[type] || type;
}
