package systembackuptrial

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestTrialCrossClusterTransfer runs one side of a disposable cluster fixture.
// Run the backup and restore phases in separate Kubernetes API environments and
// move the sealed artifact and receipt between their independent mounts.
func TestTrialCrossClusterTransfer(t *testing.T) {
	phase := os.Getenv("CLAWMANAGER_TRIAL_CLUSTER_PHASE")
	if phase == "" {
		t.Skip("requires separate disposable Kubernetes source and restore fixtures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	service := New(os.Getenv)
	ready := service.readiness(ctx)
	task := Task{ID: newID()}
	switch phase {
	case "backup":
		if len(ready.Missing) != 0 {
			t.Fatalf("source admission: %v", ready.Missing)
		}
		if err := service.backup(ctx, &task); err != nil {
			t.Fatalf("source backup stage %s: %v", task.Stage, err)
		}
	case "restore":
		task.BackupID = os.Getenv("CLAWMANAGER_TRIAL_CLUSTER_BACKUP_ID")
		if !taskID.MatchString(task.BackupID) || len(ready.RestoreMissing) != 0 ||
			service.value("SOURCE_DSN_FILE") != "" || service.value("SOURCE_UUID") != "" || service.value("WRITE_DIR") != "" {
			t.Fatalf("invalid restore-only fixture or missing restore readiness: %v", ready.RestoreMissing)
		}
		if err := service.restore(ctx, &task); err != nil {
			t.Fatalf("target restore stage %s: %v", task.Stage, err)
		}
		verifyTrialUserAccess(t, service)
	default:
		t.Fatal("CLAWMANAGER_TRIAL_CLUSTER_PHASE must be backup or restore")
	}
	if !task.Readback || task.Counts["users"] != 2 || task.Counts["instances"] != 2 {
		t.Fatalf("unexpected trial fixture result: %+v", task)
	}
	reader, err := service.readerRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	sealed, err := (storage{reader: reader}).metadata(task.BackupID)
	if err != nil {
		t.Fatal(err)
	}
	task.Status, task.Stage = "succeeded", "done"
	result, err := json.Marshal(struct {
		Task
		CiphertextSHA256 string `json:"ciphertext_sha256"`
	}{Task: task, CiphertextSHA256: sealed.Receipt.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("trial_cluster_receipt %s", result)
}
