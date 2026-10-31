package northbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/teamtemplate"
	"github.com/gin-gonic/gin"
	"github.com/upper/db/v4"
)

const ownerTeamOperationPrefix = "owner_team_"

type OwnerTeamAPI struct {
	Audit          repository.AuditEventRepository
	sess           db.Session
	store          *repository.OwnerTeamRepository
	operations     *repository.NorthboundRepository
	teams          services.TeamService
	instances      services.InstanceService
	users          repository.UserRepository
	gateway        teamtemplate.Gateway
	Enabled        bool
	CreationUserID int
	Settings       RuntimeSettingsProvider
}

func NewOwnerTeamAPI(sess db.Session, teams services.TeamService, instances services.InstanceService, users repository.UserRepository, gateway teamtemplate.Gateway, enabled bool, userID int) *OwnerTeamAPI {
	return &OwnerTeamAPI{sess: sess, store: repository.NewOwnerTeamRepository(sess), operations: repository.NewNorthboundRepository(sess), teams: teams, instances: instances, users: users, gateway: gateway, Enabled: enabled, CreationUserID: userID}
}

func (a *OwnerTeamAPI) GuardInstanceMutation(instanceID int, action string, teamID int) error {
	team, err := a.store.InstanceTeam(instanceID)
	if err != nil {
		return err
	}
	if team == nil {
		return nil
	}
	if team.ID != teamID || action != "delete" {
		return fmt.Errorf("该实例属于 Team，请从团队管理操作；不支持单独重置或删除")
	}
	busy, err := a.store.HasActiveTasks(team.ID)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("Team has active tasks")
	}
	return nil
}

type ownerTeamStoredRequest struct {
	TeamID           int                         `json:"team_id,omitempty"`
	Action           string                      `json:"action"`
	Body             json.RawMessage             `json:"body"`
	MemberID         string                      `json:"member_id,omitempty"`
	TeamRequest      *services.CreateTeamRequest `json:"team_request,omitempty"`
	TemplateID       int                         `json:"template_id,omitempty"`
	ExpectedRevision int                         `json:"expected_revision,omitempty"`
}

type ownerTeamCreateRequest struct {
	Name              string  `json:"name"`
	Description       *string `json:"description,omitempty"`
	TemplateID        int     `json:"template_id,omitempty"`
	TemplateKey       string  `json:"template_key,omitempty"`
	Revision          int     `json:"revision"`
	CommunicationMode string  `json:"communication_mode,omitempty"`
	Members           []struct {
		MemberID     string `json:"member_id"`
		RuntimeType  string `json:"runtime_type"`
		InstanceMode string `json:"instance_mode"`
	} `json:"members,omitempty"`
}

func (a *OwnerTeamAPI) templateService(owner string) (teamtemplate.Service, error) {
	repo, err := repository.NewOwnerTeamTemplateRepository(a.sess, owner)
	if err != nil {
		return nil, err
	}
	return teamtemplate.NewService(repo, a.gateway), nil
}

func (a *OwnerTeamAPI) templateRecord(identity OwnerTeamIdentity, id int) (*models.CustomTeamTemplate, error) {
	cond := db.Cond{"id": id, "owner": identity.Owner, "resource_scope": models.TeamScopeNorthbound}
	if identity.UserID > 0 {
		cond["user_id"] = identity.UserID
	}
	var item models.CustomTeamTemplate
	if err := a.sess.Collection("custom_team_templates").Find(cond).One(&item); err != nil {
		return nil, apiError(404, "TEMPLATE_NOT_FOUND", "模板不存在", nil)
	}
	return &item, nil
}

