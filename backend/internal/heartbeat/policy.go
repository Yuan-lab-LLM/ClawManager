// Package heartbeat defines the control-plane policy shared by runtime-pod
// agents and instance agents.
package heartbeat

import "time"

const (
	ReportInterval = 2 * time.Minute
	OnlineWindow   = 3 * time.Minute
	Timeout        = 6 * time.Minute
)

func ReportIntervalSeconds() int {
	return int(ReportInterval / time.Second)
}
