import React, { useEffect, useRef, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import {
  modelService,
  type DiscoveredProviderModel,
  type LLMModel,
} from '../../services/modelService';
import { useI18n } from '../../contexts/I18nContext';

const PROVIDER_OPTIONS = [
  { value: 'openai-compatible', label: 'OpenAI Compatible' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'google', label: 'Google' },
  { value: 'azure-openai', label: 'Azure OpenAI' },
  { value: 'local', label: 'Local / Internal' },
];

interface EditableModel extends LLMModel {
  local_id: string;
  isNew?: boolean;
  isEditing?: boolean;
  saving?: boolean;
  error?: string | null;
  discovering?: boolean;
  discovery_error?: string | null;
  discovered_models?: DiscoveredProviderModel[];
  discovery_key?: string;
  edit_snapshot?: EditableModelSnapshot;
}

type EditableModelSnapshot = Pick<
  EditableModel,
  | 'display_name'
  | 'description'
  | 'provider_type'
  | 'base_url'
  | 'provider_model_name'
  | 'api_key'
  | 'api_key_secret_ref'
  | 'is_secure'
  | 'is_active'
  | 'input_price'
  | 'output_price'
  | 'currency'
  | 'discovered_models'
  | 'discovery_key'
  | 'discovery_error'
>;

const captureSnapshot = (card: EditableModel): EditableModelSnapshot => ({
  display_name: card.display_name,
  description: card.description,
  provider_type: card.provider_type,
  base_url: card.base_url,
  provider_model_name: card.provider_model_name,
  api_key: card.api_key,
  api_key_secret_ref: card.api_key_secret_ref,
  is_secure: card.is_secure,
  is_active: card.is_active,
  input_price: card.input_price,
  output_price: card.output_price,
  currency: card.currency,
  discovered_models: card.discovered_models ?? [],
  discovery_key: card.discovery_key,
  discovery_error: card.discovery_error,
});

const createEmptyModel = (): EditableModel => ({
  local_id: `new-${Date.now()}-${Math.random().toString(16).slice(2)}`,
  display_name: '',
  description: '',
  provider_type: 'openai-compatible',
  base_url: '',
  provider_model_name: '',
  api_key: '',
  api_key_secret_ref: '',
  is_secure: false,
  is_active: true,
  input_price: 0,
  output_price: 0,
  currency: 'USD',
  isNew: true,
  isEditing: true,
  error: null,
  discovery_error: null,
  discovered_models: [],
});

const AUTO_DISCOVERY_PROVIDERS = new Set([
  'openai-compatible',
  'openai',
  'local',
  'anthropic',
  'google',
]);

function buildDiscoveryKey(card: EditableModel) {
  return [
    card.provider_type,
    card.base_url.trim(),
    card.api_key?.trim() ?? '',
    card.api_key_secret_ref?.trim() ?? '',
  ].join('|');
}

function canDiscover(card: EditableModel) {
  if (!AUTO_DISCOVERY_PROVIDERS.has(card.provider_type)) {
    return false;
  }

  if (!card.base_url.trim()) {
    return false;
  }

  return true;
}

const ModelManagementPage: React.FC = () => {
  const { t } = useI18n();
  const [models, setModels] = useState<EditableModel[]>([]);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState<string | null>(null);
  const discoveryTimersRef = useRef<Record<string, number>>({});
  const editingCard = models.find((item) => item.isEditing);
  const hasEditingCard = Boolean(editingCard);

  useEffect(() => {
    const loadModels = async () => {
      try {
        setLoading(true);
        setPageError(null);
        const items = await modelService.getModels();
        setModels(items.map((item, index) => ({
          ...item,
          description: item.description ?? '',
          api_key: item.api_key ?? '',
          api_key_secret_ref: item.api_key_secret_ref ?? '',
          local_id: `${item.id ?? item.display_name}-${index}`,
          isEditing: false,
          error: null,
          discovery_error: null,
          discovered_models: [],
          edit_snapshot: undefined,
        })));
      } catch (error: any) {
        setPageError(error.response?.data?.error || t('modelManagementPage.loadFailed'));
      } finally {
        setLoading(false);
      }
    };

    loadModels();
  }, []);

  useEffect(() => {
    Object.values(discoveryTimersRef.current).forEach((timer) => window.clearTimeout(timer));
    discoveryTimersRef.current = {};

    models.forEach((card) => {
      if (!canDiscover(card)) {
        return;
      }

      const nextKey = buildDiscoveryKey(card);
      if (card.discovery_key === nextKey || card.discovering) {
        return;
      }

      discoveryTimersRef.current[card.local_id] = window.setTimeout(() => {
        void discoverModels(card.local_id, false);
      }, 700);
    });

    return () => {
      Object.values(discoveryTimersRef.current).forEach((timer) => window.clearTimeout(timer));
      discoveryTimersRef.current = {};
    };
  }, [models]);

  const addCard = () => {
    if (hasEditingCard) {
      return;
    }
    setModels((current) => [...current, createEmptyModel()]);
  };

  const updateCard = (localId: string, patch: Partial<EditableModel>) => {
    setModels((current) => current.map((card) => {
      if (card.local_id !== localId) {
        return card;
      }

      const next = { ...card, ...patch, error: patch.error ?? card.error };
      if (
        Object.prototype.hasOwnProperty.call(patch, 'provider_type') ||
        Object.prototype.hasOwnProperty.call(patch, 'base_url') ||
        Object.prototype.hasOwnProperty.call(patch, 'api_key') ||
        Object.prototype.hasOwnProperty.call(patch, 'api_key_secret_ref')
      ) {
        next.discovery_error = null;
        next.discovery_key = undefined;
        next.discovered_models = [];
        if (
          Object.prototype.hasOwnProperty.call(patch, 'provider_type') ||
          Object.prototype.hasOwnProperty.call(patch, 'base_url')
        ) {
          next.provider_model_name = '';
        }
      }
      return next;
    }));
  };

  const startEditing = (card: EditableModel) => {
    if (hasEditingCard && !card.isEditing) {
      return;
    }
    updateCard(card.local_id, {
      isEditing: true,
      error: null,
      edit_snapshot: captureSnapshot(card),
    });
  };

  const cancelEditing = (card: EditableModel) => {
    if (card.isNew || !card.id) {
      setModels((current) => current.filter((item) => item.local_id !== card.local_id));
      return;
    }

    const snapshot = card.edit_snapshot ?? captureSnapshot(card);
    updateCard(card.local_id, {
      ...snapshot,
      isEditing: false,
      saving: false,
      error: null,
      discovering: false,
      edit_snapshot: undefined,
    });
  };

  const discoverModels = async (localId: string, force: boolean) => {
    const snapshot = models.find((item) => item.local_id === localId);
    if (!snapshot || !canDiscover(snapshot)) {
      return;
    }

    const nextKey = buildDiscoveryKey(snapshot);
    if (!force && snapshot.discovery_key === nextKey) {
      return;
    }

    updateCard(localId, { discovering: true, discovery_error: null });

    try {
      const discovered = await modelService.discoverModels({
        provider_type: snapshot.provider_type,
        base_url: snapshot.base_url.trim(),
        api_key: snapshot.api_key?.trim() || undefined,
        api_key_secret_ref: snapshot.api_key_secret_ref?.trim() || undefined,
      });

      setModels((current) => current.map((item) => {
        if (item.local_id !== localId) {
          return item;
        }

        const shouldKeepSelection = discovered.some((model) => model.id === item.provider_model_name);
        const nextProviderModelName = shouldKeepSelection
          ? item.provider_model_name
          : discovered[0]?.id ?? '';

        return {
          ...item,
          discovering: false,
              discovery_error: discovered.length === 0 ? t('modelManagementPage.noProviderModels') : null,
          discovered_models: discovered,
          discovery_key: nextKey,
          provider_model_name: nextProviderModelName,
        };
      }));
    } catch (error: any) {
      setModels((current) => current.map((item) => (
        item.local_id === localId
          ? {
              ...item,
              discovering: false,
              discovery_error: error.response?.data?.error || t('modelManagementPage.discoverFailed'),
              discovered_models: [],
              discovery_key: nextKey,
            }
          : item
      )));
    }
  };

  const saveCard = async (card: EditableModel) => {
    if (!card.display_name.trim() || !card.provider_type || !card.base_url.trim() || !card.provider_model_name.trim()) {
      updateCard(card.local_id, { error: t('modelManagementPage.requiredFields') });
      return;
    }

    updateCard(card.local_id, { saving: true, error: null });

    try {
      const saved = await modelService.saveModel({
        id: card.id,
        display_name: card.display_name.trim(),
        description: card.description?.trim() || undefined,
        provider_type: card.provider_type,
        base_url: card.base_url.trim(),
        provider_model_name: card.provider_model_name.trim(),
        api_key: card.api_key?.trim() || undefined,
        api_key_secret_ref: card.api_key_secret_ref?.trim() || undefined,
        is_secure: card.is_secure,
        is_active: card.is_active,
        input_price: Number(card.input_price) || 0,
        output_price: Number(card.output_price) || 0,
        currency: card.currency.trim() || 'USD',
      });

      setModels((current) => current.map((item) => (
        item.local_id === card.local_id
          ? {
              ...item,
              ...saved,
              description: saved.description ?? '',
              api_key: saved.api_key ?? '',
              api_key_secret_ref: saved.api_key_secret_ref ?? '',
              isNew: false,
              isEditing: false,
              saving: false,
              error: null,
              edit_snapshot: undefined,
            }
          : item
      )));
    } catch (error: any) {
      updateCard(card.local_id, {
        saving: false,
        error: error.response?.data?.error || t('modelManagementPage.saveFailed'),
      });
    }
  };

  const deleteCard = async (card: EditableModel) => {
    if (!card.id) {
      setModels((current) => current.filter((item) => item.local_id !== card.local_id));
      return;
    }

    updateCard(card.local_id, { saving: true, error: null });
    try {
      await modelService.deleteModel(card.id);
      setModels((current) => current.filter((item) => item.local_id !== card.local_id));
    } catch (error: any) {
      updateCard(card.local_id, {
        saving: false,
        error: error.response?.data?.error || t('modelManagementPage.deleteFailed'),
      });
    }
  };

  return (
    <AdminLayout title={t('nav.models')}>
      <div className="space-y-6">
        <section className="app-panel p-6">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-xl font-semibold text-gray-900">{t('modelManagementPage.title')}</h2>
              <p className="mt-1 text-sm text-gray-500">
                {t('modelManagementPage.subtitle')}
              </p>
            </div>
            <button
              type="button"
              onClick={addCard}
              disabled={hasEditingCard}
              className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {t('modelManagementPage.addModel')}
            </button>
          </div>

          {hasEditingCard && (
            <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
              {t('modelManagementPage.finishEditing')}
            </div>
          )}

          {pageError && (
            <div className="mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {pageError}
            </div>
          )}

          {loading ? (
            <div className="mt-6 text-sm text-gray-500">{t('modelManagementPage.loading')}</div>
          ) : (
            <div className="mt-6 grid grid-cols-1 items-start gap-5 xl:grid-cols-2">
              {models.map((card) => {
                const autoDiscoverySupported = AUTO_DISCOVERY_PROVIDERS.has(card.provider_type);
                const discoveredModels = card.discovered_models ?? [];

                if (!card.isEditing) {
                  return (
                    <div
                      key={card.local_id}
                      className="self-start rounded-[26px] border border-[#ead8cf] bg-[rgba(255,248,245,0.84)] p-5 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)]"
                    >
                      <div className="flex items-start justify-between gap-4">
                        <div>
                          <h3 className="text-lg font-semibold text-gray-900">{card.display_name}</h3>
                          <p className="mt-1 text-sm text-gray-500">
                            {PROVIDER_OPTIONS.find((option) => option.value === card.provider_type)?.label ?? card.provider_type}
                          </p>
                        </div>
                        <div className="flex items-center gap-2 text-xs text-gray-500">
                          {card.is_secure && (
                            <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-emerald-700">
                              {t('modelManagementPage.secure')}
                            </span>
                          )}
                          <span className={`rounded-full px-2.5 py-1 ${card.is_active ? 'border border-[#d9ead3] bg-[#f3fff0] text-[#2f6b2f]' : 'border border-[#eadfd8] bg-white text-[#7b6f6a]'}`}>
                            {card.is_active ? t('modelManagementPage.active') : t('modelManagementPage.inactive')}
                          </span>
                        </div>
                      </div>

                      <dl className="mt-5 grid grid-cols-1 gap-4 text-sm sm:grid-cols-2">
                        <div>
                          <dt className="font-medium text-gray-700">{t('modelManagementPage.providerModel')}</dt>
                          <dd className="mt-1 text-gray-600">{card.provider_model_name || '-'}</dd>
                        </div>
                        <div>
                          <dt className="font-medium text-gray-700">{t('modelManagementPage.currency')}</dt>
                          <dd className="mt-1 text-gray-600">{card.currency || '-'}</dd>
                        </div>
                        <div className="sm:col-span-2">
                          <dt className="font-medium text-gray-700">{t('modelManagementPage.baseUrl')}</dt>
                          <dd className="mt-1 break-all text-gray-600">{card.base_url || '-'}</dd>
                        </div>
                        <div>
                          <dt className="font-medium text-gray-700">{t('modelManagementPage.inputPriceShort')}</dt>
                          <dd className="mt-1 text-gray-600">{card.input_price}</dd>
                        </div>
                        <div>
                          <dt className="font-medium text-gray-700">{t('modelManagementPage.outputPriceShort')}</dt>
                          <dd className="mt-1 text-gray-600">{card.output_price}</dd>
                        </div>
                        <div className="sm:col-span-2">
                          <dt className="font-medium text-gray-700">{t('common.description')}</dt>
                          <dd className="mt-1 whitespace-pre-wrap text-gray-600">{card.description || '-'}</dd>
                        </div>
                      </dl>

                      <div className="mt-5 flex items-center justify-end gap-3">
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
                          onClick={() => startEditing(card)}
                          disabled={hasEditingCard}
                          className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {t('modelManagementPage.edit')}
                        </button>
                      </div>
                    </div>
                  );
                }

                return (
                  <div
                    key={card.local_id}
                    className="self-start rounded-[26px] border border-[#ead8cf] bg-[rgba(255,248,245,0.84)] p-5 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)]"
                  >
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.displayName')}</label>
                        <input
                          type="text"
                          value={card.display_name}
                          onChange={(event) => updateCard(card.local_id, { display_name: event.target.value })}
                          className="app-input mt-1 block w-full"
                          placeholder={t('modelManagementPage.displayNamePlaceholder')}
                        />
                      </div>

                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.providerType')}</label>
                        <select
                          value={card.provider_type}
                          onChange={(event) => updateCard(card.local_id, { provider_type: event.target.value })}
                          className="app-input mt-1 block w-full"
                        >
                          {PROVIDER_OPTIONS.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </div>
                    </div>

                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.baseUrl')}</label>
                      <input
                        type="text"
                        value={card.base_url}
                        onChange={(event) => updateCard(card.local_id, { base_url: event.target.value })}
                        className="app-input mt-1 block w-full"
                        placeholder={t('modelManagementPage.baseUrlPlaceholder')}
                      />
                    </div>

                    <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.apiKey')}</label>
                        <input
                          type="password"
                          value={card.api_key}
                          onChange={(event) => updateCard(card.local_id, { api_key: event.target.value })}
                          className="app-input mt-1 block w-full"
                          placeholder={t('modelManagementPage.optionalPlaceholder')}
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.secretRef')}</label>
                      <input
                        type="text"
                        value={card.api_key_secret_ref}
                        onChange={(event) => updateCard(card.local_id, { api_key_secret_ref: event.target.value })}
                        className="app-input mt-1 block w-full"
                        placeholder={t('modelManagementPage.secretRefPlaceholder')}
                      />
                    </div>
                  </div>

                    <div className="mt-4">
                      <div className="flex items-center justify-between gap-3">
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.providerModel')}</label>
                        <button
                          type="button"
                          onClick={() => void discoverModels(card.local_id, true)}
                          disabled={!canDiscover(card) || card.discovering}
                          className="rounded-xl border border-[#ead8cf] bg-white px-3 py-1 text-xs font-medium text-[#7c5a4d] hover:bg-[#fff5f0] disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {card.discovering ? t('common.loading') : t('common.refresh')}
                        </button>
                      </div>

                      {autoDiscoverySupported ? (
                        <select
                          value={card.provider_model_name}
                          onChange={(event) => updateCard(card.local_id, { provider_model_name: event.target.value })}
                          className="app-input mt-1 block w-full"
                          disabled={card.discovering || discoveredModels.length === 0}
                        >
                          <option value="">
                            {card.discovering
                              ? t('modelManagementPage.loadingProviderModels')
                              : discoveredModels.length > 0
                                ? t('modelManagementPage.selectProviderModel')
                                : t('modelManagementPage.waitingDiscovery')}
                          </option>
                          {discoveredModels.map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <input
                          type="text"
                          value={card.provider_model_name}
                          onChange={(event) => updateCard(card.local_id, { provider_model_name: event.target.value })}
                          className="app-input mt-1 block w-full"
                          placeholder={t('modelManagementPage.manualProviderModelPlaceholder')}
                        />
                      )}

                      <div className="mt-2 text-xs text-gray-500">
                        {autoDiscoverySupported
                          ? t('modelManagementPage.discoveryHelp')
                          : t('modelManagementPage.manualEntryHelp')}
                      </div>

                      {card.discovery_error && (
                        <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
                          {card.discovery_error}
                        </div>
                      )}
                    </div>

                    <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.currency')}</label>
                        <input
                          type="text"
                          value={card.currency}
                          onChange={(event) => updateCard(card.local_id, { currency: event.target.value.toUpperCase() })}
                          className="app-input mt-1 block w-full"
                          placeholder={t('modelManagementPage.currencyPlaceholder')}
                        />
                      </div>
                    </div>

                    <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.inputPrice')}</label>
                        <input
                          type="number"
                          min="0"
                          step="0.000001"
                          value={card.input_price}
                          onChange={(event) => updateCard(card.local_id, { input_price: Number(event.target.value) })}
                          className="app-input mt-1 block w-full"
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.outputPrice')}</label>
                        <input
                          type="number"
                          min="0"
                          step="0.000001"
                          value={card.output_price}
                          onChange={(event) => updateCard(card.local_id, { output_price: Number(event.target.value) })}
                          className="app-input mt-1 block w-full"
                        />
                      </div>
                    </div>

                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700">{t('common.description')}</label>
                      <textarea
                        value={card.description}
                        onChange={(event) => updateCard(card.local_id, { description: event.target.value })}
                        rows={3}
                        className="app-input mt-1 block w-full"
                        placeholder={t('modelManagementPage.descriptionPlaceholder')}
                      />
                    </div>

                    <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:gap-6">
                      <label className="inline-flex items-center gap-2 text-sm text-gray-700">
                        <input
                          type="checkbox"
                          checked={card.is_secure}
                          onChange={(event) => updateCard(card.local_id, { is_secure: event.target.checked })}
                          className="h-4 w-4 rounded border-gray-300 text-[#b84c28] focus:ring-[#b84c28]"
                        />
                        {t('modelManagementPage.secureModel')}
                      </label>

                      <label className="inline-flex items-center gap-2 text-sm text-gray-700">
                        <input
                          type="checkbox"
                          checked={card.is_active}
                          onChange={(event) => updateCard(card.local_id, { is_active: event.target.checked })}
                          className="h-4 w-4 rounded border-gray-300 text-[#b84c28] focus:ring-[#b84c28]"
                        />
                        {t('modelManagementPage.active')}
                      </label>
                    </div>

                    {card.error && (
                      <div className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                        {card.error}
                      </div>
                    )}

                    <div className="mt-4 flex items-center justify-between gap-3">
                      <div className="flex items-center gap-2 text-xs text-gray-500">
                        {card.is_secure && (
                          <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-emerald-700">
                            {t('modelManagementPage.secure')}
                          </span>
                        )}
                        <span className={`rounded-full px-2.5 py-1 ${card.is_active ? 'border border-[#d9ead3] bg-[#f3fff0] text-[#2f6b2f]' : 'border border-[#eadfd8] bg-white text-[#7b6f6a]'}`}>
                          {card.is_active ? t('modelManagementPage.active') : t('modelManagementPage.inactive')}
                        </span>
                      </div>

                      <div className="flex items-center gap-3">
                        <button
                          type="button"
                          onClick={() => cancelEditing(card)}
                          disabled={card.saving}
                          className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {t('common.cancel')}
                        </button>
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
                  </div>
                );
              })}
            </div>
          )}

          {!loading && models.length === 0 && (
            <div className="mt-6 rounded-[24px] border border-dashed border-[#ead8cf] bg-[rgba(255,248,245,0.72)] px-6 py-10 text-center text-sm text-gray-500">
              {t('modelManagementPage.empty')}
            </div>
          )}
        </section>

      </div>
    </AdminLayout>
  );
};

export default ModelManagementPage;
