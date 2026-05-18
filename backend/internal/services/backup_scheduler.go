package services

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/repository"
)

// ---------------------------------------------------------------------------
// Minimal cron expression evaluator
//
// Supports:
//   - Presets: @hourly, @daily, @weekly, @monthly
//   - Standard 5-field: minute hour day-of-month month day-of-week
//   - Field tokens: * (any), number, range (1-5), step (*/6, 1-5/2), list (1,3,5)
//
// All times are evaluated in UTC, consistent with K8s CronJob behaviour.
// ---------------------------------------------------------------------------

// cronMatchesTime returns true if the given cron expression matches t (UTC).
func cronMatchesTime(expr string, t time.Time) bool {
	t = t.UTC()
	switch strings.TrimSpace(expr) {
	case "@hourly":
		return t.Minute() == 0
	case "@daily":
		return t.Hour() == 0 && t.Minute() == 0
	case "@weekly":
		return t.Weekday() == time.Sunday && t.Hour() == 0 && t.Minute() == 0
	case "@monthly":
		return t.Day() == 1 && t.Hour() == 0 && t.Minute() == 0
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}

	return cronFieldMatches(fields[0], t.Minute(), 0, 59) &&
		cronFieldMatches(fields[1], t.Hour(), 0, 23) &&
		cronFieldMatches(fields[2], t.Day(), 1, 31) &&
		cronFieldMatches(fields[3], int(t.Month()), 1, 12) &&
		cronFieldMatches(fields[4], int(t.Weekday()), 0, 6)
}

// cronFieldMatches checks whether value is in the set described by field.
func cronFieldMatches(field string, value, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		if cronPartMatches(part, value, min, max) {
			return true
		}
	}
	return false
}

// cronPartMatches handles a single part: *, N, N-M, */S, N-M/S.
func cronPartMatches(part string, value, min, max int) bool {
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return false
		}
		step = s
		part = part[:idx]
	}

	var lo, hi int
	switch {
	case part == "*":
		lo, hi = min, max
	case strings.Contains(part, "-"):
		rng := strings.SplitN(part, "-", 2)
		var err error
		lo, err = strconv.Atoi(rng[0])
		if err != nil {
			return false
		}
		hi, err = strconv.Atoi(rng[1])
		if err != nil {
			return false
		}
	default:
		n, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		lo, hi = n, n
	}

	for v := lo; v <= hi; v += step {
		if v == value {
			return true
		}
	}
	return false
}

// ValidateCronExpression returns an error if expr is not a supported cron
// expression. It does NOT evaluate the expression against a time.
func ValidateCronExpression(expr string) error {
	expr = strings.TrimSpace(expr)
	switch expr {
	case "@hourly", "@daily", "@weekly", "@monthly":
		return nil
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("invalid cron expression: expected 5 fields or a preset (@hourly, @daily, @weekly, @monthly)")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, f := range fields {
		if err := validateCronField(f, limits[i][0], limits[i][1]); err != nil {
			return fmt.Errorf("invalid cron field %d (%q): %w", i+1, f, err)
		}
	}
	return nil
}

func validateCronField(field string, min, max int) error {
	for _, part := range strings.Split(field, ",") {
		if err := validateCronPart(part, min, max); err != nil {
			return err
		}
	}
	return nil
}

func validateCronPart(part string, min, max int) error {
	raw := part
	if idx := strings.Index(part, "/"); idx >= 0 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return fmt.Errorf("invalid step in %q", raw)
		}
		part = part[:idx]
	}
	if part == "*" {
		return nil
	}
	if strings.Contains(part, "-") {
		rng := strings.SplitN(part, "-", 2)
		lo, err := strconv.Atoi(rng[0])
		if err != nil || lo < min || lo > max {
			return fmt.Errorf("out of range in %q", raw)
		}
		hi, err := strconv.Atoi(rng[1])
		if err != nil || hi < min || hi > max || hi < lo {
			return fmt.Errorf("out of range in %q", raw)
		}
		return nil
	}
	n, err := strconv.Atoi(part)
	if err != nil || n < min || n > max {
		return fmt.Errorf("out of range in %q", raw)
	}
	return nil
}

// ---------------------------------------------------------------------------
// BackupScheduler — background loop
//
// Every schedulerInterval it:
//  1. Loads all active schedules.
//  2. For each schedule, checks whether the cron expression matches the
//     current minute (truncated to the minute boundary).
//  3. Idempotency guard: if the latest scheduled backup for that instance
//     was created less than minScheduleGap ago, skip — prevents double-fire.
//  4. Calls BackupService.CreateScheduledBackup.
//  5. Scans for expired backups (completed + expires_at < now) and soft-deletes
//     them via BackupService.DeleteBackup (which also cleans up the archive).
// ---------------------------------------------------------------------------

