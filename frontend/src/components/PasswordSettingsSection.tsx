import React from 'react';
import { useI18n } from '../contexts/I18nContext';
import ChangePasswordModal from './ChangePasswordModal';

const PasswordSettingsSection: React.FC = () => {
  const { t } = useI18n();
  const [open, setOpen] = React.useState(false);

  return (
    <>
      <div className="app-panel p-6">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold text-[var(--cm-ink)]">{t('auth.changePassword')}</h2>
            <p className="mt-1 text-sm text-[var(--cm-muted)]">{t('auth.changePasswordDesc')}</p>
          </div>
          <button
            onClick={() => setOpen(true)}
            className="app-button-primary"
          >
            {t('auth.changePasswordAction')}
          </button>
        </div>
      </div>
      <ChangePasswordModal open={open} onClose={() => setOpen(false)} />
    </>
  );
};

export default PasswordSettingsSection;
