package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func workspaceFenceMySQL(t *testing.T) *instanceRepository {
	t.Helper()
	r := runtimeSecurityMySQL(t)
	for _, name := range []string{"068_add_workspace_write_intents.sql", "099_add_workspace_write_fences.sql", "100_add_nfs_external_writer_tracking.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(q) != "" {
				if _, err = r.sess.SQL().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	teamGateFixtureTables(t, r.sess)
	if _, err := r.sess.SQL().Exec("UPDATE instances SET instance_mode='lite',status='running' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWorkspaceWriteFencePersistsUnknownWritesAndAdvancesOnlyNewGeneration(t *testing.T) {
	r := workspaceFenceMySQL(t)
	ctx := context.Background()
	fence := NewWorkspaceWriteFenceRepository(r.sess)
	writes := NewWorkspaceWriteRepository(r.sess)
	if err := fence.Activate(ctx, 7, 3, 2, "pod-old"); err != nil {
		t.Fatal(err)
	}
	old := models.WorkspaceWriteIntent{InstanceID: 7, OwnerID: 3, Generation: 2, Operation: strings.Repeat("a", 32), Action: "edit"}
	if err := writes.Begin(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := fence.Activate(ctx, 7, 3, 2, "pod-old"); err != nil {
		t.Fatalf("exact open retry was blocked by its own current writer: %v", err)
	}
	if err := fence.Fence(ctx, 7, 3, 2, "pod-old"); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("unknown old write was not fenced: %v", err)
	}
	late := old
	late.Operation = strings.Repeat("b", 32)
	if err := writes.Begin(ctx, late); err == nil {
		t.Fatal("new management write entered committed stop fence")
	}
	// A backend restart and old timestamps never clear unresolved intent state.
	fence = NewWorkspaceWriteFenceRepository(r.sess)
	if err := fence.Fence(ctx, 7, 3, 2, "pod-old"); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatal("restart forgot old write")
	}
	if err := writes.Complete(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := fence.Fence(ctx, 7, 3, 2, "pod-old"); err != nil {
		t.Fatal(err)
	}
	if err := fence.Activate(ctx, 7, 3, 2, "pod-old"); err == nil {
		t.Fatal("same stopped generation was reopened")
	}
	if _, err := r.sess.SQL().Exec("UPDATE instances SET runtime_generation=3,status='creating' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	if err := fence.Activate(ctx, 7, 3, 3, "pod-new"); err != nil {
		t.Fatal(err)
	}
	if err := fence.Fence(ctx, 7, 3, 2, "pod-old"); err == nil {
		t.Fatal("delayed old stop changed a new execution fence")
	}
	if err := fence.Fence(ctx, 7, 3, 3, "wrong-pod"); err == nil {
		t.Fatal("wrong Pod closed current execution fence")
	}
	current := old
	current.Generation = 3
	current.Operation = strings.Repeat("c", 32)
	if err := writes.Begin(ctx, current); err != nil {
		t.Fatalf("new generation did not retain write access: %v", err)
	}
}

func TestWorkspaceWriteFenceAllowsOnlyConfirmedLifecycleErase(t *testing.T) {
	r := workspaceFenceMySQL(t)
	ctx := context.Background()
	fence := NewWorkspaceWriteFenceRepository(r.sess)
	writes := NewWorkspaceWriteRepository(r.sess)
	if _, err := r.sess.SQL().Exec("CREATE TABLE instance_runtime_bindings(instance_id INT)"); err != nil {
		t.Fatal(err)
	}
	if err := fence.Activate(ctx, 7, 3, 2, "pod-old"); err != nil {
		t.Fatal(err)
	}
	if err := fence.Fence(ctx, 7, 3, 2, "pod-old"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sess.SQL().Exec("UPDATE instances SET status='deleting' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	intent := models.WorkspaceWriteIntent{InstanceID: 7, OwnerID: 3, Generation: 2, Operation: strings.Repeat("e", 32), Action: "delete"}
	if err := writes.Begin(ctx, intent); err == nil {
		t.Fatal("public delete bypassed stopped generation admission")
	}
	intent.Action = "erase"
	if err := writes.Begin(ctx, intent); err != nil {
		t.Fatal(err)
	}
	second := intent
	second.Operation = strings.Repeat("f", 32)
	if err := writes.Begin(ctx, second); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("parallel lifecycle erase admitted: %v", err)
	}
	if err := writes.Complete(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sess.SQL().Exec("INSERT INTO instance_runtime_bindings VALUES(7)"); err != nil {
		t.Fatal(err)
	}
	if err := writes.Begin(ctx, second); err == nil {
		t.Fatal("lifecycle erase admitted while runtime binding remained")
	}
}

func TestWorkspaceWriteFenceSerializesWithConcurrentAdmission(t *testing.T) {
	r := workspaceFenceMySQL(t)
	ctx := context.Background()
	fence := NewWorkspaceWriteFenceRepository(r.sess)
	writes := NewWorkspaceWriteRepository(r.sess)
	for generation := 2; generation < 22; generation++ {
		if _, err := r.sess.SQL().Exec("UPDATE instances SET runtime_generation=?,status='running' WHERE id=7", generation); err != nil {
			t.Fatal(err)
		}
		pod := fmt.Sprintf("pod-%d", generation)
		if err := fence.Activate(ctx, 7, 3, generation, pod); err != nil {
			t.Fatal(err)
		}
		intent := models.WorkspaceWriteIntent{InstanceID: 7, OwnerID: 3, Generation: generation, Operation: fmt.Sprintf("%032x", generation), Action: "upload"}
		start := make(chan struct{})
		admitted := make(chan error, 1)
		stopped := make(chan error, 1)
		go func() { <-start; admitted <- writes.Begin(ctx, intent) }()
		go func() { <-start; stopped <- fence.Fence(ctx, 7, 3, generation, pod) }()
		close(start)
		writeErr, stopErr := <-admitted, <-stopped
		if writeErr == nil {
			if !errors.Is(stopErr, models.ErrRuntimeUpgradeWritersActive) {
				t.Fatalf("admitted writer and completed fence overlapped: %v", stopErr)
			}
			if err := writes.Complete(ctx, intent); err != nil {
				t.Fatal(err)
			}
		} else if stopErr != nil {
			t.Fatalf("neither admission nor durable fence succeeded: %v %v", writeErr, stopErr)
		}
		if err := fence.Fence(ctx, 7, 3, generation, pod); err != nil {
			t.Fatal(err)
		}
		late := intent
		late.Operation = fmt.Sprintf("%032x", generation+100)
		if err := writes.Begin(ctx, late); err == nil {
			t.Fatal("concurrent stop left admission open")
		}
	}
}
