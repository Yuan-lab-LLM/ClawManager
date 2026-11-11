package runtimeeviction

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRuntime struct {
	containers []Container
	stopped    []string
	listCalls  int
	failList   int
}

func (f *fakeRuntime) ListContainers(context.Context) ([]Container, error) {
	f.listCalls++
	if f.listCalls == f.failList {
		return nil, errors.New("lost runtime response")
	}
	return f.containers, nil
}
func (f *fakeRuntime) StopContainer(_ context.Context, id string) error {
	f.stopped = append(f.stopped, id)
	for i := range f.containers {
		if f.containers[i].ID == id {
			f.containers[i].State = "exited"
		}
	}
	return nil
}
func fixture(t *testing.T) (Scope, *fakeRuntime, string, string) {
	t.Helper()
	root := t.TempDir()
	boot := filepath.Join(t.TempDir(), "boot")
	os.WriteFile(boot, []byte("boot"), 0600)
	s := Scope{Version: 1, OperationID: "op", Namespace: "ns", PodName: "pod", PodUID: "uid", NodeName: "node", NodeUID: "nodeuid", NodeBootID: "boot", ContainerName: "slot-1", ContainerID: "containerd://old", RuntimeType: "opencode", GatewayID: "gw-1-3", WorkspaceRoot: root, InstanceID: 1, UserID: 2, Generation: 3}
	f := &fakeRuntime{containers: []Container{{ID: "old", PodUID: "uid", Name: "slot-1", State: "running"}, {ID: "neighbor", PodUID: "uid", Name: "slot-2", State: "running"}}}
	os.Mkdir(filepath.Join(root, ".clawmanager-slot-locks"), 0700)
	b, _ := json.Marshal(map[string]any{"instance_id": 1, "user_id": 2, "generation": 3, "runtime_type": "opencode", "pod_uid": "uid", "slot": "slot-1", "state": "running"})
	os.WriteFile(filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json"), b, 0600)
	os.WriteFile(filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.lock"), nil, 0600)
	return s, f, root, boot
}
func TestExactStopAndDurableReplay(t *testing.T) {
	s, f, root, boot := fixture(t)
	r, err := Run(context.Background(), s, f, root, boot)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateReceipt(s, r); err != nil {
		t.Fatal(err)
	}
	if len(f.stopped) != 1 || f.stopped[0] != "old" {
		t.Fatalf("wrong stop %v", f.stopped)
	}
	os.WriteFile(filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json"), []byte(`{"generation":4}`), 0600)
	again, err := Run(context.Background(), s, f, root, boot)
	if err != nil || again.ScopeDigest != r.ScopeDigest {
		t.Fatalf("lost durable receipt: %v", err)
	}
}
func TestRejectIdentityMismatchBeforeStop(t *testing.T) {
	for _, field := range []string{"boot", "uid", "container", "owner"} {
		t.Run(field, func(t *testing.T) {
			s, f, root, boot := fixture(t)
			switch field {
			case "boot":
				s.NodeBootID = "other"
			case "uid":
				f.containers[0].PodUID = "other"
			case "container":
				s.ContainerID = "containerd://neighbor"
			case "owner":
				s.Generation = 4
			}
			if _, err := Run(context.Background(), s, f, root, boot); err == nil {
				t.Fatal("accepted mismatch")
			}
			if len(f.stopped) != 0 {
				t.Fatal("stopped on mismatch")
			}
		})
	}
}

func TestAlreadyStoppedContainerGarbageCollected(t *testing.T) {
	s, f, root, boot := fixture(t)
	f.containers = f.containers[1:]
	r, e := Run(context.Background(), s, f, root, boot)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateReceipt(s, r); e != nil {
		t.Fatal(e)
	}
	if len(f.stopped) != 0 {
		t.Fatal("stopped neighbor")
	}
}
func TestRejectSymlinkLedger(t *testing.T) {
	s, f, root, boot := fixture(t)
	p := filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json")
	b, _ := os.ReadFile(p)
	target := filepath.Join(t.TempDir(), "ledger")
	os.WriteFile(target, b, 0600)
	os.Remove(p)
	os.Symlink(target, p)
	if _, e := Run(context.Background(), s, f, root, boot); e == nil {
		t.Fatal("followed symlink")
	}
	if len(f.stopped) != 0 {
		t.Fatal("stopped before validating ledger")
	}
}

func TestRejectUnknownExecutionAndWrongSandbox(t *testing.T) {
	for _, bad := range []string{"unknown", "namespace"} {
		t.Run(bad, func(t *testing.T) {
			s, f, root, boot := fixture(t)
			if bad == "unknown" {
				f.containers[0].State = "unknown"
			} else {
				f.containers[0].Namespace = "other"
				f.containers[0].PodName = s.PodName
			}
			if _, e := Run(context.Background(), s, f, root, boot); e == nil {
				t.Fatal("accepted uncertain execution")
			}
			if len(f.stopped) != 0 {
				t.Fatal("stopped uncertain execution")
			}
		})
	}
}

func TestCanonicalScopeAndReceipt(t *testing.T) {
	s, f, root, boot := fixture(t)
	for _, name := range []string{"runtime", "slot-01", "slot--1", "slot-256"} {
		bad := s
		bad.ContainerName = name
		if bad.Validate() == nil {
			t.Fatalf("accepted slot %q", name)
		}
	}
	bad := s
	bad.GatewayID = "gw-1-4"
	if bad.Validate() == nil {
		t.Fatal("accepted wrong gateway generation")
	}
	r, e := Run(context.Background(), s, f, root, boot)
	if e != nil {
		t.Fatal(e)
	}
	r.LedgerDigest = strings.Repeat("z", 64)
	if ValidateReceipt(s, r) == nil {
		t.Fatal("accepted nonhex digest")
	}
	r.LedgerDigest = strings.Repeat("a", 64)
	r.FinishedAt = time.Now().Add(time.Hour)
	if ValidateReceipt(s, r) == nil {
		t.Fatal("accepted future proof")
	}
}

func TestPinnedStopLeavesKubeletReincarnationUntouched(t *testing.T) {
	s, f, root, boot := fixture(t)
	f.containers = append(f.containers, Container{ID: "new", PodUID: s.PodUID, Name: s.ContainerName, State: "running"})
	r, e := Run(context.Background(), s, f, root, boot)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateReceipt(s, r); e != nil {
		t.Fatal(e)
	}
	if len(f.stopped) != 1 || f.stopped[0] != "old" || f.containers[2].State != "running" {
		t.Fatalf("reincarnation touched: %+v", f)
	}
}

func TestNeverClaimedLedgerCreatesStoppedTombstoneAfterExactStop(t *testing.T) {
	s, f, root, boot := fixture(t)
	os.RemoveAll(filepath.Join(root, ".clawmanager-slot-locks"))
	r, e := Run(context.Background(), s, f, root, boot)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateReceipt(s, r); e != nil {
		t.Fatal(e)
	}
	if len(f.stopped) != 1 || f.stopped[0] != "old" {
		t.Fatalf("wrong stop: %v", f.stopped)
	}
	var l ledger
	b, e := os.ReadFile(filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json"))
	if e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(b, &l) != nil || l.State != "stopped" || l.Generation != 3 || l.PodUID != s.PodUID {
		t.Fatalf("invalid tombstone %s", b)
	}
}
func TestMissingLedgerRequiresPositivePinnedIdentity(t *testing.T) {
	s, f, root, boot := fixture(t)
	os.RemoveAll(filepath.Join(root, ".clawmanager-slot-locks"))
	f.containers = f.containers[1:]
	if _, e := Run(context.Background(), s, f, root, boot); e == nil {
		t.Fatal("created tombstone without positively observed source")
	}
	if len(f.stopped) != 0 {
		t.Fatal("stopped healthy neighbor")
	}
}

func TestNeverClaimedCrashThenGCResumesFromPositiveIntent(t *testing.T) {
	s, f, root, boot := fixture(t)
	os.RemoveAll(filepath.Join(root, ".clawmanager-slot-locks"))
	f.failList = 2
	if _, e := Run(context.Background(), s, f, root, boot); e == nil {
		t.Fatal("expected lost post-stop reply")
	}
	if len(f.stopped) != 1 {
		t.Fatal("old container not stopped")
	}
	f.containers = f.containers[1:]
	f.failList = 0
	r, e := Run(context.Background(), s, f, root, boot)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateReceipt(s, r); e != nil {
		t.Fatal(e)
	}
}
