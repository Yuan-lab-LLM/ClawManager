import React, { useEffect, useMemo, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import { useI18n } from '../../contexts/I18nContext';
import PasswordSettingsSection from '../../components/PasswordSettingsSection';
import {
  systemSettingsService,
  type SystemImageSetting,
} from '../../services/systemSettingsService';

const IMAGE_TYPE_OPTIONS = [
  { value: 'openclaw', label: 'OpenClaw Desktop', defaultImage: 'ericpearlee/openclaw:v2026.3.13' },
  { value: 'ubuntu', label: 'Ubuntu Desktop', defaultImage: 'lscr.io/linuxserver/webtop:ubuntu-xfce' },
  { value: 'webtop', label: 'Webtop Desktop', defaultImage: 'lscr.io/linuxserver/webtop:ubuntu-xfce' },
  { value: 'debian', label: 'Debian Desktop', defaultImage: 'docker.io/clawreef/debian-desktop:12' },
  { value: 'centos', label: 'CentOS Desktop', defaultImage: 'docker.io/clawreef/centos-desktop:9' },
  { value: 'custom', label: 'Custom Image', defaultImage: 'registry.example.com/your-custom-image:latest' },
];

interface EditableImageCard extends SystemImageSetting {
  local_id: string;
  isNew?: boolean;
  saving?: boolean;
  error?: string | null;
}

const SystemSettingsPage: React.FC = () => {
  const { t } = useI18n();
  const [cards, setCards] = useState<EditableImageCard[]>([]);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState<string | null>(null);

  const usedTypes = useMemo(
    () => cards.map((card) => card.instance_type).filter(Boolean),
    [cards],
  );

  useEffect(() => {
    const loadSettings = async () => {
      try {
        setLoading(true);
        setPageError(null);
        const items = await systemSettingsService.getImageSettings();
        setCards(items.filter((item) => item.is_enabled !== false).map((item, index) => ({
          ...item,
          local_id: `${item.instance_type}-${index}`,
          error: null,
        })));
      } catch (error: any) {
        setPageError(error.response?.data?.error || t('systemSettingsPage.loadFailed'));
      } finally {
        setLoading(false);
      }
    };

    loadSettings();
  }, []);

  const addCard = () => {
    const nextType = IMAGE_TYPE_OPTIONS.find((option) => !usedTypes.includes(option.value));
    setCards((current) => [
      ...current,
      {
        local_id: `new-${Date.now()}`,
        instance_type: nextType?.value ?? 'ubuntu',
        display_name: nextType?.label ?? 'Ubuntu Desktop',
        image: nextType?.defaultImage ?? '',
        isNew: true,
        is_enabled: true,
        error: null,
      },
    ]);
  };

  const updateCard = (localId: string, patch: Partial<EditableImageCard>) => {
    setCards((current) => current.map((card) => {
      if (card.local_id !== localId) {
        return card;
      }

      const next = { ...card, ...patch, error: null };
      if (patch.instance_type) {
        const option = IMAGE_TYPE_OPTIONS.find((item) => item.value === patch.instance_type);
        if (option) {
          next.display_name = option.label;
          if (card.isNew && (!card.image || card.image === card.display_name)) {
            next.image = option.defaultImage;
          }
        }
      }
      return next;
    }));
  };

  const saveCard = async (card: EditableImageCard) => {
    if (!card.instance_type || !card.image.trim()) {
      updateCard(card.local_id, { error: t('systemSettingsPage.requiredFields') });
      return;
    }

    const duplicate = cards.some((item) => item.local_id !== card.local_id && item.instance_type === card.instance_type);
    if (duplicate) {
      updateCard(card.local_id, { error: t('systemSettingsPage.duplicateType') });
      return;
    }

    updateCard(card.local_id, { saving: true, error: null });

    try {
      const saved = await systemSettingsService.saveImageSetting({
        instance_type: card.instance_type,
        display_name: card.display_name,
        image: card.image.trim(),
      });

      setCards((current) => current.map((item) => item.local_id === card.local_id ? {
        ...item,
        ...saved,
        local_id: item.local_id,
        isNew: false,
        saving: false,
        error: null,
      } : item));
    } catch (error: any) {
      updateCard(card.local_id, {
        saving: false,
        error: error.response?.data?.error || t('systemSettingsPage.saveFailed'),
      });
    }
  };

  const deleteCard = async (card: EditableImageCard) => {
    if (card.isNew) {
      setCards((current) => current.filter((item) => item.local_id !== card.local_id));
      return;
    }

    updateCard(card.local_id, { saving: true, error: null });
    try {
      await systemSettingsService.deleteImageSetting(card.instance_type);
      setCards((current) => current.filter((item) => item.local_id !== card.local_id));
    } catch (error: any) {
      updateCard(card.local_id, {
        saving: false,
        error: error.response?.data?.error || t('systemSettingsPage.deleteFailed'),
      });
    }
  };

  return (
    <AdminLayout title={t('admin.systemSettings')}>
      <div className="space-y-6">
        <PasswordSettingsSection />
        <section className="app-panel p-6">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-xl font-semibold text-gray-900">{t('systemSettingsPage.runtimeImageCards')}</h2>
              <p className="mt-1 text-sm text-gray-500">
                {t('systemSettingsPage.runtimeImageCardsSubtitle')}
              </p>
            </div>
            <button
              type="button"
              onClick={addCard}
              className="app-button-primary"
            >
              {t('systemSettingsPage.addCard')}
            </button>
          </div>

          {pageError && (
            <div className="mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {pageError}
            </div>
          )}

          {loading ? (
            <div className="mt-6 text-sm text-gray-500">{t('common.loading')}</div>
          ) : (
            <div className="mt-6 grid grid-cols-1 gap-4 xl:grid-cols-2">
              {cards.map((card) => {
                const selectableTypes = IMAGE_TYPE_OPTIONS.filter((option) => (
                  option.value === card.instance_type || !usedTypes.includes(option.value)
                ));

                const defaultImage = IMAGE_TYPE_OPTIONS.find((option) => option.value === card.instance_type)?.defaultImage ?? '-';

                return (
                  <div key={card.local_id} className="rounded-[26px] border border-[#ead8cf] bg-[rgba(255,248,245,0.84)] p-5 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)]">
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.instanceType')}</label>
                        <select
                          value={card.instance_type}
                          onChange={(event) => updateCard(card.local_id, { instance_type: event.target.value })}
                          className="app-input mt-1 block w-full"
                        >
                          {selectableTypes.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.cardTitle')}</label>
                        <input
                          type="text"
                          value={card.display_name}
                          onChange={(event) => updateCard(card.local_id, { display_name: event.target.value })}
                          className="app-input mt-1 block w-full"
                        />
                      </div>
                    </div>

                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.imageAddress')}</label>
                      <input
                        type="text"
                        value={card.image}
                        onChange={(event) => updateCard(card.local_id, { image: event.target.value })}
                        placeholder={defaultImage}
                        className="app-input mt-1 block w-full"
                      />
                    </div>

                    <p className="mt-3 text-xs text-gray-500">
                      {t('systemSettingsPage.defaultImage')}: <span className="font-mono">{defaultImage}</span>
                    </p>

                    {card.error && (
                      <div className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                        {card.error}
                      </div>
                    )}

                    <div className="mt-4 flex items-center justify-end gap-3">
                      <button
                        type="button"
                        onClick={() => deleteCard(card)}
                        disabled={card.saving}
                        className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {t('common.delete')}
                      </button>
                      <button
                        type="button"
                        onClick={() => saveCard(card)}
                        disabled={card.saving}
                        className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {card.saving ? t('modelManagementPage.saving') : t('common.save')}
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          {!loading && cards.length === 0 && (
            <div className="mt-6 rounded-[24px] border border-dashed border-[#ead8cf] bg-[rgba(255,248,245,0.72)] px-6 py-10 text-center text-sm text-gray-500">
              {t('systemSettingsPage.empty')}
            </div>
          )}
        </section>

      </div>
    </AdminLayout>
  );
};

export default SystemSettingsPage;
