// Production settings bundle with read-only fixtures; never a server upgrade.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {chromium} from 'playwright';
const dist=path.resolve(import.meta.dirname,'../frontend/dist');
const server=http.createServer((req,res)=>{
 let file=path.join(dist,new URL(req.url,'http://local').pathname);
 if(!file.startsWith(dist+path.sep)){res.writeHead(403).end();return;}
 if(!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.svg':'image/svg+xml'})[path.extname(file)]||'application/octet-stream');
 fs.createReadStream(file).pipe(res);
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'chrome',headless:true});
try {
 const page=await browser.newPage({viewport:{width:1440,height:1000}});
 const content=page.getByRole('main');
 const errors=[];page.on('pageerror',err=>errors.push(String(err)));
 await page.addInitScript(()=>{localStorage.setItem('access_token','local-fixture-only');localStorage.setItem('clawmanager_locale','zh');});
 let legacy=false,partial=false;
 const rollout={id:136,runtime_type:'openclaw',status:'running',phase:'compatibility_check',target_image_ref:'fixture',batch_size:8};
 const details=()=>partial?({rollout:{...rollout,status:'finished',phase:'postflight'},execution:{version:2,check:4,migrate:2,start:2,restore:2},audits:[],items:[{id:1,instance_id:482,state:'prepared',isolated_at:new Date().toISOString(),recovery_status:'needs_attention',error_message:'custom plugin retained'},{id:2,instance_id:484,state:'verified',recovery_status:'verified'}]}):({rollout,execution:legacy?undefined:{version:1,check:4,migrate:2,start:2,restore:2},audits:[],items:[
  {instance_id:481,state:'prepared',operation_phase:'compatibility_check',operation_started_at:new Date().toISOString(),operation_deadline_at:new Date(Date.now()+60000).toISOString(),operation_attempts:1},
  {instance_id:482,state:'prepared'},
  {instance_id:484,state:'compatibility_checked'},
  {instance_id:518,state:'restart_ready',recovery_status:'needs_attention',error_message:'fixture source gateway unavailable'},
 ]});
 await page.route('**/api/v1/**',route=>{
  const url=new URL(route.request().url());
  const send=data=>route.fulfill({json:{data}});
  if(url.pathname.endsWith('/auth/me'))return send({id:1,username:'admin',role:'admin',status:'active'});
  if(url.pathname.endsWith('/version'))return send({version:'local-upgrade-ui'});
  if(url.pathname.endsWith('/runtime-rollouts/136'))return send(details());
  if(url.pathname.endsWith('/instances/482/upgrade-recovery'))return send({instance_id:482,generation:8,parent_rollout_id:136,target_image_ref:'fixture',can_retry:true,can_rebuild:true,isolated:true,reason:'data retained',current:details()});
  if(url.pathname.endsWith('/runtime-rollouts'))return send({items:[rollout]});
  if(url.pathname.endsWith('/runtime-pods'))return send({pods:[]});
  if(url.pathname.endsWith('/images'))return send({items:[]});
  return send({enabled:false,ldap:{},status:{enabled:false,configured:false,checks:{}}});
 });
 await page.goto(`http://127.0.0.1:${server.address().port}/admin/settings`);
 await content.getByText('并发上限：检查 4 · 迁移 2 · 启动 2 · 回退 2',{exact:false}).waitFor({timeout:20000});
 assert.match(await page.locator('body').innerText(),/当前处理中 1 个；待检查 1 个；检查通过 1 个/);
 await content.getByText('恢复受阻，需要处理（升级隔离仍保留）',{exact:false}).waitFor();
 await content.getByText('fixture source gateway unavailable',{exact:false}).waitFor();
 legacy=true;
 await page.reload();
 await content.getByText('升级 #136',{exact:false}).waitFor();
 assert.equal(await content.getByText('并发上限：',{exact:false}).count(),0,'old API unexpectedly requires execution metadata');
 legacy=false;partial=true;rollout.status='finished';
 await page.reload();
 await content.locator('[data-upgrade-partial]').waitFor();
 assert.match(await content.locator('[data-upgrade-partial]').innerText(),/已隔离 1 个实例.*其余实例已完成升级/);
 await content.getByRole('button',{name:'保留数据重建升级',exact:true}).waitFor();
 assert.deepEqual(errors,[]);
 console.log('PASS: production settings progress, active/queued counts, recovery error and old API rendering');
}finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
