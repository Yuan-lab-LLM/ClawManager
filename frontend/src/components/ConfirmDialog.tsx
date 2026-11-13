import React, { useEffect, useId, useRef } from 'react';

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
  onConfirm: () => void;
  onCancel: () => void;
  loading?: boolean;
  destructive?: boolean;
}
const ConfirmDialog: React.FC<ConfirmDialogProps> = ({ open, title, message, confirmLabel, cancelLabel, onConfirm, onCancel, loading = false, destructive = false }) => {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const messageId = useId();
  useEffect(() => {
    const element = dialog.current;
    if (!element || !open) return;
    const previous = document.activeElement as HTMLElement | null;
    element.showModal();
    return () => { element.close(); if (previous?.isConnected) previous.focus(); };
  }, [open]);
  return <dialog ref={dialog} className="cm-confirm-dialog" aria-labelledby={titleId} aria-describedby={messageId} aria-busy={loading} onCancel={event => { event.preventDefault(); if (!loading) onCancel(); }} onClick={event => { if (event.target === event.currentTarget && !loading) { const bounds = event.currentTarget.getBoundingClientRect(); if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) onCancel(); } }}>
    <h2 id={titleId}>{title}</h2><p id={messageId}>{message}</p>
    <div className="cm-dialog-actions"><button type="button" className="app-button-secondary" disabled={loading} onClick={onCancel} autoFocus>{cancelLabel}</button><button type="button" className={`${destructive ? 'app-button-primary cm-button-danger' : 'app-button-primary'}`} disabled={loading} onClick={onConfirm}>{loading ? `${confirmLabel}…` : confirmLabel}</button></div>
  </dialog>;
};
export default ConfirmDialog;
