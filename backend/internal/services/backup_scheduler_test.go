package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// ---------- cron parser tests ----------

func TestCronMatchesTime_Presets(t *testing.T) {
	t.Parallel()
	// 2026-03-15 00:00:00 UTC is a Sunday
	sunday := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	weekday := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC) // Monday
	firstOfMonth := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	hourBoundary := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	midHour := time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)

	cases := []struct {
		expr string
		t    time.Time
		want bool
	}{
		{"@hourly", hourBoundary, true},
		{"@hourly", midHour, false},
		{"@daily", sunday, true},
		{"@daily", midHour, false},
		{"@weekly", sunday, true},
		{"@weekly", weekday, false},
		{"@monthly", firstOfMonth, true},
		{"@monthly", sunday, false},
	}
	for _, tc := range cases {
		if got := cronMatchesTime(tc.expr, tc.t); got != tc.want {
			t.Errorf("cronMatchesTime(%q, %v) = %v, want %v", tc.expr, tc.t, got, tc.want)
		}
	}
}

func TestCronMatchesTime_Standard(t *testing.T) {
	t.Parallel()
	// 2026-04-27 14:30 UTC (Monday, April)
	ts := time.Date(2026, 4, 27, 14, 30, 0, 0, time.UTC)

	cases := []struct {
		expr string
		want bool
	}{
		{"30 14 * * *", true},
		{"30 14 27 4 *", true},
		{"30 14 27 4 1", true},  // Monday = 1
		{"0 14 * * *", false},   // minute mismatch
		{"30 15 * * *", false},  // hour mismatch
		{"30 14 28 * *", false}, // day mismatch
		{"30 14 * 5 *", false},  // month mismatch
		{"30 14 * * 0", false},  // weekday mismatch (Sunday)
	}
	for _, tc := range cases {
		if got := cronMatchesTime(tc.expr, ts); got != tc.want {
			t.Errorf("cronMatchesTime(%q, %v) = %v, want %v", tc.expr, ts, got, tc.want)
		}
	}
}

func TestCronMatchesTime_RangeStepList(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 4, 27, 6, 15, 0, 0, time.UTC)

	cases := []struct {
		expr string
		want bool
	}{
		{"15 6 * * *", true},
		{"0,15,30,45 6 * * *", true},  // list
		{"10-20 6 * * *", true},        // range
		{"*/15 */6 * * *", true},       // step
		{"*/10 6 * * *", false},        // 15 is not a multiple of 10
		{"0-10 6 * * *", false},        // 15 not in 0-10
		{"15 4-8 * * *", true},         // 6 in 4-8
		{"15 4-8/2 * * *", true},       // 6 matches 4,6,8
	}
	for _, tc := range cases {
		if got := cronMatchesTime(tc.expr, ts); got != tc.want {
			t.Errorf("cronMatchesTime(%q, %v) = %v, want %v", tc.expr, ts, got, tc.want)
		}
	}
}

func TestCronMatchesTime_InvalidExpr(t *testing.T) {
	t.Parallel()
	ts := time.Now()
	invalids := []string{"", "bad", "* * *", "* * * * * *", "a b c d e"}
	for _, expr := range invalids {
		if cronMatchesTime(expr, ts) {
			t.Errorf("cronMatchesTime(%q, ...) should return false for invalid expr", expr)
		}
	}
}

// ---------- cron validation tests ----------

func TestValidateCronExpression(t *testing.T) {
	t.Parallel()
	valid := []string{
		"@hourly", "@daily", "@weekly", "@monthly",
		"0 2 * * *", "*/15 * * * *", "0 0 1 * *",
		"30 4 1-15 * 1-5", "0,30 */6 * * *",
	}
	for _, expr := range valid {
		if err := ValidateCronExpression(expr); err != nil {
			t.Errorf("ValidateCronExpression(%q) unexpected error: %v", expr, err)
		}
	}

	invalid := []string{
		"", "bad", "* * *", "60 * * * *", "* 24 * * *",
		"* * 0 * *", "* * * 13 *", "* * * * 7",
		"1-60 * * * *", "abc * * * *",
	}
	for _, expr := range invalid {
		if err := ValidateCronExpression(expr); err == nil {
			t.Errorf("ValidateCronExpression(%q) expected error, got nil", expr)
		}
	}
}

// ---------- scheduler tests ----------

// stubScheduleRepo implements repository.BackupScheduleRepository for testing.
type stubScheduleRepo struct {
	schedules []models.BackupSchedule
}

func (r *stubScheduleRepo) Create(s *models.BackupSchedule) error                        { return nil }
func (r *stubScheduleRepo) GetByID(id int) (*models.BackupSchedule, error)                { return nil, nil }
func (r *stubScheduleRepo) ListByInstanceID(id int) ([]models.BackupSchedule, error)      { return nil, nil }
func (r *stubScheduleRepo) Update(s *models.BackupSchedule) error                         { return nil }
func (r *stubScheduleRepo) Delete(id int) error                                           { return nil }
func (r *stubScheduleRepo) ListAllActive() ([]models.BackupSchedule, error) {
	return r.schedules, nil
}

var _ repository.BackupScheduleRepository = (*stubScheduleRepo)(nil)



// stubBackupSvc records calls to CreateScheduledBackup.
type stubBackupSvc struct {
	calls []scheduledBackupCall
}

type scheduledBackupCall struct {
	instanceID    int
	name          string
	retentionDays int
}

