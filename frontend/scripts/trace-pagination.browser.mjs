// Production-page regression with isolated API fixtures. Build on the test cluster.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.CM_PLAYWRIGHT_MODULE || '../../e2e/node_modules/playwright');
const dist = path.resolve(process.env.CM_TRACE_DIST ?? path.join(import.meta.dirname, '../dist'));
const server = http.createServer((request, response) => {
 let file=path.resolve(dist,'.'+new URL(request.url,'http://local').pathname);
 if(!file.startsWith(dist+path.sep)||!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(dist,'index.html');
 response.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(response);
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({headless:true,...(process.env.CM_CHROME_PATH?{executablePath:process.env.CM_CHROME_PATH}:{channel:'chrome'}),args:['--no-sandbox']});
try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}});page.on("pageerror",e=>console.log("PAGEERROR",e.message));page.on("console",m=>{if(m.type()==="error")console.log("CONSOLE",m.text())});
 const bodyReads = [];
 const flowReads = [];
 const cursors = [];
 let failNext = false;
 let failBody = true;
 await page.addInitScript(()=>{localStorage.setItem('access_token','fixture');if(!localStorage.getItem('clawmanager_locale'))localStorage.setItem('clawmanager_locale','en');window.WebSocket=class{static OPEN=1;readyState=1;close(){}send(){}}});
 await page.route('**/api/v1/**',async route=>{
 const url=new URL(route.request().url());let data={};
 if(url.pathname.endsWith('/auth/me'))data={id:1,username:'admin',role:'admin'};
 else if(url.pathname.endsWith('/admin/ai-audit/paged')){
 if(url.searchParams.has('flow_ids')) {
   const ids = url.searchParams.get('flow_ids').split(',').map(Number);
   flowReads.push([ids, url.searchParams.get('flow_previous_id')]);
   const id = ids[0];
   const created_at = '2026-11-17T12:00:00Z';
   const node = (kind, title, content) => ({ id: `${kind}-${id}`, invocation_id: id, kind, title, output_payload: content, created_at });
   data = { processed: 1, incomplete: false, flow_nodes: [
     ...(id === 1 ? [node('user_message', 'User Prompt', 'TOOL_LOOP_PROMPT')] : id === 2 ? [node('tool_output', 'Tool Output', 'TOOL_LOOP_RESULT')] : []),
     { id: `invocation-${id}`, invocation_id: id, kind: 'llm_call', title: 'LLM Call', created_at },
     ...(id === 1 ? [node('tool_call', 'lookup_fixture', 'TOOL_LOOP_ARGUMENTS')] : id === 2 ? [node('assistant_response', 'Assistant Response', 'TOOL_LOOP_ANSWER')] : []),
   ] };
 }
 else if(url.searchParams.has('body_kind')){
   const id = Number(url.searchParams.get('body_id'));
   const offset = Number(url.searchParams.get('offset'));
   bodyReads.push([id, offset]);
   if (failBody) {
     failBody = false;
     await route.fulfill({ status: 503, json: { error: 'Fixture body retry' } });
     return;
   }
   const text = JSON.stringify(id === 2 ? { id, body_status: { RequestPayload: 'expired', ResponsePayload: 'expired' } } : {
     id, request_payload: 'COMPLETE_BODY_CONTENT', response_payload: 'COMPLETE_RESPONSE_CONTENT', error_message: 'COMPLETE_ERROR_DETAILS',
   });
   data = { data: offset ? text.slice(20) : text.slice(0, 20), next_offset: offset ? 0 : 20, total_bytes: text.length };
 }
 else {const cursor=url.searchParams.get('cursor')||'';cursors.push(cursor);if(cursor&&failNext){failNext=false;await route.fulfill({status:503,json:{error:'Fixture retry'}});return;}
 const ids = cursor ? [201] : [1, 2];
 const created_at = '2026-11-17T12:00:00Z';
 data = { trace_id: 'paged', username: 'admin', next_cursor: cursor ? '' : 'next-snapshot',
   body_records: ids.map(id => ({ kind: 'model_invocations', id })),
   invocations: ids.map(id => ({ id, trace_id: 'paged', request_id: 'req-shared', requested_model: 'm', actual_provider_model: 'm', provider_type: 'openai', status: 'completed', prompt_tokens: 10, completion_tokens: 5, total_tokens: 15, created_at })),
   audit_events: [], cost_records: [], risk_hits: [], messages: [],
   flow_nodes: ids.map(id => ({ id: `invocation-${id}`, kind: 'llm_call', title: 'LLM Call', invocation_id: id, created_at })),
 };}}
 else if(url.pathname.endsWith('/admin/ai-audit'))data={items:[],total:0,page:1,limit:20};
 await route.fulfill({json:{success:true,data}});
 });
 await page.goto(`http://127.0.0.1:${server.address().port}/admin/ai-audit?trace=paged`);
 await page.getByText('Trace metadata page 1',{exact:true}).waitFor();
 assert.deepEqual(bodyReads, [], 'metadata page fetched bodies eagerly');
 assert.equal(await page.getByText('Read record bodies', { exact: true }).count(), 0, 'a separate body list remains');
 const first = page.locator('[data-flow-node-id="invocation-1"]');
 const second = page.locator('[data-flow-node-id="invocation-2"]');
 await page.getByText('TOOL_LOOP_PROMPT', { exact: true }).waitFor({ timeout: 3500 });
 await page.getByText('TOOL_LOOP_ARGUMENTS', { exact: true }).waitFor();
 await page.getByRole('button', { name: 'Load more steps', exact: true }).click();
 await page.getByText('TOOL_LOOP_RESULT', { exact: true }).waitFor();
 await page.getByText('TOOL_LOOP_ANSWER', { exact: true }).waitFor();
 assert.deepEqual(await page.locator('[data-flow-node-id]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-flow-node-id'))), [
   'user_message-1', 'invocation-1', 'tool_call-1', 'tool_output-2', 'invocation-2', 'assistant_response-2',
 ]);
 assert.deepEqual(flowReads.slice(0, 2), [[[1, 2], null], [[2], '1']]);
 assert.deepEqual(bodyReads, [], 'restoring the flow downloaded original payloads');
 const firstAction = first.getByRole('button', { name: 'View original', exact: true });
 await firstAction.focus();
 await page.keyboard.press('Enter');
 await first.getByRole('alert').waitFor();
 await first.getByRole('button', { name: 'Retry', exact: true }).click();
 await first.getByText('COMPLETE_BODY_CONTENT', { exact: true }).waitFor();
 await first.getByText('COMPLETE_RESPONSE_CONTENT', { exact: true }).waitFor();
 await first.getByText('COMPLETE_ERROR_DETAILS', { exact: true }).waitFor();
 assert.deepEqual(bodyReads, [[1, 0], [1, 0], [1, 20]], 'the card did not retry and read its own complete body');
 assert.equal(await second.locator('pre').count(), 0, 'another attempt sharing the request ID rendered the body');
 await first.getByRole('button', { name: 'Hide original', exact: true }).click();
 await first.getByText('COMPLETE_BODY_CONTENT', { exact: true }).waitFor({ state: 'hidden' });
 await firstAction.click();
 await first.getByText('COMPLETE_BODY_CONTENT', { exact: true }).waitFor();
 assert.equal(bodyReads.length, 3, 'reopening an already loaded body fetched it again');
 await second.getByRole('button', { name: 'View original', exact: true }).click();
 await second.getByText('Original content has expired.', { exact: true }).waitFor();
 await first.screenshot({ path: process.env.CM_TRACE_SCREENSHOT ?? '/tmp/cm-trace-pagination-desktop.png' });
 for (const width of [1440, 1280, 768, 390]) {
   await page.setViewportSize({ width, height: 900 });
   assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `horizontal overflow at ${width}px`);
   await first.getByText('COMPLETE_BODY_CONTENT', { exact: true }).waitFor();
 }
 await first.screenshot({ path: process.env.CM_TRACE_MOBILE_SCREENSHOT ?? '/tmp/cm-trace-pagination-mobile.png' });
 await page.setViewportSize({ width: 1440, height: 1000 });
 failNext=true;await page.getByRole('button',{name:'Next',exact:true}).click();await page.waitForTimeout(300);await page.getByText('Trace metadata page 1',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Next',exact:true}).click();await page.getByText('Trace metadata page 2',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'Next',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'Prev',exact:true}).click();await page.getByText('Trace metadata page 1',{exact:true}).waitFor();assert.deepEqual(cursors.slice(-3),['next-snapshot','next-snapshot','']);
 await page.setViewportSize({ width: 390, height: 900 });
 for (const [locale, label] of [['zh', '查看原文'], ['ja', '原文を表示'], ['ko', '원문 보기'], ['de', 'Original anzeigen']]) {
   await page.evaluate(value => localStorage.setItem('clawmanager_locale', value), locale);
   await page.reload({ waitUntil: 'domcontentloaded' });
   const action = first.getByRole('button', { name: label, exact: true });
   await action.waitFor();
   assert.ok(await action.evaluate(button => button.scrollWidth <= button.clientWidth), `body action overflows in ${locale}`);
 }
 console.log('PASS execution flow ordering and continuation, inline original bodies, keyboard retry, expiry, metadata paging, and responsive labels');
}finally{await browser.close();await new Promise(resolve=>{server.close(resolve);server.closeAllConnections()})}
