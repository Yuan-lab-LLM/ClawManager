package backupgate

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
)

type workBoundary struct {
	acquire func(context.Context) (func(), error)
}

var installedWorkBoundary atomic.Pointer[workBoundary]
var workUnitName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

// ConfigureWorkBoundary is an installation bootstrap hook. It fences finite
// application work, including synchronous file/Redis/provider effects. It does
// not certify detached Runtime/tool processes or enroll a writer inventory.
func ConfigureWorkBoundary(acquire func(context.Context) (func(), error)) func() {
	var next *workBoundary
	if acquire != nil {
		next = &workBoundary{acquire: acquire}
	}
	previous := installedWorkBoundary.Swap(next)
	return func() { installedWorkBoundary.CompareAndSwap(next, previous) }
}

// HoldWorkUnit must be acquired before claiming work. The caller releases it
// only after the finite unit and all its joined children have actually ended.
// Context cancellation alone is not permission to resume source writers.
func HoldWorkUnit(ctx context.Context, name string) (func(), error) {
	boundary := installedWorkBoundary.Load()
	if boundary == nil {
		return func() {}, nil
	}
	if !workUnitName.MatchString(name) {
		return nil, errors.New("invalid backup work unit")
	}
	release, err := boundary.acquire(ctx)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, errors.New("backup work boundary unavailable")
	}
	var once sync.Once
	return func() { once.Do(release) }, nil
}