func (a *OwnerTeamAPI) creationUser(identity OwnerTeamIdentity) (int, error) {
	uid := identity.UserID
	if identity.Source == "iei" {
		uid = a.CreationUserID
		if a.Settings != nil {
			uid = a.Settings.Current().OwnerTeamCreationUserID
		}
	}
	if uid <= 0 {
		return 0, apiError(409, "TEAM_CREATION_NOT_CONFIGURED", "尚未配置团队创建账号", nil)
	}
	user, err := a.users.GetByID(uid)
	if err != nil {
		return 0, err
	}
	if user == nil || !user.IsActive {
		return 0, apiError(409, "TEAM_CREATION_DISABLED", "团队创建账号不可用", nil)
	}
	return uid, nil
}

func (a *OwnerTeamAPI) authorize(identity OwnerTeamIdentity, id int) (*models.Team, error) {
	team, err := a.store.Get(identity.UserID, identity.Owner, id)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, apiError(404, "TEAM_NOT_FOUND", "团队不存在", nil)
	}
	members, err := a.store.Members(id)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		if member.Status == models.TeamMemberStatusDeleted || member.InstanceID == nil {
			continue
		}
		instance, err := a.instances.GetByID(*member.InstanceID)
		if err != nil {
			return nil, err
		}
		if instance == nil || instance.UserID != team.UserID || instance.Owner == nil || !strings.EqualFold(strings.TrimSpace(*instance.Owner), identity.Owner) {
			return nil, apiError(409, "TEAM_OWNERSHIP_INVALID", "成员归属异常，已暂停操作", nil)
		}
	}
	return team, nil
}

