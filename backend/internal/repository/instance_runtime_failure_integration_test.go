package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/upper/db/v4/adapter/mysql"
)

// Opt-in against an isolated localhost MySQL, never the application database.
func TestRuntimeFailureMySQLContract(t *testing.T) {
	port := os.Getenv("TEST_RUNTIME_MYSQL_PORT")
	if port == "" {
		t.Skip("set TEST_RUNTIME_MYSQL_PORT for isolated database acceptance")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			t.Fatal("invalid localhost port")
		}
	}
	url := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := fmt.Sprintf("runtime_contract_%d", time.Now().UnixNano())
	if _, err := root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	defer root.SQL().Exec("DROP DATABASE " + name)
	url.Database = name
	sess, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	for _, query := range []string{
		"CREATE TABLE instances (id INT PRIMARY KEY, status VARCHAR(32), runtime_generation INT, runtime_error_message TEXT NULL, updated_at DATETIME(6))",
		"CREATE TABLE llm_models (id INT PRIMARY KEY, is_active BOOL)",
		"INSERT INTO instances VALUES (89,'running',8,NULL,NOW())",
	} {
		if _, err := sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../db/migrations/069_add_instance_runtime_error_code.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	r := &instanceRepository{sess: sess}
	ctx := context.Background()
	if claimed, err := r.FenceRuntimeRestart(ctx, 89, 8); err != nil || claimed {
		t.Fatalf("no-model restart claimed=%v err=%v", claimed, err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO llm_models VALUES (1,TRUE)"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := r.FenceRuntimeRestart(ctx, 89, 8); err != nil || !claimed {
		t.Fatalf("valid restart claimed=%v err=%v", claimed, err)
	}
	if claimed, err := r.FenceRuntimeRestart(ctx, 89, 8); err != nil || claimed {
		t.Fatalf("stale restart claimed=%v err=%v", claimed, err)
	}
	if err := r.UpdateRuntimeFailure(ctx, 89, 9, "no models", "MODEL_CONFIGURATION_REQUIRED"); err != nil {
		t.Fatal(err)
	}
	var state struct {
		Status     string  `db:"status"`
		Generation int     `db:"runtime_generation"`
		Code       *string `db:"runtime_error_code"`
	}
	read := func() {
		t.Helper()
		if err := sess.Collection("instances").Find("id", 89).One(&state); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if state.Code == nil || *state.Code != "MODEL_CONFIGURATION_REQUIRED" || state.Status != "error" {
		t.Fatalf("lost typed failure: %+v", state)
	}
	if err := r.UpdateRuntimeState(ctx, 89, "running", 8, nil); !errors.Is(err, ErrStaleRuntimeGeneration) {
		t.Fatalf("stale heartbeat: %v", err)
	}
	if err := r.UpdateRuntimeState(ctx, 89, "running", 9, nil); err != nil {
		t.Fatal(err)
	}
	read()
	if state.Code != nil || state.Status != "running" || state.Generation != 9 {
		t.Fatalf("stale code survives recovery: %+v", state)
	}
}
