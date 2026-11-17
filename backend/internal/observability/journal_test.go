package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestJournalAdmissionReserveRestartAndDuplicate(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{3}, 32)
	limits := Limits{GlobalBytes: 2 << 20, OwnerBytes: 1 << 20, RequestBytes: 1 << 20}
	j, e := NewJournal(dir, key, limits)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e = j.Admit(ctx, "user:1", "request-1"); e != nil {
		t.Fatal(e)
	}
	if e = j.Admit(ctx, "user:1", "request-2"); !errors.Is(e, ErrOverloaded) {
		t.Fatalf("owner quota: %v", e)
	}
	if e = j.Admit(ctx, "user:2", "request-2"); e != nil {
		t.Fatal(e)
	}
	if e = j.Admit(ctx, "user:3", "request-3"); !errors.Is(e, ErrOverloaded) {
		t.Fatalf("global quota: %v", e)
	}
	ev := Event{UID: "event-1", Owner: "user:1", RequestID: "request-1", Kind: "cost_records", Payload: json.RawMessage(`{"actual_cost":1}`)}
	if e = j.Append(ctx, ev); e != nil {
		t.Fatal(e)
	}
	if e = j.Append(ctx, ev); e != nil {
		t.Fatal(e)
	}
	if e = j.Finish(ctx, ev.Owner, ev.RequestID); e != nil {
		t.Fatal(e)
	}
	restarted, e := NewJournal(dir, key, limits)
	if e != nil {
		t.Fatal(e)
	}
	outage := errors.New("sql unavailable")
	if _, e = restarted.Replay(ctx, 10, func(context.Context, Event) error { return outage }); !errors.Is(e, outage) {
		t.Fatalf("outage %v", e)
	}
	seen := map[string]int{}
	n, e := restarted.Replay(ctx, 10, func(_ context.Context, v Event) error { seen[v.UID]++; return nil })
	if e != nil || n != 1 || seen[ev.UID] != 1 {
		t.Fatalf("replay %d %v %v", n, seen, e)
	}
	if e = restarted.Admit(ctx, "user:1", "request-3"); e != nil {
		t.Fatalf("completed capacity not released: %v", e)
	}
	states, e := restarted.Recovery(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(states) != 2 {
		t.Fatalf("ambiguous admissions lost: %+v", states)
	}
}
func TestJournalRejectsConflictingIdentity(t *testing.T) {
	j, _ := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{GlobalBytes: 1 << 20, OwnerBytes: 1 << 20, RequestBytes: 1 << 20})
	ctx := context.Background()
	j.Admit(ctx, "u", "r")
	e := Event{UID: "stable", Owner: "u", RequestID: "r", Kind: "audit_events", Payload: json.RawMessage(`{"x":1}`)}
	if err := j.Append(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Payload = json.RawMessage(`{"x":2}`)
	if err := j.Append(ctx, e); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("conflict: %v", err)
	}
}

func TestJournalWrongKeyAndUnpublishedReservationRecovery(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{8}, 32)
	limits := Limits{GlobalBytes: 1 << 20, OwnerBytes: 1 << 20, RequestBytes: 1 << 20}
	j, e := NewJournal(dir, key, limits)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, ".admitting-abandoned"), make([]byte, 1024), 0600); e != nil {
		t.Fatal(e)
	}
	if e = j.Admit(context.Background(), "owner", "request"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(dir, ".admitting-abandoned")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("unpublished reservation leaked")
	}
	if _, e = NewJournal(dir, bytes.Repeat([]byte{9}, 32), limits); e == nil {
		t.Fatal("wrong stable key accepted")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".wal") {
			data, e := os.ReadFile(filepath.Join(dir, entry.Name()))
			if e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(data, []byte("owner")) || bytes.Contains(data, []byte("request")) {
				t.Fatal("plaintext journal identity")
			}
		}
	}
}
func TestJournalConcurrentAdmissionIsBounded(t *testing.T) {
	j, e := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{GlobalBytes: 2 << 20, OwnerBytes: 1 << 20, RequestBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	var group sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			err := j.Admit(context.Background(), fmt.Sprint(i), fmt.Sprint(i))
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrOverloaded) {
				t.Error(err)
			}
		}(i)
	}
	group.Wait()
	if successes.Load() != 2 {
		t.Fatalf("admitted %d beyond reserved limit", successes.Load())
	}
}

// The child dies after fsync of an unpublished long ciphertext, exactly at the
// append commit boundary. Retrying with a short control frame must hide its tail.
func TestJournalUnpublishedLongFrameThenShortRetry(t *testing.T) {
	key := bytes.Repeat([]byte{6}, 32)
	limits := Limits{GlobalBytes: 1 << 20, OwnerBytes: 1 << 20, RequestBytes: 1 << 20}
	ctx := context.Background()
	if dir := os.Getenv("CM_WAL_CRASH_DIR"); dir != "" {
		j, err := NewJournal(dir, key, limits)
		if err != nil {
			t.Fatal(err)
		}
		s, err := j.read(j.key("owner", "request"))
		if err != nil {
			t.Fatal(err)
		}
		frame := journalFrame{Type: "event", Event: &Event{UID: "unpublished", Owner: "owner", RequestID: "request", Kind: "audit_events", Payload: json.RawMessage(`"` + strings.Repeat("x", 4096) + `"`)}}
		raw, _ := json.Marshal(frame)
		ciphertext, err := j.crypto.seal("journal-v1", raw, []byte(s.file))
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(dir, s.file), os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.WriteAt(ciphertext, s.offset+4); err != nil {
			t.Fatal(err)
		}
		if err = f.Sync(); err != nil {
			t.Fatal(err)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		t.Fatal("child survived SIGKILL")
	}
	dir := t.TempDir()
	j, err := NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Admit(ctx, "owner", "request"); err != nil {
		t.Fatal(err)
	}
	event := Event{UID: "accepted", Owner: "owner", RequestID: "request", Kind: "audit_events", Payload: json.RawMessage(`{"accepted":true}`)}
	if err = j.Append(ctx, event); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestJournalUnpublishedLongFrameThenShortRetry$")
	cmd.Env = append(os.Environ(), "CM_WAL_CRASH_DIR="+dir)
	if err = cmd.Run(); err == nil {
		t.Fatal("crash child unexpectedly succeeded")
	}
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("child did not reach durable crash boundary: %v", err)
	}
	j, err = NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	if n, err := j.Replay(ctx, 500, func(_ context.Context, got Event) error {
		if got.UID != event.UID {
			t.Fatalf("unpublished event resurrected: %s", got.UID)
		}
		seen++
		return nil
	}); err != nil || n != 1 || seen != 1 {
		t.Fatalf("accepted replay lost: n=%d seen=%d err=%v", n, seen, err)
	}
	j, err = NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := j.Replay(ctx, 500, func(context.Context, Event) error { t.Fatal("delivered frame resurrected"); return nil }); err != nil || n != 0 {
		t.Fatalf("short retry corrupted journal: n=%d err=%v", n, err)
	}
	if err = j.Finish(ctx, "owner", "request"); err != nil {
		t.Fatal(err)
	}
	if _, err = j.Replay(ctx, 500, func(context.Context, Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = j.Admit(ctx, "owner", "next"); err != nil {
		t.Fatalf("reservation not freed: %v", err)
	}
}
