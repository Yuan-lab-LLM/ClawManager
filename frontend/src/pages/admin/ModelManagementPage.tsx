import React, { useEffect, useMemo, useRef, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import {
  modelService,
  type DiscoveredProviderModel,
  type LLMModel,
} from '../../services/modelService';
import { useI18n } from '../../contexts/I18nContext';
import {
  BUILTIN_PROVIDER_TEMPLATES,
  findProviderTemplate,
  normalizeBaseUrl,
  type ProviderTemplate,
} from '../../lib/modelProviderTemplates';

const PROVIDER_TYPE_LABELS: Record<string, string> = {
  'openai-compatible': 'OpenAI Compatible',
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google',
  'azure-openai': 'Azure OpenAI',
  local: 'Local / Internal',
};

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
  provider_type: '',
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

  const matchedTemplate = findProviderTemplate(card.provider_type, card.base_url);
  if (
    matchedTemplate?.requiresApiKey &&
    !card.api_key?.trim() &&
    !card.api_key_secret_ref?.trim()
  ) {
    return false;
  }

  return true;
}

function getProviderTypeLabel(providerType: string) {
  return PROVIDER_TYPE_LABELS[providerType] ?? providerType;
}

function buildCurrentTemplate(card: Pick<EditableModel, 'provider_type' | 'base_url'>, label: string): ProviderTemplate | undefined {
  const providerType = card.provider_type.trim().toLowerCase();
  const baseUrl = card.base_url.trim();
  if (!providerType || !baseUrl) {
    return undefined;
  }

  return {
    id: `current-${providerType}-${normalizeBaseUrl(baseUrl)}`,
    label,
    providerType,
    baseUrl,
    allowCustomBaseUrl: providerType === 'local',
    keywords: ['current', 'custom', 'existing', providerType, baseUrl],
  };
}

function getTemplateOptions(card: EditableModel, currentLabel: string) {
  const matchedTemplate = findProviderTemplate(card.provider_type, card.base_url);
  if (matchedTemplate) {
    return BUILTIN_PROVIDER_TEMPLATES;
  }

  const currentTemplate = buildCurrentTemplate(card, currentLabel);
  return currentTemplate ? [currentTemplate, ...BUILTIN_PROVIDER_TEMPLATES] : BUILTIN_PROVIDER_TEMPLATES;
}

function getSelectedTemplate(card: EditableModel, currentLabel: string) {
  return (
    findProviderTemplate(card.provider_type, card.base_url) ??
    buildCurrentTemplate(card, currentLabel)
  );
}

const TEMPLATE_ICON_META: Record<string, { src: string; glyph: string; className: string }> = {
  openai: {
    src: '/vendor-icons/openai.png',
    glyph: 'OA',
    className: 'border-[#cfe7dc] bg-[#eff8f3] text-[#0f7a5c]',
  },
  openrouter: {
    src: '/vendor-icons/openrouter.ico',
    glyph: 'OR',
    className: 'border-[#d9d7f5] bg-[#f3f1ff] text-[#5847b7]',
  },
  deepseek: {
    src: '/vendor-icons/deepseek.ico',
    glyph: 'DS',
    className: 'border-[#cfe1fb] bg-[#eff6ff] text-[#2f6bb2]',
  },
  siliconflow: {
    src: '/vendor-icons/siliconflow.ico',
    glyph: 'SF',
    className: 'border-[#d4ebef] bg-[#eefbfd] text-[#21708a]',
  },
  moonshot: {
    src: '/vendor-icons/moonshot.ico',
    glyph: 'KM',
    className: 'border-[#e3d7f7] bg-[#f7f1ff] text-[#7148b0]',
  },
  zhipu: {
    src: '/vendor-icons/zhipu.png',
    glyph: 'GL',
    className: 'border-[#d4e6df] bg-[#eef8f4] text-[#1e7a5e]',
  },
  dashscope: {
    src: '/vendor-icons/dashscope.png',
    glyph: 'QW',
    className: 'border-[#f0ddbf] bg-[#fff8ea] text-[#9b6a11]',
  },
  ark: {
    src: '/vendor-icons/ark.png',
    glyph: 'DB',
    className: 'border-[#f5d5cc] bg-[#fff3ef] text-[#b14d2c]',
  },
  groq: {
    src: '/vendor-icons/groq.ico',
    glyph: 'GQ',
    className: 'border-[#d9d9d9] bg-[#f4f4f4] text-[#3e3e3e]',
  },
  together: {
    src: '/vendor-icons/together.png',
    glyph: 'TG',
    className: 'border-[#d4e6ff] bg-[#eef6ff] text-[#2a5fa6]',
  },
  fireworks: {
    src: '/vendor-icons/fireworks.ico',
    glyph: 'FW',
    className: 'border-[#f6d6c4] bg-[#fff2ea] text-[#c05621]',
  },
  xai: {
    src: '/vendor-icons/xai.ico',
    glyph: 'xA',
    className: 'border-[#ddd7f3] bg-[#f3f0ff] text-[#5b4bb4]',
  },
  perplexity: {
    src: '/vendor-icons/perplexity.ico',
    glyph: 'PX',
    className: 'border-[#d4eee7] bg-[#ecfaf5] text-[#0d8265]',
  },
  lingyiwanwu: {
    src: '/vendor-icons/lingyiwanwu.ico',
    glyph: '01',
    className: 'border-[#ebd3f4] bg-[#faf0ff] text-[#9244b5]',
  },
  minimax: {
    src: '/vendor-icons/minimax.ico',
    glyph: 'MM',
    className: 'border-[#f1ddd4] bg-[#fff5f0] text-[#af5f3d]',
  },
  ollama: {
    src: '/vendor-icons/local-internal.svg',
    glyph: 'LC',
    className: 'border-[#d9dfef] bg-[#f2f5fb] text-[#49618d]',
  },
};

