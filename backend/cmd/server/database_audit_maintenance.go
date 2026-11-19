package main

import (
	"context"
	"time"

	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

func runAuditReportMaintenance(ctx context.Context, database db.Session, report func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		for work.Err() == nil {
			_, ready, err := repository.ReconcileAuditReports(work, database, 500)
			if err != nil {
				if work.Err() == nil {
					report(err)
				}
				break
			}
			if ready {
				break
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
