import {useEffect,useRef,type ReactNode} from 'react';
import {X} from 'lucide-react';

export function OwnerModal({title,onClose,children}:{title:string;onClose:()=>void;children:ReactNode}) {
  const ref=useRef<HTMLDialogElement>(null);
  useEffect(()=>{const dialog=ref.current;dialog?.showModal();return()=>dialog?.close()},[]);
  return <dialog ref={ref} aria-label={title} onCancel={onClose} className="owner-modal" onClick={e=>{if(e.target===e.currentTarget)onClose()}}>
    <header><h3>{title}</h3><button autoFocus type="button" aria-label="关闭明细" onClick={onClose}><X size={20}/></button></header>
    <div className="owner-modal-body">{children}</div>
  </dialog>;
}
