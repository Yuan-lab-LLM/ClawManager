package maintenancecli

import (
	"clawreef/internal/config"
	"context"
	"testing"
)

func TestMaintenanceCLIRequiresIndependentBackupBoundaryBeforeConnecting(t *testing.T) {
	t.Setenv("SYSTEM_BACKUP_SQL_GATE_SCOPE", "")
	sess, release, err := Open(context.Background(), config.DatabaseConfig{Host: "invalid.example", Port: 1})
	if err == nil || sess != nil || release != nil {
		t.Fatal("missing writer boundary allowed a connection/mutation")
	}
}
