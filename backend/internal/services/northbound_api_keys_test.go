package services

import (
	"clawreef/internal/models"
	"clawreef/internal/nbkeys"
	"clawreef/internal/repository"
	"context"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4/adapter/mysql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAPIKeyMutationValidation(t *testing.T) {
	for _, scopes := range [][]string{nil, {"admin:*"}, {"pro-instances:delete"}} {
		req := APIKeyMutation{Name: "test", Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour)}
		if validateKeyMutation(&req, []string{"pro-instances:read"}) == nil {
			t.Fatal("invalid scopes accepted")
		}
	}
	p := models.NorthboundCallerPolicy{APIKeysJSON: func() *string { s := `[{"secret_hash":"sensitive"}]`; return &s }()}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "sensitive") {
		t.Fatal("key document leaked into caller serialization")
	}
}

// Real MySQL, explicitly local and isolated. No runtime instances are created.
func TestAPIKeyExistingGrantsMySQL(t *testing.T) {
	host := os.Getenv("NB_KEYS_TEST_MYSQL")
	if host == "" {
		t.Skip("local MySQL acceptance not configured")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") {
		t.Fatal("local disposable database required")
	}
	root, err := mysql.Open(mysql.ConnectionURL{Host: host, User: "root", Password: os.Getenv("NB_KEYS_TEST_PASSWORD"), Database: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := fmt.Sprintf("nbkeys_test_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	defer root.SQL().Exec("DROP DATABASE " + name)
	conn := mysql.ConnectionURL{Host: host, User: "root", Password: os.Getenv("NB_KEYS_TEST_PASSWORD"), Database: name, Options: map[string]string{"parseTime": "true"}}
	db, err := mysql.Open(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	must := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.SQL().Exec(sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE northbound_caller_policies (user_id INT PRIMARY KEY,enabled BOOLEAN,scopes_json JSON,created_at DATETIME(6),updated_at DATETIME(6),updated_by INT)`)
	must(`CREATE TABLE northbound_sessions (session_id VARCHAR(64),user_id INT,status VARCHAR(30),revoked_at DATETIME(6),updated_at DATETIME(6))`)
	must(`CREATE TABLE northbound_operations (id BIGINT PRIMARY KEY)`)
	must(`INSERT INTO northbound_sessions VALUES ('legacy',12,'active',NULL,NOW(6))`)
	user := fmt.Sprintf("nbk_%d", time.Now().UnixNano())
	_, password, err := nbkeys.New(12, "db", nil, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	// Existing table grant is deliberately applied BEFORE adding the columns.
	must("CREATE USER '" + user + "'@'%' IDENTIFIED BY '" + password + "'")
	defer root.SQL().Exec("DROP USER '" + user + "'@'%'")
	must("GRANT SELECT ON " + name + ".northbound_caller_policies TO '" + user + "'@'%'")
	migration, err := os.ReadFile("../db/migrations/066_add_northbound_api_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(sql) != "" {
			must(sql)
		}
	}
	// Idempotent even after an interrupted migration.
	for _, sql := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(sql) != "" {
			must(sql)
		}
	}
	repo := repository.NewNorthboundRepository(db)
	ctx := context.Background()
	p := &models.NorthboundCallerPolicy{UserID: 12, Enabled: true, Scopes: []string{"pro-instances:read", "pro-instances:restart"}}
	if err := repo.UpsertCallerPolicy(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetCallerPolicy(12)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := nbkeys.New(12, "test", []string{"pro-instances:read"}, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := repo.UpdateAPIKeys(ctx, 12, 0, true, before.ScopesJSON, []nbkeys.Key{key})
	if err != nil || !ok {
		t.Fatal("key save", ok, err)
	}
	ok, err = repo.UpdateAPIKeys(ctx, 12, 0, true, before.ScopesJSON, nil)
	if err != nil || ok {
		t.Fatal("stale update accepted")
	}
	// Saving old caller fields cannot erase the key document.
	p.Scopes = []string{"pro-instances:read"}
	if err := repo.UpsertCallerPolicy(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	ok, err = repo.UpdateAPIKeys(ctx, 12, 1, true, before.ScopesJSON, nil)
	if err != nil || ok {
		t.Fatal("stale policy ceiling accepted")
	}
	conn.User = user
	conn.Password = password
	restricted, err := mysql.Open(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer restricted.Close()
	actual, err := repository.NewNorthboundRepository(restricted).GetCallerPolicy(12)
	if err != nil {
		t.Fatal("existing SELECT grant must cover new columns", err)
	}
	keys, err := nbkeys.Decode(actual.APIKeysJSON)
	if err != nil || len(keys) != 1 || keys[0].ID != key.ID {
		t.Fatal("old policy update erased keys")
	}
	if _, err := restricted.SQL().Exec("UPDATE northbound_caller_policies SET enabled=FALSE"); err == nil {
		t.Fatal("Gateway unexpectedly has write access")
	}
	var state string
	row, err := db.SQL().QueryRow("SELECT status FROM northbound_sessions WHERE session_id='legacy'")
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&state); err != nil || state != "active" {
		t.Fatal("key update revoked session")
	}
}
