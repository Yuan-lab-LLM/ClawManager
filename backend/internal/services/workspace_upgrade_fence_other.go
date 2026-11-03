//go:build !linux

package services

import (
	"fmt"
	"sync"
)

var devWorkspaceUpgradeLocks sync.Map

// Development-only; production App and Runtime use Linux NFS flock.
func (s *RuntimeUpgradeService) lockWorkspaceOperations(ids []int) (func(), error) {
	var locks []*sync.Mutex
	release := func() {
		for i := len(locks) - 1; i >= 0; i-- {
			locks[i].Unlock()
		}
	}
	for _, id := range normalizedPositiveIDs(ids) {
		key := fmt.Sprintf("%s/%d", s.workspaceRoot, id)
		v, _ := devWorkspaceUpgradeLocks.LoadOrStore(key, &sync.Mutex{})
		m := v.(*sync.Mutex)
		if !m.TryLock() {
			release()
			return nil, ErrWorkspaceUpgradeBusy
		}
		locks = append(locks, m)
	}
	return release, nil
}
