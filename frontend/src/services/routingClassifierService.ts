import api from './api';

export type RoutingMode = 'rules' | 'hybrid' | 'laya';
export interface ClassifierSettings {
  base_url: string;
  timeout_ms: number;
  model: string;
  has_api_key: boolean;
  encryption_ready: boolean;
  mode: 'manual_test_only' | 'laya_auto' | 'hybrid_auto';
  routing_mode?: RoutingMode;
  mode_source?: 'saved' | 'deployment_default';
}
export interface RuleAssessment {
  status: 'matched' | 'ambiguous' | 'unmatched';
  reason: string;
  rule_ids: string[];
}
export interface ClassifierDraft {
  base_url: string;
  timeout_ms: number;
  api_key?: string;
  clear_api_key?: boolean;
}
export interface ClassifierResult {
  status: 'success' | 'error';
  classifier: string;
  duration_ms: number;
  error_code?: string;
  classification?: {
    required_tier: number;
    score?: number;
    confidence?: number;
    confidence_kind?: 'normalized_entropy';
    probabilities?: Record<'simple' | 'medium' | 'complex' | 'reasoning', number>;
    rule_assessment?: RuleAssessment;
    signals: string[];
    warnings: string[];
  };
}
export interface ClassifierComparison {
  rule: ClassifierResult;
  laya: ClassifierResult;
  same_tier?: boolean;
  production_routing_changed: boolean;
  hybrid?: {
    rule_assessment: RuleAssessment;
    would_call_laya: boolean;
    source: 'rules' | 'laya' | 'rules_fallback';
    required_tier: number;
    fallback_reason?: string;
    duration_ms: number;
  };
}
export const routingClassifierService = {
  async get(): Promise<ClassifierSettings> {
    return (await api.get('/system-settings/routing-classifier')).data.data;
  },
  async save(draft: ClassifierDraft): Promise<ClassifierSettings> {
    return (await api.put('/system-settings/routing-classifier', draft)).data.data;
  },
  async setMode(mode: RoutingMode, confirmExternal: boolean): Promise<ClassifierSettings> {
    return (await api.put('/system-settings/routing-classifier/mode', { mode, confirm_external: confirmExternal })).data.data;
  },
  async compare(text: string, draft: ClassifierDraft): Promise<ClassifierComparison> {
    return (await api.post('/system-settings/routing-classifier/compare', { text, draft }, { timeout: draft.timeout_ms + 10000 })).data.data;
  },
};
