import api from './api';
export interface CalendarDocument {schema_version:number;region:string;timezone:string;year:number;coverage:'official_full_year'|'partial';sources:{id:string;title:string;url:string;published_at:string}[];days:{date:string;kind:'holiday'|'workday';name:string;source_id:string}[]}
export interface CalendarVersion {document:CalendarDocument;version:number;actor:number;at:string;builtin?:boolean}
export interface CalendarPreview {document:CalendarDocument;expected_version:number;added:number;changed:number;removed:number;affected_users:number;examples:{owner:string;before:number;after:number}[]}
export interface PortalUser {owner:string;xp:number;level:number;tokens:number;questions:number|null;sessions:number;tasks_done:number;tasks_total:number;snapshot_at:string;ready:boolean;visits:{streak:number;longest:number;total:number;provisional:boolean}}
export const portalAdminService={
 users:async(search:string,sort:string,page:number):Promise<{users:PortalUser[];total:number;page:number;as_of:string}> => (await api.get('/admin/northbound/portal/users',{params:{search,sort,page},timeout:15000})).data.data,
 calendar:async(year:number):Promise<{head:CalendarVersion;history:CalendarVersion[]}> => (await api.get('/admin/northbound/portal/calendar',{params:{year}})).data.data,
 preview:async(document:unknown):Promise<CalendarPreview> => (await api.post('/admin/northbound/portal/calendar',{action:'preview',...(typeof document==='string'?{document_json:document}:{document})})).data.data,
 publish:async(document:CalendarDocument,expected_version:number):Promise<CalendarVersion> => (await api.post('/admin/northbound/portal/calendar',{action:'publish',document,expected_version,confirmed:true})).data.data,
 rollback:async(year:number,revision:number,expected_version:number):Promise<CalendarVersion> => (await api.post('/admin/northbound/portal/calendar',{action:'rollback',year,revision,expected_version,confirmed:true})).data.data,
};
