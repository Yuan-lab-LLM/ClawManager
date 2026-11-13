import assert from 'node:assert/strict';
import {test} from 'node:test';
import {workspaceUsageValue, formatWorkspaceUsage} from '../src/lib/workspaceUsage.ts';
test('workspace bytes require observation proof, including zero',()=>{
 assert.equal(workspaceUsageValue({workspace_usage_bytes:0}),undefined);
 assert.equal(workspaceUsageValue({workspace_usage_bytes:123}),undefined);
 assert.equal(formatWorkspaceUsage({workspace_usage_bytes:0},'Unavailable'),'Unavailable');
 assert.equal(formatWorkspaceUsage({workspace_usage_bytes:0,workspace_usage_observed_at:'2026-11-12T00:00:00Z'},'Unavailable'),'0 B');
 assert.equal(formatWorkspaceUsage({workspace_usage_bytes:2048,workspace_usage_observed_at:'2025-01-01T00:00:00Z'},'Unavailable'),'2.0 KB');
 assert.equal(workspaceUsageValue({workspace_usage_bytes:-1,workspace_usage_observed_at:'2026-11-12T00:00:00Z'}),undefined);
});

test('status proof invalidation wins over stale detail and failures retain good value',async()=>{
 const {selectWorkspaceUsage,workspaceUsageText}=await import('../src/lib/workspaceUsage.ts');
 const good={workspace_usage_bytes:2048,workspace_usage_observed_at:'2025-01-01T00:00:00Z'};
 assert.equal(formatWorkspaceUsage(selectWorkspaceUsage(good,null),'Unavailable'),'2.0 KB');
 assert.equal(formatWorkspaceUsage(selectWorkspaceUsage(good,{workspace_usage_bytes:0,workspace_usage_observed_at:null}),'Unavailable'),'Unavailable');
 assert.match(workspaceUsageText('en',good).description,/Last measured/);
 assert.match(workspaceUsageText('zh',good).description,/最近测量/);
 for(const locale of ['ja','ko','de'])assert.equal(workspaceUsageText(locale,{}).unavailable,'Unavailable');
});
