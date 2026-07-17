package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type runtimeSkillSyncDeps struct {
	bindingRepo        repository.InstanceRuntimeBindingRepository
	runtimePodRepo     repository.RuntimePodRepository
	runtimeAgentClient RuntimeAgentClient
}

func (s *skillService) ConfigureRuntimeSkillSync(bindingRepo repository.InstanceRuntimeBindingRepository, runtimePodRepo repository.RuntimePodRepository, agentClient RuntimeAgentClient) {
	if s == nil {
		return
	}
	s.runtimeSkillSync = &runtimeSkillSyncDeps{
		bindingRepo:        bindingRepo,
		runtimePodRepo:     runtimePodRepo,
		runtimeAgentClient: agentClient,
	}
}

func ConfigureSkillRuntimeSync(service SkillService, bindingRepo repository.InstanceRuntimeBindingRepository, runtimePodRepo repository.RuntimePodRepository, agentClient RuntimeAgentClient) {
	if impl, ok := service.(*skillService); ok {
		impl.ConfigureRuntimeSkillSync(bindingRepo, runtimePodRepo, agentClient)
	}
}

func (s *skillService) SyncRuntimeAgentSkillsReport(payload map[string]any) error {
	if s == nil {
		return fmt.Errorf("skill service is not configured")
	}
	reports, mode, reportedAt, err := parseRuntimeAgentSkillsReport(payload)
	if err != nil {
		return err
	}
	for _, report := range reports {
		if report.InstanceID <= 0 {
			continue
		}
		skills := make([]AgentSkillRecord, 0, len(report.Skills))
		for _, record := range report.Skills {
			record.Source = normalizeRuntimeSkillSource(record.Source)
			skills = append(skills, record)
		}
		req := AgentSkillInventoryReportRequest{
			AgentID:    fmt.Sprintf("runtime-instance-%d", report.InstanceID),
			ReportedAt: reportedAt,
			Mode:       mode,
			Trigger:    "runtime_agent_report",
			Skills:     skills,
		}
		if err := s.SyncAgentSkills(report.InstanceID, req); err != nil {
			return err
		}
		s.completePendingSkillInventorySync(report.InstanceID)
	}
	return nil
}

func (s *skillService) RequestLiteSkillInventorySync(instanceID int) error {
	if s == nil || s.instanceRepo == nil {
		return fmt.Errorf("skill service is not configured")
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	if err := EnsureInstanceWorkspacePathForServerScan(context.Background(), s.instanceRepo, instance); err != nil {
		return err
	}
	if !isLiteRuntimeInstance(instance) && !SupportsServerWorkspaceSkillScan(instance) {
		return fmt.Errorf("instance does not support workspace skill inventory sync")
	}

	if SupportsServerWorkspaceSkillScan(instance) {
		workspaceMode := "full"
		willResyncAgent := isLiteRuntimeInstance(instance) && s.runtimeSkillSync != nil
		if willResyncAgent {
			workspaceMode = "incremental"
		}
		if err := s.syncRuntimeSkillsFromWorkspace(instanceID, workspaceMode); err != nil {
			return err
		}
		if !willResyncAgent {
			s.completePendingSkillInventorySync(instanceID)
		}
	}

	if !isLiteRuntimeInstance(instance) || s.runtimeSkillSync == nil {
		return nil
	}
	deps := s.runtimeSkillSync
	if deps.bindingRepo == nil || deps.runtimePodRepo == nil || deps.runtimeAgentClient == nil {
		return nil
	}

	ctx := context.Background()
	binding, err := deps.bindingRepo.GetRunningByInstanceID(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to resolve runtime binding: %w", err)
	}
	if binding == nil {
		binding, err = deps.bindingRepo.GetByInstanceID(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("failed to resolve runtime binding: %w", err)
		}
	}
	if binding == nil || binding.Generation != instance.RuntimeGeneration {
		return nil
	}
	runtimePod, err := deps.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return fmt.Errorf("failed to resolve runtime pod: %w", err)
	}
	if runtimePod != nil && runtimePod.AgentEndpoint != nil && strings.TrimSpace(*runtimePod.AgentEndpoint) != "" {
		if err := deps.runtimeAgentClient.ResyncInstanceSkills(ctx, strings.TrimSpace(*runtimePod.AgentEndpoint), instanceID, "full"); err != nil {
			return fmt.Errorf("failed to request runtime skill inventory resync: %w", err)
		}
	}
	return nil
}

