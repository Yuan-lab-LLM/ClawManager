package runtimeauth

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

func TestControlDiagnosticsAreBoundedAndNeverLogDependencySecrets(t *testing.T) {
	ctx := WithDiagnostics(context.Background())
	err := Unavailable(errors.New("secret-dsn-and-token"))
	for i := 0; i < 80; i++ {
		RecordStage(ctx, "instance_lookup", time.Now(), err)
	}
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	LogDiagnostics(ctx, 7, 2, time.Now(), err)
	if strings.Contains(output.String(), "secret") || strings.Count(output.String(), `"name"`) != 24 || !strings.Contains(output.String(), "dependency_unavailable") {
		t.Fatal(output.String())
	}
	if !errors.Is(err, err.(*DependencyFailure).cause) || !IsUnavailable(context.DeadlineExceeded) || IsUnavailable(errors.New("revoked")) {
		t.Fatal("error classification lost")
	}
}
