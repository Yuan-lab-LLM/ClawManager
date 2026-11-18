import test from 'node:test';
import assert from 'node:assert/strict';
import { preferenceKey, readPreferences, validPins, resultKey, taskRoute, agentRoute, chartSegments, formatMoney, comparison, resumeTasks } from './model.ts';

test('preferences isolate accounts, reject malformed data, preserve explicit empty selection', () => {
 assert.notEqual(preferenceKey(1), preferenceKey(2));
 assert.deepEqual(readPreferences('{bad'), {pins:null,read:[]});
 assert.deepEqual(readPreferences('{"pins":[],"read":[]}'), {pins:[],read:[]});
 assert.deepEqual(readPreferences('{"pins":[1,1,-2,"2",2,3,4,5,6,7],"read":[4,"x"]}'), {pins:[1,2,3,4,5,6],read:['x']});
});
test('pins validate visible authorization without destroying stored membership on a capped response', () => {
 assert.deepEqual(validPins([1,2,3], [{id:2}]), [2]);
 assert.deepEqual(validPins(null, [{id:2},{id:3}]), [2,3]);
 assert.deepEqual(validPins([], [{id:2}]), []);
});
test('result versions are independent of instance state and task display keys', () => {
 const task={source:'team',team_id:4,id:'8',finished_at:'2026-11-12',updated_at:'now',status:'succeeded'};
 assert.equal(resultKey(task), resultKey({...task,status:'failed'}));
 assert.notEqual(resultKey(task), resultKey({...task,team_id:5}));
 assert.notEqual(resultKey(task), resultKey({...task,finished_at:'later'}));
 assert.deepEqual(resumeTasks([task], [resultKey(task)], 'completed'), []);
 assert.equal(resumeTasks([{...task,status:'failed'}], [resultKey(task)], 'failed').length,1);
});
test('navigation uses actual routes, never a fabricated session or lifecycle action', () => {
 const agents=[{id:2,team_id:4,status:'stopped'},{id:3,status:'running'}];
 assert.equal(agentRoute(agents[0]), '/teams/4');
 assert.equal(agentRoute(agents[1]), '/instances/3');
 assert.equal(taskRoute({source:'team',team_id:4,session_id:'secret'},agents), '/teams/4');
 assert.equal(taskRoute({source:'runtime',instance_id:3,session_id:'secret'},agents), '/instances/3');
 assert.equal(taskRoute({source:'runtime',instance_id:99},agents), null);
 assert.equal(taskRoute({source:'team',team_id:99},agents), null);
});
test('chart gaps preserve unknown separately from actual zero', () => {
 assert.deepEqual(chartSegments([null,0,2,null,4,null,0]), [[{index:1,value:0},{index:2,value:2}],[{index:4,value:4}],[{index:6,value:0}]]);
 assert.deepEqual(chartSegments([null,null]), []);
});
test('money never merges currencies, never hides tiny positive spend and tolerates snapshots', () => {
 assert.match(formatMoney(.000000001,'USD','en'), /1/);
 assert.match(formatMoney(2,'private-credit','en'), /private-credit/);
 assert.match(formatMoney(0,'USD','en'), /0/);
 assert.equal(formatMoney(null,'USD','en'),'—');
 assert.equal(comparison(12,10,false),null);
 assert.equal(comparison(null,10,true),null);
 assert.equal(comparison(12,0,true),null);
 assert.equal(comparison(12,10,true),20);
});
test('unavailable ranking fields remain unknown and currencies have independent rankings', async () => {
 const { usageRanking } = await import('./model.ts');
 const rows=[{instance_id:1,tokens:100,costs:{USD:2}},{instance_id:2,tokens:50,costs:{EUR:3}}];
 assert.deepEqual(usageRanking(rows,'USD',true).map(row=>row.instance_id),[1]);
 assert.deepEqual(usageRanking(rows,'EUR',true).map(row=>row.instance_id),[2]);
 assert.deepEqual(usageRanking(rows,null,false),[]);
});
test('reporting dates use Shanghai midnight instead of UTC calendar slicing', async () => {
 const { reportingDate } = await import('./model.ts');
 assert.equal(reportingDate('2026-11-05T16:00:00Z','Asia/Shanghai'),'2026-11-06');
 assert.equal(reportingDate('2026-11-05T15:59:59Z','Asia/Shanghai'),'2026-11-05');
 assert.equal(reportingDate('2026-11-11T16:00:00Z','Asia/Shanghai'),'2026-11-12');
 assert.equal(reportingDate('2026-11-12T03:45:17Z','Asia/Shanghai',true),'2026-11-12 11:45:17');
});
test('unavailable populated activity masks zero defaults; partial recorded values remain partial', async () => {
 const { activityCounts } = await import('./model.ts');
 const day={date:'2026-11-12',events:0,calls:0,tasks:0,tokens:0,observed:false};
 assert.deepEqual(activityCounts(day,{status:'unavailable'}),{events:null,calls:null,tasks:null,status:'unavailable'});
 assert.deepEqual(activityCounts({...day,events:3,calls:2,tasks:1},{status:'partial'}),{events:3,calls:2,tasks:1,status:'partial'});
 assert.deepEqual(activityCounts({...day,observed:true},{status:'ready'}),{events:0,calls:0,tasks:0,status:'ready'});
 assert.equal(day.calls,0);
});
test('gauge ratios distinguish unavailable and zero limits and preserve oversubscription', async () => {
 const { quotaGauge } = await import('./model.ts');
 assert.deepEqual(quotaGauge(0, 10), {ratio:0, needle:0});
 assert.deepEqual(quotaGauge(15, 10), {ratio:1.5, needle:1});
 assert.deepEqual(quotaGauge(0, 0), {ratio:null, needle:null});
 assert.deepEqual(quotaGauge(5, null), {ratio:null, needle:null});
});
test('state ring totals use every aggregate state, independent of the presentation cap', async () => {
 const { stateRing } = await import('./model.ts');
 const ring = stateRing({busy:200,idle:100,running:20,sleeping:10,starting:1,failed:2,offline:3,unknown:4});
 assert.equal(ring.total,340);
 assert.equal(ring.running,320,'running includes busy, idle and running-with-unknown-activity Agents');
 assert.equal(ring.segments.reduce((n, part)=>n+part.count,0),340);
 assert.ok(Math.abs(ring.segments.reduce((n, part)=>n+part.share,0)-1)<1e-9);
 assert.deepEqual(stateRing({}).segments.map(part=>part.share),Array(8).fill(0));
 assert.equal(stateRing({}).running,0);
 assert.equal(stateRing({sleeping:4,starting:3,failed:2,offline:1,unknown:5}).running,0,'non-running states must not inflate the numerator');
});

test('smooth history paths pass through samples without overshooting spikes', async () => {
 const { smoothPath } = await import('./model.ts');
 assert.equal(smoothPath([]), '');
 assert.equal(smoothPath([{x:0,y:0}]), 'M 0 0');
 assert.equal(smoothPath([{x:0,y:10},{x:10,y:0},{x:20,y:10}]), 'M 0 10 C 5 10 5 0 10 0 C 15 0 15 10 20 10');
});
