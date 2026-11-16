package workspacedashboard

import (
	"context"
	"encoding/json"
	"time"
)

func (s *Store) snapshot(ctx context.Context, id int, now time.Time, d *Dashboard) error {
	if d.Coverage.Instances.Status != "ready" {
		return nil
	}
	at := now.UTC().Truncate(5 * time.Minute)
	value := Observation{At: at, Total: &d.InstancesTotal, AllocatedCPU: &d.Capacity.AllocatedCPU, AllocatedMemoryGB: &d.Capacity.AllocatedMemoryGB, MaxCPU: d.Capacity.MaxCPU, MaxMemoryGB: d.Capacity.MaxMemoryGB, EligibleSlots: d.Capacity.EligibleSlots}
	value.Busy = intPtr(d.StateCounts["busy"])
	value.Idle = intPtr(d.StateCounts["idle"])
	value.Running = intPtr(d.StateCounts["running"])
	value.Sleeping = intPtr(d.StateCounts["sleeping"])
	value.Unknown = intPtr(d.StateCounts["unknown"])
	value.Failed = intPtr(d.StateCounts["failed"])
	value.Offline = intPtr(d.StateCounts["offline"])
	value.Starting = intPtr(d.StateCounts["starting"])
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO workspace_dashboard_observations(user_id,bucket_start,observed_at,snapshot_json) VALUES(?,?,?,?)`, id, at, now.UTC(), string(raw))
	return err
}
func intPtr(n int) *int { return &n }
func (s *Store) observations(ctx context.Context, id int, d *Dashboard) {
	d.Coverage.Observations = Coverage{Status: "partial", Note: "历史从实际采样开始，空白时段无观测"}
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT MIN(observed_at) FROM workspace_dashboard_observations WHERE user_id=?`, id)
	var first *time.Time
	if err == nil {
		err = row.Scan(&first)
	}
	if err != nil {
		d.Coverage.Observations = unavailable("状态历史暂不可用")
		return
	}
	if first != nil {
		utc := first.UTC()
		d.Coverage.Observations.Since = &utc
	}
	// Each displayed bucket uses the latest actual observation, never a backfill.
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT o.observed_at,o.snapshot_json FROM workspace_dashboard_observations o JOIN
 (SELECT MAX(bucket_start) AS latest FROM workspace_dashboard_observations WHERE user_id=? AND observed_at>=? AND observed_at<=? GROUP BY `+sqlBucket(d.Range, "observed_at")+`) chosen ON chosen.latest=o.bucket_start WHERE o.user_id=? ORDER BY o.bucket_start`, id, d.Window.Start, d.Window.End, id)
	if err != nil {
		d.Coverage.Observations = unavailable("状态历史暂不可用")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var raw string
		if err = rows.Scan(&at, &raw); err != nil {
			break
		}
		var value Observation
		if json.Unmarshal([]byte(raw), &value) != nil {
			continue
		}
		at = bucket(at, d.Range)
		value.At = at
		for i := range d.Observations {
			if d.Observations[i].At.Equal(at) {
				d.Observations[i] = value
				break
			}
		}
	}
	if err != nil || rows.Err() != nil {
		d.Coverage.Observations = unavailable("状态历史暂不可用")
	}
}

// Run samples each active CM owner on a five-minute cadence. Unique(user,bucket)
// makes concurrent replicas safe. Cancellation bounds reads and terminates the loop.
func (s *Store) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.collect(ctx, now)
		}
	}
}
func (s *Store) collect(ctx context.Context, now time.Time) {
	defer s.pruneObservations(ctx, now)
	passCtx, passCancel := context.WithTimeout(ctx, 30*time.Second)
	defer passCancel()
	readCtx, cancel := context.WithTimeout(passCtx, 5*time.Second)
	rows, err := s.sess.SQL().QueryContext(readCtx, `SELECT DISTINCT i.user_id FROM instances i WHERE i.owner IS NULL AND i.status<>'deleted' AND i.user_id>? ORDER BY i.user_id LIMIT 100`, s.observationCursor)
	ids := []int{}
	if err == nil {
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
	}
	cancel()
	if len(ids) == 0 {
		s.observationCursor = 0
	}
	sampler := *s
	if s.capacity != nil {
		slots, capacityErr := s.capacity(passCtx)
		sampler.capacity = func(context.Context) (map[string]int, error) { return slots, capacityErr }
	}
	for _, id := range ids {
		if passCtx.Err() != nil {
			return
		}
		sampleCtx, stop := context.WithTimeout(passCtx, 5*time.Second)
		observedAt := time.Now().UTC()
		w, _ := makeWindow("today", observedAt)
		d := emptyDashboard("today", observedAt, w)
		if sampler.instances(sampleCtx, id, observedAt, d) == nil {
			sampler.readCapacity(sampleCtx, id, d)
			_ = sampler.snapshot(sampleCtx, id, observedAt, d)
		}
		stop()
		s.observationCursor = id
	}
}

// Retention uses the lifecycle context, independently of an exhausted sampling
// budget; shutdown cancellation still prevents or cancels its bounded delete.
func (s *Store) pruneObservations(ctx context.Context, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	cleanupCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	_, _ = s.sess.SQL().ExecContext(cleanupCtx, `DELETE FROM workspace_dashboard_observations WHERE bucket_start<? LIMIT 10000`, now.AddDate(0, 0, -400).UTC())
}
