type WorkspaceUsage = {workspace_usage_bytes?: number; workspace_usage_observed_at?: string | null};

export function workspaceUsageValue(usage: WorkspaceUsage): number | undefined {
 const value=usage.workspace_usage_bytes;
 if (!usage.workspace_usage_observed_at || !Number.isFinite(Date.parse(usage.workspace_usage_observed_at)) || value===undefined || !Number.isFinite(value) || value<0) return undefined;
 return value;
}

export function formatWorkspaceUsage(usage: WorkspaceUsage, unavailable: string): string {
 const value=workspaceUsageValue(usage);
 if (value===undefined) return unavailable;
 const units=['B','KB','MB','GB','TB'];
 let size=value,index=0;
 while(size>=1024 && index<units.length-1){size/=1024;index++;}
 return `${size.toFixed(size>=10 || index===0 ? 0 : 1)} ${units[index]}`;
}

// A current status response can explicitly invalidate an old detail snapshot
// after reset. Do not merge its null proof with stale instance byte values.
export function selectWorkspaceUsage(instance: WorkspaceUsage, status?: WorkspaceUsage | null): WorkspaceUsage {
 return status && 'workspace_usage_observed_at' in status ? status : instance;
}

export function workspaceUsageText(locale: string, usage: WorkspaceUsage) {
 const unavailable=locale==='zh'?'不可用':'Unavailable';
 const value=workspaceUsageValue(usage);
 if(value===undefined) return {unavailable,description:unavailable};
 const date=new Date(usage.workspace_usage_observed_at!).toLocaleString(locale);
 const measured=locale==='zh'?`最近测量：${date}`:`Last measured: ${date}`;
 return {unavailable,description:`${formatWorkspaceUsage(usage,unavailable)} · ${measured}`};
}
