// Display-only formatting. Keep original timestamps for ordering and full tooltips.
export function compactDisplayTime(value?:string) {
 if(!value)return '—';
 const date=new Date(value);if(!Number.isFinite(date.getTime()))return '—';
 const pad=(n:number)=>String(n).padStart(2,'0');
 return `${pad(date.getMonth()+1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
export function fullDisplayTime(value?:string,locale='zh-CN') {
 if(!value)return '—';const date=new Date(value);if(!Number.isFinite(date.getTime()))return '—';
 return new Intl.DateTimeFormat(locale,{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false,timeZoneName:'short'}).format(date);
}
