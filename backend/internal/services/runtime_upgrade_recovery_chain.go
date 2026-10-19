package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

var ErrUpgradeSourceState = errors.New("upgrade source state needs explicit recovery")

// Follow only the admitted recovery ancestry, never an arbitrary older success.
// Prepared children can have no binding after an earlier attempt stopped the
// gateway. The stop evidence belongs to that ancestor, not the child's phase.
func (s *RuntimeUpgradeService) recoveryStoppedSource(ctx context.Context, parent int64, id, generation int) (models.RuntimeUpgradeItem, error) {
	seen := map[int64]bool{}
	for parent > 0 && len(seen) < 32 && !seen[parent] {
		seen[parent] = true
		r, err := s.rollouts.GetByID(ctx, parent)
		if err != nil {
			return models.RuntimeUpgradeItem{}, err
		}
		if r == nil || r.RuntimeType != RuntimeTypeOpenClaw {
			break
		}
		switch r.Status {
		case "error", "cancelled", "preflight_blocked", "preflight_passed", "finished":
		default:
			return models.RuntimeUpgradeItem{}, fmt.Errorf("%w: recovery ancestor is not terminal", ErrUpgradeSourceState)
		}
		var prior models.RuntimeUpgradeItem
		err = s.sess.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": parent, "instance_id": id}).One(&prior)
		if err != nil && err != db.ErrNoMoreRows {
			return prior, err
		}
		if err == nil && upgradeItemOwnsConfirmedStoppedGateway(prior) && intValue(prior.SourceGeneration, -1) == generation {
			if r.Status == "finished" && prior.IsolatedAt == nil {
				return prior, ErrUpgradeSourceState
			}
			return prior, nil
		}
		next := runtimeUpgradeScopeFromRollout(r).RecoveryParentID
		if next >= parent {
			break
		}
		parent = next
	}
	return models.RuntimeUpgradeItem{}, fmt.Errorf("%w: instance %d has no matching confirmed-stop evidence; data remains isolated", ErrUpgradeSourceState, id)
}

