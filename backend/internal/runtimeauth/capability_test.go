package runtimeauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var testIdentity = Identity{InstanceID: 12, UserID: 34, Generation: 2, PodUID: "pod-uid-123"}
var testTime = time.Unix(1800000000, 0)

func testService(t *testing.T, lookup Lookup) (*Service, []byte) {
	t.Helper()
	key := []byte("dedicated-runtime-key-32-bytes!!!")
	if lookup == nil {
		lookup = func(context.Context, int) (Identity, error) { return testIdentity, nil }
	}
	s, err := New(key, lookup)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return testTime }
	return s, key
}

func issueToken(t *testing.T, s *Service) string {
	t.Helper()
	token, err := s.Issue(context.Background(), testIdentity, AudienceModel)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func signedPayload(key []byte, payload string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("clawmanager/runtime-capability/v1\x00"))
	mac.Write([]byte(encoded))
	return "v1." + encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func payloadOf(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("unexpected token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestNewRejectsWeakKeyAndMissingLookup(t *testing.T) {
	lookup := func(context.Context, int) (Identity, error) { return testIdentity, nil }
	for _, size := range []int{0, 1, 31} {
		if _, err := New(make([]byte, size), lookup); err == nil {
			t.Fatalf("accepted %d-byte key", size)
		}
	}
	if _, err := New(make([]byte, 32), nil); err == nil {
		t.Fatal("accepted missing authoritative lookup")
	}
}

func TestRoundTripAudienceAndExactTTL(t *testing.T) {
	for _, audience := range []string{AudienceModel, AudienceEgress, "services"} {
		t.Run(audience, func(t *testing.T) {
			calls := 0
			s, _ := testService(t, func(ctx context.Context, id int) (Identity, error) {
				calls++
				if id != testIdentity.InstanceID || ctx == nil {
					t.Fatal("lookup did not receive instance and context")
				}
				return testIdentity, nil
			})
			token, err := s.Issue(context.Background(), testIdentity, audience)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Verify(context.Background(), token, audience)
			if err != nil || got != testIdentity || calls != 2 {
				t.Fatalf("roundtrip: identity=%+v calls=%d err=%v", got, calls, err)
			}
			var claims struct {
				Version int   `json:"v"`
				Issued  int64 `json:"iat"`
				Expires int64 `json:"exp"`
			}
			if err := json.Unmarshal([]byte(payloadOf(t, token)), &claims); err != nil {
				t.Fatal(err)
			}
			if claims.Version != 1 || claims.Issued != testTime.Unix() || claims.Expires-claims.Issued != 900 {
				t.Fatalf("unexpected version or lifetime: %+v", claims)
			}
		})
	}
}

func TestSigningKeyIsCopiedAndDomainSeparated(t *testing.T) {
	s, key := testService(t, nil)
	original := append([]byte(nil), key...)
	for i := range key {
		key[i] ^= 0xff
	}
	token := issueToken(t, s)
	if token != signedPayload(original, payloadOf(t, token)) {
		t.Fatal("key was not copied or signature domain differs")
	}
	parts := strings.Split(token, ".")
	mac := hmac.New(sha256.New, original)
	mac.Write([]byte(parts[1]))
	parts[2] = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := s.Verify(context.Background(), strings.Join(parts, "."), AudienceModel); err == nil {
		t.Fatal("accepted signature without domain separation")
	}
}

func TestVerifyRejectsTamperPurposeAndMalformedTokens(t *testing.T) {
	s, _ := testService(t, nil)
	token := issueToken(t, s)
	parts := strings.Split(token, ".")
	bad := map[string]string{
		"empty": "", "oversize": strings.Repeat("x", 4097),
		"payload tamper":   "v1." + base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(payloadOf(t, token), `"user_id":34`, `"user_id":35`, 1))) + "." + parts[2],
		"signature tamper": strings.Join(parts[:2], ".") + "." + strings.Repeat("A", 43),
		"short signature":  strings.Join(parts[:2], ".") + ".AA",
		"extra component":  token + ".more", "version": "v2." + strings.Join(parts[1:], "."),
		"space": " " + token, "trailing newline": token + "\n",
		"padding":          "v1." + parts[1] + "=." + parts[2],
		"embedded newline": "v1." + parts[1][:3] + "\n" + parts[1][3:] + "." + parts[2],
	}
	for name, input := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Verify(context.Background(), input, AudienceModel); err == nil {
				t.Fatal("accepted invalid token")
			}
		})
	}
	for _, audience := range []string{AudienceEgress, "services", "", "control", "MODEL"} {
		if _, err := s.Verify(context.Background(), token, audience); err == nil {
			t.Fatalf("accepted audience %q", audience)
		}
	}
	for _, audience := range []string{"", "control", "MODEL"} {
		if _, err := s.Issue(context.Background(), testIdentity, audience); err == nil {
			t.Fatalf("issued unknown audience %q", audience)
		}
	}
}

