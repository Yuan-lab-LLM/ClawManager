//go:build linux

package services

import (
	"clawreef/internal/ownerrecords"
	"os"
	"path/filepath"
	"testing"
)

func TestNFSOwnerHistoryReadsPrivateFilesWithInstanceScope(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("trusted credential helper requires root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-nfs-history-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	workspace := RuntimeWorkspacePathWithRoot(base, RuntimeTypeOpenClaw, 3, 12)
	dir := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "sessions")
	if err = os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "session.jsonl")
	content := `{"type":"session","id":"session-1"}
{"type":"message","id":"u1","timestamp":"2026-09-17T02:00:00Z","message":{"role":"user","idempotencyKey":"run-1","__openclaw":{"senderIsOwner":true},"content":"private question"}}
{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-09-17T02:01:00Z","message":{"role":"assistant","stopReason":"stop","content":"reply"}}
`
	os.WriteFile(log, []byte(content), 0600)
	os.Chown(log, 200012, 200012)
	store := &ownerrecords.Store{HistoryRoot: base}
	ConfigureOwnerHistoryNFSIO(store, "nfs-rootless-v1")
	identity := ownerrecords.Identity{Scope: 3, Instance: 12, Runtime: "openclaw", Mode: "lite"}
	questions, err := store.NativeHistory(t.Context(), workspace, identity, false)
	if err != nil || len(questions) != 1 || questions[0].ID != "run-1" || questions[0].Text != "" {
		t.Fatalf("history: %+v %v", questions, err)
	}
	questions, err = store.NativeHistory(t.Context(), workspace, identity, true)
	if err != nil || len(questions) == 0 || questions[0].Text != "private question" {
		t.Fatalf("recent: %+v %v", questions, err)
	}
	fingerprint, err := store.NativeFingerprint(t.Context(), workspace, identity)
	if err != nil || fingerprint == "" {
		t.Fatal(fingerprint, err)
	}
	identity.Instance = 13
	if _, err = store.NativeHistory(t.Context(), workspace, identity, false); err == nil {
		t.Fatal("foreign identity authorized")
	}
}

func TestNFSOwnerHistoryRealRootSquash(t *testing.T) {
	root := os.Getenv("CM_NFS_OWNER_HISTORY_ROOT")
	if root == "" {
		t.Skip("requires isolated root-squash fixture")
	}
	for _, runtimeType := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes} {
		workspace := RuntimeWorkspacePathWithRoot(root, runtimeType, 3, 12)
		private := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "sessions", "session.jsonl")
		if runtimeType == RuntimeTypeHermes {
			private = filepath.Join(workspace, "home", ".hermes", "state.db")
		}
		if _, err := os.ReadFile(private); !os.IsPermission(err) {
			t.Fatalf("backend root must be denied %s: %v", runtimeType, err)
		}
		store := &ownerrecords.Store{HistoryRoot: root}
		ConfigureOwnerHistoryNFSIO(store, "nfs-rootless-v1")
		identity := ownerrecords.Identity{Scope: 3, Instance: 12, Runtime: runtimeType, Mode: "lite"}
		facts, err := store.NativeHistory(t.Context(), workspace, identity, false)
		if err != nil || len(facts) != 1 {
			t.Fatalf("%s facts %+v %v", runtimeType, facts, err)
		}
		recent, err := store.NativeHistory(t.Context(), workspace, identity, true)
		if err != nil || len(recent) != 1 || recent[0].Text == "" {
			t.Fatalf("%s recent %+v %v", runtimeType, recent, err)
		}
		fingerprint, err := store.NativeFingerprint(t.Context(), workspace, identity)
		if err != nil || fingerprint == "" {
			t.Fatal(runtimeType, fingerprint, err)
		}
	}
	t.Log("root-squash denied backend UID0; exact-instance read-only helpers parsed private native JSONL and SQLite histories, recent questions and fingerprints")
}

func TestNFSOwnerHistoryRejectsPreexistingForeignHardlink(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("trusted credential helper requires root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-nfs-history-hardlink-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	workspace := RuntimeWorkspacePathWithRoot(base, RuntimeTypeOpenClaw, 3, 12)
	directory := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "sessions")
	if err = os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(base, "foreign.jsonl")
	data := `{"type":"session","id":"foreign-session"}
{"type":"message","id":"foreign-question","message":{"role":"user","idempotencyKey":"foreign-id","__openclaw":{"senderIsOwner":true},"content":"foreign private history"}}
{"type":"message","parentId":"foreign-question","timestamp":"2026-09-17T02:01:00Z","message":{"role":"assistant","stopReason":"stop","content":"reply"}}
`
	if err = os.WriteFile(foreign, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	os.Chown(foreign, 200013, 200013)
	if err = os.Link(foreign, filepath.Join(directory, "foreign.jsonl")); err != nil {
		t.Fatal(err)
	}
	store := &ownerrecords.Store{HistoryRoot: base}
	ConfigureOwnerHistoryNFSIO(store, "nfs-rootless-v1")
	identity := ownerrecords.Identity{Scope: 3, Instance: 12, Runtime: "openclaw", Mode: "lite"}
	if questions, err := store.NativeHistory(t.Context(), workspace, identity, true); err == nil {
		t.Fatalf("foreign hard-linked history exposed: %+v", questions)
	}
}

func TestNFSOwnerHistoryRealRootSquashHardlink(t *testing.T) {
	root := os.Getenv("CM_NFS_OWNER_HISTORY_HARDLINK_ROOT")
	if root == "" {
		t.Skip("requires isolated hardlink fixture")
	}
	workspace := RuntimeWorkspacePathWithRoot(root, RuntimeTypeOpenClaw, 3, 12)
	store := &ownerrecords.Store{HistoryRoot: root}
	ConfigureOwnerHistoryNFSIO(store, "nfs-rootless-v1")
	identity := ownerrecords.Identity{Scope: 3, Instance: 12, Runtime: "openclaw", Mode: "lite"}
	if questions, err := store.NativeHistory(t.Context(), workspace, identity, true); err == nil {
		t.Fatalf("NFS foreign hardlink accepted: %+v", questions)
	}
	t.Log("pre-existing foreign NFS history hardlink rejected before parser access")
}