function resolveTemplateIconMeta(template?: ProviderTemplate) {
  if (!template) {
    return {
      src: '/vendor-icons/custom-endpoint.svg',
      glyph: 'AI',
      className: 'border-[#eadfd8] bg-white text-[#7c5a4d]',
    };
  }

  if (template.id.startsWith('current-')) {
    return {
      src: '/vendor-icons/custom-endpoint.svg',
      glyph: 'CU',
      className: 'border-[#eadfd8] bg-[#fff7f3] text-[#7c5a4d]',
    };
  }

  return TEMPLATE_ICON_META[template.id] ?? {
    src: '/vendor-icons/custom-endpoint.svg',
    glyph: template.label.slice(0, 2).toUpperCase(),
    className: 'border-[#eadfd8] bg-white text-[#7c5a4d]',
  };
}

interface VendorTemplateIconProps {
  template?: ProviderTemplate;
  size?: 'sm' | 'md';
}

const VendorTemplateIcon: React.FC<VendorTemplateIconProps> = ({ template, size = 'md' }) => {
  const meta = resolveTemplateIconMeta(template);
  const [imageFailed, setImageFailed] = useState(false);
  const sizeClass = size === 'sm'
    ? 'h-9 w-9 rounded-xl text-[11px]'
    : 'h-11 w-11 rounded-2xl text-xs';

  useEffect(() => {
    setImageFailed(false);
  }, [meta.src]);

  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center border font-semibold uppercase tracking-[0.12em] ${sizeClass} ${meta.className}`}
      aria-hidden="true"
    >
      {!imageFailed ? (
        <img
          src={meta.src}
          alt=""
          className="h-[72%] w-[72%] object-contain"
          onError={() => setImageFailed(true)}
        />
      ) : (
        meta.glyph
      )}
    </span>
  );
};

interface ProviderTemplatePickerProps {
  selectedTemplate?: ProviderTemplate;
  options: ProviderTemplate[];
  placeholder: string;
  searchPlaceholder: string;
  emptyText: string;
  getTypeLabel: (providerType: string) => string;
  onSelect: (template: ProviderTemplate) => void;
}

const ProviderTemplatePicker: React.FC<ProviderTemplatePickerProps> = ({
  selectedTemplate,
  options,
  placeholder,
  searchPlaceholder,
  emptyText,
  getTypeLabel,
  onSelect,
}) => {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const containerRef = useRef<HTMLDivElement | null>(null);

  const filteredOptions = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    if (!normalizedQuery) {
      return options;
    }

    return options.filter((template) => {
      const haystack = [
        template.label,
        template.providerType,
        template.baseUrl,
        ...(template.keywords ?? []),
      ].join(' ').toLowerCase();
      return haystack.includes(normalizedQuery);
    });
  }, [options, query]);

  useEffect(() => {
    if (!open) {
      setQuery('');
      return undefined;
    }

    const handlePointerDown = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false);
      }
    };

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [open]);

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        className="app-input mt-1 flex min-h-[56px] w-full items-center justify-between gap-4 text-left hover:border-[#ef6b4a]"
      >
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <VendorTemplateIcon template={selectedTemplate} size="sm" />
          {selectedTemplate ? (
            <div className="min-w-0 flex flex-wrap items-center gap-2">
              <span className="text-sm font-semibold text-gray-900">{selectedTemplate.label}</span>
              <span className="rounded-full border border-[#eadfd8] bg-[#fff7f3] px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.12em] text-[#8f5a47]">
                {getTypeLabel(selectedTemplate.providerType)}
              </span>
            </div>
          ) : (
            <span className="text-sm text-gray-400">{placeholder}</span>
          )}
        </div>
        <svg
          viewBox="0 0 20 20"
          fill="none"
          aria-hidden="true"
          className={`h-5 w-5 shrink-0 text-[#8f5a47] transition-transform ${open ? 'rotate-180' : ''}`}
        >
          <path d="M5 7.5 10 12.5 15 7.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {open && (
        <div className="absolute left-0 right-0 z-20 mt-2 overflow-hidden rounded-[26px] border border-[#ead8cf] bg-white shadow-[0_30px_60px_-36px_rgba(72,44,24,0.45)]">
          <div className="border-b border-[#f1e3db] p-3">
            <input
              type="text"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="app-input block w-full"
              placeholder={searchPlaceholder}
              autoFocus
            />
          </div>

          <div className="max-h-80 overflow-y-auto p-2">
            {filteredOptions.length === 0 ? (
              <div className="px-4 py-6 text-sm text-gray-500">{emptyText}</div>
            ) : (
              filteredOptions.map((template) => {
                const isActive = selectedTemplate?.id === template.id;
                return (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => {
                      onSelect(template);
                      setOpen(false);
                    }}
                    className={`w-full rounded-[22px] px-4 py-3 text-left transition-colors ${
                      isActive
                        ? 'bg-[#fff2eb] text-[#7c3f28]'
                        : 'text-gray-700 hover:bg-[#fff8f4]'
                    }`}
                  >
                    <div className="flex items-start gap-3">
                      <VendorTemplateIcon template={template} />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-3">
                          <span className="text-sm font-semibold">{template.label}</span>
                          <span className="rounded-full border border-[#eadfd8] bg-white px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.12em] text-[#8f5a47]">
                            {getTypeLabel(template.providerType)}
                          </span>
                        </div>
                        <div className="mt-2 break-all text-xs text-gray-500">{template.baseUrl}</div>
                      </div>
                    </div>
                  </button>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
};

const ModelManagementPage: React.FC = () => {
  const { t } = useI18n();
  const [models, setModels] = useState<EditableModel[]>([]);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState<string | null>(null);
  const discoveryTimersRef = useRef<Record<string, number>>({});
  const newCardRef = useRef<HTMLDivElement | null>(null);
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

  useEffect(() => {
    if (!editingCard?.isNew || !newCardRef.current) {
      return;
    }

    newCardRef.current.scrollIntoView({ behavior: 'smooth', block: 'start' });
    const firstInput = newCardRef.current.querySelector('input[type="text"]');
    if (firstInput instanceof HTMLInputElement) {
      firstInput.focus();
    }
  }, [editingCard?.isNew, editingCard?.local_id]);

  const addCard = () => {
    if (hasEditingCard) {
      return;
    }
    const nextCard = createEmptyModel();
    setModels((current) => [nextCard, ...current]);
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
        next.discovering = false;
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

        if (buildDiscoveryKey(item) !== nextKey) {
          return item;
        }

        const nextProviderModelName = item.provider_model_name.trim()
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
          ? buildDiscoveryKey(item) === nextKey
            ? {
                ...item,
                discovering: false,
                discovery_error: error.response?.data?.error || t('modelManagementPage.discoverFailed'),
                discovered_models: [],
                discovery_key: nextKey,
              }
            : item
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
                const currentTemplate = getSelectedTemplate(card, t('modelManagementPage.currentVendorTemplate'));
                const templateOptions = getTemplateOptions(card, t('modelManagementPage.currentVendorTemplate'));
                const providerHeadline = currentTemplate?.label || getProviderTypeLabel(card.provider_type);
                const providerTypeLabel = getProviderTypeLabel(card.provider_type || currentTemplate?.providerType || '');
                const allowBaseUrlEditing = Boolean(currentTemplate?.allowCustomBaseUrl);
                const providerModelListId = `provider-model-options-${card.local_id}`;
                const providerModelHelpText = card.discovering
                  ? t('modelManagementPage.loadingProviderModels')
                  : autoDiscoverySupported
                    ? discoveredModels.length > 0
                      ? t('modelManagementPage.discoveryHelp')
                      : card.discovery_error
                        ? t('modelManagementPage.manualFallbackHelp')
                        : canDiscover(card)
                          ? t('modelManagementPage.waitingDiscovery')
                          : t('modelManagementPage.discoveryCredentialsHelp')
                    : t('modelManagementPage.manualEntryHelp');

                if (!card.isEditing) {
                  return (
                    <div
                      key={card.local_id}
                      className="self-start rounded-[26px] border border-[#ead8cf] bg-[rgba(255,248,245,0.84)] p-5 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)]"
                    >
                      <div className="flex items-start justify-between gap-4">
                        <div className="flex min-w-0 items-start gap-3">
                          <VendorTemplateIcon template={currentTemplate} />
                          <div className="min-w-0">
                            <h3 className="text-lg font-semibold text-gray-900">{card.display_name}</h3>
                            <p className="mt-1 text-sm text-gray-500">{providerHeadline || '-'}</p>
                            {providerTypeLabel && providerTypeLabel !== providerHeadline && (
                              <p className="mt-1 text-xs uppercase tracking-[0.16em] text-[#a78374]">
                                {providerTypeLabel}
                              </p>
                            )}
                          </div>
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
                    ref={card.isNew ? newCardRef : undefined}
                    className="self-start rounded-[26px] border border-[#ead8cf] bg-[rgba(255,248,245,0.84)] p-5 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)]"
                  >
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.displayName')}</label>
                        <input
                          type="text"
                          value={card.display_name}
                          onChange={(event) => updateCard(card.local_id, { display_name: event.target.value })}
                          className="app-input mt-1 block min-h-[56px] w-full"
                          placeholder={t('modelManagementPage.displayNamePlaceholder')}
                        />
                      </div>

                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.vendorTemplate')}</label>
                        <ProviderTemplatePicker
                          selectedTemplate={currentTemplate}
                          options={templateOptions}
                          placeholder={t('modelManagementPage.vendorTemplatePlaceholder')}
                          searchPlaceholder={t('modelManagementPage.vendorTemplateSearchPlaceholder')}
                          emptyText={t('modelManagementPage.vendorTemplateEmpty')}
                          getTypeLabel={getProviderTypeLabel}
                          onSelect={(template) => updateCard(card.local_id, {
                            provider_type: template.providerType,
                            base_url: template.baseUrl,
                          })}
                        />
                      </div>
                    </div>

                    <div className="mt-4">
                      <div className="flex items-center justify-between gap-3">
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.baseUrl')}</label>
                        {currentTemplate && (
                          <span className="rounded-full border border-[#eadfd8] bg-[#fff7f3] px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.12em] text-[#8f5a47]">
                            {getProviderTypeLabel(currentTemplate.providerType)}
                          </span>
                        )}
                      </div>
                      {allowBaseUrlEditing ? (
                        <>
                          <input
                            type="text"
                            value={card.base_url}
                            onChange={(event) => updateCard(card.local_id, { base_url: event.target.value })}
                            className="app-input mt-1 block w-full"
                            placeholder={currentTemplate?.baseUrl || t('modelManagementPage.baseUrlPlaceholder')}
                          />
                          <p className="mt-2 text-xs text-gray-500">{t('modelManagementPage.editableBaseUrlHelp')}</p>
                        </>
                      ) : (
                        <>
                          <div className="mt-1 rounded-[22px] border border-[#e5d9d1] bg-white/90 px-4 py-4 text-sm text-[#171212] shadow-[0_10px_24px_-20px_rgba(72,44,24,0.45)]">
                            {currentTemplate ? (
                              <span className="break-all">{currentTemplate.baseUrl}</span>
                            ) : (
                              <span className="text-gray-400">{t('modelManagementPage.fixedBaseUrlPlaceholder')}</span>
                            )}
                          </div>
                          <p className="mt-2 text-xs text-gray-500">{t('modelManagementPage.fixedBaseUrlHelp')}</p>
                        </>
                      )}
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

                      <input
                        type="text"
                        list={autoDiscoverySupported && discoveredModels.length > 0 ? providerModelListId : undefined}
                        value={card.provider_model_name}
                        onChange={(event) => updateCard(card.local_id, { provider_model_name: event.target.value })}
                        className="app-input mt-1 block w-full"
                        placeholder={discoveredModels.length > 0
                          ? t('modelManagementPage.selectProviderModel')
                          : t('modelManagementPage.manualProviderModelPlaceholder')}
                      />

                      {autoDiscoverySupported && discoveredModels.length > 0 && (
                        <datalist id={providerModelListId}>
                          {discoveredModels.map((model) => (
                            <option key={model.id} value={model.id}>
                              {model.display_name}
                            </option>
                          ))}
                        </datalist>
                      )}

                      <div className="mt-2 text-xs text-gray-500">
                        {providerModelHelpText}
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
