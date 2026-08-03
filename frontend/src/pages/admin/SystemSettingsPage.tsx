import React, { useEffect, useMemo, useState } from 'react';
import { Plus, Rocket, Save, Trash2 } from 'lucide-react';
import AdminLayout from '../../components/AdminLayout';
import { useI18n } from '../../contexts/I18nContext';
import PasswordSettingsSection from '../../components/PasswordSettingsSection';
import {
  systemSettingsService,
  type SystemImageSetting,
} from '../../services/systemSettingsService';
import { runtimePoolService } from '../../services/runtimePoolService';
import type { RuntimePod, RuntimeType } from '../../types/runtimePool';

type ImageRuntimeType = 'desktop' | 'gateway';
type RuntimeGroup = 'lite' | 'pro';

interface RuntimeCardDefinition {
  instance_type: string;
  runtime_type: ImageRuntimeType;
  display_name: string;
  image: string;
}

const LITE_RUNTIME_CARDS: RuntimeCardDefinition[] = [
  {
    instance_type: 'openclaw',
    runtime_type: 'gateway',
    display_name: 'OpenClaw Lite',
    image: 'ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest',
  },
  {
    instance_type: 'hermes',
    runtime_type: 'gateway',
    display_name: 'Hermes Lite',
    image: 'ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite:latest',
  },
];

const PRO_BASE_RUNTIME_CARDS: RuntimeCardDefinition[] = [
  {
    instance_type: 'openclaw',
    runtime_type: 'desktop',
    display_name: 'OpenClaw Pro',
    image: 'ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest',
  },
  {
    instance_type: 'hermes',
    runtime_type: 'desktop',
    display_name: 'Hermes Pro',
    image: 'ghcr.io/yuan-lab-llm/agentsruntime/hermes:latest',
  },
  {
    instance_type: 'workbuddy',
    runtime_type: 'desktop',
    display_name: 'Workbuddy Pro',
    image: 'ghcr.io/yuan-lab-llm/agentsruntime/workbuddy-linux:latest',
  },
];

const PRO_CUSTOM_DEFAULT_IMAGE = 'registry.example.com/your-custom-image:latest';
const FIXED_RUNTIME_CARDS = [...LITE_RUNTIME_CARDS, ...PRO_BASE_RUNTIME_CARDS];

interface EditableImageCard extends SystemImageSetting {
  local_id: string;
  runtime_type: ImageRuntimeType;
  default_image?: string;
  group: RuntimeGroup;
  isBase?: boolean;
  isNew?: boolean;
  saving?: boolean;
  error?: string | null;
}

function getErrorMessage(error: unknown, fallback: string) {
  const responseError = (error as { response?: { data?: { error?: string } } })?.response?.data?.error;
  if (responseError) {
    return responseError;
  }
  return error instanceof Error ? error.message : fallback;
}

function normalizeImageRuntimeType(runtimeType?: string): ImageRuntimeType {
  return runtimeType === 'gateway' || runtimeType === 'shell' ? 'gateway' : 'desktop';
}

function fixedCardKey(card: Pick<SystemImageSetting, 'instance_type' | 'runtime_type'>) {
  return `${card.instance_type}:${normalizeImageRuntimeType(card.runtime_type)}`;
}

function defaultForCard(card: Pick<SystemImageSetting, 'instance_type' | 'runtime_type'>) {
  return FIXED_RUNTIME_CARDS.find((item) => fixedCardKey(item) === fixedCardKey(card));
}

function groupForRuntimeType(runtimeType: ImageRuntimeType): RuntimeGroup {
  return runtimeType === 'gateway' ? 'lite' : 'pro';
}

function runtimePodSeenAt(pod: RuntimePod) {
  const parsed = Date.parse(pod.last_seen_at || pod.updated_at || '');
  return Number.isNaN(parsed) ? 0 : parsed;
}

function resolveCurrentRuntimeImage(pods: RuntimePod[]) {
  const candidates = pods
    .filter((pod) => pod.image_ref?.trim())
    .filter((pod) => !pod.draining && pod.state !== 'deleted')
    .sort((a, b) => runtimePodSeenAt(b) - runtimePodSeenAt(a) || b.id - a.id);
  if (candidates.length === 0) {
    return '';
  }

  const images = Array.from(new Set(candidates.map((pod) => pod.image_ref.trim())));
  return images.join(', ');
}

