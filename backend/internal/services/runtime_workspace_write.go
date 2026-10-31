package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"clawreef/internal/models"
	utilexec "k8s.io/client-go/util/exec"
)

// Only constructed by trusted executor code BEFORE StreamWithContext is called.
// Stderr text or an HTTP/transport error must never be interpreted as this proof.
type workspaceExecNotStarted struct{ error }

func (e *workspaceExecNotStarted) Unwrap() error { return e.error }

func (s *runtimeWorkspaceFileService) runWriteJSON(ctx context.Context, scope WorkspaceFileScope, action string, command []string, stdin io.Reader, target any) error {
	var stdout bytes.Buffer
	if err := s.runWrite(ctx, scope, action, command, stdin, &stdout); err != nil {
		return err
	}
	if err := json.Unmarshal(stdout.Bytes(), target); err != nil {
		return fmt.Errorf("failed to parse runtime workspace response: %w", err)
	}
	return nil
}

func (s *runtimeWorkspaceFileService) runWrite(ctx context.Context, scope WorkspaceFileScope, action string, command []string, stdin io.Reader, stdout io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.executor == nil || s.writes == nil {
		return fmt.Errorf("runtime workspace write tracking is not configured")
	}
	if !knownWorkspaceWriteCommand(action, command) {
		return fmt.Errorf("unrecognized runtime workspace write command")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	w := models.WorkspaceWriteIntent{Operation: hex.EncodeToString(id[:]), InstanceID: scope.InstanceID, OwnerID: scope.UserID, Generation: scope.RuntimeGeneration, Action: action}
	if err := s.writes.Begin(ctx, w); err != nil {
		return err
	}
	// A crash or ambiguous Begin acknowledgement leaves an intent, never a silent
	// redispatch. Normal runtime use is not blocked; protected upgrade is blocked.
	var stderr bytes.Buffer
	execErr := s.executor.exec(ctx, scope.UserID, scope.InstanceID, command, stdin, stdout, &stderr)
	var notStarted *workspaceExecNotStarted
	var exited utilexec.ExitError
	// These fixed Python scripts perform synchronous filesystem work and spawn no
	// children. Do NOT reuse this rule for shell commands or arbitrary user tools.
	ended := execErr == nil || errors.As(execErr, &notStarted) || (errors.As(execErr, &exited) && exited.Exited())
	var settleErr error
	if ended {
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.writes.Complete(settleCtx, w); err != nil {
			settleErr = fmt.Errorf("workspace write ended but confirmation could not be persisted (operation %s): %w", w.Operation, err)
		}
	}
	if execErr != nil {
		mapped := mapRuntimeWorkspaceError(execErr, stderr.String())
		if !ended {
			return fmt.Errorf("远端工作区写入结果未确认，已阻止受保护升级（操作 %s）；请先核对文件状态，勿盲目重试: %w", w.Operation, mapped)
		}
		return errors.Join(mapped, settleErr)
	}
	return settleErr
}

// The completion rule is only valid for these synchronous scripts, not a shell
// launcher which could exit while its background child continues writing.
func knownWorkspaceWriteCommand(action string, command []string) bool {
	if len(command) < 3 || command[0] != "python3" || command[1] != "-c" {
		return false
	}
	switch action {
	case "upload":
		return len(command) == 7 && command[2] == runtimeWorkspaceUploadScript
	case "mkdir":
		return len(command) == 5 && command[2] == runtimeWorkspaceMkdirScript
	case "rename":
		return len(command) == 6 && command[2] == runtimeWorkspaceRenameScript
	case "delete":
		return len(command) == 5 && command[2] == runtimeWorkspaceDeleteScript
	}
	return false
}
