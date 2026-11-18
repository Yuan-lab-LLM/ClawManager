package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrAutoStopConflict              = errors.New("automatic stop settings or instance changed; refresh and retry")
	ErrInstanceStopping              = errors.New("instance is stopping or stopped; start it manually before retrying")
	ErrActivityUnavailable           = errors.New("model activity could not be durably recorded; retry later")
	ErrModelActivityInProgress       = errors.New("live model requests are still active; waiting for completion or lease expiry")
	ErrLegacyModelActivityUnverified = errors.New("legacy model request awaits verification that prior application processes exited")
)

const AutoStopDay = 24 * time.Hour

const (
	ModelActivityLeaseDuration = 90 * time.Second
	ModelActivityRenewInterval = 15 * time.Second
)

// Legacy day-only configurations retain the accelerated idle window in test builds.
func InstanceAutoStopIdleTimeout(days int) time.Duration {
	if AutoStopTestTiming {
		return 5 * time.Minute
	}
	return time.Duration(days) * AutoStopDay
}

type InstanceAutoStopSettings struct {
	ScanID               string     `json:"-"`
	ScanCursor           int        `json:"-"`
	ScanLeaseUntil       *time.Time `json:"-"`
	Enabled              bool       `json:"enabled"`
	CheckIntervalDays    int        `json:"check_interval_days"`
	CheckIntervalSeconds *int64     `json:"check_interval_seconds,omitempty"`
	IdleTimeoutDays      int        `json:"idle_timeout_days"`
	IdleTimeoutSeconds   *int64     `json:"idle_timeout_seconds,omitempty"`
	Version              int64      `json:"version"`
	NextCheckAt          time.Time  `json:"next_check_at"`
}

func DefaultInstanceAutoStopSettings(now time.Time) InstanceAutoStopSettings {
	return InstanceAutoStopSettings{Enabled: true, CheckIntervalDays: 1, IdleTimeoutDays: 1, Version: 1, NextCheckAt: NextInstanceAutoStopCheck(now)}
}

// Nil seconds retain the legacy day settings, including test-build defaults.
// Explicit seconds always win, even in a build with accelerated legacy timing.
func (s InstanceAutoStopSettings) CheckInterval() time.Duration {
	if s.CheckIntervalSeconds != nil {
		return time.Duration(*s.CheckIntervalSeconds) * time.Second
	}
	if AutoStopTestTiming {
		return time.Minute
	}
	return time.Duration(s.CheckIntervalDays) * AutoStopDay
}
func (s InstanceAutoStopSettings) IdleTimeout() time.Duration {
	if s.IdleTimeoutSeconds != nil {
		return time.Duration(*s.IdleTimeoutSeconds) * time.Second
	}
	return InstanceAutoStopIdleTimeout(s.IdleTimeoutDays)
}
func (s InstanceAutoStopSettings) MarshalJSON() ([]byte, error) {
	type settings InstanceAutoStopSettings
	interval, idle := int64(s.CheckInterval()/time.Second), int64(s.IdleTimeout()/time.Second)
	out := settings(s)
	out.CheckIntervalSeconds, out.IdleTimeoutSeconds = &interval, &idle
	return json.Marshal(out)
}
func (s InstanceAutoStopSettings) Validate() error {
	valid := func(seconds *int64, days int) bool {
		if seconds != nil {
			return *seconds >= 1 && *seconds <= math.MaxInt64/int64(time.Second)
		}
		return days >= 1 && days <= int(math.MaxInt64/int64(AutoStopDay))
	}
	if !valid(s.CheckIntervalSeconds, s.CheckIntervalDays) || !valid(s.IdleTimeoutSeconds, s.IdleTimeoutDays) {
		return fmt.Errorf("check interval and idle timeout must be positive whole seconds or legacy days within the supported duration range")
	}
	return nil
}
func (s InstanceAutoStopSettings) NextCheck(now time.Time) time.Time {
	if s.CheckIntervalSeconds == nil {
		return NextInstanceAutoStopCheck(now)
	}
	return now.UTC().Add(s.CheckInterval())
}
func (s InstanceAutoStopSettings) AdvanceCheck(due, now time.Time) time.Time {
	if s.CheckIntervalSeconds == nil {
		return AdvanceInstanceAutoStopCheck(due, now, s.CheckIntervalDays)
	}
	interval := s.CheckInterval()
	if due.After(now) {
		return due.UTC().Add(interval)
	}
	// Retain cadence while skipping missed checks without overflowing Duration.
	return now.UTC().Add(interval - now.Sub(due)%interval)
}

