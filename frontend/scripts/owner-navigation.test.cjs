const assert=require('node:assert/strict');
const fs=require('node:fs');const vm=require('node:vm');const ts=require('typescript');
const source=fs.readFileSync(require('node:path').join(__dirname,'../src/lib/ownerPortalNavigation.ts'),'utf8');
const context={exports:{},URLSearchParams};vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText,context);
const {ownerWorkspaceURL,ownerInstanceURL,ownerTeamURL,ownerDashboardSection,ownerTeamOverviewURL}=context.exports;
const url=new URL(ownerWorkspaceURL('/ieisystem/list-instances','?view=instance&dashboard_tab=growth&q=办公&runtime=opencode&usage_range=30d&expanded_instance_id=269&token=SECRET&redirect=https://example.com',{view:'overview',dashboard_tab:'overview'}),'https://portal.invalid');
assert.equal(url.searchParams.get('view'),'overview');assert.equal(url.searchParams.get('dashboard_tab'),'overview');
assert.equal(url.searchParams.get('q'),'办公');assert.equal(url.searchParams.get('runtime'),'opencode');
assert.equal(url.searchParams.get('token'),null);assert.equal(url.searchParams.get('redirect'),null);
assert.equal(url.searchParams.get('expanded_instance_id'),null);
const team=new URL(ownerTeamURL(186,'?team_q=验收&usage_range=30d&report_kind=month&report_date=2026-10-19&report_revision=2&token=SECRET&redirect=https://example.com'),url.origin);
const member=new URL(ownerInstanceURL(628,team.search,186),url.origin);
assert.equal(member.pathname,'/ieisystem/instances/628');
assert.equal(member.searchParams.get('workspace_tab'),'teams');
assert.equal(member.searchParams.get('selected_team_id'),'186');
assert.equal(member.searchParams.get('team_q'),'验收');
assert.equal(member.searchParams.get('token'),null);
assert.equal(member.searchParams.get('redirect'),null);
for(const search of [member.search,new URL(ownerTeamOverviewURL(186,member.search),url.origin).search]){
 const params=new URLSearchParams(search);
 assert.equal(params.get('report_kind'),'month');assert.equal(params.get('report_date'),'2026-10-19');assert.equal(params.get('report_revision'),'2');
}
assert.equal(ownerDashboardSection(new URLSearchParams('dashboard_tab=growth')).detail,'growth');
assert.equal(ownerDashboardSection(new URLSearchParams('dashboard_tab=reports')).detail,'report');
assert.equal(ownerDashboardSection(new URLSearchParams('dashboard_tab=team&dashboard_detail=report')).detail,null);
console.log('PASS: overview navigation, removed expansion, preserved filters, no token/redirect propagation');
