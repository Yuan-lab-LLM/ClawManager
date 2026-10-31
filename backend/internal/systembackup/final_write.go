package systembackup

import (
	"bytes"
	"context"
	"errors"
	"time"
)

// FinalWriteProvider must be resolved from the frozen SecretRef. The provider
// performs conditional-create and exact immutable-version reads. A reader's
// 404 is never treated as proof that an earlier dispatched write is absent.
type FinalWriteProvider interface {
	PutImmutable(context.Context, string, []byte) (ObjectReference, error)
	ReadCurrent(context.Context, string) ([]byte, ObjectReference, error)
}

func (s ImmutableObjectStore) ReadCurrent(ctx context.Context, key string) ([]byte, ObjectReference, error) {
	return s.readCurrent(ctx, key)
}

type FinalWriteDriver struct {
	Resolve func(context.Context, SecretRef) (FinalWriteProvider, error)
}

// Tick processes one already frozen action. A replacement leader uses the
// same bytes/key. After deadline it only reads; it cannot begin another PUT.
func (d FinalWriteDriver) Tick(ctx context.Context, s Store, l Leadership, t Task, a ExternalAction) (bool, error) {
	if d.Resolve == nil || a.TaskID != t.ID {
		return false, errors.New("validation_failed")
	}
	actual, err := s.GetExternalAction(ctx, t.ID, a.Type)
	if err != nil {
		return false, err
	}
	if actual.ID != a.ID {
		return false, errors.New("artifact_conflict")
	}
	a = actual
	if a.State == "confirmed_present" {
		return true, nil
	}
	if a.State == "conflict" {
		return false, errors.New("artifact_conflict")
	}
	if a.State == "confirmed_absent" {
		return false, errors.New("finalizer_request_unavailable")
	}
	var now time.Time
	if err = s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return false, err
	}
	provider, err := d.Resolve(ctx, a.Request.Target)
	if err != nil {
		return false, markFinalWriteUnknown(ctx, s, l, t, a, now)
	}
	var ref ObjectReference
	if now.Before(a.Request.Deadline) && t.Status == "publishing" {
		writeContext, cancel := context.WithDeadline(ctx, a.Request.Deadline)
		ref, err = provider.PutImmutable(writeContext, a.Request.Key, a.Request.Payload)
		cancel()
		if err == nil {
			return true, s.ResolveExternalAction(ctx, l, t, a, "confirmed_present", ref)
		}
	}
	// This also adopts a successful PUT whose response was lost. Compare full
	// bytes as well as checksum, size and the immutable version reference.
	raw, observed, readErr := provider.ReadCurrent(ctx, a.Request.Key)
	if readErr == nil {
		if !bytes.Equal(raw, a.Request.Payload) {
			return false, s.ResolveExternalAction(ctx, l, t, a, "conflict", ObjectReference{})
		}
		return true, s.ResolveExternalAction(ctx, l, t, a, "confirmed_present", observed)
	}
	return false, markFinalWriteUnknown(ctx, s, l, t, a, now)
}

func markFinalWriteUnknown(ctx context.Context, s Store, l Leadership, t Task, a ExternalAction, now time.Time) error {
	if err := s.ResolveExternalAction(ctx, l, t, a, "provider_unreachable", ObjectReference{}); err != nil {
		return err
	}
	if !now.Before(a.Request.Deadline) && t.Status == "publishing" {
		_, err := s.Advance(ctx, t, l.Owner, "finalization_unknown", "none")
		return err
	}
	return nil
}