// Shanghai has no DST; storing the resulting instant in UTC avoids dependence
// on either the database session timezone or the controller host timezone.
func NextInstanceAutoStopCheck(now time.Time) time.Time {
	if AutoStopTestTiming {
		return now.UTC().Add(time.Minute)
	}
	local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	next := time.Date(local.Year(), local.Month(), local.Day(), 4, 0, 0, 0, local.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.UTC()
}

func AdvanceInstanceAutoStopCheck(due, now time.Time, days int) time.Time {
	if AutoStopTestTiming {
		next := due.Add(time.Minute)
		if !next.After(now) {
			next = due.Add((now.Sub(due)/time.Minute + 1) * time.Minute)
		}
		return next.UTC()
	}
	if days < 1 {
		return NextInstanceAutoStopCheck(now)
	}
	next := due.AddDate(0, 0, days)
	if !next.After(now) {
		// Catch up once rather than spawning a scan for every missed interval.
		elapsed := int(now.Sub(due) / AutoStopDay)
		next = due.AddDate(0, 0, (elapsed/days+1)*days)
	}
	return next.UTC()
}

type InstanceModelActivity struct {
	InstanceID            int        `json:"instance_id"`
	Generation            int        `json:"generation"`
	MonitoringSince       time.Time  `json:"monitoring_since"`
	LastGatewayActivityAt *time.Time `json:"last_gateway_activity_at,omitempty"`
	ActivityVersion       int64      `json:"activity_version"`
	AutoStopDisabled      bool       `json:"auto_stop_disabled"`
	ActiveRequests        int        `json:"active_requests"`
	StopOperationID       string     `json:"stop_operation_id,omitempty"`
	StopPhase             string     `json:"stop_phase,omitempty"`
	StoppedReason         string     `json:"stopped_reason,omitempty"`
	StoppedAt             *time.Time `json:"stopped_at,omitempty"`
	Blocker               string     `json:"auto_stop_blocker,omitempty"`
	StartDeadlineAt       *time.Time `json:"start_deadline_at,omitempty"`
}

func (s InstanceModelActivity) IdleAt(now time.Time, days int) bool {
	if days < 1 || days > int(math.MaxInt64/int64(AutoStopDay)) {
		return false
	}
	return s.IdleAtDuration(now, InstanceAutoStopIdleTimeout(days))
}

func (s InstanceModelActivity) IdleAtDuration(now time.Time, timeout time.Duration) bool {
	if s.MonitoringSince.IsZero() || s.AutoStopDisabled || s.ActiveRequests != 0 || s.StopOperationID != "" || timeout < time.Second {
		return false
	}
	anchor := s.MonitoringSince
	if s.LastGatewayActivityAt != nil && s.LastGatewayActivityAt.After(anchor) {
		anchor = *s.LastGatewayActivityAt
	}
	return !now.Before(anchor.Add(timeout))
}

type InstanceIdleStopOperation struct {
	Kind            string    `json:"stop_kind"`
	ID              string    `json:"id"`
	InstanceID      int       `json:"instance_id"`
	UserID          int       `json:"user_id"`
	Generation      int       `json:"generation"`
	RuntimePodID    int64     `json:"runtime_pod_id"`
	GatewayID       string    `json:"gateway_id"`
	PodUID          string    `json:"pod_uid"`
	Endpoint        string    `json:"-"`
	WorkspacePath   string    `json:"-"`
	StorageIdentity string    `json:"-"`
	Phase           string    `json:"phase"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}
