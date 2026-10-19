package ownerrecords

import (
	"context"
	"sync"
	"time"
)

const summaryInterval = 10 * time.Minute

type queuedObservation struct {
	instance uint64
	event    OpenClawEvent
	at       time.Time
}
type growthRead struct {
	at   time.Time
	data *Growth
}
type lowCostState struct {
	ownerKeys  map[string]map[string]bool
	slots      chan struct{}
	reports    map[string]reportRead
	mu         sync.Mutex
	bindings   map[uint64]bool
	events     []queuedObservation
	growth     map[string]growthRead
	growthBusy map[string]chan struct{}
	visits     map[string]time.Time
	activityMu sync.Mutex
	streams    map[string]activityStream
	windows    map[string]activityWindow
}

// Best-effort bounded side channel: proxy/inference never waits for statistics SQL.
func (s *Store) QueueBinding(instance uint64) {
	if !s.Available() {
		return
	}
	s.perf.low.mu.Lock()
	defer s.perf.low.mu.Unlock()
	if s.perf.low.bindings == nil {
		s.perf.low.bindings = map[uint64]bool{}
	}
	if len(s.perf.low.bindings) < 2048 {
		s.perf.low.bindings[instance] = true
	}
}
func (s *Store) QueueOpenClaw(instance uint64, event OpenClawEvent, at time.Time) {
	if !s.Available() {
		return
	}
	s.perf.low.mu.Lock()
	defer s.perf.low.mu.Unlock()
	if len(s.perf.low.events) < 2048 {
		s.perf.low.events = append(s.perf.low.events, queuedObservation{instance, event, at})
	}
}
func (s *Store) drainObservations(ctx context.Context) {
	s.perf.low.mu.Lock()
	bindings, events := s.perf.low.bindings, s.perf.low.events
	s.perf.low.bindings = nil
	s.perf.low.events = nil
	s.perf.low.mu.Unlock()
	n := 0
	for id := range bindings {
		if ctx.Err() != nil || n >= 100 {
			break
		}
		_, _ = s.Bind(ctx, id)
		n++
	}
	for _, e := range events {
		if ctx.Err() != nil {
			return
		}
		_ = s.ObserveOpenClaw(ctx, e.instance, e.event, e.at)
	}
}
func (s *Store) CachedGrowth(ctx context.Context, owner string, now time.Time) (*Growth, error) {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	if epoch, e := s.analyticsEpoch(ctx); e != nil {
		return nil, e
	} else if epoch != nil {
		snapshot, e := s.publishedOwner(ctx, owner, now)
		if e != nil {
			return nil, e
		}
		return snapshot.Growth, nil
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	key := Key(owner, now.In(loc).Format("2006-01-02"))
	s.registerRead(owner, key)
	for {
		s.perf.low.mu.Lock()
		if e, ok := s.perf.low.growth[key]; ok && !now.Before(e.at) && now.Sub(e.at) < summaryInterval {
			s.perf.low.mu.Unlock()
			return e.data, nil
		}
		if done, ok := s.perf.low.growthBusy[key]; ok {
			s.perf.low.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}
		if s.perf.low.growthBusy == nil {
			s.perf.low.growthBusy = map[string]chan struct{}{}
		}
		done := make(chan struct{})
		s.perf.low.growthBusy[key] = done
		revision := s.perf.cacheRevision.Load()
		s.perf.low.mu.Unlock()
		release, budgetErr := s.readBudget(ctx)
		var data *Growth
		err := budgetErr
		if err == nil {
			data, err = s.Growth(ctx, owner, now)
			release()
		}
		s.perf.low.mu.Lock()
		delete(s.perf.low.growthBusy, key)
		close(done)
		if err == nil && revision == s.perf.cacheRevision.Load() {
			if s.perf.low.growth == nil || len(s.perf.low.growth) >= 2048 {
				s.perf.low.growth = map[string]growthRead{}
			}
			s.perf.low.growth[key] = growthRead{now, data}
		}
		s.perf.low.mu.Unlock()
		return data, err
	}
}

func (s *Store) readBudget(ctx context.Context) (func(), error) {
	s.perf.low.mu.Lock()
	if s.perf.low.slots == nil {
		s.perf.low.slots = make(chan struct{}, 2)
	}
	slots := s.perf.low.slots
	s.perf.low.mu.Unlock()
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type reportRead struct {
	at   time.Time
	data *Report
}

func (s *Store) CachedReport(ctx context.Context, owner, kind, date string, now time.Time) (*Report, error) {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	if epoch, e := s.analyticsEpoch(ctx); e != nil {
		return nil, e
	} else if epoch != nil {
		snapshot, e := s.publishedOwner(ctx, owner, now)
		if e != nil {
			return nil, e
		}
		start, _, e := reportWindow(kind, date, now)
		if e != nil {
			return nil, e
		}
		if r := snapshot.Reports[kind]; r != nil && r.Start.Equal(start) {
			return r, nil
		}
		return s.Report(ctx, owner, kind, date, now.Truncate(summaryInterval), false)
	}
	key := Key("report-read", owner, kind, date)
	s.registerRead(owner, key)
	for {
		s.perf.low.mu.Lock()
		if e, ok := s.perf.low.reports[key]; ok && !now.Before(e.at) && now.Sub(e.at) < summaryInterval {
			s.perf.low.mu.Unlock()
			return e.data, nil
		}
		if done, ok := s.perf.low.growthBusy[key]; ok {
			s.perf.low.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}
		if s.perf.low.growthBusy == nil {
			s.perf.low.growthBusy = map[string]chan struct{}{}
		}
		done := make(chan struct{})
		s.perf.low.growthBusy[key] = done
		revision := s.perf.cacheRevision.Load()
		s.perf.low.mu.Unlock()
		release, err := s.readBudget(ctx)
		var data *Report
		if err == nil {
			data, err = s.ReadReport(ctx, owner, kind, date, now)
			release()
		}
		s.perf.low.mu.Lock()
		delete(s.perf.low.growthBusy, key)
		close(done)
		if err == nil && revision == s.perf.cacheRevision.Load() {
			if s.perf.low.reports == nil || len(s.perf.low.reports) >= 2048 {
				s.perf.low.reports = map[string]reportRead{}
			}
			s.perf.low.reports[key] = reportRead{now, data}
		}
		s.perf.low.mu.Unlock()
		return data, err
	}
}
