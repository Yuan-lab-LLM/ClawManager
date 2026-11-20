import {useLayoutEffect,useRef,type ReactNode} from 'react';
import {X} from 'lucide-react';

export function OwnerModal({title,onClose,children,wide=false}:{title:string;onClose:()=>void;children:ReactNode;wide?:boolean}) {
  const ref=useRef<HTMLDialogElement>(null);
  useLayoutEffect(()=>{
    const dialog=ref.current;
    const trigger=document.activeElement;
    dialog?.showModal();
    return ()=>{dialog?.close();if(trigger instanceof HTMLElement&&trigger.isConnected)trigger.focus({preventScroll:true})};
  },[]);
  return <dialog ref={ref} aria-label={title} onCancel={event=>{event.preventDefault();onClose()}} className={`owner-modal${wide?' owner-modal-wide':''}`} onClick={event=>{
    if(event.target!==event.currentTarget)return;
    const box=event.currentTarget.getBoundingClientRect();
    if(event.clientX<box.left||event.clientX>box.right||event.clientY<box.top||event.clientY>box.bottom)onClose();
  }}>
    <header><h3>{title}</h3><button type="button" aria-label="关闭明细" onClick={onClose}><X size={20}/></button></header>
    <div className="owner-modal-body">{children}</div>
  </dialog>;
}
