const assert=require('node:assert/strict');const fs=require('node:fs');const path=require('node:path');const vm=require('node:vm');const ts=require('typescript');
const cache=new Map();
function load(file){file=path.resolve(file);if(cache.has(file))return cache.get(file);const exports={};cache.set(file,exports);const source=fs.readFileSync(file,'utf8');const compiled=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;vm.runInNewContext(compiled,{exports,require:id=>load(path.resolve(path.dirname(file),id+'.ts'))});return exports;}
const {ownerAgentState,ownerMemberAgentState}=load(path.join(__dirname,'../src/lib/ownerAgentState.ts'));
for(const [status,reason,expected] of [['running',undefined,'running'],['stopped','idle_timeout','sleeping'],['stopped','manual','stopped'],['stopped',undefined,'stopped'],['error',undefined,'error'],['creating',undefined,'starting'],['offline',undefined,'offline'],['future-health',undefined,'unknown']])assert.equal(ownerAgentState(status,reason),expected);
const member={status:'idle',availability:'idle'};
assert.equal(ownerAgentState(ownerMemberAgentState(member)),'running');
for(const state of ['offline','unknown','starting','online'])assert.equal(ownerAgentState(ownerMemberAgentState({...member,runtime_observation:{state}})),state==='online'?'running':state);
assert.equal(ownerAgentState(ownerMemberAgentState({...member,runtime_status:'stopped',runtime_observation:{state:'offline'}})),'stopped');
assert.equal(ownerAgentState(ownerMemberAgentState({...member,runtime_status:'starting',blocked_reason:'Runtime unavailable: linked instance is starting'})),'starting');
assert.equal(ownerAgentState(ownerMemberAgentState({...member,runtime_status:'starting',blocked_reason:'Missing required source'})),'error');
assert.equal(ownerAgentState(ownerMemberAgentState({...member,status:'failed'})),'error');
console.log('PASS: manual stops and sleep stay distinct; failed/unknown observations cannot become online icons; startup and faults retain their provenance');
