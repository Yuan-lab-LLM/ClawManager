import React, { useEffect, useMemo, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import {
  riskRuleService,
  type RiskRule,
  type RiskRuleTestResult,
} from '../../services/riskRuleService';
import { useI18n } from '../../contexts/I18nContext';

const SEVERITY_OPTIONS: Array<{ value: RiskRule['severity']; label: string }> = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
];

const ACTION_OPTIONS: Array<{ value: RiskRule['action']; label: string }> = [
  { value: 'allow', label: 'Allow' },
  { value: 'route_secure_model', label: 'Route Secure Model' },
];

type RuleCategory = 'personal' | 'company' | 'customer' | 'security' | 'financeLegal' | 'political' | 'custom';

interface RuleVisualMeta {
  category: RuleCategory;
  icon: string;
  panelClass: string;
  badgeClass: string;
  iconShellClass: string;
}

const RULE_VISUALS: Record<string, RuleVisualMeta> = {
  private_key_marker: {
    category: 'security',
    icon: 'M15 7a3 3 0 10-5.196 2.018L3 16v5h5l6.982-6.804A3 3 0 0015 7z',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  api_key_like: {
    category: 'security',
    icon: 'M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2h-1V9a5 5 0 00-10 0v2H6a2 2 0 00-2 2v6a2 2 0 002 2z',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  credential_assignment: {
    category: 'security',
    icon: 'M4 7h16M4 12h8m-8 5h16',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  private_ip: {
    category: 'company',
    icon: 'M3 10l9-7 9 7v8a2 2 0 01-2 2H5a2 2 0 01-2-2v-8z',
    panelClass: 'border-[#d7e7f2] bg-[rgba(248,251,255,0.94)]',
    badgeClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
    iconShellClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
  },
  email_address: {
    category: 'personal',
    icon: 'M4 6h16v12H4V6zm0 0l8 6 8-6',
    panelClass: 'border-[#f2d7d7] bg-[rgba(255,248,248,0.94)]',
    badgeClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
    iconShellClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
  },
  cn_mobile_number: {
    category: 'personal',
    icon: 'M7 4h10a1 1 0 011 1v14a1 1 0 01-1 1H7a1 1 0 01-1-1V5a1 1 0 011-1zm5 13h.01',
    panelClass: 'border-[#f2d7d7] bg-[rgba(255,248,248,0.94)]',
    badgeClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
    iconShellClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
  },
  cn_id_card: {
    category: 'personal',
    icon: 'M4 7h16v10H4V7zm4 2h5m-5 3h8m3-3h.01',
    panelClass: 'border-[#f2d7d7] bg-[rgba(255,248,248,0.94)]',
    badgeClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
    iconShellClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
  },
  passport_number: {
    category: 'personal',
    icon: 'M12 2a7 7 0 017 7v11H5V9a7 7 0 017-7zm0 0v18m-4-9h8',
    panelClass: 'border-[#f2d7d7] bg-[rgba(255,248,248,0.94)]',
    badgeClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
    iconShellClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
  },
  bank_card_context: {
    category: 'personal',
    icon: 'M3 7h18v10H3V7zm0 3h18M7 14h2',
    panelClass: 'border-[#f2d7d7] bg-[rgba(255,248,248,0.94)]',
    badgeClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
    iconShellClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
  },
  personal_address_keywords: {
    category: 'personal',
    icon: 'M12 21s-6-4.35-6-10a6 6 0 1112 0c0 5.65-6 10-6 10zm0-8a2 2 0 100-4 2 2 0 000 4z',
    panelClass: 'border-[#f2d7d7] bg-[rgba(255,248,248,0.94)]',
    badgeClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
    iconShellClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
  },
  resume_cv_keywords: {
    category: 'personal',
    icon: 'M7 3h7l5 5v13H7V3zm7 0v5h5',
    panelClass: 'border-[#f2d7d7] bg-[rgba(255,248,248,0.94)]',
    badgeClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
    iconShellClass: 'border-[#f0d2d2] bg-[#fff3f3] text-[#a65252]',
  },
  internal_domain_name: {
    category: 'company',
    icon: 'M4 5h16v14H4V5zm4 4h8m-8 4h5',
    panelClass: 'border-[#d7e7f2] bg-[rgba(248,251,255,0.94)]',
    badgeClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
    iconShellClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
  },
  internal_hostname_pattern: {
    category: 'company',
    icon: 'M5 12h14M5 8h14M5 16h14',
    panelClass: 'border-[#d7e7f2] bg-[rgba(248,251,255,0.94)]',
    badgeClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
    iconShellClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
  },
  kubernetes_service_dns: {
    category: 'company',
    icon: 'M12 3l7 4v10l-7 4-7-4V7l7-4zm0 6l3 1.7V14L12 15.7 9 14v-3.3L12 9z',
    panelClass: 'border-[#d7e7f2] bg-[rgba(248,251,255,0.94)]',
    badgeClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
    iconShellClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
  },
  project_codename_keywords: {
    category: 'company',
    icon: 'M5 12h4l2 3 3-6 2 3h3',
    panelClass: 'border-[#d7e7f2] bg-[rgba(248,251,255,0.94)]',
    badgeClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
    iconShellClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
  },
  org_roster_keywords: {
    category: 'company',
    icon: 'M8 7a2 2 0 114 0 2 2 0 01-4 0zm8 2a2 2 0 100-4 2 2 0 000 4zM4 9a2 2 0 100-4 2 2 0 000 4zm4 10H2v-1a4 4 0 014-4h2a4 4 0 014 4v1H8zm10 0h-6v-1a4 4 0 014-4h2a4 4 0 014 4v1z',
    panelClass: 'border-[#d7e7f2] bg-[rgba(248,251,255,0.94)]',
    badgeClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
    iconShellClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
  },
  salary_hr_keywords: {
    category: 'company',
    icon: 'M12 8c-2.761 0-5 1.12-5 2.5S9.239 13 12 13s5 1.12 5 2.5S14.761 18 12 18m0-10V6m0 12v-2',
    panelClass: 'border-[#d7e7f2] bg-[rgba(248,251,255,0.94)]',
    badgeClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
    iconShellClass: 'border-[#cfe1f0] bg-[#eff7ff] text-[#2f6f96]',
  },
  customer_list_keywords: {
    category: 'customer',
    icon: 'M7 10a3 3 0 116 0 3 3 0 01-6 0zm10 1a2 2 0 100-4 2 2 0 000 4zm-8 8H3v-1a4 4 0 014-4h2a4 4 0 014 4v1zm10 0h-5v-1a4 4 0 00-2-3.46',
    panelClass: 'border-[#e6dcf5] bg-[rgba(251,249,255,0.94)]',
    badgeClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
    iconShellClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
  },
  contract_quote_keywords: {
    category: 'customer',
    icon: 'M7 3h7l5 5v13H7V3zm7 0v5h5M10 13h4m-4 4h6',
    panelClass: 'border-[#e6dcf5] bg-[rgba(251,249,255,0.94)]',
    badgeClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
    iconShellClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
  },
  invoice_tax_keywords: {
    category: 'customer',
    icon: 'M6 3h12v18l-2-1-2 1-2-1-2 1-2-1-2 1V3zm3 5h6m-6 4h6m-6 4h4',
    panelClass: 'border-[#e6dcf5] bg-[rgba(251,249,255,0.94)]',
    badgeClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
    iconShellClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
  },
  crm_ticket_keywords: {
    category: 'customer',
    icon: 'M5 7h14v4a2 2 0 010 4v4H5v-4a2 2 0 010-4V7zm4 3h6',
    panelClass: 'border-[#e6dcf5] bg-[rgba(251,249,255,0.94)]',
    badgeClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
    iconShellClass: 'border-[#ddd1f0] bg-[#f6f1ff] text-[#7052a3]',
  },
  jwt_token_like: {
    category: 'security',
    icon: 'M12 2l7 4v5c0 5-3.5 9.5-7 11-3.5-1.5-7-6-7-11V6l7-4zm0 7v3m0 4h.01',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  cookie_session_like: {
    category: 'security',
    icon: 'M7 3a2 2 0 104 0 2 2 0 104 0 2 2 0 104 0v4a7 7 0 11-12 7V3z',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  db_connection_string: {
    category: 'security',
    icon: 'M12 4c4.418 0 8 1.12 8 2.5S16.418 9 12 9 4 7.88 4 6.5 7.582 4 12 4zm8 6v3.5C20 14.88 16.418 16 12 16s-8-1.12-8-2.5V10',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  kubeconfig_content: {
    category: 'security',
    icon: 'M12 3l7 4v10l-7 4-7-4V7l7-4zm0 5l3 1.5v3L12 14l-3-1.5v-3L12 8z',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  env_file_secret: {
    category: 'security',
    icon: 'M8 4h8l4 4v12H4V4h4zm1 5h6m-6 4h6m-6 4h4',
    panelClass: 'border-[#f5dfcb] bg-[rgba(255,249,244,0.94)]',
    badgeClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
    iconShellClass: 'border-[#f5d5c2] bg-[#fff5ef] text-[#b46c50]',
  },
  financial_metrics_keywords: {
    category: 'financeLegal',
    icon: 'M4 17l4-4 3 3 5-6 4 4',
    panelClass: 'border-[#efe3bf] bg-[rgba(255,252,244,0.94)]',
    badgeClass: 'border-[#e8dab0] bg-[#fff8e7] text-[#9a7b20]',
    iconShellClass: 'border-[#e8dab0] bg-[#fff8e7] text-[#9a7b20]',
  },
  legal_document_keywords: {
    category: 'financeLegal',
    icon: 'M9 4h6m-3 0v12m-5 0h10',
    panelClass: 'border-[#efe3bf] bg-[rgba(255,252,244,0.94)]',
    badgeClass: 'border-[#e8dab0] bg-[#fff8e7] text-[#9a7b20]',
    iconShellClass: 'border-[#e8dab0] bg-[#fff8e7] text-[#9a7b20]',
  },
  political_person_org_keywords: {
    category: 'political',
    icon: 'M4 20h16M6 20V9h12v11M9 9V6l3-2 3 2v3',
    panelClass: 'border-[#efd7de] bg-[rgba(255,248,250,0.94)]',
    badgeClass: 'border-[#ebcbd4] bg-[#fff2f6] text-[#a24f68]',
    iconShellClass: 'border-[#ebcbd4] bg-[#fff2f6] text-[#a24f68]',
  },
  military_security_keywords: {
    category: 'political',
    icon: 'M12 2l7 4v6c0 4.5-3 8.5-7 10-4-1.5-7-5.5-7-10V6l7-4z',
    panelClass: 'border-[#efd7de] bg-[rgba(255,248,250,0.94)]',
    badgeClass: 'border-[#ebcbd4] bg-[#fff2f6] text-[#a24f68]',
    iconShellClass: 'border-[#ebcbd4] bg-[#fff2f6] text-[#a24f68]',
  },
  extremism_keywords: {
    category: 'political',
    icon: 'M12 9v4m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z',
    panelClass: 'border-[#efd7de] bg-[rgba(255,248,250,0.94)]',
    badgeClass: 'border-[#ebcbd4] bg-[#fff2f6] text-[#a24f68]',
    iconShellClass: 'border-[#ebcbd4] bg-[#fff2f6] text-[#a24f68]',
  },
};

const CATEGORY_LABEL_KEYS: Record<RuleCategory, string> = {
  personal: 'riskRulesPage.categoryPersonal',
  company: 'riskRulesPage.categoryCompany',
  customer: 'riskRulesPage.categoryCustomer',
  security: 'riskRulesPage.categorySecurity',
  financeLegal: 'riskRulesPage.categoryFinanceLegal',
  political: 'riskRulesPage.categoryPolitical',
  custom: 'riskRulesPage.categoryCustom',
};

const CATEGORY_OPTIONS: RuleCategory[] = ['personal', 'company', 'customer', 'security', 'financeLegal', 'political', 'custom'];

const DEFAULT_RULE_VISUAL: RuleVisualMeta = {
  category: 'custom',
  icon: 'M12 4v16m8-8H4',
  panelClass: 'border-[#ead8cf] bg-[rgba(255,248,245,0.84)]',
  badgeClass: 'border-[#ead8cf] bg-white text-[#7c5a4d]',
  iconShellClass: 'border-[#ead8cf] bg-white text-[#7c5a4d]',
};

function getRuleVisual(ruleId: string): RuleVisualMeta {
  return RULE_VISUALS[ruleId] ?? DEFAULT_RULE_VISUAL;
}

interface EditableRiskRule extends RiskRule {
  local_id: string;
  isNew?: boolean;
  isEditing?: boolean;
  saving?: boolean;
  error?: string | null;
  editSnapshot?: EditableRiskRuleSnapshot;
}

type EditableRiskRuleSnapshot = Pick<
  EditableRiskRule,
  | 'rule_id'
  | 'display_name'
  | 'description'
  | 'pattern'
  | 'severity'
  | 'action'
  | 'is_enabled'
  | 'sort_order'
>;

const createEmptyRule = (): EditableRiskRule => ({
  local_id: `new-${Date.now()}-${Math.random().toString(16).slice(2)}`,
  rule_id: '',
  display_name: '',
  description: '',
  pattern: '',
  severity: 'medium',
  action: 'route_secure_model',
  is_enabled: true,
  sort_order: 100,
  isNew: true,
  isEditing: true,
  error: null,
});

const captureRuleSnapshot = (rule: EditableRiskRule): EditableRiskRuleSnapshot => ({
  rule_id: rule.rule_id,
  display_name: rule.display_name,
  description: rule.description,
  pattern: rule.pattern,
  severity: rule.severity,
  action: rule.action,
  is_enabled: rule.is_enabled,
  sort_order: rule.sort_order,
});

const RiskRulesPage: React.FC = () => {
  const { t } = useI18n();
  const [rules, setRules] = useState<EditableRiskRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [severityFilter, setSeverityFilter] = useState<'all' | RiskRule['severity']>('all');
  const [actionFilter, setActionFilter] = useState<'all' | RiskRule['action']>('all');
  const [statusFilter, setStatusFilter] = useState<'all' | 'enabled' | 'disabled'>('all');
  const [categoryFilter, setCategoryFilter] = useState<'all' | RuleCategory>('all');
  const [selectedRuleIds, setSelectedRuleIds] = useState<string[]>([]);
  const [sampleText, setSampleText] = useState('');
  const [testLoading, setTestLoading] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<RiskRuleTestResult | null>(null);
  const editingRule = rules.find((item) => item.isEditing);
  const hasEditingRule = Boolean(editingRule);
  const filteredRules = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    return rules.filter((rule) => {
      if (severityFilter !== 'all' && rule.severity !== severityFilter) {
        return false;
      }
      if (actionFilter !== 'all' && rule.action !== actionFilter) {
        return false;
      }
      if (statusFilter === 'enabled' && !rule.is_enabled) {
        return false;
      }
      if (statusFilter === 'disabled' && rule.is_enabled) {
        return false;
      }
      const category = getRuleVisual(rule.rule_id).category;
      if (categoryFilter !== 'all' && category !== categoryFilter) {
        return false;
      }
      if (!normalizedSearch) {
        return true;
      }
      return [
        rule.rule_id,
        rule.display_name,
        rule.description || '',
        rule.pattern,
      ].some((value) => value.toLowerCase().includes(normalizedSearch));
    });
  }, [rules, search, severityFilter, actionFilter, statusFilter, categoryFilter]);

  const groupedRules = useMemo(() => {
    const buckets: Record<RuleCategory, EditableRiskRule[]> = {
      personal: [],
      company: [],
      customer: [],
      security: [],
      financeLegal: [],
      political: [],
      custom: [],
    };

    filteredRules.forEach((rule) => {
      const category = getRuleVisual(rule.rule_id).category;
      buckets[category].push(rule);
    });

    return CATEGORY_OPTIONS
      .map((category) => ({
        category,
        rules: buckets[category],
      }))
      .filter((group) => group.rules.length > 0);
  }, [filteredRules]);

  const selectableRuleIds = useMemo(
    () => filteredRules.filter((rule) => !rule.isEditing).map((rule) => rule.rule_id),
    [filteredRules],
  );

  const allVisibleSelected = selectableRuleIds.length > 0 && selectableRuleIds.every((ruleId) => selectedRuleIds.includes(ruleId));

  const summary = useMemo(() => ({
    total: rules.length,
    enabled: rules.filter((rule) => rule.is_enabled).length,
    allowed: rules.filter((rule) => rule.action === 'allow').length,
    rerouted: rules.filter((rule) => rule.action === 'route_secure_model').length,
  }), [rules]);

  useEffect(() => {
    const loadRules = async () => {
      try {
        setLoading(true);
        setPageError(null);
        const items = await riskRuleService.getRules();
        setRules(items.map((item, index) => ({
          ...item,
          description: item.description ?? '',
          local_id: `${item.rule_id}-${index}`,
          isEditing: false,
          error: null,
        })));
      } catch (error: any) {
        setPageError(error.response?.data?.error || t('riskRulesPage.loadFailed'));
      } finally {
        setLoading(false);
      }
    };

    loadRules();
  }, []);

  const addRule = () => {
    if (hasEditingRule) {
      return;
    }
    setRules((current) => [...current, createEmptyRule()]);
  };

  const updateRule = (localId: string, patch: Partial<EditableRiskRule>) => {
    setRules((current) => current.map((item) => (
      item.local_id === localId
        ? { ...item, ...patch, error: patch.error ?? item.error }
        : item
    )));
  };

  const startEditing = (rule: EditableRiskRule) => {
    if (hasEditingRule && !rule.isEditing) {
      return;
    }
    updateRule(rule.local_id, {
      isEditing: true,
      error: null,
      editSnapshot: captureRuleSnapshot(rule),
    });
  };

  const cancelEditing = (rule: EditableRiskRule) => {
    if (rule.isNew) {
      setRules((current) => current.filter((item) => item.local_id !== rule.local_id));
      return;
    }

    const snapshot = rule.editSnapshot ?? captureRuleSnapshot(rule);
    updateRule(rule.local_id, {
      ...snapshot,
      isEditing: false,
      saving: false,
      error: null,
      editSnapshot: undefined,
    });
  };

  const saveRule = async (rule: EditableRiskRule) => {
    if (!rule.rule_id.trim() || !rule.display_name.trim() || !rule.pattern.trim()) {
      updateRule(rule.local_id, { error: t('riskRulesPage.requiredFields') });
      return;
    }

    const duplicateRule = rules.find((item) => item.local_id !== rule.local_id && item.rule_id.trim().toLowerCase() === rule.rule_id.trim().toLowerCase());
    if (duplicateRule) {
      updateRule(rule.local_id, { error: t('riskRulesPage.duplicateRuleId') });
      return;
    }

    updateRule(rule.local_id, { saving: true, error: null });
    try {
      const saved = await riskRuleService.saveRule({
        rule_id: rule.rule_id.trim(),
        display_name: rule.display_name.trim(),
        description: rule.description?.trim() || undefined,
        pattern: rule.pattern.trim(),
        severity: rule.severity,
        action: rule.action,
        is_enabled: rule.is_enabled,
        sort_order: Number(rule.sort_order) || 0,
      });

      setRules((current) => current.map((item) => (
        item.local_id === rule.local_id
          ? {
              ...item,
              ...saved,
              description: saved.description ?? '',
              isEditing: false,
              isNew: false,
              saving: false,
              error: null,
              editSnapshot: undefined,
            }
          : item
      )));
    } catch (error: any) {
      updateRule(rule.local_id, {
        saving: false,
        error: error.response?.data?.error || t('riskRulesPage.saveFailed'),
      });
    }
  };

  const disableRule = async (rule: EditableRiskRule) => {
    if (rule.isNew) {
      setRules((current) => current.filter((item) => item.local_id !== rule.local_id));
      return;
    }

    updateRule(rule.local_id, { saving: true, error: null });
    try {
      await riskRuleService.disableRule(rule.rule_id);
      setRules((current) => current.map((item) => (
        item.local_id === rule.local_id
          ? { ...item, is_enabled: false, isEditing: false, saving: false, error: null }
          : item
      )));
    } catch (error: any) {
      updateRule(rule.local_id, {
        saving: false,
        error: error.response?.data?.error || t('riskRulesPage.disableFailed'),
      });
    }
  };

  const toggleRuleEnabled = async (rule: EditableRiskRule) => {
    if (rule.isNew) {
      return;
    }

    updateRule(rule.local_id, { saving: true, error: null });
    try {
      const saved = await riskRuleService.saveRule({
        rule_id: rule.rule_id,
        display_name: rule.display_name,
        description: rule.description,
        pattern: rule.pattern,
        severity: rule.severity,
        action: rule.action,
        is_enabled: !rule.is_enabled,
        sort_order: rule.sort_order,
      });

      setRules((current) => current.map((item) => (
        item.local_id === rule.local_id
          ? {
              ...item,
              ...saved,
              description: saved.description ?? '',
              saving: false,
              error: null,
            }
          : item
      )));
    } catch (error: any) {
      updateRule(rule.local_id, {
        saving: false,
        error: error.response?.data?.error || t('riskRulesPage.statusUpdateFailed'),
      });
    }
  };

  const toggleRuleSelection = (ruleId: string) => {
    setSelectedRuleIds((current) => (
      current.includes(ruleId)
        ? current.filter((id) => id !== ruleId)
        : [...current, ruleId]
    ));
  };

  const toggleSelectVisible = () => {
    setSelectedRuleIds((current) => {
      if (allVisibleSelected) {
        return current.filter((id) => !selectableRuleIds.includes(id));
      }

      const merged = new Set([...current, ...selectableRuleIds]);
      return Array.from(merged);
    });
  };

  const bulkSetEnabled = async (isEnabled: boolean) => {
    if (selectedRuleIds.length === 0) {
      setPageError(t('riskRulesPage.bulkNoSelection'));
      return;
    }

    try {
      setPageError(null);
      await riskRuleService.bulkSetEnabled(selectedRuleIds, isEnabled);
      setRules((current) => current.map((rule) => (
        selectedRuleIds.includes(rule.rule_id)
          ? { ...rule, is_enabled: isEnabled, saving: false, error: null }
          : rule
      )));
      setSelectedRuleIds([]);
    } catch (error: any) {
      setPageError(error.response?.data?.error || t('riskRulesPage.bulkStatusUpdateFailed'));
    }
  };

  const runRuleTest = async (mode: 'enabled' | 'draft') => {
    if (!sampleText.trim()) {
      setTestError(t('riskRulesPage.sampleRequired'));
      setTestResult(null);
      return;
    }

    if (mode === 'draft' && !editingRule) {
      setTestError(t('riskRulesPage.noDraftRule'));
      setTestResult(null);
      return;
    }

    try {
      setTestLoading(true);
      setTestError(null);
      const result = await riskRuleService.testRules({
        text: sampleText,
        rule: mode === 'draft' && editingRule
          ? {
              rule_id: editingRule.rule_id,
              display_name: editingRule.display_name,
              description: editingRule.description,
              pattern: editingRule.pattern,
              severity: editingRule.severity,
              action: editingRule.action,
              is_enabled: editingRule.is_enabled,
              sort_order: editingRule.sort_order,
            }
          : undefined,
      });
      setTestResult(result);
    } catch (error: any) {
      setTestError(error.response?.data?.error || t('riskRulesPage.testFailed'));
      setTestResult(null);
    } finally {
      setTestLoading(false);
    }
  };

  return (
    <AdminLayout title={t('nav.riskRules')}>
      <div className="space-y-6">
        <section className="app-panel p-6">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-xl font-semibold text-gray-900">{t('riskRulesPage.title')}</h2>
              <p className="mt-1 text-sm text-gray-500">
                {t('riskRulesPage.subtitle')}
              </p>
            </div>
            <button
              type="button"
              onClick={addRule}
              disabled={hasEditingRule}
              className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {t('riskRulesPage.addRule')}
            </button>
          </div>

          <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <SummaryCard label={t('riskRulesPage.totalRules')} value={String(summary.total)} />
            <SummaryCard label={t('riskRulesPage.enabled')} value={String(summary.enabled)} highlight="green" />
            <SummaryCard label={t('riskRulesPage.allowActions')} value={String(summary.allowed)} />
            <SummaryCard label={t('riskRulesPage.secureRouteActions')} value={String(summary.rerouted)} highlight="amber" />
          </div>

          <div className="mt-5 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div className="flex flex-col gap-3 sm:flex-row">
              <input
                type="text"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                className="app-input min-w-[280px]"
                placeholder={t('riskRulesPage.searchPlaceholder')}
              />
              <select
                value={severityFilter}
                onChange={(event) => setSeverityFilter(event.target.value as 'all' | RiskRule['severity'])}
                className="app-input"
              >
                <option value="all">{t('riskRulesPage.allSeverities')}</option>
                {SEVERITY_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
              <select
                value={actionFilter}
                onChange={(event) => setActionFilter(event.target.value as 'all' | RiskRule['action'])}
                className="app-input"
              >
                <option value="all">{t('riskRulesPage.allActions')}</option>
                {ACTION_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
              <select
                value={categoryFilter}
                onChange={(event) => setCategoryFilter(event.target.value as 'all' | RuleCategory)}
                className="app-input"
              >
                <option value="all">{t('riskRulesPage.allCategories')}</option>
                {CATEGORY_OPTIONS.map((category) => (
                  <option key={category} value={category}>
                    {t(CATEGORY_LABEL_KEYS[category])}
                  </option>
                ))}
              </select>
              <select
                value={statusFilter}
                onChange={(event) => setStatusFilter(event.target.value as 'all' | 'enabled' | 'disabled')}
                className="app-input"
              >
                <option value="all">{t('riskRulesPage.allStatuses')}</option>
                <option value="enabled">{t('riskRulesPage.enabled')}</option>
                <option value="disabled">{t('riskRulesPage.disabled')}</option>
              </select>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <label className="inline-flex items-center gap-2 text-sm text-[#5f5957]">
                <input
                  type="checkbox"
                  checked={allVisibleSelected}
                  onChange={toggleSelectVisible}
                  className="h-4 w-4 rounded border-gray-300 text-[#b84c28] focus:ring-[#b84c28]"
                />
                {t('riskRulesPage.selectVisible')}
              </label>
              <button
                type="button"
                onClick={() => void bulkSetEnabled(true)}
                disabled={selectedRuleIds.length === 0}
                className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {t('riskRulesPage.bulkEnable')}
              </button>
              <button
                type="button"
                onClick={() => void bulkSetEnabled(false)}
                disabled={selectedRuleIds.length === 0}
                className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {t('riskRulesPage.bulkDisable')}
              </button>
            </div>
          </div>

          {hasEditingRule && (
            <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
              {t('riskRulesPage.finishEditing')}
            </div>
          )}

          {pageError && (
            <div className="mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {pageError}
            </div>
          )}

          {loading ? (
            <div className="mt-6 text-sm text-gray-500">{t('riskRulesPage.loading')}</div>
          ) : (
            <div className="mt-6 space-y-6">
              {groupedRules.map((group) => (
                <section key={group.category} className="space-y-3">
                  <div className="flex items-center gap-3 px-1">
                    <div className={`inline-flex items-center rounded-full border px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] ${DEFAULT_RULE_VISUAL.badgeClass}`}>
                      {t(CATEGORY_LABEL_KEYS[group.category])}
                    </div>
                    <div className="text-xs font-medium text-[#8f8681]">{group.rules.length}</div>
                  </div>
                  <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2 xl:grid-cols-4">
                    {group.rules.map((rule) => {
                      if (!rule.isEditing) {
                        const visual = getRuleVisual(rule.rule_id);
                        return (
                          <div
                            key={rule.local_id}
                            className={`flex h-[340px] flex-col self-start rounded-[22px] p-4 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)] ${visual.panelClass}`}
                          >
                            <div className="flex items-start justify-between gap-3">
                              <div className="flex min-w-0 flex-1 gap-3">
                                <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border ${visual.iconShellClass}`}>
                                  <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d={visual.icon} />
                                  </svg>
                                </div>
                                <div className="min-w-0 flex-1">
                            <div className={`inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.16em] ${visual.badgeClass}`}>
                              {t(CATEGORY_LABEL_KEYS[visual.category])}
                            </div>
                            <label className="mt-2 inline-flex items-center gap-2 text-xs text-[#8f8681]">
                              <input
                                type="checkbox"
                                checked={selectedRuleIds.includes(rule.rule_id)}
                                onChange={() => toggleRuleSelection(rule.rule_id)}
                                className="h-4 w-4 rounded border-gray-300 text-[#b84c28] focus:ring-[#b84c28]"
                              />
                              {t('riskRulesPage.selectRule')}
                            </label>
                            <h3 className="mt-2 line-clamp-2 text-base font-semibold leading-6 text-gray-900">{rule.display_name}</h3>
                                  <p className="mt-1 truncate text-xs text-gray-500">{rule.rule_id}</p>
                                </div>
                              </div>
                              <div className="flex shrink-0 items-center gap-2 self-start text-xs text-gray-500">
                                <span className={`rounded-full px-2.5 py-1 ${
                                  rule.severity === 'high'
                                    ? 'border border-red-200 bg-red-50 text-red-700'
                                    : rule.severity === 'medium'
                                      ? 'border border-amber-200 bg-amber-50 text-amber-700'
                                      : 'border border-slate-200 bg-slate-50 text-slate-700'
                                }`}>
                                  {rule.severity}
                                </span>
                                <span className={`rounded-full px-2.5 py-1 ${rule.is_enabled ? 'border border-[#d9ead3] bg-[#f3fff0] text-[#2f6b2f]' : 'border border-[#eadfd8] bg-white text-[#7b6f6a]'}`}>
                                  {rule.is_enabled ? t('riskRulesPage.enabled') : t('riskRulesPage.disabled')}
                                </span>
                              </div>
                            </div>

                            <dl className="mt-4 grid min-h-0 flex-1 grid-cols-1 gap-3 text-sm overflow-hidden">
                              <div>
                                <dt className="font-medium text-gray-700">{t('riskRulesPage.action')}</dt>
                                <dd className="mt-1 truncate text-gray-600">{rule.action}</dd>
                              </div>
                              <div>
                                <dt className="font-medium text-gray-700">{t('riskRulesPage.pattern')}</dt>
                                <dd className="mt-1 line-clamp-3 overflow-hidden rounded-lg bg-[#fffaf7] px-3 py-2 font-mono text-[11px] leading-5 text-gray-600 break-all">
                                  {rule.pattern}
                                </dd>
                              </div>
                              <div>
                                <dt className="font-medium text-gray-700">{t('common.description')}</dt>
                                <dd className="mt-1 line-clamp-3 overflow-hidden whitespace-pre-wrap text-gray-600">{rule.description || '-'}</dd>
                              </div>
                            </dl>

                            <div className="mt-auto flex items-center justify-end gap-2 pt-4">
                              <button
                                type="button"
                                onClick={() => void toggleRuleEnabled(rule)}
                                disabled={rule.saving}
                                className="app-button-secondary shrink-0 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {rule.is_enabled ? t('riskRulesPage.disable') : t('riskRulesPage.enable')}
                              </button>
                              <button
                                type="button"
                                onClick={() => startEditing(rule)}
                                disabled={hasEditingRule}
                                className="app-button-primary shrink-0 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {t('modelManagementPage.edit')}
                              </button>
                            </div>
                          </div>
                        );
                      }

                      return (
                        <div
                          key={rule.local_id}
                          className="self-start rounded-[24px] border border-[#ead8cf] bg-[rgba(255,248,245,0.84)] p-5 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)] md:col-span-2 xl:col-span-2"
                        >
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('riskRulesPage.ruleId')}</label>
                        <input
                          type="text"
                          value={rule.rule_id}
                          onChange={(event) => updateRule(rule.local_id, { rule_id: event.target.value })}
                          className="app-input mt-1 block w-full"
                          placeholder={t('riskRulesPage.ruleIdPlaceholder')}
                          disabled={!rule.isNew}
                        />
                        {!rule.isNew && (
                          <p className="mt-2 text-xs text-[#8f8681]">
                            {t('riskRulesPage.ruleIdLocked')}
                          </p>
                        )}
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('modelManagementPage.displayName')}</label>
                        <input
                          type="text"
                          value={rule.display_name}
                          onChange={(event) => updateRule(rule.local_id, { display_name: event.target.value })}
                          className="app-input mt-1 block w-full"
                          placeholder="Email address"
                        />
                      </div>
                    </div>

                    <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3">
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('riskRulesPage.severity')}</label>
                        <select
                          value={rule.severity}
                          onChange={(event) => updateRule(rule.local_id, { severity: event.target.value as RiskRule['severity'] })}
                          className="app-input mt-1 block w-full"
                        >
                          {SEVERITY_OPTIONS.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('riskRulesPage.action')}</label>
                        <select
                          value={rule.action}
                          onChange={(event) => updateRule(rule.local_id, { action: event.target.value as RiskRule['action'] })}
                          className="app-input mt-1 block w-full"
                        >
                          {ACTION_OPTIONS.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700">{t('riskRulesPage.sortOrder')}</label>
                        <input
                          type="number"
                          value={rule.sort_order}
                          onChange={(event) => updateRule(rule.local_id, { sort_order: Number(event.target.value) })}
                          className="app-input mt-1 block w-full"
                        />
                      </div>
                    </div>

                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700">{t('riskRulesPage.regexPattern')}</label>
                      <textarea
                        value={rule.pattern}
                        onChange={(event) => updateRule(rule.local_id, { pattern: event.target.value })}
                        rows={3}
                        className="app-input mt-1 block w-full font-mono"
                        placeholder={t('riskRulesPage.patternPlaceholder')}
                      />
                    </div>

                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700">{t('common.description')}</label>
                      <textarea
                        value={rule.description}
                        onChange={(event) => updateRule(rule.local_id, { description: event.target.value })}
                        rows={3}
                        className="app-input mt-1 block w-full"
                        placeholder={t('riskRulesPage.descriptionPlaceholder')}
                      />
                    </div>

                    <div className="mt-4">
                      <label className="inline-flex items-center gap-2 text-sm text-gray-700">
                        <input
                          type="checkbox"
                          checked={rule.is_enabled}
                          onChange={(event) => updateRule(rule.local_id, { is_enabled: event.target.checked })}
                          className="h-4 w-4 rounded border-gray-300 text-[#b84c28] focus:ring-[#b84c28]"
                        />
                        {t('riskRulesPage.enabled')}
                      </label>
                    </div>

                    {rule.error && (
                      <div className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                        {rule.error}
                      </div>
                    )}

                    <div className="mt-4 flex items-center justify-end gap-3">
                      <button
                        type="button"
                        onClick={() => cancelEditing(rule)}
                        disabled={rule.saving}
                        className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                      >
                          {t('common.cancel')}
                      </button>
                      <button
                        type="button"
                        onClick={() => void disableRule(rule)}
                        disabled={rule.saving}
                        className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                      >
                          {t('riskRulesPage.disable')}
                      </button>
                      <button
                        type="button"
                        onClick={() => void saveRule(rule)}
                        disabled={rule.saving}
                        className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                      >
                          {rule.saving ? t('modelManagementPage.saving') : t('common.save')}
                      </button>
                    </div>
                        </div>
                      );
                    })}
                  </div>
                </section>
              ))}
            </div>
          )}

          {!loading && filteredRules.length === 0 && (
            <div className="mt-6 rounded-xl border border-dashed border-[#eadfd8] bg-[#fffaf7] px-4 py-8 text-center text-sm text-[#8f8681]">
              {t('riskRulesPage.noRules')}
            </div>
          )}
        </section>

        <section className="app-panel p-6">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h2 className="text-xl font-semibold text-gray-900">{t('riskRulesPage.testLab')}</h2>
              <p className="mt-1 text-sm text-gray-500">
                {t('riskRulesPage.testLabSubtitle')}
              </p>
            </div>
            <div className="flex flex-col gap-3 sm:flex-row">
              <button
                type="button"
                onClick={() => void runRuleTest('enabled')}
                disabled={testLoading}
                className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {testLoading ? t('riskRulesPage.testing') : t('riskRulesPage.testEnabled')}
              </button>
              <button
                type="button"
                onClick={() => void runRuleTest('draft')}
                disabled={testLoading || !editingRule}
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {t('riskRulesPage.testDraft')}
              </button>
            </div>
          </div>

          <div className="mt-5">
            <label className="block text-sm font-medium text-gray-700">{t('riskRulesPage.sampleText')}</label>
            <textarea
              value={sampleText}
              onChange={(event) => setSampleText(event.target.value)}
              rows={6}
              className="app-input mt-2 block w-full"
              placeholder={t('riskRulesPage.samplePlaceholder')}
            />
          </div>

          {testError && (
            <div className="mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {testError}
            </div>
          )}

          {testResult && (
            <div className="mt-5 space-y-4">
              <div className="flex flex-wrap items-center gap-3">
                <span className={`rounded-full px-3 py-1 text-sm font-medium ${
                  testResult.is_sensitive
                    ? 'border border-red-200 bg-red-50 text-red-700'
                    : 'border border-[#d9ead3] bg-[#f3fff0] text-[#2f6b2f]'
                }`}>
                  {testResult.is_sensitive ? t('riskRulesPage.sensitive') : t('riskRulesPage.noMatch')}
                </span>
                <span className="rounded-full border border-[#eadfd8] bg-white px-3 py-1 text-sm text-[#5f5957]">
                  {t('riskRulesPage.highestSeverity', { value: testResult.highest_severity })}
                </span>
                <span className="rounded-full border border-[#eadfd8] bg-white px-3 py-1 text-sm text-[#5f5957]">
                  {t('riskRulesPage.highestAction', { value: testResult.highest_action })}
                </span>
              </div>

              <div className="grid gap-4 xl:grid-cols-2">
                {testResult.hits.length === 0 ? (
                  <div className="rounded-xl border border-[#eadfd8] bg-[#fffaf7] px-4 py-3 text-sm text-[#8f8681]">
                    {t('riskRulesPage.noMatchesForSample')}
                  </div>
                ) : (
                  testResult.hits.map((hit, index) => (
                    <div key={`${hit.rule_id}-${index}`} className="rounded-xl border border-[#eadfd8] bg-[#fffaf7] p-4">
                      <div className="flex items-center justify-between gap-3">
                        <div className="font-medium text-[#171212]">{hit.rule_name}</div>
                        <div className="flex items-center gap-2">
                          <span className="rounded-full border border-[#eadfd8] bg-white px-2.5 py-1 text-xs text-[#5f5957]">
                            {hit.severity}
                          </span>
                          <span className="rounded-full border border-[#eadfd8] bg-white px-2.5 py-1 text-xs text-[#5f5957]">
                            {hit.action}
                          </span>
                        </div>
                      </div>
                      <div className="mt-2 text-xs text-[#8f8681]">{hit.rule_id}</div>
                      <div className="mt-3 text-sm text-[#5f5957]">{hit.match_summary}</div>
                    </div>
                  ))
                )}
              </div>
            </div>
          )}
        </section>
      </div>
    </AdminLayout>
  );
};

const SummaryCard: React.FC<{
  label: string;
  value: string;
  highlight?: 'green' | 'red' | 'amber';
}> = ({ label, value, highlight }) => {
  const toneClass = highlight === 'green'
    ? 'border-[#d9ead3] bg-[#f3fff0]'
    : highlight === 'red'
      ? 'border-red-200 bg-red-50'
      : highlight === 'amber'
        ? 'border-amber-200 bg-amber-50'
        : 'border-[#eadfd8] bg-white';

  return (
    <div className={`rounded-2xl border px-4 py-4 ${toneClass}`}>
      <div className="text-xs font-semibold uppercase tracking-[0.14em] text-[#8f8681]">{label}</div>
      <div className="mt-3 text-2xl font-semibold tracking-[-0.04em] text-[#171212]">{value}</div>
    </div>
  );
};

export default RiskRulesPage;
