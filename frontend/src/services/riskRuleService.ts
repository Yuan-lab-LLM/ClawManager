import api from './api';

export interface RiskRule {
  id?: number;
  rule_id: string;
  display_name: string;
  description?: string;
  pattern: string;
  severity: 'low' | 'medium' | 'high';
  action: 'allow' | 'route_secure_model' | 'block';
  is_enabled: boolean;
  sort_order: number;
  created_at?: string;
  updated_at?: string;
}

export interface RiskRuleTestResult {
  is_sensitive: boolean;
  highest_severity: 'low' | 'medium' | 'high';
  highest_action: 'allow' | 'route_secure_model' | 'block';
  hits: Array<{
    rule_id: string;
    rule_name: string;
    severity: 'low' | 'medium' | 'high';
    action: 'allow' | 'route_secure_model' | 'block';
    match_summary: string;
  }>;
}

export const riskRuleService = {
  getRules: async (): Promise<RiskRule[]> => {
    const response = await api.get('/admin/risk-rules');
    return response.data.data?.items ?? [];
  },

  saveRule: async (rule: RiskRule): Promise<RiskRule> => {
    const response = await api.put('/admin/risk-rules', rule);
    return response.data.data;
  },

  testRules: async (request: {
    text: string;
    rule?: RiskRule;
  }): Promise<RiskRuleTestResult> => {
    const response = await api.post('/admin/risk-rules/test', request);
    return response.data.data;
  },

  disableRule: async (ruleId: string): Promise<void> => {
    await api.delete(`/admin/risk-rules/${ruleId}`);
  },

  bulkSetEnabled: async (ruleIds: string[], isEnabled: boolean): Promise<void> => {
    await api.post('/admin/risk-rules/bulk-status', {
      rule_ids: ruleIds,
      is_enabled: isEnabled,
    });
  },
};
