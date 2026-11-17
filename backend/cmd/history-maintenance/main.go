// history-maintenance performs one bounded, backup-fenced maintenance batch.
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/config"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/maintenancecli"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

type archiveObjects struct{}

func (archiveObjects) Put(ctx context.Context, owner, key string, raw []byte) ([]byte, error) {
	return repository.DurableHistoryArchivePut(ctx, owner, key, raw)
}
func (archiveObjects) Read(ctx context.Context, owner string, ref []byte) ([]byte, error) {
	return repository.DurableArchiveRead(ctx, owner, json.RawMessage(ref))
}
func (archiveObjects) RestoreDetail(ctx context.Context, kind string, raw []byte) ([]byte, error) {
	return repository.HydrateArchivedPayload(ctx, kind, json.RawMessage(raw))
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("history-maintenance", flag.ContinueOnError)
	mode := flags.String("mode", "", "backfill, verify, rollback, prove, log-backfill, log-catchup, log-verify, or log-cutover")
	kind := flags.String("kind", "", "registered history kind for backfill")
	month := flags.String("month", "", "registered monthly table for verify/rollback")
	since := flags.String("since", "", "UTC RFC3339 lower bound for backfill")
	until := flags.String("until", "", "UTC RFC3339 upper bound for backfill")
	after := flags.Int64("after", 0, "durable last original ID from the prior batch")
	limit := flags.Int("limit", 500, "one batch, 1..500 rows / 30 seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *limit < 1 || *limit > 500 || *after < 0 {
		return fmt.Errorf("bounded batch required")
	}
	if *mode != "backfill" && *mode != "verify" && *mode != "rollback" && *mode != "prove" && *mode != "log-backfill" && *mode != "log-catchup" && *mode != "log-verify" && *mode != "log-cutover" {
		return fmt.Errorf("explicit supported -mode required")
	}
	var start, end time.Time
	var err error
	if *mode == "backfill" || *mode == "log-backfill" {
		start, err = time.Parse(time.RFC3339, *since)
		if err != nil {
			return err
		}
		end, err = time.Parse(time.RFC3339, *until)
		if err != nil || !end.After(start) || end.Sub(start) > 90*24*time.Hour {
			return fmt.Errorf("backfill requires an increasing range of at most90days")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.ObjectStorage.Endpoint == "" {
		return fmt.Errorf("maintenance requires the serving durable object storage endpoint")
	}
	objects, err := services.NewObjectStorageService(cfg.ObjectStorage)
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("CLAWMANAGER_OBSERVABILITY_KEY"))
	if err != nil || len(key) != 32 {
		return fmt.Errorf("stable observability key required")
	}
	defer clear(key)
	sess, release, err := maintenancecli.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer release()
	journal, err := observability.NewJournal(os.Getenv("CLAWMANAGER_OBSERVABILITY_DIR"), key, observability.Limits{})
	if err != nil {
		return err
	}
	encrypted, err := observability.NewEncryptedObjects(objects, key)
	if err != nil {
		return err
	}
	undo := repository.ConfigureObservability(sess, journal, encrypted)
	defer undo()
	pool, ok := sess.Driver().(*sql.DB)
	if !ok {
		return fmt.Errorf("SQL pool required")
	}
	service := historymaintenance.New(pool, archiveObjects{}, func(ctx context.Context) (func(), error) { return backupgate.HoldWorkUnit(ctx, "history.cli") })
	closeDDL, err := service.ConfigureMonthlyDDL(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer closeDDL()
	result := map[string]any{"mode": *mode, "limit": *limit, "after_id": *after}
	switch *mode {
	case "log-catchup":
		processed, e := service.CatchupLogs(ctx, *kind, *limit)
		err = e
		result["processed"] = processed
	case "log-backfill":
		last, processed, e := service.BackfillLogs(ctx, *kind, start, end, *after, *limit)
		err = e
		result["last_id"], result["processed"] = last, processed
		// A nonempty prefix may stop on bytes before the row limit. Resume from
		// last_id until an empty batch explicitly proves the range exhausted.
		result["complete"] = processed == 0
	case "log-verify":
		last, complete, e := service.VerifyLogMonth(ctx, *month, *after)
		err = e
		result["last_id"], result["complete"] = last, complete
	case "log-cutover":
		err = service.CutoverLogMonth(ctx, *month)
	case "backfill":
		last, processed, e := service.Backfill(ctx, *kind, start, end, *after, *limit)
		err = e
		result["last_id"], result["processed"] = last, processed
	case "verify":
		last, complete, e := service.VerifyMonth(ctx, *month, *after)
		err = e
		result["last_id"], result["complete"] = last, complete
	case "rollback":
		err = service.RollbackMonth(ctx, *month)
	case "prove":
		target, e := service.OpenRestoreTarget(ctx, os.Getenv("CLAWMANAGER_HISTORY_RESTORE_DSN_FILE"))
		if e != nil {
			return e
		}
		defer target.Close()
		processed, e := service.ProvePending(ctx, target, *limit)
		err = e
		result["processed"] = processed
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
