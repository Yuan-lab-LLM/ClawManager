import {useState} from 'react';
import {Check,TrendingUp,Footprints,MessagesSquare,Cpu,CalendarCheck} from 'lucide-react';
import type {OwnerGrowth,UsageDay} from '../services/ieiSystemService';
import {UsageChart,ownerNumber} from './OwnerUsageCharts';
import {OwnerModal} from './OwnerModal';
import {taskAchieved,taskStateLabel,statisticsTime} from '../lib/ownerTaskState';
import {ownerWelcomeName} from '../lib/ownerPresentation';
import {OwnerGrowthMedal,growthStage,growthTitles} from './OwnerGrowthMedal';
import {OwnerGrowthPath} from './OwnerGrowthPath';
import {visibleMilestones} from '../lib/ownerMilestones';

export function GrowthSummary({growth,owner='',embedded=false}:{growth:OwnerGrowth|null;owner?:string;embedded?:boolean}){
 const capped=!!growth&&growth.level>=(growth.open_level??50);const progress=capped?100:growth?Math.max(0,(growth.xp-growth.level_start)/(growth.next_level-growth.level_start)*100):0;const name=ownerWelcomeName(owner);
 const levelLine=<div className="owner-level-line"><div className="owner-level">Lv.{growth?.level??'—'}</div><span className="owner-title-badge">{growth?growthTitles[growthStage(growth.level)]:'正在读取'}</span></div>;
 const progressLine=<div className="owner-progress"><div className="h-1.5 overflow-hidden rounded-full bg-blue-100"><div className="h-full rounded-full bg-gradient-to-r from-blue-600 to-cyan-400" style={{width:`${Math.min(100,progress)}%`}}/></div><p>{capped?'已达当前开放等级':growth?`${growth.xp-growth.level_start} / ${growth.next_level-growth.level_start} XP · 距下级 ${growth.next_level-growth.xp} XP`:'仅展示已入账经验'}</p></div>;
 return <div className={`owner-growth-summary ${embedded?'embedded':''}`}>
  <div className="owner-welcome"><OwnerGrowthMedal level={growth?.level??1}/><div className="owner-identity-copy">{embedded&&<div className="owner-welcome-line"><span title={`欢迎回来，${name}`}>欢迎回来，{name}</span></div>}{levelLine}{progressLine}</div></div>
  <div className="owner-growth-stat" data-testid="growth-stat"><p>近七天 Token 排名</p><strong>{growth?.ranking?.rank?`第 ${growth.ranking.rank} 名`:'暂未上榜'}</strong><small title={growth?.ranking?`${new Date(growth.ranking.start).toLocaleString('zh-CN')} 至 ${new Date(growth.ranking.end).toLocaleString('zh-CN')} · 同量并列`:undefined}>{growth?.ranking?`${ownerNumber(growth.ranking.tokens)} Token · ${growth.ranking.participants} 位参与者`:'正在读取排名'}</small></div>
  <div className="owner-growth-stat" data-testid="task-stat"><p>今日任务</p><strong>{growth?.tasks.filter(t=>t.period==='day'&&taskAchieved(t)).length??'—'} / {growth?.tasks.filter(t=>t.period==='day').length??'—'}</strong><small>已达标任务 · 奖励自动入账</small></div>
 </div>;
}
export function OwnerGrowthPanel({growth,owner,onRefresh,loading}:{growth:OwnerGrowth|null;owner:string;onRefresh:()=>void;loading:boolean}){
 const [period,setPeriod]=useState<'day'|'all'>('day');const [modal,setModal]=useState<'ledger'|'rules'|'preview'|null>(null);
 if(!growth)return <p role="status">成长数据暂不可用，请重试。</p>;
 const now=new Date();const dateLabel=(at:Date)=>new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'}).format(at);
 const days:UsageDay[]=Array.from({length:30},(_,i)=>{const date=dateLabel(new Date(now.getTime()-(29-i)*86400000));return {date,tokens:growth.days?.find(d=>d.date===date)?.xp??0,prompt_tokens:0,completion_tokens:0,questions:0,sessions:0,observed:!!growth.started_at&&date>=dateLabel(new Date(growth.started_at))}});
 const preview=false;
 const tasks=growth.tasks.filter(t=>t.period===period);const displayed=period==='day'?tasks:visibleMilestones(tasks);
 const dailyXP=growth.tasks.filter(t=>t.period==='day'&&t.awarded).reduce((sum,t)=>sum+t.xp,0);
 const labels:Record<string,string>={login:'今日访问',questions:'有效提问',tokens:'Token 用量',seconds:'前台活跃',full_days:'全部完成天数'};
 const collectionIcons={login:Footprints,questions:MessagesSquare,tokens:Cpu,full_days:CalendarCheck};
 return <div className="owner-growth"><GrowthSummary growth={growth} owner={owner}/><div className="owner-growth-body">
  <section className="owner-chart-card"><div className="owner-chart-head"><h3 className="flex items-center gap-2"><TrendingUp size={16}/>近 30 天经验增长</h3><button disabled={loading} onClick={onRefresh} className="rounded-lg border px-2 py-1 text-sm">更新进度</button></div><p className="owner-chart-caption">已入账奖励 · 统计快照 {statisticsTime(growth.as_of)}</p><UsageChart days={days} label="XP" maxTicks={4} fit/></section>
  <section className="owner-task-panel" aria-label="成长任务"><div className="owner-task-tabs">{(['day','all'] as const).map(p=><button key={p} aria-pressed={period===p} onClick={()=>setPeriod(p)}>{p==='day'?'每日任务':'累计任务'}</button>)}<span className="ml-auto text-xs text-slate-500">{period==='day'?`已入账 ${dailyXP} / ${growth.daily_limit??105} XP`: `${tasks.filter(t=>t.awarded).length} / ${tasks.length} 已完成`}</span></div>
   {!!growth.upcoming_tasks?.length&&<button className="owner-rule-scheduled" onClick={()=>setModal('preview')}>新任务于 {new Date(growth.effective_at!).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false})} 生效 · 查看规则</button>}
   <div className={`owner-task-list owner-daily-grid ${period==='all'?'owner-milestone-grid':''}`}>{displayed.map(task=>{const Icon=collectionIcons[task.metric as keyof typeof collectionIcons]??Footprints;return <article key={task.code} className="owner-task"><div className="flex items-center justify-between gap-2 text-sm"><span className="flex min-w-0 items-center gap-2">{period==='all'&&<Icon size={18} className="shrink-0 text-blue-500"/>}{task.name}{period==='all'?` ${ownerNumber(task.target)}${task.metric==='login'?' 天':task.metric==='questions'?' 次':''}`:''}</span><span className="shrink-0 text-blue-600">+{task.xp} XP</span></div><p className="owner-task-condition">{period==='all'?'累计达标后自动奖励':`${labels[task.metric]} · ${task.metric==='login'?'每日首次访问':`${ownerNumber(task.target)}${task.metric==='questions'?' 次':' Token'}`}`}</p><div className="my-2 h-1.5 overflow-hidden rounded-full bg-slate-100"><div className={`h-full rounded-full ${task.awarded?'bg-teal-400':'bg-blue-400'}`} style={{width:`${Math.min(100,(task.progress??0)/task.target*100)}%`}}/></div><div className="flex justify-between text-xs text-slate-500"><span>{preview?'待生效':task.metric==='seconds'?`${Math.floor(task.progress/60)} / ${task.target/60} 分钟`:`${ownerNumber(task.progress)} / ${ownerNumber(task.target)}`}</span><span className="flex items-center gap-1">{task.awarded&&<Check size={12}/>} {preview?'新任务预览':taskStateLabel(task)}</span></div></article>})}</div>
  </section></div><OwnerGrowthPath growth={growth}/><footer className="owner-growth-footer"><span title="刷新读取已有统计快照；新用量由后台分批汇总，不会立即重算。">数据按批次同步</span><button onClick={()=>setModal('rules')}>成长规则与统计范围</button><button className="text-blue-600" onClick={()=>setModal('ledger')}>查看经验明细</button></footer>
  {modal&&<OwnerModal title={modal==='ledger'?'经验明细':modal==='preview'?'新任务预告':'成长规则与统计范围'} onClose={()=>setModal(null)}>{modal==='ledger'?<><p className="mb-3 text-slate-500">最近 100 条奖励记录；每个任务阶梯只奖励一次。</p>{growth.ledger.length?growth.ledger.map((entry,i)=><div key={`${entry.at}-${i}`} className="flex justify-between gap-4 border-t py-3"><div>{entry.name}<p className="text-xs text-slate-500">{new Date(entry.at).toLocaleString('zh-CN')}</p></div><span className="text-blue-600">+{entry.xp} XP</span></div>):<p>尚无奖励记录</p>}</>:<><p>{growth.note}</p>{modal==='preview'&&growth.upcoming_tasks?.filter(t=>t.period==='day').map(t=><div className="flex justify-between border-t py-2" key={t.code}><span>{t.name} · {labels[t.metric]} {t.target.toLocaleString()}</span><span>+{t.xp} XP</span></div>)}</>}</OwnerModal>}
 </div>;
}