func (a *OwnerTeamAPI) Handle(c *gin.Context, identity OwnerTeamIdentity, route OwnerTeamRoute) {
	enabled := a.Enabled
	if a.Settings != nil {
		enabled = a.Settings.Current().OwnerTeamsEnabled
	}
	if !enabled && route.Action == "capabilities" {
		ownerTeamSuccess(c, gin.H{"enabled": false, "can_create": false, "create_disabled_reason": "北向 Team 尚未启用"})
		return
	}
	if !enabled {
		writeError(c, apiError(503, "OWNER_TEAMS_DISABLED", "北向 Team 尚未启用", nil))
		return
	}
	owner, err := models.NormalizeTeamOwner(identity.Owner)
	if err != nil {
		writeError(c, apiError(422, "OWNER_REQUIRED", "有效 owner 是必需的", nil))
		return
	}
	identity.Owner = owner
	if identity.RequestID == "" {
		identity.RequestID = requestIDFromContext(c)
	}
	defer func() {
		if a.Audit == nil {
			return
		}
		provider := identity.UserID
		if uid, ok := c.Get("ownerTeamProviderUserID"); ok {
			provider, _ = uid.(int)
		}
		actorType, actorID := identity.Source, sha256Hex(identity.SessionID)
		if identity.Principal != nil {
			actorType = identity.Principal.AuthType
			if identity.Principal.KeyID != "" {
				actorID = identity.Principal.KeyID
			}
		}
		details, _ := json.Marshal(map[string]interface{}{"owner": owner, "source": identity.Source, "provider_user_id": provider, "actor_type": actorType, "actor_id": actorID, "action": route.Action, "request_id": identity.RequestID, "operation_id": c.Param("operationID"), "team_or_template_id": c.Param("id"), "http_status": c.Writer.Status()})
		raw := string(details)
		trace, _ := randomToken("ownerteam_", 12)
		_ = a.Audit.Create(&models.AuditEvent{TraceID: trace, EventType: "owner_team." + route.Action, TrafficClass: "control", Severity: "info", Message: "Owner Team API access", Details: &raw, CreatedAt: time.Now().UTC()})
	}()
	c.Header("Cache-Control", "no-store")
	id, _ := strconv.Atoi(c.Param("id"))
	if route.Action == "capabilities" {
		_, createErr := a.creationUser(identity)
		reason := ""
		if createErr != nil {
			reason = "团队创建暂不可用"
			if failure, ok := createErr.(*APIError); ok {
				reason = failure.Message
			}
		}
		ownerTeamSuccess(c, map[string]interface{}{"enabled": true, "can_create": createErr == nil, "create_disabled_reason": reason, "runtime_types": []string{"openclaw", "hermes"}, "instance_modes": []string{"lite", "pro"}, "custom_member_min": 2, "custom_member_max": 6})
		return
	}
	if strings.HasPrefix(route.Action, "catalog.") {
		a.catalog(c, route.Action)
		return
	}
	if strings.HasPrefix(route.Action, "templates.") {
		a.handleTemplate(c, identity, route, id)
		return
	}
	if route.Action == "operations.get" || route.Action == "operations.retry" {
		if route.Action == "operations.retry" {
			a.retryOperation(c, identity, c.Param("operationID"))
		} else {
			a.getOperation(c, identity, c.Param("operationID"))
		}
		return
	}
	if route.Action == "teams.list" {
		page, limit := boundedPage(c)
		items, total, err := a.store.List(identity.UserID, owner, c.Query("q"), (page-1)*limit, limit)
		if err != nil {
			writeError(c, err)
			return
		}
		views := make([]map[string]interface{}, 0, len(items))
		groups, err := a.store.MemberGroups(items)
		if err != nil {
			writeError(c, err)
			return
		}
		if observer, ok := a.teams.(services.TeamMemberObserver); ok {
			var all []models.TeamMember
			for _, team := range items {
				all = append(all, groups[team.ID]...)
			}
			for id := range groups {
				groups[id] = nil
			}
			for _, member := range observer.ObserveTeamMembers(c.Request.Context(), items, all) {
				groups[member.TeamID] = append(groups[member.TeamID], member)
			}
		}
		for _, team := range items {
			views = append(views, ownerTeamSummary(team, groups[team.ID]))
		}
		ownerTeamSuccess(c, gin.H{"teams": views, "total": total, "page": page, "limit": limit})
		return
	}
	if route.Action == "teams.create" {
		a.submitCreate(c, identity)
		return
	}
	team, err := a.authorize(identity, id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Set("ownerTeamProviderUserID", team.UserID)
	var result interface{}
	switch route.Action {
	case "teams.get":
		var details *services.TeamDetailsPayload
		details, err = a.teams.GetTeam(team.UserID, team.ID)
		if err == nil {
			result = ownerTeamDetails(details)
		}
	case "members.list":
		var members []models.TeamMember
		members, err = a.store.Members(id)
		if err == nil {
			if observer, ok := a.teams.(services.TeamMemberObserver); ok {
				members = observer.ObserveTeamMembers(c.Request.Context(), []models.Team{*team}, members)
			}
		}
		result = ownerTeamMemberViews(members)
	case "teams.update":
		var req struct {
			Name        string  `json:"name"`
			Description *string `json:"description"`
		}
		if !ownerTeamBind(c, &req) {
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len([]rune(req.Name)) > 100 || req.Description != nil && len(*req.Description) > 2000 {
			err = apiError(422, "VALIDATION_ERROR", "团队名称或描述无效", nil)
			break
		}
		err = a.store.UpdateDisplay(team, req.Name, req.Description)
		result = gin.H{"id": id, "name": req.Name}
	case "tasks.list":
		result, err = a.teams.ListTeamTasks(team.UserID, id, queryInt(c, "before_id"), queryInt(c, "limit"))
		if err == nil {
			result = safeOwnerTeamValue(result)
		}
	case "events.list":
		var history *services.TeamEventsHistoryPayload
		history, err = a.teams.ListTeamEvents(team.UserID, id, queryInt(c, "before_id"), queryInt(c, "limit"))
		if err == nil {
			result = gin.H{"events": ownerTeamVisibleEvents(history.Events), "has_more": history.HasMore, "next_before_id": history.NextBeforeID}
		}
	case "tasks.dispatch":
		if team.Status != models.TeamStatusRunning {
			err = apiError(409, "TEAM_NOT_READY", "团队尚不可接收任务", nil)
			break
		}
		var req services.DispatchTeamTaskRequest
		if !ownerTeamBind(c, &req) {
			return
		}
		if strings.TrimSpace(req.MessageID) == "" {
			err = apiError(422, "MESSAGE_ID_REQUIRED", "缺少消息重试标识", nil)
			break
		}
		if req.Payload == nil {
			req.Payload = map[string]interface{}{}
		}
		req.Payload["user_key"], req.Payload["user_label"] = "owner-"+identity.Owner, identity.Owner
		err = a.store.WithAdmission(id, func() error {
			fresh, err := a.store.Get(team.UserID, identity.Owner, id)
			if err != nil {
				return err
			}
			if fresh == nil || fresh.Status != models.TeamStatusRunning {
				return apiError(409, "TEAM_NOT_READY", "团队尚不可接收任务", nil)
			}
			pending, err := a.store.PendingMutation(id)
			if err != nil {
				return err
			}
			if pending {
				return apiError(409, "TEAM_MUTATION_PENDING", "团队正在处理管理操作，请稍后发送", nil)
			}
			result, err = a.teams.DispatchTask(team.UserID, id, req)
			return err
		})
		if err == nil {
			result = safeOwnerTeamValue(result)
		}
	case "teams.delete", "members.delete":
		var req struct {
			ConfirmDataLoss bool `json:"confirm_data_loss"`
		}
		if !ownerTeamBind(c, &req) {
			return
		}
		if !req.ConfirmDataLoss {
			err = apiError(422, "CONFIRMATION_REQUIRED", "此操作会删除团队资源及数据，需要明确确认", nil)
			break
		}
		active, checkErr := a.store.HasActiveTasks(id)
		if checkErr != nil {
			err = checkErr
			break
		}
		if active {
			err = apiError(409, "TEAM_BUSY", "团队任务尚未结束", nil)
			break
		}
		a.submit(c, identity, route, team, nil, ownerTeamStoredRequest{Action: route.Action, MemberID: c.Param("memberID")})
		return
	default:
		if strings.HasPrefix(route.Action, "files.") {
			a.handleFiles(c, team, route.Action)
			return
		}
		err = apiError(404, "NOT_FOUND", "Not found", nil)
	}
	if err != nil {
		ownerTeamError(c, err)
		return
	}
	ownerTeamSuccess(c, result)
}

func (a *OwnerTeamAPI) retryOperation(c *gin.Context, identity OwnerTeamIdentity, id string) {
	op, err := a.operations.GetOperationByID(id)
	if err != nil {
		writeError(c, err)
		return
	}
	if op == nil || op.Owner == nil || *op.Owner != identity.Owner || identity.UserID > 0 && op.UserID != identity.UserID || identity.Source == "iei" && op.SessionID != identity.SessionID {
		writeError(c, apiError(404, "OPERATION_NOT_FOUND", "操作不存在", nil))
		return
	}
	if identity.Principal != nil && (op.AuthScope == nil || !identity.Principal.HasScope(*op.AuthScope)) {
		writeError(c, apiError(403, "SCOPE_DENIED", "Insufficient permission", nil))
		return
	}
	if op.OperationType != "owner_team_teams_create" && op.OperationType != "owner_team_teams_delete" && op.OperationType != "owner_team_members_delete" {
		writeError(c, apiError(409, "EXPLICIT_MODEL_RETRY_REQUIRED", "不会自动重做模型生成；请核对模板后明确重新生成", nil))
		return
	}
	var req struct {
		ExpectedAttempt int `json:"expected_attempt_count"`
	}
	if !ownerTeamBind(c, &req) {
		return
	}
	if op.Status == "queued" || op.Status == "processing" {
		ownerTeamSuccess(c, operationResponse(op))
		return
	}
	if op.Status != "failed" || req.ExpectedAttempt != op.AttemptCount {
		writeError(c, apiError(409, "OPERATION_CHANGED", "操作状态已变化，请重新检查", nil))
		return
	}
	changed, err := a.store.RetryOperation(op, req.ExpectedAttempt)
	if err != nil {
		writeError(c, err)
		return
	}
	if !changed {
		writeError(c, apiError(409, "OPERATION_CHANGED", "操作状态已变化，请重新检查", nil))
		return
	}
	op.Status = "queued"
	op.ErrorCode = nil
	op.ErrorMessage = nil
	ownerTeamSuccess(c, operationResponse(op))
}

func boundedPage(c *gin.Context) (int, int) {
	page, limit := queryInt(c, "page"), queryInt(c, "limit")
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}
func queryInt(c *gin.Context, key string) int { n, _ := strconv.Atoi(c.Query(key)); return n }
func ownerTeamSuccess(c *gin.Context, value interface{}) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": value})
}
func ownerTeamBind(c *gin.Context, value interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(c, apiError(422, "VALIDATION_ERROR", "请求字段或格式无效", nil))
		return false
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		writeError(c, apiError(422, "VALIDATION_ERROR", "只能提交一个 JSON 对象", nil))
		return false
	}
	return true
}
func ownerTeamError(c *gin.Context, err error) {
	if _, ok := err.(*APIError); ok {
		writeError(c, err)
		return
	}
	if strings.Contains(err.Error(), "revision conflict") || strings.Contains(err.Error(), "文件已") {
		writeError(c, apiError(409, "REVISION_CONFLICT", "内容已发生变化，请重新载入", nil))
		return
	}
	writeError(c, apiError(422, "TEAM_ACTION_FAILED", "操作未完成，请检查当前状态或联系管理员", err))
}

