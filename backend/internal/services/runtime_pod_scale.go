package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/upper/db/v4"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

type RuntimePodScaleOptions struct {
	Items       []k8s.RuntimePodScaleOption  `json:"items"`
	Unavailable []RuntimePodScaleUnavailable `json:"unavailable,omitempty"`
	Limits      struct {
		MinPods int `json:"min_pods"`
		MaxPods int `json:"max_pods"`
	} `json:"limits"`
}
type RuntimePodScaleUnavailable struct {
	RuntimeType string `json:"runtime_type"`
	Reason      string `json:"reason"`
}

func (s *RuntimePodMaintenanceService) scaleKube() (k8s.RuntimePodScaleDeploymentService, error) {
	if s == nil || s.scheduler == nil || s.scheduler.podRepo == nil || s.sess == nil {
		return nil, fmt.Errorf("persistent Runtime expansion is unavailable")
	}
	kube, ok := s.scheduler.deployments.(k8s.RuntimePodScaleDeploymentService)
	if !ok {
		return nil, fmt.Errorf("Kubernetes Runtime expansion is unavailable")
	}
	return kube, nil
}

// OpenClaw retains serving old pools after scoped upgrades. Only committed,
// approved source inventories establish which descendants are current defaults.
// Registration order, replicas and names alone never select an upgrade pool.
func runtimeDefaultScaleTargets(namespace, runtimeType string, rollouts []models.RuntimeRollout) ([]k8s.RuntimePodScaleTarget, error) {
	base := runtimeType + "-runtime"
	if runtimeType == "deepseek-harness" {
		base = "deepseek-harness-runtime"
	}
	targets := []k8s.RuntimePodScaleTarget{{RuntimeType: runtimeType, Namespace: namespace, DeploymentName: base}}
	if runtimeType != "openclaw" {
		return targets, nil
	}
	canonical := map[string]bool{base: true}
	ordered := append([]models.RuntimeRollout(nil), rollouts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, rollout := range ordered {
		if rollout.RuntimeType != "openclaw" || rollout.Status != "finished" {
			continue
		}
		if rollout.PreflightID == nil || strings.TrimSpace(*rollout.PreflightID) == "" || rollout.PlanFingerprint == nil || strings.TrimSpace(*rollout.PlanFingerprint) == "" || rollout.SourceImagesJSON == nil || strings.TrimSpace(rollout.TargetImageRef) == "" {
			return nil, fmt.Errorf("finished OpenClaw rollout %d lacks approved default source proof", rollout.ID)
		}
		var sources map[string]string
		if err := json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources); err != nil || len(sources) == 0 {
			return nil, fmt.Errorf("finished OpenClaw rollout %d source inventory is unavailable", rollout.ID)
		}
		current := []k8s.RuntimePodScaleTarget{}
		for key := range sources {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid OpenClaw default source inventory")
			}
			if parts[0] != namespace || !canonical[parts[1]] {
				continue
			}
			current = append(current, k8s.RuntimePodScaleTarget{RuntimeType: runtimeType, Namespace: namespace, DeploymentName: runtimeUpgradeTargetDeploymentName(parts[1], rollout.ID), UpgradeID: strconv.FormatInt(rollout.ID, 10), SourceDeployment: parts[1], ImageRef: rollout.TargetImageRef})
		}
		if len(current) == 0 {
			return nil, fmt.Errorf("finished OpenClaw rollout %d has no proven canonical default lineage", rollout.ID)
		}
		// Add after processing all sources: a rollout cannot authorize itself.
		for _, target := range current {
			canonical[target.DeploymentName] = true
		}
		targets = current
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].DeploymentName < targets[j].DeploymentName })
	return targets, nil
}

