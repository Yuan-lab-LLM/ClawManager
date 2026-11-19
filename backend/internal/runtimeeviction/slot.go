package runtimeeviction

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// SlotScope is deliberately independent of business ownership. It authorizes
// stopping one physical execution, never editing an instance or NFS writer.
type SlotScope struct {
	Version       int    `json:"version"`
	OperationID   string `json:"operation_id"`
	Namespace     string `json:"namespace"`
	PodName       string `json:"pod_name"`
	PodUID        string `json:"pod_uid"`
	NodeName      string `json:"node_name"`
	NodeUID       string `json:"node_uid"`
	NodeBootID    string `json:"node_boot_id"`
	ContainerName string `json:"container_name"`
	ContainerID   string `json:"container_id"`
}

type SlotReceipt struct {
	ScopeDigest   string    `json:"scope_digest"`
	OperationID   string    `json:"operation_id"`
	PodUID        string    `json:"pod_uid"`
	ContainerName string    `json:"container_name"`
	ContainerID   string    `json:"container_id"`
	NodeUID       string    `json:"node_uid"`
	NodeBootID    string    `json:"node_boot_id"`
	FinishedAt    time.Time `json:"finished_at"`
	Confirmed     bool      `json:"confirmed"`
}

func (s SlotScope) Validate() error {
	if s.Version != 1 || !canonicalSlot(s.ContainerName) {
		return fmt.Errorf("invalid physical slot scope")
	}
	for _, value := range []string{s.OperationID, s.Namespace, s.PodName, s.PodUID, s.NodeName, s.NodeUID, s.NodeBootID} {
		if value == "" || len(value) > 256 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("incomplete physical slot identity")
		}
	}
	id := strings.TrimPrefix(s.ContainerID, "containerd://")
	b, err := hex.DecodeString(id)
	if !strings.HasPrefix(s.ContainerID, "containerd://") || err != nil || len(b) != 32 || id != hex.EncodeToString(b) {
		return fmt.Errorf("exact containerd ID required")
	}
	return nil
}

func SlotScopeDigest(s SlotScope) string { b, _ := json.Marshal(s); return digest(b) }

func ValidateSlotReceipt(s SlotScope, r *SlotReceipt) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if r == nil || !r.Confirmed || r.ScopeDigest != SlotScopeDigest(s) || r.OperationID != s.OperationID || r.PodUID != s.PodUID || r.ContainerName != s.ContainerName || r.ContainerID != s.ContainerID || r.NodeUID != s.NodeUID || r.NodeBootID != s.NodeBootID || r.FinishedAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) || r.FinishedAt.After(time.Now().Add(5*time.Minute)) {
		return fmt.Errorf("physical slot receipt identity mismatch")
	}
	return nil
}

func RunSlot(ctx context.Context, s SlotScope, cri CRIRuntime, bootIDPath string) (*SlotReceipt, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	boot, err := os.ReadFile(bootIDPath)
	if err != nil || strings.TrimSpace(string(boot)) != s.NodeBootID {
		return nil, fmt.Errorf("node boot identity changed")
	}
	all, err := cri.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	target := strings.TrimPrefix(s.ContainerID, "containerd://")
	var old *Container
	for i := range all {
		if all[i].ID != target {
			continue
		}
		if old != nil {
			return nil, fmt.Errorf("ambiguous physical container inventory")
		}
		old = &all[i]
	}
	if old == nil || old.PodUID != s.PodUID || old.Namespace != s.Namespace || old.PodName != s.PodName || old.Name != s.ContainerName {
		return nil, fmt.Errorf("pinned container is not positively identified")
	}
	if old.State != "running" && old.State != "created" && old.State != "exited" {
		return nil, fmt.Errorf("physical container state is unknown")
	}
	if old.State != "exited" {
		if err = cri.StopContainer(ctx, target); err != nil {
			return nil, err
		}
	}
	all, err = cri.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		if c.ID == target && (c.PodUID != s.PodUID || c.Namespace != s.Namespace || c.PodName != s.PodName || c.Name != s.ContainerName || c.State != "exited") {
			return nil, fmt.Errorf("pinned old container termination is not confirmed")
		}
	}
	boot, err = os.ReadFile(bootIDPath)
	if err != nil || strings.TrimSpace(string(boot)) != s.NodeBootID {
		return nil, fmt.Errorf("node rebooted during physical reset")
	}
	r := &SlotReceipt{ScopeDigest: SlotScopeDigest(s), OperationID: s.OperationID, PodUID: s.PodUID, ContainerName: s.ContainerName, ContainerID: s.ContainerID, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, FinishedAt: time.Now().UTC(), Confirmed: true}
	return r, ValidateSlotReceipt(s, r)
}
