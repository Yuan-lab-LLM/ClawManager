import {useState} from 'react';
import {ChevronRight, Cpu, Footprints, MessagesSquare, Route} from 'lucide-react';
import type {GrowthTask, OwnerGrowth, UsageDay} from '../services/ieiSystemService';
import {UsageChart, ownerNumber} from './OwnerUsageCharts';
import {OwnerModal} from './OwnerModal';
import {taskAchieved, taskStateLabel, statisticsTime} from '../lib/ownerTaskState';
import {ownerWelcomeName} from '../lib/ownerPresentation';
import {OwnerGrowthMedal, growthStage, growthTitles} from './OwnerGrowthMedal';
import {OwnerGrowthPath} from './OwnerGrowthPath';
import {visibleMilestones} from '../lib/ownerMilestones';

export function GrowthSummary({growth, owner, openGrowth}: {growth: OwnerGrowth | null; owner: string; openGrowth: () => void}) {
 const capped = !!growth && growth.level >= (growth.open_level ?? 50);
 const span = growth ? growth.next_level - growth.level_start : 0;
 const earned = growth ? Math.max(0, growth.xp - growth.level_start) : 0;
 const progress = capped ? 100 : span > 0 ? Math.min(100, earned / span * 100) : 0;
 const name = ownerWelcomeName(owner);
 return <>
  <div className="owner-welcome">
   <OwnerGrowthMedal level={growth?.level ?? 1}/>
   <div className="owner-identity-copy">
    <div className="owner-welcome-line"><span title={`欢迎回来，${name}`}>欢迎回来，{name}</span></div>
    <div className="owner-level-line">
     <div className="owner-level">Lv.{growth?.level ?? '—'}</div>
     <span className="owner-title-badge">{growth ? growthTitles[growthStage(growth.level)] : '称号待同步'}</span>
     <button type="button" className="owner-compact-button" onClick={openGrowth} aria-haspopup="dialog"><Route size={14} aria-hidden="true"/>成长路径</button>
    </div>
    <div className="owner-progress">
     <div className="h-1.5 overflow-hidden rounded-full bg-blue-100" role="progressbar" aria-label="升级经验" aria-valuenow={Math.round(progress)}>
      <div className="h-full rounded-full bg-[var(--cm-primary)]" style={{width:`${progress}%`}}/>
     </div>
     <p>{capped ? '已达当前开放等级' : growth ? `${earned} / ${span} XP · 距下级 ${Math.max(0, growth.next_level - growth.xp)} XP` : '等待经验统计'}</p>
    </div>
   </div>
  </div>
  <div className="owner-growth-stat" data-testid="growth-stat">
   <p>近七天 Token 排名</p>
   <strong>{growth?.ranking?.rank ? `第 ${growth.ranking.rank} 名` : growth ? '暂未上榜' : '—'}</strong>
   <small title={growth?.ranking ? `${new Date(growth.ranking.start).toLocaleString('zh-CN')} 至 ${new Date(growth.ranking.end).toLocaleString('zh-CN')} · 同量并列` : undefined}>
    {growth?.ranking ? `${ownerNumber(growth.ranking.tokens)} Token · ${growth.ranking.participants} 位参与者` : '暂无排名数据'}
   </small>
  </div>
 </>;
}

const dateLabel = new Intl.DateTimeFormat('en-CA', {timeZone:'Asia/Shanghai', year:'numeric', month:'2-digit', day:'2-digit'});

export function OwnerGrowthDetails({growth, onRefresh, loading, onClose}: {growth: OwnerGrowth | null; onRefresh: () => void; loading: boolean; onClose: () => void}) {
 const [section,setSection] = useState<'path'|'trend'|'ledger'|'rules'>('path');
 const sections = [['path','成长路径'],['trend','经验趋势'],['ledger','经验明细'],['rules','成长规则']] as const;
 const today = Date.now();
 const firstDay = growth?.started_at ? dateLabel.format(new Date(growth.started_at)) : '';
 const days: UsageDay[] = Array.from({length:30}, (_, index) => {
  const date = dateLabel.format(new Date(today - (29-index)*86400000));
  return {date,tokens:growth?.days?.find(day => day.date === date)?.xp ?? 0,prompt_tokens:0,completion_tokens:0,questions:0,sessions:0,observed:!!firstDay && date >= firstDay};
 });
 return <OwnerModal title="成长详情" onClose={onClose} wide>
  <div className="owner-detail-toolbar">
   <div className="owner-compact-tabs" aria-label="成长详情栏目">
    {sections.map(([value,label]) => <button key={value} type="button" aria-pressed={section===value} onClick={() => setSection(value)}>{label}</button>)}
   </div>
   <button type="button" className="owner-compact-button" disabled={loading} onClick={onRefresh}>更新进度</button>
  </div>
  {!growth ? <p role="status">{loading ? '正在读取成长数据…' : '成长数据暂不可用，请重试。'}</p> : <>
   {section==='path' && <OwnerGrowthPath growth={growth}/>}
   {section==='trend' && <section className="owner-chart-card"><h3>近 30 天经验增长</h3><p className="owner-chart-caption">已入账奖励 · 统计快照 {statisticsTime(growth.as_of)}</p><UsageChart days={days} label="XP" maxTicks={4} height={160}/></section>}
   {section==='ledger' && <section><p className="owner-chart-caption">最近 100 条奖励记录 · 每个任务阶梯只奖励一次</p>{growth.ledger.length ? growth.ledger.map((entry,index) => <div key={`${entry.at}-${index}`} className="owner-detail-row"><div>{entry.name}<div className="owner-metric-note">{statisticsTime(entry.at)}</div></div><strong>+{entry.xp} XP</strong></div>) : <p>尚无奖励记录</p>}</section>}
   {section==='rules' && <section><h4>成长规则与统计范围</h4><p>{growth.note}</p></section>}
  </>}
 </OwnerModal>;
}

