package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationFailureReportDoesNotPersistRawCredentialErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failure.json")
	err := &operationStageError{stage: "gate_connection", cause: errors.New("connection failed using fixture-secret-password")}
	if e := writeOperationFailure(path, "capture-phase", "installation", "sbk_fixture", err); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(path)
	if e != nil || strings.Contains(string(raw), "fixture-secret-password") || !strings.Contains(string(raw), "gate_connection") || !strings.Contains(string(raw), "internal_error") {
		t.Fatal("unsafe or missing failure diagnostic", e)
	}
}