func (a *OwnerTeamAPI) submit(c *gin.Context, identity OwnerTeamIdentity, route OwnerTeamRoute, team *models.Team, staged *models.Team, request ownerTeamStoredRequest) {
	if team != nil {
		request.TeamID = team.ID
	}
	uid, err := a.creationUser(identity)
	if team != nil {
		uid = team.UserID
		err = nil
	}
	if err != nil {
		writeError(c, err)
		return
	}
	c.Set("ownerTeamProviderUserID", uid)
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(key) < 8 || len(key) > 128 {
		writeError(c, apiError(422, "IDEMPOTENCY_REQUIRED", "需要稳定的 Idempotency-Key", nil))
		return
	}
	raw, err := json.Marshal(request)
	if err != nil {
		writeError(c, err)
		return
	}
	typ := ownerTeamOperationPrefix + strings.ReplaceAll(request.Action, ".", "_")
	hash := ownerTeamOperationKey(identity, typ, key)
	existing, err := a.store.RequestOperation(hash)
	if err != nil {
		writeError(c, err)
		return
	}
	hashRequest := request
	hashRequest.TeamRequest = nil
	hashPayload, _ := json.Marshal(hashRequest)
	requestHash := sha256Hex(string(hashPayload))
	if existing != nil {
		if existing.RequestHash != requestHash || existing.Owner == nil || *existing.Owner != identity.Owner || identity.Source == "iei" && existing.SessionID != identity.SessionID {
			writeError(c, apiError(409, "IDEMPOTENCY_CONFLICT", "重试标识已用于其他请求", nil))
			return
		}
		c.Header("Idempotent-Replayed", "true")
		c.JSON(202, gin.H{"data": operationResponse(existing)})
		return
	}
	opID, err := randomToken("teamop_", 24)
	if err != nil {
		writeError(c, err)
		return
	}
	now := time.Now().UTC()
	scope := route.Scope
	op := &models.NorthboundOperation{OperationID: opID, UserID: uid, Owner: &identity.Owner, OwnerRequestKey: &hash, SessionID: identity.SessionID, OperationType: typ, IdempotencyKeyHash: hash, RequestHash: requestHash, RequestPayload: string(raw), Status: "queued", AvailableAt: now, CreatedAt: now, UpdatedAt: now, AuthScope: &scope}
	if identity.Principal != nil && identity.Principal.AuthType == "api_key" {
		op.APIKeyID = &identity.Principal.KeyID
	}
	if team != nil {
		op.TeamID = &team.ID
	}
	if request.TemplateID > 0 {
		op.TemplateID = &request.TemplateID
	}
	if staged != nil {
		staged.Name = "nb-" + opID
		staged.UserID = uid
		limit := 5
		if a.Settings != nil {
			limit = a.Settings.Current().MaxPendingOperations
		}
		err = a.store.ReserveCreate(c.Request.Context(), staged, op, limit)
	} else {
		if team != nil {
			err = a.store.WithAdmission(team.ID, func() error {
				busy, err := a.store.HasActiveTasks(team.ID)
				if err != nil {
					return err
				}
				if busy {
					return apiError(409, "TEAM_BUSY", "团队任务尚未结束", nil)
				}
				pending, err := a.store.PendingMutation(team.ID)
				if err != nil {
					return err
				}
				if pending {
					return apiError(409, "TEAM_MUTATION_PENDING", "团队已有管理操作处理中", nil)
				}
				return a.store.ReserveOperation(c.Request.Context(), op, 0)
			})
		} else {
			limit := 5
			if a.Settings != nil {
				limit = a.Settings.Current().MaxPendingOperations
			}
			err = a.store.ReserveOperation(c.Request.Context(), op, limit)
		}
	}
	if err != nil {
		// A competing identical request may have committed after our first read.
		prior, readErr := a.store.RequestOperation(hash)
		if readErr == nil && prior != nil && prior.RequestHash == requestHash && prior.Owner != nil && *prior.Owner == identity.Owner && (identity.Source != "iei" || prior.SessionID == identity.SessionID) {
			c.Header("Idempotent-Replayed", "true")
			c.JSON(202, gin.H{"data": operationResponse(prior)})
			return
		}
		if errors.Is(err, repository.ErrOwnerTeamQueueFull) {
			writeError(c, apiError(429, "TEAM_QUEUE_FULL", "创建操作较多，请等待已有操作完成", nil))
			return
		}
		writeError(c, err)
		return
	}
	c.Header("Location", "team-operations/"+opID)
	c.JSON(202, gin.H{"data": operationResponse(op)})
}

