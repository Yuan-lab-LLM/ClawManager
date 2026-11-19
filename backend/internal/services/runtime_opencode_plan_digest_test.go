package services

import (
	"encoding/json"
	"testing"
)

func TestOpenCodeImmutablePlanDigestCanonicalizesNestedEvidenceOnlyRepresentation(t *testing.T) {
	p := openCodeUpgradePlan{Kind: "opencode-native-preserve", Version: 1, ForceRetry: &openCodeForceAttempt{RequestID: "0afeb7c4-1cf6-4f88-8e4a-fe835bc2efb2", NativeEvidence: json.RawMessage(`[{"operation_id":"original","pod_uid":"old-pod","record_uid":"record","result_sha256":"proof","sequence":9007199254740992}]`)}}
	raw, _ := json.Marshal(p)
	var q openCodeUpgradePlan
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	q.ForceRetry.NativeEvidence = json.RawMessage(` [ { "pod_uid" : "old-pod", "sequence":9007199254740992,"result_sha256":"proof", "record_uid":"record", "operation_id":"original" } ] `)
	if openCodeImmutablePlanDigest(p) != openCodeImmutablePlanDigest(q) {
		t.Fatal("JSON object key order/whitespace changed immutable native identity")
	}
	for _, changed := range []json.RawMessage{json.RawMessage(`[{"operation_id":"other","pod_uid":"old-pod","record_uid":"record","result_sha256":"proof","sequence":9007199254740992}]`), json.RawMessage(`[{"operation_id":"original","pod_uid":"new-pod","record_uid":"record","result_sha256":"proof","sequence":9007199254740992}]`), json.RawMessage(`[{"operation_id":"original","pod_uid":"old-pod","record_uid":"record","result_sha256":"proof","sequence":9007199254740993}]`)} {
		q.ForceRetry.NativeEvidence = changed
		if openCodeImmutablePlanDigest(p) == openCodeImmutablePlanDigest(q) {
			t.Fatal("canonicalization erased real operation/Pod/numeric identity drift")
		}
	}
	q.ForceRetry.NativeEvidence = p.ForceRetry.NativeEvidence
	q.ForceRetry.RequestID = "81e247de-ef46-41ad-853e-0bb20524303e"
	if openCodeImmutablePlanDigest(p) == openCodeImmutablePlanDigest(q) {
		t.Fatal("force request UUID drift admitted")
	}
}
