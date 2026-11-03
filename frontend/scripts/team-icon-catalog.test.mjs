import assert from 'node:assert/strict';
import fs from 'node:fs';
import ts from 'typescript';
const catalog=JSON.parse(fs.readFileSync(new URL('../../backend/internal/teampresentation/catalog.json',import.meta.url),'utf8'));
const source=fs.readFileSync(new URL('../src/lib/teamIcons.ts',import.meta.url),'utf8');
const mod={exports:{}};
new Function('exports','require',ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS,esModuleInterop:true}}).outputText)(mod.exports,()=>({default:catalog,__esModule:true}));
const {resolveTeamIcon,memberIcon,validWorkerIcon}=mod.exports;
assert.equal(new Set(catalog.map(e=>e.key)).size,catalog.length);
for(const e of catalog) {
 for(const profile of e.profiles) assert.equal(resolveTeamIcon({leader:e.key==='leader',profileKey:profile}),e.key);
 for(const alias of e.aliases) assert.equal(resolveTeamIcon({role:alias}),e.key,alias);
}
const templateModule={exports:{}};
new Function('exports',ts.transpileModule(fs.readFileSync(new URL('../src/lib/teamTemplates.ts',import.meta.url),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText)(templateModule.exports);
for(const template of templateModule.exports.BUILTIN_MEMBER_TEMPLATES) {
  for(const member of template.members) {
    const key=resolveTeamIcon({leader:member.isLeader,profileKey:member.agentProfileKey,role:member.role,name:member.name});
    assert.notEqual(key,'general',`${template.id}/${member.memberId} canonical profile`);
    assert.notEqual(resolveTeamIcon({leader:member.isLeader,role:member.role,name:member.name}),'general',`${template.id}/${member.memberId} legacy role/name`);
  }
}
assert.equal(resolveTeamIcon({role:'数据分析师'}),'data');
assert.equal(resolveTeamIcon({profileKey:'custom.team-template.9.worker',role:'worker',tags:['news-collection']}),'news');
assert.equal(resolveTeamIcon({role:'worker',tags:['frontend','data-analysis']}),'general');
assert.equal(memberIcon({member_key:'not-a-leader',role:'worker',display_name:'Not a leader'}),'general');
assert.equal(memberIcon({member_key:'user-1',role:'developer',display_name:'User specialist'}),'developer');
assert.equal(resolveTeamIcon({leader:true,override:'data'}),'leader');
for(const key of ['user','leader','system','unknown','<svg>','https://bad.test/icon']) assert.equal(validWorkerIcon(key),false);
console.log('PASS: shared canonical icon catalog, all profile/alias families and legacy/custom fallback');
