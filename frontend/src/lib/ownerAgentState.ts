import {getInstanceDisplayStatus} from './instanceStatus';
import type {TeamMember} from '../types/team';
import {memberHasRuntimeFault, memberIsStarting} from '../components/teams/teamMemberPresentation';

export type OwnerAgentState = 'running' | 'sleeping' | 'stopped' | 'error' | 'starting' | 'offline' | 'unknown';

export function ownerAgentState(status: string, stoppedReason?: string): OwnerAgentState {
 const state = getInstanceDisplayStatus(status, stoppedReason);
 if (['running','online','ready','idle','busy'].includes(state)) return 'running';
 if (['auto_sleep','sleeping'].includes(state)) return 'sleeping';
 if (['stopped','deleted'].includes(state)) return 'stopped';
 if (['error','failed','blocked'].includes(state)) return 'error';
 if (['creating','starting','pending','restarting','resetting','upgrading','deleting'].includes(state)) return 'starting';
 return state==='offline' ? 'offline' : 'unknown';
}

// Keep the runtime observation authoritative; idle scheduling is not health proof.
export function ownerMemberAgentState(member: TeamMember): string {
 if (member.runtime_status==='stopped' || member.runtime_observation?.reason==='intentionally_stopped') return 'stopped';
 if (memberHasRuntimeFault(member) || member.status==='failed') return 'error';
 if (member.status==='deleted') return 'stopped';
 if (member.status==='deleting' || memberIsStarting(member)) return 'starting';
 if (member.runtime_observation) return member.runtime_observation.state;
 if (member.status==='creating') return 'starting';
 if (member.availability==='blocked') return 'error';
 return member.availability || 'unknown';
}
