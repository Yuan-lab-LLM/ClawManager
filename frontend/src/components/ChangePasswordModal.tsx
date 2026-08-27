import React, { useState } from 'react';
import { createPortal } from 'react-dom';
import { authService } from '../services/authService';
import { useI18n } from '../contexts/I18nContext';
import { localizeAuthError } from '../utils/authErrors';

interface ChangePasswordModalProps {
  open: boolean;
  onClose: () => void;
}

const ChangePasswordModal: React.FC<ChangePasswordModalProps> = ({ open, onClose }) => {
  const { t } = useI18n();
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  if (!open) {
    return null;
  }

  const resetState = () => {
    setCurrentPassword('');
    setNewPassword('');
    setConfirmPassword('');
    setError(null);
    setSuccess(null);
    setLoading(false);
  };

  const handleClose = () => {
    resetState();
    onClose();
  };

  const handleSubmit = async () => {
    if (newPassword.length < 8) {
      setError(t('auth.passwordTooShort'));
      return;
    }
    if (newPassword !== confirmPassword) {
      setError(t('auth.passwordsMismatch'));
      return;
    }

    try {
      setLoading(true);
      setError(null);
      await authService.changePassword({
        current_password: currentPassword,
        new_password: newPassword,
      });
      setSuccess(t('auth.passwordChanged'));
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (err: any) {
      const backendError =
        err.response?.data?.error ||
        err.response?.data?.message ||
        err.message;
      setError(backendError ? localizeAuthError(backendError, t) : t('auth.changePasswordFailed'));
    } finally {
      setLoading(false);
    }
  };

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(23,18,18,0.34)] px-4 backdrop-blur-sm">
      <div className="app-panel-warm w-full max-w-md p-6">
        <div className="flex items-center justify-between">
          <h3 className="text-lg font-semibold text-[#171212]">{t('auth.changePassword')}</h3>
          <button onClick={handleClose} className="text-[#8f8681] hover:text-[#171212]">
            ×
          </button>
        </div>

        <div className="mt-4 space-y-4">
          {error && <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}
          {success && <div className="rounded-xl border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-700">{success}</div>}

          <div>
            <label className="block text-sm font-medium text-gray-700">{t('auth.currentPassword')}</label>
            <input
              type="password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              className="app-input mt-1 block w-full"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700">{t('auth.newPassword')}</label>
            <input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              className="app-input mt-1 block w-full"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700">{t('auth.confirmNewPassword')}</label>
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className="app-input mt-1 block w-full"
            />
          </div>
        </div>

        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={handleClose}
            className="app-button-secondary"
          >
            {t('common.cancel')}
          </button>
          <button
            onClick={handleSubmit}
            disabled={loading}
            className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
          >
            {loading ? t('auth.changingPassword') : t('auth.changePasswordAction')}
          </button>
        </div>
      </div>
    </div>,
    document.body
  );
};

export default ChangePasswordModal;