func (s *stubBackupSvc) CreateBackup(userID, instanceID int, name string) (*models.Backup, error) {
	return nil, nil
}
func (s *stubBackupSvc) CreateScheduledBackup(instanceID int, name string, retentionDays int) (*models.Backup, error) {
	s.calls = append(s.calls, scheduledBackupCall{instanceID, name, retentionDays})
	return &models.Backup{ID: len(s.calls), InstanceID: instanceID}, nil
}
func (s *stubBackupSvc) ListBackups(userID, instanceID int) ([]models.Backup, error) {
	return nil, nil
}
func (s *stubBackupSvc) GetBackup(userID, backupID int) (*models.Backup, error) { return nil, nil }
func (s *stubBackupSvc) DeleteBackup(userID, backupID int) error                { return nil }
func (s *stubBackupSvc) RestoreBackup(userID, backupID int) error               { return nil }

var _ BackupService = (*stubBackupSvc)(nil)

func TestSchedulerIdempotency(t *testing.T) {
	t.Parallel()
	name := "nightly"
	schedRepo := &stubScheduleRepo{
		schedules: []models.BackupSchedule{
			{ID: 1, InstanceID: 10, CronExpression: "0 2 * * *", RetentionDays: 7, IsActive: true, ScheduleName: &name},
		},
	}
	backupRepo := newStubBackupRepo()
	svc := &stubBackupSvc{}

	scheduler := NewBackupScheduler(schedRepo, backupRepo, svc)

	// Simulate a tick at 02:00 UTC — should trigger.
	nowMinute := time.Date(2026, 4, 27, 2, 0, 0, 0, time.UTC)
	scheduler.processSchedules(nowMinute)
	if len(svc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.calls))
	}

	// Insert the backup that was just created into the repo so the
	// idempotency guard can find it.
	backupRepo.backups[1] = &models.Backup{
		ID:         1,
		InstanceID: 10,
		BackupType: "scheduled",
		Status:     "creating",
		CreatedAt:  time.Now(),
	}

	// Second tick at the same minute — should be skipped (idempotency).
	scheduler.processSchedules(nowMinute)
	if len(svc.calls) != 1 {
		t.Fatalf("expected idempotency guard to prevent second call, got %d calls", len(svc.calls))
	}
}

func TestCleanupExpired(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	now := time.Now()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	// Completed + expired — should be cleaned up.
	backupRepo.backups[1] = &models.Backup{ID: 1, InstanceID: 10, Status: "completed", ExpiresAt: &past}
	// Completed + not expired — should NOT be cleaned up.
	backupRepo.backups[2] = &models.Backup{ID: 2, InstanceID: 10, Status: "completed", ExpiresAt: &future}
	// Creating + expired — should NOT be cleaned up (safety: don't touch in-progress).
	backupRepo.backups[3] = &models.Backup{ID: 3, InstanceID: 10, Status: "creating", ExpiresAt: &past}
	// Completed + no expiry — should NOT be cleaned up.
	backupRepo.backups[4] = &models.Backup{ID: 4, InstanceID: 10, Status: "completed", ExpiresAt: nil}

	scheduler := NewBackupScheduler(&stubScheduleRepo{}, backupRepo, &stubBackupSvc{})
	scheduler.cleanupExpired(now)

	if backupRepo.backups[1].Status != "deleted" {
		t.Errorf("backup 1 should be soft-deleted, got status %q", backupRepo.backups[1].Status)
	}
	if backupRepo.backups[2].Status != "completed" {
		t.Errorf("backup 2 should remain completed, got status %q", backupRepo.backups[2].Status)
	}
	if backupRepo.backups[3].Status != "creating" {
		t.Errorf("backup 3 (creating) should NOT be touched, got status %q", backupRepo.backups[3].Status)
	}
	if backupRepo.backups[4].Status != "completed" {
		t.Errorf("backup 4 (no expiry) should NOT be touched, got status %q", backupRepo.backups[4].Status)
	}
}

func TestSchedulerSkipsInvalidCron(t *testing.T) {
	t.Parallel()
	schedRepo := &stubScheduleRepo{
		schedules: []models.BackupSchedule{
			{ID: 1, InstanceID: 10, CronExpression: "bad cron", RetentionDays: 7, IsActive: true},
		},
	}
	svc := &stubBackupSvc{}
	scheduler := NewBackupScheduler(schedRepo, newStubBackupRepo(), svc)

	scheduler.processSchedules(time.Now().UTC().Truncate(time.Minute))
	if len(svc.calls) != 0 {
		t.Errorf("invalid cron should not trigger backup, got %d calls", len(svc.calls))
	}
}

func TestStopDoubleCallSafety(t *testing.T) {
	t.Parallel()
	scheduler := NewBackupScheduler(&stubScheduleRepo{}, newStubBackupRepo(), &stubBackupSvc{})
	scheduler.Start()

	// Calling Stop twice must not panic (sync.Once protection).
	scheduler.Stop()
	scheduler.Stop() // would panic without sync.Once
}

// panicBackupSvc panics on CreateScheduledBackup to test recovery.
type panicBackupSvc struct{ stubBackupSvc }

func (s *panicBackupSvc) CreateScheduledBackup(instanceID int, name string, retentionDays int) (*models.Backup, error) {
	panic("intentional test panic")
}

func TestTickPanicRecovery(t *testing.T) {
	t.Parallel()
	// Use "* * * * *" so every tick matches, guaranteeing the panic path fires.
	schedRepo := &stubScheduleRepo{
		schedules: []models.BackupSchedule{
			{ID: 1, InstanceID: 10, CronExpression: "* * * * *", RetentionDays: 7, IsActive: true},
		},
	}
	svc := &panicBackupSvc{}
	scheduler := NewBackupScheduler(schedRepo, newStubBackupRepo(), svc)

	// tick() internally calls processSchedules → CreateScheduledBackup → panic.
	// The deferred recover in tick() must catch it so this doesn't crash.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("tick() panic leaked to caller: %v", r)
		}
	}()
	scheduler.tick()
	// If we reach here, the panic was recovered successfully.
}
