package runtimeeviction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectSourceLedgerReadOnly(t *testing.T) {
	s, _, root, _ := fixture(t)
	p := filepath.Join(root, ".clawmanager-slot-locks", "opencode-2-1.json")
	before, _ := os.ReadFile(p)
	slot, e := InspectSourceLedger(root, s.InstanceID, s.UserID, s.Generation, s.RuntimeType, s.PodUID)
	if e != nil || slot != s.ContainerName {
		t.Fatalf("wrong inspection %q: %v", slot, e)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("inspection rewrote ledger")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 2 {
		t.Fatalf("inspection created files: %v", entries)
	}
}
func TestInspectRejectsUntrustedOrDifferentOwner(t *testing.T) {
	for _, kind := range []string{"generation", "uid", "type", "slot", "symlink", "directory", "root", "missing"} {
		t.Run(kind, func(t *testing.T) {
			s, _, root, _ := fixture(t)
			dir := filepath.Join(root, ".clawmanager-slot-locks")
			p := filepath.Join(dir, "opencode-2-1.json")
			switch kind {
			case "generation":
				s.Generation++
			case "uid":
				s.PodUID = "other"
			case "type":
				s.RuntimeType = "../opencode"
			case "slot":
				b := []byte(`{"instance_id":1,"user_id":2,"generation":3,"runtime_type":"opencode","pod_uid":"uid","slot":"slot-01","state":"running"}`)
				os.WriteFile(p, b, 0600)
			case "symlink":
				target := filepath.Join(t.TempDir(), "ledger")
				os.Rename(p, target)
				os.Symlink(target, p)
			case "directory":
				os.Chmod(dir, 0777)
			case "root":
				os.Chmod(root, 0777)
			case "missing":
				os.Remove(p)
			}
			if _, e := InspectSourceLedger(root, s.InstanceID, s.UserID, s.Generation, s.RuntimeType, s.PodUID); e == nil {
				t.Fatal("accepted untrusted or different source")
			}
		})
	}
}
