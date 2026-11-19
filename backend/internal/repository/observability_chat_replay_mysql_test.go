package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/observability"
)

func TestDurableChatReplayAcceptsLegacyReceiptAcrossRequests(t *testing.T) {
	sess := auditFullFixture(t)
	ctx := t.Context()
	key := bytes.Repeat([]byte{3}, 32)
	journal, err := observability.NewJournal(t.TempDir(), key, observability.Limits{RequestBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := observability.NewEncryptedObjects(observationObjects{}, key)
	if err != nil {
		t.Fatal(err)
	}
	defer ConfigureObservability(sess, journal, objects)()
	owner, instance := 1, 10
	firstRequest, nextRequest := "original-request", "follow-up-request"
	sessionID := "main"
	if err := BeginObservedRequest(ctx, owner, firstRequest); err != nil {
		t.Fatal(err)
	}
	invocation := models.ModelInvocation{
		TraceID: "tool-loop", SessionID: &sessionID, RequestID: firstRequest, UserID: &owner, InstanceID: &instance,
		ProviderType: "openai", RequestedModel: "auto", ActualProviderModel: "model", Status: "completed", AttemptNo: 1,
	}
	if err := NewModelInvocationRepository(sess).Create(&invocation); err != nil || invocation.ID == 0 {
		t.Fatalf("persist invocation: id=%d err=%v", invocation.ID, err)
	}
	unknownInvocation := 0
	original := models.ChatMessageRecord{
		TraceID: "tool-loop", SessionID: "main", RequestID: &firstRequest,
		UserID: &owner, InstanceID: &instance, InvocationID: &unknownInvocation, Role: "tool", SequenceNo: 4,
		Content: "finished reading the document",
	}
	uid := observationUID(original.TableName(), &original)
	original.EventUID = &uid
	legacyPayload, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewChatMessageRepository(sess).Create(&original); err != nil || original.ID == 0 {
		t.Fatalf("persist original message: id=%d err=%v", original.ID, err)
	}
	var assigned int
	linked, err := sess.SQL().QueryRow("SELECT invocation_id FROM chat_messages WHERE id=?", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := linked.Scan(&assigned); err != nil || assigned != invocation.ID {
		t.Fatalf("projection did not attach invocation: id=%d err=%v", assigned, err)
	}
	if _, err := sess.SQL().Exec("UPDATE observability_event_receipts SET payload_sha256=? WHERE event_uid=?", observability.EventDigest(legacyPayload), uid); err != nil {
		t.Fatal(err)
	}
	if err := FinishObservedRequest(ctx, owner, firstRequest); err != nil {
		t.Fatal(err)
	}
	if err := BeginObservedRequest(ctx, owner, nextRequest); err != nil {
		t.Fatal(err)
	}
	repeated := original
	repeated.ID, repeated.RequestID, repeated.InvocationID = 0, &nextRequest, nil
	raw, err := json.Marshal(repeated)
	if err != nil {
		t.Fatal(err)
	}
	event := observability.Event{UID: uid, Owner: "user:1", RequestID: nextRequest, Kind: original.TableName(), Payload: raw}
	if err := journal.Append(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := FinishObservedRequest(ctx, owner, nextRequest); err != nil {
		t.Fatal(err)
	}
	if n, err := ReplayObservability(ctx, 20); err != nil || n != 1 {
		t.Fatalf("replay duplicate transcript and release both journals: n=%d err=%v", n, err)
	}
	if states, err := journal.Recovery(ctx); err != nil || len(states) != 0 {
		t.Fatalf("journals still reserved: %+v %v", states, err)
	}
	var count int
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM chat_messages WHERE event_uid=? AND request_id=?", uid, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&count); err != nil || count != 1 {
		t.Fatalf("original attribution lost: count=%d err=%v", count, err)
	}
	for _, mutate := range []func(*models.ChatMessageRecord){
		func(message *models.ChatMessageRecord) { message.Content = "different tool result" },
		func(message *models.ChatMessageRecord) { message.SessionID = "different session" },
	} {
		different := repeated
		mutate(&different)
		event.Payload, err = json.Marshal(different)
		if err != nil {
			t.Fatal(err)
		}
		if _, found, err := observationReceipt(ctx, sess, event); !errors.Is(err, observability.ErrIdentityConflict) || found {
			t.Fatalf("different message accepted: found=%t err=%v", found, err)
		}
	}
}

func TestDurableReplayExpiresAdmissionAfterPreservingRecordedFacts(t *testing.T) {
	sess := auditFullFixture(t)
	ctx := t.Context()
	dir, key := t.TempDir(), bytes.Repeat([]byte{4}, 32)
	journal, err := observability.NewJournal(dir, key, observability.Limits{RequestBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := observability.NewEncryptedObjects(observationObjects{}, key)
	if err != nil {
		t.Fatal(err)
	}
	defer ConfigureObservability(sess, journal, objects)()
	const owner = "user:1"
	request := "recovered-unconfirmed-request"
	if err := journal.Admit(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	userID := 1
	observed := models.AuditEvent{
		TraceID: "interrupted-pre-dispatch", RequestID: &request, UserID: &userID,
		EventType: "gateway.request.received", TrafficClass: models.TrafficClassLLM,
		Severity: models.AuditSeverityInfo, Message: "Gateway accepted request", CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	uid := observationUID("audit_events", &observed)
	observed.EventUID = &uid
	raw, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(ctx, observability.Event{UID: uid, Owner: owner, RequestID: request, Kind: "audit_events", Payload: raw}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.wal"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected a single journal: %v %v", files, err)
	}
	if err = os.Chtimes(files[0], observed.CreatedAt, observed.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if n, err := ReplayObservability(ctx, 20); n != 1 || err != nil {
		t.Fatalf("replay found facts before expiry: n=%d err=%v", n, err)
	}
	var state string
	row, err := sess.SQL().QueryRow("SELECT state FROM observability_admissions WHERE owner=? AND request_id=? AND completed_at IS NOT NULL", owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&state); err != nil || state != observability.AdmissionOutcomeUnknown {
		t.Fatalf("missing persisted unknown outcome: state=%s err=%v", state, err)
	}
	if _, err = os.Stat(files[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal quota still held: %v", err)
	}
	if err = BeginObservedRequest(ctx, userID, request); !errors.Is(err, observability.ErrRecoveryRequired) {
		t.Fatalf("expired request could execute again: %v", err)
	}
	var count int
	row, err = sess.SQL().QueryRow("SELECT COUNT(*) FROM audit_events WHERE event_uid=? AND event_type='gateway.request.received'", uid)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); count != 1 || err != nil {
		t.Fatalf("original audit evidence was not preserved: count=%d err=%v", count, err)
	}
}
