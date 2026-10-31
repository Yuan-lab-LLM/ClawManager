import type { DiscoveredProviderModel, LLMModel } from '../services/modelService';

export function modelVisionSupport(model: Pick<LLMModel, 'vision_support_override' | 'supports_vision' | 'capability_source'> & Pick<DiscoveredProviderModel, 'model_kind' | 'classification_source'>): boolean | null {
  if (model.model_kind && model.model_kind !== 'chat') return false;
  if (model.vision_support_override != null) return model.vision_support_override;
  return model.supports_vision === true && (model.capability_source ?? model.classification_source) === 'provider_metadata' ? true : null;
}
