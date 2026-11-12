// Server timestamps, not the browser's old snapshot date, describe progress.
// Missing checkpoints are unknown: never synthesize a successful completion.
export function portalSyncProgress(status:string, details?:{[key:string]:unknown}, now=Date.now()):string {
 if(status!=='updating')return '';
 const stage=typeof details?.current_stage==='string'?details.current_stage:'';
 const progress=typeof details?.last_progress_at==='string'?Date.parse(details.last_progress_at):NaN;
 const names:Record<string,string>={bootstrap:'历史初始化',usage:'用量来源',recent:'最近提问',history:'有效提问',projection:'数值快照',publish:'统计发布',ranking:'排名',reports:'周期报告',repair:'迟到数据核对'};
 if(!Number.isFinite(progress))return '处理中；尚无阶段进度记录，不能据此判断已完成。';
 const stalled=now-progress>120000;
 return `${stage?`当前阶段：${names[stage]||stage}。`:''}${stalled?'超过 2 分钟没有新进度，请核对工作者；未记录为成功。':'已保存阶段检查点，等待本批完成。'}`;
}
