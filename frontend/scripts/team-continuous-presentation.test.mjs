import assert from 'node:assert/strict';
import fs from 'node:fs';
import ts from 'typescript';
const text=fs.readFileSync(new URL('../src/components/teams/teamChatPresentation.ts',import.meta.url),'utf8');
const mod={exports:{}};
new Function('exports',ts.transpileModule(text,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText)(mod.exports);
const { startsCollaborationSegment, chatAuthorType, chatPresentationContext, buildCollaborationPresentation }=mod.exports;
assert.equal(chatAuthorType('user-1','user-1',true,false),'member','real worker with a user-shaped member ID');
assert.equal(chatAuthorType('user-1','clawmanager',true,false),'system','platform forwarding does not become a colliding worker');
assert.equal(chatAuthorType('system','system',true,false),'member','actual member identity is not inferred solely from its name');
assert.equal(chatAuthorType('user-1','user-1',false,false),'user');
assert.equal(chatAuthorType('future-source','future-source',false,false),'unknown');
assert.equal(chatAuthorType('developer','developer',true,true),'system','explicit system process has precedence');
const first={id:'1',senderKey:'worker-1',authorType:'member',threadKey:'root-1',time:1000,tone:'normal',kind:'member'};
const next={...first,id:'2',time:2000};
assert.equal(startsCollaborationSegment(undefined,first),true);
assert.equal(startsCollaborationSegment(first,next),false);
// Team153's real shape: a reply can be a structured dispatch. The assignment
// and phase belong to its recipient, not to the author who wrote the plan.
const leaderArtifact={...first,id:'9356',senderKey:'leader',tone:'leader',segmentKey:'||1',time:1000,presentationContext:{revision:1}};
const leaderDispatch={...leaderArtifact,id:'9358',segmentKey:'work-gather|phase-gather|1',time:24000,presentationContext:{dispatchTarget:'collector',dispatchAssignmentId:'work-gather',dispatchPhaseId:'phase-gather'}};
const leaderPlan={...leaderArtifact,id:'9359',time:24001};
assert.equal(startsCollaborationSegment(leaderArtifact,leaderDispatch),false,'recipient work must not repeat the author header');
assert.equal(startsCollaborationSegment(leaderDispatch,leaderPlan),false,'returning to root coordination is still the same author');
const contexts = buildCollaborationPresentation([leaderArtifact,leaderDispatch,leaderPlan]);
assert.deepEqual(contexts.map(row=>row.startsSegment),[true,false,false]);
assert.equal(contexts[1].contextBoundary,'dispatch');
const dispatchContext=chatPresentationContext({assignmentId:'gather',phaseId:'gather-phase',revision:8},{type:'assignment'},'author','collector',false,false);
assert.deepEqual(dispatchContext,{dispatchTarget:'collector',dispatchAssignmentId:'gather',dispatchPhaseId:'gather-phase'},'structured reply dispatch must exclude recipient revision too');
assert.equal(chatPresentationContext({},undefined,'peer','other',true,false).dispatchTarget,'other','legacy explicit outbound remains compatible');
assert.equal(chatPresentationContext({assignment_id:'own',phase_id:'phase-a',revision:'2'},undefined,'peer','peer',false,false).assignmentId,'own');
assert.equal(chatPresentationContext({revision:'bad'},undefined,'peer','peer',false,false).revision,undefined);
assert.equal(chatPresentationContext({}, {type:'result'}, 'peer','leader',false,true).dispatchTarget,undefined,'feedback is not a dispatch');
assert.equal(chatPresentationContext({}, {type:'assignment'}, 'peer','peer',false,false).dispatchTarget,undefined,'self envelope does not become an outgoing target');
const owned={...first,presentationContext:{assignmentId:'own',phaseId:'phase-a',revision:1}};
const outbound={...next,tone:'assignment',presentationContext:dispatchContext};
const phase={...next,id:'phase',time:3000,presentationContext:{assignmentId:'own',phaseId:'phase-b',revision:1}};
assert.deepEqual(buildCollaborationPresentation([owned,outbound,phase]).map(row=>row.startsSegment),[true,false,false]);
assert.equal(buildCollaborationPresentation([owned,outbound,phase])[2].contextBoundary,'phase','real phase boundary stays visible without repeating author');
const revision={...phase,presentationContext:{assignmentId:'own',phaseId:'phase-a',revision:2}};
assert.equal(buildCollaborationPresentation([owned,outbound,revision])[2].startsSegment,true,'dispatch cannot erase own revision history');
assert.equal(buildCollaborationPresentation([owned,outbound,{...phase,presentationContext:{assignmentId:'different',revision:1}}])[2].startsSegment,true,'own work change remains a hard boundary');
assert.equal(buildCollaborationPresentation([owned,{...outbound,presentationContext:{}},revision])[2].startsSegment,true,'missing metadata cannot erase a retry boundary');
for (const tone of ['feedback','warning','error']) {
  assert.equal(buildCollaborationPresentation([owned,{...outbound,tone}])[1].startsSegment,true,`${tone} always stays independent`);
}
for (const changed of [{senderKey:'other'},{threadKey:'another-root'},{kind:'system'},{authorType:'unknown'},{time:400000},{time:0}]) {
  assert.equal(buildCollaborationPresentation([owned,{...outbound,...changed}])[1].startsSegment,true,JSON.stringify(changed));
}
const human={...first,authorType:'user',senderKey:'user-1',presentationContext:{}};
assert.deepEqual(buildCollaborationPresentation([human,{...human,id:'next',time:2000}]).map(row=>row.startsSegment),[true,true],'User always keeps independent headers, including same-root messages');
const snapshot=JSON.stringify([owned,outbound,phase]);
buildCollaborationPresentation([owned,outbound,phase]);
assert.equal(JSON.stringify([owned,outbound,phase]),snapshot,'presentation does not mutate event context or message identity');
assert.deepEqual(buildCollaborationPresentation([owned,{...outbound,senderKey:'other'},phase]).map(row=>row.startsSegment),[true,true,true],'A-B-A never merges');
const midnight={...owned,time:new Date(2026,9,9,23,59,59).getTime()};
assert.equal(startsCollaborationSegment(midnight,{...midnight,id:'tomorrow',time:midnight.time+2000}),true);
for (const changed of [{senderKey:'worker-2'},{threadKey:'root-2'},{authorType:'user'},{kind:'system'},{tone:'error'},{tone:'assignment'},{time:400000},{segmentKey:'phase-2'}]) assert.equal(startsCollaborationSegment(first,{...next,...changed}),true,JSON.stringify(changed));
assert.equal(startsCollaborationSegment({...first,tone:'error'},next),true);
assert.equal(startsCollaborationSegment({...first,senderKey:'same',authorType:'unknown'}, {...next,senderKey:'same',authorType:'unknown'}),true);
assert.equal(startsCollaborationSegment({...first,segmentKey:'work:revision:1'}, {...next,segmentKey:'work:revision:2'}),true);
console.log('PASS: adjacent-only continuous collaboration and identity/task/phase/failure boundaries');
