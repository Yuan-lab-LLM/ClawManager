// system-backup-controller owns installation-local coordination and safe
// unstarted-task recovery. It does not start the application or run migrations.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clawreef/internal/systembackup"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func run(ctx context.Context) error {
	path := os.Getenv("SYSTEM_BACKUP_CONTROL_DSN_FILE")
	identity := os.Getenv("SYSTEM_BACKUP_INSTALLATION_ID")
	owner := os.Getenv("SYSTEM_BACKUP_CONTROLLER_ID")
	if path == "" || identity == "" || owner == "" {
		return errors.New("controller bootstrap configuration missing")
	}
	db, e := systembackup.OpenControlDatabase(ctx, path)
	if e != nil {
		return e
	}
	defer db.Close()
	store := systembackup.Store{DB: db, InstallationID: identity}
	if e = store.Bootstrap(ctx); e != nil {
		return errors.New("controller identity or schema unavailable")
	}
	controller := systembackup.Controller{Store: store, Owner: owner, PollInterval: 5 * time.Second}
	if configPath := os.Getenv("SYSTEM_BACKUP_CAPTURE_RUNNER_FILE"); configPath != "" {
		config, e := rest.InClusterConfig()
		if e != nil {
			return errors.New("capture runner requires in-cluster identity")
		}
		config.Timeout = 10 * time.Second
		client, e := kubernetes.NewForConfig(config)
		if e != nil {
			return errors.New("capture runner client unavailable")
		}
		runner, closeRunner, e := systembackup.LoadCaptureTaskRunner(ctx, configPath, client)
		if e != nil {
			return e
		}
		defer closeRunner()
		controller.Capturer = runner
	}
	if configPath := os.Getenv("SYSTEM_BACKUP_VERIFY_RUNNER_FILE"); configPath != "" {
		info, e := os.Stat(configPath)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
			return errors.New("verification runner configuration unavailable")
		}
		raw, e := os.ReadFile(configPath)
		if e != nil {
			return errors.New("verification runner configuration unavailable")
		}
		var template systembackup.VerifyJobRequest
		if systembackup.DecodeStrictJSON(raw, &template) != nil {
			return errors.New("verification runner configuration invalid")
		}
		// Per-task identities and budgets are assigned from the control store.
		template.Origin = identity
		template.BackupID = "sbk_00000000000000000000000000000000"
		template.MaxBytes = 1 << 20
		template.DeadlineSeconds = 1
		if e = template.Validate(); e != nil {
			return e
		}
		config, e := rest.InClusterConfig()
		if e != nil {
			return errors.New("verification runner requires in-cluster identity")
		}
		config.Timeout = 10 * time.Second
		client, e := kubernetes.NewForConfig(config)
		if e != nil {
			return errors.New("verification runner client unavailable")
		}
		controller.Verifier = &systembackup.VerifyTaskRunner{Jobs: systembackup.VerifyJobs{Client: client}, Template: template}
	}
	return controller.Run(ctx)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx); e != nil && !errors.Is(e, context.Canceled) {
		log.Print("system backup controller stopped: ", systembackup.PublicError(e))
		os.Exit(1)
	}
}