func TestVerifyStrictClaimsAndIdentityBounds(t *testing.T) {
	s, key := testService(t, nil)
	valid := payloadOf(t, issueToken(t, s))
	changes := map[string]string{
		"unknown field":   strings.TrimSuffix(valid, "}") + `,"unknown":true}`,
		"duplicate field": strings.TrimSuffix(valid, "}") + `,"v":1}`,
		"trailing JSON":   valid + `{}`, "whitespace": " " + valid,
		"unknown version":     strings.Replace(valid, `"v":1`, `"v":2`, 1),
		"case alias":          strings.Replace(valid, `"v":1`, `"V":1`, 1),
		"missing field":       strings.Replace(valid, `"generation":2,`, "", 1),
		"null field":          strings.Replace(valid, `"generation":2`, `"generation":null`, 1),
		"fraction":            strings.Replace(valid, `"generation":2`, `"generation":2.0`, 1),
		"zero instance":       strings.Replace(valid, `"instance_id":12`, `"instance_id":0`, 1),
		"large user":          strings.Replace(valid, `"user_id":34`, `"user_id":2147483648`, 1),
		"negative generation": strings.Replace(valid, `"generation":2`, `"generation":-1`, 1),
		"empty pod":           strings.Replace(valid, `"pod_uid":"pod-uid-123"`, `"pod_uid":""`, 1),
		"long pod":            strings.Replace(valid, "pod-uid-123", strings.Repeat("p", 129), 1),
		"invalid pod":         strings.Replace(valid, "pod-uid-123", "pod uid", 1),
		"unknown audience":    strings.Replace(valid, `"aud":"model"`, `"aud":"admin"`, 1),
		"escaped spelling":    strings.Replace(valid, "pod-uid-123", `pod\u002duid-123`, 1),
		"long expiry":         strings.Replace(valid, fmt.Sprint(testTime.Unix()+900), fmt.Sprint(testTime.Unix()+901), 1),
		"short expiry":        strings.Replace(valid, fmt.Sprint(testTime.Unix()+900), fmt.Sprint(testTime.Unix()+899), 1),
		"zero issuance":       strings.Replace(valid, fmt.Sprint(testTime.Unix()), "0", 1),
	}
	for name, payload := range changes {
		t.Run(name, func(t *testing.T) {
			if payload == valid {
				t.Fatal("test did not change claims")
			}
			if _, err := s.Verify(context.Background(), signedPayload(key, payload), AudienceModel); err == nil {
				t.Fatal("accepted invalid claims")
			}
		})
	}
}

func TestIssueRejectsInvalidIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*Identity){
		"instance":      func(i *Identity) { i.InstanceID = 0 },
		"user":          func(i *Identity) { i.UserID = -1 },
		"generation":    func(i *Identity) { i.Generation = 2147483648 },
		"empty pod":     func(i *Identity) { i.PodUID = "" },
		"long pod":      func(i *Identity) { i.PodUID = strings.Repeat("p", 129) },
		"non ASCII pod": func(i *Identity) { i.PodUID = "pod-你好" },
	} {
		t.Run(name, func(t *testing.T) {
			id := testIdentity
			mutate(&id)
			s, _ := testService(t, func(context.Context, int) (Identity, error) { return id, nil })
			if _, err := s.Issue(context.Background(), id, AudienceModel); err == nil {
				t.Fatal("issued invalid identity")
			}
		})
	}
}

