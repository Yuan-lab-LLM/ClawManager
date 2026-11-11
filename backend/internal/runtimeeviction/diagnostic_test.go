package runtimeeviction

import (
	"errors"
	"strings"
	"testing"
)

func TestFailureDiagnosticDoesNotExposeOpaqueRuntimeErrors(t *testing.T) {
	b := FailureJSON(errors.New("rpc failed password=secret-token"))
	if len(b) > 4096 || strings.Contains(string(b), "secret-token") {
		t.Fatalf("unsafe diagnostic %s", b)
	}
	message, e := ParseFailureJSON(b)
	if e != nil || message == "" {
		t.Fatalf("invalid bounded diagnostic: %v", e)
	}
	message, e = ParseFailureJSON(FailureJSON(errors.New("ledger owner or generation changed")))
	if e != nil || !strings.Contains(message, "ledger") {
		t.Fatalf("lost actionable ledger diagnosis: %s %v", message, e)
	}
}
