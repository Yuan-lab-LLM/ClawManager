import {useCallback,useEffect,useRef,useState} from 'react';
import {ieiSystemService} from '../services/ieiSystemService';

// Only interaction timing is observed, never keys, text, files or messages.
// Cross-origin frame contents are intentionally not inspected or trusted.
export function useOwnerActivity(instanceID:number,enabled:boolean){
 const lastInteraction=useRef(0);const cleanupFrame=useRef<(()=>void)|null>(null);const [frameObserved,setFrameObserved]=useState(false);const [captureError,setCaptureError]=useState(false);
 const observe=useCallback((doc:Document)=>{
  const touched=(event:Event)=>{if(event.isTrusted&&document.visibilityState==='visible')lastInteraction.current=Date.now()};
  const events=['pointerdown','keydown','wheel','pointermove'];events.forEach(name=>doc.addEventListener(name,touched,{passive:true,capture:true}));
  return()=>events.forEach(name=>doc.removeEventListener(name,touched,true));
 },[]);
 const onFrameLoad=useCallback((frame:HTMLIFrameElement)=>{cleanupFrame.current?.();cleanupFrame.current=null;setFrameObserved(false);try{const doc=frame.contentDocument;if(doc){cleanupFrame.current=observe(doc);setFrameObserved(true)}}catch{/* Independent origins are unsupported, not inferred as active. */}},[observe]);
 useEffect(()=>{
  if(!enabled||instanceID<=0)return;let stopped=false;let pending=false;let wasActive=false;let sequence=0;let stream=crypto.randomUUID();lastInteraction.current=0;
  const stopDocument=observe(document);if(document.visibilityState==='visible')void ieiSystemService.getGrowth(true,true).catch(()=>{});
  const visibility=()=>{lastInteraction.current=0;stream=crypto.randomUUID();sequence=0;wasActive=false};document.addEventListener('visibilitychange',visibility);
  const timer=setInterval(()=>{if(stopped||pending)return;const active=document.visibilityState==='visible'&&document.hasFocus()&&Date.now()-lastInteraction.current<60000;if(!active&&!wasActive)return;wasActive=active;pending=true;void ieiSystemService.activity(instanceID,stream,++sequence,active).then(()=>{if(!stopped)setCaptureError(false)}).catch(()=>{if(!stopped)setCaptureError(true)}).finally(()=>{pending=false})},60000);
  return()=>{stopped=true;clearInterval(timer);stopDocument();cleanupFrame.current?.();cleanupFrame.current=null;document.removeEventListener('visibilitychange',visibility)};
 },[enabled,instanceID,observe]);
 return {onFrameLoad,activityNote:captureError?'前台计时暂不可用，实例使用不受影响':frameObserved?'前台交互计时已启用 · 闲置 60 秒后暂停':'仅门户区域交互可计时；独立域名内的交互不计时'};
}
