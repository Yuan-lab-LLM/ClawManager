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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReplayAcknowledgesCompletedEventsAfterCancellation(t *testing.T) {
	j, err := NewJournal(t.TempDir(), bytes.Repeat([]byte{31}, 32), Limits{RequestBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	const owner, request = "user:1", "replay-deadline"
	if err := j.Admit(t.Context(), owner, request); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"first", "second"} {
		if err := j.Append(t.Context(), Event{UID: uid, Owner: owner, RequestID: request, Kind: "audit_events", Payload: json.RawMessage(`{"kind":"test"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	n, err := j.Replay(ctx, 10, func(ctx context.Context, event Event) error {
		if event.UID == "first" {
			cancel()
			return nil
		}
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("successful delivery was not acknowledged at cancellation: %d, %v", n, err)
	}
	n, err = j.Replay(t.Context(), 10, func(_ context.Context, event Event) error {
		if event.UID != "second" {
			t.Fatalf("already acknowledged event replayed again: %s", event.UID)
		}
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("remaining event did not replay: %d, %v", n, err)
	}
}

func TestJournalChatMessageLinkMayChangeAfterModelCall(t *testing.T) {
	j, err := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{RequestBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := j.Admit(ctx, "user:1", "request-1"); err != nil {
		t.Fatal(err)
	}
	event := Event{UID: "chat-message", Owner: "user:1", RequestID: "request-1", Kind: "chat_messages", Payload: json.RawMessage(`{"request_id":"request-1","content":"reply","session_id":"main"}`)}
	if err := j.Append(ctx, event); err != nil {
		t.Fatal(err)
	}
	event.Payload = json.RawMessage(`{"request_id":"request-1","invocation_id":25,"content":"reply","session_id":"main"}`)
	if err := j.Append(ctx, event); err != nil {
		t.Fatalf("complete the same message with invocation attribution: %v", err)
	}
	event.Payload = json.RawMessage(`{"request_id":"request-1","content":"another reply","session_id":"main"}`)
	if err := j.Append(ctx, event); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("different message was accepted: %v", err)
	}
}

func TestReplayReleasesEndedStreamsAndRetainsUnknownOutcomes(t *testing.T) {
	j, err := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{RequestBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const owner = "user:1"
	for _, request := range []string{"ended", "prior-version", "unknown", "missing-cost", "new-version"} {
		if err := j.Admit(ctx, owner, request); err != nil {
			t.Fatal(err)
		}
		if request != "new-version" {
			name := j.key(owner, request)
			state := &journalState{file: name, size: j.limits.RequestBytes}
			version := 0
			if request == "prior-version" {
				version = 1
			}
			if err := j.append(state, journalFrame{Type: "admit", Version: version, Owner: owner, RequestID: request, At: time.Now().Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, request := range []string{"ended", "prior-version", "missing-cost", "new-version"} {
		proof, err := json.Marshal(map[string]any{"event_type": "gateway.request.stream_failed", "request_id": request, "user_id": 1, "created_at": time.Now().Add(-2 * time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		invocation := json.RawMessage(fmt.Sprintf(`{"request_id":%q,"user_id":1,"status":"completed"}`, request))
		cost := json.RawMessage(fmt.Sprintf(`{"request_id":%q,"user_id":1,"invocation_event_uid":"invocation"}`, request))
		for _, event := range []Event{
			{UID: "end-proof", Owner: owner, RequestID: request, Kind: "audit_events", Payload: proof},
			{UID: "invocation", Owner: owner, RequestID: request, Kind: "model_invocations", Payload: invocation},
			{UID: "cost", Owner: owner, RequestID: request, Kind: "cost_records", Payload: cost},
		} {
			if request == "missing-cost" && event.Kind == "cost_records" {
				continue
			}
			if err := j.Append(ctx, event); err != nil {
				t.Fatal(err)
			}
		}
	}
	var closed []string
	terminal := func(_ context.Context, state RecoveryState) error {
		closed = append(closed, state.RequestID)
		return nil
	}
	if _, err := j.ReplayWithTerminal(ctx, 10, func(context.Context, Event) error { return ErrIdentityConflict }, terminal); !errors.Is(err, ErrIdentityConflict) || len(closed) != 0 {
		t.Fatalf("unacknowledged stream proof closed: closed=%v err=%v", closed, err)
	}
	n, err := j.ReplayWithTerminal(ctx, 20, func(context.Context, Event) error { return nil }, terminal)
	if err != nil || n != 11 || len(closed) != 2 || !slices.Contains(closed, "ended") || !slices.Contains(closed, "prior-version") {
		t.Fatalf("end receipt was not recovered: n=%d closed=%v err=%v", n, closed, err)
	}
	states, err := j.Recovery(ctx)
	if err != nil || len(states) != 3 {
		t.Fatalf("unknown outcome was not preserved: states=%+v err=%v", states, err)
	}
}

func TestReplayAnIdentityConflictKeepsTheJournalAndReleasesOthers(t *testing.T) {
	j, err := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{RequestBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const owner = "user:1"
	bad, good := "first", "second"
	if j.key(owner, bad) > j.key(owner, good) {
		bad, good = good, bad
	}
	for _, request := range []string{bad, good} {
		if err := j.Admit(ctx, owner, request); err != nil {
			t.Fatal(err)
		}
		if err := j.Append(ctx, Event{UID: request, Owner: owner, RequestID: request, Kind: "audit_events", Payload: json.RawMessage(`{"event_type":"seen"}`)}); err != nil {
			t.Fatal(err)
		}
		if err := j.Finish(ctx, owner, request); err != nil {
			t.Fatal(err)
		}
	}
	n, err := j.Replay(ctx, 10, func(_ context.Context, event Event) error {
		if event.RequestID == bad {
			return ErrIdentityConflict
		}
		return nil
	})
	if n != 1 || !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("independent journal did not complete: n=%d err=%v", n, err)
	}
	states, err := j.Recovery(ctx)
	if err != nil || len(states) != 1 || states[0].RequestID != bad || states[0].Pending != 1 {
		t.Fatalf("conflicting journal was not retained: %+v %v", states, err)
	}
}

func TestReplayClosesExpiredAdmissionWithUnknownOutcome(t *testing.T) {
	dir := t.TempDir()
	j, err := NewJournal(dir, bytes.Repeat([]byte{4}, 32), Limits{RequestBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const owner, request = "user:1", "no-execution-evidence"
	if err := j.Admit(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	aged := time.Now().Add(-2 * time.Hour)
	path := filepath.Join(dir, j.key(owner, request))
	if err := os.Chtimes(path, aged, aged); err != nil {
		t.Fatal(err)
	}
	var closed RecoveryState
	n, err := j.ReplayWithTerminal(ctx, 20, func(context.Context, Event) error {
		t.Fatal("a model event was invented for a request with no evidence")
		return nil
	}, func(_ context.Context, state RecoveryState) error {
		closed = state
		return nil
	})
	if err != nil || n != 0 || closed.RequestID != request || closed.State != "terminal_unknown" {
		t.Fatalf("expected an unknown terminal result: n=%d state=%+v err=%v", n, closed, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reservation remained after its outcome persisted: %v", err)
	}
	if err := j.Admit(ctx, owner, "next-request"); err != nil {
		t.Fatalf("capacity was not reusable: %v", err)
	}
}

func TestReplayProtectsActiveAdmissionAndWaitsForOutcomePersistence(t *testing.T) {
	dir, key := t.TempDir(), bytes.Repeat([]byte{4}, 32)
	limits := Limits{RequestBytes: 1 << 20}
	writer, err := NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	replayer, err := NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const owner, request = "user:1", "active-request"
	release, err := writer.AdmitActive(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	path := filepath.Join(dir, writer.key(owner, request))
	aged := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, aged, aged); err != nil {
		t.Fatal(err)
	}
	noEvents := func(context.Context, Event) error { return nil }
	if _, err := replayer.ReplayWithTerminal(ctx, 20, noEvents, func(context.Context, RecoveryState) error {
		t.Fatal("expired a request locked by another journal writer")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	release()
	outage := errors.New("admission database unavailable")
	if _, err := replayer.ReplayWithTerminal(ctx, 20, noEvents, func(_ context.Context, state RecoveryState) error {
		if state.State != AdmissionOutcomeUnknown {
			t.Fatalf("wrong durable outcome: %+v", state)
		}
		return outage
	}); !errors.Is(err, outage) {
		t.Fatalf("SQL failure was lost: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("reservation removed before SQL success: %v", err)
	}
	if _, err := replayer.ReplayWithTerminal(ctx, 20, noEvents, func(_ context.Context, state RecoveryState) error {
		if state.State != AdmissionOutcomeUnknown {
			t.Fatalf("retry did not read the durable unknown outcome: %+v", state)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reservation still exists after durable outcome committed: %v", err)
	}
}

func TestJournalAdmissionReserveRestartAndDuplicate(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{3}, 32)
	limits := Limits{RequestBytes: 1 << 20}
	j, e := NewJournal(dir, key, limits)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e = j.Admit(ctx, "user:1", "request-1"); e != nil {
		t.Fatal(e)
	}
	if e = j.Admit(ctx, "user:1", "request-1"); !errors.Is(e, ErrRecoveryRequired) {
		t.Fatalf("admitted an already reserved request: %v", e)
	}
	if e = j.Admit(ctx, "user:2", "request-1"); e != nil {
		t.Fatal(e)
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
	j, _ := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{RequestBytes: 1 << 20})
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
	limits := Limits{RequestBytes: 1 << 20}
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
func TestJournalConcurrentAdmissionUsesDiskCapacity(t *testing.T) {
	const reserve = 64 << 10
	j, e := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{RequestBytes: reserve})
	if e != nil {
		t.Fatal(e)
	}
	var group sync.WaitGroup
	var successes atomic.Int32
	const concurrentRequests = 65
	for i := 0; i < concurrentRequests; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			err := j.Admit(t.Context(), "user:1", fmt.Sprint(i))
			if err != nil {
				t.Error(err)
				return
			}
			successes.Add(1)
		}(i)
	}
	group.Wait()
	if successes.Load() != concurrentRequests {
		t.Fatalf("admitted %d of %d requests with available disk space", successes.Load(), concurrentRequests)
	}
}

func TestJournalAppendsMoreThan128Events(t *testing.T) {
	j, err := NewJournal(t.TempDir(), bytes.Repeat([]byte{4}, 32), Limits{RequestBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const owner, request = "owner", "many-events"
	if err := j.Admit(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	for i := range 160 {
		event := Event{UID: fmt.Sprint(i), Owner: owner, RequestID: request, Kind: "audit_events", Payload: json.RawMessage(`{}`)}
		if err := j.Append(ctx, event); err != nil {
			t.Fatalf("append event %d: %v", i+1, err)
		}
	}
	states, err := j.Recovery(ctx)
	if err != nil || len(states) != 1 || states[0].Pending != 160 {
		t.Fatalf("recover all events: states=%+v err=%v", states, err)
	}
}

func TestJournalCompletionReserveGrowsWithoutOverwritingEvents(t *testing.T) {
	dir, key := t.TempDir(), bytes.Repeat([]byte{4}, 32)
	limits := Limits{}
	j, err := NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	event := Event{UID: "prior", Owner: "owner", RequestID: "expand", Kind: "audit_events", Payload: json.RawMessage(`{"preserve":true}`)}
	if err := j.Admit(ctx, event.Owner, event.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(ctx, event); err != nil {
		t.Fatal(err)
	}
	const required = 70 << 20
	if err := j.CheckCompletionReserve(ctx, event.Owner, event.RequestID, required, 180); err != nil {
		t.Fatalf("reserve completion beyond the initial WAL size: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, j.key(event.Owner, event.RequestID)))
	if err != nil {
		t.Fatal(err)
	}
	if blocks := info.Sys().(*syscall.Stat_t).Blocks; info.Size() < required || blocks*512 < required {
		t.Fatalf("completion reserve was not physically allocated: size=%d blocks=%d", info.Size(), blocks)
	}
	j, err = NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := j.Replay(ctx, 10, func(_ context.Context, got Event) error {
		if got.UID != event.UID || !bytes.Equal(got.Payload, event.Payload) {
			t.Fatalf("growth changed prior event: %s %s", got.UID, got.Payload)
		}
		return nil
	}); err != nil || n != 1 {
		t.Fatalf("replay after growth: count=%d err=%v", n, err)
	}
}

func TestJournalBatchGrowsAndReplaysPendingEvents(t *testing.T) {
	dir, key := t.TempDir(), bytes.Repeat([]byte{4}, 32)
	limits := Limits{RequestBytes: 64 << 10}
	j, err := NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const owner, request = "owner", "batch"
	if err := j.Admit(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	prior := Event{UID: "prior", Owner: owner, RequestID: request, Kind: "model_invocations", Payload: json.RawMessage(`{}`)}
	if err := j.Append(ctx, prior); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`"` + strings.Repeat("x", 1024) + `"`)
	events := make([]Event, 160)
	for i := range events {
		events[i] = Event{UID: fmt.Sprintf("%03d", i), Owner: owner, RequestID: request, Kind: "chat_messages", Payload: payload}
	}
	newEvent := Event{UID: "new", Owner: owner, RequestID: request, Kind: "chat_messages", Payload: payload}
	conflict := prior
	conflict.Payload = json.RawMessage(`{"different":true}`)
	if err := j.AppendBatch(ctx, []Event{newEvent, conflict}); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("batch identity conflict: %v", err)
	}
	for range 2 {
		if err := j.AppendBatch(ctx, events); err != nil {
			t.Fatal(err)
		}
		if err := j.DeliveredBatch(ctx, events[:40]); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, j.key(owner, request)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= limits.RequestBytes {
		t.Fatalf("batch did not grow the WAL: size=%d", info.Size())
	}
	if err := j.Finish(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	j, err = NewJournal(dir, key, limits)
	if err != nil {
		t.Fatal(err)
	}
	seen := make([]string, 0, 121)
	sink := func(_ context.Context, event Event) error {
		seen = append(seen, event.UID)
		return nil
	}
	if n, err := j.Replay(ctx, 10, sink); err != nil || n != 10 {
		t.Fatalf("first replay: count=%d err=%v", n, err)
	}
	if n, err := j.Replay(ctx, 500, sink); err != nil || n != 111 {
		t.Fatalf("remaining replay: count=%d err=%v", n, err)
	}
	want := []string{prior.UID}
	for _, event := range events[40:] {
		want = append(want, event.UID)
	}
	if !slices.Equal(seen, want) {
		t.Fatalf("replay changed pending events or ordering: got %v want %v", seen, want)
	}
	states, err := j.Recovery(ctx)
	if err != nil || len(states) != 0 {
		t.Fatalf("terminal batch was not collected: states=%+v err=%v", states, err)
	}
}

// The child dies after fsync of an unpublished long ciphertext, exactly at the
// append commit boundary. Retrying with a short control frame must hide its tail.
func TestJournalUnpublishedLongFrameThenShortRetry(t *testing.T) {
	key := bytes.Repeat([]byte{6}, 32)
	limits := Limits{RequestBytes: 1 << 20}
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
