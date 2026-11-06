package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"clawreef/internal/browserorigin"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

// This fixture only touches a pre-created EMPTY disposable schema. It refuses
// application schemas and cleans up only tables created after that check.
func browserOriginMySQL(t *testing.T) (db.Session, mysql.ConnectionURL) {
	t.Helper()
	dsn := os.Getenv("CLAWMANAGER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLAWMANAGER_TEST_MYSQL_DSN not configured; requires disposable MySQL")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_") {
		t.Fatal("refusing invalid or non-test database")
	}
	connection := mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true", "charset": "utf8mb4"}}
	sess, err := mysql.Open(connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("refusing non-empty test database")
	}
	t.Cleanup(func() {
		for _, table := range []string{"browser_origin_ports", "instances"} {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + table)
		}
	})
	for _, statement := range []string{
		"CREATE TABLE instances (id INT PRIMARY KEY,user_id INT NOT NULL DEFAULT 7,created_at DATETIME(6) NOT NULL DEFAULT '2026-11-06 00:00:00',workspace_path VARCHAR(512) NULL,runtime_generation INT NOT NULL DEFAULT 1) ENGINE=InnoDB",
		"INSERT INTO instances(id) VALUES (42),(43),(44),(45)",
	} {
		if _, err = sess.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../db/migrations/102_add_browser_origin_ports.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	return sess, connection
}

func TestBrowserOriginMySQLCreationIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes []string
		reject  bool
	}{
		{"recreated ID", []string{"DELETE FROM instances WHERE id=42", "INSERT INTO instances(id,user_id,created_at) VALUES(42,7,'2026-11-06 00:00:00.000001')"}, true},
		{"owner transfer", []string{"UPDATE instances SET user_id=8 WHERE id=42"}, true},
		{"workspace relocation", []string{"UPDATE instances SET workspace_path='/relocated/workspaces/instance42' WHERE id=42"}, false},
		{"runtime generation", []string{"UPDATE instances SET runtime_generation=runtime_generation+1 WHERE id=42"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, _ := browserOriginMySQL(t)
			store := NewBrowserOriginStore(session)
			ctx := context.Background()
			const first, last = 31000, 31009
			native, err := store.Bind(ctx, 42, browserorigin.Native, "opencode", first, last)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := store.Bind(ctx, 42, browserorigin.Preview, "http://localhost:3000/", first, last)
			if err != nil {
				t.Fatal(err)
			}
			for _, query := range tc.changes {
				if _, err := session.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			for _, binding := range []browserorigin.Binding{native, preview} {
				got, lookupErr := store.ByPort(ctx, binding.Port)
				rebound, bindErr := store.Bind(ctx, 42, binding.Kind, binding.Target, first, last)
				if tc.reject {
					if !errors.Is(lookupErr, browserorigin.ErrNotFound) {
						t.Errorf("changed identity resolved old origin %+v: %v", got, lookupErr)
					}
					if !errors.Is(bindErr, browserorigin.ErrNotFound) {
						t.Errorf("changed identity rebound old origin %+v: %v", rebound, bindErr)
					}
				} else if lookupErr != nil || bindErr != nil || got != binding || rebound != binding {
					t.Errorf("stable identity lost origin: lookup=%+v %v bind=%+v %v", got, lookupErr, rebound, bindErr)
				}
			}
			newBinding, err := store.Bind(ctx, 42, browserorigin.Native, "deepseek-harness", first, last)
			if tc.reject {
				if !errors.Is(err, browserorigin.ErrNotFound) {
					t.Errorf("changed identity allocated unseen target %+v: %v", newBinding, err)
				}
				if _, err = store.Bind(ctx, 43, browserorigin.Native, "opencode", first, first); !errors.Is(err, browserorigin.ErrCapacity) {
					t.Errorf("old origin recycled: %v", err)
				}
			} else if err != nil {
				t.Fatalf("stable identity cannot allocate target: %v", err)
			}
		})
	}
}