func ownerTeamOperationKey(identity OwnerTeamIdentity, typ, key string) string {
	namespace := identity.Source + ":" + identity.Owner
	if identity.Source != "iei" {
		namespace = fmt.Sprintf("%s:%d", namespace, identity.UserID)
	}
	return sha256Hex(namespace + ":" + typ + ":" + key)
}

// Resolve an existing creation before reading mutable templates/image/account
// defaults. A retry must not reinterpret the original request after an upgrade.
func (a *OwnerTeamAPI) replayCreate(c *gin.Context, identity OwnerTeamIdentity, request ownerTeamStoredRequest) bool {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	typ := ownerTeamOperationPrefix + strings.ReplaceAll(request.Action, ".", "_")
	op, err := a.store.RequestOperation(ownerTeamOperationKey(identity, typ, key))
	if err != nil {
		writeError(c, err)
		return true
	}
	if op == nil {
		return false
	}
	request.TeamRequest = nil
	raw, _ := json.Marshal(request)
	if op.Owner == nil || *op.Owner != identity.Owner || op.RequestHash != sha256Hex(string(raw)) || identity.Source == "iei" && op.SessionID != identity.SessionID || identity.Source != "iei" && op.UserID != identity.UserID {
		writeError(c, apiError(409, "IDEMPOTENCY_CONFLICT", "重试标识已用于其他请求", nil))
		return true
	}
	c.Set("ownerTeamProviderUserID", op.UserID)
	c.Header("Idempotent-Replayed", "true")
	c.JSON(202, gin.H{"data": operationResponse(op)})
	return true
}

