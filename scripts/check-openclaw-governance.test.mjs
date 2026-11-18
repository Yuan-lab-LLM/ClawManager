import test from 'node:test';
import assert from 'node:assert/strict';
import {inspectGatewayProtocol} from './check-openclaw-governance.mjs';
const config={baseURL:'http://gateway.invalid/v1',apiKey:'private-test-key',generation:3};
const contract=()=>({protocol:1,generation:3,enrolled:false,enrollment_generation:null,registration_is_sticky:true,required_capabilities:['task_identity','turn_cancel','cancel_receipt','live_catalog']});
test('preflight is GET only and never claims native acceptance',async()=>{
 let calls=0;
 const result=await inspectGatewayProtocol({...config,fetcher:async(url,options)=>{calls++;assert.equal(url,config.baseURL+'/runtime/capabilities');assert.equal(options.method,'GET');assert.equal(options.redirect,'error');assert.equal(options.body,undefined);assert.equal(options.headers.Authorization,'Bearer '+config.apiKey);return Response.json({...contract(),private_detail:'must not escape'});}});
 assert.equal(calls,1);assert.equal(result.activation_ready,false);assert.equal(result.status,'preflight_passed');assert.equal(result.enrolled,false);assert.ok(!JSON.stringify(result).includes('private'));
});
test('old enrollment is reported without being upgraded or cleared',async()=>{
 const result=await inspectGatewayProtocol({...config,fetcher:async()=>Response.json({...contract(),enrolled:true,enrollment_generation:2})});
 assert.equal(result.enrolled,true);assert.equal(result.requires_renegotiation,true);assert.equal(result.enrollment_generation,2);
});
for(const [status,code] of [[404,'SERVER_PREFLIGHT_UNSUPPORTED'],[405,'SERVER_PREFLIGHT_UNSUPPORTED'],[403,'INSTANCE_AUTHORIZATION_DENIED'],[500,'SERVER_PREFLIGHT_UNAVAILABLE']])test('HTTP '+status+' never enrolls or falls back',async()=>{
 await assert.rejects(inspectGatewayProtocol({...config,fetcher:async()=>new Response('secret upstream message',{status})}),e=>e.code===code&&!e.message.includes('secret'));
});
test('generation, protocol and capability contract mismatches fail closed',async()=>{
 for(const change of [{generation:4},{protocol:2},{required_capabilities:[]},{required_capabilities:['task_identity','turn_cancel','turn_cancel','live_catalog']},{registration_is_sticky:false},{enrolled:true,enrollment_generation:null}]){
  await assert.rejects(inspectGatewayProtocol({...config,fetcher:async()=>Response.json({...contract(),...change})}),e=>['INVALID_SERVER_CONTRACT','RUNTIME_GENERATION_MISMATCH'].includes(e.code));
 }
});
test('invalid and oversized response bodies are rejected',async()=>{
 for(const body of ['not JSON','x'.repeat(4097)])await assert.rejects(inspectGatewayProtocol({...config,fetcher:async()=>new Response(body)}),e=>e.code==='INVALID_SERVER_CONTRACT');
});
test('invalid configuration makes no network request',async()=>{
 for(const change of [{baseURL:'file:///tmp/key'},{baseURL:'https://user:password@example.com'},{generation:-1},{apiKey:'x\ny'},{timeoutMs:0}]){
  await assert.rejects(inspectGatewayProtocol({...config,...change,fetcher:()=>assert.fail('network')}),e=>e.code==='INVALID_CONFIGURATION');
 }
});
test('transport failures are sanitized',async()=>{
 await assert.rejects(inspectGatewayProtocol({...config,fetcher:async()=>{throw new Error(config.apiKey);}}),e=>e.code==='PREFLIGHT_TRANSPORT_ERROR'&&!e.message.includes(config.apiKey));
});

test('renewal metadata is optional for old servers and never enables activation',async()=>{
 const legacy=await inspectGatewayProtocol({...config,fetcher:async()=>Response.json(contract())});
 assert.equal(legacy.renewal_supported,false);assert.equal(legacy.report_ttl_seconds,null);
 const ready=await inspectGatewayProtocol({...config,fetcher:async(_url,options)=>{
  assert.equal(options.method,'GET');return Response.json({...contract(),renewal_supported:true,report_ttl_seconds:30});
 }});
 assert.equal(ready.renewal_supported,true);assert.equal(ready.report_ttl_seconds,30);assert.equal(ready.activation_ready,false);
 for(const change of [{renewal_supported:'true'},{renewal_supported:true},{renewal_supported:true,report_ttl_seconds:0},{renewal_supported:true,report_ttl_seconds:3601}]){
  await assert.rejects(inspectGatewayProtocol({...config,fetcher:async()=>Response.json({...contract(),...change})}),{code:'INVALID_SERVER_CONTRACT'});
 }
});

test('conditional enrollment precondition is exposed read-only and optional for old servers',async()=>{
 const inspect=async(change)=>inspectGatewayProtocol({...config,fetcher:async(_url,options)=>{
  assert.equal(options.method,'GET');assert.equal(options.body,undefined);return Response.json({...contract(),...change});
 }});
 const legacy=await inspect({});assert.equal(legacy.conditional_enrollment_supported,false);assert.equal(legacy.enrollment_revision,null);
 const first=await inspect({conditional_enrollment_supported:true,enrollment_revision:null});
 assert.equal(first.conditional_enrollment_supported,true);assert.equal(first.enrollment_revision,null);assert.equal(first.activation_ready,false);
 const revision='a'.repeat(64);
 const enrolled=await inspect({enrolled:true,enrollment_generation:2,conditional_enrollment_supported:true,enrollment_revision:revision});
 assert.equal(enrolled.enrollment_revision,revision);assert.equal(enrolled.requires_renegotiation,true);assert.equal(enrolled.activation_ready,false);
 const blocked=await inspect({enrolled:true,enrollment_generation:2,conditional_enrollment_supported:false,enrollment_revision:null});
 assert.equal(blocked.conditional_enrollment_supported,false);assert.equal(blocked.enrollment_revision,null);
});

test('conditional enrollment metadata cannot invent a precondition',async()=>{
 for(const change of [
  {conditional_enrollment_supported:'true'}, {conditional_enrollment_supported:true},
  {conditional_enrollment_supported:true,enrollment_revision:'a'.repeat(64)},
  ...[null,'','A'.repeat(64),'g'.repeat(64),'a'.repeat(65),123].map(enrollment_revision=>({enrolled:true,enrollment_generation:2,conditional_enrollment_supported:true,enrollment_revision})),
 ])await assert.rejects(inspectGatewayProtocol({...config,fetcher:async()=>Response.json({...contract(),...change})}),{code:'INVALID_SERVER_CONTRACT'});
});
