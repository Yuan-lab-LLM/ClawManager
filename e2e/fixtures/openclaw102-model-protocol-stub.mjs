// Protocol fixture, not a language model or semantic/memory acceptance.
import http from 'node:http';
let requests=0,toolNames=[],receipts=[];
const server=http.createServer((req,res)=>{
 if(req.method==='GET') {res.setHeader('content-type','application/json');return res.end(JSON.stringify(req.url==='/stats'?{requests,toolNames,receipts}:{object:'list',data:[{id:'local-smoke',object:'model'}]}));}
 let raw='';req.on('data',x=>raw+=x);req.on('end',()=>{
  requests++;
  const body=JSON.parse(raw),messages=body.messages||[],last=messages.at(-1);
  toolNames=(body.tools||[]).map(t=>t.function?.name).filter(Boolean);
  const completion=body.tools?.find(t=>t.function?.name==='team_complete_task');
  const priorCall=[...messages].reverse().find(m=>m.role==='assistant'&&m.tool_calls?.length)?.tool_calls.at(-1)?.function?.name;
  if(last?.role==='tool')receipts.push({tool:priorCall,result:String(last.content).slice(0,2500)});
  const hasReceipt=last?.role==='tool'&&(priorCall==='team_complete_task'||priorCall==='tool_call');
  const completionArgs={status:'succeeded',summary:'LOCAL_CORE_INBOUND_ACCEPTANCE',resultMarkdown:'LOCAL_CORE_INBOUND_ACCEPTANCE'};
  // 10.2 uses native on-demand Tool Search. Exercise it rather than disabling
  // discovery or assuming every callable plugin tool is an inline API schema.
  let name='team_complete_task',args=completionArgs;
  if(!hasReceipt&&!completion){
   if(priorCall==='tool_search'){name='tool_call';args={id:'team_complete_task',args:completionArgs};}
   else if(toolNames.includes('tool_search')){name='tool_search';args={query:'team_complete_task',limit:1};}
   else{res.writeHead(400,{'content-type':'application/json'});return res.end(JSON.stringify({error:{message:'Protocol fixture lacks completion or Tool Search surface',type:'fixture_contract'}}));}
  }
  const tool={index:0,id:'local-protocol-call-'+requests,type:'function',function:{name,arguments:JSON.stringify(args)}};
  const delta=hasReceipt?{role:'assistant',content:'LOCAL_CORE_INBOUND_ACCEPTANCE'}:{role:'assistant',tool_calls:[tool]};
  if(body.stream){
   res.writeHead(200,{'content-type':'text/event-stream','cache-control':'no-cache'});
   const send=(d,finish)=>res.write('data: '+JSON.stringify({id:'local-response-'+requests,object:'chat.completion.chunk',created:1700000000,model:body.model,choices:[{index:0,delta:d,finish_reason:finish}]})+'\n\n');
   send(delta,null);send({},hasReceipt?'stop':'tool_calls');res.end('data: [DONE]\n\n');
  }else{res.setHeader('content-type','application/json');res.end(JSON.stringify({id:'local-response-'+requests,object:'chat.completion',created:1700000000,model:body.model,choices:[{index:0,message:delta,finish_reason:hasReceipt?'stop':'tool_calls'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}));}
 });
});
server.listen(19333,'127.0.0.1',()=>console.log('LOCAL_PROTOCOL_STUB_READY'));
