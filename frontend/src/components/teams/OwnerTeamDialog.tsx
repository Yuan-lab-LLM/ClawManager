import { useEffect, useId, useRef, type ReactNode } from 'react';
import { X } from 'lucide-react';

/** Native top-layer dialog: the middle scrolls while its title/actions stay visible. */
export function OwnerTeamDialog({ title, busy = false, dirty = false, onClose, children }: { title: string; busy?: boolean; dirty?: boolean; onClose: () => void; children: ReactNode }) {
  const element = useRef<HTMLDialogElement>(null), id = useId();
  const close = () => { if (!busy && (!dirty || window.confirm('有尚未保存的修改，确认关闭？'))) onClose(); };
  useEffect(() => { const dialog = element.current; dialog?.showModal(); return () => dialog?.close(); }, []);
  return <dialog ref={element} className="ot-modal" aria-labelledby={id} onCancel={event => { event.preventDefault(); close(); }}>
    <header><h3 id={id}>{title}</h3><button type="button" className="ot-icon" aria-label={`关闭${title}`} disabled={busy} onClick={close}><X size={18}/></button></header>
    {children}
  </dialog>;
}

export function useOwnerTeamDraftGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    const refresh = (event: Event) => event.preventDefault();
    window.addEventListener('beforeunload', unload);
    window.addEventListener('clawmanager:before-page-refresh', refresh);
    return () => { window.removeEventListener('beforeunload', unload); window.removeEventListener('clawmanager:before-page-refresh', refresh); };
  }, [dirty]);
}
