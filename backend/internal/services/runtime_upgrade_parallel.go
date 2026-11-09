package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"clawreef/internal/models"
)

// This policy is frozen in the preflight record. Legacy records retain their
// original batch interpretation; no scheduler-wide setting changes an active run.
type runtimeUpgradeExecutionPolicy struct {
	Version int `json:"version"`
	Check   int `json:"check"`
	Migrate int `json:"migrate"`
	Start   int `json:"start"`
	Restore int `json:"restore"`
}

func defaultRuntimeUpgradeExecutionPolicy() runtimeUpgradeExecutionPolicy {
	return runtimeUpgradeExecutionPolicy{Version: 2, Check: 4, Migrate: 2, Start: 2, Restore: 2}
}

func runtimeUpgradePolicy(rollout *models.RuntimeRollout) runtimeUpgradeExecutionPolicy {
	legacy := runtimeUpgradeExecutionPolicy{Check: 1, Migrate: 1, Start: 1, Restore: 1}
	if rollout == nil || rollout.PreflightJSON == nil {
		return legacy
	}
	var frozen struct {
		Execution runtimeUpgradeExecutionPolicy `json:"execution"`
	}
	if json.Unmarshal([]byte(*rollout.PreflightJSON), &frozen) != nil || (frozen.Execution.Version != 1 && frozen.Execution.Version != 2) {
		return legacy
	}
	p := frozen.Execution
	for _, limit := range []*int{&p.Check, &p.Migrate, &p.Start, &p.Restore} {
		*limit = minInt(maxInt(*limit, 1), 8)
	}
	return p
}

func runtimeUpgradePodPolicy(rollout *models.RuntimeRollout, pods []models.RuntimePod) runtimeUpgradeExecutionPolicy {
	p := runtimeUpgradePolicy(rollout)
	if len(pods) == 0 {
		p.Check, p.Migrate, p.Start, p.Restore = 1, 1, 1, 1
	}
	for _, pod := range pods {
		fenced := false
		for _, capability := range pod.Capabilities() {
			if capability == "openclaw.upgrade-operation-fence-v1" {
				fenced = true
			}
		}
		if !fenced {
			p.Check, p.Migrate, p.Start, p.Restore = 1, 1, 1, 1
			break
		}
	}
	return p
}

// Both the Runtime heartbeat and the assigned Gateway must be current.
func upgradeRecoveryObservationFresh(now time.Time, heartbeat, gatewayHealth sql.NullTime) bool {
	for _, value := range []sql.NullTime{heartbeat, gatewayHealth} {
		if !value.Valid || value.Time.After(now.Add(30*time.Second)) || now.Sub(value.Time) > 2*time.Minute {
			return false
		}
	}
	return true
}

// Team maintenance is the atomicity boundary, not an unbounded goroutine wave.
// A leader cannot be selected while any of its workers still needs this phase,
// even when the worker lies beyond the current page/batch.
func boundedUpgradeBatch(items []models.RuntimeUpgradeItem, limit int, states ...string) []models.RuntimeUpgradeItem {
	allowed := map[string]bool{}
	for _, state := range states {
		allowed[state] = true
	}
	pendingTeams := map[int]bool{}
	for _, item := range items {
		if item.TeamID != nil && !item.IsTeamLeader && allowed[item.State] {
			pendingTeams[*item.TeamID] = true
		}
	}
	batch := make([]models.RuntimeUpgradeItem, 0, minInt(len(items), maxInt(limit, 1)))
	for _, item := range items {
		if !allowed[item.State] || (item.IsTeamLeader && item.TeamID != nil && pendingTeams[*item.TeamID]) {
			continue
		}
		batch = append(batch, item)
		if len(batch) >= maxInt(limit, 1) {
			break
		}
	}
	return batch
}

// Stop admitting queued work on error/cancellation, but join every admitted
// operation before returning. In particular rollback must never race an in-flight
// migration. Cancellation is passed through; it is not an excuse to abandon it.
func runUpgradeBounded(ctx context.Context, count, concurrency int, run func(context.Context, int) error) error {
	var mu sync.Mutex
	var wg sync.WaitGroup
	next := 0
	var failures []error
	for worker := 0; worker < minInt(count, maxInt(concurrency, 1)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if next == count || len(failures) > 0 || ctx.Err() != nil {
					mu.Unlock()
					return
				}
				index := next
				next++
				mu.Unlock()
				if err := run(ctx, index); err != nil {
					mu.Lock()
					failures = append(failures, err)
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	return errors.Join(failures...)
}
