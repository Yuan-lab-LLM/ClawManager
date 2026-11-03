const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),ts=require('typescript');
const source=fs.readFileSync(require('node:path').join(__dirname,'../src/lib/compactDisplayTime.ts'),'utf8');
const context={exports:{}};vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText,context);
assert.equal(context.exports.compactDisplayTime('2026-09-18T10:59:32'),'09/18 10:59');
assert.equal(context.exports.compactDisplayTime('2026-01-02T03:04:00'),'01/02 03:04');
for(const value of [undefined,'','not a date'])assert.equal(context.exports.compactDisplayTime(value),'—');
assert.match(context.exports.fullDisplayTime('2026-09-18T10:59:32'),/2026/);
console.log('PASS compact timestamp, invalid date fallback and full date retained');
