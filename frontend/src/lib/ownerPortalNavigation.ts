import type { IEISystemInstance } from '../services/ieiSystemService';

const reportKeys = ['report_kind', 'report_date', 'report_revision'] as const;

export function ownerDashboardSection(params: URLSearchParams) {
 const tab = params.get('dashboard_tab');
 const detail = params.get('dashboard_detail');
 return {
  tab: tab === 'team' ? 'team' : 'overview',
  detail: tab === 'team' ? null : tab === 'growth' ? 'growth' : tab === 'reports' ? 'report' :
   detail === 'growth' || detail === 'report' || detail === 'tasks' ? detail : null,
  legacy: tab === 'growth' || tab === 'reports',
 } as const;
}

// Only known presentation parameters are carried back; never copy tokens or URLs.
export function ownerTeamOverviewURL(teamID: number, search = ''): string {
 const prior = new URLSearchParams(search);
 const result = new URLSearchParams({view:'overview',dashboard_tab:'team',workspace_tab:'teams'});
 for (const key of ['q','runtime','usage_range','team_q','expanded_team_id','selected_instance_id', ...reportKeys]) {const value=prior.get(key);if(value)result.set(key,value)}
 if (Number.isSafeInteger(teamID) && teamID>0) {
  result.set('selected_team_id',String(teamID));
  // A direct URL has no saved presentation. Returning from the portal must
  // preserve the user's collapsed state (absence of expanded_team_id).
  if (!search) result.set('expanded_team_id',String(teamID));
 }
 return `/ieisystem/list-instances?${result}`;
}
export type OwnerWorkspaceOutlet = {
 owner: string;
 reportInstance: (instance: IEISystemInstance) => void;
};

const presentationKeys = ['view','q','runtime','usage_range','dashboard_tab','dashboard_detail','workspace_tab','team_q','team_section','selected_team_id','expanded_team_id','selected_instance_id', ...reportKeys] as const;

export function ownerWorkspaceURL(path: string, search: string, updates: Record<string, string | null> = {}): string {
 const previous = new URLSearchParams(search);
 const next = new URLSearchParams();
 for (const key of presentationKeys) {
  const value = Object.hasOwn(updates, key) ? updates[key] : previous.get(key);
  if (value) next.set(key, value);
 }
 return next.size ? path+'?'+next.toString() : path;
}

export const ownerInstanceURL = (id: number, search: string, teamID?: number) =>
 ownerWorkspaceURL(`/ieisystem/instances/${id}`, search, {
  workspace_tab: teamID ? 'teams' : 'instances',
  ...(teamID ? { selected_team_id: String(teamID) } : {}),
 });

export const ownerTeamURL = (id: number, search: string) =>
 ownerWorkspaceURL(`/ieisystem/teams/${id}`, search, {workspace_tab:'teams',selected_team_id:String(id)});