func (a *OwnerTeamAPI) getOperation(c *gin.Context, identity OwnerTeamIdentity, id string) {
	op, err := a.operations.GetOperationByID(id)
	if err != nil {
		writeError(c, err)
		return
	}
	if op == nil || op.Owner == nil || *op.Owner != identity.Owner || !strings.HasPrefix(op.OperationType, ownerTeamOperationPrefix) || identity.UserID > 0 && op.UserID != identity.UserID || identity.Source == "iei" && op.SessionID != identity.SessionID {
		writeError(c, apiError(404, "OPERATION_NOT_FOUND", "操作不存在", nil))
		return
	}
	c.Set("ownerTeamProviderUserID", op.UserID)
	if identity.Principal != nil && (op.AuthScope == nil || !identity.Principal.HasScope(*op.AuthScope)) {
		writeError(c, apiError(403, "SCOPE_DENIED", "Insufficient permission", nil))
		return
	}
	ownerTeamSuccess(c, operationResponse(op))
}

// Used by the existing operation worker, with its renewable database lease.
func (a *OwnerTeamAPI) execute(ctx context.Context, op *models.NorthboundOperation) (*int, error) {
	if op.Owner == nil {
		return nil, fmt.Errorf("missing operation owner")
	}
	var req ownerTeamStoredRequest
	if err := json.Unmarshal([]byte(op.RequestPayload), &req); err != nil {
		return nil, err
	}
	if req.Action == "teams.create" {
		if op.TeamID == nil || req.TeamRequest == nil {
			return nil, fmt.Errorf("missing frozen Team request")
		}
		team, err := a.store.Get(op.UserID, *op.Owner, *op.TeamID)
		if err != nil {
			return nil, err
		}
		if team == nil {
			return nil, fmt.Errorf("Team not found")
		}
		engine, ok := a.teams.(services.OwnerTeamProvisioner)
		if !ok {
			return nil, fmt.Errorf("provisioning unavailable")
		}
		provisionErr := engine.ProvisionOwnerTeam(ctx, team, *req.TeamRequest, op.OperationID, a.store)
		var pending *services.OwnerTeamRuntimePending
		if errors.As(provisionErr, &pending) {
			if op.StartedAt != nil && time.Since(*op.StartedAt) >= 15*time.Minute {
				provisionErr = fmt.Errorf("runtime readiness timed out: %s", provisionErr.Error())
			} else {
				err := a.sess.Collection("teams").Find(db.Cond{"id": team.ID, "resource_scope": models.TeamScopeNorthbound, "owner": *op.Owner}).Update(map[string]interface{}{"status": models.TeamStatusCreating, "updated_at": time.Now().UTC()})
				if err != nil {
					return nil, err
				}
				return nil, provisionErr
			}
		}
		if provisionErr != nil {
			// A failed operation must not leave the portal claiming endless creation.
			// This changes presentation only; all existing resources are retained.
			_ = a.sess.Collection("teams").Find(db.Cond{"id": team.ID, "resource_scope": models.TeamScopeNorthbound, "owner": *op.Owner}).Update(map[string]interface{}{"status": models.TeamStatusFailed, "updated_at": time.Now().UTC()})
		}
		return nil, provisionErr
	}
	if strings.HasPrefix(req.Action, "templates.") {
		return a.executeTemplate(ctx, op, req)
	}
	if op.TeamID == nil {
		return nil, fmt.Errorf("missing Team operation target")
	}
	team, err := a.authorize(OwnerTeamIdentity{UserID: op.UserID, Owner: *op.Owner}, *op.TeamID)
	if err != nil {
		return nil, err
	}
	busy, err := a.store.HasActiveTasks(team.ID)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, fmt.Errorf("Team is busy")
	}
	if req.Action == "teams.delete" {
		return nil, a.teams.DeleteTeam(team.UserID, team.ID)
	}
	if req.Action == "members.delete" {
		return nil, a.teams.DeleteMember(team.UserID, team.ID, req.MemberID)
	}
	return nil, fmt.Errorf("unsupported Team operation")
}
