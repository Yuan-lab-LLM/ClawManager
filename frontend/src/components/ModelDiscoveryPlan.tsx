import { useI18n } from '../contexts/I18nContext';
import { useState } from 'react';
import type { DiscoveredProviderModel } from '../services/modelService';
import SelectionCheckbox from './SelectionCheckbox';
import ModelAccessEditor, { type ModelAccessDraft } from './ModelAccessEditor';
import { toggleSelection } from '../lib/modelSelection';
import { modelToolSupport } from '../lib/modelToolSupport';
import { modelVisionSupport } from '../lib/modelVisionSupport';
import { resolveProviderProtocolType, supportsManagedReasoningControl } from '../lib/modelProviderTemplates';
import { plannedModelKind, plannedReasoningEnabled } from '../lib/modelRoutingPlan';

export const discoveryPools = ['default', 'general', 'cheap', 'coding', 'reasoning', 'specialized'] as const;
export type DiscoveryPlanPatch = Partial<Pick<DiscoveredProviderModel, 'model_kind' | 'chat_capable' | 'image_generation_enabled' | 'image_edit_enabled' | 'image_price_per_image' | 'reset_image_price' | 'display_name' | 'suggested_pool' | 'suggested_quality_tier' | 'suggested_priority' | 'tools_support_override' | 'vision_support_override' | 'reset_vision_support_override' | 'reasoning_enabled' | 'reasoning_control_mode' | 'visibility_scope' | 'allowed_user_ids'>>;

interface Props {
  models: DiscoveredProviderModel[];
  disabled: boolean;
  onChange: (id: string, patch: DiscoveryPlanPatch) => void;
  batch: boolean;
  excluded: ReadonlySet<string>;
  onExcludedChange: (ids: Set<string>) => void;
  connection: { providerType: string; protocolType?: string; baseUrl: string };
  onModelIdChange?: (id: string) => void;
  onRefresh: () => void;
  refreshDisabled: boolean;
  refreshing: boolean;
  discoveryHelp: string;
}

