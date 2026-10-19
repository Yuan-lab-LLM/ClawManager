const assert=require('node:assert/strict'),fs=require('node:fs'),ts=require('typescript'),vm=require('node:vm'),path=require('node:path');
function load(name){const ctx={exports:{},require:n=>load(n.replace('./',''))};vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(__dirname,'../src/lib',name+'.ts'),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText,ctx);return ctx.exports}
const {visibleMilestones}=load('ownerMilestones');
for(const [progress,expected] of [[0,[7,30]],[7,[7,30]],[30,[30,60]],[60,[60,90]],[90,[90,180]],[180,[90,180]],[500,[90,180]]]){
 const tasks=['login','questions','tokens'].flatMap(metric=>[7,30,60,90,180].map(target=>({metric,period:'all',target,progress,awarded:false})));
 const before=JSON.stringify(tasks),actual=visibleMilestones(tasks);assert.equal(actual.length,6);
 assert.equal(JSON.stringify(actual.map(t=>t.target)),JSON.stringify([...expected,...expected,...expected]));assert.equal(JSON.stringify(tasks),before);
}
console.log('PASS milestone pairs: initial, first, middle, skipped stages, all completed; source unchanged');
