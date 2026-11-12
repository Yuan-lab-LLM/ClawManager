import assert from 'node:assert/strict';
import {portalSyncProgress} from '../src/lib/portalSyncProgress.ts';
const now=Date.parse('2026-10-16T12:00:00Z');
assert.equal(portalSyncProgress('success',{},now),'');
assert.match(portalSyncProgress('updating',{},now),/尚无阶段进度/);
assert.match(portalSyncProgress('updating',{current_stage:'history',last_progress_at:'2026-10-16T11:59:30Z'},now),/有效提问.*等待本批完成/);
assert.match(portalSyncProgress('updating',{current_stage:'usage',last_progress_at:'2026-10-16T11:55:00Z'},now),/超过 2 分钟.*未记录为成功/);
assert.match(portalSyncProgress('updating',{last_progress_at:'invalid'},now),/尚无阶段进度/);
console.log('PASS portal sync progress: current, stalled, missing, invalid, completed');
