// observation-backfill prepares durable report rows before enabling the new API.
// It connects through the normal backup-fenced SQL path, never runs migrations,
// and leaves its durable watermarks intact on cancellation or backup admission.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clawreef/internal/config"
	database "clawreef/internal/db"
	"clawreef/internal/maintenancecli"
	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

func main() {
	check := flag.Bool("check", false, "read-only rollout preflight: fail until all report summaries are ready")
	duration := flag.Duration("timeout", 30*time.Minute, "maximum total catch-up time; each transaction contains at most 500 source rows")
	flag.Parse()
	if err := run(*check, *duration); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(check bool, duration time.Duration) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	var sess db.Session
	var closeSession func()
	if check {
		sess, err = database.Connect(cfg.Database)
		if err == nil {
			closeSession = func() { _ = sess.Close() }
		}
	} else {
		sess, closeSession, err = maintenancecli.Open(ctx, cfg.Database)
	}
	if err != nil {
		return err
	}
	defer closeSession()
	started := time.Now()
	for {
		var invocationReady bool
		var pending int
		row, e := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*)=2 AND MIN(ready) AND NOT EXISTS(SELECT 1 FROM observation_invocation_pending) AND NOT EXISTS(SELECT 1 FROM observation_cost_pending) FROM observation_summary_state WHERE id IN(1,2)`)
		if e != nil {
			return e
		}
		if e = row.Scan(&invocationReady); e != nil {
			return e
		}
		row, e = sess.SQL().QueryRowContext(ctx, `SELECT 5-COALESCE(SUM(complete),0)+(SELECT EXISTS(SELECT 1 FROM observation_audit_pending)) FROM observation_audit_backfill`)
		if e != nil {
			return e
		}
		if e = row.Scan(&pending); e != nil {
			return e
		}
		progress := map[string]interface{}{"invocation_and_cost_summaries_ready": invocationReady, "audit_sources_pending": pending, "elapsed_seconds": time.Since(started).Seconds()}
		_ = json.NewEncoder(os.Stdout).Encode(progress)
		if invocationReady && pending == 0 {
			return nil
		}
		if check {
			return fmt.Errorf("summary preflight failed: run observation-backfill before cutover")
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		round, done := context.WithTimeout(ctx, 30*time.Second)
		for round.Err() == nil {
			n, e := repository.ReconcileObservationSummaries(round, sess, 500)
			if e != nil {
				if round.Err() != nil {
					break
				}
				done()
				return e
			}
			_, e = repository.ReconcileCostSummaries(round, sess, 500)
			if e != nil {
				if round.Err() != nil {
					break
				}
				done()
				return e
			}
			_, ready, e := repository.ReconcileAuditReports(round, sess, 500)
			if e != nil {
				if round.Err() != nil {
					break
				}
				done()
				return e
			}
			if n == 0 && ready {
				row, e := sess.SQL().QueryRowContext(round, `SELECT f.id,f.invocation_id FROM observation_cost_facts f WHERE f.resolved=FALSE AND NOT EXISTS(SELECT 1 FROM observation_invocation_summaries i WHERE i.id=f.invocation_id) ORDER BY f.id LIMIT 1`)
				if e != nil {
					done()
					return e
				}
				var costID, invocationID int
				if e = row.Scan(&costID, &invocationID); e == nil {
					done()
					return fmt.Errorf("summary backfill requires missing invocation %d referenced by cost record %d; restore/replay the original invocation before cutover", invocationID, costID)
				} else if e != sql.ErrNoRows {
					done()
					return e
				}
				break
			}
		}
		done()
	}
}