function toEditableCard(
  item: SystemImageSetting,
  index: number,
  fallback?: RuntimeCardDefinition,
): EditableImageCard {
  const runtimeType = normalizeImageRuntimeType(item.runtime_type);
  const definition = fallback ?? defaultForCard({ ...item, runtime_type: runtimeType });
  return {
    ...item,
    runtime_type: runtimeType,
    display_name: item.display_name || definition?.display_name || 'Custom Pro',
    image: item.image || definition?.image || PRO_CUSTOM_DEFAULT_IMAGE,
    default_image: definition?.image,
    group: groupForRuntimeType(runtimeType),
    isBase: Boolean(definition),
    isNew: !item.id,
    local_id: item.id ? `image-${item.id}` : `image-${item.instance_type}-${runtimeType}-${index}`,
    error: null,
  };
}

function buildRuntimeCards(items: SystemImageSetting[]): EditableImageCard[] {
  const enabledCards = items
    .filter((item) => item.is_enabled !== false)
    .map((item, index) => toEditableCard(item, index));
  const byFixedKey = new Map(enabledCards.map((card) => [fixedCardKey(card), card]));

  const fixedCards = FIXED_RUNTIME_CARDS.map((definition, index) => {
    const existing = byFixedKey.get(fixedCardKey(definition));
    if (existing) {
      return {
        ...existing,
        display_name: definition.display_name,
        default_image: definition.image,
        group: groupForRuntimeType(definition.runtime_type),
        isBase: true,
      };
    }

    return toEditableCard(
      {
        instance_type: definition.instance_type,
        runtime_type: definition.runtime_type,
        display_name: definition.display_name,
        image: definition.image,
        is_enabled: true,
      },
      index,
      definition,
    );
  });

  const customProCards = enabledCards.filter((card) =>
    card.runtime_type === 'desktop' && !defaultForCard(card),
  ).map((card) => ({
    ...card,
    group: 'pro' as const,
    isBase: false,
    default_image: card.default_image ?? PRO_CUSTOM_DEFAULT_IMAGE,
  }));

  return [...fixedCards, ...customProCards];
}

