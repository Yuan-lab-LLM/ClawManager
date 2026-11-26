package main

import (
	"context"
	"database/sql"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

type trialHistorySession struct {
	db.Session
	pool *sql.DB
}

func (s trialHistorySession) Driver() interface{} { return s.pool }

func TestTrialRuntimeConfiguresHistoryReaderWithoutStartingMaintenance(t *testing.T) {
	t.Setenv("CLAWMANAGER_HISTORY_RESTORE_DSN_FILE", "")
	session := trialHistorySession{pool: new(sql.DB)}
	previous := databaseHistory
	t.Cleanup(func() {
		databaseHistory = previous
		repository.ConfigureHistoryReader(session, nil)
	})

	err := startHistoryMaintenance(context.Background(), session, &observability.EncryptedObjects{}, config.DatabaseConfig{}, true)
	if err != nil {
		t.Fatalf("configure restored runtime history reader: %v", err)
	}
	if databaseHistory == nil || databaseHistory.DB != session.pool {
		t.Fatal("restored runtime history reader was not configured")
	}
}
