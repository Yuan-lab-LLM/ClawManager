package services

import (
	"context"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// The local guard also covers repositories used by offline tools and tests.
// Production repositories additionally hold a MySQL lock across API replicas.
var instanceLifecycleOperations = struct {
	sync.Mutex
	active map[int]bool
}{active: make(map[int]bool)}

type instanceLifecycleLocker interface {
	AcquireLifecycleLock(context.Context, int) (func(), error)
}

func (s *instanceService) acquireLifecycleOperation(id int) (func(), error) {
	return acquireInstanceLifecycleOperation(context.Background(), s.instanceRepo, id)
}

func acquireInstanceLifecycleOperation(parent context.Context, instances repository.InstanceRepository, id int) (func(), error) {
	instanceLifecycleOperations.Lock()
	if instanceLifecycleOperations.active[id] {
		instanceLifecycleOperations.Unlock()
		return nil, models.ErrInstanceOperationInProgress
	}
	instanceLifecycleOperations.active[id] = true
	instanceLifecycleOperations.Unlock()
	releaseLocal := func() {
		instanceLifecycleOperations.Lock()
		delete(instanceLifecycleOperations.active, id)
		instanceLifecycleOperations.Unlock()
	}
	if repo, ok := instances.(instanceLifecycleLocker); ok {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		release, err := repo.AcquireLifecycleLock(ctx, id)
		if err != nil {
			releaseLocal()
			return nil, err
		}
		return func() { defer releaseLocal(); release() }, nil
	}
	return releaseLocal, nil
}
