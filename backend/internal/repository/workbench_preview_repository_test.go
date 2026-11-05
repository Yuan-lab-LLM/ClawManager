package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/workbench"
	"github.com/upper/db/v4/adapter/mysql"
)

func TestWorkbenchPreviewPersistsAcrossReplicasWithoutRawCapabilities(t *testing.T) {
	port := os.Getenv("TEST_WORKBENCH_MYSQL_PORT")
	if port == "" {
		t.Skip("requires the disposable localhost workbench MySQL container")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		t.Fatal("invalid disposable localhost MySQL port")
	}
	connection := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Password: os.Getenv("TEST_WORKBENCH_MYSQL_PASSWORD"), Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(connection)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("workbench_preview_%d", time.Now().UnixNano())
	if _, err := root.SQL().Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.SQL().Exec("DROP DATABASE " + name); _ = root.Close() })
	connection.Database = name
	session, err := mysql.Open(connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	for _, query := range []string{"CREATE TABLE instances(id INT PRIMARY KEY, user_id INT NOT NULL, runtime_generation INT NOT NULL)", "INSERT INTO instances VALUES (42,7,3)"} {
		if _, err := session.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../db/migrations/101_add_instance_workbench_previews.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.SQL().Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	store := NewWorkbenchPreviewStore(session)
	first, err := workbench.NewPreviews("http://p-{token}.localhost:9001", store, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	scope := workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}
	lease, err := first.Create(context.Background(), scope, "localhost:3000/login?next=home")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := workbench.NewPreviews("http://p-{token}.localhost:9001", NewWorkbenchPreviewStore(session), time.Hour)
	address, _ := url.Parse(first.URL(lease))
	resolved, err := second.LookupHost(context.Background(), address.Host)
	if err != nil || resolved.Scope != scope || resolved.Target.Port != 3000 {
		t.Fatalf("persisted preview not resolved by another registry: %+v %v", resolved, err)
	}
	var stored struct {
		Hash string `db:"token_hash"`
		URL  string `db:"target_url"`
	}
	if err := session.Collection("instance_workbench_previews").Find().One(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Hash) != 64 || strings.Contains(stored.Hash, lease.ID) || !strings.Contains(stored.URL, "localhost:3000/login") {
		t.Fatal("raw capability persisted or target corrupted")
	}
	if _, err := first.Create(context.Background(), workbench.PreviewScope{InstanceID: 42, UserID: 8, Generation: 3, GatewayID: "gw-42-3"}, "localhost:3000"); !errors.Is(err, workbench.ErrPreviewScope) {
		t.Fatalf("storage accepted an unrelated owner: %v", err)
	}
	for i := 1; i < maxWorkbenchPreviewsPerInstance; i++ {
		if _, err := first.Create(context.Background(), scope, "localhost:3000"); err != nil {
			t.Fatalf("preview %d rejected before limit: %v", i, err)
		}
	}
	if _, err := first.Create(context.Background(), scope, "localhost:3000"); !errors.Is(err, workbench.ErrPreviewLimit) {
		t.Fatalf("preview limit was not enforced: %v", err)
	}
	if _, err := session.SQL().Exec("UPDATE instance_workbench_previews SET expires_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 SECOND)"); err != nil {
		t.Fatal(err)
	}
	if _, err := second.LookupHost(context.Background(), address.Host); !errors.Is(err, workbench.ErrPreviewNotFound) {
		t.Fatalf("expired persisted capability accepted: %v", err)
	}
	if _, err := first.Create(context.Background(), scope, "localhost:3000"); err != nil {
		t.Fatalf("expired previews were not released: %v", err)
	}
}
