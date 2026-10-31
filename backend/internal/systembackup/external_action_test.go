package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

type lostFinalWrite struct {
	puts      int
	payload   []byte
	reference ObjectReference
	absent    bool
}

func (p *lostFinalWrite) PutImmutable(_ context.Context, key string, raw []byte) (ObjectReference, error) {
	p.puts++
	p.payload = append([]byte(nil), raw...)
	return ObjectReference{}, errors.New("lost PUT response")
}
func (p *lostFinalWrite) ReadCurrent(_ context.Context, key string) ([]byte, ObjectReference, error) {
	if p.absent {
		return nil, ObjectReference{}, errors.New("unreachable")
	}
	return p.payload, p.reference, nil
}

func publishingTask(t *testing.T, s Store, l Leadership) Task {
	t.Helper()
	ctx := context.Background()
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	x, _, err := s.Create(ctx, 1, "publish-task-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.ClaimLedTask(ctx, l, x.ID, x.Version, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"preparing", "ready_for_capture", "acquiring_gate", "capturing", "validating", "publishing"} {
		x, err = s.Advance(ctx, x, l.Owner, next, "none")
		if err != nil {
			t.Fatal(err)
		}
	}
	return x
}

func TestExternalActionFreezesBytesAndSerializesFinalWrites(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "external-actions"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "leader-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	x := publishingTask(t, s, l)
	r := FinalWriteRequest{Type: "manifest_put", Target: SecretRef{"kubernetes", "archive", "v1", "artifact"}, Key: artifactPrefix(s.InstallationID, x.ID) + "manifest.json", Payload: []byte(`{"a":1,"z":2}`), Deadline: x.DeadlineAt}
	a, x, err := s.PrepareExternalAction(ctx, l, x, r)
	if err != nil || !x.CommitStarted || x.CanCancel {
		t.Fatal(a, x, err)
	}
	if _, _, err = s.CancelIdempotent(ctx, x.ID, x.Version, 1, "cancel-after-commit"); PublicError(err) != "commit_started" {
		t.Fatal(err)
	}
	if b, _, err := s.PrepareExternalAction(ctx, l, x, r); err != nil || a.ID != b.ID {
		t.Fatal("lost response created a second action", b, err)
	}
	r.Payload = []byte(`{"a":2,"z":2}`)
	if _, _, err = s.PrepareExternalAction(ctx, l, x, r); PublicError(err) != "artifact_conflict" {
		t.Fatal("changed request bytes accepted", err)
	}
	r.Type = "marker_put"
	r.Key = artifactPrefix(s.InstallationID, x.ID) + "_COMMITTED"
	if _, _, err = s.PrepareExternalAction(ctx, l, x, r); PublicError(err) != "coordination_invalid" {
		t.Fatal("two unresolved final actions", err)
	}
	reference := ObjectReference{a.Request.Key, "immutable-v1", int64(len(a.Request.Payload)), a.Request.SHA256}
	if err = s.ResolveExternalAction(ctx, l, x, a, "confirmed_present", reference); err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveExternalAction(ctx, l, x, a, "provider_unreachable", ObjectReference{}); PublicError(err) != "state_conflict" {
		t.Fatal("terminal action was reopened", err)
	}
	if _, _, err = s.PrepareExternalAction(ctx, l, x, r); PublicError(err) != "coordination_invalid" {
		t.Fatal("marker bypassed missing index", err)
	}
	indexRequest := r
	indexRequest.Type = "index_put"
	indexRequest.Key = artifactPrefix(s.InstallationID, x.ID) + "index.json"
	indexAction, _, err := s.PrepareExternalAction(ctx, l, x, indexRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveExternalAction(ctx, l, x, indexAction, "confirmed_present", ObjectReference{Key: indexAction.Request.Key, VersionID: "index-v1", Bytes: int64(len(indexAction.Request.Payload)), SHA256: indexAction.Request.SHA256}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PrepareExternalAction(ctx, l, x, r); err != nil {
		t.Fatal("next final action blocked", err)
	}
}

func TestSignaturePurposeAndCanonicalization(t *testing.T) {
	raw, err := canonicalJSON(map[string]any{"z": "<>&\u2028", "a": 1e-6})
	if err != nil || string(raw) != "{\"a\":0.000001,\"z\":\"<>&\u2028\"}" {
		t.Fatalf("%s %v", raw, err)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	sig, err := signPurpose("test-domain", "artifact", map[string]any{"a": 1}, key, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyPurpose("test-domain", "acceptance", map[string]any{"a": 1}, sig, pub, "v1"); err == nil {
		t.Fatal("artifact signature accepted as acceptance")
	}
	if err = verifyPurpose("another-domain", "artifact", map[string]any{"a": 1}, sig, pub, "v1"); err == nil {
		t.Fatal("signature crossed domain")
	}
	sig.Protected.KeyVersion = "v2"
	if err = verifyPurpose("test-domain", "artifact", map[string]any{"a": 1}, sig, pub, "v1"); err == nil {
		t.Fatal("mutable signing header")
	}
}

func TestFinalWriteAdoptsResponseLossAndDoesNotDispatchAfterDeadline(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "final-driver"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "leader-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	x := publishingTask(t, s, l)
	r := FinalWriteRequest{Type: "manifest_put", Target: SecretRef{"kubernetes", "archive", "v1", "artifact"}, Key: artifactPrefix(s.InstallationID, x.ID) + "manifest.json", Payload: []byte(`{"a":1}`), Deadline: x.DeadlineAt}
	a, x, err := s.PrepareExternalAction(ctx, l, x, r)
	if err != nil {
		t.Fatal(err)
	}
	p := &lostFinalWrite{reference: ObjectReference{r.Key, "version-1", int64(len(r.Payload)), a.Request.SHA256}}
	d := FinalWriteDriver{Resolve: func(context.Context, SecretRef) (FinalWriteProvider, error) { return p, nil }}
	if done, err := d.Tick(ctx, s, l, x, a); err != nil || !done || p.puts != 1 {
		t.Fatal(done, err, p.puts)
	}
	if done, err := d.Tick(ctx, s, l, x, a); err != nil || !done || p.puts != 1 {
		t.Fatal("successful action dispatched again", done, err, p.puts)
	}
	r.Type = "marker_put"
	r.Key = artifactPrefix(s.InstallationID, x.ID) + "_COMMITTED"
	a, x, err = s.PrepareExternalAction(ctx, l, x, r)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash until the absolute dispatch deadline has passed. The
	// frozen ledger changes only in this fixture to avoid a wall-clock sleep.
	a.Request.Deadline = time.Now().UTC().Add(-time.Minute)
	encoded, _ := canonicalJSON(a.Request)
	hash, _ := RequestHash(a.Request)
	if _, err = s.DB.Exec("UPDATE system_backup_external_actions SET request_json=?,request_hash=?,dispatch_deadline=? WHERE public_id=?", encoded, hash, a.Request.Deadline, a.ID); err != nil {
		t.Fatal(err)
	}
	p.absent = true
	if done, err := d.Tick(ctx, s, l, x, a); err != nil || done || p.puts != 1 {
		t.Fatal("post-deadline write", done, err, p.puts)
	}
	if actual, err := s.Get(ctx, x.ID); err != nil || actual.Status != "finalization_unknown" || actual.CanCancel {
		t.Fatal(actual, err)
	}
}
