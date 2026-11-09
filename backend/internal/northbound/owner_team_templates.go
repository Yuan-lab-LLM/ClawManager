package northbound

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/teamtemplate"
	"github.com/gin-gonic/gin"
	"github.com/upper/db/v4"
)

//go:embed team_catalog.json
var ownerTeamCatalogJSON []byte

type ownerTeamCatalogEntry struct {
	Key               string                             `json:"key"`
	Revision          int                                `json:"revision"`
	Name              string                             `json:"name"`
	Description       string                             `json:"description"`
	CommunicationMode string                             `json:"communication_mode"`
	Members           []services.CreateTeamMemberRequest `json:"members"`
}

func ownerTeamCatalog() []ownerTeamCatalogEntry {
	var entries []ownerTeamCatalogEntry
	_ = json.Unmarshal(ownerTeamCatalogJSON, &entries)
	return entries
}

func (a *OwnerTeamAPI) catalog(c *gin.Context, action string) {
	entries := ownerTeamCatalog()
	if action == "catalog.list" {
		ownerTeamSuccess(c, gin.H{"templates": entries})
		return
	}
	for _, entry := range entries {
		if entry.Key == c.Param("key") {
			ownerTeamSuccess(c, entry)
			return
		}
	}
	writeError(c, apiError(404, "TEMPLATE_NOT_FOUND", "内置模板不存在", nil))
}