const SystemSettingsPage: React.FC = () => {
  const { t } = useI18n();
  const [cards, setCards] = useState<EditableImageCard[]>([]);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState<string | null>(null);
  const [rolloutRuntimeType, setRolloutRuntimeType] = useState<RuntimeType>('openclaw');
  const [rolloutImage, setRolloutImage] = useState(LITE_RUNTIME_CARDS[0].image);
  const [rolloutCurrentImage, setRolloutCurrentImage] = useState('');
  const [rolloutCurrentLoading, setRolloutCurrentLoading] = useState(false);
  const [rolloutBatchSize, setRolloutBatchSize] = useState(1);
  const [rolloutMaxUnavailable, setRolloutMaxUnavailable] = useState(1);
  const [rolloutSaving, setRolloutSaving] = useState(false);
  const [rolloutError, setRolloutError] = useState<string | null>(null);

  const liteCards = useMemo(
    () => LITE_RUNTIME_CARDS.map((definition) =>
      cards.find((card) => fixedCardKey(card) === fixedCardKey(definition)),
    ).filter((card): card is EditableImageCard => Boolean(card)),
    [cards],
  );

  const proBaseCards = useMemo(
    () => PRO_BASE_RUNTIME_CARDS.map((definition) =>
      cards.find((card) => fixedCardKey(card) === fixedCardKey(definition)),
    ).filter((card): card is EditableImageCard => Boolean(card)),
    [cards],
  );

  const proCustomCards = useMemo(
    () => cards.filter((card) => card.group === 'pro' && !card.isBase),
    [cards],
  );

  const rolloutCard = useMemo(
    () => liteCards.find((card) => card.instance_type === rolloutRuntimeType),
    [liteCards, rolloutRuntimeType],
  );

  useEffect(() => {
    const loadSettings = async () => {
      try {
        setLoading(true);
        setPageError(null);
        const items = await systemSettingsService.getImageSettings();
        const nextCards = buildRuntimeCards(items);
        setCards(nextCards);
        const nextRolloutCard = nextCards.find(
          (card) => card.instance_type === rolloutRuntimeType && card.runtime_type === 'gateway',
        );
        setRolloutImage(nextRolloutCard?.image.trim() || LITE_RUNTIME_CARDS[0].image);
      } catch (error: unknown) {
        setPageError(getErrorMessage(error, t('systemSettingsPage.loadFailed')));
      } finally {
        setLoading(false);
      }
    };

    loadSettings();
  }, [t, rolloutRuntimeType]);

  useEffect(() => {
    let cancelled = false;
    const loadCurrentImage = async () => {
      try {
        setRolloutCurrentLoading(true);
        const pods = await runtimePoolService.listPods(rolloutRuntimeType);
        if (!cancelled) {
          setRolloutCurrentImage(resolveCurrentRuntimeImage(pods));
        }
      } catch {
        if (!cancelled) {
          setRolloutCurrentImage('');
        }
      } finally {
        if (!cancelled) {
          setRolloutCurrentLoading(false);
        }
      }
    };

    void loadCurrentImage();
    return () => {
      cancelled = true;
    };
  }, [rolloutRuntimeType]);

  const refreshRolloutCurrentImage = async (runtimeType: RuntimeType) => {
    try {
      setRolloutCurrentLoading(true);
      const pods = await runtimePoolService.listPods(runtimeType);
      setRolloutCurrentImage(resolveCurrentRuntimeImage(pods));
    } catch {
      setRolloutCurrentImage('');
    } finally {
      setRolloutCurrentLoading(false);
    }
  };

  const updateCard = (localId: string, patch: Partial<EditableImageCard>) => {
    setCards((current) => current.map((card) =>
      card.local_id === localId ? { ...card, ...patch, error: null } : card,
    ));
  };

  const addProCustomCard = () => {
    setCards((current) => [
      ...current,
      {
        local_id: `new-pro-custom-${Date.now()}`,
        instance_type: 'custom',
        runtime_type: 'desktop',
        display_name: 'Custom Pro',
        image: PRO_CUSTOM_DEFAULT_IMAGE,
        default_image: PRO_CUSTOM_DEFAULT_IMAGE,
        group: 'pro',
        isBase: false,
        isNew: true,
        is_enabled: true,
        error: null,
      },
    ]);
  };

  const saveCard = async (card: EditableImageCard) => {
    if (!card.instance_type || !card.image.trim()) {
      updateCard(card.local_id, { error: t('systemSettingsPage.requiredFields') });
      return;
    }

    const normalizedImage = card.image.trim().toLowerCase();
    const duplicate = cards.some((item) =>
      item.local_id !== card.local_id &&
      item.instance_type === card.instance_type &&
      item.runtime_type === card.runtime_type &&
      item.image.trim().toLowerCase() === normalizedImage,
    );
    if (duplicate) {
      updateCard(card.local_id, { error: t('systemSettingsPage.duplicateImage') });
      return;
    }

    updateCard(card.local_id, { saving: true, error: null });

    try {
      const saved = await systemSettingsService.saveImageSetting({
        id: card.id,
        instance_type: card.instance_type,
        runtime_type: card.runtime_type,
        display_name: card.display_name.trim() || (card.isBase ? card.display_name : 'Custom Pro'),
        image: card.image.trim(),
      });
      const nextCard = toEditableCard(
        {
          ...saved,
          runtime_type: normalizeImageRuntimeType(saved.runtime_type),
        },
        0,
        defaultForCard(saved),
      );

      setCards((current) => current.map((item) => item.local_id === card.local_id ? {
        ...item,
        ...nextCard,
        local_id: item.local_id,
        isNew: false,
        saving: false,
        error: null,
      } : item));

      if (nextCard.runtime_type === 'gateway' && nextCard.instance_type === rolloutRuntimeType) {
        setRolloutImage(nextCard.image.trim());
      }
    } catch (error: unknown) {
      updateCard(card.local_id, {
        saving: false,
        error: getErrorMessage(error, t('systemSettingsPage.saveFailed')),
      });
    }
  };

  const deleteCard = async (card: EditableImageCard) => {
    if (card.isBase) {
      return;
    }
    if (card.isNew) {
      setCards((current) => current.filter((item) => item.local_id !== card.local_id));
      return;
    }

    updateCard(card.local_id, { saving: true, error: null });
    try {
      await systemSettingsService.deleteImageSetting(card.id ?? card.instance_type);
      setCards((current) => current.filter((item) => item.local_id !== card.local_id));
    } catch (error: unknown) {
      updateCard(card.local_id, {
        saving: false,
        error: getErrorMessage(error, t('systemSettingsPage.deleteFailed')),
      });
    }
  };

  const handleRolloutRuntimeTypeChange = (runtimeType: RuntimeType) => {
    const nextCard = liteCards.find((card) => card.instance_type === runtimeType);
    setRolloutRuntimeType(runtimeType);
    setRolloutImage(nextCard?.image.trim() || LITE_RUNTIME_CARDS.find((item) => item.instance_type === runtimeType)?.image || '');
    setRolloutError(null);
  };

  const startRollout = async () => {
    if (!rolloutImage.trim()) {
      setRolloutError(t('systemSettingsPage.rolloutTargetRequired'));
      return;
    }

    try {
      setRolloutSaving(true);
      setRolloutError(null);
      await runtimePoolService.startRollout({
        runtime_type: rolloutRuntimeType,
        target_image_ref: rolloutImage.trim(),
        batch_size: Math.max(1, rolloutBatchSize),
        max_unavailable: Math.max(1, rolloutMaxUnavailable),
      });
      setRolloutImage(rolloutCard?.image.trim() || rolloutImage.trim());
      void refreshRolloutCurrentImage(rolloutRuntimeType);
      window.setTimeout(() => {
        void refreshRolloutCurrentImage(rolloutRuntimeType);
      }, 5000);
    } catch (error: unknown) {
      setRolloutError(getErrorMessage(error, t('systemSettingsPage.rolloutFailed')));
    } finally {
      setRolloutSaving(false);
    }
  };

  const renderRuntimeCard = (card: EditableImageCard) => (
    <div key={card.local_id} className="rounded-lg border border-slate-200 bg-white p-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3 className="text-base font-semibold text-gray-900">{card.display_name}</h3>
          <p className="mt-1 text-xs font-medium uppercase tracking-[0.14em] text-gray-500">
            {card.group === 'lite'
              ? t('systemSettingsPage.gatewayMode')
              : t('systemSettingsPage.desktopMode')}
          </p>
        </div>
        {!card.isBase && (
          <span className="inline-flex w-fit rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-600">
            {t('systemSettingsPage.customProBadge')}
          </span>
        )}
      </div>

      {!card.isBase && (
        <div className="mt-4">
          <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.cardTitle')}</label>
          <input
            type="text"
            value={card.display_name}
            onChange={(event) => updateCard(card.local_id, { display_name: event.target.value })}
            className="app-input mt-1 block w-full"
          />
        </div>
      )}

      <div className="mt-4">
        <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.imageAddress')}</label>
        <input
          type="text"
          value={card.image}
          onChange={(event) => updateCard(card.local_id, { image: event.target.value })}
          placeholder={card.default_image}
          className="app-input mt-1 block w-full"
        />
      </div>

      <p className="mt-3 break-all text-xs text-gray-500">
        {t('systemSettingsPage.defaultImage')}: <span className="font-mono">{card.default_image ?? PRO_CUSTOM_DEFAULT_IMAGE}</span>
      </p>

      {card.error && (
        <div className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {card.error}
        </div>
      )}

      <div className="mt-4 flex items-center justify-end gap-3">
        {!card.isBase && (
          <button
            type="button"
            onClick={() => void deleteCard(card)}
            disabled={card.saving}
            className="app-button-secondary inline-flex items-center gap-2 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Trash2 className="h-4 w-4" />
            {t('common.delete')}
          </button>
        )}
        <button
          type="button"
          onClick={() => void saveCard(card)}
          disabled={card.saving}
          className="app-button-primary inline-flex items-center gap-2 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Save className="h-4 w-4" />
          {card.saving ? t('modelManagementPage.saving') : t('common.save')}
        </button>
      </div>
    </div>
  );

  return (
    <AdminLayout title={t('admin.systemSettings')}>
      <div className="space-y-6">
        <PasswordSettingsSection />

        <section className="app-panel p-6">
          <div className="flex flex-col gap-1">
            <h2 className="text-xl font-semibold text-gray-900">{t('systemSettingsPage.liteRolloutTitle')}</h2>
            <p className="text-sm text-gray-500">{t('systemSettingsPage.liteRolloutSubtitle')}</p>
          </div>
          <div className="mt-5 grid grid-cols-1 gap-4 lg:grid-cols-[minmax(180px,240px)_1fr] xl:grid-cols-[minmax(180px,240px)_minmax(260px,1fr)_minmax(320px,1.4fr)_120px_140px_auto] xl:items-end">
            <div>
              <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.liteRuntime')}</label>
              <select
                value={rolloutRuntimeType}
                onChange={(event) => handleRolloutRuntimeTypeChange(event.target.value as RuntimeType)}
                className="app-input mt-1 block w-full"
              >
                {LITE_RUNTIME_CARDS.map((option) => (
                  <option key={option.instance_type} value={option.instance_type}>
                    {option.display_name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.currentGatewayImage')}</label>
              <div className="mt-1 min-h-10 break-all rounded-md border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-sm text-slate-700">
                {rolloutCurrentLoading ? t('common.loading') : rolloutCurrentImage || 'N/A'}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.targetGatewayImage')}</label>
              <input
                type="text"
                value={rolloutImage}
                onChange={(event) => setRolloutImage(event.target.value)}
                className="app-input mt-1 block w-full"
                placeholder={rolloutCard?.default_image}
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.rolloutBatch')}</label>
              <input
                type="number"
                min={1}
                value={rolloutBatchSize}
                onChange={(event) => setRolloutBatchSize(Number(event.target.value) || 1)}
                className="app-input mt-1 block w-full"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">{t('systemSettingsPage.rolloutUnavailable')}</label>
              <input
                type="number"
                min={1}
                value={rolloutMaxUnavailable}
                onChange={(event) => setRolloutMaxUnavailable(Number(event.target.value) || 1)}
                className="app-input mt-1 block w-full"
              />
            </div>
            <button
              type="button"
              onClick={() => void startRollout()}
              disabled={rolloutSaving}
              className="app-button-primary inline-flex items-center justify-center gap-2 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Rocket className="h-4 w-4" />
              {rolloutSaving ? t('systemSettingsPage.rolloutStarting') : t('systemSettingsPage.startRollout')}
            </button>
          </div>
          {rolloutError && (
            <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {rolloutError}
            </div>
          )}
        </section>

        <section className="app-panel p-6">
          <div>
            <h2 className="text-xl font-semibold text-gray-900">{t('systemSettingsPage.liteRuntimeTitle')}</h2>
            <p className="mt-1 text-sm text-gray-500">{t('systemSettingsPage.liteRuntimeSubtitle')}</p>
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
              {liteCards.map(renderRuntimeCard)}
            </div>
          )}
        </section>

        <section className="app-panel p-6">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-xl font-semibold text-gray-900">{t('systemSettingsPage.proRuntimeTitle')}</h2>
              <p className="mt-1 text-sm text-gray-500">{t('systemSettingsPage.proRuntimeSubtitle')}</p>
            </div>
            <button
              type="button"
              onClick={addProCustomCard}
              className="app-button-primary inline-flex items-center gap-2"
            >
              <Plus className="h-4 w-4" />
              {t('systemSettingsPage.addProCustomCard')}
            </button>
          </div>
          {loading ? (
            <div className="mt-6 text-sm text-gray-500">{t('common.loading')}</div>
          ) : (
            <div className="mt-6 grid grid-cols-1 gap-4 xl:grid-cols-2">
              {[...proBaseCards, ...proCustomCards].map(renderRuntimeCard)}
            </div>
          )}
        </section>
      </div>
    </AdminLayout>
  );
};

export default SystemSettingsPage;
