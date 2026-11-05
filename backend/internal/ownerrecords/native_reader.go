package ownerrecords

import (
	"context"
	"time"
)

// NativeHistory keeps the SQL projection independent of how private runtime
// files are read. The default reader retains the existing local behavior.
func (s *Store) NativeHistory(ctx context.Context, workspace string, identity Identity, recent bool, cutoff ...time.Time) ([]NativeQuestion, error) {
	if s.HistoryReader != nil {
		return s.HistoryReader(ctx, s.HistoryRoot, workspace, identity, recent, cutoff...)
	}
	return readNativeHistory(ctx, s.HistoryRoot, workspace, identity, recent, cutoff...)
}

func (s *Store) NativeFingerprint(ctx context.Context, workspace string, identity Identity) (string, error) {
	if s.FingerprintReader != nil {
		return s.FingerprintReader(ctx, s.HistoryRoot, workspace, identity)
	}
	return NativeHistoryFingerprint(ctx, s.HistoryRoot, workspace, identity)
}

type nativeHistoryHelperContextKey struct{}

// ReadNativeHistorySnapshot runs inside the credential helper's locked OS
// thread. Its fixed SQLite child must die with that helper, including on cancel.
func ReadNativeHistorySnapshot(ctx context.Context, root, workspace string, identity Identity, recent bool, cutoff ...time.Time) ([]NativeQuestion, error) {
	return readNativeHistory(context.WithValue(ctx, nativeHistoryHelperContextKey{}, true), root, workspace, identity, recent, cutoff...)
}

func NativeHistoryFingerprint(ctx context.Context, root, workspace string, identity Identity) (string, error) {
	return historyFingerprint(ctx, root, workspace, identity)
}
