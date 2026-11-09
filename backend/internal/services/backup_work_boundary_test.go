package services

import (
	"context"
	"testing"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
)

func TestBackupMaintenanceDoesNotAdvanceTeamEventCursor(t *testing.T) {
	restore := backupgate.ConfigureWorkBoundary(func(context.Context) (func(), error) { return nil, backupgate.ErrMaintenance })
	defer restore()
	team := &models.Team{ID: 41, RedisEventsLastID: "100-1"}
	service := &teamService{}
	if service.projectConsumedTeamEvent(context.Background(), team, nil, redisStreamMessage{ID: "100-2"}) {
		t.Fatal("maintenance admitted an event projection")
	}
	if team.RedisEventsLastID != "100-1" {
		t.Fatal("event was skipped across backup pause", team.RedisEventsLastID)
	}
}

func TestBackupMaintenanceDefersBackgroundWorkBeforeRepositoryClaims(t *testing.T) {
	count := 0
	restore := backupgate.ConfigureWorkBoundary(func(context.Context) (func(), error) { count++; return nil, backupgate.ErrMaintenance })
	defer restore()
	// Nil dependencies deliberately panic if any work is attempted before the
	// maintenance admission check. A denied unit must not claim/fail user work.
	(&SkillPackageMaterializeWorker{}).processBatch(context.Background())
	(&SyncService{}).syncAllInstances()
	(&RuntimeScheduler{}).reconcileIfLeader(context.Background())
	(&teamService{}).runTeamMaintenanceSweep(context.Background())
	(&teamService{}).resumePendingTeamDeletions()
	(&instanceService{}).completeDeletion(1, 1)
	if count != 6 {
		t.Fatal("a background entry point bypassed admission", count)
	}
}
