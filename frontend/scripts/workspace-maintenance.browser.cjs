const {chromium}=require('playwright'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',headless:true});
 const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:900}});const page=await context.newPage();
 page.setDefaultTimeout(12000);
 let content='name: 中文\n',version='v1',saveCount=0,deny=false;
 let name='config.yaml',instanceType='hermes',runtimeType='gateway',lastListPath;
 const failures=[];page.on('pageerror',e=>failures.push(e.message));
 await page.route('**/api/v1/ieisystem/**',async route=>{
  const req=route.request(),p=new URL(req.url()).pathname;let data={};
  if(p.endsWith('/instances/1'))data={instance:{id:1,name:'异常实例测试',type:instanceType,runtime_type:runtimeType,status:'error',owner:'test@example.com',instance_mode:'lite',workspace_available:true,workspace_root:'Workspace'}};
  else if(p.endsWith('/access'))return route.fulfill({status:409,json:{error:'Instance is not running'}});
  else if(p.endsWith('/workspace/files')){lastListPath=new URL(req.url()).searchParams.get('path');data={entries:[{name,path:name,is_dir:false,size:50,previewable:true,downloadable:true,modified_at:'2026-09-18T08:00:00Z'},{name:'state.db',path:'state.db',size:22,is_dir:false,downloadable:true,modified_at:'2026-09-18T08:00:00Z'}]};}
  else if(p.endsWith('/workspace/text')){
   if(req.method()==='PUT'){saveCount++;const body=req.postDataJSON();if(deny)return route.fulfill({status:409,json:{error:'文件已被修改，请重新载入后再保存；当前编辑内容已保留。'}});assert.equal(body.version,version);content=body.text;version='v'+(saveCount+1)}
   data={text:content,version};
  }else if(p.endsWith('/workspace/entries')&&req.method()==='PATCH')name=req.postDataJSON().new_path;
  await route.fulfill({json:{data}});
 });
 await page.goto((process.env.OWNER_PREVIEW_URL||'http://127.0.0.1:38463')+'/ieisystem/instances/1');
 await page.getByText('实例运行异常，暂时无法连接运行界面。',{exact:true}).waitFor();
 await page.getByRole('button',{name:'编辑 config.yaml',exact:true}).click();
 const editor=page.locator('.cm-content');await editor.waitFor();await editor.fill('name: 测试\n');
 await page.getByRole('button',{name:'保存',exact:true}).click();await page.getByText('已保存。不会自动重启实例或执行脚本。').waitFor();
 assert.equal(content,'name: 测试\n');
 await editor.fill('name: [\n');await page.getByRole('button',{name:'保存',exact:true}).click();await page.getByRole('alert').filter({hasText:'语法检查未通过'}).waitFor();assert.equal(saveCount,1);
 await editor.fill('name: 冲突\n');deny=true;await page.keyboard.press('Control+s');await page.getByRole('alert').filter({hasText:'文件已被修改'}).waitFor();assert.match(await editor.innerText(),/冲突/);
 deny=false;await page.getByRole('button',{name:'保存',exact:true}).click();await page.getByText('已保存。不会自动重启实例或执行脚本。').waitFor();
 page.once('dialog',d=>d.accept('renamed.yaml'));await page.getByRole('button',{name:'重命名',exact:true}).click();await page.getByText('renamed.yaml',{exact:true}).first().waitFor();
 await page.screenshot({path:'.tmp/workspace-maintenance-editor.png',fullPage:true});
 await page.getByRole('button',{name:'关闭编辑器'}).click();await page.getByRole('button',{name:'编辑 renamed.yaml',exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'重命名 state.db',exact:true}).count(),1);
 await page.getByRole('button',{name:'重新连接',exact:true}).click();await page.getByRole('button',{name:'编辑 renamed.yaml',exact:true}).waitFor();
 await page.screenshot({path:'.tmp/workspace-maintenance-error.png',fullPage:true});
 instanceType='workbuddy';await page.reload();await page.getByRole('button',{name:'重命名 renamed.yaml',exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'编辑 renamed.yaml',exact:true}).count(),0);assert.equal(lastListPath,''); // API paths are relative to the Runtime-selected root.
 assert.deepEqual(failures,[]);
 await browser.close();console.log('PASS abnormal runtime + independent workspace, editing, validation, Ctrl+S, conflict preservation, rename, differentiated actions, retry');
})().catch(e=>{console.error(e);process.exit(1)});
