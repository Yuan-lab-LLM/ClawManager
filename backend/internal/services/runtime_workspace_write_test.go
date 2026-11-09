package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"clawreef/internal/models"
	utilexec "k8s.io/client-go/util/exec"
)

type workspaceWriteFake struct {
	intent                models.WorkspaceWriteIntent
	beginErr, completeErr error
	active                bool
	begins, completes     int
	completionContextErr  error
}

func (r *workspaceWriteFake) Begin(_ context.Context, w models.WorkspaceWriteIntent) error {
	r.begins++
	if r.beginErr != nil {
		return r.beginErr
	}
	r.intent, r.active = w, true
	return nil
}
func (r *workspaceWriteFake) Complete(ctx context.Context, w models.WorkspaceWriteIntent) error {
	r.completes++
	r.completionContextErr = ctx.Err()
	if r.completeErr != nil {
		return r.completeErr
	}
	if w != r.intent {
		return errors.New("wrong operation")
	}
	r.active = false
	return nil
}

type workspaceExecFunc func(context.Context, int, int, []string, io.Reader, io.Writer, io.Writer) error

func (f workspaceExecFunc) exec(ctx context.Context, user, instance int, cmd []string, in io.Reader, out, stderr io.Writer) error {
	return f(ctx, user, instance, cmd, in, out, stderr)
}

func TestRuntimeWorkspaceWriteTracksAllMutations(t *testing.T) {
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 8, WorkspacePath: "/config"}
	for _, action := range []string{"upload", "mkdir", "rename", "delete"} {
		t.Run(action, func(t *testing.T) {
			writes := &workspaceWriteFake{}
			s := &runtimeWorkspaceFileService{writes: writes}
			s.executor = workspaceExecFunc(func(_ context.Context, user, instance int, cmd []string, _ io.Reader, out, _ io.Writer) error {
				if !writes.active || !writes.intent.Valid() || writes.intent.Action != action || user != 3 || instance != 12 || writes.intent.Generation != 8 {
					t.Fatalf("exec without identity-bound intent: %+v", writes)
				}
				_, err := io.WriteString(out, `{"name":"file","path":"file","is_dir":false,"size":1,"modified_at":"2026-10-20T00:00:00Z"}`)
				return err
			})
			var err error
			switch action {
			case "upload":
				_, err = s.Upload(t.Context(), scope, "", "file", strings.NewReader("x"), 1)
			case "mkdir":
				_, err = s.Mkdir(t.Context(), scope, "folder")
			case "rename":
				_, err = s.Rename(t.Context(), scope, "file", "new")
			case "delete":
				err = s.Delete(t.Context(), scope, "file")
			}
			if err != nil || writes.active || writes.begins != 1 || writes.completes != 1 {
				t.Fatalf("%+v: %v", writes, err)
			}
		})
	}
}

func TestRuntimeWorkspaceWriteRequiresTrustedCompletion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		stderr string
		settle bool
	}{
		{"success", nil, "", true},
		{"explicit-exit", utilexec.CodeExitError{Err: errors.New("exit"), Code: 1}, "CLAW_WORKSPACE_ERR:exists:exists", true},
		{"wrapped-exit", fmt.Errorf("wrapped: %w", utilexec.CodeExitError{Err: errors.New("exit"), Code: 1}), "", true},
		{"not-dispatched", &workspaceExecNotStarted{errors.New("pod absent")}, "", true},
		{"network", io.ErrUnexpectedEOF, "", false},
		{"cancel", context.Canceled, "", false},
		{"timeout", context.DeadlineExceeded, "", false},
		{"stderr-is-not-proof", io.ErrUnexpectedEOF, "CLAW_WORKSPACE_ERR:exists:exists", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := &workspaceWriteFake{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s := &runtimeWorkspaceFileService{writes: writes, executor: workspaceExecFunc(func(_ context.Context, _, _ int, _ []string, _ io.Reader, _, stderr io.Writer) error {
				cancel() // Receipt cleanup must not inherit a disconnected HTTP request.
				_, _ = io.WriteString(stderr, tc.stderr)
				return tc.err
			})}
			err := s.runWrite(ctx, WorkspaceFileScope{InstanceID: 12, UserID: 3}, "delete", []string{"python3", "-c", runtimeWorkspaceDeleteScript, "/config", "file"}, nil, io.Discard)
			if (err != nil) != (tc.err != nil) {
				t.Fatal(err)
			}
			if writes.active == tc.settle || writes.completionContextErr != nil {
				t.Fatalf("bad cleanup: %+v", writes)
			}
			if !tc.settle && (writes.completes != 0 || !strings.Contains(err.Error(), writes.intent.Operation)) {
				t.Fatal("uncertainty lost", err)
			}
		})
	}
}

func TestRuntimeWorkspaceWriteFailClosedAndPreserveReads(t *testing.T) {
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3}
	called := false
	writes := &workspaceWriteFake{beginErr: errors.New("database unavailable")}
	s := &runtimeWorkspaceFileService{writes: writes, executor: workspaceExecFunc(func(_ context.Context, _, _ int, _ []string, _ io.Reader, out, _ io.Writer) error {
		called = true
		_, _ = io.WriteString(out, `{"entries":[]}`)
		return nil
	})}
	if s.Delete(t.Context(), scope, "file") == nil || called {
		t.Fatal("dispatched without persistence")
	}
	s.writes = nil
	if s.Delete(t.Context(), scope, "file") == nil || called {
		t.Fatal("dispatched without store")
	}
	if _, err := s.List(t.Context(), scope, ""); err != nil || !called {
		t.Fatal("reads require ledger", err)
	}
	called = false
	s.writes = writes
	before := writes.begins
	if _, err := s.Mkdir(t.Context(), scope, "../outside"); err == nil || called || writes.begins != before {
		t.Fatal("invalid input recorded/dispatched")
	}
	writes.beginErr, writes.completeErr = nil, errors.New("completion database unavailable")
	if err := s.Delete(t.Context(), scope, "file"); err == nil || !writes.active {
		t.Fatal("lost unpersisted completion")
	}
}

func TestRuntimeWorkspaceWriteParseFailureAfterExitDoesNotKeepIntent(t *testing.T) {
	writes := &workspaceWriteFake{}
	s := &runtimeWorkspaceFileService{writes: writes, executor: workspaceExecFunc(func(_ context.Context, _, _ int, _ []string, _ io.Reader, out, _ io.Writer) error {
		_, _ = io.WriteString(out, "invalid JSON")
		return nil
	})}
	if _, err := s.Mkdir(t.Context(), WorkspaceFileScope{InstanceID: 12, UserID: 3}, "folder"); err == nil || writes.active || writes.completes != 1 {
		t.Fatal("parse error treated as running process", err)
	}
}

func TestRuntimeWorkspaceWriteRejectsArbitraryCommands(t *testing.T) {
	writes := &workspaceWriteFake{}
	s := &runtimeWorkspaceFileService{writes: writes, executor: workspaceExecFunc(func(context.Context, int, int, []string, io.Reader, io.Writer, io.Writer) error {
		t.Fatal("untrusted command dispatched")
		return nil
	})}
	for _, cmd := range [][]string{nil, {"sh", "-c", "sleep 30 &"}, {"python3", "-c", "import subprocess", "/config", "file"}, {"python3", "-c", runtimeWorkspaceDeleteScript, "/config", "file", "extra"}} {
		if s.runWrite(t.Context(), WorkspaceFileScope{InstanceID: 12, UserID: 3}, "delete", cmd, nil, io.Discard) == nil || writes.begins != 0 {
			t.Fatal("unknown script accepted")
		}
	}
}