const (
	schedulerInterval = 60 * time.Second
	// minScheduleGap prevents double-triggering within the same cron window.
	// Set to 90 seconds so that two consecutive 60-second ticks cannot both
	// fire for the same minute boundary.
	minScheduleGap = 90 * time.Second
)

// BackupScheduler runs scheduled backups and expiry cleanup.
type BackupScheduler struct {
	scheduleRepo repository.BackupScheduleRepository
	backupRepo   repository.BackupRepository
	backupSvc    BackupService
	stopChan     chan struct{}
	stopOnce     sync.Once
	tickMu       sync.Mutex
}

// NewBackupScheduler creates a new scheduler.
func NewBackupScheduler(
	scheduleRepo repository.BackupScheduleRepository,
	backupRepo repository.BackupRepository,
	backupSvc BackupService,
) *BackupScheduler {
	return &BackupScheduler{
		scheduleRepo: scheduleRepo,
		backupRepo:   backupRepo,
		backupSvc:    backupSvc,
		stopChan:     make(chan struct{}),
	}
}

// Start launches the scheduler loop in a background goroutine.
func (s *BackupScheduler) Start() {
	fmt.Println("[BackupScheduler] Starting backup scheduler...")
	go s.loop()
}

// Stop signals the scheduler to exit.
func (s *BackupScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})
}

func (s *BackupScheduler) loop() {
	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-s.stopChan:
			fmt.Println("[BackupScheduler] Stopped.")
			return
		}
	}
}

func (s *BackupScheduler) tick() {
	// Prevent overlapping ticks: if the previous tick is still running, skip.
	if !s.tickMu.TryLock() {
		fmt.Println("[BackupScheduler] Previous tick still running, skipping this cycle")
		return
	}
	defer s.tickMu.Unlock()

	// Recover from panics so the scheduler goroutine survives.
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[BackupScheduler] Recovered from panic in tick: %v\n", r)
		}
	}()

	now := time.Now().UTC()
	// Truncate to the minute so that the cron check is deterministic within
	// the 60-second window.
	nowMinute := now.Truncate(time.Minute)

	s.processSchedules(nowMinute)
	s.cleanupExpired(now)
}

// processSchedules evaluates all active schedules against the current minute.
func (s *BackupScheduler) processSchedules(nowMinute time.Time) {
	schedules, err := s.scheduleRepo.ListAllActive()
	if err != nil {
		fmt.Printf("[BackupScheduler] Error loading active schedules: %v\n", err)
		return
	}

	for _, sched := range schedules {
		if !cronMatchesTime(sched.CronExpression, nowMinute) {
			continue
		}

		// --- Idempotency guard ---
		latest, err := s.backupRepo.GetLatestScheduledBackup(sched.InstanceID)
		if err != nil {
			fmt.Printf("[BackupScheduler] Error checking latest backup for instance %d: %v\n", sched.InstanceID, err)
			continue
		}
		if latest != nil && time.Since(latest.CreatedAt) < minScheduleGap {
			// Already triggered recently — skip to avoid double-fire.
			continue
		}

		name := fmt.Sprintf("scheduled-%s", nowMinute.Format("20060102-1504"))
		if sched.ScheduleName != nil && *sched.ScheduleName != "" {
			name = fmt.Sprintf("%s-%s", *sched.ScheduleName, nowMinute.Format("20060102-1504"))
		}

		_, err = s.backupSvc.CreateScheduledBackup(sched.InstanceID, name, sched.RetentionDays)
		if err != nil {
			fmt.Printf("[BackupScheduler] Failed to create scheduled backup for instance %d: %v\n", sched.InstanceID, err)
		} else {
			fmt.Printf("[BackupScheduler] Created scheduled backup for instance %d (schedule %d)\n", sched.InstanceID, sched.ID)
		}
	}
}

// cleanupExpired soft-deletes completed backups that have passed their
// expires_at timestamp. Only "completed" backups are touched — "creating"
// backups are left alone even if their expires_at is in the past.
func (s *BackupScheduler) cleanupExpired(now time.Time) {
	expired, err := s.backupRepo.ListExpired(now)
	if err != nil {
		fmt.Printf("[BackupScheduler] Error listing expired backups: %v\n", err)
		return
	}

	for _, b := range expired {
		b.Status = backupStatusDeleted
		if err := s.backupRepo.Update(&b); err != nil {
			fmt.Printf("[BackupScheduler] Error soft-deleting expired backup %d: %v\n", b.ID, err)
		} else {
			fmt.Printf("[BackupScheduler] Expired backup %d soft-deleted\n", b.ID)
		}
	}
}

