package db

import (
	"clawreef/internal/config"
	"strings"
	"testing"
)

func TestBusinessProcessesRefuseIsolatedRestoreDatabase(t *testing.T) {
	cfg := config.DatabaseConfig{Host: "127.0.0.1", Port: 1, Database: "cm_restore_" + strings.Repeat("a", 32)}
	if _, e := Connect(cfg); e == nil || !strings.Contains(e.Error(), "cannot run business services") {
		t.Fatal("business connection attempted", e)
	}
	if _, e := Initialize(cfg); e == nil || !strings.Contains(e.Error(), "cannot run business services") {
		t.Fatal("business migrations attempted", e)
	}
}
