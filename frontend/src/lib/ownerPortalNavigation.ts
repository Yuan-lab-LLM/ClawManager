// Only known presentation parameters are carried back; never copy tokens or URLs.
export function ownerOverviewURL(instanceID: number, search = ''): string {
 const prior = new URLSearchParams(search);
 const result = new URLSearchParams({view:'overview',dashboard_tab:'overview'});
 for (const key of ['q','runtime','usage_range']) {const value=prior.get(key);if(value)result.set(key,value)}
 if (Number.isSafeInteger(instanceID) && instanceID>0) {
  result.set('selected_instance_id',String(instanceID));
  result.set('expanded_instance_id',String(instanceID));
 }
 return `/ieisystem/list-instances?${result}`;
}

export function ownerTeamOverviewURL(teamID: number, search = ''): string {
 const prior = new URLSearchParams(search);
 const result = new URLSearchParams({view:'overview',dashboard_tab:'team',workspace_tab:'teams'});
 for (const key of ['q','runtime','usage_range','team_q','expanded_team_id','selected_instance_id','expanded_instance_id']) {const value=prior.get(key);if(value)result.set(key,value)}
 if (Number.isSafeInteger(teamID) && teamID>0) {
  result.set('selected_team_id',String(teamID));
  // A direct URL has no saved presentation. Returning from the portal must
  // preserve the user's collapsed state (absence of expanded_team_id).
  if (!search) result.set('expanded_team_id',String(teamID));
 }
 return `/ieisystem/list-instances?${result}`;
}
export function expandedInstanceFromSearch(search: string): number|null {
 const value=Number(new URLSearchParams(search).get('expanded_instance_id'));
 return Number.isSafeInteger(value)&&value>0?value:null;
}
