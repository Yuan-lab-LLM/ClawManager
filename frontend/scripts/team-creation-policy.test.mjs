import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { customTemplateCreationError, teamMemberCreationError, teamRuntimeOptions } from '../src/lib/teamCreationPolicy.ts';

assert.deepEqual(teamRuntimeOptions(true, ['openclaw','opencode','hermes']), ['openclaw']);
assert.deepEqual(teamRuntimeOptions(false), ['openclaw','hermes','opencode','deepseek-harness']);
for (const runtime of teamRuntimeOptions(false)) {
  assert.equal(teamMemberCreationError({isLeader:false,role:'developer',runtimeType:runtime,instanceMode:'lite'}), '');
  assert.ok(teamMemberCreationError({isLeader:false,role:'developer',runtimeType:runtime,instanceMode:'pro'}));
}
assert.ok(teamMemberCreationError({isLeader:true,role:'leader',runtimeType:'opencode',instanceMode:'lite'}));
const template={spec:{members:[{memberId:'leader',isLeader:true,role:'leader'},{memberId:'worker',isLeader:false,role:'developer'}]}};
const original=JSON.stringify(template);
assert.equal(customTemplateCreationError(template),'');
assert.equal(JSON.stringify(template),original);
for (const role of ['leader','team_leader','Team Leader']) {
  const invalid=structuredClone(template);invalid.spec.members[1].role=role;
  assert.ok(customTemplateCreationError(invalid));
}
assert.ok(customTemplateCreationError({...template,creation_error:'Legacy invalid'}));
assert.ok(customTemplateCreationError({...template,requested_member_count:3}));
// Bind the tested admission presentation to both actual pages, not a detached
// policy helper. Historical display types still support Pro.
for (const file of ['../src/components/teams/OwnerTeamWorkspace.tsx','../src/pages/teams/CreateTeamPage.tsx']) {
  const source=readFileSync(new URL(file,import.meta.url),'utf8');
  assert.ok(source.includes('teamMemberCreationError')&&source.includes('teamRuntimeOptions'));
  assert.ok(!source.includes('<option value="pro">')&&!source.includes('<option value="pro" '));
  assert.ok(source.includes('固定 Leader'));
}
assert.ok(readFileSync(new URL('../src/pages/instances/CreateInstancePage.tsx',import.meta.url),'utf8').includes('pro'));
console.log('CM/northbound/custom Team admission, immutable drafts and ordinary Pro preservation: PASS');

const memberEntries=readFileSync(new URL('../src/components/teams/TeamMembers.tsx',import.meta.url),'utf8');
assert.ok(memberEntries.includes('target="_blank" rel="noopener noreferrer"'));
assert.ok(!memberEntries.includes('target={entry ?'));
const ownerEntries=readFileSync(new URL('../src/components/teams/OwnerTeamWorkspace.tsx',import.meta.url),'utf8');
assert.ok(ownerEntries.includes('<Link to={`/ieisystem/instances/${member.instance_id}`} target="_blank" rel="noopener noreferrer"'));
