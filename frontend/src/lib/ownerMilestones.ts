import type {GrowthTask} from '../services/ieiSystemService';
import {taskAchieved} from './ownerTaskState';

// Presentation only: hidden milestones remain in the authoritative task list.
export function visibleMilestones(tasks: GrowthTask[]): GrowthTask[] {
 return ['login','questions','tokens'].flatMap(metric=>{
  const stages=tasks.filter(t=>t.period==='all'&&t.metric===metric).sort((a,b)=>a.target-b.target);
  if(stages.length<=2)return stages;
  let reached=-1;
  stages.forEach((task,index)=>{if(taskAchieved(task))reached=index});
  const first=Math.max(0,Math.min(reached,stages.length-2));
  return stages.slice(first,first+2);
 });
}