func (s *skillService) syncRuntimeSkillsFromWorkspace(instanceID int, mode string) error {
	if s == nil || s.instanceRepo == nil {
		return fmt.Errorf("skill service is not configured")
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	if err := EnsureInstanceWorkspacePathForServerScan(context.Background(), s.instanceRepo, instance); err != nil {
		return err
	}
	if !isLiteRuntimeInstance(instance) && !SupportsServerWorkspaceSkillScan(instance) {
		return fmt.Errorf("instance does not support workspace skill inventory sync")
	}

	root := runtimeSkillInstallRoot(instance)
	if root == "" {
		return fmt.Errorf("runtime skill workspace root is not configured")
	}

	records := make([]AgentSkillRecord, 0)
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return s.SyncAgentSkills(instanceID, runtimeWorkspaceSkillInventoryRequest(instanceID, mode, records))
		}
		return fmt.Errorf("failed to inspect runtime skill directory: %w", err)
	}
	discoveries, err := discoverRuntimeSkillDirectories(root, runtimeSkillDiscoveryMaxDepth)
	if err != nil {
		return fmt.Errorf("failed to scan runtime skill directory: %w", err)
	}

	for _, discovery := range discoveries {
		files, err := collectLiteSkillDirectoryFiles(discovery.SkillRoot)
		if err != nil {
			return fmt.Errorf("failed to scan runtime skill %q: %w", discovery.RelativePath, err)
		}
		if len(files) == 0 {
			continue
		}
		records = append(records, AgentSkillRecord{
			Identifier:  discovery.RelativePath,
			InstallPath: runtimeSkillInstallRelativePath(instance, discovery.RelativePath),
			ContentMD5:  hashDirectory(files),
			Source:      "discovered_in_instance",
			Type:        "agent-skill",
		})
	}

	return s.SyncAgentSkills(instanceID, runtimeWorkspaceSkillInventoryRequest(instanceID, mode, records))
}

func (s *skillService) syncLiteSkillsFromWorkspace(instanceID int) error {
	return s.syncRuntimeSkillsFromWorkspace(instanceID, "full")
}

func runtimeWorkspaceSkillInventoryRequest(instanceID int, mode string, records []AgentSkillRecord) AgentSkillInventoryReportRequest {
	now := time.Now().UTC()
	normalizedMode := strings.TrimSpace(mode)
	if normalizedMode == "" {
		normalizedMode = "full"
	}
	return AgentSkillInventoryReportRequest{
		AgentID:    fmt.Sprintf("workspace-scan-instance-%d", instanceID),
		ReportedAt: &now,
		Mode:       normalizedMode,
		Trigger:    "runtime_workspace_scan",
		Skills:     records,
	}
}

func liteWorkspaceSkillInventoryRequest(instanceID int, records []AgentSkillRecord) AgentSkillInventoryReportRequest {
	return runtimeWorkspaceSkillInventoryRequest(instanceID, "full", records)
}

func liteSkillInstallRelativePath(instance *models.Instance, skillName string) string {
	return runtimeSkillInstallRelativePath(instance, skillName)
}

func collectLiteSkillDirectoryFiles(skillRoot string) (map[string][]byte, error) {
	manifestPath := filepath.Join(skillRoot, "SKILL.md")
	info, err := os.Stat(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, nil
	}

	files := map[string][]byte{}
	err = filepath.WalkDir(skillRoot, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(skillRoot, current)
		if err != nil {
			return err
		}
		rel = normalizeSkillRelPath(filepath.ToSlash(rel))
		if rel == "" || hasHiddenPathSegment(rel) {
			return nil
		}
		body, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		files[rel] = body
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

type runtimeAgentInstanceSkillReport struct {
	InstanceID int
	Skills     []AgentSkillRecord
}

func parseRuntimeAgentSkillsReport(payload map[string]any) ([]runtimeAgentInstanceSkillReport, string, *time.Time, error) {
	if payload == nil {
		return nil, "", nil, fmt.Errorf("skills report payload is required")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to encode skills report payload: %w", err)
	}
	var decoded struct {
		Mode       string     `json:"mode"`
		ReportedAt *time.Time `json:"reported_at"`
		Instances  []struct {
			InstanceID int                `json:"instance_id"`
			Skills     []AgentSkillRecord `json:"skills"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, "", nil, fmt.Errorf("failed to decode skills report payload: %w", err)
	}
	mode := strings.TrimSpace(decoded.Mode)
	if mode == "" {
		mode = "full"
	}
	reports := make([]runtimeAgentInstanceSkillReport, 0, len(decoded.Instances))
	for _, item := range decoded.Instances {
		reports = append(reports, runtimeAgentInstanceSkillReport{
			InstanceID: item.InstanceID,
			Skills:     item.Skills,
		})
	}
	return reports, mode, decoded.ReportedAt, nil
}

func normalizeRuntimeSkillSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "runtime", "discovered", "agent-skill", "agent_skill":
		return "discovered_in_instance"
	default:
		return normalizeSkillSource(value)
	}
}

func (s *skillService) CompletePendingSkillInventorySync(instanceID int) {
	s.completePendingSkillInventorySync(instanceID)
}

func (s *skillService) completePendingSkillInventorySync(instanceID int) {
	if s == nil || s.commandRepo == nil || instanceID <= 0 {
		return
	}
	commands, err := s.commandRepo.ListByInstanceID(instanceID, 20)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, command := range commands {
		if command.CommandType != InstanceCommandTypeSyncSkillInventory {
			continue
		}
		switch strings.TrimSpace(command.Status) {
		case instanceCommandStatusPending, instanceCommandStatusDispatched, instanceCommandStatusRunning:
			command.Status = instanceCommandStatusSucceeded
			command.FinishedAt = &now
			command.UpdatedAt = now
			_ = s.commandRepo.Update(&command)
			return
		}
	}
}

func SupportsServerWorkspaceSkillScan(instance *models.Instance) bool {
	if instance == nil || instance.WorkspacePath == nil {
		return false
	}
	if strings.TrimSpace(*instance.WorkspacePath) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(instance.Type)) {
	case "hermes", "openclaw":
		return true
	default:
		return false
	}
}

func IsLiteRuntimeInstance(instance *models.Instance) bool {
	return isLiteRuntimeInstance(instance)
}
