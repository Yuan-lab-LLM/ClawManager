package services

import (
	"clawreef/internal/ownerrecords"
	"time"
)

// boundedUsageFrame is a side-copy only. Oversized and binary frames still
// traverse the original proxy unchanged, but are not eligible for observation.
type boundedUsageFrame struct {
	data     []byte
	overflow bool
}

func (b *boundedUsageFrame) Write(p []byte) (int, error) {
	if !b.overflow {
		if len(b.data)+len(p) > 256<<10 {
			b.data = nil
			b.overflow = true
		} else {
			b.data = append(b.data, p...)
		}
	}
	return len(p), nil
}
func WithInstanceProxyOwnerUsage(store *ownerrecords.Store) InstanceProxyServiceOption {
	return func(s *InstanceProxyService) { s.ownerUsage = store }
}
func (s *InstanceProxyService) observeOwnerFrame(instanceID int, upstream bool, frame *boundedUsageFrame) {
	if s.ownerUsage == nil || frame.overflow {
		return
	}
	event := ownerrecords.DecodeOpenClaw(frame.data, upstream)
	if event == nil {
		return
	}
	s.ownerUsage.QueueOpenClaw(uint64(instanceID), *event, time.Now())
}
