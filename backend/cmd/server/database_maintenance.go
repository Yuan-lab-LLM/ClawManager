package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/upper/db/v4"
)

func observationBytes(name string, fallback int64) (int64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be positive bytes", name)
	}
	return n, nil
}
func configureDatabaseMaintenance(database db.Session, store services.ObjectStorageService) (*observability.EncryptedObjects, error) {
	encoded := os.Getenv("CLAWMANAGER_OBSERVABILITY_KEY")
	key, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil || len(key) != 32 {
		return nil, errors.New("CLAWMANAGER_OBSERVABILITY_KEY must contain a stable base64-encoded 32-byte secret; initialize the dedicated secret before rollout")
	}
	dir := os.Getenv("CLAWMANAGER_OBSERVABILITY_DIR")
	if dir == "" {
		return nil, errors.New("CLAWMANAGER_OBSERVABILITY_DIR must point to the dedicated durable shared volume")
	}
	request, e := observationBytes("CLAWMANAGER_OBSERVABILITY_REQUEST_BYTES", 64<<20)
	if e != nil {
		return nil, e
	}
	if request < 64<<20 {
		return nil, errors.New("observability request reservation must be at least 64 MiB")
	}
	objects, e := observability.NewEncryptedObjects(store, key)
	if e != nil {
		return nil, e
	}
	journal, e := observability.NewJournal(dir, key, observability.Limits{RequestBytes: request})
	if e != nil {
		return nil, e
	}
	repository.ConfigureObservability(database, journal, objects)
	return objects, nil
}
func startDatabaseMaintenance(ctx context.Context, database db.Session, objects *observability.EncryptedObjects, cfg config.DatabaseConfig, trialRuntime bool) error {
	report := func(e error) { log.Printf("Database maintenance deferred; recovery records retained: %v", e) }
	go repository.RunObservability(ctx, report)
	go repository.RunObservationBodyMigration(ctx, report)
	go runAuditReportMaintenance(ctx, database, report)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				work, cancel := context.WithTimeout(ctx, 30*time.Second)
				err := repository.ReconcileObservationDay(work, database)
				cancel()
				if err != nil {
					report(err)
				}
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			work, cancel := context.WithTimeout(ctx, 30*time.Second)
			var err error
			for work.Err() == nil {
				var count int
				count, err = repository.ReconcileObservationSummaries(work, database, 500)
				if err == nil {
					var costs int
					costs, err = repository.ReconcileCostSummaries(work, database, 500)
					count += costs
				}
				if err != nil || count == 0 {
					break
				}
			}
			cancel()
			if err != nil {
				report(err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				work, cancel := context.WithTimeout(ctx, 30*time.Second)
				_, e := repository.ReclaimObservationObjects(work, 500)
				cancel()
				if e != nil {
					report(e)
				}
			}
		}
	}()
	return startHistoryMaintenance(ctx, database, objects, cfg, trialRuntime)
}
