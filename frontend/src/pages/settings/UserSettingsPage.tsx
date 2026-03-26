import React from 'react';
import UserLayout from '../../components/UserLayout';
import PasswordSettingsSection from '../../components/PasswordSettingsSection';
import { useI18n } from '../../contexts/I18nContext';

const UserSettingsPage: React.FC = () => {
  const { t } = useI18n();

  return (
    <UserLayout title={t('nav.settings')}>
      <div>
        <PasswordSettingsSection />
      </div>
    </UserLayout>
  );
};

export default UserSettingsPage;
