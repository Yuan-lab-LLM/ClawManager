package services

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
)

// RuntimeCommittedImages is read-only release truth. Physical Pod reports never
// become defaults; finished is the scheduler's native verification commit point.
type RuntimeCommittedImages struct {
	Images           map[string]string
	BootstrapSources []k8s.RuntimeSlotBootstrapSourceProof
}

// IsImmutableRuntimeImage accepts a repository pinned to a lowercase sha256 digest.
func IsImmutableRuntimeImage(image string) bool {
	parts := strings.Split(image, "@sha256:")
	if len(parts) != 2 || parts[0] == "" || strings.Contains(parts[0], "@") || strings.IndexFunc(parts[0], unicode.IsSpace) >= 0 || len(parts[1]) != 64 || strings.ToLower(parts[1]) != parts[1] {
		return false
	}
	_, err := hex.DecodeString(parts[1])
	return err == nil
}

func LoadCommittedRuntimeImages(ctx context.Context, sess db.Session) (*RuntimeCommittedImages, error) {
	result := &RuntimeCommittedImages{Images: map[string]string{}}
	rows, err := sess.SQL().QueryContext(ctx, `SELECT r.id,r.runtime_type,r.target_image_ref,r.preflight_id,r.preflight_json,r.source_images_json,COALESCE(c.state_json,'') FROM runtime_rollouts r LEFT JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id WHERE r.status='finished' AND r.runtime_type IN ('openclaw','hermes','deepseek-harness') AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(r.preflight_json,'$.recovery_mode')),'')='' AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(r.preflight_json,'$.upgrade_lab_run_id')),'null')='null' ORDER BY r.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("load committed Runtime images: %w", err)
	}
	defer rows.Close()
	seenSources := map[string]bool{}
	seenTypes := map[string]bool{}
	legacyTypes := map[string]bool{}
	for rows.Next() {
		var r models.RuntimeRollout
		var checkpoint string
		if err = rows.Scan(&r.ID, &r.RuntimeType, &r.TargetImageRef, &r.PreflightID, &r.PreflightJSON, &r.SourceImagesJSON, &checkpoint); err != nil {
			return nil, err
		}
		if !seenTypes[r.RuntimeType] {
			seenTypes[r.RuntimeType] = true
			if !IsImmutableRuntimeImage(r.TargetImageRef) {
				legacyTypes[r.RuntimeType] = true
			} else {
				result.Images[r.RuntimeType] = r.TargetImageRef
			}
		}
		// Legacy tag deliveries predate immutable release admission. Preserve
		// this type's configured defaults; older digests cannot supersede it.
		if legacyTypes[r.RuntimeType] {
			continue
		}
		// Only OpenClaw successor flows retain an intentionally different source.
		if r.RuntimeType != RuntimeTypeOpenClaw {
			continue
		}
		var plan struct {
			Sources []k8s.RuntimeRolloutTarget `json:"source_deployment_proofs"`
		}
		if r.PreflightJSON != nil {
			if err = json.Unmarshal([]byte(*r.PreflightJSON), &plan); err != nil {
				return nil, err
			}
		}
		var progress struct {
			Kind    string                     `json:"kind"`
			Mode    string                     `json:"mode"`
			Targets []k8s.RuntimeRolloutTarget `json:"targets"`
		}
		if checkpoint != "" {
			if err = json.Unmarshal([]byte(checkpoint), &progress); err != nil {
				return nil, err
			}
		}
		if r.PreflightID == nil && progress.Mode != "cold-successor" {
			continue
		}
		if len(plan.Sources) == 0 && progress.Kind == "openclaw-pool-replacement" {
			plan.Sources = progress.Targets
		}
		var sources map[string]string
		if r.SourceImagesJSON != nil {
			if err = json.Unmarshal([]byte(*r.SourceImagesJSON), &sources); err != nil {
				return nil, err
			}
		}
		if len(plan.Sources) == 0 {
			for key, image := range sources {
				parts := strings.SplitN(key, "/", 2)
				if len(parts) != 2 {
					return nil, fmt.Errorf("invalid committed source scope")
				}
				plan.Sources = append(plan.Sources, k8s.RuntimeRolloutTarget{Namespace: parts[0], Name: parts[1], Image: image})
			}
		}
		for _, source := range plan.Sources {
			key := source.Namespace + "/" + source.Name
			if seenSources[key] {
				continue
			}
			seenSources[key] = true
			if source.Namespace == "" || source.Name == "" || sources[key] != source.Image {
				return nil, fmt.Errorf("committed rollout %d source proof is incomplete", r.ID)
			}
			allowServing, proofErr := committedIsolatedSourceBindings(ctx, sess, r.ID, source)
			if proofErr != nil {
				return nil, proofErr
			}
			result.BootstrapSources = append(result.BootstrapSources, k8s.RuntimeSlotBootstrapSourceProof{Source: source, RolloutID: r.ID, AllowServingSource: allowServing})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// Preserve a partial rollout's still-serving source without changing its image
// or placing new instances there. One unknown binding defeats authorization.
func committedIsolatedSourceBindings(ctx context.Context, sess db.Session, rolloutID int64, source k8s.RuntimeRolloutTarget) (bool, error) {
	var retained, authorized int
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN
	 ui.isolated_at IS NOT NULL AND ui.runtime_pod_id=b.runtime_pod_id
	 AND (ui.rollout_id=committed.id OR JSON_CONTAINS(JSON_EXTRACT(committed.preflight_json,'$.exclude_instance_ids'),JSON_ARRAY(b.instance_id)))
	 AND ui.source_pod_uid=p.pod_uid AND ui.source_deployment_name=p.deployment_name
	 AND ui.source_image_digest=? AND p.image_digest=ui.source_image_digest
	 AND ui.source_generation>0
	 AND b.generation=ui.source_generation+CASE WHEN ui.state IN ('restored','restart_ready') THEN 1 ELSE 0 END
	 AND i.runtime_generation=b.generation AND i.type='openclaw'
	 AND p.runtime_type='openclaw' AND b.runtime_type='openclaw'
	 AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items newer JOIN runtime_rollouts nr ON nr.id=newer.rollout_id WHERE newer.instance_id=b.instance_id AND nr.id>ui.rollout_id AND nr.status IN ('pending','running','finished','error'))
	 THEN 1 ELSE 0 END),0)
	 FROM instance_runtime_bindings b JOIN runtime_pods p ON p.id=b.runtime_pod_id
	 LEFT JOIN instances i ON i.id=b.instance_id
	 JOIN runtime_rollouts committed ON committed.id=? AND committed.status='finished'
	 LEFT JOIN runtime_upgrade_items ui ON ui.id=(SELECT prior.id FROM runtime_upgrade_items prior JOIN runtime_rollouts pr ON pr.id=prior.rollout_id WHERE prior.instance_id=b.instance_id AND pr.id<=committed.id AND pr.status IN ('finished','error') AND prior.isolated_at IS NOT NULL ORDER BY pr.id DESC LIMIT 1)
	 WHERE p.namespace=? AND p.deployment_name=?`, imageDigestFromReference(source.Image), rolloutID, source.Namespace, source.Name)
	if err != nil {
		return false, err
	}
	if err = row.Scan(&retained, &authorized); err != nil {
		return false, err
	}
	return retained > 0 && retained == authorized, nil
}

