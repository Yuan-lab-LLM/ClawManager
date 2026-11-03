const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),ts=require('typescript'),path=require('node:path');
const code=ts.transpileModule(fs.readFileSync(path.join(__dirname,'../src/lib/ownerTaskState.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText;
const context={exports:{}};vm.runInNewContext(code,context);const {taskAchieved,taskStateLabel,statisticsTime}=context.exports;
const task={target:1,progress:1,awarded:false};assert(taskAchieved(task));assert.equal(taskStateLabel(task),'已达标，奖励待入账');
assert.equal(taskAchieved({...task,progress:0}),false);assert.equal(taskStateLabel({...task,awarded:true}),'已发放');
assert.equal(taskStateLabel({...task,reward:'收藏章'}),'已达标，解锁待同步');assert.equal(taskStateLabel({...task,reward:'收藏章',awarded:true}),'已收藏');
assert.equal(statisticsTime(), '时间暂不可用');assert.match(statisticsTime('2026-09-21T04:00:00Z'),/12:00/);
console.log('PASS task completion/pending-award/collection states and honest snapshot timestamp');
