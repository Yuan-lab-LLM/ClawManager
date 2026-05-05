package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type SecurityScanConfigPayload struct {
	ActiveMode          string                           `json:"active_mode"`
	DefaultMode         string                           `json:"default_mode"`
	QuickAnalyzers      []string                         `json:"quick_analyzers"`
	DeepAnalyzers       []string                         `json:"deep_analyzers"`
	QuickTimeoutSeconds int                              `json:"quick_timeout_seconds"`
	DeepTimeoutSeconds  int                              `json:"deep_timeout_seconds"`
	AllowFallback       bool                             `json:"allow_fallback"`
	ScannerStatus       SecurityScannerStatusPayload     `json:"scanner_status"`
	SkillScannerConfig  SkillScannerRuntimeConfigPayload `json:"skill_scanner_config"`
}

type SecurityScannerStatusPayload struct {
	Connected             bool     `json:"connected"`
	LLMEnabled            bool     `json:"llm_enabled"`
	StatusLabel           string   `json:"status_label"`
	AvailableCapabilities []string `json:"available_capabilities"`
}

type SkillScannerRuntimeConfigPayload struct {
	Namespace      string `json:"namespace"`
	DeploymentName string `json:"deployment_name"`
	LLMAPIKey      string `json:"llm_api_key"`
	LLMModel       string `json:"llm_model"`
	LLMBaseURL     string `json:"llm_base_url"`
	MetaLLMAPIKey  string `json:"meta_llm_api_key"`
	MetaLLMModel   string `json:"meta_llm_model"`
	MetaLLMBaseURL string `json:"meta_llm_base_url"`
}

type StartSecurityScanRequest struct {
	AssetType string `json:"asset_type"`
	ScanMode  string `json:"scan_mode"`
	ScanScope string `json:"scan_scope"`
	AssetID   *int   `json:"asset_id,omitempty"`
}

type securityScanScope struct {
	ScanScope string `json:"scan_scope"`
	AssetID   *int   `json:"asset_id,omitempty"`
}

type SecurityScanJobItemPayload struct {
	ID                 int                   `json:"id"`
	AssetType          string                `json:"asset_type"`
	AssetID            int                   `json:"asset_id"`
	AssetName          string                `json:"asset_name"`
	Status             string                `json:"status"`
	ProgressPct        int                   `json:"progress_pct"`
	RiskLevel          *string               `json:"risk_level,omitempty"`
	Summary            *string               `json:"summary,omitempty"`
	ScanResultID       *int                  `json:"scan_result_id,omitempty"`
	CachedResult       bool                  `json:"cached_result"`
	TriggeredAnalyzers []string              `json:"triggered_analyzers,omitempty"`
	Findings           []SkillFindingPayload `json:"findings,omitempty"`
	ErrorMessage       *string               `json:"error_message,omitempty"`
	StartedAt          *time.Time            `json:"started_at,omitempty"`
	FinishedAt         *time.Time            `json:"finished_at,omitempty"`
}

type SecurityScanReportPayload struct {
	JobID               int                          `json:"job_id"`
	AssetType           string                       `json:"asset_type"`
	ScanMode            string                       `json:"scan_mode"`
	ScanScope           string                       `json:"scan_scope"`
	Status              string                       `json:"status"`
	StartedAt           *time.Time                   `json:"started_at,omitempty"`
	FinishedAt          *time.Time                   `json:"finished_at,omitempty"`
	TotalItems          int                          `json:"total_items"`
	CompletedItems      int                          `json:"completed_items"`
	FailedItems         int                          `json:"failed_items"`
	RiskCounts          map[string]int               `json:"risk_counts"`
	FindingsSummary     []map[string]string          `json:"findings_summary"`
	ConfiguredAnalyzers []string                     `json:"configured_analyzers"`
	AvailableAnalyzers  []string                     `json:"available_analyzers"`
	TriggeredAnalyzers  []string                     `json:"triggered_analyzers"`
	Items               []SecurityScanJobItemPayload `json:"items"`
	Config              SecurityScanConfigPayload    `json:"config"`
}

