package runtimeevidence

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

type sourceFunc func(context.Context, Binding) (ReceiptBundle, error)

func (f sourceFunc) ReadPinned(c context.Context, b Binding) (ReceiptBundle, error) { return f(c, b) }

type keyFunc func(context.Context, Binding) ([]byte, error)

func (f keyFunc) ResolveOriginal(c context.Context, b Binding) ([]byte, error) { return f(c, b) }
func fixture() (Binding, ReceiptBundle, []byte) {
	key := []byte("original-key<&>")
	owner := wire(ownerRecord{1, 3, "aabbccdd-1234-4abc-9abc-112233445566"})
	b := Binding{InstanceID: 8, OwnerID: 7, Generation: 3, Operation: strings.Repeat("a", 32), PodUID: "original-pod", PVCUID: "original-pvc", ClaimID: digest(owner), CanonicalBaseURL: "https://gateway.example/api/gateway/llm"}
	ns := digest(wire([]any{b.CanonicalBaseURL, string(key), 3}))
	m := hmac.New(sha256.New, key)
	m.Write(wire([]any{"native-cleanup-v1", ns, 3, b.ClaimID, true}))
	raw := wire(cleanupEnvelope{cleanupRecord{1, ns, 3, b.ClaimID, true}, hex.EncodeToString(m.Sum(nil))})
	return b, ReceiptBundle{b, owner, raw}, key
}
func verifier(bundle ReceiptBundle, key []byte) CleanupVerifier {
	return CleanupVerifier{sourceFunc(func(context.Context, Binding) (ReceiptBundle, error) { return bundle, nil }), keyFunc(func(context.Context, Binding) ([]byte, error) { return key, nil })}
}
func TestNativeCleanupVerificationNeverAuthorizesLifecycle(t *testing.T) {
	b, bundle, key := fixture()
	r, e := verifier(bundle, key).Verify(context.Background(), b)
	if e != nil || !r.NativeCleanupVerified || r.WriterReleaseVerified || r.LifecycleAuthorized || r.ReceiptHash != digest(bundle.ReceiptJSON) {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestNativeCleanupNodeJSONGoldenContract(t *testing.T) {
	b, s, _ := fixture()
	var e cleanupEnvelope
	if !strict(s.ReceiptJSON, &e) || b.ClaimID != "7bfaa430a2341c5c5badacfa8decabaa7b2a187be2bd86786ca7e456bf3aad2c" || e.Record.Namespace != "b434411c7ff992c07b46eeac731b3b20fbbfa76f55631d23d51978930d625901" || e.MAC != "461e9152d715f291066f4291d7a249c7295406d22ee6b9f2998e6a0102a576e9" {
		t.Fatal("Node/Go receipt wire encoding drifted")
	}
}
func TestNativeCleanupRejectsWrongPlacementKeyAndNoncanonicalInput(t *testing.T) {
	for _, name := range []string{"pod", "pvc", "owner", "operation", "generation", "new-key", "owner-bytes", "receipt-bytes", "duplicate", "partial", "oversized", "false-cleanup"} {
		t.Run(name, func(t *testing.T) {
			b, s, key := fixture()
			switch name {
			case "pod":
				s.Binding.PodUID = "different"
			case "pvc":
				s.Binding.PVCUID = "different"
			case "owner":
				s.Binding.OwnerID++
			case "operation":
				s.Binding.Operation = strings.Repeat("b", 32)
			case "generation":
				b.Generation++
			case "new-key":
				key = []byte("rotated-key")
			case "owner-bytes":
				s.OwnerJSON = append(s.OwnerJSON, '\n')
			case "receipt-bytes":
				s.ReceiptJSON = append(s.ReceiptJSON, ' ')
			case "duplicate":
				s.ReceiptJSON = []byte(strings.Replace(string(s.ReceiptJSON), `"version":1`, `"version":2,"version":1`, 1))
			case "partial":
				s.ReceiptJSON = []byte("{")
			case "oversized":
				s.ReceiptJSON = []byte(strings.Repeat("x", 2049))
			case "false-cleanup":
				s.ReceiptJSON = []byte(strings.Replace(string(s.ReceiptJSON), "true", "false", 1))
			}
			if r, e := verifier(s, key).Verify(context.Background(), b); r != nil || !errors.Is(e, ErrUnverified) {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
func TestNativeCleanupSourcesAreMandatoryAndErrorsSanitized(t *testing.T) {
	b, s, key := fixture()
	v := verifier(s, key)
	v.Evidence = sourceFunc(func(context.Context, Binding) (ReceiptBundle, error) {
		return ReceiptBundle{}, errors.New("PRIVATE-PATH-AND-KEY")
	})
	for _, v := range []CleanupVerifier{{}, v, {Evidence: verifier(s, key).Evidence}} {
		if _, e := v.Verify(context.Background(), b); e != ErrUnverified {
			t.Fatal(e)
		}
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := verifier(s, key).Verify(c, b); e != ErrUnverified {
		t.Fatal(e)
	}
}
func TestNativeCleanupBindingMustBePinnedAndCanonical(t *testing.T) {
	b, _, _ := fixture()
	for _, base := range []string{"https://GATEWAY.example", "https://gateway.example/", "https://gateway.example:443", "http://u:p@gateway.example", "https://gateway.example?x=1", "https://gateway.example#x", "file:///tmp/a"} {
		b.CanonicalBaseURL = base
		if b.Valid() {
			t.Fatal(base)
		}
	}
}
