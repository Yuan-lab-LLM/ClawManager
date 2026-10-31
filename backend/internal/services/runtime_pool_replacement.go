package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

const poolProbeInstanceID = 2140000000

type RuntimePoolOperationView struct {
	Stage     string `json:"stage"`
	Message   string `json:"message"`
	Validated int    `json:"validated"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Restored  bool   `json:"restored"`
}

func LoadRuntimePoolOperation(ctx context.Context, repo any, id int64) (*RuntimePoolOperationView, error) {
	store, ok := repo.(rolloutCheckpointStore)
	if !ok {
		return nil, nil
	}
	raw, err := store.LoadRolloutCheckpoint(ctx, id)
	if err != nil || raw == "" {
		return nil, err
	}
	var p poolReplacementProgress
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	if p.Kind != "openclaw-pool-replacement" {
		return nil, nil
	}
	message := map[string]string{"validate": "检查目标镜像及普通实例启动能力", "legacy_apply": "目标镜像已验证，准备滚动更新", "legacy_wait": "等待滚动更新及实例恢复", "switch": "切换运行池并等待验证", "verify": "验证运行池及新实例接入", "rollback": "恢复原可用运行池", "cleanup_success": "完成验证并清理临时进程", "cleanup_failed": "保留数据并结束恢复"}[p.Stage]
	return &RuntimePoolOperationView{Stage: p.Stage, Message: message, Validated: p.ProbeIndex, Completed: p.PoolIndex, Total: len(p.Targets), Restored: p.Restored}, nil
}

type poolReplacementProgress struct {
	Mode           string `json:"mode,omitempty"`
	raw            string
	Kind           string                     `json:"kind"`
	Version        int                        `json:"version"`
	Targets        []k8s.RuntimeRolloutTarget `json:"targets"`
	ProbeIndex     int                        `json:"probe_index"`
	PoolIndex      int                        `json:"pool_index"`
	Stage          string                     `json:"stage"`
	StageStarted   time.Time                  `json:"stage_started"`
	Submitted      bool                       `json:"submitted"`
	ProbePodUID    string                     `json:"probe_pod_uid"`
	Failure        string                     `json:"failure,omitempty"`
	Restored       bool                       `json:"restored,omitempty"`
	Changed        bool                       `json:"changed,omitempty"`
	RollbackImages map[string]string          `json:"rollback_images"`
}

func (s *RuntimeScheduler) loadPoolReplacement(ctx context.Context, id int64) (*poolReplacementProgress, error) {
	store, ok := s.rolloutRepo.(rolloutCheckpointStore)
	if !ok {
		return nil, fmt.Errorf("运行池恢复进度存储不可用")
	}
	raw, err := store.LoadRolloutCheckpoint(ctx, id)
	if err != nil || raw == "" {
		return nil, err
	}
	var p poolReplacementProgress
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	if p.Kind != "openclaw-pool-replacement" || p.Version != 2 || len(p.Targets) == 0 || p.PoolIndex < 0 || p.PoolIndex > len(p.Targets) || p.ProbeIndex < 0 || p.ProbeIndex > len(p.Targets) {
		return nil, fmt.Errorf("invalid pool replacement checkpoint")
	}
	p.raw = raw
	return &p, nil
}
func (s *RuntimeScheduler) savePoolReplacement(ctx context.Context, id int64, p *poolReplacementProgress) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if store, ok := s.rolloutRepo.(interface {
		CompareAndSavePoolCheckpoint(context.Context, int64, string, string) error
	}); ok {
		if string(raw) == p.raw {
			return nil
		}
		if err = store.CompareAndSavePoolCheckpoint(ctx, id, p.raw, string(raw)); err != nil {
			return err
		}
		p.raw = string(raw)
		return nil
	}
	return s.rolloutRepo.(rolloutCheckpointStore).SaveRolloutCheckpoint(ctx, id, string(raw))
}
func replacementProbeName(id int64, index int) string {
	return fmt.Sprintf("openclaw-runtime-validate-%d-%d", id, index)
}
func replacementKey(t k8s.RuntimeRolloutTarget) string { return t.Namespace + "/" + t.Name }

func (s *RuntimeScheduler) setPoolReplacementStatus(ctx context.Context, r models.RuntimeRollout, p *poolReplacementProgress, status string, started, finished *time.Time, message *string) error {
	if store, ok := s.rolloutRepo.(interface {
		UpdatePoolReplacementStatus(context.Context, int64, string, string, *time.Time, *time.Time, *string) error
	}); ok {
		return store.UpdatePoolReplacementStatus(ctx, r.ID, p.raw, status, started, finished, message)
	}
	return s.rolloutRepo.UpdateStatus(ctx, r.ID, status, started, finished, message)
}

// Healthy legacy serving updates share startup verification, then retain their
// established drain/rebind/data behavior. They do not enter empty-pool rollback.
func (s *RuntimeScheduler) startOrdinaryOpenClawValidation(ctx context.Context, r *models.RuntimeRollout) error {
	store, ok := s.rolloutRepo.(rolloutCheckpointStore)
	if !ok {
		return fmt.Errorf("运行池验证进度存储不可用")
	}
	raw, err := store.LoadRolloutCheckpoint(ctx, r.ID)
	if err != nil {
		return err
	}
	if raw == "" {
		inventory, ok := s.deployments.(k8s.RuntimeRolloutInventory)
		if !ok {
			return fmt.Errorf("运行池清单不可用")
		}
		targets, err := inventory.RolloutTargets(ctx, s.runtimeNamespace, RuntimeTypeOpenClaw)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return fmt.Errorf("没有可检查的 Runtime 部署")
		}
		p := &poolReplacementProgress{Kind: "openclaw-pool-replacement", Mode: "legacy", Version: 2, Targets: targets, Stage: "validate", StageStarted: time.Now().UTC()}
		if err = s.savePoolReplacement(ctx, r.ID, p); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	return s.rolloutRepo.UpdateStatus(ctx, r.ID, "running", &now, nil, stringPtrOrNil("正在验证目标镜像，原实例继续使用现有运行池"))
}

func (s *RuntimeScheduler) startOpenClawPoolReplacement(ctx context.Context, r *models.RuntimeRollout) error {
	if s.upgrade == nil {
		return fmt.Errorf("运行池恢复服务不可用")
	}
	if err := s.upgrade.ValidateEmptyOpenClawPoolReset(ctx, r.ID); err != nil {
		return err
	}
	if _, ok := s.deployments.(k8s.RuntimePoolValidationService); !ok {
		return fmt.Errorf("Runtime 镜像启动验证不可用")
	}
	p, err := s.loadPoolReplacement(ctx, r.ID)
	if err != nil {
		return err
	}
	if p == nil {
		inventory, ok := s.deployments.(k8s.RuntimeRolloutInventory)
		if !ok {
			return fmt.Errorf("运行池部署清单不可用")
		}
		targets, err := inventory.RolloutTargets(ctx, s.runtimeNamespace, RuntimeTypeOpenClaw)
		if err != nil {
			return err
		}
		// Match only the exact source scope admitted by preflight, never history.
		var sources map[string]string
		if err = json.Unmarshal([]byte(stringValue(r.SourceImagesJSON)), &sources); err != nil {
			return err
		}
		var selected []k8s.RuntimeRolloutTarget
		for _, t := range targets {
			if image, ok := sources[replacementKey(t)]; ok {
				if image != t.Image {
					return fmt.Errorf("运行池镜像在检查后发生变化，请重新检查")
				}
				selected = append(selected, t)
			}
		}
		if len(selected) == 0 || len(selected) != len(sources) {
			return fmt.Errorf("运行池部署在检查后发生变化，请重新检查")
		}
		p = &poolReplacementProgress{Kind: "openclaw-pool-replacement", Version: 2, Targets: selected, Stage: "validate", StageStarted: time.Now().UTC(), RollbackImages: map[string]string{}}
		for _, t := range selected {
			p.RollbackImages[replacementKey(t)] = s.upgrade.poolRecoveryImage(ctx, t)
		}
		if err = s.savePoolReplacement(ctx, r.ID, p); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	return s.setPoolReplacementStatus(ctx, *r, p, "running", &now, nil, stringPtrOrNil("正在验证目标 Runtime，原运行池尚未修改"))
}

// A replacement keeps instance admission fenced until every original pool has
// been verified. Only isolated probe Gateways run before the commit point.
func (s *RuntimeScheduler) advanceOpenClawPoolReplacement(ctx context.Context, r models.RuntimeRollout, p *poolReplacementProgress) (resultErr error) {
	defer func() {
		if resultErr != nil && ctx.Err() == nil && time.Since(p.StageStarted) > 10*time.Minute {
			now := time.Now().UTC()
			message := "运行池操作持续无法确认执行结果；实例数据保留，恢复未确认，请检查控制面后重试"
			if err := s.setPoolReplacementStatus(ctx, r, p, "error", r.StartedAt, &now, &message); err != nil {
				resultErr = err
			}
		}
	}()
	service, ok := s.deployments.(k8s.RuntimePoolValidationService)
	if !ok {
		return fmt.Errorf("运行池启动验证不可用")
	}
	operation := strconv.FormatInt(r.ID, 10)
	report := func(message string) error {
		if stringValue(r.ErrorMessage) == message {
			return nil
		}
		return s.setPoolReplacementStatus(ctx, r, p, "running", r.StartedAt, nil, stringPtrOrNil(message))
	}
	transition := func(stage string) error {
		p.Stage = stage
		p.Submitted = false
		p.StageStarted = time.Now().UTC()
		return s.savePoolReplacement(ctx, r.ID, p)
	}
	fail := func(reason string) error {
		p.Failure = reason
		if p.Stage == "validate" {
			return transition("cleanup_failed")
		}
		if r.AutoRollback && p.Changed {
			return transition("rollback")
		}
		return transition("cleanup_failed")
	}
	if s.leader != nil && !s.leader.IsLeader(ctx) {
		return fmt.Errorf("运行池控制权已转移")
	}
	if p.Stage == "validate" {
		if p.ProbeIndex == len(p.Targets) {
			if p.Mode == "legacy" {
				return transition("legacy_apply")
			}
			return transition("switch")
		}
		source := p.Targets[p.ProbeIndex]
		name := replacementProbeName(r.ID, p.ProbeIndex)
		if err := service.EnsurePoolValidation(ctx, source, name, r.TargetImageRef, operation); err != nil {
			return err
		}
		observed, err := service.ObservePoolValidation(ctx, source.Namespace, name, operation)
		if err != nil {
			return err
		}
		if observed.Failed {
			return fail(observed.Reason)
		}
		if time.Since(p.StageStarted) > 5*time.Minute {
			return fail("目标 Runtime 启动验证超时：" + observed.Reason)
		}
		if !observed.Ready {
			return report(observed.Reason)
		}
		if s.agentClient == nil {
			return fail("Runtime 验证客户端不可用")
		}
		probeCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
		defer cancel()
		if err = s.agentClient.Health(probeCtx, observed.Endpoint); err != nil {
			return report("正在等待目标 Runtime 控制接口响应")
		}
		// Apply the same tenant proxy policy as ordinary Gateway creation. A
		// validation pool must exercise required admission, not bypass it.
		if err = s.proxyPolicy.Ensure(probeCtx, &models.RuntimePod{Namespace: source.Namespace, PodUID: &observed.PodUID, AgentEndpoint: &observed.Endpoint}); err != nil {
			return report("正在准备验证 Runtime 的访问策略")
		}
		// The complete native startup writes config/session files only to emptyDir.
		// Dummy LLM credentials never invoke a provider or charge an account.
		response, err := s.agentClient.CreateGateway(probeCtx, observed.Endpoint, RuntimeAgentCreateGatewayRequest{InstanceID: poolProbeInstanceID, UserID: 1, AgentType: RuntimeTypeOpenClaw, WorkspacePath: RuntimeWorkspacePathWithRoot(s.workspaceRoot, RuntimeTypeOpenClaw, 1, poolProbeInstanceID), Generation: 1, GatewayPort: 20000, PortRange: RuntimeAgentPortRange{Start: 20000, End: 20002}, UID: 1000, GID: 1000, MemoryMB: 512, DiskQuotaMB: 64, Environment: map[string]string{"CLAWMANAGER_LLM_BASE_URL": "http://127.0.0.1:9/v1", "CLAWMANAGER_LLM_API_KEY": "isolated-validation-only", "CLAWMANAGER_LLM_MODEL": "[\"auto\"]", "CLAWMANAGER_OPENCLAW_SCHEDULED_TASKS_JSON": "[]"}})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fail("目标镜像无法启动普通实例：" + safePoolProbeError(err))
		}
		if response == nil || response.GatewayID != fmt.Sprintf("gw-%d-1", poolProbeInstanceID) || response.Port != 20000 || !NormalizeRuntimeGatewayLifecycle(response.Status, nil).CreateAccepted() {
			return fail("目标 Runtime 未确认普通实例启动成功")
		}
		if !NormalizeRuntimeGatewayLifecycle(response.Status, nil).Running {
			return report("正在等待验证 Gateway 启动；保持同一实例和代数，不重复创建")
		}
		if err = s.agentClient.DeleteGateway(probeCtx, observed.Endpoint, response.GatewayID); err != nil {
			return fail("目标验证进程等待清理")
		}
		if err = service.StopPoolValidation(ctx, source.Namespace, name, operation); err != nil {
			return err
		}
		p.ProbeIndex++
		p.StageStarted = time.Now().UTC()
		return s.savePoolReplacement(ctx, r.ID, p)
	}
	if p.Stage == "legacy_apply" {
		// A stopped validation deployment has no business mounts. Await physical
		// probe exit before handing control to the established rolling updater.
		for i, t := range p.Targets {
			state, err := service.ObservePoolValidation(ctx, t.Namespace, replacementProbeName(r.ID, i), operation)
			if err != nil {
				return err
			}
			if !state.Gone {
				if time.Since(p.StageStarted) > 5*time.Minute {
					return fail("隔离验证进程退出超时，原运行池未修改")
				}
				return report("正在结束隔离启动验证")
			}
		}
		if err := s.startRuntimeRolloutWork(ctx, &r); err != nil {
			return err
		}
		current, err := s.rolloutRepo.GetByID(ctx, r.ID)
		if err != nil {
			return err
		}
		if current.Status == "finished" || current.Status == "error" {
			return nil
		}
		return transition("legacy_wait")
	}
	if p.Stage == "legacy_wait" {
		return s.finishLiveRuntimeRollout(ctx, r, nil)
	}
	if p.Stage == "switch" || p.Stage == "rollback" {
		restoring := p.Stage == "rollback"
		// Recheck user data/reservations before every mutation, including rollback.
		if err := s.upgrade.ValidateEmptyOpenClawPoolReset(ctx, r.ID); err != nil {
			if time.Since(p.StageStarted) > 10*time.Minute {
				p.Failure = "运行池恢复遇到新的数据或占用变化，未执行不确定的覆盖"
				return transition("cleanup_failed")
			}
			return report("正在等待运行池操作完成核对：" + err.Error())
		}
		if p.PoolIndex == len(p.Targets) {
			if restoring {
				p.Restored = true
				return transition("cleanup_failed")
			}
			return transition("verify")
		}
		target := p.Targets[p.PoolIndex]
		image := r.TargetImageRef
		if restoring {
			image = p.RollbackImages[replacementKey(target)]
		}
		if !p.Submitted {
			if !p.Changed {
				p.Changed = true
				if err := s.savePoolReplacement(ctx, r.ID, p); err != nil {
					return err
				}
			}
			if err := service.RolloutPoolImage(ctx, target, image); err != nil {
				return err
			}
			p.Submitted = true
			p.StageStarted = time.Now().UTC()
			return s.savePoolReplacement(ctx, r.ID, p)
		}
		check := r
		check.TargetImageRef = image
		reason, err := s.runtimeRolloutCompletionReason(ctx, check, []k8s.RuntimeRolloutTarget{target})
		if err != nil {
			return err
		}
		if reason == "" {
			reason, err = service.PoolRetirementReason(ctx, target, image)
			if err != nil {
				return err
			}
		}
		if reason != "" {
			if time.Since(p.StageStarted) > 10*time.Minute {
				if restoring {
					p.Failure += "；原运行池恢复验证失败：" + reason
					return transition("cleanup_failed")
				}
				p.PoolIndex = 0
				return fail("运行池切换失败：" + reason)
			}
			return report(map[bool]string{true: "正在恢复原运行池：", false: "正在切换并验证运行池："}[restoring] + reason)
		}
		p.PoolIndex++
		p.Submitted = false
		p.StageStarted = time.Now().UTC()
		return s.savePoolReplacement(ctx, r.ID, p)
	}
	if p.Stage == "verify" {
		reason, err := s.runtimeRolloutCompletionReason(ctx, r, p.Targets)
		if err != nil {
			return err
		}
		if reason == "" {
			for _, target := range p.Targets {
				reason, err = service.PoolRetirementReason(ctx, target, r.TargetImageRef)
				if err != nil {
					return err
				}
				if reason != "" {
					break
				}
			}
		}
		if reason == "" {
			reason, err = s.emptyPoolPlacementCompletionReason(ctx, r)
			if err != nil {
				return err
			}
		}
		if reason != "" {
			if time.Since(p.StageStarted) > 5*time.Minute {
				p.PoolIndex = 0
				return fail(reason)
			}
			return report(reason)
		}
		return transition("cleanup_success")
	}
	if p.Stage == "cleanup_success" || p.Stage == "cleanup_failed" {
		for i, t := range p.Targets {
			name := replacementProbeName(r.ID, i)
			if err := service.StopPoolValidation(ctx, t.Namespace, name, operation); err != nil {
				return err
			}
			observed, err := service.ObservePoolValidation(ctx, t.Namespace, name, operation)
			if err != nil {
				return err
			}
			if !observed.Gone {
				if time.Since(p.StageStarted) > 5*time.Minute {
					now := time.Now().UTC()
					message := "临时验证进程尚未确认退出；已停止后续操作，实例数据保留，请检查 Kubernetes 清理状态"
					return s.setPoolReplacementStatus(ctx, r, p, "error", r.StartedAt, &now, &message)
				}
				return report("正在清理隔离验证进程")
			}
		}
		now := time.Now().UTC()
		if p.Stage == "cleanup_success" {
			reason, err := s.runtimeRolloutCompletionReason(ctx, r, p.Targets)
			if err != nil {
				return err
			}
			if reason != "" {
				return transition("verify")
			}
			for _, target := range p.Targets {
				reason, err = service.PoolRetirementReason(ctx, target, r.TargetImageRef)
				if err != nil {
					return err
				}
				if reason != "" {
					return transition("verify")
				}
			}
			return s.setPoolReplacementStatus(ctx, r, p, "finished", r.StartedAt, &now, nil)
		}
		recovery := "原运行池未修改"
		if p.Restored {
			recovery = "原运行池已恢复并验证"
		} else if p.Changed {
			recovery = "运行池恢复尚未完成"
		}
		return s.setPoolReplacementStatus(ctx, r, p, "error", r.StartedAt, &now, stringPtrOrNil(p.Failure+"；"+recovery+"；数据保留，可更换镜像后重试"))
	}
	return fmt.Errorf("unknown pool replacement stage %q", p.Stage)
}

func safePoolProbeError(err error) string {
	// Agent prose can include runtime configuration. Expose only a fixed class;
	// diagnostic evidence remains on the isolated Runtime, never in public DTOs.
	if runtimeGatewayCreateOutcomeUncertain(err) {
		return "启动确认超时"
	}
	return "普通实例启动检查失败，请查看验证 Runtime 日志"
}

func (s *RuntimeUpgradeService) poolRecoveryImage(ctx context.Context, t k8s.RuntimeRolloutTarget) string {
	image := t.Image
	for i := 0; i < 8; i++ {
		var raw string
		row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COALESCE(source_images_json,'{}') FROM runtime_rollouts WHERE runtime_type='openclaw' AND status='error' AND phase='empty_pool_reset' AND target_image_ref=? ORDER BY id DESC LIMIT 1`, image)
		if err != nil || row.Scan(&raw) != nil {
			return image
		}
		var sources map[string]string
		if json.Unmarshal([]byte(raw), &sources) != nil {
			return image
		}
		previous := sources[replacementKey(t)]
		if previous == "" || previous == image {
			return image
		}
		image = previous
	}
	return image
}
