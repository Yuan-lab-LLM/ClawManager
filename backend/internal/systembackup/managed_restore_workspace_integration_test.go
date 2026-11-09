package systembackup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// A real filesystem/PV/PVC component acceptance. This does not provision SQL,
// Redis, object identities or a complete drill, and never starts a business app.
func TestRealManagedRestoreWorkspacePreservesPartialDataAndCleansOwnedVolume(t *testing.T) {
	path := os.Getenv("TEST_SYSTEM_BACKUP_CAPTURE_FIXTURE_FILE")
	if path == "" {
		t.Skip("requires owned Kubernetes restore workspace fixture")
	}
	const owner = "cm-backup-first-20261024-0543"
	if filepath.Dir(path) != "/home/hxc/"+owner {
		t.Fatal("fixture is outside owned root")
	}
	marker, err := os.ReadFile(filepath.Join(filepath.Dir(path), ".owner"))
	if err != nil || strings.TrimSpace(string(marker)) != owner {
		t.Fatal("fixture owner mismatch")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("fixture unavailable")
	}
	defer clear(raw)
	var cfg realCaptureFixture
	if strictDecode(raw, &cfg) != nil || cfg.Owner != owner || cfg.Request.Namespace != owner || cfg.KubeServer != "https://10.130.14.23:6443" {
		t.Fatal("fixture identity mismatch")
	}
	sourceUID := os.Getenv("TEST_SYSTEM_BACKUP_SOURCE_NAMESPACE_UID")
	if !refPattern.MatchString(sourceUID) || sourceUID == cfg.Request.NamespaceUID {
		t.Fatal("source and target namespace identities must differ")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: cfg.KubeServer, BearerToken: cfg.ControllerToken, Timeout: 10 * time.Second, TLSClientConfig: rest.TLSClientConfig{CAData: []byte(cfg.KubeCA), ServerName: "10.130.14.23"}})
	if err != nil {
		t.Fatal("fixture client unavailable")
	}
	ns, err := client.CoreV1().Namespaces().Get(ctx, owner, metav1.GetOptions{})
	if err != nil || string(ns.UID) != cfg.Request.NamespaceUID || ns.Labels["backup-test-owner"] != owner {
		t.Fatal("fixture namespace owner mismatch")
	}
	database, err := OpenControlDatabase(ctx, cfg.ControlDSNFile)
	if err != nil {
		t.Fatal("fixture control database unavailable")
	}
	defer database.Close()
	ddl, err := os.ReadFile(filepath.Join(filepath.Dir(path), "092_allow_owned_backup_restore_workspaces.sql"))
	if err != nil {
		t.Fatal("restore workspace migration unavailable")
	}
	for _, statement := range strings.Split(string(ddl), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err = database.ExecContext(ctx, statement); err != nil {
				t.Fatal("restore workspace migration failed")
			}
		}
	}
	s := Store{database, "managed-stage-fixture"}
	if err = s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "managed-restore-workspace", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := RandomID("managed-restore-")
	source, _ := RandomID("sbk_")
	x, _, err := s.Create(ctx, 1, key, CreateTaskRequest{Type: Drill, Purpose: "functional_test", SourceID: source}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.ClaimLedTask(ctx, l, x.ID, x.Version, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"preflighting", "provisioning"} {
		x, err = s.Advance(ctx, x, l.Owner, phase, "none")
		if err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(filepath.Dir(path), "managed-restore-root")
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	d := LocalStageVolumes{Client: client, MountDirectory: root}
	r := StageVolumeRequest{Phase: "restore", Namespace: owner, NamespaceUID: cfg.Request.NamespaceUID, SourceNamespaceUID: sourceUID, NodeName: "k8s-master", HostRoot: root, StorageClass: "manual", CapacityBytes: 16 << 20}
	var v StageVolume
	defer func() {
		if v.ID == "" {
			return
		}
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		for {
			done, e := d.Cleanup(cleanup, s, l, x, v)
			if e != nil {
				t.Error("owned restore workspace cleanup deferred", PublicError(e))
				return
			}
			if done {
				return
			}
			select {
			case <-cleanup.Done():
				t.Error("owned restore workspace cleanup deadline")
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()
	for {
		var ready bool
		v, ready, err = d.PrepareRestoreWorkspace(ctx, s, l, x, r)
		if err != nil {
			t.Fatal("automatic restore workspace provisioning", PublicError(err))
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("restore workspace binding deadline")
		case <-time.After(250 * time.Millisecond):
		}
	}
	if v.PVCUID == "" || v.PVUID == "" {
		t.Fatal("missing real volume identities")
	}
	partial := filepath.Join(root, v.ID, "data", "partial-result.bin")
	if err = os.WriteFile(partial, []byte("preserve interrupted result"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.PrepareRestoreWorkspace(ctx, s, l, x, r); err == nil {
		t.Fatal("partial restore target was reused")
	}
	if b, e := os.ReadFile(partial); e != nil || string(b) != "preserve interrupted result" {
		t.Fatal("partial restore result lost")
	}
	for {
		done, e := d.Cleanup(ctx, s, l, x, v)
		if e != nil {
			t.Fatal("owned restore target cleanup", PublicError(e))
		}
		if done {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("restore target cleanup deadline")
		case <-time.After(250 * time.Millisecond):
		}
	}
	if _, err = os.Stat(filepath.Join(root, v.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned target directory retained")
	}
	if _, _, err = d.PrepareRestoreWorkspace(ctx, s, l, x, r); err == nil {
		t.Fatal("deleted restore target resurrected")
	}
	if _, err = s.Advance(ctx, x, l.Owner, "failed", "evidence_incomplete"); err != nil {
		t.Fatal(err)
	}
	t.Logf("real owned restore workspace resource=%s pvc=%s pv=%s; partial result retained before explicit cleanup; no application/Runtime started; component-only task is noneligible", v.ID, v.PVCUID, v.PVUID)
}
