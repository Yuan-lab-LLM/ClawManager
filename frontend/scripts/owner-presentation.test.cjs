const assert=require('node:assert/strict');
const fs=require('node:fs');const vm=require('node:vm');const ts=require('typescript');
const source=fs.readFileSync(require('node:path').join(__dirname,'../src/lib/ownerPresentation.ts'),'utf8');
const out=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS}}).outputText;
const context={exports:{}};vm.runInNewContext(out,context);
for(const [owner,expected] of [['zhangsan123@example.com','zhangsan'],['accept-0915@example.com','accept'],['li2si3@example.com','lisi'],['abc１２@example.com','abc'],['123@example.com','朋友'],['@example.com','朋友'],['a.b@example.com','a.b']]) {
 assert.equal(context.exports.ownerWelcomeName(owner),expected);
}
console.log('PASS presentation removes digits without mutating owner identity; empty-name fallback');
