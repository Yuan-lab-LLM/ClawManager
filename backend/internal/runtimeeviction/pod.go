package runtimeeviction

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PodScope authorizes only one terminal original Pod. The controller persists a
// positive terminal observation while a finalizer retains that exact Pod object;
// API absence and expired heartbeats never supply this barrier.
type PodScope struct {
	Version                 int    `json:"version"`
	OperationID             string `json:"operation_id"`
	Namespace               string `json:"namespace"`
	PodName                 string `json:"pod_name"`
	PodUID                  string `json:"pod_uid"`
	NodeName                string `json:"node_name"`
	NodeUID                 string `json:"node_uid"`
	NodeBootID              string `json:"node_boot_id"`
	RuntimeType             string `json:"runtime_type"`
	WorkspaceRoot           string `json:"workspace_root"`
	ControllerUID           string `json:"controller_uid"`
	StorageDigest           string `json:"storage_digest"`
	TerminalResourceVersion string `json:"terminal_resource_version"`
}
type PodReceipt struct {
	ScopeDigest           string    `json:"scope_digest"`
	OperationID           string    `json:"operation_id"`
	PodUID                string    `json:"pod_uid"`
	NodeUID               string    `json:"node_uid"`
	NodeBootID            string    `json:"node_boot_id"`
	InventoryDigest       string    `json:"inventory_digest"`
	LedgerDigest          string    `json:"ledger_digest"`
	StoppedContainerCount int       `json:"stopped_container_count"`
	RepairedLedgerCount   int       `json:"repaired_ledger_count"`
	FinishedAt            time.Time `json:"finished_at"`
	Confirmed             bool      `json:"confirmed"`
}

func podHexDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func (s PodScope) Validate() error {
	if s.Version != 1 || !filepath.IsAbs(s.WorkspaceRoot) || filepath.Clean(s.WorkspaceRoot) != s.WorkspaceRoot || s.WorkspaceRoot == "/" || len(s.WorkspaceRoot) > 4096 || strings.ContainsAny(s.WorkspaceRoot, "\x00\r\n") || !podHexDigest(s.StorageDigest) || strings.ContainsAny(s.RuntimeType, "/\\.") {
		return fmt.Errorf("invalid orphan recovery scope")
	}
	for _, value := range []string{s.OperationID, s.Namespace, s.PodName, s.PodUID, s.NodeName, s.NodeUID, s.NodeBootID, s.RuntimeType, s.ControllerUID, s.TerminalResourceVersion} {
		if value == "" || len(value) > 256 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("incomplete orphan recovery identity or terminal barrier")
		}
	}
	return nil
}
func PodScopeDigest(s PodScope) string { b, _ := json.Marshal(s); return digest(b) }
func ValidatePodReceipt(s PodScope, r *PodReceipt) error {
	if e := s.Validate(); e != nil {
		return e
	}
	if r == nil || !r.Confirmed || r.ScopeDigest != PodScopeDigest(s) || r.OperationID != s.OperationID || r.PodUID != s.PodUID || r.NodeUID != s.NodeUID || r.NodeBootID != s.NodeBootID || !podHexDigest(r.InventoryDigest) || !podHexDigest(r.LedgerDigest) || r.StoppedContainerCount < 0 || r.RepairedLedgerCount < 0 || r.FinishedAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) || r.FinishedAt.After(time.Now().Add(5*time.Minute)) {
		return fmt.Errorf("orphan recovery receipt identity mismatch")
	}
	return nil
}

type podLedgerSnapshot struct {
	name   string
	record ledger
}