func (s *RuntimePodMaintenanceService) defaultScaleSnapshot(ctx context.Context, sess db.Session, kube k8s.RuntimePodScaleDeploymentService, runtimeType string) (k8s.RuntimePodScaleTarget, *k8s.RuntimePodScaleSnapshot, error) {
	var rollouts []models.RuntimeRollout
	if runtimeType == "openclaw" {
		if err := sess.Collection("runtime_rollouts").Find(db.Cond{"runtime_type": "openclaw", "status": "finished"}).OrderBy("id").All(&rollouts); err != nil {
			return k8s.RuntimePodScaleTarget{}, nil, err
		}
	}
	targets, err := runtimeDefaultScaleTargets(s.scheduler.runtimeNamespace, runtimeType, rollouts)
	if err != nil {
		return k8s.RuntimePodScaleTarget{}, nil, err
	}
	pods, err := repository.NewRuntimePodRepository(sess).List(ctx, runtimeType)
	if err != nil {
		return k8s.RuntimePodScaleTarget{}, nil, err
	}
	sort.Slice(pods, func(i, j int) bool { return pods[i].PodName < pods[j].PodName })
	var lastErr error
	var selectedTarget k8s.RuntimePodScaleTarget
	var selected *k8s.RuntimePodScaleSnapshot
	now := time.Now()
	for _, target := range targets {
		if err := repository.CheckRuntimePodDeletionIntent(ctx, sess, target.Namespace, target.DeploymentName); err != nil {
			lastErr = err
			continue
		}
		for _, pod := range pods {
			if pod.RuntimeType != runtimeType || pod.Namespace != target.Namespace || pod.DeploymentName != target.DeploymentName || pod.State != "ready" || pod.Draining || pod.SchedulingPaused || pod.PodUID == nil || *pod.PodUID == "" || pod.LastSeenAt == nil || now.Sub(*pod.LastSeenAt) > 2*time.Minute || pod.LastSeenAt.After(now.Add(time.Minute)) {
				continue
			}
			target.PodName = pod.PodName
			target.PodUID = *pod.PodUID
			snapshot, err := kube.InspectRuntimeScale(ctx, target)
			if err != nil {
				lastErr = err
				continue
			}
			if selected == nil {
				selectedTarget, selected = target, snapshot
			} else {
				a, b := selected.Option, snapshot.Option
				equivalent := a.RuntimeType == b.RuntimeType && a.SlotCount == b.SlotCount && a.PodResources == b.PodResources && a.CoordinatorResources == b.CoordinatorResources && a.SlotResources == b.SlotResources && a.ImageRef == b.ImageRef && apiequality.Semantic.DeepEqual(selected.Template.Spec.Resources, snapshot.Template.Spec.Resources)
				if equivalent {
					for i, c := range selected.Template.Spec.Containers {
						if !apiequality.Semantic.DeepEqual(c.Resources, snapshot.Template.Spec.Containers[i].Resources) {
							equivalent = false
							break
						}
					}
				}
				if !equivalent {
					return k8s.RuntimePodScaleTarget{}, nil, fmt.Errorf("current %s default targets have conflicting slot, resource or image configurations", runtimeType)
				}
			}
			// One fresh native Pod proves each distinct latest target. Compare
			// all such targets before selecting the first (lexical) equivalent.
			break
		}
	}
	if selected != nil {
		return selectedTarget, selected, nil
	}
	if lastErr != nil {
		return k8s.RuntimePodScaleTarget{}, nil, lastErr
	}
	return k8s.RuntimePodScaleTarget{}, nil, fmt.Errorf("no fresh healthy serving Pod proves the current %s default Deployment", runtimeType)
}

func (s *RuntimePodMaintenanceService) ScaleOptions(ctx context.Context) (*RuntimePodScaleOptions, error) {
	kube, err := s.scaleKube()
	if err != nil {
		return nil, err
	}
	result := &RuntimePodScaleOptions{Items: []k8s.RuntimePodScaleOption{}}
	result.Limits.MinPods = 1
	result.Limits.MaxPods = 10
	for _, runtimeType := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		_, snapshot, err := s.defaultScaleSnapshot(ctx, s.sess, kube, runtimeType)
		if err != nil {
			result.Unavailable = append(result.Unavailable, RuntimePodScaleUnavailable{RuntimeType: runtimeType, Reason: err.Error()})
			continue
		}
		result.Items = append(result.Items, snapshot.Option)
	}
	return result, nil
}
func (s *RuntimePodMaintenanceService) Scale(ctx context.Context, req k8s.RuntimePodScaleRequest) (*k8s.RuntimePodScaleResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	kube, err := s.scaleKube()
	if err != nil {
		return nil, err
	}
	var result *k8s.RuntimePodScaleResult
	err = s.createAdmission(ctx, req.RuntimeType, func(tx db.Session) error {
		target, snapshot, err := s.defaultScaleSnapshot(ctx, tx, kube, req.RuntimeType)
		if err != nil {
			return err
		}
		if err := repository.CheckRuntimePodDeletionIntent(ctx, tx, target.Namespace, target.DeploymentName); err != nil {
			return err
		}
		result, err = kube.ExpandRuntimeScale(ctx, target, snapshot, req.PodCount)
		return err
	})
	return result, err
}
