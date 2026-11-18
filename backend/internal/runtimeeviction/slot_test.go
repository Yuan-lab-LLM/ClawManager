package runtimeeviction

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func slotFixture(t *testing.T) (SlotScope, *fakeRuntime, string) {
	t.Helper()
	s := SlotScope{Version: 1, OperationID: "reset-operation", Namespace: "runtime", PodName: "pool", PodUID: "old-pod", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64)}
	f := &fakeRuntime{containers: []Container{
		{ID: strings.Repeat("a", 64), Namespace: s.Namespace, PodName: s.PodName, PodUID: s.PodUID, Name: s.ContainerName, State: "running"},
		{ID: "replacement", Namespace: s.Namespace, PodName: s.PodName, PodUID: s.PodUID, Name: s.ContainerName, State: "running"},
		{ID: "neighbor", Namespace: s.Namespace, PodName: s.PodName, PodUID: s.PodUID, Name: "slot-1", State: "running"},
	}}
	boot := filepath.Join(t.TempDir(), "boot")
	if err := os.WriteFile(boot, []byte("boot\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return s, f, boot
}

func TestSlotResetStopsOnlyPinnedContainerWithoutInstanceOrWorkspace(t *testing.T) {
	s, f, boot := slotFixture(t)
	r, err := RunSlot(context.Background(), s, f, boot)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateSlotReceipt(s, r); err != nil {
		t.Fatal(err)
	}
	if len(f.stopped) != 1 || f.stopped[0] != strings.TrimPrefix(s.ContainerID, "containerd://") {
		t.Fatalf("collateral stop: %v", f.stopped)
	}
	if f.containers[1].State != "running" || f.containers[2].State != "running" {
		t.Fatal("replacement or neighbor changed")
	}
}

func TestSlotResetRejectsUnknownOrChangedPhysicalIdentity(t *testing.T) {
	for _, change := range []string{"missing", "pod", "namespace", "slot", "boot", "unknown", "inventory"} {
		t.Run(change, func(t *testing.T) {
			s, f, boot := slotFixture(t)
			switch change {
			case "missing":
				f.containers = f.containers[1:]
			case "pod":
				f.containers[0].PodUID = "new-pod"
			case "namespace":
				f.containers[0].Namespace = "other"
			case "slot":
				f.containers[0].Name = "slot-1"
			case "boot":
				s.NodeBootID = "new-boot"
			case "unknown":
				f.containers[0].State = "unknown"
			case "inventory":
				f.failList = 1
			}
			if _, err := RunSlot(context.Background(), s, f, boot); err == nil {
				t.Fatal("unverified target accepted")
			}
			if len(f.stopped) != 0 {
				t.Fatal("unverified target stopped")
			}
		})
	}
}

func TestSlotResetRequiresIndependentPostStopInventory(t *testing.T) {
	s, f, boot := slotFixture(t)
	f.failList = 2
	if r, err := RunSlot(context.Background(), s, f, boot); err == nil || r != nil {
		t.Fatal("lost confirmation became success")
	}
}

func TestSlotResetReceiptCannotAuthorizeAnotherContainer(t *testing.T) {
	s, f, boot := slotFixture(t)
	r, err := RunSlot(context.Background(), s, f, boot)
	if err != nil {
		t.Fatal(err)
	}
	r.ContainerID = "containerd://" + strings.Repeat("b", 64)
	if ValidateSlotReceipt(s, r) == nil {
		t.Fatal("changed old container accepted")
	}
}