func (a *OwnerTeamAPI) submitCreate(c *gin.Context, identity OwnerTeamIdentity) {
	var req ownerTeamCreateRequest
	if !ownerTeamBind(c, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 100 || req.Description != nil && len(*req.Description) > 2000 || (req.TemplateID > 0) == (req.TemplateKey != "") {
		writeError(c, apiError(422, "VALIDATION_ERROR", "请填写团队名称并选择一个模板", nil))
		return
	}
	body, _ := json.Marshal(req)
	stored := ownerTeamStoredRequest{Action: "teams.create", Body: body, TemplateID: req.TemplateID, ExpectedRevision: req.Revision}
	if a.replayCreate(c, identity, stored) {
		return
	}
	uid, err := a.creationUser(identity)
	if err != nil {
		writeError(c, err)
		return
	}
	frozen := services.CreateTeamRequest{Name: req.Name, Description: req.Description, CommunicationMode: req.CommunicationMode}
	if req.TemplateKey != "" {
		for _, entry := range ownerTeamCatalog() {
			if entry.Key == req.TemplateKey && entry.Revision == req.Revision {
				frozen.Members = entry.Members
				if frozen.CommunicationMode == "" {
					frozen.CommunicationMode = entry.CommunicationMode
				}
				break
			}
		}
	} else {
		item, err := a.templateRecord(identity, req.TemplateID)
		if err != nil {
			writeError(c, err)
			return
		}
		if item.UserID != uid || item.Revision != req.Revision {
			writeError(c, apiError(409, "TEMPLATE_REVISION_CONFLICT", "模板账号或版本已变化，请重新选择", nil))
			return
		}
		var spec teamtemplate.TemplateSpec
		if json.Unmarshal([]byte(item.SpecJSON), &spec) != nil {
			writeError(c, apiError(409, "TEMPLATE_INVALID", "模板内容不可用", nil))
			return
		}
		for _, m := range spec.Members {
			profile := &services.TeamMemberRoleProfileRequest{SchemaVersion: spec.SchemaVersion, ProfileKey: m.MemberID, DisplayName: m.DisplayName, RoleHint: m.Role, Summary: m.Summary, Mission: m.Mission, Responsibilities: m.Responsibilities, Boundaries: m.Boundaries, ExpectedInputs: m.ExpectedInputs, Deliverables: m.Deliverables, AcceptanceCriteria: m.AcceptanceCriteria, CollaborationNotes: m.CollaborationNotes, CapabilityTags: m.CapabilityTags}
			description := m.Summary
			frozen.Members = append(frozen.Members, services.CreateTeamMemberRequest{MemberID: m.MemberID, Name: m.DisplayName, IconKey: m.IconKey, Role: m.Role, IsLeader: m.IsLeader, RuntimeType: "openclaw", InstanceMode: "lite", Description: &description, RoleProfile: profile})
		}
	}
	if len(frozen.Members) == 0 {
		writeError(c, apiError(409, "TEMPLATE_REVISION_CONFLICT", "模板不存在或版本已变化", nil))
		return
	}
	if frozen.CommunicationMode == "" {
		frozen.CommunicationMode = "leader_mediated"
	}
	if frozen.CommunicationMode != "leader_mediated" && frozen.CommunicationMode != "peer_assisted" && frozen.CommunicationMode != "full_mesh" {
		writeError(c, apiError(422, "VALIDATION_ERROR", "不支持的协作模式", nil))
		return
	}
	overrides := map[string]struct{ runtime, mode string }{}
	for _, m := range req.Members {
		if _, exists := overrides[m.MemberID]; exists {
			writeError(c, apiError(422, "VALIDATION_ERROR", "成员配置重复", nil))
			return
		}
		overrides[m.MemberID] = struct{ runtime, mode string }{m.RuntimeType, m.InstanceMode}
	}
	for i := range frozen.Members {
		m := &frozen.Members[i]
		if v, ok := overrides[m.MemberID]; ok {
			if v.runtime != "" {
				m.RuntimeType = v.runtime
			}
			if v.mode != "" {
				m.InstanceMode = v.mode
			}
			delete(overrides, m.MemberID)
		}
		if m.RuntimeType == "" {
			m.RuntimeType = "openclaw"
		}
		if m.InstanceMode == "" {
			m.InstanceMode = "lite"
		}
		if (m.RuntimeType != "openclaw" && m.RuntimeType != "hermes") || (m.InstanceMode != "lite" && m.InstanceMode != "pro") || m.IsLeader && m.RuntimeType != "openclaw" {
			writeError(c, apiError(422, "RUNTIME_DISABLED", "不支持的成员 Runtime 或模式", nil))
			return
		}
		if a.Settings != nil {
			policy := a.Settings.Current()
			allowed := policy.AllowedLiteTypes
			m.CPUCores, m.MemoryGB, m.DiskGB = policy.LiteCPUCores, policy.LiteMemoryGB, policy.LiteDiskGB
			if m.InstanceMode == "pro" {
				allowed = policy.AllowedProTypes
				m.CPUCores, m.MemoryGB, m.DiskGB = policy.ProCPUCores, policy.ProMemoryGB, policy.ProDiskGB
			}
			if !allowedType(allowed, m.RuntimeType) {
				writeError(c, apiError(403, "RUNTIME_DISABLED", "该成员类型尚未获准创建", nil))
				return
			}
		}
		backend := services.RuntimeBackendGateway
		if m.InstanceMode == "pro" {
			backend = services.RuntimeBackendDesktop
		}
		image, ok := services.RuntimeImageForBackend(m.RuntimeType, backend)
		if !ok || strings.TrimSpace(image.Image) == "" {
			writeError(c, apiError(422, "RUNTIME_DISABLED", "成员镜像尚未配置", nil))
			return
		}
		m.ImageRegistry = &image.Image
	}
	if len(overrides) > 0 {
		writeError(c, apiError(422, "VALIDATION_ERROR", "模板不包含指定成员", nil))
		return
	}
	if validator, ok := a.teams.(interface {
		ValidateOwnerTeamCreate(int, string, services.CreateTeamRequest) error
	}); ok {
		if err := validator.ValidateOwnerTeamCreate(uid, identity.Owner, frozen); err != nil {
			writeError(c, apiError(422, "TEAM_CREATION_REJECTED", "成员资源校验未通过，请检查可用配额和 Runtime 配置", nil))
			return
		}
	}
	now := time.Now().UTC()
	name := req.Name
	team := &models.Team{ResourceScope: models.TeamScopeNorthbound, Owner: &identity.Owner, DisplayName: &name, Description: req.Description, Status: models.TeamStatusCreating, CommunicationMode: frozen.CommunicationMode, RedisEventsLastID: "0-0", SharedMountPath: "/team", CreatedAt: now, UpdatedAt: now}
	stored.TeamRequest = &frozen
	a.submit(c, identity, OwnerTeamRoute{Scope: ScopeTeamsCreate}, nil, team, stored)
}

func (a *OwnerTeamAPI) handleTemplate(c *gin.Context, identity OwnerTeamIdentity, route OwnerTeamRoute, id int) {
	service, err := a.templateService(identity.Owner)
	if err != nil {
		writeError(c, err)
		return
	}
	if route.Action == "templates.list" {
		cond := db.Cond{"owner": identity.Owner, "resource_scope": models.TeamScopeNorthbound}
		if identity.UserID > 0 {
			cond["user_id"] = identity.UserID
		}
		var records []models.CustomTeamTemplate
		if err := a.sess.Collection("custom_team_templates").Find(cond).OrderBy("-id").Limit(100).All(&records); err != nil {
			writeError(c, err)
			return
		}
		items := make([]teamtemplate.Payload, 0, len(records))
		for _, r := range records {
			p, err := service.Get(r.UserID, r.ID)
			if err != nil {
				writeError(c, err)
				return
			}
			items = append(items, *p)
		}
		ownerTeamSuccess(c, gin.H{"templates": items})
		return
	}
	var record *models.CustomTeamTemplate
	if route.Action != "templates.generate" {
		record, err = a.templateRecord(identity, id)
		if err != nil {
			writeError(c, err)
			return
		}
	}
	var value interface{}
	switch route.Action {
	case "templates.get":
		value, err = service.Get(record.UserID, id)
	case "templates.update":
		var req teamtemplate.UpdateMetadataRequest
		if !ownerTeamBind(c, &req) {
			return
		}
		value, err = service.UpdateMetadata(record.UserID, id, req)
	case "templates.icon":
		var req teamtemplate.UpdateMemberIconRequest
		if !ownerTeamBind(c, &req) {
			return
		}
		value, err = service.UpdateMemberIcon(record.UserID, id, c.Param("memberID"), req)
	case "templates.delete":
		err = service.Delete(record.UserID, id)
	default:
		var input interface{}
		switch route.Action {
		case "templates.generate":
			input = &teamtemplate.GenerateRequest{}
		case "templates.revise":
			input = &teamtemplate.ReviseRequest{}
		case "templates.regenerate":
			input = &teamtemplate.RegenerateRequest{}
		case "templates.adjust":
			input = &teamtemplate.AdjustMemberRequest{}
		case "templates.regenerate-member":
			input = &teamtemplate.RegenerateMemberRequest{}
		default:
			writeError(c, apiError(404, "NOT_FOUND", "操作不存在", nil))
			return
		}
		if !ownerTeamBind(c, input) {
			return
		}
		body, _ := json.Marshal(input)
		uid, userErr := a.creationUser(identity)
		if userErr != nil {
			writeError(c, userErr)
			return
		}
		if record != nil && record.UserID != uid {
			writeError(c, apiError(409, "TEMPLATE_PROVIDER_MISMATCH", "当前账号不能生成此模板的修订", nil))
			return
		}
		a.submit(c, identity, route, nil, nil, ownerTeamStoredRequest{Action: route.Action, Body: body, TemplateID: id, MemberID: c.Param("memberID")})
		return
	}
	if err != nil {
		ownerTeamError(c, err)
		return
	}
	ownerTeamSuccess(c, value)
}

func (a *OwnerTeamAPI) executeTemplate(ctx context.Context, op *models.NorthboundOperation, req ownerTeamStoredRequest) (*int, error) {
	// Never replay an uncertain paid model call after a process/lease failure.
	// Completed database writes carry this exact operation ID and can be found.
	var prior models.CustomTeamTemplate
	if err := a.sess.Collection("custom_team_templates").Find(db.Cond{"owner": *op.Owner, "user_id": op.UserID, "resource_scope": models.TeamScopeNorthbound, "last_operation_id": op.OperationID}).One(&prior); err == nil {
		return &prior.ID, nil
	} else if err != db.ErrNoMoreRows {
		return nil, err
	}
	if op.AttemptCount > 1 {
		return nil, fmt.Errorf("TEMPLATE_RESULT_UNCERTAIN: no automatic repeat model billing")
	}
	repo, err := repository.NewOwnerTeamTemplateOperationRepository(a.sess, *op.Owner, op.OperationID)
	if err != nil {
		return nil, err
	}
	service := teamtemplate.NewService(repo, a.gateway)
	var result *teamtemplate.Payload
	switch req.Action {
	case "templates.generate":
		var request teamtemplate.GenerateRequest
		if err = json.Unmarshal(req.Body, &request); err == nil {
			result, err = service.Generate(ctx, op.UserID, request)
		}
	case "templates.revise":
		var request teamtemplate.ReviseRequest
		if err = json.Unmarshal(req.Body, &request); err == nil {
			result, err = service.Revise(ctx, op.UserID, req.TemplateID, request)
		}
	case "templates.regenerate":
		var request teamtemplate.RegenerateRequest
		if err = json.Unmarshal(req.Body, &request); err == nil {
			result, err = service.Regenerate(ctx, op.UserID, req.TemplateID, request)
		}
	case "templates.adjust":
		var request teamtemplate.AdjustMemberRequest
		if err = json.Unmarshal(req.Body, &request); err == nil {
			result, err = service.AdjustMember(ctx, op.UserID, req.TemplateID, req.MemberID, request)
		}
	case "templates.regenerate-member":
		var request teamtemplate.RegenerateMemberRequest
		if err = json.Unmarshal(req.Body, &request); err == nil {
			result, err = service.RegenerateMember(ctx, op.UserID, req.TemplateID, req.MemberID, request)
		}
	default:
		err = fmt.Errorf("unsupported template operation")
	}
	if err != nil {
		return nil, err
	}
	return &result.ID, nil
}
