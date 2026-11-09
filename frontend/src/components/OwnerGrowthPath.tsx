import {useState,type CSSProperties} from 'react';
import type {OwnerGrowth} from '../services/ieiSystemService';
import {OwnerGrowthMedal,growthStage,growthThreshold,growthTitles} from './OwnerGrowthMedal';
import {OwnerModal} from './OwnerModal';

export function OwnerGrowthPath({growth}:{growth:OwnerGrowth}){
 const [selected,setSelected]=useState<number|null>(null);
 const threshold=(level:number)=>growth.level_thresholds?.[level-1]??growthThreshold(level);
 const description=(level:number)=>`Lv.${level} · ${growthTitles[growthStage(level)]}｜累计 ${threshold(level).toLocaleString()} XP｜${level===50?'当前开放上限':`升至下一级需要 ${threshold(level+1)-threshold(level)} XP`}｜${level===growth.level?'你在这里':level<growth.level?'已达到':`还需 ${Math.max(0,threshold(level)-growth.xp).toLocaleString()} XP`}`;
 return <section className="owner-growth-path" aria-label="等级成长路径">
  <header><h3>成长路径 <span>Lv.1–50</span></h3><span>你在 Lv.{growth.level}</span></header>
  <div className="owner-path-stages">{growthTitles.map((title,stage)=><div key={title} className={`owner-path-stage ${growthStage(growth.level)===stage?'current':''}`}>
   <div className="owner-path-stage-label"><OwnerGrowthMedal level={stage*10+1}/><div><strong>{title}</strong></div></div>
  </div>)}</div>
  <div className="owner-path-nodes owner-path-axis" style={{'--growth-axis-progress':`${Math.max(0,Math.min(49,growth.level-1))/49*98}%`} as CSSProperties}>{Array.from({length:50},(_,i)=>{const level=i+1;return <button key={level} type="button" className={`${level===growth.level?'current':level<growth.level?'reached':''} ${level%10===0?'major':''}`} aria-current={level===growth.level?'step':undefined} aria-label={description(level)} onClick={()=>setSelected(level)}><span aria-hidden="true"/>{level%10===0&&<em className="owner-axis-label">{level}</em>}{level===growth.level&&<b className="owner-path-locate">当前 Lv.{level}</b>}<i role="tooltip">{description(level)}</i></button>})}</div>
  {selected!==null&&<OwnerModal title={`Lv.${selected} · ${growthTitles[growthStage(selected)]}`} onClose={()=>setSelected(null)}><div className="owner-level-detail"><OwnerGrowthMedal level={selected}/><p>累计经验门槛：{threshold(selected).toLocaleString()} XP</p><p>{selected===50?'已达到当前开放上限。':`从 Lv.${selected} 升至 Lv.${selected+1} 需要 ${threshold(selected+1)-threshold(selected)} XP。`}</p><p>{selected<=growth.level?'你已解锁此等级。':`距离该等级还需 ${Math.max(0,threshold(selected)-growth.xp).toLocaleString()} XP。`}</p><p>每十级一个称号与勋章阶段，当前只开放前五档。</p></div></OwnerModal>}
 </section>;
}
