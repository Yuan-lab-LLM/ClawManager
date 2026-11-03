// Production UI with local HTTP fixtures. No server mutation/model invocation.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {chromium} from 'playwright';
const dist=path.resolve(import.meta.dirname,'../frontend/dist');
const server=http.createServer((req,res)=>{
 let file=path.join(dist,new URL(req.url,'http://local').pathname);
 if(!file.startsWith(dist+path.sep)){res.writeHead(403).end();return}
 if(!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');
 res.setHeader('content-type',({'.js':'text/javascript','.css':'text/css','.html':'text/html'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',headless:true});
try{
 const page=await browser.newPage({viewport:{width:1440,height:1100}}),errors=[],writes=[];
 page.on('pageerror',e=>errors.push(String(e)));
 await page.addInitScript(()=>{localStorage.setItem('access_token','local-fixture');localStorage.setItem('clawmanager_locale','zh')});
 let r={id:136,runtime_type:'openclaw',status:'error',phase:'gateway_restart',target_image_ref:'fixture@sha256:repair',batch_size:2,error_message:'fixture failed upgrade'};
 let rebuild=false;
 const details=()=>({rollout:r,items:[{id:1,instance_id:481,state:'restart_ready',recovery_status:'needs_attention',error_message:'fixture instance needs repair'}],audits:[]});
 await page.route('**/api/v1/**',async route=>{
  const req=route.request(),url=new URL(req.url()),send=data=>route.fulfill({json:{data}});
  if(req.method()==='POST'){
   writes.push({url:url.pathname,body:req.postDataJSON()});
   if(url.pathname.endsWith('/runtime-rollouts/preflight'))return send({passed:true,strategy:'openclaw_8plus_data_safe',target_image_ref:'fixture@sha256:repair',instance_count:2,team_count:0,openclaw_team_member_count:0,hermes_team_member_count:0,required_capabilities:[],blockers:[],warnings:[],excluded_instance_ids:[481],isolated:[{instance_id:481,reason:'previous recovery failed; original data retained'}],rollout:{...r,id:142,status:'preflight_passed',phase:'preflight',preflight_id:'healthy-plan'}});
   assert.match(url.pathname,/\/instances\/481\/upgrade-recovery$/,'upgrade repair called reset/delete API');
   rebuild=req.postDataJSON().mode==='rebuild';r={...r,id:rebuild?139:138,status:'pending',phase:'maintenance'};
   return send({rollout:r});
  }
  if(url.pathname.endsWith('/auth/me'))return send({id:1,username:'admin',role:'admin',status:'active'});
  if(url.pathname.endsWith('/version'))return send({version:'local-recovery-acceptance'});
  if(url.pathname.endsWith('/instances/481/upgrade-recovery'))return send({instance_id:481,generation:8,parent_rollout_id:r.id,target_image_ref:r.target_image_ref,can_retry:r.status==='error',can_rebuild:rebuild&&r.status==='error',isolated:r.status==='error',reason:'文件、配置、历史和 Team 关联保留，不是实例重置。',current:details()});
  if(/\/runtime-rollouts\/\d+$/.test(url.pathname))return send(details());
  if(url.pathname.endsWith('/runtime-rollouts'))return send({items:[r]});
  if(url.pathname.endsWith('/runtime-pods'))return send({pods:[]});
  if(url.pathname.endsWith('/images'))return send({items:[]});
  return send({enabled:false,ldap:{},status:{enabled:false,configured:false,checks:{}}});
 });
 const visit=()=>page.goto(`http://127.0.0.1:${server.address().port}/admin/settings`);
 await visit();
 const panel=page.locator('[data-upgrade-recovery="481"]');
 await panel.getByRole('button',{name:'重试升级',exact:true}).click();
 await panel.getByText('升级恢复处理中',{exact:true}).waitFor();
 assert.equal(writes.length,1);assert.equal(writes[0].body.mode,'retry');
 assert.equal(await panel.getByRole('button',{name:'重试升级',exact:true}).count(),0);
 r={...r,status:'error'};rebuild=true;await page.reload();
 page.once('dialog',dialog=>{assert.match(dialog.message(),/不是.*实例重置/);dialog.dismiss()});
 await panel.getByRole('button',{name:'保留数据重建升级',exact:true}).click();
 assert.equal(writes.length,1,'cancelled rebuild still submitted');
 page.once('dialog',dialog=>dialog.accept());
 await panel.getByRole('button',{name:'保留数据重建升级',exact:true}).click();
 await panel.getByText('升级恢复处理中',{exact:true}).waitFor();
 assert.equal(writes.length,2);assert.equal(writes[1].body.mode,'rebuild');assert.equal(writes[1].body.confirm_preserve,true);
 r={...r,status:'error',phase:'compatibility_check'};await page.reload();
 await page.locator('[data-upgrade-target-image]:visible').fill('fixture@sha256:repair');
 await panel.getByRole('button',{name:'升级其他实例',exact:true}).click();
 await page.locator('[data-upgrade-isolation]:visible').getByText('其余 2 个实例预检通过，可确认升级；隔离实例修复后再重新加入。',{exact:true}).waitFor();
 assert.equal(writes.length,3);assert.match(writes[2].url,/runtime-rollouts\/preflight$/);
 assert.equal(writes.filter(w=>/reset|delete/.test(w.url)).length,0);
 assert.deepEqual(errors,[]);
 console.log('PASS production recovery UI: retry, lockout, preservation confirmation/cancel, persistent isolation and healthy-fleet preview; zero factory reset/delete calls.');
}finally{await browser.close();await new Promise(r=>server.close(r))}
