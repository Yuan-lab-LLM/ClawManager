package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"clawreef/internal/models"
)

type conditionalEnrollmentStore struct {
	protocolTestStore
	enrollments int
	expected    *string
	report      models.RuntimeCapabilityReport
}

func (s *conditionalEnrollmentStore) Enroll(_ context.Context, instanceID, ownerID int, report models.RuntimeCapabilityReport, expected *string) error {
	s.enrollments++
	s.instanceID, s.ownerID, s.expected, s.report = instanceID, ownerID, expected, report
	return s.err
}

const conditionalReport = `{"protocol":1,"generation":3,"runtime_version":"native.1","adapter_version":"adapter.1","capabilities":["task_identity","turn_cancel","cancel_receipt","live_catalog"]}`

func TestConditionalEnrollmentUsesTrustedIdentityAndNeverFallsBack(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{nil, 200}, {models.ErrRuntimeCapabilityEnrollmentConflict, 409},
		{models.ErrModelUnavailable, 403}, {errors.New("private database detail"), 503},
	} {
		for _, expected := range []string{"null", `"` + strings.Repeat("a", 64) + `"`} {
			store := &conditionalEnrollmentStore{protocolTestStore: protocolTestStore{err: tc.err}}
			c, w := renewalContext(`{"report":` + conditionalReport + `,"expected_enrollment_revision":` + expected + `}`)
			NewRuntimeProtocolHandler(store, nil).Enroll(c)
			if w.Code != tc.status || store.enrollments != 1 || store.calls != 0 || store.instanceID != 8 || store.ownerID != 7 || store.report.Generation != 3 {
				t.Fatalf("status=%d store=%+v body=%s", w.Code, store, w.Body.String())
			}
			if (store.expected == nil) != (expected == "null") || strings.Contains(w.Body.String(), "private database") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("precondition, privacy or cache contract broken")
			}
			if tc.status == 200 && !strings.Contains(w.Body.String(), `"task_identity_required":true`) {
				t.Fatal("success omitted sticky policy")
			}
		}
	}
	store := &protocolTestStore{}
	c, w := renewalContext(`{"report":` + conditionalReport + `,"expected_enrollment_revision":null}`)
	NewRuntimeProtocolHandler(store, nil).Enroll(c)
	if w.Code != 503 || store.calls != 0 {
		t.Fatal("legacy repository fell back to unconditional negotiation")
	}
}

func TestConditionalEnrollmentRejectsInvalidBodiesAndStaleIdentity(t *testing.T) {
	valid := `{"report":` + conditionalReport + `,"expected_enrollment_revision":null}`
	for _, body := range []string{
		`null`, `{}`, `{"report":` + conditionalReport + `}`, valid + `{}`, valid + ` garbage`, valid + strings.Repeat(" ", 4096),
		strings.Replace(valid, `:null`, `:12`, 1), strings.Replace(valid, `:null`, `:""`, 1),
		strings.Replace(valid, `:null`, `:[]`, 1), strings.Replace(valid, `"generation":3`, `"generation":-1`, 1),
		strings.Replace(valid, `"generation":3`, `"generation":2147483648`, 1),
		strings.Replace(valid, `"report":`, `"instance_id":999,"report":`, 1),
		strings.Replace(valid, `"protocol":1`, `"protocol":1,"user_id":999`, 1),
	} {
		store := &conditionalEnrollmentStore{}
		c, w := renewalContext(body)
		NewRuntimeProtocolHandler(store, nil).Enroll(c)
		if w.Code != 400 || store.enrollments != 0 || store.calls != 0 {
			t.Fatalf("invalid body reached storage: %d", w.Code)
		}
	}
	for _, key := range []string{"instanceID", "userID", "runtimeGeneration"} {
		store := &conditionalEnrollmentStore{}
		c, w := renewalContext(valid)
		delete(c.Keys, key)
		NewRuntimeProtocolHandler(store, nil).Enroll(c)
		if w.Code != 403 || store.enrollments != 0 {
			t.Fatalf("missing %s accepted", key)
		}
	}
	store := &conditionalEnrollmentStore{}
	c, w := renewalContext(valid)
	c.Set("runtimeGeneration", 4)
	NewRuntimeProtocolHandler(store, nil).Enroll(c)
	if w.Code != 403 || store.enrollments != 0 {
		t.Fatal("stale generation accepted")
	}
}

func TestInspectAdvertisesConditionalEnrollmentWithoutWriting(t *testing.T) {
	valid := &models.RuntimeProtocolState{InstanceID: 8, OwnerUserID: 7, Generation: 2, Protocol: 1, RuntimeVersion: "native.1", AdapterVersion: "adapter.1", CapabilitiesJSON: `["task_identity","turn_cancel","cancel_receipt","live_catalog"]`}
	for _, state := range []*models.RuntimeProtocolState{nil, valid, {Protocol: 1, Generation: 2}} {
		store := &conditionalEnrollmentStore{protocolTestStore: protocolTestStore{state: state}}
		c, w := renewalContext("")
		NewRuntimeProtocolHandler(store, nil).Inspect(c)
		var body struct {
			Supported bool    `json:"conditional_enrollment_supported"`
			Revision  *string `json:"enrollment_revision"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || store.enrollments != 0 || store.instanceID != 0 {
			t.Fatal("preflight failed or wrote enrollment")
		}
		if body.Supported != (state == nil || state == valid) || (body.Revision != nil) != (state == valid) {
			t.Fatalf("invalid support advertisement: %s", w.Body.String())
		}
		if state == valid {
			revision, _ := valid.EnrollmentRevision()
			if *body.Revision != revision {
				t.Fatal("incorrect preflight revision")
			}
		}
	}
}
