// Package runtimeeviction provides exact-container, fail-closed recovery.
package runtimeeviction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Scope struct {
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
	RuntimeType   string `json:"runtime_type"`
	GatewayID     string `json:"gateway_id"`
	WorkspaceRoot string `json:"workspace_root"`
	InstanceID    int    `json:"instance_id"`
	UserID        int    `json:"user_id"`
	Generation    int    `json:"generation"`
}
type Receipt struct {
	ScopeDigest         string    `json:"scope_digest"`
	OperationID         string    `json:"operation_id"`
	PodUID              string    `json:"pod_uid"`
	ContainerName       string    `json:"container_name"`
	NodeUID             string    `json:"node_uid"`
	NodeBootID          string    `json:"node_boot_id"`
	StoppedContainerIDs []string  `json:"stopped_container_ids"`
	LedgerDigest        string    `json:"ledger_digest"`
	FinishedAt          time.Time `json:"finished_at"`
	Confirmed           bool      `json:"confirmed"`
}
type Container struct{ ID, PodUID, Name, Namespace, PodName, State string }
type CRIRuntime interface {
	ListContainers(context.Context) ([]Container, error)
	StopContainer(context.Context, string) error
}

func digest(b []byte) string     { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func ScopeDigest(s Scope) string { b, _ := json.Marshal(s); return digest(b) }
func ValidateReceipt(s Scope, r *Receipt) error {
	if e := s.Validate(); e != nil {
		return e
	}
	if r == nil || !r.Confirmed || r.ScopeDigest != ScopeDigest(s) || r.OperationID != s.OperationID || r.PodUID != s.PodUID || r.ContainerName != s.ContainerName || r.NodeUID != s.NodeUID || r.NodeBootID != s.NodeBootID || len(r.LedgerDigest) != 64 || r.FinishedAt.IsZero() {
		return fmt.Errorf("recovery receipt identity mismatch")
	}
	if decoded, e := hex.DecodeString(r.LedgerDigest); e != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != r.LedgerDigest {
		return fmt.Errorf("invalid ledger digest")
	}
	if r.FinishedAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) || r.FinishedAt.After(time.Now().Add(5*time.Minute)) {
		return fmt.Errorf("invalid receipt time")
	}
	if s.ContainerID != "" && (len(r.StoppedContainerIDs) != 1 || r.StoppedContainerIDs[0] != s.ContainerID) {
		return fmt.Errorf("receipt pinned container missing")
	}
	for _, id := range r.StoppedContainerIDs {
		if s.ContainerID != "" && id != s.ContainerID {
			return fmt.Errorf("receipt container mismatch")
		}
	}
	return nil
}
func (s Scope) Validate() error {
	if s.Version != 1 || s.OperationID == "" || s.Namespace == "" || s.PodName == "" || s.PodUID == "" || s.NodeName == "" || s.NodeUID == "" || s.NodeBootID == "" || s.ContainerName == "" || s.GatewayID == "" || s.InstanceID < 1 || s.UserID < 1 || s.Generation < 1 || s.RuntimeType == "" || strings.ContainsAny(s.RuntimeType, "/\\.") || !filepath.IsAbs(s.WorkspaceRoot) || filepath.Clean(s.WorkspaceRoot) != s.WorkspaceRoot || s.WorkspaceRoot == "/" {
		return fmt.Errorf("incomplete recovery scope")
	}
	if !canonicalSlot(s.ContainerName) || s.GatewayID != fmt.Sprintf("gw-%d-%d", s.InstanceID, s.Generation) {
		return fmt.Errorf("noncanonical slot or gateway identity")
	}

	for _, v := range []string{s.OperationID, s.Namespace, s.PodName, s.PodUID, s.NodeName, s.NodeUID, s.NodeBootID, s.RuntimeType, s.ContainerID} {
		if len(v) > 256 || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\x00\r\n") {
			return fmt.Errorf("invalid recovery identity")
		}
	}
	if len(s.WorkspaceRoot) > 4096 {
		return fmt.Errorf("workspace root too long")
	}
	if s.ContainerID != "" {
		parts := strings.Split(s.ContainerID, "://")
		if len(parts) != 2 || parts[0] != "containerd" || parts[1] == "" {
			return fmt.Errorf("unsupported container identity")
		}
	}
	return nil
}

type ledger struct {
	InstanceID  int    `json:"instance_id"`
	UserID      int    `json:"user_id"`
	Generation  int    `json:"generation"`
	RuntimeType string `json:"runtime_type"`
	PodUID      string `json:"pod_uid"`
	Slot        string `json:"slot"`
	State       string `json:"state"`
}