func (s *RuntimeUpgradeService) verifyRecoverySourceStopped(ctx context.Context, item models.RuntimeUpgradeItem) error {
	pod, err := s.pods.GetByID(ctx, *item.RuntimePodID)
	if err != nil {
		return err
	}
	if pod == nil || !persistedSourcePodMatches(*pod, item) {
		// A replaced source can be proven absent only with a recorded Pod UID.
		if stringValue(item.SourcePodUID) != "" {
			return nil
		}
		return fmt.Errorf("%w: original Runtime identity cannot be verified", ErrUpgradeSourceState)
	}
	reader, ok := s.agent.(gatewayStateReader)
	if !ok || stringValue(pod.AgentEndpoint) == "" {
		return fmt.Errorf("%w: source Gateway stop cannot be checked", ErrUpgradeSourceState)
	}
	state, err := reader.GatewayState(ctx, stringValue(pod.AgentEndpoint), stringValue(item.SourceGatewayID))
	if errors.Is(err, ErrRuntimeAgentNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if state != nil && state.State == "stopped" {
		return nil
	}
	return fmt.Errorf("%w: source Gateway is not confirmed stopped", ErrUpgradeSourceState)
}

// Existing failed item records ARE the persistent quarantine ledger. Derive it
// from the latest executed attempt, so a blocked preview or a successful repair
// cannot resurrect an old failure. No filesystem/instance mutation is involved.
func (s *RuntimeUpgradeService) isolatedUpgradeInstances(ctx context.Context) ([]RuntimeUpgradeUnavailable, error) {
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT ui.instance_id,ui.team_id,COALESCE(ui.error_message,r.error_message,'升级未完成，数据已保留'),r.id FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id JOIN instances i ON i.id=ui.instance_id WHERE (r.status='error' OR (r.status='finished' AND ui.isolated_at IS NOT NULL)) AND r.preflight_id IS NOT NULL AND (ui.isolated_at IS NOT NULL OR (ui.state NOT IN ('verified','gateway_verified') AND (ui.recovery_status='needs_attention' OR i.status='error'))) AND i.status<>'deleting' AND NOT EXISTS(SELECT 1 FROM runtime_upgrade_items n JOIN runtime_rollouts nr ON nr.id=n.rollout_id WHERE n.instance_id=ui.instance_id AND nr.id>r.id AND nr.status IN ('pending','running','finished','error')) ORDER BY ui.instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type observation struct {
		item      RuntimeUpgradeUnavailable
		rolloutID int64
	}
	var observations []observation
	for rows.Next() {
		var item RuntimeUpgradeUnavailable
		var rolloutID int64
		if err := rows.Scan(&item.InstanceID, &item.TeamID, &item.Reason, &rolloutID); err != nil {
			return nil, err
		}
		observations = append(observations, observation{item, rolloutID})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	healthy := make([]bool, len(observations))
	if err := runUpgradeBounded(ctx, len(observations), 4, func(ctx context.Context, index int) error {
		var err error
		healthy[index], err = s.restoredSourceHealthy(ctx, observations[index].rolloutID, observations[index].item.InstanceID)
		return err
	}); err != nil {
		return nil, err
	}
	result := []RuntimeUpgradeUnavailable{}
	for index, o := range observations {
		if !healthy[index] {
			result = append(result, o.item)
		}
	}
	return result, nil
}

// A batch rollback can finish restoring data but retain needs_attention because
// one sibling failed. Do not quarantine a different member that demonstrably
// recovered on its exact original core. This is observation, not rewriting the
// old batch as successful or replaying its restore.
func (s *RuntimeUpgradeService) restoredSourceHealthy(ctx context.Context, rolloutID int64, id int) (bool, error) {
	r, err := s.rollouts.GetByID(ctx, rolloutID)
	if err != nil {
		return false, err
	}
	if r == nil || r.Phase != "rollback_restore" || !(stringValue(r.RollbackStatus) == "needs_attention" || stringValue(r.RollbackStatus) == "waiting" || stringValue(r.RollbackStatus) == "restored") {
		return false, nil
	}
	var item models.RuntimeUpgradeItem
	if err := s.sess.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": rolloutID, "instance_id": id}).One(&item); err != nil {
		return false, err
	}
	if item.IsolatedAt != nil {
		return false, nil
	} // serving the old core is not a successful target repair
	if item.State != "restart_ready" || item.SourceGeneration == nil || item.RuntimePodID == nil || stringValue(item.SourceImageDigest) == "" {
		return false, nil
	}
	var status string
	var generation int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT status,runtime_generation FROM instances WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	if err = row.Scan(&status, &generation); err != nil {
		return false, err
	}
	if status != "running" || generation != *item.SourceGeneration+1 {
		return false, nil
	}
	b, err := s.bindings.GetByInstanceID(ctx, id)
	if err != nil {
		return false, err
	}
	if b == nil || b.State != "running" || b.Generation != generation || b.RuntimePodID != *item.RuntimePodID || b.LastHealthAt == nil {
		return false, nil
	}
	p, err := s.pods.GetByID(ctx, b.RuntimePodID)
	if err != nil {
		return false, err
	}
	if p == nil || p.State != "ready" || p.Draining || p.LastSeenAt == nil || !persistedSourcePodMatches(*p, item) {
		return false, nil
	}
	if !upgradeRecoveryObservationFresh(time.Now().UTC(), sql.NullTime{Time: *p.LastSeenAt, Valid: true}, sql.NullTime{Time: *b.LastHealthAt, Valid: true}) {
		return false, nil
	}
	reader, ok := s.agent.(gatewayStateReader)
	if !ok || stringValue(p.AgentEndpoint) == "" {
		return false, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, err := reader.GatewayState(checkCtx, stringValue(p.AgentEndpoint), b.GatewayID)
	if err != nil {
		return false, nil
	} // unavailable is not healthy; keep isolation
	return state != nil && state.State == "running" && state.InstanceID == id && state.Generation == generation && state.GatewayID == b.GatewayID, nil
}

func (s *RuntimeUpgradeService) expandIsolatedUpgradeTeams(ctx context.Context, items []RuntimeUpgradeUnavailable) ([]RuntimeUpgradeUnavailable, error) {
	if len(items) == 0 {
		return items, nil
	}
	teams, ids := map[int]bool{}, map[int]bool{}
	for _, item := range items {
		ids[item.InstanceID] = true
		if item.TeamID != nil {
			teams[*item.TeamID] = true
		}
	}
	if len(teams) == 0 {
		return items, nil
	}
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT tm.instance_id,tm.team_id FROM team_members tm JOIN instances i ON i.id=tm.instance_id WHERE tm.status NOT IN ('deleted','deleting') AND i.type='openclaw' AND i.status<>'deleting' ORDER BY tm.instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, team int
		if err := rows.Scan(&id, &team); err != nil {
			return nil, err
		}
		if teams[team] && !ids[id] {
			v := team
			items = append(items, RuntimeUpgradeUnavailable{InstanceID: id, TeamID: &v, Reason: "所属 Team 有成员升级失败，整组暂缓，数据保留"})
			ids[id] = true
		}
	}
	return items, rows.Err()
}