func readPodLedger(p, name, runtimeType string) (ledger, error) {
	var l ledger
	b, e := readTrusted(p)
	if e != nil {
		return l, e
	}
	if e = json.Unmarshal(b, &l); e != nil {
		return l, e
	}
	// Trusted maintenance uses the same protected ledger while no ordinary slot
	// owns the writer. Pod-wide recovery can also leave its stopped tombstone.
	validSlot := canonicalSlot(l.Slot) || (l.Slot == "maintenance" && (l.State == "upgrading" || l.State == "stopped"))
	if l.InstanceID < 1 || l.UserID < 1 || l.Generation < 1 || l.RuntimeType != runtimeType || l.PodUID == "" || len(l.PodUID) > 256 || strings.TrimSpace(l.PodUID) != l.PodUID || strings.ContainsAny(l.PodUID, "\x00\r\n") || !validSlot || (l.State != "running" && l.State != "stopped" && l.State != "upgrading") || name != fmt.Sprintf("%s-%d-%d.json", runtimeType, l.UserID, l.InstanceID) {
		return l, fmt.Errorf("invalid protected writer ledger identity")
	}
	return l, nil
}
func podLedgerSnapshots(dir, runtimeType string) ([]podLedgerSnapshot, error) {
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	out := []podLedgerSnapshot{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, runtimeType+"-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		// The filename cannot supply arbitrary paths or aliases for another owner.
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, runtimeType+"-"), ".json"), "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid protected ledger filename")
		}
		for _, part := range parts {
			n, e := strconv.Atoi(part)
			if e != nil || n < 1 || strconv.Itoa(n) != part {
				return nil, fmt.Errorf("invalid protected ledger filename")
			}
		}
		l, e := readPodLedger(filepath.Join(dir, name), name, runtimeType)
		if e != nil {
			return nil, e
		}
		out = append(out, podLedgerSnapshot{name, l})
	}
	return out, nil
}
func podContainers(s PodScope, all []Container) ([]Container, error) {
	result := []Container{}
	seen := map[string]bool{}
	for _, c := range all {
		if c.PodUID != s.PodUID {
			continue
		}
		if c.Namespace != s.Namespace || c.PodName != s.PodName || c.ID == "" || c.Name == "" || seen[c.ID] {
			return nil, fmt.Errorf("sandbox identity mismatch")
		}
		seen[c.ID] = true
		if c.State != "running" && c.State != "created" && c.State != "exited" {
			return nil, fmt.Errorf("container state unknown")
		}
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// RunPod is independent of legacy Agent acknowledgements. The caller must fence
// SQL and native publication and positively observe the terminal original Pod
// before supplying this scope. Only the old Pod UID is ever stopped or repaired.
func RunPod(ctx context.Context, s PodScope, cri CRIRuntime, workspaceRoot, bootIDPath string) (*PodReceipt, error) {
	if e := s.Validate(); e != nil {
		return nil, e
	}
	if workspaceRoot != s.WorkspaceRoot {
		return nil, fmt.Errorf("workspace differs from scope")
	}
	boot, e := os.ReadFile(bootIDPath)
	if e != nil || strings.TrimSpace(string(boot)) != s.NodeBootID {
		return nil, fmt.Errorf("node boot identity changed")
	}
	if e = trustedDirectory(workspaceRoot); e != nil {
		return nil, e
	}
	dir := filepath.Join(workspaceRoot, ".clawmanager-slot-locks")
	if _, e = os.Lstat(dir); os.IsNotExist(e) {
		if e = os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
			return nil, e
		} else if e == nil {
			parent, e := os.Open(workspaceRoot)
			if e != nil {
				return nil, e
			}
			e = parent.Sync()
			parent.Close()
			if e != nil {
				return nil, e
			}
		}
	} else if e != nil {
		return nil, e
	}
	if e = trustedDirectory(dir); e != nil {
		return nil, e
	}
	receiptPath := filepath.Join(dir, ".pod-recovery-"+PodScopeDigest(s)+".json")
	operationLock, e := lock(ctx, receiptPath+".lock", true)
	if e != nil {
		return nil, e
	}
	defer operationLock.Close()
	if b, e := readTrusted(receiptPath); e == nil {
		var r PodReceipt
		if e = json.Unmarshal(b, &r); e != nil {
			return nil, e
		}
		return &r, ValidatePodReceipt(s, &r)
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	all, e := cri.ListContainers(ctx)
	if e != nil {
		return nil, e
	}
	target, e := podContainers(s, all)
	if e != nil {
		return nil, e
	}
	for _, c := range target {
		if c.State != "exited" {
			if e = cri.StopContainer(ctx, c.ID); e != nil {
				return nil, e
			}
		}
	}
	verify := func() ([]Container, error) {
		all, e := cri.ListContainers(ctx)
		if e != nil {
			return nil, e
		}
		current, e := podContainers(s, all)
		if e != nil {
			return nil, e
		}
		for _, c := range current {
			if c.State != "exited" {
				return nil, fmt.Errorf("old source still executing")
			}
		}
		return current, nil
	}
	final, e := verify()
	if e != nil {
		return nil, e
	}
	snapshots, e := podLedgerSnapshots(dir, s.RuntimeType)
	if e != nil {
		return nil, e
	}
	type ledgerProof struct{ Name, Digest string }
	repaired := []ledgerProof{}
	for _, snapshot := range snapshots {
		if snapshot.record.PodUID != s.PodUID {
			continue
		}
		lp := filepath.Join(dir, snapshot.name)
		lease, e := lock(ctx, strings.TrimSuffix(lp, ".json")+".lock", false)
		if e != nil {
			return nil, e
		}
		current, e := readPodLedger(lp, snapshot.name, s.RuntimeType)
		if e != nil {
			lease.Close()
			return nil, e
		}
		// A different/newer owner wins. Never rewrite it using this historical scope.
		if current != snapshot.record {
			lease.Close()
			continue
		}
		current.State = "stopped"
		e = atomic(lp, current)
		if e == nil {
			var b []byte
			b, e = readTrusted(lp)
			if e == nil {
				repaired = append(repaired, ledgerProof{snapshot.name, digest(b)})
			}
		}
		lease.Close()
		if e != nil {
			return nil, e
		}
	}
	if final, e = verify(); e != nil {
		return nil, e
	}
	inventory := map[string]string{}
	for _, c := range target {
		inventory[c.ID] = c.Name
	}
	for _, c := range final {
		inventory[c.ID] = c.Name
	}
	inventoryBytes, _ := json.Marshal(inventory)
	ledgerBytes, _ := json.Marshal(repaired)
	r := &PodReceipt{ScopeDigest: PodScopeDigest(s), OperationID: s.OperationID, PodUID: s.PodUID, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, InventoryDigest: digest(inventoryBytes), LedgerDigest: digest(ledgerBytes), StoppedContainerCount: len(inventory), RepairedLedgerCount: len(repaired), FinishedAt: time.Now().UTC(), Confirmed: true}
	if e = atomic(receiptPath, r); e != nil {
		return nil, e
	}
	return r, nil
}

// PodLedgerOwner is a read-only snapshot of a protected writer identity. It is
// neither execution-stop evidence nor historical volume provenance.
type PodLedgerOwner struct {
	InstanceID  int    `json:"instance_id"`
	UserID      int    `json:"user_id"`
	Generation  int    `json:"generation"`
	RuntimeType string `json:"runtime_type"`
	PodUID      string `json:"pod_uid"`
	Slot        string `json:"slot"`
	State       string `json:"state"`
}

func InspectPodLedgerOwners(root, runtimeType, podUID string) ([]PodLedgerOwner, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" || len(root) > 4096 || strings.ContainsAny(root, "\x00\r\n") || runtimeType == "" || len(runtimeType) > 256 || strings.TrimSpace(runtimeType) != runtimeType || strings.ContainsAny(runtimeType, "/\\.\x00\r\n") || podUID == "" || len(podUID) > 256 || strings.TrimSpace(podUID) != podUID || strings.ContainsAny(podUID, "\x00\r\n") {
		return nil, fmt.Errorf("invalid source ledger identity")
	}
	if e := trustedDirectory(root); e != nil {
		return nil, e
	}
	dir := filepath.Join(root, ".clawmanager-slot-locks")
	if e := trustedDirectory(dir); os.IsNotExist(e) {
		return nil, nil
	} else if e != nil {
		return nil, e
	}
	snapshots, e := podLedgerSnapshots(dir, runtimeType)
	if e != nil {
		return nil, e
	}
	result := []PodLedgerOwner{}
	for _, s := range snapshots {
		l := s.record
		if l.PodUID != podUID {
			continue
		}
		result = append(result, PodLedgerOwner{InstanceID: l.InstanceID, UserID: l.UserID, Generation: l.Generation, RuntimeType: l.RuntimeType, PodUID: l.PodUID, Slot: l.Slot, State: l.State})
	}
	return result, nil
}