func readTrusted(p string) ([]byte, error) {
	fd, e := unix.Open(p, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), p)
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 || int(st.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return nil, fmt.Errorf("untrusted recovery file")
	}
	return io.ReadAll(io.LimitReader(f, 65537))
}
func atomic(p string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".recovery-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), p); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func lock(ctx context.Context, p string, create bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_NOFOLLOW
	if create {
		flags |= unix.O_CREAT
	}
	fd, e := unix.Open(p, flags, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), p)
	st, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 || int(st.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		f.Close()
		return nil, fmt.Errorf("untrusted lock file")
	}
	for {
		e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			return f, nil
		}
		if e != unix.EWOULDBLOCK {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func Run(ctx context.Context, s Scope, cri CRIRuntime, workspaceRoot, bootIDPath string) (*Receipt, error) {
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
	if e := trustedDirectory(workspaceRoot); e != nil {
		return nil, e
	}

	dir := filepath.Join(workspaceRoot, ".clawmanager-slot-locks")
	_, e = os.Lstat(dir)
	if os.IsNotExist(e) {
		if e = os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
			return nil, e
		} else if e == nil {
			parent, err := os.Open(workspaceRoot)
			if err != nil {
				return nil, err
			}
			err = parent.Sync()
			parent.Close()
			if err != nil {
				return nil, err
			}
		}
		_, e = os.Lstat(dir)
	}
	if e != nil {
		return nil, e
	}
	if e = trustedDirectory(dir); e != nil {
		return nil, e
	}

	receiptPath := filepath.Join(dir, ".recovery-"+ScopeDigest(s)+".json")
	op, e := lock(ctx, receiptPath+".lock", true)
	if e != nil {
		return nil, e
	}
	defer op.Close()
	if b, e := readTrusted(receiptPath); e == nil {
		var r Receipt
		if e = json.Unmarshal(b, &r); e != nil {
			return nil, e
		}
		return &r, ValidateReceipt(s, &r)
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	lp := filepath.Join(dir, fmt.Sprintf("%s-%d-%d.json", s.RuntimeType, s.UserID, s.InstanceID))
	read := func() (*ledger, error) {
		b, e := readTrusted(lp)
		if e != nil {
			return nil, e
		}
		var l ledger
		if e = json.Unmarshal(b, &l); e != nil {
			return nil, e
		}
		if l.InstanceID != s.InstanceID || l.UserID != s.UserID || l.Generation != s.Generation || l.RuntimeType != s.RuntimeType || l.PodUID != s.PodUID || l.Slot != s.ContainerName || (l.State != "running" && l.State != "stopped" && l.State != "upgrading") {
			return nil, fmt.Errorf("ledger owner or generation changed")
		}
		return &l, nil
	}
	_, readErr := read()
	missing := os.IsNotExist(readErr)
	if readErr != nil && !missing {
		return nil, readErr
	}
	if _, e = readTrusted(strings.TrimSuffix(lp, ".json") + ".lock"); e != nil && !(missing && os.IsNotExist(e)) {
		return nil, e
	}
	// A protected intent records positive CRI identity before stopping. It permits
	// a crash retry after CRI garbage collection, but never proves a stop by itself.
	intentPath := receiptPath + ".intent"
	type stopIntent struct {
		ScopeDigest string `json:"scope_digest"`
		ContainerID string `json:"container_id"`
	}
	intentVerified := false
	if b, e := readTrusted(intentPath); e == nil {
		var intent stopIntent
		if json.Unmarshal(b, &intent) != nil || intent.ScopeDigest != ScopeDigest(s) || intent.ContainerID != s.ContainerID || intent.ContainerID == "" {
			return nil, fmt.Errorf("invalid durable stop intent")
		}
		intentVerified = true
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	all, e := cri.ListContainers(ctx)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	toStop := []string{}
	positivelyIdentified := false
	expected := strings.TrimPrefix(s.ContainerID, "containerd://")
	for _, c := range all {
		if c.ID == expected && expected != "" && (c.PodUID != s.PodUID || c.Name != s.ContainerName) {
			return nil, fmt.Errorf("container identity mismatch")
		}
		if c.PodUID == s.PodUID && c.Name == s.ContainerName {
			if expected != "" && c.ID != expected {
				continue
			}
			if c.Namespace != "" && (c.Namespace != s.Namespace || c.PodName != s.PodName) {
				return nil, fmt.Errorf("sandbox identity mismatch")
			}
			if c.State != "exited" {
				if c.State != "running" && c.State != "created" {
					return nil, fmt.Errorf("container state unknown")
				}
				toStop = append(toStop, c.ID)
			}
			if expected == "" || c.ID == expected {
				ids = append(ids, "containerd://"+c.ID)
				if expected != "" && c.ID == expected {
					positivelyIdentified = true
				}
			}
		}
	}
	if missing && !positivelyIdentified && !intentVerified {
		return nil, fmt.Errorf("missing ledger requires positively identified pinned container")
	}
	if positivelyIdentified && !intentVerified {
		if e = atomic(intentPath, stopIntent{ScopeDigest(s), s.ContainerID}); e != nil {
			return nil, e
		}
	}
	for _, id := range toStop {
		if e = cri.StopContainer(ctx, id); e != nil {
			return nil, e
		}
	}
	// A complete CRI inventory and unchanged node boot permit an already-GCed pinned container.
	if expected != "" && len(ids) == 0 {
		ids = append(ids, s.ContainerID)
	}

	lf, e := lock(ctx, strings.TrimSuffix(lp, ".json")+".lock", missing)
	if e != nil {
		return nil, e
	}
	defer lf.Close()
	l, e := read()
	if os.IsNotExist(e) && missing {
		l = &ledger{InstanceID: s.InstanceID, UserID: s.UserID, Generation: s.Generation, RuntimeType: s.RuntimeType, PodUID: s.PodUID, Slot: s.ContainerName, State: "stopped"}
		e = nil
	}
	if e != nil {
		return nil, e
	}
	all, e = cri.ListContainers(ctx)
	if e != nil {
		return nil, e
	}
	for _, c := range all {
		if c.PodUID == s.PodUID && c.Name == s.ContainerName && (expected == "" || c.ID == expected) && c.State != "exited" {
			return nil, fmt.Errorf("old source still executing")
		}
	}
	l.State = "stopped"
	if e = atomic(lp, l); e != nil {
		return nil, e
	}
	b, e := readTrusted(lp)
	if e != nil {
		return nil, e
	}
	r := &Receipt{ScopeDigest: ScopeDigest(s), OperationID: s.OperationID, PodUID: s.PodUID, ContainerName: s.ContainerName, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, StoppedContainerIDs: ids, LedgerDigest: digest(b), FinishedAt: time.Now().UTC(), Confirmed: true}
	if e = atomic(receiptPath, r); e != nil {
		return nil, e
	}
	return r, nil
}
