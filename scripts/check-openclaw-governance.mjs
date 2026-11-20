// Read-only server protocol preflight. Never POST capability enrollment here:
// enrollment is sticky and requires separately accepted native entrypoints.
import {pathToFileURL} from 'node:url';
import path from 'node:path';

export class ProtocolPreflightError extends Error {
  constructor(code) {super(code);this.name='ProtocolPreflightError';this.code=code;}
}
const fail=code=>{throw new ProtocolPreflightError(code);};
const required=['task_identity','turn_cancel','cancel_receipt','live_catalog'];
const validGeneration=value=>Number.isSafeInteger(value)&&value>=0&&value<=2147483647;

export async function inspectGatewayProtocol({baseURL,apiKey,generation,fetcher=globalThis.fetch,timeoutMs=2500}={}) {
  let url;
  try {url=new URL(baseURL);}catch{fail('INVALID_CONFIGURATION');}
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password||url.search||url.hash||
    typeof apiKey!=='string'||!apiKey.trim()||/[\r\n]/.test(apiKey)||!validGeneration(generation)||
    !Number.isSafeInteger(timeoutMs)||timeoutMs<1)fail('INVALID_CONFIGURATION');
  const signal=AbortSignal.timeout(timeoutMs);let reader;
  try {
    const response=await fetcher(url.href.replace(/\/+$/,'')+'/runtime/capabilities',{
      method:'GET',redirect:'error',signal,headers:{Authorization:`Bearer ${apiKey}`,Accept:'application/json'},
    });
    if(response.status===404||response.status===405)fail('SERVER_PREFLIGHT_UNSUPPORTED');
    if(response.status===401||response.status===403)fail('INSTANCE_AUTHORIZATION_DENIED');
    if(!response.ok||!response.body)fail('SERVER_PREFLIGHT_UNAVAILABLE');
    reader=response.body.getReader();const chunks=[];let size=0;
    for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>4096)fail('INVALID_SERVER_CONTRACT');chunks.push(Buffer.from(value));}
    signal.throwIfAborted();
    let value;try{value=JSON.parse(Buffer.concat(chunks).toString('utf8'));}catch{fail('INVALID_SERVER_CONTRACT');}
    if(!value||value.protocol!==1||!validGeneration(value.generation)||typeof value.enrolled!=='boolean'||value.registration_is_sticky!==true||
      !Array.isArray(value.required_capabilities)||value.required_capabilities.length!==required.length||
      new Set(value.required_capabilities).size!==required.length||!required.every(c=>value.required_capabilities.includes(c))||
      (value.enrolled?!validGeneration(value.enrollment_generation):value.enrollment_generation!==null))fail('INVALID_SERVER_CONTRACT');
    if(value.generation!==generation)fail('RUNTIME_GENERATION_MISMATCH');
    // Old servers omit renewal metadata. Do not infer renewal support from a
    // successful enrollment/preflight, or silently POST to the sticky endpoint.
    if(value.renewal_supported!==undefined&&typeof value.renewal_supported!=='boolean'||
      value.renewal_supported===true&&(!Number.isSafeInteger(value.report_ttl_seconds)||value.report_ttl_seconds<1||value.report_ttl_seconds>3600))fail('INVALID_SERVER_CONTRACT');
    // A contract fingerprint is a write precondition, never an authorization
    // token or native release acceptance. Old servers may omit both fields.
    if(value.conditional_enrollment_supported!==undefined&&typeof value.conditional_enrollment_supported!=='boolean'||
      value.conditional_enrollment_supported===true&&(value.enrolled?
        typeof value.enrollment_revision!=='string'||!/^[a-f0-9]{64}$/.test(value.enrollment_revision):value.enrollment_revision!==null))fail('INVALID_SERVER_CONTRACT');
    return Object.freeze({status:'preflight_passed',protocol:1,generation,enrolled:value.enrolled,
      enrollment_generation:value.enrollment_generation,requires_renegotiation:value.enrolled&&value.enrollment_generation!==generation,
      renewal_supported:value.renewal_supported===true,report_ttl_seconds:value.renewal_supported===true?value.report_ttl_seconds:null,
      conditional_enrollment_supported:value.conditional_enrollment_supported===true,
      enrollment_revision:value.conditional_enrollment_supported===true?value.enrollment_revision:null,
      activation_ready:false,activation_blocker:'native_runtime_release_acceptance_required'});
  } catch(error) {
    if(error instanceof ProtocolPreflightError)throw error;
    fail('PREFLIGHT_TRANSPORT_ERROR');
  } finally {void reader?.cancel().catch(()=>{});}
}

if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
  try{
    const raw=process.env.CLAWMANAGER_GATEWAY_GENERATION;
    if(!/^(0|[1-9][0-9]*)$/.test(raw??''))fail('INVALID_CONFIGURATION');
    const result=await inspectGatewayProtocol({baseURL:process.env.CLAWMANAGER_LLM_BASE_URL,apiKey:process.env.CLAWMANAGER_LLM_API_KEY,generation:Number(raw)});
    console.log(JSON.stringify(result,null,2));
  }catch(error){console.error(error instanceof ProtocolPreflightError?error.code:'PREFLIGHT_FAILED');process.exitCode=1;}
}
