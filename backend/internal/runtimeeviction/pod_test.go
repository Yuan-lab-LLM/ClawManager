package runtimeeviction

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func podFixture(t *testing.T) (PodScope, *fakeRuntime, string, string) {
	t.Helper()
	s, f, root, boot := fixture(t)
	for i := range f.containers {
		f.containers[i].Namespace = s.Namespace
		f.containers[i].PodName = s.PodName
	}
	f.containers = append(f.containers, Container{ID: "healthy", PodUID: "another-uid", Name: "slot-0", Namespace: s.Namespace, PodName: "neighbor", State: "running"})
	return PodScope{Version: 1, OperationID: "orphan", Namespace: s.Namespace, PodName: s.PodName, PodUID: s.PodUID, NodeName: s.NodeName, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, RuntimeType: s.RuntimeType, WorkspaceRoot: root, ControllerUID: "controller", StorageDigest: strings.Repeat("a", 64), TerminalResourceVersion: "42"}, f, root, boot
}
func TestPodExactUIDStopLedgerCASAndDurableReplay(t *testing.T) {
	s, f, root, boot := podFixture(t)
	dir := filepath.Join(root, ".clawmanager-slot-locks")
	neighbor := []byte(`{"instance_id":2,"user_id":2,"generation":7,"runtime_type":"opencode","pod_uid":"another-uid","slot":"slot-0","state":"running"}`)
	os.WriteFile(filepath.Join(dir, "opencode-2-2.json"), neighbor, 0600)
	r, e := RunPod(context.Background(), s, f, root, boot)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidatePodReceipt(s, r); e != nil {
		t.Fatal(e)
	}
	if len(f.stopped) != 2 || f.containers[2].State != "running" || r.StoppedContainerCount != 2 || r.RepairedLedgerCount != 1 {
		t.Fatalf("unexpected effects %+v %+v", f, r)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "opencode-2-2.json"))
	if string(b) != string(neighbor) {
		t.Fatal("rewrote neighbor ledger")
	}
	var l ledger
	b, _ = os.ReadFile(filepath.Join(dir, "opencode-2-1.json"))
	json.Unmarshal(b, &l)
	if l.State != "stopped" {
		t.Fatalf("ledger not repaired: %s", b)
	}
	os.WriteFile(filepath.Join(dir, "opencode-2-1.json"), []byte(`{"generation":4}`), 0600)
	again, e := RunPod(context.Background(), s, f, root, boot)
	if e != nil || again.ScopeDigest != r.ScopeDigest {
		t.Fatalf("lost durable receipt: %v", e)
	}
}
func TestPodRequiresExactBarrierBootAndSandbox(t *testing.T) {
	for _, kind := range []string{"barrier", "boot", "namespace", "unknown", "digest"} {
		t.Run(kind, func(t *testing.T) {
			s, f, root, boot := podFixture(t)
			switch kind {
			case "barrier":
				s.TerminalResourceVersion = ""
			case "boot":
				s.NodeBootID = "other"
			case "namespace":
				f.containers[1].Namespace = "other"
			case "unknown":
				f.containers[1].State = "unknown"
			case "digest":
				s.StorageDigest = strings.Repeat("z", 64)
			}
			if _, e := RunPod(context.Background(), s, f, root, boot); e == nil {
				t.Fatal("accepted uncertain scope")
			}
			if len(f.stopped) != 0 {
				t.Fatal("stopped before complete identity validation")
			}
		})
	}
}
func TestPodLedgerlessLegacySourcePreservesWorkspace(t *testing.T) {
	s, f, root, boot := podFixture(t)
	os.RemoveAll(filepath.Join(root, ".clawmanager-slot-locks"))
	project := filepath.Join(root, "project")
	os.WriteFile(project, []byte("keep"), 0600)
	r, e := RunPod(context.Background(), s, f, root, boot)
	if e != nil || r.RepairedLedgerCount != 0 {
		t.Fatalf("legacy proof failed %v", e)
	}
	b, _ := os.ReadFile(project)
	if string(b) != "keep" {
		t.Fatal("project removed")
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".clawmanager-slot-locks"))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".pod-recovery-") {
			t.Fatalf("created ledger tombstone %s", entry.Name())
		}
	}
}

func TestPodMaintenanceLedgerIsRecognizedWithoutTouchingNeighbor(t *testing.T) {
	s, f, root, boot := podFixture(t)
	dir := filepath.Join(root, ".clawmanager-slot-locks")
	neighbor := []byte(`{"instance_id":2,"user_id":2,"generation":7,"runtime_type":"opencode","pod_uid":"another-uid","slot":"maintenance","state":"upgrading"}`)
	if err := os.WriteFile(filepath.Join(dir, "opencode-2-2.json"), neighbor, 0600); err != nil {
		t.Fatal(err)
	}
	owners, err := InspectPodLedgerOwners(root, s.RuntimeType, s.PodUID)
	if err != nil || len(owners) != 1 {
		t.Fatalf("valid maintenance scope blocked unrelated source: %+v %v", owners, err)
	}
	if _, err = RunPod(t.Context(), s, f, root, boot); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "opencode-2-2.json"))
	if err != nil || string(got) != string(neighbor) {
		t.Fatalf("neighbor maintenance changed: %s %v", got, err)
	}
}

