import type {GrowthTask} from '../services/ieiSystemService';

export function taskAchieved(task: Pick<GrowthTask,'awarded'|'progress'|'target'>): boolean {
 return task.awarded || task.target > 0 && task.progress >= task.target;
}
export function taskStateLabel(task: GrowthTask): string {
 if (task.awarded) return task.reward ? '已收藏' : '已发放';
 if (taskAchieved(task)) return task.reward ? '已达标，解锁待同步' : '已达标，奖励待入账';
 return '进行中';
}
export function statisticsTime(at?: string): string {
 if (!at || Number.isNaN(Date.parse(at))) return '时间暂不可用';
 return new Date(at).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false});
}