export default function ModelDiscoveryPlan({ models, disabled, onChange, batch, excluded, onExcludedChange, connection, onModelIdChange, onRefresh, refreshDisabled, refreshing, discoveryHelp }: Props) {
  const { t } = useI18n();
  const [customGroups, setCustomGroups] = useState<Set<string>>(new Set());
  const [batchAccess, setBatchAccess] = useState<ModelAccessDraft>({ visibility_scope: 'all', allowed_user_ids: [] });
  const selected = models.filter((model) => !excluded.has(model.id));
  const missingNames = selected.some((model) => !model.display_name?.trim());
  const label = (key: string) => t(`modelManagementPage.${key}`);
  return (
    <section className="mt-3 overflow-hidden rounded-xl border border-sky-200 bg-sky-50/60" aria-label={label('planEditorTitle')}>
      <div className="space-y-2 p-4">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold text-sky-950">{label('planEditorTitle')}</h3>
          <button type="button" onClick={onRefresh} disabled={disabled || refreshDisabled || refreshing}
            className="shrink-0 rounded-lg border border-sky-200 bg-white px-3 py-1 text-xs text-sky-900 disabled:opacity-50">
            {refreshing ? t('common.loading') : label('refreshModels')}
          </button>
        </div>
        <p className="text-xs text-gray-500">{discoveryHelp}</p>
        <p className="text-xs leading-relaxed text-sky-800">{label(batch ? 'planEditorHelp' : 'planSingleHelp')}</p>
        <p className="text-xs leading-relaxed text-sky-800">{label('planDisplayNameHelp')}</p>
        <p className="text-xs leading-relaxed text-sky-800">{label('supportsToolsHelp')}</p>
        <p className="text-xs leading-relaxed text-sky-800">可见范围按实例所有者判断；不同 URL 下的模型分别授权。保存后限制后续调用。支持授权联动的运行时会自动同步模型目录；旧版本需重启刷新。整轮任务中止以运行时取消回执为准，已完成的工具操作不会回滚。</p>
        <details className="rounded-lg border border-sky-100 bg-white p-3 text-xs text-gray-600">
          <summary className="cursor-pointer font-medium text-sky-900">{label('planGuideTitle')}</summary>
          <p className="mt-3 leading-relaxed">{label('planGroupHelp')}</p>
          <dl className="mt-3 grid gap-3 sm:grid-cols-2">
            {discoveryPools.map((pool) => <div key={pool}>
              <dt className="font-medium text-gray-900">{label(`pool_${pool}`)}</dt>
              <dd className="mt-1 leading-relaxed">{label(`poolHelp_${pool}`)}</dd>
            </div>)}
          </dl>
          <p className="mt-3 border-t pt-3 leading-relaxed">{label('planTierHelp')}</p>
          <p className="mt-2 leading-relaxed">{label('deploymentPriorityHelp')}</p>
        </details>
      </div>
      <div className="max-h-[28rem] overflow-auto border-y border-sky-100 bg-white">
        <table className="w-full min-w-[640px] text-left text-xs">
          <thead className="sticky top-0 bg-sky-50 text-gray-700"><tr>
            {batch && <th scope="col" className="w-10 px-3 py-3"><SelectionCheckbox
              label={label('selectAllPlan')} checked={models.length > 0 && selected.length === models.length}
              mixed={selected.length > 0 && selected.length < models.length} disabled={disabled || models.length === 0}
              onChange={(checked) => onExcludedChange(toggleSelection(excluded, models.map((model) => model.id), !checked))} /></th>}
            {['displayName', 'modelKind', 'planGroupLabel', 'qualityTier', 'deploymentPriority', 'supportsTools', 'supportsVision', 'reasoning'].map((key) => (
              <th scope="col" key={key} className="px-3 py-3 font-medium">{label(key)}</th>
            ))}
            <th scope="col" className="px-3 py-3 font-medium">可见范围</th>
          </tr></thead>
          <tbody className="divide-y divide-gray-100">
            {models.map((model) => {
              const pool = model.suggested_pool || 'default';
              const kind = plannedModelKind(model);
              const nonChat = kind !== 'chat';
              const tools = modelToolSupport(model);
              const vision = modelVisionSupport(model);
              const protocol = resolveProviderProtocolType(connection.providerType, connection.protocolType);
              const canConfigureReasoning = !nonChat && ['openai', 'openai-compatible'].includes(protocol);
              const reasoningSupported = !nonChat && supportsManagedReasoningControl(connection.providerType, connection.protocolType, connection.baseUrl, model.id, model.reasoning_control_mode);
              return <tr key={batch ? model.id : 'current-model'} className="align-top">
                {batch && <td className="px-3 py-3"><SelectionCheckbox label={`${label('selectModel')} ${model.display_name || model.id}`}
                  checked={!excluded.has(model.id)} disabled={disabled}
                  onChange={(checked) => onExcludedChange(toggleSelection(excluded, [model.id], !checked))} /></td>}
                <td className="min-w-[220px] max-w-[300px] break-words px-3 py-3">
                  {model.id.trim() && <input type="text" className="app-input w-full" value={model.display_name ?? ''}
                    aria-label={`${model.id} ${label('displayName')}`} placeholder={model.id}
                    maxLength={255} disabled={disabled} aria-invalid={!model.display_name?.trim()}
                    onChange={(event) => onChange(model.id, { display_name: event.target.value })} />}
                  {onModelIdChange ? <label className="mt-2 block text-gray-500">{label('providerModel')}
                    <input type="text" className="app-input mt-1 w-full" value={model.id} disabled={disabled} maxLength={255}
                      placeholder={label('manualProviderModelPlaceholder')} onChange={(event) => onModelIdChange(event.target.value)} />
                  </label> : <p className="mt-1 break-all text-gray-500">{label('providerModel')}: {model.id || '-'}</p>}
                  <p className={`mt-1 ${nonChat ? 'text-amber-700' : 'text-gray-500'}`}>
                    {nonChat ? label(kind === 'image' ? 'planImageModel' : 'planNonChat') : !batch ? label('planCurrent') : model.plan_edited ? label('planEdited') : label('planSuggested')}
                  </p>
                </td>
                <td className="min-w-48 max-w-[260px] px-3 py-3">
                  <select className="app-input w-full" aria-label={`${model.id} ${label('modelKind')}`} value={kind} disabled={disabled}
                    onChange={(event) => onChange(model.id, {
                      model_kind: event.target.value, chat_capable: event.target.value === 'chat',
                      image_generation_enabled: false, image_edit_enabled: false,
                      ...(event.target.value !== 'chat' ? { reasoning_enabled: false, reasoning_control_mode: '', suggested_quality_tier: 0 } : {}),
                    })}>
                    {['chat', 'image', 'other'].map((value) => <option key={value} value={value}>{label(`modelKind_${value}`)}</option>)}
                  </select>
                  {kind === 'image' && <div className="mt-2 space-y-2">
                    <label className="flex items-center gap-2"><SelectionCheckbox
                      label={`${model.id} ${label('imageGenerationEnabled')}`} checked={model.image_generation_enabled === true}
                      disabled={disabled || (!model.image_generation_enabled && !['openai', 'openai-compatible'].includes(protocol))}
                      onChange={(checked) => onChange(model.id, { image_generation_enabled: checked })} />
                      <span>{label('imageGenerationEnabled')}</span></label>
                    <label className="flex items-center gap-2"><SelectionCheckbox
                      label={`${model.id} ${label('imageEditEnabled')}`} checked={model.image_edit_enabled === true}
                      disabled={disabled || (!model.image_edit_enabled && !['openai', 'openai-compatible'].includes(protocol))}
                      onChange={(checked) => onChange(model.id, { image_edit_enabled: checked })} />
                      <span>{label('imageEditEnabled')}</span></label>
                    <label className="block">{label('imagePricePerImage')}
                      <input className="app-input mt-1 w-full" type="number" min="0" max="1000000" step="0.00000001"
                        aria-label={`${model.id} ${label('imagePricePerImage')}`} disabled={disabled}
                        value={model.image_price_per_image ?? ''} placeholder={label('imagePriceUnset')}
                        onChange={(event) => onChange(model.id, { image_price_per_image: event.target.value === '' ? null : Number(event.target.value), reset_image_price: event.target.value === '' })} />
                    </label>
                    <p className="leading-relaxed text-gray-500">{label('imagePriceHelp')}</p>
                    <p className="leading-relaxed text-amber-700">{label(['openai', 'openai-compatible'].includes(protocol) ? 'imageOperationsPending' : 'imageProtocolUnsupported')}</p>
                  </div>}
                </td>
                <td className="w-[30%] px-3 py-3">
                  <select className="app-input w-full" aria-label={`${model.id} ${label('planGroupLabel')}`} value={pool}
                    disabled={disabled || kind === 'other'} onChange={(e) => {
                      if (e.target.value === '__custom__') {
                        setCustomGroups((current) => new Set(current).add(model.id));
                      } else {
                        setCustomGroups((current) => { const next = new Set(current); next.delete(model.id); return next; });
                        onChange(model.id, { suggested_pool: e.target.value });
                      }
                    }}>
                    {!discoveryPools.includes(pool as typeof discoveryPools[number]) && <option value={pool}>{pool}</option>}
                    {discoveryPools.map((value) => <option key={value} value={value}>{label(`pool_${value}`)}</option>)}
                    <option value="__custom__">{label('planCustomGroup')}</option>
                  </select>
                  {(customGroups.has(model.id) || !discoveryPools.includes(pool as typeof discoveryPools[number])) && (
                    <input className="app-input mt-2 w-full" aria-label={`${model.id} ${label('planCustomGroup')}`} value={model.suggested_pool ?? ''}
                      disabled={disabled || kind === 'other'} maxLength={100} onChange={(e) => onChange(model.id, { suggested_pool: e.target.value })} />
                  )}
                  <p className="mt-1 leading-relaxed text-gray-500">{discoveryPools.includes(pool as typeof discoveryPools[number]) ? label(`poolHelp_${pool}`) : label('planGroupHelp')}</p>
                </td>
                <td className="px-3 py-3">
                  <select className="app-input w-full" aria-label={`${model.id} ${label('qualityTier')}`} value={model.suggested_quality_tier ?? 0}
                    disabled={disabled || nonChat} onChange={(e) => onChange(model.id, { suggested_quality_tier: Number(e.target.value) })}>
                    {[0, 1, 2, 3, 4].map((tier) => <option key={tier} value={tier}>{label(`qualityTier${tier}`)}</option>)}
                  </select>
                </td>
                <td className="w-28 px-3 py-3">
                  <input type="number" min="0" max="2147483647" step="1" className="app-input w-full" aria-label={`${model.id} ${label('deploymentPriority')}`}
                    value={model.suggested_priority ?? 50} disabled={disabled}
                    onChange={(e) => onChange(model.id, { suggested_priority: Math.min(2147483647, Math.max(0, Number.parseInt(e.target.value, 10) || 0)) })} />
                </td>
                <td className="min-w-32 px-3 py-3">
                  <label className="flex items-center gap-2">
                    <SelectionCheckbox label={`${model.id} ${label('supportsTools')}`} checked={tools === true}
                      mixed={tools === null} disabled={disabled || nonChat}
                      onChange={(checked) => onChange(model.id, { tools_support_override: checked })} />
                    <span>{label(tools === null ? 'toolsUnknown' : tools ? 'toolsSupported' : 'toolsUnsupported')}</span>
                  </label>
                  {model.tools_support_override != null && <p className="mt-1 text-gray-500">{label('toolsManual')}</p>}
                </td>
                <td className="min-w-36 max-w-[220px] px-3 py-3">
                  <label className="flex items-center gap-2">
                    <SelectionCheckbox label={`${model.id} ${label('supportsVision')}`} checked={vision === true}
                      mixed={vision === null} disabled={disabled || nonChat}
                      onChange={(checked) => onChange(model.id, { vision_support_override: checked, reset_vision_support_override: false })} />
                    <span>{label(vision === null ? 'visionUnknown' : vision ? 'visionSupported' : 'visionUnsupported')}</span>
                  </label>
                  <p className="mt-1 text-gray-500">{label('supportsVisionHelp')}</p>
                  {model.vision_support_override != null && <button type="button" disabled={disabled}
                    className="mt-1 text-sky-700 disabled:opacity-50" onClick={() => onChange(model.id, { vision_support_override: null, reset_vision_support_override: true })}>
                    {label('visionReset')}
                  </button>}
                </td>
                <td className="min-w-36 max-w-[220px] px-3 py-3">
                  <select className="app-input mb-2 w-full" aria-label={`${model.id} ${label('reasoningFormat')}`}
                    value={model.reasoning_control_mode ?? ''} disabled={disabled || !canConfigureReasoning}
                    onChange={(event) => onChange(model.id, { reasoning_control_mode: event.target.value, reasoning_enabled: false })}>
                    <option value="">{label('reasoningFormatAuto')}</option>
                    <option value="none">{label('reasoningFormatNone')}</option>
                    <option value="thinking-type">thinking.type · Kimi / GLM</option>
                    <option value="deepseek-thinking">thinking.type · DeepSeek</option>
                    <option value="enable-thinking">enable_thinking · Qwen</option>
                    <option value="chat-template-kwargs">chat_template_kwargs · vLLM</option>
                  </select>
                  <label className="flex items-center gap-2">
                    <SelectionCheckbox label={`${model.id} ${label('reasoning')}`} checked={reasoningSupported && plannedReasoningEnabled(model)}
                      disabled={disabled || !reasoningSupported} onChange={(checked) => onChange(model.id, { reasoning_enabled: checked })} />
                    <span>{label(!reasoningSupported ? 'reasoningUnmanaged' : plannedReasoningEnabled(model) ? 'reasoningEnabled' : 'reasoningDisabled')}</span>
                  </label>
                  <p className="mt-1 text-gray-500">{label(reasoningSupported ? 'reasoningFormatHelp' : 'reasoningUnmanagedHelp')}</p>
                </td>
                <td className="min-w-52 px-3 py-3"><ModelAccessEditor label={model.display_name || model.id}
                  scope={model.visibility_scope} userIds={model.allowed_user_ids} disabled={disabled}
                  onChange={(patch) => onChange(model.id, patch)} /></td>
              </tr>;
            })}
          </tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 p-4">
        {batch && <div className="flex flex-wrap items-start gap-3 rounded border border-sky-100 bg-white p-3">
          <span>批量可见范围（只修改勾选行的草稿）</span>
          <ModelAccessEditor label="批量" scope={batchAccess.visibility_scope} userIds={batchAccess.allowed_user_ids}
            disabled={disabled} onChange={setBatchAccess} />
          <button type="button" className="rounded border px-3 py-2 disabled:opacity-40"
            disabled={disabled || selected.length === 0 || (batchAccess.visibility_scope === 'selected_users' && batchAccess.allowed_user_ids.length === 0)}
            onClick={() => selected.forEach((model) => onChange(model.id, { ...batchAccess, allowed_user_ids: [...batchAccess.allowed_user_ids] }))}>
            应用到选中行 ({selected.length})</button>
        </div>}
        <div className="text-xs text-gray-600"><p>{label(batch ? 'planSaveSelectionHelp' : 'planSingleHelp')}</p>
          {batch && <p className="mt-1" role="status">{t('modelManagementPage.selectionCount', { count: selected.length, total: models.length })}</p>}
          {batch && missingNames && <p className="mt-1 text-amber-700" role="alert">{label('planNamesRequired')}</p>}
        </div>
      </div>
    </section>
  );
}
