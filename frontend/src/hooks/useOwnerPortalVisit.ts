import {useEffect} from 'react';
import {ieiSystemService} from '../services/ieiSystemService';

// Best effort only: failures never block instance access or the unassigned page.
// Successful visits are deduplicated per authenticated owner and Beijing date.
export function useOwnerPortalVisit(owner?:string) {
  useEffect(()=>{
    if(!owner)return;
    let active=true, busy=false, completed='';
    const day=()=>new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date());
    const visit=()=>{
      if(!active||busy||document.visibilityState!=='visible')return;
      const date=day();
      if(completed===date)return;
      busy=true;
      ieiSystemService.recordVisit().then(()=>{completed=date}).catch(()=>{}).finally(()=>{busy=false});
    };
    visit();
    // No network request after a successful same-day visit; this handles midnight
    // for a portal left visible overnight and bounded retries after failure.
    const timer=window.setInterval(visit,60000);
    document.addEventListener('visibilitychange',visit);
    return()=>{active=false;clearInterval(timer);document.removeEventListener('visibilitychange',visit)};
  },[owner]);
}