type SecurityScanJobPayload struct {
	ID              int                          `json:"id"`
	AssetType       string                       `json:"asset_type"`
	ScanMode        string                       `json:"scan_mode"`
	ScanScope       string                       `json:"scan_scope"`
	Status          string                       `json:"status"`
	RequestedBy     *int                         `json:"requested_by,omitempty"`
	TotalItems      int                          `json:"total_items"`
	CompletedItems  int                          `json:"completed_items"`
	FailedItems     int                          `json:"failed_items"`
	CurrentItemName *string                      `json:"current_item_name,omitempty"`
	ProgressPct     int                          `json:"progress_pct"`
	StartedAt       *time.Time                   `json:"started_at,omitempty"`
	FinishedAt      *time.Time                   `json:"finished_at,omitempty"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
	Items           []SecurityScanJobItemPayload `json:"items,omitempty"`
	Report          *SecurityScanReportPayload   `json:"report,omitempty"`
}

type SecurityScanService interface {
	GetConfig() (*SecurityScanConfigPayload, error)
	SaveConfig(updatedBy int, req SecurityScanConfigPayload) (*SecurityScanConfigPayload, error)
	StartScan(requestedBy int, req StartSecurityScanRequest) (*SecurityScanJobPayload, error)
	RescanSkill(requestedBy, skillID int, scanMode string) (*SecurityScanJobPayload, error)
	ListJobs(limit int) ([]SecurityScanJobPayload, error)
	GetJob(jobID int) (*SecurityScanJobPayload, error)
}

type securityScanService struct {
	repo      repository.SecurityScanRepository
	skillRepo repository.SkillRepository
	storage   ObjectStorageService
	scanner   SkillScannerClient
	running   sync.Map
}

func NewSecurityScanService(repo repository.SecurityScanRepository, skillRepo repository.SkillRepository, storage ObjectStorageService, scanner SkillScannerClient) SecurityScanService {
	return &securityScanService{repo: repo, skillRepo: skillRepo, storage: storage, scanner: scanner}
}

func (s *securityScanService) GetConfig() (*SecurityScanConfigPayload, error) {
	item, err := s.ensureConfig()
	if err != nil {
		return nil, err
	}
	payload, err := s.toConfigPayload(*item)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *securityScanService) SaveConfig(updatedBy int, req SecurityScanConfigPayload) (*SecurityScanConfigPayload, error) {
	item, err := s.ensureConfig()
	if err != nil {
		return nil, err
	}
	activeMode := strings.TrimSpace(req.ActiveMode)
	if activeMode == "" {
		activeMode = strings.TrimSpace(req.DefaultMode)
	}
	if activeMode == "" {
		activeMode = "quick"
	}
	quickJSON, _ := json.Marshal(normalizeAnalyzerList(req.QuickAnalyzers, defaultQuickAnalyzers()))
	deepJSON, _ := json.Marshal(normalizeAnalyzerList(req.DeepAnalyzers, defaultDeepAnalyzers()))
	item.DefaultMode = normalizeScanMode(activeMode)
	item.QuickAnalyzersJSON = string(quickJSON)
	item.DeepAnalyzersJSON = string(deepJSON)
	item.QuickTimeoutSeconds = normalizeTimeout(req.QuickTimeoutSeconds, 30)
	item.DeepTimeoutSeconds = normalizeTimeout(req.DeepTimeoutSeconds, 120)
	item.AllowFallback = false
	item.UpdatedBy = &updatedBy
	if err := s.repo.UpsertConfig(item); err != nil {
		return nil, err
	}
	if err := s.applySkillScannerConfig(req.SkillScannerConfig); err != nil {
		return nil, err
	}
	return s.toConfigPayload(*item)
}

func (s *securityScanService) StartScan(requestedBy int, req StartSecurityScanRequest) (*SecurityScanJobPayload, error) {
	assetType := strings.TrimSpace(req.AssetType)
	if assetType == "" {
		assetType = "skill"
	}
	if assetType != "skill" {
		return nil, fmt.Errorf("unsupported asset type")
	}
	config, err := s.ensureConfig()
	if err != nil {
		return nil, err
	}
	mode := normalizeScanMode(config.DefaultMode)
	scope := normalizeScanScope(req.ScanScope)
	skills, err := s.resolveScanSkills(req.AssetID)
	if err != nil {
		return nil, err
	}
	scopePayload := securityScanScope{
		ScanScope: scope,
		AssetID:   req.AssetID,
	}
	scopeJSONBytes, _ := json.Marshal(scopePayload)
	scopeJSON := string(scopeJSONBytes)
	job := &models.SecurityScanJob{
		AssetType:      assetType,
		ScanMode:       mode,
		ScopeJSON:      &scopeJSON,
		Status:         "queued",
		RequestedBy:    &requestedBy,
		TotalItems:     len(skills),
		CompletedItems: 0,
		FailedItems:    0,
	}
	if err := s.repo.CreateJob(job); err != nil {
		return nil, err
	}
	for _, skill := range skills {
		item := &models.SecurityScanJobItem{
			JobID:       job.ID,
			AssetType:   assetType,
			AssetID:     skill.ID,
			AssetName:   skill.Name,
			Status:      "pending",
			ProgressPct: 0,
		}
		if err := s.repo.CreateJobItem(item); err != nil {
			return nil, err
		}
	}
	go s.runJob(job.ID)
	return s.GetJob(job.ID)
}

func (s *securityScanService) RescanSkill(requestedBy, skillID int, scanMode string) (*SecurityScanJobPayload, error) {
	return s.StartScan(requestedBy, StartSecurityScanRequest{
		AssetType: "skill",
		ScanMode:  scanMode,
		ScanScope: "full",
		AssetID:   &skillID,
	})
}

func (s *securityScanService) resolveScanSkills(assetID *int) ([]models.Skill, error) {
	if assetID != nil && *assetID > 0 {
		skill, err := s.skillRepo.GetSkillByID(*assetID)
		if err != nil {
			return nil, err
		}
		if skill == nil {
			return nil, fmt.Errorf("skill not found")
		}
		return []models.Skill{*skill}, nil
	}
	return s.skillRepo.ListAllSkills()
}

func (s *securityScanService) ListJobs(limit int) ([]SecurityScanJobPayload, error) {
	items, err := s.repo.ListJobs(limit)
	if err != nil {
		return nil, err
	}
	result := make([]SecurityScanJobPayload, 0, len(items))
	for _, item := range items {
		payload, err := s.toJobPayload(item, false)
		if err != nil {
			return nil, err
		}
		result = append(result, *payload)
	}
	return result, nil
}

func (s *securityScanService) GetJob(jobID int) (*SecurityScanJobPayload, error) {
	item, err := s.repo.GetJobByID(jobID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("security scan job not found")
	}
	return s.toJobPayload(*item, true)
}

func (s *securityScanService) runJob(jobID int) {
	if _, loaded := s.running.LoadOrStore(jobID, struct{}{}); loaded {
		return
	}
	defer s.running.Delete(jobID)

	job, err := s.repo.GetJobByID(jobID)
	if err != nil || job == nil {
		return
	}
	config, err := s.ensureConfig()
	if err != nil {
		return
	}
	configPayload, err := s.toConfigPayload(*config)
	if err != nil {
		return
	}
	scope := parseSecurityScanScope(job.ScopeJSON)
	now := time.Now().UTC()
	job.Status = "running"
	job.StartedAt = &now
	job.UpdatedAt = now
	_ = s.repo.UpdateJob(job)

	items, err := s.repo.ListJobItems(jobID)
	if err != nil {
		return
	}

	for _, item := range items {
		currentName := item.AssetName
		startedAt := time.Now().UTC()
		item.Status = "running"
		item.ProgressPct = 10
		item.StartedAt = &startedAt
		item.UpdatedAt = startedAt
		job.CurrentItemName = &currentName
		job.UpdatedAt = startedAt
		_ = s.repo.UpdateJobItem(&item)
		_ = s.repo.UpdateJob(job)

		skill, err := s.skillRepo.GetSkillByID(item.AssetID)
		if err != nil || skill == nil {
			s.markJobItemFailed(job, &item, "skill not found")
			continue
		}
		if skill.CurrentVersionID == nil {
			s.markJobItemFailed(job, &item, "skill has no active version")
			continue
		}
		version, err := s.skillRepo.GetVersionByID(*skill.CurrentVersionID)
		if err != nil || version == nil {
			s.markJobItemFailed(job, &item, "skill version not found")
			continue
		}
		blob, err := s.skillRepo.GetBlobByID(version.BlobID)
		if err != nil || blob == nil {
			s.markJobItemFailed(job, &item, "skill blob not found")
			continue
		}

		result, cached, err := s.scanBlob(skill, blob, job.ScanMode, scope.ScanScope, *configPayload)
		if err != nil {
			s.markJobItemFailed(job, &item, err.Error())
			continue
		}

		finishedAt := time.Now().UTC()
		item.Status = "completed"
		item.ProgressPct = 100
		item.CachedResult = cached
		item.RiskLevel = &result.RiskLevel
		item.Summary = result.Summary
		item.ScanResultID = &result.ID
		item.FinishedAt = &finishedAt
		item.UpdatedAt = finishedAt
		job.CompletedItems++
		job.CurrentItemName = &currentName
		job.UpdatedAt = finishedAt
		_ = s.repo.UpdateJobItem(&item)
		_ = s.repo.UpdateJob(job)
	}

	doneAt := time.Now().UTC()
	job.Status = "completed"
	job.CurrentItemName = nil
	job.FinishedAt = &doneAt
	job.UpdatedAt = doneAt
	_ = s.repo.UpdateJob(job)
	_ = s.generateReport(job.ID, *config)
}

func (s *securityScanService) markJobItemFailed(job *models.SecurityScanJob, item *models.SecurityScanJobItem, message string) {
	finishedAt := time.Now().UTC()
	item.Status = "failed"
	item.ProgressPct = 100
	item.ErrorMessage = optionalString(message)
	item.FinishedAt = &finishedAt
	item.UpdatedAt = finishedAt
	job.CompletedItems++
	job.FailedItems++
	job.UpdatedAt = finishedAt
	_ = s.repo.UpdateJobItem(item)
	_ = s.repo.UpdateJob(job)
}

func (s *securityScanService) scanBlob(skill *models.Skill, blob *models.SkillBlob, mode, scope string, config SecurityScanConfigPayload) (*models.SkillScanResult, bool, error) {
	latest, err := s.skillRepo.GetLatestScanResultByBlobID(blob.ID)
	if err != nil {
		return nil, false, err
	}
	if scope == "incremental" && latest != nil && latest.Status == "completed" && strings.EqualFold(strings.TrimSpace(latest.Engine), "skill-scanner") {
		return latest, true, nil
	}
	if strings.TrimSpace(blob.ObjectKey) == "" || blob.SizeBytes <= 0 {
		now := time.Now().UTC()
		blob.ScanStatus = "pending"
		blob.UpdatedAt = now
		_ = s.skillRepo.UpdateBlob(blob)
		return nil, false, fmt.Errorf("skill package has not been collected from agent yet")
	}

	content, err := s.storage.GetObject(context.Background(), blob.ObjectKey)
	if err != nil {
		now := time.Now().UTC()
		blob.ScanStatus = "pending"
		blob.UpdatedAt = now
		_ = s.skillRepo.UpdateBlob(blob)
		if strings.Contains(strings.ToLower(err.Error()), "specified key does not exist") {
			return nil, false, fmt.Errorf("skill package has not been collected from agent yet")
		}
		return nil, false, err
	}
	timeout := config.QuickTimeoutSeconds
	if mode == "deep" {
		timeout = config.DeepTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	options := map[string]string{
		"scan_mode": mode,
	}
	analyzers := config.QuickAnalyzers
	if mode == "deep" {
		analyzers = config.DeepAnalyzers
	}
	if len(analyzers) > 0 {
		options["analyzers"] = strings.Join(analyzers, ",")
	}

	if s.scanner == nil {
		return nil, false, fmt.Errorf("skill scanner is not configured")
	}
	riskLevel, findings, summary, err := s.scanner.ScanArchive(ctx, blob.FileName, content, options)
	if err != nil {
		now := time.Now().UTC()
		blob.ScanStatus = "failed"
		blob.UpdatedAt = now
		_ = s.skillRepo.UpdateBlob(blob)
		return nil, false, fmt.Errorf("skill scanner failed: %w", err)
	}
	if strings.TrimSpace(summary) == "" {
		summary = "Skill scanned by external skill-scanner service"
	}

	scannedAt := time.Now().UTC()
	findingsJSON, _ := json.Marshal(findings)
	result := &models.SkillScanResult{
		BlobID: blob.ID, Engine: "skill-scanner", RiskLevel: riskLevel, Status: "completed",
		Summary: &summary, FindingsJSON: optionalString(string(findingsJSON)), ScannedAt: &scannedAt,
	}
	if err := s.skillRepo.CreateScanResult(result); err != nil {
		return nil, false, err
	}
	blob.ScanStatus = "completed"
	blob.RiskLevel = riskLevel
	blob.LastScannedAt = &scannedAt
	blob.LastScanResultID = &result.ID
	if err := s.skillRepo.UpdateBlob(blob); err != nil {
		return nil, false, err
	}
	if skill != nil {
		skill.RiskLevel = riskLevel
		skill.LastScannedAt = &scannedAt
		skill.LastScanResultID = &result.ID
		skill.UpdatedAt = scannedAt
		_ = s.skillRepo.UpdateSkill(skill)
	}
	return result, false, nil
}

func (s *securityScanService) generateReport(jobID int, config models.SecurityScanConfig) error {
	job, err := s.repo.GetJobByID(jobID)
	if err != nil || job == nil {
		return err
	}
	jobItems, err := s.repo.ListJobItems(jobID)
	if err != nil {
		return err
	}
	cfgPayload, err := s.toConfigPayload(config)
	if err != nil {
		return err
	}
	riskCounts := map[string]int{}
	findingsSummary := make([]map[string]string, 0, len(jobItems))
	itemPayloads := make([]SecurityScanJobItemPayload, 0, len(jobItems))
	triggeredAnalyzers := map[string]struct{}{}
	for _, item := range jobItems {
		if item.RiskLevel != nil {
			riskCounts[*item.RiskLevel]++
		}
		itemPayload := toSecurityScanJobItemPayload(item)
		if item.ScanResultID != nil {
			result, err := s.skillRepo.GetScanResultByID(*item.ScanResultID)
			if err != nil {
				return err
			}
			findings := parseSkillFindings(result)
			itemPayload.TriggeredAnalyzers = extractTriggeredAnalyzers(result)
			itemPayload.Findings = topRiskFindings(findings, 5)
			if summary := summarizeScanJobItem(item, findings); summary != nil {
				itemPayload.Summary = summary
				findingsSummary = append(findingsSummary, map[string]string{
					"asset_name": item.AssetName,
					"summary":    *summary,
				})
			}
			for _, analyzer := range itemPayload.TriggeredAnalyzers {
				triggeredAnalyzers[analyzer] = struct{}{}
			}
		} else if summary := summarizeScanJobItem(item, nil); summary != nil {
			itemPayload.Summary = summary
			findingsSummary = append(findingsSummary, map[string]string{
				"asset_name": item.AssetName,
				"summary":    *summary,
			})
		}
		itemPayloads = append(itemPayloads, itemPayload)
	}
	configuredAnalyzers := cfgPayload.QuickAnalyzers
	if job.ScanMode == "deep" {
		configuredAnalyzers = cfgPayload.DeepAnalyzers
	}
	availableAnalyzers := []string{}
	if s.scanner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if analyzers, err := s.scanner.AvailableAnalyzers(ctx); err == nil {
			availableAnalyzers = normalizeAnalyzerList(analyzers, []string{})
		}
	}
	reportPayload := SecurityScanReportPayload{
		JobID:               job.ID,
		AssetType:           job.AssetType,
		ScanMode:            job.ScanMode,
		ScanScope:           parseSecurityScanScope(job.ScopeJSON).ScanScope,
		Status:              job.Status,
		StartedAt:           job.StartedAt,
		FinishedAt:          job.FinishedAt,
		TotalItems:          job.TotalItems,
		CompletedItems:      job.CompletedItems,
		FailedItems:         job.FailedItems,
		RiskCounts:          riskCounts,
		FindingsSummary:     findingsSummary,
		ConfiguredAnalyzers: append([]string{}, configuredAnalyzers...),
		AvailableAnalyzers:  availableAnalyzers,
		TriggeredAnalyzers:  sortedAnalyzerKeys(triggeredAnalyzers),
		Items:               itemPayloads,
		Config:              *cfgPayload,
	}
	summaryJSON, _ := json.Marshal(reportPayload)
	return s.repo.UpsertReport(&models.SecurityScanReport{
		JobID:       job.ID,
		SummaryJSON: string(summaryJSON),
	})
}

func (s *securityScanService) ensureConfig() (*models.SecurityScanConfig, error) {
	item, err := s.repo.GetConfig()
	if err != nil {
		return nil, err
	}
	if item != nil {
		return item, nil
	}
	quickJSON, _ := json.Marshal(defaultQuickAnalyzers())
	deepJSON, _ := json.Marshal(defaultDeepAnalyzers())
	item = &models.SecurityScanConfig{
		ID:                  1,
		DefaultMode:         "quick",
		QuickAnalyzersJSON:  string(quickJSON),
		DeepAnalyzersJSON:   string(deepJSON),
		QuickTimeoutSeconds: 30,
		DeepTimeoutSeconds:  120,
		AllowFallback:       false,
	}
	if err := s.repo.UpsertConfig(item); err != nil {
		return nil, err
	}
	return item, nil
}

func summarizeScanJobItem(item models.SecurityScanJobItem, findings []SkillFindingPayload) *string {
	if summary := summarizeRiskReason(topRiskFindings(findings, 2)); summary != nil {
		return summary
	}
	if item.ErrorMessage != nil && strings.TrimSpace(*item.ErrorMessage) != "" {
		message := strings.TrimSpace(*item.ErrorMessage)
		return &message
	}
	if item.RiskLevel != nil {
		switch strings.ToLower(strings.TrimSpace(*item.RiskLevel)) {
		case "none":
			text := "未发现风险项"
			return &text
		case "low":
			text := "发现低风险提示，建议查看逐项结果"
			return &text
		case "medium":
			text := "发现中风险问题，建议尽快整改"
			return &text
		case "high":
			text := "发现高风险问题，建议立即处置"
			return &text
		}
	}
	if item.Status == "completed" {
		text := "扫描完成，未返回详细发现"
		return &text
	}
	if item.Summary != nil && strings.TrimSpace(*item.Summary) != "" {
		text := strings.TrimSpace(*item.Summary)
		return &text
	}
	return nil
}

func (s *securityScanService) toConfigPayload(item models.SecurityScanConfig) (*SecurityScanConfigPayload, error) {
	var quickAnalyzers []string
	var deepAnalyzers []string
	if strings.TrimSpace(item.QuickAnalyzersJSON) != "" {
		if err := json.Unmarshal([]byte(item.QuickAnalyzersJSON), &quickAnalyzers); err != nil {
			return nil, fmt.Errorf("failed to decode quick analyzers: %w", err)
		}
	}
	if strings.TrimSpace(item.DeepAnalyzersJSON) != "" {
		if err := json.Unmarshal([]byte(item.DeepAnalyzersJSON), &deepAnalyzers); err != nil {
			return nil, fmt.Errorf("failed to decode deep analyzers: %w", err)
		}
	}
	return &SecurityScanConfigPayload{
		ActiveMode:          item.DefaultMode,
		DefaultMode:         item.DefaultMode,
		QuickAnalyzers:      normalizeAnalyzerList(quickAnalyzers, defaultQuickAnalyzers()),
		DeepAnalyzers:       normalizeAnalyzerList(deepAnalyzers, defaultDeepAnalyzers()),
		QuickTimeoutSeconds: item.QuickTimeoutSeconds,
		DeepTimeoutSeconds:  item.DeepTimeoutSeconds,
		AllowFallback:       item.AllowFallback,
		ScannerStatus:       resolveSecurityScannerStatus(),
		SkillScannerConfig:  resolveSkillScannerRuntimeConfig(),
	}, nil
}

func resolveSecurityScannerStatus() SecurityScannerStatusPayload {
	runtime := resolveSkillScannerRuntimeConfig()
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv("SKILL_SCANNER_ENABLED")), "true") &&
		strings.TrimSpace(os.Getenv("SKILL_SCANNER_BASE_URL")) != ""
	llmConfigured := strings.TrimSpace(runtime.LLMModel) != "" && strings.TrimSpace(runtime.LLMBaseURL) != "" && strings.TrimSpace(runtime.LLMAPIKey) != "" ||
		(strings.TrimSpace(runtime.MetaLLMModel) != "" && strings.TrimSpace(runtime.MetaLLMBaseURL) != "" && strings.TrimSpace(runtime.MetaLLMAPIKey) != "")

	status := SecurityScannerStatusPayload{
		Connected:             enabled,
		LLMEnabled:            llmConfigured,
		StatusLabel:           "未启用",
		AvailableCapabilities: []string{},
	}
	if !enabled {
		return status
	}
	status.StatusLabel = "静态扫描可用"
	status.AvailableCapabilities = []string{"静态扫描"}
	if llmConfigured {
		status.StatusLabel = "静态 + LLM 扫描可用"
		status.AvailableCapabilities = []string{"静态扫描", "LLM 扫描"}
	}
	return status
}

func resolveSkillScannerRuntimeConfig() SkillScannerRuntimeConfigPayload {
	cfg := SkillScannerRuntimeConfigPayload{
		Namespace:      skillScannerNamespace(),
		DeploymentName: skillScannerDeploymentName(),
	}
	if envs, err := loadSkillScannerDeploymentEnv(); err == nil {
		cfg.LLMAPIKey = envs["SKILL_SCANNER_LLM_API_KEY"]
		cfg.LLMModel = envs["SKILL_SCANNER_LLM_MODEL"]
		cfg.LLMBaseURL = envs["SKILL_SCANNER_LLM_BASE_URL"]
		cfg.MetaLLMAPIKey = envs["SKILL_SCANNER_META_LLM_API_KEY"]
		cfg.MetaLLMModel = envs["SKILL_SCANNER_META_LLM_MODEL"]
		cfg.MetaLLMBaseURL = envs["SKILL_SCANNER_META_LLM_BASE_URL"]
		return cfg
	}
	cfg.LLMAPIKey = os.Getenv("SKILL_SCANNER_LLM_API_KEY")
	cfg.LLMModel = os.Getenv("SKILL_SCANNER_LLM_MODEL")
	cfg.LLMBaseURL = os.Getenv("SKILL_SCANNER_LLM_BASE_URL")
	cfg.MetaLLMAPIKey = os.Getenv("SKILL_SCANNER_META_LLM_API_KEY")
	cfg.MetaLLMModel = os.Getenv("SKILL_SCANNER_META_LLM_MODEL")
	cfg.MetaLLMBaseURL = os.Getenv("SKILL_SCANNER_META_LLM_BASE_URL")
	return cfg
}

func (s *securityScanService) applySkillScannerConfig(req SkillScannerRuntimeConfigPayload) error {
	client := k8s.GetClient()
	if client == nil || client.Clientset == nil {
		return fmt.Errorf("kubernetes client is unavailable, cannot update skill-scanner config")
	}
	namespace := skillScannerNamespace()
	deploymentName := skillScannerDeploymentName()
	deployment, err := client.Clientset.AppsV1().Deployments(namespace).Get(context.Background(), deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to load skill-scanner deployment: %w", err)
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("skill-scanner deployment has no containers")
	}
	container := &deployment.Spec.Template.Spec.Containers[0]
	upsertEnvVar(container, "SKILL_SCANNER_LLM_API_KEY", strings.TrimSpace(req.LLMAPIKey))
	upsertEnvVar(container, "SKILL_SCANNER_LLM_MODEL", strings.TrimSpace(req.LLMModel))
	upsertEnvVar(container, "SKILL_SCANNER_LLM_BASE_URL", strings.TrimSpace(req.LLMBaseURL))
	upsertEnvVar(container, "SKILL_SCANNER_META_LLM_API_KEY", strings.TrimSpace(req.MetaLLMAPIKey))
	upsertEnvVar(container, "SKILL_SCANNER_META_LLM_MODEL", strings.TrimSpace(req.MetaLLMModel))
	upsertEnvVar(container, "SKILL_SCANNER_META_LLM_BASE_URL", strings.TrimSpace(req.MetaLLMBaseURL))

	updated, err := client.Clientset.AppsV1().Deployments(namespace).Update(context.Background(), deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update skill-scanner deployment: %w", err)
	}
	return waitForSkillScannerRollout(context.Background(), client.Clientset, namespace, deploymentName, updated.Generation, 90*time.Second)
}

func loadSkillScannerDeploymentEnv() (map[string]string, error) {
	client := k8s.GetClient()
	if client == nil || client.Clientset == nil {
		return nil, fmt.Errorf("kubernetes client is unavailable")
	}
	deployment, err := client.Clientset.AppsV1().Deployments(skillScannerNamespace()).Get(context.Background(), skillScannerDeploymentName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("skill-scanner deployment has no containers")
	}
	envs := map[string]string{}
	for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
		envs[env.Name] = env.Value
	}
	return envs, nil
}

func upsertEnvVar(container *corev1.Container, name, value string) {
	for i := range container.Env {
		if container.Env[i].Name == name {
			container.Env[i].Value = value
			container.Env[i].ValueFrom = nil
			return
		}
	}
	container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
}

func waitForSkillScannerRollout(ctx context.Context, clientset kubernetes.Interface, namespace, deploymentName string, generation int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to observe skill-scanner rollout: %w", err)
		}
		replicas := int32(1)
		if deployment.Spec.Replicas != nil {
			replicas = *deployment.Spec.Replicas
		}
		if deployment.Status.ObservedGeneration >= generation &&
			deployment.Status.UpdatedReplicas == replicas &&
			deployment.Status.AvailableReplicas == replicas &&
			deployment.Status.UnavailableReplicas == 0 {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("skill-scanner rollout timed out")
}

func skillScannerNamespace() string {
	if value := strings.TrimSpace(os.Getenv("SKILL_SCANNER_NAMESPACE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(readInClusterNamespace()); value != "" {
		return value
	}
	return "clawmanager-system"
}

func skillScannerDeploymentName() string {
	if value := strings.TrimSpace(os.Getenv("SKILL_SCANNER_DEPLOYMENT")); value != "" {
		return value
	}
	return "skill-scanner"
}

func readInClusterNamespace() string {
	data, err := os.ReadFile(filepath.Clean("/var/run/secrets/kubernetes.io/serviceaccount/namespace"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s *securityScanService) toJobPayload(item models.SecurityScanJob, includeDetails bool) (*SecurityScanJobPayload, error) {
	payload := &SecurityScanJobPayload{
		ID:              item.ID,
		AssetType:       item.AssetType,
		ScanMode:        item.ScanMode,
		ScanScope:       parseSecurityScanScope(item.ScopeJSON).ScanScope,
		Status:          item.Status,
		RequestedBy:     item.RequestedBy,
		TotalItems:      item.TotalItems,
		CompletedItems:  item.CompletedItems,
		FailedItems:     item.FailedItems,
		CurrentItemName: item.CurrentItemName,
		ProgressPct:     computeProgress(item.TotalItems, item.CompletedItems),
		StartedAt:       item.StartedAt,
		FinishedAt:      item.FinishedAt,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
	if !includeDetails {
		return payload, nil
	}
	items, err := s.repo.ListJobItems(item.ID)
	if err != nil {
		return nil, err
	}
	payload.Items = make([]SecurityScanJobItemPayload, 0, len(items))
	for _, entry := range items {
		payload.Items = append(payload.Items, toSecurityScanJobItemPayload(entry))
	}
	report, err := s.repo.GetReportByJobID(item.ID)
	if err != nil {
		return nil, err
	}
	if report != nil && strings.TrimSpace(report.SummaryJSON) != "" {
		var reportPayload SecurityScanReportPayload
		if err := json.Unmarshal([]byte(report.SummaryJSON), &reportPayload); err == nil {
			payload.Report = &reportPayload
		}
	}
	return payload, nil
}

func toSecurityScanJobItemPayload(item models.SecurityScanJobItem) SecurityScanJobItemPayload {
	return SecurityScanJobItemPayload{
		ID:                 item.ID,
		AssetType:          item.AssetType,
		AssetID:            item.AssetID,
		AssetName:          item.AssetName,
		Status:             item.Status,
		ProgressPct:        item.ProgressPct,
		RiskLevel:          item.RiskLevel,
		Summary:            item.Summary,
		ScanResultID:       item.ScanResultID,
		CachedResult:       item.CachedResult,
		TriggeredAnalyzers: []string{},
		Findings:           []SkillFindingPayload{},
		ErrorMessage:       item.ErrorMessage,
		StartedAt:          item.StartedAt,
		FinishedAt:         item.FinishedAt,
	}
}

func extractTriggeredAnalyzers(result *models.SkillScanResult) []string {
	if result == nil || result.FindingsJSON == nil || strings.TrimSpace(*result.FindingsJSON) == "" {
		return []string{}
	}
	var raw struct {
		Findings []struct {
			Analyzer string `json:"analyzer"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(*result.FindingsJSON), &raw); err != nil {
		return []string{}
	}
	seen := map[string]struct{}{}
	resultList := make([]string, 0, len(raw.Findings))
	for _, item := range raw.Findings {
		analyzer := strings.TrimSpace(strings.ToLower(item.Analyzer))
		if analyzer == "" {
			continue
		}
		if _, ok := seen[analyzer]; ok {
			continue
		}
		seen[analyzer] = struct{}{}
		resultList = append(resultList, analyzer)
	}
	return resultList
}

func sortedAnalyzerKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func defaultQuickAnalyzers() []string {
	return []string{"static", "behavioral", "trigger"}
}

func defaultDeepAnalyzers() []string {
	return []string{"static", "bytecode", "pipeline", "behavioral", "trigger", "llm", "meta"}
}

func normalizeAnalyzerList(values []string, fallback []string) []string {
	if len(values) == 0 {
		return append([]string{}, fallback...)
	}
	allowed := map[string]struct{}{
		"static":     {},
		"bytecode":   {},
		"pipeline":   {},
		"behavioral": {},
		"trigger":    {},
		"llm":        {},
		"meta":       {},
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		if _, ok := allowed[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return append([]string{}, fallback...)
	}
	return result
}

func normalizeScanMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "deep") {
		return "deep"
	}
	return "quick"
}

func normalizeScanScope(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "full") {
		return "full"
	}
	return "incremental"
}

func parseSecurityScanScope(raw *string) securityScanScope {
	scope := securityScanScope{ScanScope: "incremental"}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return scope
	}
	if err := json.Unmarshal([]byte(*raw), &scope); err != nil {
		return securityScanScope{ScanScope: "incremental"}
	}
	scope.ScanScope = normalizeScanScope(scope.ScanScope)
	return scope
}

func normalizeTimeout(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func computeProgress(total, completed int) int {
	if total <= 0 {
		return 0
	}
	if completed >= total {
		return 100
	}
	return int(float64(completed) / float64(total) * 100)
}