func TestExpiryAndFutureIssuance(t *testing.T) {
	s, _ := testService(t, nil)
	token := issueToken(t, s)
	for _, offset := range []time.Duration{-time.Second, 15 * time.Minute, 16 * time.Minute} {
		s.now = func() time.Time { return testTime.Add(offset) }
		if _, err := s.Verify(context.Background(), token, AudienceModel); err == nil {
			t.Fatalf("accepted token at offset %v", offset)
		}
	}
	s.now = func() time.Time { return testTime.Add(15*time.Minute - time.Nanosecond) }
	if _, err := s.Verify(context.Background(), token, AudienceModel); err != nil {
		t.Fatalf("expired before fifteen minutes: %v", err)
	}
}

func TestAuthoritativeLookupRevokesAndRejectsChangedIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*Identity){
		"instance":   func(i *Identity) { i.InstanceID++ },
		"user":       func(i *Identity) { i.UserID++ },
		"generation": func(i *Identity) { i.Generation++ },
		"pod":        func(i *Identity) { i.PodUID += "-new" },
		"inactive":   func(i *Identity) { *i = Identity{} },
	} {
		t.Run(name, func(t *testing.T) {
			current := testIdentity
			s, _ := testService(t, func(context.Context, int) (Identity, error) { return current, nil })
			token := issueToken(t, s)
			mutate(&current)
			if _, err := s.Verify(context.Background(), token, AudienceModel); err == nil {
				t.Fatal("verified stale identity")
			}
			if _, err := s.Issue(context.Background(), testIdentity, AudienceModel); err == nil {
				t.Fatal("issued stale identity")
			}
		})
	}
	s, _ := testService(t, nil)
	token := issueToken(t, s)
	s.lookup = func(context.Context, int) (Identity, error) { return testIdentity, errors.New("sentinel-secret") }
	_, issueErr := s.Issue(context.Background(), testIdentity, AudienceModel)
	_, verifyErr := s.Verify(context.Background(), token, AudienceModel)
	for _, err := range []error{issueErr, verifyErr} {
		if err == nil || strings.Contains(err.Error(), "sentinel-secret") || strings.Contains(err.Error(), token) {
			t.Fatal("lookup error did not fail closed with sanitized error")
		}
	}
}

func TestCancellationBeforeAndDuringLookup(t *testing.T) {
	s, _ := testService(t, nil)
	token := issueToken(t, s)
	for _, during := range []bool{false, true} {
		for _, operation := range []string{"issue", "verify"} {
			ctx, cancel := context.WithCancel(context.Background())
			calls := 0
			s.lookup = func(context.Context, int) (Identity, error) {
				calls++
				cancel()
				return testIdentity, nil
			}
			if !during {
				cancel()
			}
			var err error
			if operation == "issue" {
				_, err = s.Issue(ctx, testIdentity, AudienceModel)
			} else {
				_, err = s.Verify(ctx, token, AudienceModel)
			}
			cancel()
			if err == nil || (!during && calls != 0) || (during && calls != 1) {
				t.Fatalf("%s during=%v: calls=%d err=%v", operation, during, calls, err)
			}
		}
	}
}

func TestExpiryRecheckedAfterLookup(t *testing.T) {
	s, _ := testService(t, nil)
	token := issueToken(t, s)
	now := testTime
	s.now = func() time.Time { return now }
	s.lookup = func(context.Context, int) (Identity, error) {
		now = testTime.Add(15 * time.Minute)
		return testIdentity, nil
	}
	if _, err := s.Verify(context.Background(), token, AudienceModel); err == nil {
		t.Fatal("accepted token that expired during lookup")
	}
}

func TestConcurrentIssueVerify(t *testing.T) {
	s, _ := testService(t, nil)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 20 {
				token, err := s.Issue(context.Background(), testIdentity, AudienceEgress)
				if err != nil {
					t.Error(err)
					return
				}
				if got, err := s.Verify(context.Background(), token, AudienceEgress); err != nil || got != testIdentity {
					t.Errorf("concurrent roundtrip: identity=%+v err=%v", got, err)
				}
			}
		})
	}
	wg.Wait()
}
