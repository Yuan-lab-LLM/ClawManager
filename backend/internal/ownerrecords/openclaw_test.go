package ownerrecords

import "testing"

func TestOpenClawRequiresStableHumanAndFinalEvidence(t *testing.T) {
	submit := []byte(`{"type":"req","method":"chat.send","params":{"idempotencyKey":"run-1","sessionKey":"s-1","message":"hello"}}`)
	done := []byte(`{"type":"event","event":"chat","payload":{"runId":"run-1","sessionKey":"s-1","state":"final","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}}`)
	if p := DecodeOpenClaw(submit, false); p == nil || p.Kind != "submitted" || p.RunID != "run-1" {
		t.Fatal("submission not recognized")
	}
	if p := DecodeOpenClaw(done, true); p == nil || p.Kind != "completed" {
		t.Fatal("final not recognized")
	}
	for _, raw := range []string{
		`{"type":"event","event":"chat","payload":{"runId":"run-1","sessionKey":"s-1","state":"delta","message":{"role":"assistant","content":"partial"}}}`,
		`{"type":"event","event":"chat","payload":{"runId":"run-1","sessionKey":"s-1","state":"error","message":{"role":"assistant","content":"failure"}}}`,
		`{"type":"event","event":"chat","payload":{"runId":"run-1","sessionKey":"s-1","state":"final","message":{"role":"assistant","content":[]}}}`,
		`{"type":"res","ok":true,"payload":{"runId":"run-1","status":"accepted"}}`,
	} {
		if DecodeOpenClaw([]byte(raw), true) != nil {
			t.Fatal("non-final credited", raw)
		}
	}
	if DecodeOpenClaw(done, false) != nil || DecodeOpenClaw(submit, true) != nil {
		t.Fatal("wrong direction accepted")
	}
}
