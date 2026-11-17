// Production-page cost controls against isolated, explicitly mocked APIs.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url);
const {chromium}=require(process.env.CM_PLAYWRIGHT_MODULE||'../../e2e/node_modules/playwright');
const dist=path.resolve(import.meta.dirname,'../dist');
const server=http.createServer((request,response)=>{let file=path.resolve(dist,'.'+new URL(request.url,'http://local').pathname);if(!file.startsWith(dist+path.sep)||!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');response.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(response);});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({headless:true,...(process.env.CM_CHROME_PATH?{executablePath:process.env.CM_CHROME_PATH}:{channel:'chrome'}),args:['--no-sandbox']});
try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}});const requests=[],errors=[];let failOnce=true;page.on('pageerror',e=>errors.push(e.message));
 await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');localStorage.setItem('clawmanager_locale','en');window.WebSocket=class{static OPEN=1;readyState=1;close(){}send(){}}});
 await page.route('**/api/v1/**',async route=>{const url=new URL(route.request().url());let data={};
  if(url.pathname.endsWith('/auth/me'))data={id:1,username:'admin',role:'admin'};
  else if(url.pathname.endsWith('/admin/costs')){const query=Object.fromEntries(url.searchParams);requests.push(query);if(query.cursor&&failOnce){failOnce=false;await route.fulfill({status:503,json:{error:'Fixture cost retry'}});return;}data={total_prompt_tokens:100,total_completion_tokens:50,total_tokens:150,total_estimated_cost:12.34,total_internal_cost:1,currency:'USD',user_summary:[],instance_summary:[],top_models:[],top_users:[],daily_trend:[],model_trends:[],user_trends:[],recent_records:[],total_recent_records:25,page:Number(query.page||1),limit:20,next_cursor:query.cursor?'':'cost-snapshot'};}
  await route.fulfill({json:{success:true,data}});
 });
 await page.goto(`http://127.0.0.1:${server.address().port}/admin/costs`);
 await page.getByText('12.34000000 USD',{exact:true}).waitFor();assert.equal(Date.parse(requests[0].until)-Date.parse(requests[0].since),30*86400000);
 await page.locator('input[type=text]').fill('draft-not-applied');await page.getByRole('button',{name:'Next',exact:true}).click();await page.waitForTimeout(200);assert.equal(requests.at(-1).cursor,'cost-snapshot');assert.equal(requests.at(-1).search,undefined);assert.equal(requests.at(-1).until,requests[0].until);
 await page.getByText('Fixture cost retry',{exact:true}).waitFor();await page.getByRole('button',{name:'Next',exact:true}).click();await page.waitForTimeout(200);assert.equal(requests.at(-1).cursor,'cost-snapshot');assert.equal(await page.getByRole('button',{name:'Next',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'Prev',exact:true}).click();await page.waitForTimeout(200);assert.equal(requests.at(-1).cursor,undefined);
 await page.locator('input[type=datetime-local]').nth(0).fill('2025-01-01T00:00');await page.locator('input[type=datetime-local]').nth(1).fill('2025-01-31T00:00');await page.getByRole('button',{name:'Apply',exact:true}).first().click();await page.waitForTimeout(200);assert.equal(requests.at(-1).since,'2025-01-01T00:00:00Z');assert.equal(requests.at(-1).search,'draft-not-applied');assert.equal(requests.at(-1).cursor,undefined);
 await page.getByRole('heading',{name:'2025-01-01 – 2025-01-31',exact:true}).waitFor();assert.equal(await page.getByText('Last 7 days',{exact:true}).count(),0,'chart mislabeled selected window');
 const before=requests.length;await page.locator('input[type=datetime-local]').nth(0).fill('2020-01-01T00:00');await page.getByRole('button',{name:'Apply',exact:true}).first().click();await page.getByText(/at most 90 days/).waitFor();assert.equal(requests.length,before);
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile overflow');await page.screenshot({path:'/tmp/cm-cost-window-mobile.png',fullPage:true});assert.deepEqual(errors,[]);
 console.log('PASS cost default30d, frozen next/previous cursor, failed page retry, unapplied drafts, historical UTC window, invalid90d rejection, mobile; mocked API declared');
}finally{await browser.close();await new Promise(resolve=>{server.close(resolve);server.closeAllConnections()})}
