package services

import (
	"context"
	"log"
	"sync"
	"time"
)

type SkillPackageMaterializeWorker struct {
	service            *SkillPackageMaterializeService
	tick               time.Duration
	batchSize          int
	concurrency        int
	perInstanceLimit   int
	enabled            bool

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
}

func NewSkillPackageMaterializeWorker(service *SkillPackageMaterializeService, tick time.Duration, batchSize, concurrency, perInstanceLimit int, enabled bool) *SkillPackageMaterializeWorker {
	if tick <= 0 {
		tick = 2 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 5
	}
	if concurrency <= 0 {
		concurrency = 5
	}
	if perInstanceLimit <= 0 {
		perInstanceLimit = 2
	}
	return &SkillPackageMaterializeWorker{
		service:          service,
		tick:             tick,
		batchSize:        batchSize,
		concurrency:      concurrency,
		perInstanceLimit: perInstanceLimit,
		enabled:          enabled,
	}
}

func (w *SkillPackageMaterializeWorker) Start() {
	if w == nil || !w.enabled || w.service == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return
	}
	w.stopChan = make(chan struct{})
	w.running = true
	go w.loop(w.stopChan)
}

func (w *SkillPackageMaterializeWorker) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	close(w.stopChan)
	w.running = false
}

func (w *SkillPackageMaterializeWorker) loop(stop <-chan struct{}) {
	ctx := context.Background()
	if count, err := w.service.BackfillOnce(ctx, 500); err != nil {
		log.Printf("skill package materialize backfill failed: %v", err)
	} else if count > 0 {
		log.Printf("skill package materialize backfill enqueued %d jobs", count)
	}

	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.processBatch(context.Background())
		}
	}
}

func (w *SkillPackageMaterializeWorker) processBatch(ctx context.Context) {
	jobs, err := w.service.ClaimNextPending(ctx, w.batchSize)
	if err != nil {
		log.Printf("skill package materialize claim failed: %v", err)
		return
	}
	if len(jobs) == 0 {
		return
	}

	sem := make(chan struct{}, w.concurrency)
	instanceActive := make(map[int]int)
	var instanceMu sync.Mutex
	var wg sync.WaitGroup

	for _, job := range jobs {
		instanceMu.Lock()
		if instanceActive[job.InstanceID] >= w.perInstanceLimit {
			instanceMu.Unlock()
			if err := w.service.ReleaseToPending(job.ID); err != nil {
				log.Printf("skill package materialize release job %d failed: %v", job.ID, err)
			}
			continue
		}
		instanceActive[job.InstanceID]++
		instanceMu.Unlock()

		job := job
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				instanceMu.Lock()
				instanceActive[job.InstanceID]--
				if instanceActive[job.InstanceID] <= 0 {
					delete(instanceActive, job.InstanceID)
				}
				instanceMu.Unlock()
			}()
			if err := w.service.ProcessJob(ctx, job.ID); err != nil {
				log.Printf("skill package materialize job %d failed: %v", job.ID, err)
			}
		}()
	}
	wg.Wait()
}