const taskUnits: Record<string,string> = {questions:'次',login:'天',full_days:'天',tokens:'Token'};

function taskTarget(task: GrowthTask) {
 if (task.metric === 'seconds') return `${Math.floor(task.progress/60)} / ${task.target/60} 分钟`;
 const unit = taskUnits[task.metric];
 return `${ownerNumber(task.progress)} / ${ownerNumber(task.target)}${unit ? ` ${unit}` : ''}`;
}

function TaskRows({tasks, upcoming=false}: {tasks: GrowthTask[]; upcoming?: boolean}) {
 if (!tasks.length) return <p className="owner-metric-note">本栏暂无任务</p>;
 return <ul className="owner-integrated-task-list">
  {tasks.map(task => <li key={task.code} className="owner-detail-row" data-awarded={task.awarded}>
   <div><strong>{task.name}</strong><small>{upcoming ? taskTarget({...task,progress:0}) : taskTarget(task)}</small></div>
   <div><span>{upcoming ? '待生效' : taskStateLabel(task)}</span><strong>{task.reward || `+${task.xp} XP`}</strong></div>
  </li>)}
 </ul>;
}

function TaskCards({tasks}: {tasks: GrowthTask[]}) {
 return <ul className="owner-growth-cards">
  {tasks.map(task => {
   const Icon = task.metric==='tokens' ? Cpu : ['login','full_days'].includes(task.metric) ? Footprints : MessagesSquare;
   const progress = Math.min(100,Math.max(0,task.progress/Math.max(1,task.target)*100));
   return <li key={task.code} className="owner-task" data-awarded={task.awarded}>
    <div className="owner-task-title"><Icon size={16} aria-hidden="true"/><strong>{task.name}</strong></div>
    <p className="owner-task-condition" title={taskStateLabel(task)}><span>{taskStateLabel(task)}</span> · {task.metric==='tokens'?'Token 用量':task.metric==='questions'?'有效提问':task.metric==='seconds'?'使用时长':'访问天数'}</p>
    <div className="owner-task-progress" role="progressbar" aria-label={`${task.name}进度`} aria-valuenow={Math.round(progress)} aria-valuemin={0} aria-valuemax={100}><span style={{width:`${progress}%`}}/></div>
    <div className="owner-task-values"><span>{taskTarget(task)}</span><strong>{task.reward || `+${task.xp} XP`}</strong></div>
   </li>;
  })}
 </ul>;
}

export function OwnerGrowthTasks({growth, open, show, close, loading}: {growth: OwnerGrowth | null; open: boolean; show: () => void; close: () => void; loading: boolean}) {
 const [period,setPeriod] = useState<'day'|'all'>('day');
 const [showUpcoming,setShowUpcoming] = useState(false);
 const tasks = growth?.tasks.filter(task => task.period === period) ?? [];
 const done = tasks.filter(task => period === 'day' ? taskAchieved(task) : task.awarded).length;
 const earned = tasks.filter(task => task.awarded).reduce((total,task) => total + task.xp,0);
 const milestones = period === 'all' ? visibleMilestones(tasks) : tasks;
 const tabs = <div className="owner-compact-tabs owner-task-tabs" aria-label="任务类型">
  {(['day','all'] as const).map(value => <button key={value} type="button" aria-pressed={!showUpcoming && period===value} onClick={() => {setShowUpcoming(false);setPeriod(value)}}>{value==='day'?'每日':'累计'}</button>)}
 </div>;
 return <section className="owner-chart-card owner-integrated-tasks" aria-label="成长任务">
  <div className="owner-chart-head"><h3>成长任务</h3>{tabs}</div>
  {growth ? <>
   <div className="owner-task-summary" data-testid="task-stat"><strong>{period==='day'?'今日':'累计'} {done} / {tasks.length} {period==='day'?'达标':'已完成'}</strong>{period==='day' && <small>已入账 {earned} / {growth.daily_limit??105} XP</small>}</div>
   <TaskCards tasks={(milestones.length ? milestones : tasks).slice(0,6)}/>
  </> : <p role="status">{loading ? '正在读取成长任务…' : '成长任务暂不可用'}</p>}
  <div className="owner-task-footer"><small>达标后奖励自动入账</small><button type="button" className="owner-more-button" aria-haspopup="dialog" onClick={() => {setShowUpcoming(false);show()}}>全部任务与规则 <ChevronRight size={15} aria-hidden="true"/></button></div>
  {open && <OwnerModal title="全部成长任务" onClose={close}>
   <div className="owner-detail-toolbar">{tabs}{!!growth?.upcoming_tasks?.length && <button type="button" className="owner-compact-button" aria-pressed={showUpcoming} onClick={() => setShowUpcoming(true)}>新任务预告</button>}</div>
   {showUpcoming && growth?.effective_at && <p className="owner-metric-note">{statisticsTime(growth.effective_at)} 生效</p>}
   <TaskRows tasks={showUpcoming ? growth?.upcoming_tasks ?? [] : tasks} upcoming={showUpcoming}/>
   {growth?.note && <details><summary>成长规则</summary><p className="owner-metric-note">{growth.note}</p></details>}
  </OwnerModal>}
 </section>;
}
