import catalog from '../../../backend/internal/teampresentation/catalog.json';

export type TeamIconEntry = (typeof catalog)[number];
export const teamIconCatalog: TeamIconEntry[] = catalog;
export const workerIconChoices = catalog.filter(entry => !['leader','user','system','unknown'].includes(entry.key));
const normalized = (value?: string) => (value || '').trim().toLowerCase().replace(/[_ ]/g, '-');
export function validWorkerIcon(key?: string) { return !!key && workerIconChoices.some(entry => entry.key === key); }
const aliasKey = (value?: string) => catalog.find(entry => entry.aliases.some(alias => normalized(alias) === normalized(value)))?.key;
export function resolveTeamIcon(input: { leader?: boolean; iconKey?: string; override?: string; profileKey?: string; role?: string; name?: string; tags?: string[] }) {
  if (input.leader) return 'leader';
  if (validWorkerIcon(input.override)) return input.override!;
  if (validWorkerIcon(input.iconKey)) return input.iconKey!;
  const profile = catalog.find(entry => entry.key !== 'leader' && entry.profiles.includes(input.profileKey || ''));
  if (profile) return profile.key;
  const role = aliasKey(input.role), name = aliasKey(input.name);
  if (role || name) return role || name!;
  const candidates = new Set((input.tags || []).map(aliasKey).filter(Boolean));
  return candidates.size === 1 ? [...candidates][0]! : 'general';
}
export function memberIcon(member: { member_key: string; role: string; display_name: string; icon_key?: string }, leaderKey?: string) {
  return resolveTeamIcon({ leader: leaderKey ? member.member_key === leaderKey : member.role === 'leader', iconKey: member.icon_key, role: member.role, name: member.display_name });
}
export function iconEntry(key: string) { return catalog.find(entry => entry.key === key) || catalog.find(entry => entry.key === 'general')!; }
