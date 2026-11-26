// Standalone deployment migration job; shares the application runner and its
// capture exclusion. Does not initialize backup config, enrollment or keys.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appdb "clawreef/internal/db"
	"github.com/go-sql-driver/mysql"
)

func main() {
	log.SetOutput(io.Discard) // Never emit raw driver errors or connection inputs.
	if run() != nil {
		fmt.Fprintln(os.Stderr, "migration rejected or failed; partial DDL may exist; retain the same target/catalog for recovery")
		os.Exit(1)
	}
}
func run() error {
	apply := flag.Bool("apply", false, "apply exact pinned catalog using application migration/capture locks")
	prepareTrialRuntime := flag.Bool("prepare-trial-runtime", false, "prepare durable application admission in a pinned isolated trial target")
	catalogOnly := flag.Bool("catalog-only", false, "print embedded public migration catalog without opening a database")
	dsnFile := flag.String("dsn-file", "", "mounted credential file (never command-line DSN)")
	server := flag.String("expected-server-uuid", "", "deployment-pinned database server UUID")
	name := flag.String("expected-database", "", "deployment-pinned database name")
	digest := flag.String("expected-catalog-sha256", "", "deployment-pinned embedded catalog digest")
	flag.Parse()
	if flag.NArg() != 0 || *apply == *catalogOnly {
		return errors.New("choose exactly one mode")
	}
	catalog, err := appdb.EmbeddedMigrationCatalog()
	if err != nil {
		return err
	}
	if *catalogOnly {
		if *prepareTrialRuntime || *dsnFile != "" || *server != "" || *name != "" || *digest != "" {
			return errors.New("unexpected database input")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"algorithm": catalog.Algorithm, "sha256": catalog.Hash, "file_count": len(catalog.Filenames), "filenames": catalog.Filenames})
	}
	if *digest != catalog.Hash || *name == "" || *server == "" {
		return errors.New("missing or invalid deployment pins")
	}
	f, err := os.Open(*dsnFile)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(f, 8193))
	closeErr := f.Close()
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	if err != nil || closeErr != nil || len(raw) == 0 || len(raw) > 8192 {
		return errors.New("credential input invalid")
	}
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil || cfg.DBName != *name || cfg.MultiStatements {
		return errors.New("database input mismatch")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = 60 * time.Second
	cfg.WriteTimeout = 60 * time.Second
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	// The driver's logger is independent of the standard logger disabled in
	// main. Connection-loss diagnostics must obey the same public error boundary.
	cfg.Logger = &mysql.NopLogger{}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}
	database := sql.OpenDB(connector)
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(0)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, done := context.WithTimeout(ctx, 10*time.Minute)
	defer done()
	if err = appdb.ApplyPinnedMigrations(ctx, database, appdb.MigrationTarget{
		ServerUUID: *server, Database: *name, CatalogSHA256: *digest,
		PrepareTrialRuntime: *prepareTrialRuntime,
	}); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"result": "PASS", "migration_catalog_sha256": catalog.Hash, "migration_count": len(catalog.Filenames)})
}
