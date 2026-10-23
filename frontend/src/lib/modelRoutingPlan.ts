import type { DiscoveredProviderModel, LLMModel } from '../services/modelService';

// Provider metadata is a suggestion. Persisted entries have an explicit kind;
// missing kind on a legacy saved model remains chat, never reclassified by name.
export function plannedModelKind(model: Pick<DiscoveredProviderModel, 'model_kind' | 'chat_capable'>): 'chat' | 'image' | 'other' {
  const kind = model.model_kind?.trim().toLowerCase();
  if (['image', 'image-generation', 'image_generation', 'text-to-image', 'image-edit', 'image_edit'].includes(kind ?? '')) return 'image';
  if (kind === 'chat') return 'chat';
  return kind === 'other' || model.chat_capable === false ? 'other' : 'chat';
}

// Both preview and application consume the same rows, including the current
// card's values. Discovery must not overwrite an existing model's configuration.
export function modelRoutingPlanRows(card: LLMModel & { isNew?: boolean }, discovered: DiscoveredProviderModel[]): DiscoveredProviderModel[] {
  const selected = discovered.find((model) => model.id === card.provider_model_name);
  const current: DiscoveredProviderModel = {
    ...selected,
    id: card.provider_model_name,
    display_name: card.display_name ?? selected?.display_name ?? card.provider_model_name,
    model_kind: card.model_kind ?? (card.isNew ? plannedModelKind(selected ?? {}) : 'chat'),
    image_generation_enabled: card.image_generation_enabled ?? (card.isNew ? selected?.image_generation_enabled : false) ?? false,
    image_edit_enabled: card.image_edit_enabled ?? (card.isNew ? selected?.image_edit_enabled : false) ?? false,
    image_price_per_image: card.image_price_per_image,
    reset_image_price: card.reset_image_price,
    chat_capable: (card.model_kind ?? (card.isNew ? plannedModelKind(selected ?? {}) : 'chat')) === 'chat',
    suggested_pool: card.model_group || card.display_name || 'default',
    suggested_quality_tier: card.quality_tier ?? 0,
    suggested_priority: card.deployment_priority ?? 100,
    supports_tools: card.isNew ? selected?.supports_tools ?? card.supports_tools : card.supports_tools,
    capability_source: card.isNew ? selected?.classification_source ?? card.capability_source : card.capability_source,
    tools_support_override: card.tools_support_override ?? selected?.tools_support_override,
    supports_vision: card.isNew ? selected?.supports_vision ?? card.supports_vision : card.supports_vision,
    vision_support_override: card.vision_support_override,
    reset_vision_support_override: card.reset_vision_support_override,
    reasoning_enabled: card.reasoning_enabled,
    reasoning_control_mode: card.reasoning_control_mode,
	visibility_scope: card.visibility_scope ?? 'all',
	allowed_user_ids: card.allowed_user_ids ?? [],
  };
  if (!card.isNew || discovered.length === 0) return [current];
  const rows = discovered.map((model) => model.id === current.id ? current : model);
  if (current.id && !selected) rows.unshift(current);
  return rows;
}

// A refresh replaces provider metadata, not the administrator's per-model draft.
export function mergeDiscoveredPlan(discovered: DiscoveredProviderModel[], previous: DiscoveredProviderModel[]): DiscoveredProviderModel[] {
  const edited = new Map(previous.filter((model) => model.plan_edited).map((model) => [model.id, model]));
  return discovered.map((model) => {
    const draft = edited.get(model.id);
    return draft ? {
      ...model, display_name: draft.display_name,
      model_kind: draft.model_kind ?? model.model_kind,
      chat_capable: plannedModelKind(draft) === 'chat',
      image_generation_enabled: draft.image_generation_enabled,
      image_edit_enabled: draft.image_edit_enabled,
      image_price_per_image: draft.image_price_per_image,
      reset_image_price: draft.reset_image_price,
      suggested_pool: draft.suggested_pool, suggested_quality_tier: draft.suggested_quality_tier,
      suggested_priority: draft.suggested_priority, tools_support_override: draft.tools_support_override, plan_edited: true,
      vision_support_override: draft.vision_support_override,
      reset_vision_support_override: draft.reset_vision_support_override,
      reasoning_enabled: draft.reasoning_enabled,
      reasoning_control_mode: draft.reasoning_control_mode,
	  visibility_scope: draft.visibility_scope,
	  allowed_user_ids: draft.allowed_user_ids,
    } : model;
  });
}

export function plannedReasoningEnabled(model: DiscoveredProviderModel): boolean {
  return model.reasoning_enabled ?? (model.supports_reasoning === true || model.suggested_pool === 'reasoning');
}
