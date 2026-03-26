import React from 'react';
import { useI18n } from '../contexts/I18nContext';

const LanguageSwitcher: React.FC = () => {
  const { locale, setLocale, t, localeOptions } = useI18n();

  return (
    <label className="grid w-full grid-cols-[auto,minmax(0,1fr)] items-center gap-3 text-sm text-gray-600">
      <span className="whitespace-nowrap">{t('common.language')}</span>
      <select
        value={locale}
        onChange={(e) => setLocale(e.target.value as typeof locale)}
        className="min-w-0 w-full rounded-md border border-gray-300 bg-white px-2 py-1 text-sm text-gray-700 focus:outline-none focus:ring-2 focus:ring-indigo-500"
      >
        {localeOptions.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
};

export default LanguageSwitcher;
