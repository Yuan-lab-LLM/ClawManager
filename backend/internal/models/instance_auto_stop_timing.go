//go:build !cm_idle_stop_test_timing

package models

// Production builds always retain day-based automatic stop settings.
const AutoStopTestTiming = false
