// Validate the delivered offline report itself, including every interactive control.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {createRequire} from 'node:module';
import {createHash} from 'node:crypto';
const require=createRequire(import.meta.url);
const {chromium}=require(process.env.CM_PLAYWRIGHT_MODULE);
const filename=process.env.CM_DATABASE_REPORT;
assert.ok(filename,'CM_DATABASE_REPORT must identify the generated HTML');
const browser=await chromium.launch({headless:true,executablePath:process.env.CM_CHROME_PATH,args:['--no-sandbox']});
try {
 const page=await browser.newPage({viewport:{width:1440,height:1000},acceptDownloads:true});
 const errors=[],external=[];page.on('pageerror',error=>errors.push(error.message));page.on('request',request=>{if(/^https?:/.test(request.url()))external.push(request.url())});
 await page.goto(pathToFileURL(filename).href);
 const data=await page.locator('#report-data').evaluate(node=>JSON.parse(node.textContent));
 assert.equal(await page.locator('.verdict').textContent(),data.accepted?'验收通过':'存在未通过或未覆盖项目');
 assert.deepEqual(await page.locator('dl dd').allTextContents(),['PASS','FAIL','SKIP','INCOMPLETE'].map(status=>String(data.counts[status]||0)));
 assert.ok(data.cases.length>0,'empty report');
 assert.equal(await page.locator('#cases tr').count(),Math.min(100,data.cases.length));
 for(let index=0;index<Math.ceil(data.cases.length/100);index++){
  assert.deepEqual(await page.locator('#cases summary').allTextContents(),data.cases.slice(index*100,index*100+100).map(c=>c.name));
  if(index+1<Math.ceil(data.cases.length/100))await page.locator('#next').click();
 }
 while(!await page.locator('#prev').isDisabled())await page.locator('#prev').click();
 const category=[...new Set(data.cases.map(c=>c.category))].sort()[0];
 await page.locator('#category').selectOption(category);
 assert.equal(await page.locator('#cases tr').count(),Math.min(100,data.cases.filter(c=>c.category===category).length));
 await page.locator('#category').selectOption('');
 await page.locator('#status').selectOption(data.cases[0].status);
 assert.equal(await page.locator('#cases tr').count(),Math.min(100,data.cases.filter(c=>c.status===data.cases[0].status).length));
 await page.locator('#status').selectOption('');
 const chosen=data.cases.find(c=>c.category===category),term=chosen.name.slice(0,12);
 await page.locator('#category').selectOption(category);await page.locator('#status').selectOption(chosen.status);await page.locator('#search').fill(term);
 const combined=data.cases.filter(c=>c.category===category&&c.status===chosen.status&&(c.name+' '+c.suite).toLowerCase().includes(term.toLowerCase()));
 await page.waitForFunction(expected=>document.querySelector('#cases').children.length===Math.min(100,expected),combined.length);
 assert.equal(await page.locator('#cases tr').count(),Math.min(100,combined.length));
 await page.locator('#category').selectOption('');await page.locator('#status').selectOption('');
 await page.locator('#search').fill('no-match-unique-report-browser-check-8e934b');
 await page.waitForFunction(()=>document.querySelector('#cases').children.length===0);
 assert.equal(await page.locator('#page').textContent(),'第 0 / 0 页');
 await page.locator('#search').fill('');
 await page.waitForFunction(()=>document.querySelector('#cases').children.length>0);
 await page.locator('#cases summary').first().click();
 assert.match(await page.locator('#cases details[open] pre').first().textContent(),/命令：.*\n证据：/s);
 const downloadPromise=page.waitForEvent('download');await page.locator('#download').click();
 const download=await downloadPromise;const downloaded=JSON.parse(await fs.readFile(await download.path(),'utf8'));
 assert.deepEqual(downloaded,data);
 if(data.accepted){
  assert.match(data.evidence_bundle,/^[A-Za-z0-9_.-]+\.zip$/);
  const raw=await fs.readFile(path.join(path.dirname(filename),data.evidence_bundle));
  assert.equal(createHash('sha256').update(raw).digest('hex'),data.evidence_bundle_sha256);
  assert.equal(await page.getByRole('link',{name:'下载完整原始证据 ZIP'}).getAttribute('href'),data.evidence_bundle);
 }
 const screenshots=process.env.REPORT_SCREENSHOT_DIR;
 if(screenshots){await fs.mkdir(screenshots,{recursive:true});await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:path.join(screenshots,'database-report-desktop.png')});}
 await page.setViewportSize({width:390,height:844});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'report overflows mobile viewport');
 if(screenshots)await page.screenshot({path:path.join(screenshots,'database-report-mobile.png')});
 assert.deepEqual(errors,[]);
 assert.deepEqual(external,[]);
 console.log(JSON.stringify({offlineReport:true,records:data.cases.length,allPagesReachable:true,verdictAndCountsExact:true,combinedFilters:true,searchEmptyState:true,logDetails:true,jsonDownloadExact:true,evidenceZIPVerified:data.accepted,mobileWidth:390,externalRequests:external,pageErrors:errors}));
}finally{await browser.close();}
