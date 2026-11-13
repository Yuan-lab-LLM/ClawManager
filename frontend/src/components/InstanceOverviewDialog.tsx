import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { SlidersHorizontal, X } from "lucide-react";
import { useI18n } from "../contexts/I18nContext";
import type { Instance } from "../types/instance";
import InstanceListPage from "../pages/instances/InstanceListPage";
import { instanceWorkspaceText } from "./instanceWorkspaceText";
import "./instance-overview-dialog.css";

interface InstanceOverviewDialogProps {
  onSelectInstance: (instance: Instance) => void;
  initialSearch?: string;
  initialOpen?: boolean;
  renderTrigger?: (open: () => void) => ReactNode;
}

export default function InstanceOverviewDialog({ onSelectInstance, initialSearch, initialOpen = false, renderTrigger }: InstanceOverviewDialogProps) {
  const { t, locale } = useI18n();
  const [open, setOpen] = useState(initialOpen);
  const dialog = useRef<HTMLDialogElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const dialogId = useId();
  const manageLabel = instanceWorkspaceText(locale).manage;

  useEffect(() => {
    const element = dialog.current;
    if (!open || !element) return;
    const returnTrigger = trigger.current;
    const previous = document.activeElement as HTMLElement | null;
    element.showModal();
    return () => {
      element.close();
      if (returnTrigger?.isConnected) returnTrigger.focus();
      else if (previous?.isConnected) previous.focus();
    };
  }, [open]);

  return <>
    {renderTrigger ? renderTrigger(() => setOpen(true)) : <button ref={trigger} type="button" className="app-button-secondary" title={manageLabel} aria-haspopup="dialog" aria-expanded={open} aria-controls={dialogId} onClick={() => setOpen(true)}>
      <SlidersHorizontal size={16} aria-hidden="true" />{manageLabel}
    </button>}
    <dialog ref={dialog} id={dialogId} className="cm-confirm-dialog cm-instance-overview-dialog" aria-labelledby={titleId}
      onCancel={event => {
        if (event.target !== event.currentTarget) return;
        event.preventDefault();
        setOpen(false);
      }}
      onClose={event => { if (event.target === event.currentTarget && !event.currentTarget.open) setOpen(false); }}>
      <div className="cm-instance-overview-header">
        <h2 id={titleId}>{t("instances.listTitle")}</h2>
        <button type="button" className="cm-icon-button" title={t("common.close")} aria-label={t("common.close")} autoFocus onClick={() => setOpen(false)}><X size={18} aria-hidden="true" /></button>
      </div>
      <div className="cm-instance-overview-body">
        {open && <InstanceListPage embedded initialSearch={initialSearch} onSelectInstance={instance => { setOpen(false); onSelectInstance(instance); }} />}
      </div>
    </dialog>
  </>;
}
