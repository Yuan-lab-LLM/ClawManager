package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// RiskRuleRepository defines repository operations for configurable risk rules.
type RiskRuleRepository interface {
	List() ([]models.RiskRule, error)
	ListEnabled() ([]models.RiskRule, error)
	GetByRuleID(ruleID string) (*models.RiskRule, error)
	Upsert(rule *models.RiskRule) error
}

type riskRuleRepository struct {
	sess db.Session
}

// NewRiskRuleRepository creates a new risk rule repository and ensures its table and defaults exist.
func NewRiskRuleRepository(sess db.Session) RiskRuleRepository {
	repo := &riskRuleRepository{sess: sess}
	repo.ensureTable()
	repo.normalizeDeprecatedActions()
	repo.normalizeDefaultRuleActions()
	repo.seedDefaults()
	return repo
}

func (r *riskRuleRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS risk_rules (
  id INT AUTO_INCREMENT PRIMARY KEY,
  rule_id VARCHAR(100) NOT NULL UNIQUE,
  display_name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  pattern TEXT NOT NULL,
  severity VARCHAR(20) NOT NULL,
  action VARCHAR(50) NOT NULL,
  is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort_order INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_risk_rules_enabled (is_enabled),
  INDEX idx_risk_rules_sort (sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure risk_rules table: %w", err))
	}
}

func (r *riskRuleRepository) seedDefaults() {
	defaults := builtInRiskRules()

	for _, item := range defaults {
		existing, err := r.GetByRuleID(item.RuleID)
		if err != nil {
			panic(fmt.Errorf("failed to inspect risk rule %s: %w", item.RuleID, err))
		}
		if existing != nil {
			continue
		}
		rule := item
		now := time.Now()
		rule.CreatedAt = now
		rule.UpdatedAt = now
		if _, err := r.sess.Collection("risk_rules").Insert(&rule); err != nil {
			panic(fmt.Errorf("failed to seed risk rule %s: %w", item.RuleID, err))
		}
	}
}

func (r *riskRuleRepository) normalizeDeprecatedActions() {
	if _, err := r.sess.SQL().Exec("UPDATE risk_rules SET action = ? WHERE action = ?", models.RiskActionBlock, "require_approval"); err != nil {
		panic(fmt.Errorf("failed to normalize deprecated risk rule actions: %w", err))
	}
}

func (r *riskRuleRepository) normalizeDefaultRuleActions() {
	for _, rule := range builtInRiskRules() {
		if _, err := r.sess.SQL().Exec(
			"UPDATE risk_rules SET action = ?, updated_at = ? WHERE rule_id = ?",
			models.RiskActionRouteSecureModel,
			time.Now(),
			rule.RuleID,
		); err != nil {
			panic(fmt.Errorf("failed to normalize default risk rule action for %s: %w", rule.RuleID, err))
		}
	}
}

func builtInRiskRules() []models.RiskRule {
	return []models.RiskRule{
		{
			RuleID:      "private_key_marker",
			DisplayName: "Private key material",
			Description: stringPtr("Detect PEM private key markers."),
			Pattern:     `-----BEGIN [A-Z ]*PRIVATE KEY-----`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   10,
		},
		{
			RuleID:      "api_key_like",
			DisplayName: "API key or cloud credential pattern",
			Description: stringPtr("Detect common API key and cloud credential patterns."),
			Pattern:     `(?i)\b(sk-[a-z0-9]{12,}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|ghp_[0-9A-Za-z]{20,255}|AIza[0-9A-Za-z\-_]{35})\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   20,
		},
		{
			RuleID:      "credential_assignment",
			DisplayName: "Credential assignment pattern",
			Description: stringPtr("Detect explicit password, secret, token, or API key assignment."),
			Pattern:     `(?i)\b(password|passwd|pwd|secret|api[_-]?key|token|client[_-]?secret|access[_-]?key)\b\s*[:=]\s*\S+`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   30,
		},
		{
			RuleID:      "private_ip",
			DisplayName: "Private network address",
			Description: stringPtr("Detect RFC1918 private IP addresses."),
			Pattern:     `\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3})\b`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   40,
		},
		{
			RuleID:      "email_address",
			DisplayName: "Email address",
			Description: stringPtr("Detect email addresses."),
			Pattern:     `(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   50,
		},
		{
			RuleID:      "cn_mobile_number",
			DisplayName: "Chinese mainland mobile number",
			Description: stringPtr("Detect Chinese mainland mobile phone numbers."),
			Pattern:     `(?:\+?86[- ]?)?1[3-9]\d{9}`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   60,
		},
		{
			RuleID:      "cn_id_card",
			DisplayName: "Chinese ID card number",
			Description: stringPtr("Detect 18-digit Chinese ID card numbers."),
			Pattern:     `\b\d{17}[\dXx]\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   70,
		},
		{
			RuleID:      "passport_number",
			DisplayName: "Passport number",
			Description: stringPtr("Detect common passport number formats."),
			Pattern:     `(?i)\b(?:passport|护照)[^A-Za-z0-9]{0,12}[A-Z0-9]{7,10}\b`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   80,
		},
		{
			RuleID:      "bank_card_context",
			DisplayName: "Bank card context",
			Description: stringPtr("Detect card-number-like data near bank-card keywords."),
			Pattern:     `(?i)(银行卡|bank card|card number|account number).{0,24}\b\d(?:[ -]?\d){11,18}\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   90,
		},
		{
			RuleID:      "personal_address_keywords",
			DisplayName: "Personal address keywords",
			Description: stringPtr("Detect common personal address expressions."),
			Pattern:     `(?i)(住址|开户地址|通信地址|家庭住址|收件地址|mailing address|home address|residential address)`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   100,
		},
		{
			RuleID:      "resume_cv_keywords",
			DisplayName: "Resume or CV content",
			Description: stringPtr("Detect resume and curriculum vitae related content."),
			Pattern:     `(?i)\b(简历|履历|resume|curriculum vitae|work experience|employment history|education history|expected salary)\b`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   110,
		},
		{
			RuleID:      "internal_domain_name",
			DisplayName: "Internal domain or system hostname",
			Description: stringPtr("Detect common internal domain and system naming patterns."),
			Pattern:     `(?i)\b(?:corp|internal|intra|intranet|gitlab|jenkins|confluence|jira|wiki|oa|vpn)\.[a-z0-9.-]+\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   120,
		},
		{
			RuleID:      "internal_hostname_pattern",
			DisplayName: "Internal host or environment naming",
			Description: stringPtr("Detect internal hostnames and environment-specific resource names."),
			Pattern:     `(?i)\b(?:prod|stg|stage|dev|test|uat|db|redis|mq|k8s|kube|es|kafka|mongo|mysql|postgres)[-_][a-z0-9.-]+\b`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   130,
		},
		{
			RuleID:      "kubernetes_service_dns",
			DisplayName: "Kubernetes service DNS",
			Description: stringPtr("Detect Kubernetes internal service DNS references."),
			Pattern:     `(?i)\b[a-z0-9-]+\.[a-z0-9-]+\.svc(?:\.cluster\.local)?\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   140,
		},
		{
			RuleID:      "project_codename_keywords",
			DisplayName: "Project codename or roadmap wording",
			Description: stringPtr("Detect project codename or unreleased roadmap wording."),
			Pattern:     `(?i)(项目代号|产品代号|代号|codename|roadmap|未发布|未公开|internal launch)`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   150,
		},
		{
			RuleID:      "org_roster_keywords",
			DisplayName: "Organization roster or directory",
			Description: stringPtr("Detect organization charts, rosters, and internal directories."),
			Pattern:     `(?i)(组织架构|员工名单|花名册|通讯录|organization chart|employee roster|staff roster|directory)`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   160,
		},
		{
			RuleID:      "salary_hr_keywords",
			DisplayName: "Salary or HR data",
			Description: stringPtr("Detect compensation, payroll, and HR evaluation wording."),
			Pattern:     `(?i)(薪资|薪酬|绩效|KPI|晋升|调薪|裁员|salary|compensation|payroll|performance review)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   170,
		},
		{
			RuleID:      "customer_list_keywords",
			DisplayName: "Customer list or contact list",
			Description: stringPtr("Detect customer roster and contact-list style wording."),
			Pattern:     `(?i)(客户名单|客户列表|联系人清单|CRM导出|customer list|client list|contact list|lead list)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   180,
		},
		{
			RuleID:      "contract_quote_keywords",
			DisplayName: "Contract or quote document",
			Description: stringPtr("Detect contract, quotation, or tender document wording."),
			Pattern:     `(?i)(合同|报价单|投标|采购单|订单明细|contract|quote|quotation|proposal|tender|purchase order|order details)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   190,
		},
		{
			RuleID:      "invoice_tax_keywords",
			DisplayName: "Invoice or tax information",
			Description: stringPtr("Detect invoice and tax identifier wording."),
			Pattern:     `(?i)(发票|税号|纳税识别号|开票信息|invoice|tax id|vat|tin)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   200,
		},
		{
			RuleID:      "crm_ticket_keywords",
			DisplayName: "CRM or ticket data",
			Description: stringPtr("Detect CRM notes, ticket content, or customer profile wording."),
			Pattern:     `(?i)(工单|客服记录|客户画像|客户需求|ticket|case record|crm note|customer profile)`,
			Severity:    models.RiskSeverityMedium,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   210,
		},
		{
			RuleID:      "jwt_token_like",
			DisplayName: "JWT token",
			Description: stringPtr("Detect JWT-like bearer tokens."),
			Pattern:     `\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   220,
		},
		{
			RuleID:      "cookie_session_like",
			DisplayName: "Cookie or session secret wording",
			Description: stringPtr("Detect session, cookie, and authorization wording."),
			Pattern:     `(?i)\b(sessionid|set-cookie|refresh_token|access_token|authorization|cookie)\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   230,
		},
		{
			RuleID:      "db_connection_string",
			DisplayName: "Database or queue connection string",
			Description: stringPtr("Detect connection strings for databases, caches, and queues."),
			Pattern:     `(?i)\b(?:mysql|postgres(?:ql)?|mongodb(?:\+srv)?|redis|amqp|kafka):\/\/[^\s]+`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   240,
		},
		{
			RuleID:      "kubeconfig_content",
			DisplayName: "Kubeconfig content",
			Description: stringPtr("Detect kubeconfig-style configuration content."),
			Pattern:     `(?s)apiVersion:\s*v1.{0,400}(clusters:|contexts:|users:)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   250,
		},
		{
			RuleID:      "env_file_secret",
			DisplayName: "Environment secret variable",
			Description: stringPtr("Detect common secret variable names in env-style content."),
			Pattern:     `(?i)\b(DATABASE_URL|SECRET_KEY|PRIVATE_KEY|ACCESS_KEY|API_SECRET|CLIENT_SECRET|WEBHOOK_SECRET)\b`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   260,
		},
		{
			RuleID:      "financial_metrics_keywords",
			DisplayName: "Financial metrics or budget data",
			Description: stringPtr("Detect financial metrics, budgets, and profitability wording."),
			Pattern:     `(?i)(营收|收入|利润|毛利|成本|预算|现金流|revenue|gross margin|profit|budget|cash flow)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   270,
		},
		{
			RuleID:      "legal_document_keywords",
			DisplayName: "Legal or compliance document",
			Description: stringPtr("Detect legal opinions, disputes, and NDA style wording."),
			Pattern:     `(?i)(法务意见|诉讼|仲裁|保密协议|NDA|legal opinion|litigation|arbitration|non-disclosure agreement)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   280,
		},
		{
			RuleID:      "political_person_org_keywords",
			DisplayName: "Political or government organization wording",
			Description: stringPtr("Detect political institutions, officials, and related wording."),
			Pattern:     `(?i)(国务院|外交部|国安|公安部|人大|政协|党中央|中央军委|政府工作报告|政治局|state council|ministry of foreign affairs|national security)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   290,
		},
		{
			RuleID:      "military_security_keywords",
			DisplayName: "Military or national security wording",
			Description: stringPtr("Detect military deployment, weapons, and national security wording."),
			Pattern:     `(?i)(军事部署|武器系统|导弹|军工|部队番号|国家安全|涉密单位|military deployment|weapon system|missile|classified unit)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   300,
		},
		{
			RuleID:      "extremism_keywords",
			DisplayName: "Extremism or violent attack wording",
			Description: stringPtr("Detect extremist, violent attack, and bomb-making wording."),
			Pattern:     `(?i)(恐怖袭击|炸弹制造|极端组织|暴恐|terrorist attack|bomb making|extremist organization)`,
			Severity:    models.RiskSeverityHigh,
			Action:      models.RiskActionRouteSecureModel,
			IsEnabled:   true,
			SortOrder:   310,
		},
	}
}

func (r *riskRuleRepository) List() ([]models.RiskRule, error) {
	var items []models.RiskRule
	if err := r.sess.Collection("risk_rules").Find().OrderBy("sort_order", "rule_id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list risk rules: %w", err)
	}
	return items, nil
}

func (r *riskRuleRepository) ListEnabled() ([]models.RiskRule, error) {
	var items []models.RiskRule
	if err := r.sess.Collection("risk_rules").Find(db.Cond{"is_enabled": true}).OrderBy("sort_order", "rule_id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list enabled risk rules: %w", err)
	}
	return items, nil
}

func (r *riskRuleRepository) GetByRuleID(ruleID string) (*models.RiskRule, error) {
	var item models.RiskRule
	err := r.sess.Collection("risk_rules").Find(db.Cond{"rule_id": ruleID}).One(&item)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get risk rule by rule id: %w", err)
	}
	return &item, nil
}

func (r *riskRuleRepository) Upsert(rule *models.RiskRule) error {
	existing, err := r.GetByRuleID(rule.RuleID)
	if err != nil {
		return err
	}

	now := time.Now()
	if existing == nil {
		rule.CreatedAt = now
		rule.UpdatedAt = now
		res, err := r.sess.Collection("risk_rules").Insert(rule)
		if err != nil {
			return fmt.Errorf("failed to create risk rule: %w", err)
		}
		rule.ID = int(res.ID().(int64))
		return nil
	}

	existing.DisplayName = rule.DisplayName
	existing.Description = rule.Description
	existing.Pattern = rule.Pattern
	existing.Severity = rule.Severity
	existing.Action = rule.Action
	existing.IsEnabled = rule.IsEnabled
	existing.SortOrder = rule.SortOrder
	existing.UpdatedAt = now

	if err := r.sess.Collection("risk_rules").Find(db.Cond{"id": existing.ID}).Update(existing); err != nil {
		return fmt.Errorf("failed to update risk rule: %w", err)
	}
	rule.ID = existing.ID
	rule.CreatedAt = existing.CreatedAt
	rule.UpdatedAt = existing.UpdatedAt
	return nil
}

func stringPtr(value string) *string {
	return &value
}