func TestBrowserOriginStoreInvalidInput(t *testing.T) {
	s := NewBrowserOriginStore(nil)
	if _, err := s.Bind(context.Background(), 42, browserorigin.Native, "opencode", 31000, 31001); !errors.Is(err, browserorigin.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.ByPort(context.Background(), 31000); !errors.Is(err, browserorigin.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestBrowserOriginMySQLPermanentConcurrentAllocation(t *testing.T) {
	session, connection := browserOriginMySQL(t)
	other, err := mysql.Open(connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	stores := []browserorigin.Store{NewBrowserOriginStore(session), NewBrowserOriginStore(other)}
	ctx := context.Background()
	const first, last = 31000, 31099
	// Independent sessions represent concurrent manager replicas. All callers
	// racing on an identical identity must receive exactly one permanent port.
	var wg sync.WaitGroup
	results := make(chan browserorigin.Binding, 40)
	errorsCh := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, e := stores[i%2].Bind(ctx, 42, browserorigin.Native, "opencode", first, last)
			if e != nil {
				errorsCh <- e
			} else {
				results <- b
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("concurrent bind: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	var original browserorigin.Binding
	for b := range results {
		if original.Port == 0 {
			original = b
		}
		if b != original {
			t.Fatalf("unstable binding %+v != %+v", b, original)
		}
	}
	if original.Port != first {
		t.Fatalf("first port %+v", original)
	}
	b, err := stores[1].Bind(ctx, 43, browserorigin.Native, "opencode", first, last)
	if err != nil || b.Port == original.Port {
		t.Fatalf("different instance shares port: %+v %v", b, err)
	}
	for i := 0; i < 32; i++ {
		if _, err := stores[i%2].Bind(ctx, 42, browserorigin.Preview, fmt.Sprintf("http://localhost:%d/", 4000+i), first, last); err != nil {
			t.Fatalf("preview %d: %v", i, err)
		}
	}
	if _, err := stores[0].Bind(ctx, 42, browserorigin.Preview, "http://localhost:5000/", first, last); !errors.Is(err, browserorigin.ErrCapacity) {
		t.Fatalf("preview limit: %v", err)
	}
	if _, err := stores[1].Bind(ctx, 42, browserorigin.Preview, "http://localhost:4000", first, last); err != nil {
		t.Fatalf("existing target rejected at limit: %v", err)
	}
	if _, err := stores[0].Bind(ctx, 42, browserorigin.Native, "deepseek-harness", first, last); err != nil {
		t.Fatalf("preview limit rejected native: %v", err)
	}
	if _, err := stores[0].Bind(ctx, 999, browserorigin.Native, "opencode", first, last); !errors.Is(err, browserorigin.ErrNotFound) {
		t.Fatalf("nonexistent instance: %v", err)
	}
	if _, err := session.SQL().Exec("DELETE FROM instances WHERE id=42"); err != nil {
		t.Fatal(err)
	}
	if _, err := stores[1].ByPort(ctx, original.Port); !errors.Is(err, browserorigin.ErrNotFound) {
		t.Fatalf("deleted instance lookup: %v", err)
	}
	if _, err := stores[0].Bind(ctx, 42, browserorigin.Native, "opencode", first, last); !errors.Is(err, browserorigin.ErrNotFound) {
		t.Fatalf("deleted instance bind: %v", err)
	}
	// Shrinking the range to one assigned port must exhaust, even after deletion.
	if _, err := stores[1].Bind(ctx, 44, browserorigin.Native, "opencode", first, first); !errors.Is(err, browserorigin.ErrCapacity) {
		t.Fatalf("deleted origin recycled: %v", err)
	}
	row, err := session.SQL().QueryRow("SELECT instance_id FROM browser_origin_ports WHERE port=?", original.Port)
	if err != nil {
		t.Fatal(err)
	}
	var id int
	if err = row.Scan(&id); err != nil || id != 42 {
		t.Fatalf("permanent row missing: %d %v", id, err)
	}
	// Distinct targets also race across replicas, with no duplicate assignments.
	results = make(chan browserorigin.Binding, 24)
	errorsCh = make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, e := stores[i%2].Bind(ctx, 45, browserorigin.Preview, fmt.Sprintf("http://localhost:%d/", 6000+i), first, last)
			if e != nil {
				errorsCh <- e
			} else {
				results <- b
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("distinct concurrent bind: %v", err)
	}
	ports := map[int]bool{}
	for b := range results {
		if ports[b.Port] {
			t.Errorf("duplicate port %d", b.Port)
		}
		ports[b.Port] = true
	}
	if len(ports) != 24 {
		t.Fatalf("allocated %d/24", len(ports))
	}
	// Admission of distinct targets must remain bounded during concurrent calls.
	results = make(chan browserorigin.Binding, 40)
	errorsCh = make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, e := stores[i%2].Bind(ctx, 44, browserorigin.Preview, fmt.Sprintf("http://localhost:%d/", 7000+i), first, last)
			if e != nil {
				errorsCh <- e
			} else {
				results <- b
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	capacityErrors := 0
	for err := range errorsCh {
		if !errors.Is(err, browserorigin.ErrCapacity) {
			t.Errorf("concurrent preview admission: %v", err)
		}
		capacityErrors++
	}
	if len(results) != 32 || capacityErrors != 8 {
		t.Fatalf("preview admission successes=%d capacity errors=%d", len(results), capacityErrors)
	}
	resolved, err := stores[0].ByPort(ctx, b.Port)
	if err != nil || resolved != b {
		t.Fatalf("cross-replica lookup %+v %v", resolved, err)
	}
	if _, err := stores[0].ByPort(ctx, last); !errors.Is(err, browserorigin.ErrNotFound) {
		t.Fatalf("unassigned port lookup: %v", err)
	}
	for _, tc := range []struct {
		id, first, last int
		kind, target    string
	}{
		{42, 0, 10, browserorigin.Native, "opencode"},
		{42, 32000, 32512, browserorigin.Native, "opencode"},
		{42, 32000, 32001, browserorigin.Native, "hermes"},
		{42, 32000, 32001, browserorigin.Preview, "http://example.com:3000/"},
	} {
		if _, err := stores[0].Bind(ctx, tc.id, tc.kind, tc.target, tc.first, tc.last); !errors.Is(err, browserorigin.ErrInvalid) {
			t.Errorf("invalid store request: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := stores[0].Bind(ctx, 43, browserorigin.Native, "opencode", first, last); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := stores[0].ByPort(ctx, b.Port); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
