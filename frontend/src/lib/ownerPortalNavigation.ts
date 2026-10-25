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
export function expandedInstanceFromSearch(search: string): number|null {
 const value=Number(new URLSearchParams(search).get('expanded_instance_id'));
 return Number.isSafeInteger(value)&&value>0?value:null;
}
