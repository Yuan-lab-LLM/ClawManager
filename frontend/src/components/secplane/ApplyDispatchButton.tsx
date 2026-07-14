import React, { useState } from 'react';
import DispatchPickerModal from './DispatchPickerModal';
import { useI18n } from '../../contexts/I18nContext';

// Trigger button + DispatchPickerModal in one. Replaces the prior
// "Apply to All Instances" buttons that called dispatchAegisApply() with no args.
// The picker shows a checklist of instances (with running / unhealthy
// badges so the user can see the state), then dispatches to the chosen
// subset or to all.

export interface ApplyDispatchButtonProps {
  // Called with the selected instance ids, or null to dispatch to all.
  onDispatch: (instanceIds: number[] | null) => Promise<unknown> | void;
  busy?: boolean;
  className?: string;
  triggerLabel?: string;
  busyLabel?: string;
  modalTitle?: string;
  modalHint?: string;
  disabled?: boolean;
}

const ApplyDispatchButton: React.FC<ApplyDispatchButtonProps> = ({
  onDispatch,
  busy = false,
  className = 'btn-primary',
  triggerLabel,
  busyLabel,
  modalTitle,
  modalHint,
  disabled = false,
}) => {
  const { t } = useI18n();
  const _triggerLabel = triggerLabel ?? t('secplane.runtime.applyButton.defaultLabel');
  const _busyLabel = busyLabel ?? t('secplane.runtime.applyButton.defaultBusyLabel');
  const [open, setOpen] = useState(false);

  const handleDispatch = async (ids: number[] | null) => {
    try {
      await onDispatch(ids);
    } finally {
      setOpen(false);
    }
  };

  return (
    <>
      <button
        type="button"
        className={className}
        onClick={() => setOpen(true)}
        disabled={busy || disabled}
      >
        {busy ? _busyLabel : _triggerLabel}
      </button>
      <DispatchPickerModal
        open={open}
        onClose={() => setOpen(false)}
        onDispatch={handleDispatch}
        dispatching={busy}
        title={modalTitle}
        hint={modalHint}
      />
    </>
  );
};

export default ApplyDispatchButton;
