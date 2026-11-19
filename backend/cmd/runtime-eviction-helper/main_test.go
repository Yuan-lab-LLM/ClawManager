package main

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type emptyRuntime struct{ listed int }

func (f *emptyRuntime) ListContainers(context.Context) ([]runtimeeviction.Container, error) {
	f.listed++
	return nil, nil
}
func (f *emptyRuntime) StopContainer(context.Context, string) error { panic("unexpected stop") }

type physicalRuntime struct{ stopped bool }

func (f *physicalRuntime) ListContainers(context.Context) ([]runtimeeviction.Container, error) {
	state := "running"
	if f.stopped {
		state = "exited"
	}
	return []runtimeeviction.Container{{ID: strings.Repeat("a", 64), Namespace: "ns", PodName: "pod", PodUID: "uid", Name: "slot-0", State: state}}, nil
}
func (f *physicalRuntime) StopContainer(context.Context, string) error { f.stopped = true; return nil }
func TestPhysicalSlotModeNeedsNoWorkspace(t *testing.T) {
	boot := filepath.Join(t.TempDir(), "boot")
	os.WriteFile(boot, []byte("boot"), 0600)
	s := runtimeeviction.SlotScope{Version: 1, OperationID: "op", Namespace: "ns", PodName: "pod", PodUID: "uid", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64)}
	b, _ := json.Marshal(s)
	f := &physicalRuntime{}
	reply, err := runSlotWithRuntime(context.Background(), f, boot, string(b))
	if err != nil {
		t.Fatal(err)
	}
	var receipt runtimeeviction.SlotReceipt
	if json.Unmarshal(reply, &receipt) != nil || runtimeeviction.ValidateSlotReceipt(s, &receipt) != nil || !f.stopped {
		t.Fatalf("invalid slot reply: %s", reply)
	}
}
func TestPodModeDispatchAndBoundedReply(t *testing.T) {
	root := t.TempDir()
	boot := filepath.Join(t.TempDir(), "boot")
	os.WriteFile(boot, []byte("boot"), 0600)
	s := runtimeeviction.PodScope{Version: 1, OperationID: "op", Namespace: "ns", PodName: "old", PodUID: "uid", NodeName: "node", NodeUID: "nodeuid", NodeBootID: "boot", RuntimeType: "opencode", WorkspaceRoot: root, ControllerUID: "controller", StorageDigest: strings.Repeat("a", 64), TerminalResourceVersion: "rv"}
	b, _ := json.Marshal(s)
	f := &emptyRuntime{}
	reply, e := runWithRuntime(context.Background(), f, boot, "", string(b))
	if e != nil {
		t.Fatal(e)
	}
	if len(reply) > 4096 {
		t.Fatal("oversized reply")
	}
	var r runtimeeviction.PodReceipt
	if json.Unmarshal(reply, &r) != nil || runtimeeviction.ValidatePodReceipt(s, &r) != nil {
		t.Fatalf("invalid Pod proof %s", reply)
	}
	before := f.listed
	if _, e = runWithRuntime(context.Background(), f, boot, `{}`, string(b)); e == nil {
		t.Fatal("ambiguous helper mode accepted")
	}
	if f.listed != before {
		t.Fatal("executed ambiguous scope")
	}
}
