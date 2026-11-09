import api from './api';

export interface TeamDataState {table:string;field:string;captured:string|null;restored:string|null;rows:number}
export interface TeamDataSummary {tables:Array<{table:string;rows:number;quarantined_rows:number}>;states:TeamDataState[];sql_readback_verified:boolean}
export interface TeamDataReport {
 backup_id:string;purpose:string;consistency:string;storage:string;
 capture_started_at:string;capture_finished_at:string;integrity_verified:boolean;
 parts?:Array<{name:string;bytes:number;sha256:string}>;
 write_credential_revoked?:boolean;source_untouched:boolean;runtime_dependency_links?:number;
 restore?:{run_id:string;status:string;phase:string;failure?:string;team:TeamDataSummary;file_count:number;verified_file_count:number;verified_object_count:number;verified_redis_count:number};
 verification?:{integrity_verified:boolean;team:TeamDataSummary;file_count:number;object_count:number;redis_count:number;history_parts_verified:number};
}
export interface TeamDataJob {
 public_id:string;operation:'backup'|'restore'|'cleanup'|'verify'|'delete';source_backup_id?:string;
 status:string;phase:string;error_code?:string;cancel_requested:boolean;
 created_at:string;started_at?:string;finished_at?:string;report?:TeamDataReport;
}
export interface TeamDataOverview {configured:boolean;disabled?:boolean;storage?:string;consistency?:string;max_bytes?:number;current_teams?:number;historical_deleted_teams?:number}
export interface TeamDataArtifact {backup_id:string;commit_sha256:string;captured_at:string;bytes:number;protected:boolean;health:string;verified_at?:string;checked_at?:string;last_error?:string;leases:Record<string,string>;deleting:boolean;deleted:boolean;deleted_versions:number}
export interface TeamDataDeletePreview {backup_id:string;bytes:number;versions:number;expires_at:string;confirmation:string}
export interface TeamDataDiscovery {items:Array<{backup_id:string;state:string;error_code?:string;commit_sha256?:string;captured_at?:string;bytes:number}>;next_cursor:string;partial:boolean;authoritative_snapshot:boolean}
const base='/admin/system-backup/team-data';
export const teamDataBackupService={
	artifacts:async():Promise<TeamDataArtifact[]> => (await api.get(`${base}/artifacts`)).data.data.items,
	discover:async(cursor=''):Promise<TeamDataDiscovery> => (await api.get(`${base}/discover`,{params:{cursor}})).data.data,
	importArtifact:async(id:string):Promise<void> => {await api.post(`${base}/artifacts/${encodeURIComponent(id)}/import`,{})},
	protect:async(id:string,value:boolean):Promise<void> => {await api.post(`${base}/artifacts/${encodeURIComponent(id)}/protection`,{protected:value})},
	deletePreview:async(id:string):Promise<TeamDataDeletePreview> => (await api.post(`${base}/artifacts/${encodeURIComponent(id)}/delete-preview`,{})).data.data,
	deleteConfirmed:async(id:string,confirmation:string,key:string):Promise<TeamDataJob> => (await api.post(`${base}/artifacts/${encodeURIComponent(id)}/delete`,{confirmation},{headers:{'Idempotency-Key':key}})).data.data,
 overview:async(signal?:AbortSignal):Promise<TeamDataOverview>=>(await api.get(`${base}/overview`,{signal})).data.data,
 list:async(signal?:AbortSignal):Promise<TeamDataJob[]>=>(await api.get(`${base}/jobs`,{signal})).data.data.items,
 get:async(id:string,signal?:AbortSignal):Promise<TeamDataJob>=>(await api.get(`${base}/jobs/${encodeURIComponent(id)}`,{signal})).data.data,
 create:async(operation:TeamDataJob['operation'],key:string,source?:string):Promise<TeamDataJob>=>(await api.post(`${base}/jobs`,{operation,...(source?{source_backup_id:source}:{})},{headers:{'Idempotency-Key':key}})).data.data,
 cancel:async(id:string):Promise<TeamDataJob>=>(await api.post(`${base}/jobs/${encodeURIComponent(id)}/cancel`,{})).data.data,
};
export const teamDataActive=(j:TeamDataJob)=>['pending','running','canceling'].includes(j.status);