func TestPodMaintenanceRecoveryRetainsStoppedTombstone(t *testing.T) {
	s, f, root, boot := podFixture(t)
	p := filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var l ledger
	if err = json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	l.Slot, l.State = "maintenance", "upgrading"
	raw, _ = json.Marshal(l)
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = RunPod(t.Context(), s, f, root, boot); err != nil {
		t.Fatal(err)
	}
	owners, err := InspectPodLedgerOwners(root, s.RuntimeType, s.PodUID)
	if err != nil || len(owners) != 1 || owners[0].Slot != "maintenance" || owners[0].State != "stopped" {
		t.Fatalf("stopped maintenance tombstone unreadable: %+v %v", owners, err)
	}
}

func TestPodMaintenanceDoesNotAuthorizeRunningOrUnknownSlot(t *testing.T) {
	for _, slot := range []string{"maintenance", "arbitrary"} {
		t.Run(slot, func(t *testing.T) {
			s, _, root, _ := podFixture(t)
			p := filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json")
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			var l ledger
			if err = json.Unmarshal(raw, &l); err != nil {
				t.Fatal(err)
			}
			l.Slot, l.State = slot, "running"
			raw, _ = json.Marshal(l)
			if err = os.WriteFile(p, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = InspectPodLedgerOwners(root, s.RuntimeType, s.PodUID); err == nil {
				t.Fatal("invalid maintenance/slot state accepted")
			}
		})
	}
}
func TestPodRejectsSymlinkLedger(t *testing.T) {
	s, f, root, boot := podFixture(t)
	p := filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json")
	outside := filepath.Join(t.TempDir(), "ledger")
	os.Rename(p, outside)
	os.Symlink(outside, p)
	if _, e := RunPod(context.Background(), s, f, root, boot); e == nil {
		t.Fatal("followed ledger symlink")
	}
}

type podRacingRuntime struct {
	*fakeRuntime
	onList func(int)
}

func (f *podRacingRuntime) ListContainers(ctx context.Context) ([]Container, error) {
	if f.onList != nil {
		f.onList(f.listCalls + 1)
	}
	return f.fakeRuntime.ListContainers(ctx)
}
func TestPodNewLedgerOwnerIsNeverRewritten(t *testing.T) {
	s, f, root, boot := podFixture(t)
	p := filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json")
	newOwner := []byte(`{"instance_id":1,"user_id":2,"generation":4,"runtime_type":"opencode","pod_uid":"healthy-new-owner","slot":"slot-0","state":"running"}`)
	racing := &podRacingRuntime{fakeRuntime: f, onList: func(call int) {
		if call == 2 {
			os.WriteFile(p, newOwner, 0600)
		}
	}}
	r, e := RunPod(context.Background(), s, racing, root, boot)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if string(b) != string(newOwner) || r.RepairedLedgerCount != 0 {
		t.Fatalf("new owner changed %s", b)
	}
}
func TestPodUnknownLedgerFormatBlocksProof(t *testing.T) {
	s, f, root, boot := podFixture(t)
	p := filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-9.json")
	os.WriteFile(p, []byte(`{"generation":4}`), 0600)
	if _, e := RunPod(context.Background(), s, f, root, boot); e == nil {
		t.Fatal("accepted malformed protected ledger")
	}
}
func TestPodRefusesReceiptForStillRunningSource(t *testing.T) {
	s, f, root, boot := podFixture(t)
	racing := &podRacingRuntime{fakeRuntime: f, onList: func(call int) {
		if call == 3 {
			f.containers[0].State = "running"
		}
	}}
	if _, e := RunPod(context.Background(), s, racing, root, boot); e == nil {
		t.Fatal("confirmed restarted old source")
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".clawmanager-slot-locks"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pod-recovery-") && strings.HasSuffix(entry.Name(), ".json") {
			t.Fatal("persisted false stopped receipt")
		}
	}
}

func TestInspectPodLedgerOwnersReadOnlyExactUID(t *testing.T) {
	s, _, root, _ := podFixture(t)
	dir := filepath.Join(root, ".clawmanager-slot-locks")
	other := []byte(`{"instance_id":2,"user_id":2,"generation":7,"runtime_type":"opencode","pod_uid":"other","slot":"slot-0","state":"running"}`)
	os.WriteFile(filepath.Join(dir, "opencode-2-2.json"), other, 0600)
	before, _ := os.ReadDir(dir)
	owners, e := InspectPodLedgerOwners(root, s.RuntimeType, s.PodUID)
	if e != nil || len(owners) != 1 || owners[0].InstanceID != 1 || owners[0].Generation != 3 || owners[0].State != "running" {
		t.Fatalf("wrong owners %+v %v", owners, e)
	}
	after, _ := os.ReadDir(dir)
	if len(before) != len(after) {
		t.Fatal("read-only inspection created files")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "opencode-2-2.json"))
	if string(b) != string(other) {
		t.Fatal("inspection changed other owner")
	}
	os.RemoveAll(dir)
	owners, e = InspectPodLedgerOwners(root, s.RuntimeType, s.PodUID)
	if e != nil || len(owners) != 0 {
		t.Fatalf("legacy empty inspection failed: %v", e)
	}
	if _, e = InspectPodLedgerOwners(root, "../bad", s.PodUID); e == nil {
		t.Fatal("accepted traversal type")
	}
}
