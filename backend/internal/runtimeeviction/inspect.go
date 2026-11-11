package runtimeeviction

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func trustedDirectory(p string) error {
	info, e := os.Lstat(p)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return fmt.Errorf("untrusted recovery directory")
	}
	return nil
}
func canonicalSlot(slot string) bool {
	index, e := strconv.Atoi(strings.TrimPrefix(slot, "slot-"))
	return e == nil && index >= 0 && index < 256 && slot == fmt.Sprintf("slot-%d", index)
}

// InspectSourceLedger reads an existing protected writer record. It establishes
// only this record's identity, never historical Pod volume provenance or stop
// evidence. Atomic writer renames make a no-follow file read a coherent snapshot;
// Run must still acquire the stable lock and compare the record before mutation.
func InspectSourceLedger(root string, instance, user, generation int, runtimeType, podUID string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" || len(root) > 4096 || instance < 1 || user < 1 || generation < 1 || runtimeType == "" || len(runtimeType) > 256 || strings.ContainsAny(runtimeType, "/\\.\x00\r\n") || strings.TrimSpace(runtimeType) != runtimeType || podUID == "" || len(podUID) > 256 || strings.TrimSpace(podUID) != podUID || strings.ContainsAny(podUID, "\x00\r\n") {
		return "", fmt.Errorf("invalid source ledger identity")
	}
	if e := trustedDirectory(root); e != nil {
		return "", e
	}
	dir := filepath.Join(root, ".clawmanager-slot-locks")
	if e := trustedDirectory(dir); e != nil {
		return "", e
	}
	b, e := readTrusted(filepath.Join(dir, fmt.Sprintf("%s-%d-%d.json", runtimeType, user, instance)))
	if e != nil {
		return "", e
	}
	var l ledger
	if e = json.Unmarshal(b, &l); e != nil {
		return "", e
	}
	if l.InstanceID != instance || l.UserID != user || l.Generation != generation || l.RuntimeType != runtimeType || l.PodUID != podUID || !canonicalSlot(l.Slot) || (l.State != "running" && l.State != "stopped" && l.State != "upgrading") {
		return "", fmt.Errorf("source ledger owner or canonical slot changed")
	}
	return l.Slot, nil
}
