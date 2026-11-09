// Product-native vector artwork based on the approved five-stage hanging medals.
// Small, independent SVGs avoid scaling the labelled design sheet into the UI.
export const growthTitles=['探索者','践行者','协创者','领航者','智创专家'];
export const growthThreshold=(level:number)=>{
 const n=Math.max(0,level-1);
 return Math.floor(((120*n+5*n*(n-1))*2700+8820)/17640)*10;
};
export const growthStage=(level:number)=>Math.min(4,Math.max(0,Math.floor((level-1)/10)));
export function OwnerGrowthMedal({level,className=''}:{level:number;className?:string}){
 const stage=growthStage(level);const colors=['#258ded','#13bbb9','#9460d9','#ee5751','#2586dc'];const color=colors[stage];const rim=stage<2?'#dce7f1':'#ffe291';const edge=stage<2?'#9cacc0':'#e9aa30';
 const jewel=(x:number,y:number,size:number,c:string)=><g transform={`translate(${x} ${y})`}><path d={`M0 ${-size} L${size} 0 0 ${size} ${-size} 0Z`} fill="#ffdf85" stroke="#e7ac39" strokeWidth="1.4"/><path d={`M0 ${-size+3} L${size-3} 0 0 ${size-3} ${-size+3} 0Z`} fill={c}/><path d={`M0 ${-size+3} L0 0 ${-size+3} 0Z`} fill="#ffffff" opacity=".65"/><path d={`M0 0 L${size-3} 0 0 ${size-3}Z`} fill="#173c9a" opacity=".25"/></g>;
 return <svg className={`owner-medal ${className}`} viewBox="0 0 112 120" role="img" aria-label={`${growthTitles[stage]}勋章`}>
  <path d="M29 8H83V49L56 65 29 49Z" fill={color}/><path d="M29 8H83V18H29Z" fill="#0c3775" opacity=".15"/>
  {stage===0?<path d="M48 10H61V61L48 57Z" fill="#d3e9fc"/>:<><path d="M38 10H43V56L38 53Z M69 10H74V53L69 56Z" fill={stage===1?'#e5f2d5':'#ffe695'}/>{stage===4&&<path d="M49 10H63V59L56 64 49 59Z" fill="#a078ef"/>}</>}
  <path d="M26 5H86V13H26Z" fill="#ffdb77"/><path d="M26 12H86V14H26Z" fill="#eda934"/>
  {stage>0&&[false,true].map(mirror=><g key={String(mirror)} transform={mirror?'translate(112 0) scale(-1 1)':undefined}>
   {Array.from({length:Math.min(stage+1,3)},(_,i)=><g key={i}><path d={`M37 ${70+i*11} L${9-stage*2} ${57+i*13} L${12-stage*2} ${68+i*12} L39 ${82+i*8}Z`} fill={rim} stroke={edge} strokeWidth="1"/><path d={`M${12-stage*2} ${68+i*12} L39 ${82+i*8} L39 ${78+i*8}Z`} fill={edge}/></g>)}
  </g>)}
  {stage===0?<><circle cx="56" cy="82" r="32" fill={rim} stroke={edge} strokeWidth="1.5"/><circle cx="56" cy="82" r="25" fill={color} stroke="#2072c6" strokeWidth="1.5"/></>:<><path d="M56 47L83 62Q88 65 88 71V94Q88 100 83 103L56 118 29 103Q24 100 24 94V71Q24 65 29 62Z" fill={rim} stroke={edge} strokeWidth="1.5"/><path d="M56 56L79 70V96L56 109 33 96V70Z" fill={color}/><path d="M33 96L79 70V96L56 109Z" fill="#27378e" opacity=".12"/></>}
  {stage<4?<><path d="M56 66L61 78 73 82 61 87 56 99 51 87 39 82 51 78Z" fill="#f5fbff"/><path d="M56 66V82H73L61 78Z M56 82V99L51 87 39 82Z" fill="#b9e2fb"/></>:<><path d="M44 70H68L78 82 56 105 34 82Z" fill="#30d5f5"/><path d="M44 70L56 82 68 70Z M34 82H56L44 70Z" fill="#a1f4fc"/><path d="M56 82H78L56 105Z" fill="#0ca4db"/></>}
  {stage>=2&&jewel(56,51,10,stage===3?'#f14351':stage===4?'#9572ed':'#14c9ed')}
  {stage>=3&&<>{jewel(30,stage===4?80:103,8,stage===4?'#9272ec':'#11c8ef')}{jewel(82,stage===4?80:103,8,stage===4?'#9272ec':'#11c8ef')}</>}
 </svg>;
}
