import React, { useEffect, useMemo, useRef, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import ModelCatalog from '../../components/ModelCatalog';
import ModelDiscoveryPlan, { type DiscoveryPlanPatch } from '../../components/ModelDiscoveryPlan';
import { mergeDiscoveredPlan, modelRoutingPlanRows, plannedModelKind, plannedReasoningEnabled } from '../../lib/modelRoutingPlan';
import { selectedRows, mergeBatchSave } from '../../lib/modelSelection';
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
  resolveProviderProtocolType,
  supportsManagedReasoningControl,
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

const PROTOCOL_TYPE_LABELS: Record<string, string> = {
  'openai-compatible': 'OpenAI Compatible',
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google',
  'azure-openai': 'Azure OpenAI',
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
  plan_excluded_ids?: string[];
  edit_snapshot?: EditableModelSnapshot;
}

type EditableModelSnapshot = Pick<
  EditableModel,
  | 'visibility_scope'
  | 'allowed_user_ids'
  | 'access_policy_version'
  | 'display_name'
  | 'description'
  | 'provider_type'
  | 'protocol_type'
  | 'model_kind'
  | 'image_generation_enabled'
  | 'image_edit_enabled'
  | 'image_price_per_image'
  | 'reset_image_price'
  | 'model_group'
  | 'deployment_priority'
  | 'max_concurrent_requests'
  | 'context_window_tokens'
  | 'quality_tier'
  | 'base_url'
  | 'provider_model_name'
  | 'reasoning_enabled'
  | 'reasoning_control_mode'
  | 'supports_tools'
  | 'tools_support_override'
  | 'supports_vision'
  | 'vision_support_override'
  | 'reset_vision_support_override'
  | 'capability_source'
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
  | 'plan_excluded_ids'
>;

const captureSnapshot = (card: EditableModel): EditableModelSnapshot => ({
  visibility_scope: card.visibility_scope,
  allowed_user_ids: [...(card.allowed_user_ids ?? [])],
  access_policy_version: card.access_policy_version,
  display_name: card.display_name,
  description: card.description,
  provider_type: card.provider_type,
  protocol_type: card.protocol_type,
  model_kind: card.model_kind,
  image_generation_enabled: card.image_generation_enabled,
  image_edit_enabled: card.image_edit_enabled,
  image_price_per_image: card.image_price_per_image,
  reset_image_price: card.reset_image_price,
  model_group: card.model_group,
  deployment_priority: card.deployment_priority,
  max_concurrent_requests: card.max_concurrent_requests,
  context_window_tokens: card.context_window_tokens,
  quality_tier: card.quality_tier,
  base_url: card.base_url,
  provider_model_name: card.provider_model_name,
  reasoning_enabled: card.reasoning_enabled,
  reasoning_control_mode: card.reasoning_control_mode,
  supports_tools: card.supports_tools,
  tools_support_override: card.tools_support_override,
  supports_vision: card.supports_vision,
  vision_support_override: card.vision_support_override,
  reset_vision_support_override: card.reset_vision_support_override,
  capability_source: card.capability_source,
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
  plan_excluded_ids: [...(card.plan_excluded_ids ?? [])],
});

const createEmptyModel = (): EditableModel => ({
  visibility_scope: 'all',
  allowed_user_ids: [],
  local_id: `new-${Date.now()}-${Math.random().toString(16).slice(2)}`,
  display_name: '',
  description: '',
  provider_type: '',
  protocol_type: '',
  model_group: '',
  deployment_priority: 100,
  max_concurrent_requests: 0,
  context_window_tokens: 0,
  quality_tier: 0,
  base_url: '',
  provider_model_name: '',
  reasoning_enabled: false,
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
    resolveProviderProtocolType(card.provider_type, card.protocol_type),
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

function uniqueImportedDisplayName(base: string, baseURL: string, used: Set<string>) {
  const normalized = base.trim() || 'model';
  if (!used.has(normalized)) {
    used.add(normalized);
    return normalized;
  }
  let host = 'provider';
  try {
    host = new URL(baseURL).hostname || host;
  } catch {
    // Keep the deterministic fallback when the URL is still being edited.
  }
  let candidate = `${normalized} (${host})`;
  let suffix = 2;
  while (used.has(candidate)) {
    candidate = `${normalized} (${host} ${suffix})`;
    suffix += 1;
  }
  used.add(candidate);
  return candidate;
}

function getProviderTypeLabel(providerType: string) {
  return PROVIDER_TYPE_LABELS[providerType] ?? providerType;
}

function getProtocolTypeLabel(protocolType: string) {
  return PROTOCOL_TYPE_LABELS[protocolType] ?? protocolType;
}

function buildCurrentTemplate(card: Pick<EditableModel, 'provider_type' | 'protocol_type' | 'base_url'>, label: string): ProviderTemplate | undefined {
  const providerType = card.provider_type.trim().toLowerCase();
  const baseUrl = card.base_url.trim();
  if (!providerType || !baseUrl) {
    return undefined;
  }

  return {
    id: `current-${providerType}-${normalizeBaseUrl(baseUrl)}`,
    label,
    providerType,
    protocolType: resolveProviderProtocolType(providerType, card.protocol_type),
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
  anthropic: {
    src: '/vendor-icons/anthropic.svg',
    glyph: 'AN',
    className: 'border-[#efd8c8] bg-[#fff5ed] text-[#8a4b2b]',
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
  'minimax-anthropic': {
    src: '/vendor-icons/minimax.ico',
    glyph: 'MM',
    className: 'border-[#f1ddd4] bg-[#fff5f0] text-[#af5f3d]',
  },
  'minimax-cn': {
    src: '/vendor-icons/minimax.ico',
    glyph: 'MM',
    className: 'border-[#f1ddd4] bg-[#fff5f0] text-[#af5f3d]',
  },
  'minimax-cn-anthropic': {
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
    ? 'h-6 w-6 rounded-lg text-[11px]'
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
        className="app-input mt-1 flex h-[38px] w-full items-center justify-between gap-4 py-0 text-left hover:border-[#ef6b4a]"
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
  const [bulkSaving, setBulkSaving] = useState(false);
  const discoveryTimersRef = useRef<Record<string, number>>({});
  const newCardRef = useRef<HTMLFieldSetElement | null>(null);
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
          protocol_type: resolveProviderProtocolType(item.provider_type, item.protocol_type),
          api_key: item.api_key ?? '',
          api_key_secret_ref: item.api_key_secret_ref ?? '',
          reasoning_enabled: item.reasoning_enabled ?? false,
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
      if (!card.isEditing || card.saving || bulkSaving || !canDiscover(card)) {
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
  }, [models, bulkSaving]);

  useEffect(() => {
    if (!editingCard || !newCardRef.current) {
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
        Object.prototype.hasOwnProperty.call(patch, 'protocol_type') ||
        Object.prototype.hasOwnProperty.call(patch, 'base_url') ||
        Object.prototype.hasOwnProperty.call(patch, 'api_key') ||
        Object.prototype.hasOwnProperty.call(patch, 'api_key_secret_ref')
      ) {
        next.discovery_error = null;
        next.discovery_key = undefined;
        next.discovered_models = [];
        next.plan_excluded_ids = [];
        next.discovering = false;
        if (
          Object.prototype.hasOwnProperty.call(patch, 'provider_type') ||
          Object.prototype.hasOwnProperty.call(patch, 'protocol_type') ||
          Object.prototype.hasOwnProperty.call(patch, 'base_url')
        ) {
          next.provider_model_name = '';
          next.reasoning_control_mode = '';
          next.reasoning_enabled = false;
          if (next.isNew) {
            next.display_name = '';
            next.model_kind = undefined;
            next.image_generation_enabled = undefined;
            next.image_edit_enabled = undefined;
            next.image_price_per_image = undefined;
            next.reset_image_price = undefined;
          }
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
    // Restore the snapshot directly: updateCard deliberately invalidates model
    // settings when connection fields are supplied, even during restoration.
    setModels((current) => current.map((item) => item.local_id === card.local_id ? {
      ...item,
      ...snapshot,
      isEditing: false,
      saving: false,
      error: null,
      discovering: false,
      edit_snapshot: undefined,
    } : item));
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
        protocol_type: resolveProviderProtocolType(snapshot.provider_type, snapshot.protocol_type),
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
        const initialPlan = item.isNew && !item.model_group
          ? discovered.find((model) => model.id === nextProviderModelName) : undefined;

        return {
          ...item,
          discovering: false,
          discovery_error: discovered.length === 0 ? t('modelManagementPage.noProviderModels') : null,
          discovered_models: mergeDiscoveredPlan(discovered, item.discovery_key === nextKey ? item.discovered_models ?? [] : []),
          discovery_key: nextKey,
          provider_model_name: nextProviderModelName,
          ...(!item.provider_model_name && !item.display_name ? {
            display_name: discovered.find((model) => model.id === nextProviderModelName)?.display_name || nextProviderModelName,
          } : {}),
          ...(item.isNew ? {
            supports_tools: discovered.find((model) => model.id === nextProviderModelName)?.supports_tools,
            supports_vision: discovered.find((model) => model.id === nextProviderModelName)?.supports_vision,
            capability_source: discovered.find((model) => model.id === nextProviderModelName)?.classification_source,
          } : {}),
          ...(initialPlan ? {
            model_kind: item.model_kind ?? plannedModelKind(initialPlan),
            model_group: initialPlan.suggested_pool || 'default',
            quality_tier: initialPlan.suggested_quality_tier ?? 0,
            deployment_priority: initialPlan.suggested_priority ?? 100,
          } : {}),
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
                // Keep same-source drafts; mark this attempt to avoid an
                // automatic retry loop. The refresh button can retry explicitly.
                discovery_key: nextKey,
              }
            : item
          : item
      )));
    }
  };

  const updateDiscoveryPlan = (localId: string, modelId: string, patch: DiscoveryPlanPatch) => {
    setModels((current) => current.map((card) => {
      if (card.local_id !== localId) return card;
      return {
        ...card,
        discovered_models: card.discovered_models?.map((model) => model.id === modelId ? { ...model, ...patch, plan_edited: true } : model),
        ...(card.provider_model_name === modelId ? {
          ...(patch.visibility_scope !== undefined ? { visibility_scope: patch.visibility_scope } : {}),
          ...(patch.allowed_user_ids !== undefined ? { allowed_user_ids: patch.allowed_user_ids } : {}),
          ...(patch.display_name !== undefined ? { display_name: patch.display_name } : {}),
          ...(patch.model_kind !== undefined ? { model_kind: plannedModelKind(patch) } : {}),
          ...(patch.image_generation_enabled !== undefined ? { image_generation_enabled: patch.image_generation_enabled } : {}),
          ...(patch.image_edit_enabled !== undefined ? { image_edit_enabled: patch.image_edit_enabled } : {}),
          ...(patch.image_price_per_image !== undefined ? { image_price_per_image: patch.image_price_per_image, reset_image_price: patch.reset_image_price ?? false } : {}),
          ...(patch.suggested_pool !== undefined ? { model_group: patch.suggested_pool } : {}),
          ...(patch.suggested_quality_tier !== undefined ? { quality_tier: patch.suggested_quality_tier } : {}),
          ...(patch.suggested_priority !== undefined ? { deployment_priority: patch.suggested_priority } : {}),
          ...(patch.tools_support_override != null ? { tools_support_override: patch.tools_support_override } : {}),
          ...(patch.vision_support_override !== undefined ? { vision_support_override: patch.vision_support_override, reset_vision_support_override: false } : {}),
          ...(patch.reset_vision_support_override ? { vision_support_override: null, reset_vision_support_override: true } : {}),
          ...(patch.reasoning_enabled !== undefined ? { reasoning_enabled: patch.reasoning_enabled } : {}),
          ...(patch.reasoning_control_mode !== undefined ? { reasoning_control_mode: patch.reasoning_control_mode } : {}),
        } : {}),
      };
    }));
  };

  const applyDiscoveryPlan = async (localId: string, selectedIds: string[]) => {
    const source = models.find((item) => item.local_id === localId);
    const discovered = source ? selectedRows(modelRoutingPlanRows(source, source.discovered_models ?? []), new Set(selectedIds), (model) => model.id) : [];
    if (!source || source.discovering || source.saving || bulkSaving || discovered.length === 0) {
      return;
    }
    if (discovered.some((model) => model.visibility_scope === 'selected_users' && !model.allowed_user_ids?.length)) {
      updateCard(localId, { error: '请选择至少一位可使用此模型的用户' }); return;
    }
    if (discovered.some((model) => !model.display_name?.trim())) {
      updateCard(localId, { discovery_error: t('modelManagementPage.planNamesRequired') });
      return;
    }
    if (!source.provider_type || !source.base_url.trim() || discovered.some((model) => !model.id.trim())) {
      updateCard(localId, { error: t('modelManagementPage.requiredFields') });
      return;
    }
    const existingKeys = new Set(
      models.filter((item) => !(item.local_id === localId && item.isNew)).map((item) => `${item.base_url.trim()}\n${item.provider_model_name.trim()}`),
    );
    const usedDisplayNames = new Set(models.filter((item) => !(item.local_id === localId && item.isNew)).map((item) => item.display_name.trim()).filter(Boolean));
    const planned = discovered
      .filter((model) => !existingKeys.has(`${source.base_url.trim()}\n${model.id.trim()}`))
      .map((model, index): EditableModel => ({
        ...createEmptyModel(),
        display_name: uniqueImportedDisplayName(
          model.display_name.trim(),
          source.base_url, usedDisplayNames,
        ),
        provider_type: source.provider_type,
        protocol_type: source.protocol_type,
        model_group: model.suggested_pool || "default",
        deployment_priority: model.suggested_priority ?? 50 + index,
        context_window_tokens: model.context_window_tokens ?? 0,
        ...(source.context_window_tokens ? { context_window_tokens: source.context_window_tokens } : {}),
        max_concurrent_requests: source.max_concurrent_requests,
        description: source.description,
        quality_tier: model.suggested_quality_tier ?? 0,
        base_url: source.base_url,
        provider_model_name: model.id,
        model_kind: plannedModelKind(model),
        image_generation_enabled: model.image_generation_enabled ?? false,
        image_edit_enabled: model.image_edit_enabled ?? false,
        image_price_per_image: model.image_price_per_image,
        reset_image_price: model.reset_image_price,
        visibility_scope: model.visibility_scope ?? 'all',
        allowed_user_ids: model.allowed_user_ids ?? [],
        supports_tools: model.supports_tools,
        tools_support_override: model.tools_support_override,
        supports_vision: model.supports_vision,
        vision_support_override: model.vision_support_override,
        reset_vision_support_override: model.reset_vision_support_override,
        capability_source: model.classification_source,
        reasoning_control_mode: model.reasoning_control_mode ?? '',
        reasoning_enabled: plannedModelKind(model) === 'chat' && supportsManagedReasoningControl(
          source.provider_type,
          source.protocol_type,
          source.base_url,
          model.id,
          model.reasoning_control_mode,
        ) && plannedReasoningEnabled(model),
        api_key: source.api_key,
        api_key_secret_ref: source.api_key_secret_ref,
        is_secure: source.is_secure,
        is_active: source.is_active && (plannedModelKind(model) === 'chat' || (plannedModelKind(model) === 'image' && (model.image_generation_enabled === true || model.image_edit_enabled === true))),
        input_price: source.input_price,
        output_price: source.output_price,
        currency: source.currency,
        isNew: true,
        isEditing: false,
        discovered_models: [],
        discovery_key: undefined,
      }));

    if (planned.length === 0) {
      updateCard(localId, { discovery_error: "已存在相同来源和模型，无需重复添加" });
      return;
    }
    setModels((current) => [
      ...planned,
      ...current.filter((item) => !(item.local_id === localId && item.isNew))
        .map((item) => item.local_id === localId ? { ...item, discovery_error: null } : item),
    ]);
    await saveSelectedCards(planned, true);
  };

  const saveCard = async (card: EditableModel) => {
    if (!card.display_name.trim() || !card.provider_type || !card.base_url.trim() || !card.provider_model_name.trim()) {
      updateCard(card.local_id, { error: t('modelManagementPage.requiredFields') });
      return;
    }

    if (card.visibility_scope === 'selected_users' && !card.allowed_user_ids?.length) {
      updateCard(card.local_id, { error: '请选择至少一位可使用此模型的用户' }); return;
    }
    updateCard(card.local_id, { saving: true, error: null });

    try {
      const saved = await modelService.saveModel({
        id: card.id,
        visibility_scope: card.visibility_scope ?? 'all',
        allowed_user_ids: card.allowed_user_ids ?? [],
        access_policy_version: card.access_policy_version,
        display_name: card.display_name.trim(),
        description: card.description?.trim() || undefined,
        provider_type: card.provider_type,
        protocol_type: resolveProviderProtocolType(card.provider_type, card.protocol_type),
        model_kind: card.model_kind,
        image_generation_enabled: card.image_generation_enabled,
        image_edit_enabled: card.image_edit_enabled,
        image_price_per_image: card.image_price_per_image,
        reset_image_price: card.reset_image_price,
        model_group: card.model_group?.trim() || card.display_name.trim(),
        deployment_priority: Number(card.deployment_priority) || 0,
        max_concurrent_requests: Number(card.max_concurrent_requests) || 0,
        context_window_tokens: Number(card.context_window_tokens) || 0,
        quality_tier: Number(card.quality_tier) || 0,
        base_url: card.base_url.trim(),
        provider_model_name: card.provider_model_name.trim(),
        reasoning_control_mode: card.reasoning_control_mode ?? '',
        reasoning_enabled: (card.model_kind ?? 'chat') === 'chat' && supportsManagedReasoningControl(
          card.provider_type,
          card.protocol_type,
          card.base_url,
          card.provider_model_name,
          card.reasoning_control_mode,
        ) && card.reasoning_enabled,
        api_key: card.api_key?.trim() || undefined,
        api_key_secret_ref: card.api_key_secret_ref?.trim() || undefined,
        is_secure: card.is_secure,
        is_active: card.is_active,
        input_price: Number(card.input_price) || 0,
        output_price: Number(card.output_price) || 0,
        currency: card.currency.trim() || 'USD',
        supports_tools: card.supports_tools,
        tools_support_override: card.tools_support_override,
        supports_vision: card.supports_vision,
        vision_support_override: card.vision_support_override,
        reset_vision_support_override: card.reset_vision_support_override,
        capability_source: card.capability_source,
      });

      setModels((current) => current.map((item) => (
        item.local_id === card.local_id
          ? {
              ...item,
              ...saved,
              description: saved.description ?? '',
              api_key: saved.api_key ?? '',
              api_key_secret_ref: saved.api_key_secret_ref ?? '',
              discovered_models: card.discovered_models ?? [],
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

  const saveSelectedCards = async (selection: EditableModel[], fromPlan = false) => {
    const selectedIds = new Set(selection.map((card) => card.local_id));
    // A just-created plan is not in this render's models yet. Submit that exact
    // selection, then reconcile responses by local ID using functional updates.
    const pending = selectedRows(fromPlan ? selection : models, selectedIds, (card) => card.local_id).filter((card) => card.isNew);
    if (pending.length === 0 || bulkSaving || (!fromPlan && hasEditingCard) || models.some((card) => card.saving)) {
      return;
    }
    const invalid = pending.filter((card) => !card.display_name.trim() || !card.provider_type || !card.base_url.trim() || !card.provider_model_name.trim());
    if (invalid.length > 0) {
      const invalidIds = new Set(invalid.map((card) => card.local_id));
      setPageError(t('modelManagementPage.requiredFields'));
      setModels((current) => current.map((card) => invalidIds.has(card.local_id) ? { ...card, error: t('modelManagementPage.requiredFields') } : card));
      return;
    }
    setBulkSaving(true);
    setPageError(null);
    try {
      const payloads = pending.map((card) => ({
        id: card.id,
        visibility_scope: card.visibility_scope ?? 'all',
        allowed_user_ids: card.allowed_user_ids ?? [],
        access_policy_version: card.access_policy_version,
        display_name: card.display_name.trim(),
        description: card.description?.trim() || undefined,
        provider_type: card.provider_type,
        protocol_type: resolveProviderProtocolType(card.provider_type, card.protocol_type),
        model_kind: card.model_kind,
        image_generation_enabled: card.image_generation_enabled,
        image_edit_enabled: card.image_edit_enabled,
        image_price_per_image: card.image_price_per_image,
        reset_image_price: card.reset_image_price,
        model_group: card.model_group?.trim() || card.display_name.trim(),
        deployment_priority: Number(card.deployment_priority) || 0,
        max_concurrent_requests: Number(card.max_concurrent_requests) || 0,
        context_window_tokens: Number(card.context_window_tokens) || 0,
        quality_tier: Number(card.quality_tier) || 0,
        base_url: card.base_url.trim(),
        provider_model_name: card.provider_model_name.trim(),
        reasoning_control_mode: card.reasoning_control_mode ?? '',
        reasoning_enabled: (card.model_kind ?? 'chat') === 'chat' && supportsManagedReasoningControl(card.provider_type, card.protocol_type, card.base_url, card.provider_model_name, card.reasoning_control_mode) && card.reasoning_enabled,
        api_key: card.api_key?.trim() || undefined,
        api_key_secret_ref: card.api_key_secret_ref?.trim() || undefined,
        is_secure: card.is_secure,
        is_active: card.is_active,
        input_price: Number(card.input_price) || 0,
        output_price: Number(card.output_price) || 0,
        currency: card.currency.trim() || 'USD',
        supports_tools: card.supports_tools,
        tools_support_override: card.tools_support_override,
        supports_vision: card.supports_vision,
        vision_support_override: card.vision_support_override,
        reset_vision_support_override: card.reset_vision_support_override,
        capability_source: card.capability_source,
      }));
      const saved: LLMModel[] = [];
      const failed: Array<{ display_name: string; provider_model_name: string; error: string }> = [];
      for (let offset = 0; offset < payloads.length; offset += 200) {
        const chunk = payloads.slice(offset, offset + 200);
        const result = await modelService.saveModelsBatch(chunk);
        saved.push(...result.saved);
        failed.push(...result.failed);
        const chunkIds = new Set(pending.slice(offset, offset + 200).map((card) => card.local_id));
        setModels((current) => mergeBatchSave(current, chunkIds, result, t('modelManagementPage.saveFailed')));
      }
      const result = { saved, failed, saved_count: saved.length, failed_count: failed.length };
      if (result.failed.length > 0) {
        setPageError(`批量保存完成：成功 ${result.saved_count ?? result.saved.length} 个，失败 ${result.failed.length} 个`);
      }
    } catch (error: any) {
      setPageError(error.response?.data?.error || '批量保存中断，已保留已成功保存的模型');
    } finally {
      setBulkSaving(false);
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

  const deleteSelectedCards = async (selection: EditableModel[]) => {
    if (bulkSaving || hasEditingCard || models.some((card) => card.saving)) return;
    const targets = selectedRows(models, new Set(selection.map((card) => card.local_id)), (card) => card.local_id);
    if (targets.length === 0) return;
    const saved = targets.filter((card) => card.id).length;
    if (!window.confirm(t('modelManagementPage.confirmDeleteSelected', { saved, pending: targets.length - saved }))) return;
    setBulkSaving(true);
    setPageError(null);
    let failed = 0;
    try {
      // Keep each successful deletion; retain failures in the list for retry.
      for (const card of targets) {
        try {
          if (card.id) await modelService.deleteModel(card.id);
          setModels((current) => current.filter((item) => item.local_id !== card.local_id));
        } catch (error: any) {
          failed++;
          updateCard(card.local_id, { saving: false, error: error.response?.data?.error || t('modelManagementPage.deleteFailed') });
        }
      }
      if (failed > 0) setPageError(t('modelManagementPage.batchDeleteFailed', { count: failed }));
    } finally {
      setBulkSaving(false);
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
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={addCard}
                disabled={hasEditingCard || bulkSaving}
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {t('modelManagementPage.addModel')}
              </button>
            </div>
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
          <div className="mt-6 space-y-5">
              {models.filter((card) => card.isEditing).map((card) => {
                const autoDiscoverySupported = AUTO_DISCOVERY_PROVIDERS.has(card.provider_type);
                const discoveredModels = card.discovered_models ?? [];
                const batchPlan = Boolean(card.isNew && discoveredModels.length > 0);
                const planRows = modelRoutingPlanRows(card, discoveredModels);
                const excluded = new Set(card.plan_excluded_ids ?? []);
                const selectedPlan = planRows.filter((model) => !excluded.has(model.id));
                const currentTemplate = getSelectedTemplate(card, t('modelManagementPage.currentVendorTemplate'));
                const templateOptions = getTemplateOptions(card, t('modelManagementPage.currentVendorTemplate'));
                const effectiveProtocolType = resolveProviderProtocolType(card.provider_type, card.protocol_type);
                const allowBaseUrlEditing = Boolean(currentTemplate?.allowCustomBaseUrl);
                const showsProtocolSelector = card.provider_type === 'local';
                const providerModelHelpText = card.discovering
                  ? t('modelManagementPage.loadingProviderModels')
                  : autoDiscoverySupported
                    ? discoveredModels.length > 0
                      ? t('modelManagementPage.planDiscoveryHelp')
                      : card.discovery_error
                        ? t('modelManagementPage.manualFallbackHelp')
                        : canDiscover(card)
                          ? t('modelManagementPage.waitingDiscovery')
                          : t('modelManagementPage.discoveryCredentialsHelp')
                    : t('modelManagementPage.manualEntryHelp');


                return (
                  <fieldset
                    key={card.local_id}
                    ref={newCardRef}
                    disabled={Boolean(card.saving || bulkSaving)}
                    className="min-w-0 self-start rounded-[26px] border border-[#ead8cf] bg-[rgba(255,248,245,0.84)] p-5 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)]"
                  >
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
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
                            protocol_type: resolveProviderProtocolType(template.providerType, template.protocolType),
                            base_url: template.baseUrl,
                          })}
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.protocol')}</label>
                        {showsProtocolSelector ? (
                        <select
                          value={effectiveProtocolType}
                          onChange={(event) => updateCard(card.local_id, {
                            protocol_type: resolveProviderProtocolType(card.provider_type, event.target.value),
                          })}
                          className="app-input mt-1 block h-[38px] w-full"
                          aria-label={t('modelManagementPage.protocol')}
                        >
                          <option value="openai-compatible">{getProtocolTypeLabel('openai-compatible')}</option>
                          <option value="anthropic">{getProtocolTypeLabel('anthropic')}</option>
                        </select>
                        ) : (
                          <input className="app-input mt-1 block h-[38px] w-full bg-gray-50" readOnly
                            value={getProtocolTypeLabel(effectiveProtocolType)} aria-label={t('modelManagementPage.protocol')} />
                        )}
                        <p className="mt-2 text-xs text-gray-500">{t(showsProtocolSelector ? 'modelManagementPage.localProtocolHelp' : 'modelManagementPage.templateProtocolHelp')}</p>
                      </div>
                    </div>

                    <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div className="min-w-0">
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.baseUrl')}</label>
                        <input type="text" value={card.base_url} readOnly={!allowBaseUrlEditing}
                          onChange={(event) => updateCard(card.local_id, { base_url: event.target.value })}
                          className={`app-input mt-1 block w-full ${allowBaseUrlEditing ? '' : 'bg-gray-50'}`}
                          placeholder={currentTemplate?.baseUrl || t('modelManagementPage.baseUrlPlaceholder')} />
                        <p className="mt-2 text-xs text-gray-500">{t(allowBaseUrlEditing ? 'modelManagementPage.editableBaseUrlHelp' : 'modelManagementPage.fixedBaseUrlHelp')}</p>
                      </div>
                      <div className="min-w-0">
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.apiKey')}</label>
                        <input type="password" value={card.api_key}
                          onChange={(event) => updateCard(card.local_id, { api_key: event.target.value })}
                          className="app-input mt-1 block w-full" placeholder={t('modelManagementPage.optionalPlaceholder')} />
                      </div>
                    </div>

                    <details className="mt-3 rounded-xl border border-[#ead8cf] bg-white/60 p-3 text-xs text-gray-600">
                      <summary className="cursor-pointer font-medium">
                        {t('modelManagementPage.advancedCredentials')}
                        {card.api_key_secret_ref?.trim() && <span className="ml-2 text-sky-700">{t('modelManagementPage.secretRefConfigured')}</span>}
                      </summary>
                      <label className="mt-3 block">{t('modelManagementPage.secretRef')}
                        <input type="text" value={card.api_key_secret_ref}
                          onChange={(event) => updateCard(card.local_id, { api_key_secret_ref: event.target.value })}
                          className="app-input mt-1 block w-full" placeholder={t('modelManagementPage.secretRefPlaceholder')} />
                      </label>
                      <p className="mt-2">{t('modelManagementPage.secretRefHelp')}</p>
                    </details>

                    <div className="mt-4">
                      <ModelDiscoveryPlan key={`${card.local_id}:${buildDiscoveryKey(card)}`} models={planRows} disabled={Boolean(card.discovering || card.saving || bulkSaving)}
                        batch={batchPlan} excluded={excluded} onExcludedChange={(ids) => updateCard(card.local_id, { plan_excluded_ids: [...ids] })}
                        connection={{ providerType: card.provider_type, protocolType: card.protocol_type, baseUrl: card.base_url }}
                        onRefresh={() => void discoverModels(card.local_id, true)} refreshDisabled={!canDiscover(card)}
                        refreshing={Boolean(card.discovering)} discoveryHelp={providerModelHelpText}
                        onModelIdChange={!(card.isNew && discoveredModels.length > 0) ? (id) => updateCard(card.local_id, {
                          provider_model_name: id,
                          ...(card.isNew && (!card.display_name || card.display_name === card.provider_model_name) ? { display_name: id } : {}),
                        }) : undefined}
                        onChange={(modelId, patch) => updateDiscoveryPlan(card.local_id, modelId, patch)} />

                      {card.discovery_error && (
                        <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
                          {card.discovery_error}
                        </div>
                      )}
                    </div>

                    <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                      <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.contextWindowTokens')}</label>
                      <input
                        type="number"
                        min="0"
                        max="2147483647"
                        step="1"
                        value={card.context_window_tokens ?? 0}
                        onChange={(event) => updateCard(card.local_id, {
                          context_window_tokens: Number.parseInt(event.target.value, 10) || 0,
                        })}
                        className="app-input mt-1 block w-full"
                        placeholder={t('modelManagementPage.contextWindowPlaceholder')}
                      />
                      <p className="mt-2 text-xs text-gray-500">{t('modelManagementPage.contextWindowHelp')}</p>
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.maxConcurrentRequests')}</label>
                        <input type="number" min="0" max="2147483647" step="1" value={card.max_concurrent_requests ?? 0}
                          onChange={(event) => updateCard(card.local_id, {
                            max_concurrent_requests: Number.parseInt(event.target.value, 10) || 0,
                          })} className="app-input mt-1 block w-full" />
                        <p className="mt-2 text-xs text-gray-500">{t('modelManagementPage.maxConcurrentRequestsHelp')}</p>
                      </div>
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
                          onClick={() => batchPlan ? void applyDiscoveryPlan(card.local_id, selectedPlan.map((model) => model.id)) : void saveCard(card)}
                          disabled={Boolean(card.saving || bulkSaving || card.discovering || (batchPlan && (selectedPlan.length === 0 || selectedPlan.some((model) => !model.display_name?.trim()))))}
                          className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {card.saving ? t('modelManagementPage.saving') : t('common.save')}
                        </button>
                      </div>
                    </div>
                  </fieldset>
                );
              })}
              {models.some((card) => !card.isEditing) && <ModelCatalog models={models.filter((card) => !card.isEditing)} busy={bulkSaving} editing={hasEditingCard}
                onEdit={startEditing} onDelete={(card) => void deleteCard(card)}
                onSaveSelected={(cards) => void saveSelectedCards(cards)} onDeleteSelected={(cards) => void deleteSelectedCards(cards)} />}
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