func (images *RuntimeCommittedImages) Apply(cfg config.RuntimePoolConfig) config.RuntimePoolConfig {
	if image := images.Images[RuntimeTypeOpenClaw]; image != "" {
		cfg.OpenClawImage = image
	}
	if image := images.Images[RuntimeTypeHermes]; image != "" {
		cfg.HermesImage = image
	}
	if image := images.Images[RuntimeTypeDeepSeekHarness]; image != "" {
		cfg.DeepSeekHarnessImage = image
	}
	return cfg
}

func WithCommittedRuntimeImages(sess db.Session) func(*systemImageSettingService) {
	return func(service *systemImageSettingService) {
		service.committedImages = func() (*RuntimeCommittedImages, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return LoadCommittedRuntimeImages(ctx, sess)
		}
	}
}

func (s *systemImageSettingService) effectiveImages(items []models.SystemImageSetting) ([]models.SystemImageSetting, error) {
	if s.committedImages == nil {
		return items, nil
	}
	images, err := s.committedImages()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range items {
		item := &items[i]
		if !item.IsEnabled || normalizeSystemImageRuntimeType(item.RuntimeType) != "gateway" || seen[item.InstanceType] {
			continue
		}
		seen[item.InstanceType] = true
		if image := images.Images[item.InstanceType]; image != "" {
			item.Image = image
		}
	}
	return items, nil
}

func (s *systemImageSettingService) effectiveImagesForType(instanceType string) ([]models.SystemImageSetting, error) {
	stored, err := s.repo.ListByInstanceType(instanceType)
	if err != nil {
		return nil, err
	}
	return s.effectiveImages(enabledSystemImageSettingsForType(instanceType, stored))
}

// Snapshot controller identity while the same native inventory is still a live
// source. Historical preflights without this field keep the strict bootstrap guard.
func (s *RuntimeScheduler) RuntimeUpgradeSourceProofs(ctx context.Context, runtimeType string, sources map[string]string) ([]k8s.RuntimeRolloutTarget, error) {
	scoped, ok := s.deployments.(k8s.RuntimeRolloutScopedInventory)
	if !ok {
		return nil, nil
	}
	refs := make([]k8s.RuntimeDeploymentRef, 0, len(sources))
	for key := range sources {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid source scope")
		}
		refs = append(refs, k8s.RuntimeDeploymentRef{Namespace: parts[0], Name: parts[1]})
	}
	targets, err := scoped.RolloutTargetsFor(ctx, refs, runtimeType)
	if err != nil {
		return nil, err
	}
	if len(targets) != len(sources) {
		return nil, fmt.Errorf("native source controller inventory changed during preflight")
	}
	for _, target := range targets {
		// Preserve the existing legacy tag-source preflight path. It cannot
		// authorize relaxing bootstrap's immutable retained-source guard.
		if !IsImmutableRuntimeImage(target.Image) {
			return nil, nil
		}
		if target.UID == "" || target.Image != sources[target.Namespace+"/"+target.Name] {
			return nil, fmt.Errorf("native source controller identity changed during preflight")
		}
	}
	return targets, nil
}
