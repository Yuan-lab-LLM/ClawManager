package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

var databaseHistory *historymaintenance.Service

type databaseHistoryObjects struct{}

func (databaseHistoryObjects) Put(ctx context.Context, owner, key string, raw []byte) ([]byte, error) {
	return repository.DurableHistoryArchivePut(ctx, owner, key, raw)
}
func (databaseHistoryObjects) Read(ctx context.Context, owner string, reference []byte) ([]byte, error) {
	return repository.DurableArchiveRead(ctx, owner, json.RawMessage(reference))
}
func (databaseHistoryObjects) RestoreDetail(ctx context.Context, kind string, raw []byte) ([]byte, error) {
	return repository.HydrateArchivedPayload(ctx, kind, json.RawMessage(raw))
}
func startHistoryMaintenance(ctx context.Context, database db.Session, objects *observability.EncryptedObjects, cfg config.DatabaseConfig) error {
	pool, ok := database.Driver().(*sql.DB)
	if !ok || objects == nil {
		return errors.New("history maintenance requires SQL and encrypted objects")
	}
	service := historymaintenance.New(pool, databaseHistoryObjects{}, func(ctx context.Context) (func(), error) { return backupgate.HoldWorkUnit(ctx, "history.maintenance") })
	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var closeDDL func()
	if appdb.SQLGateScope() != "" {
		var err error
		closeDDL, err = service.ConfigureMonthlyDDL(openCtx, cfg)
		if err != nil {
			return err
		}
	}
	target, err := service.OpenRestoreTarget(openCtx, os.Getenv("CLAWMANAGER_HISTORY_RESTORE_DSN_FILE"))
	cancel()
	if err != nil {
		if closeDDL != nil {
			closeDDL()
		}
		return err
	}
	databaseHistory = service
	if err = service.PrepareMonths(ctx, time.Now().UTC()); err != nil {
		target.Close()
		if closeDDL != nil {
			closeDDL()
		}
		return err
	}
	repository.ConfigureHistoryReader(database, service)
	go func() {
		if closeDDL != nil {
			defer closeDDL()
		}
		defer target.Close()

		service.RunWithRestore(ctx, target, func(err error) { log.Printf("History maintenance deferred; original data retained: %v", err) })
	}()
	return nil
}
