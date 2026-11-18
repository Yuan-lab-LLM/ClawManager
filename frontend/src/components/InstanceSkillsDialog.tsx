import { useEffect, useId, useRef, useState } from 'react';
import { KeyRound, X } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import type { Instance, InstanceRuntimeDetails } from '../types/instance';
import InstanceSkillHubPanel from './InstanceSkillHubPanel';
import './instance-workspace.css';

export default function InstanceSkillsDialog({ instance, onRuntimeDetailsChange, iconOnly = false }: { instance: Instance; onRuntimeDetailsChange: (details: InstanceRuntimeDetails) => void; iconOnly?: boolean }) {
  const { t } = useI18n();
  const title = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const element = dialog.current;
    if (!open || !element) return;
    const previous = document.activeElement as HTMLElement | null;
    element.showModal();
    return () => { element.close(); if (previous?.isConnected) previous.focus(); };
  }, [open]);
  return <>
    <button type="button" className={iconOnly ? "cm-icon-button" : "app-button-secondary"} aria-label={t('instances.skillManagement')} title={t('instances.skillManagement')} aria-haspopup="dialog" onClick={() => setOpen(true)}><KeyRound className="h-4 w-4" />{!iconOnly && t('instances.skillManagement')}</button>
    <dialog ref={dialog} className="cm-confirm-dialog cm-instance-skills-dialog" aria-labelledby={title} onCancel={event => { event.preventDefault(); setOpen(false); }}>
      <div className="cm-instance-skills-heading"><h2 id={title}>{t('instances.skillManagement')}</h2><button type="button" className="cm-icon-button" aria-label={t('common.close')} onClick={() => setOpen(false)} autoFocus><X size={20} /></button></div>
      <InstanceSkillHubPanel instance={instance} onRuntimeDetailsChange={onRuntimeDetailsChange} embedded active={open} />
    </dialog>
  </>;
}
