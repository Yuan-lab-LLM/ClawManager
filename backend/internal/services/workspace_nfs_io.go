package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/ownerrecords"
	"clawreef/internal/repository"
)

// ErrWorkspaceNFSUncertain retains the durable write intent until authoritative
// reconciliation; process exit alone cannot disprove a delayed NFS server RPC.
var ErrWorkspaceNFSUncertain = errors.New("NFS workspace mutation outcome is uncertain")

const workspaceOperationLockHelperCommand = "workspace-operation-lock-helper"

const WorkspaceIOHelperCommand = "workspace-io-helper"
const workspaceNFSHeaderLimit = 8 << 20
const workspaceNFSResponseLimit = 8 << 20

type workspaceNFSRequest struct {
	HistorySnapshot                                json.RawMessage
	HistoryTempPath                                string
	HistoryIdentity                                ownerrecords.Identity
	HistoryRecent                                  bool
	HistoryCutoff                                  []time.Time
	Root                                           string
	Scope                                          WorkspaceFileScope
	Action, Path, Next, Name, Text, Version, Query string
	BrowserHost                                    string
	BrowserEnabled                                 bool
	UID, GID, Limit                                int
	Size, UploadMax                                int64
}
type workspaceNFSResponse struct {
	HistorySnapshot json.RawMessage
	GitData         []byte
	Questions       []ownerrecords.NativeQuestion
	Fingerprint     string
	UploadIdentity  string
	Entries         []WorkspaceEntry
	Entry           *WorkspaceEntry
	Preview         *WorkspacePreview
	Text            *WorkspaceText
	Name, Error     string
	Size            int64
	Owner           int
}
type workspaceNFSEvent struct {
	Kind, Action, Path string
	Bytes              int64
	OK                 bool
}

// ConfigureWorkspaceNFSIO preserves the concrete service so both existing
// lifecycle/upgrade guards remain outside the credential-scoped operation.
func ConfigureWorkspaceNFSIO(files WorkspaceFileService, profile, root string, writes repository.WorkspaceWriteRepository) {
	if s, ok := files.(*workspaceFileService); ok {
		s.nfsProfile = profile
		s.nfsRoot = filepath.Clean(root)
		s.nfsWrites = writes
	}
}

func (s *workspaceFileService) useNFSIO(scope WorkspaceFileScope) bool {
	if s.nfsProfile != "nfs-rootless-v1" || scope.credentialScoped {
		return false
	}
	if scope.TeamID > 0 {
		return true
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(scope.WorkspacePath)), "/")
	if len(parts) < 3 {
		return false
	}
	switch parts[len(parts)-3] {
	case RuntimeTypeOpenClaw, RuntimeTypeOpenCode, RuntimeTypeHermes, RuntimeTypeDeepSeekHarness:
		return true
	}
	return false
}

func validateWorkspaceNFSScope(root string, scope WorkspaceFileScope) (int, int, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" || scope.UserID <= 0 || scope.RuntimeGeneration < 0 || filepath.Clean(scope.WorkspacePath) != scope.WorkspacePath {
		return 0, 0, ErrWorkspacePathInvalid
	}
	if scope.TeamID > 0 {
		if scope.InstanceID != 0 || scope.WorkspacePath != filepath.Join(root, "teams", fmt.Sprintf("user-%d", scope.UserID), fmt.Sprintf("team-%d-shared", scope.TeamID)) {
			return 0, 0, ErrWorkspacePathEscape
		}
		return 10001, teamSharedGID, nil
	}
	uid, gid, ok := runtimeWorkspaceOwner(scope)
	if !ok || uid <= RuntimeLinuxIDBase || uint64(uid) >= 1<<32-1 {
		return 0, 0, ErrWorkspacePathInvalid
	}
	runtimeType := filepath.Base(filepath.Dir(filepath.Dir(scope.WorkspacePath)))
	if scope.WorkspacePath != RuntimeWorkspacePathWithRoot(root, runtimeType, scope.UserID, scope.InstanceID) {
		return 0, 0, ErrWorkspacePathEscape
	}
	return uid, gid, nil
}

func workspaceNFSMutation(action string) string {
	switch action {
	case "upload", "mkdir", "rename", "delete":
		return action
	case "save_text":
		return "edit"
	}
	return ""
}

func (s *workspaceFileService) nfsIO(ctx context.Context, scope WorkspaceFileScope, request workspaceNFSRequest, input io.Reader) (response workspaceNFSResponse, file *os.File, result error) {
	if err := ctx.Err(); err != nil {
		return response, nil, err
	}
	uid, gid, err := validateWorkspaceNFSScope(s.nfsRoot, scope)
	if err != nil {
		return response, nil, err
	}
	request.Root, request.Scope, request.UID, request.GID, request.UploadMax = s.nfsRoot, scope, uid, gid, WorkspaceUploadMaxBytes()
	if request.Action == "save_text" {
		request.Path, err = cleanWorkspaceRelativePath(request.Path)
		if err != nil {
			return response, nil, err
		}
	}
	if request.Action == "upload" {
		if input == nil {
			return response, nil, ErrWorkspaceFileExpected
		}
		if request.Size > request.UploadMax {
			return response, nil, ErrWorkspaceUploadTooLarge
		}
		if request.Size < 0 {
			spool, err := os.CreateTemp("", "cm-workspace-upload-")
			if err != nil {
				return response, nil, err
			}
			defer func() { spool.Close(); os.Remove(spool.Name()) }()
			n, err := io.Copy(spool, io.LimitReader(input, request.UploadMax+1))
			if err != nil {
				return response, nil, err
			}
			if n > request.UploadMax {
				return response, nil, ErrWorkspaceUploadTooLarge
			}
			if _, err = spool.Seek(0, io.SeekStart); err != nil {
				return response, nil, err
			}
			request.Size, input = n, spool
		}
	}
	if scope.TeamID > 0 && (request.Action == "save_text" || request.Action == "upload") {
		preflight := request
		preflight.Action = "text_owner"
		if request.Action == "upload" {
			name, e := sanitizeWorkspaceFilename(request.Name)
			if e != nil {
				return response, nil, e
			}
			dir, e := cleanWorkspaceRelativePath(request.Path)
			if e != nil {
				return response, nil, e
			}
			preflight.Action, preflight.Path = "upload_owner", joinWorkspaceRelative(dir, name)
		}
		owner, _, _, err := runWorkspaceNFSHelper(ctx, preflight, nil, nil)
		if err != nil {
			return response, nil, err
		}
		if owner.Owner != 10001 && (owner.Owner <= RuntimeLinuxIDBase || uint64(owner.Owner) >= 1<<32-1) {
			return response, nil, os.ErrPermission
		}
		request.UID = owner.Owner
		if request.Action == "upload" {
			request.Version = owner.UploadIdentity
		}
	}
	var intents []models.WorkspaceWriteIntent
	mutation := workspaceNFSMutation(request.Action)
	if mutation != "" {
		if s.nfsWrites == nil {
			return response, nil, fmt.Errorf("NFS workspace write tracking is not configured")
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return response, nil, err
		}
		intent := models.WorkspaceWriteIntent{Operation: hex.EncodeToString(nonce[:]), InstanceID: scope.InstanceID, TeamID: scope.TeamID, OwnerID: scope.UserID, Generation: scope.RuntimeGeneration, Action: mutation}
		if err = s.nfsWrites.Begin(ctx, intent); err != nil {
			return response, nil, err
		}
		intents = append(intents, intent)
	}
	audit := func(event workspaceNFSEvent) error {
		if !validNFSAudit(request, event) {
			return fmt.Errorf("invalid NFS workspace audit receipt")
		}
		return s.recordAudit(ctx, scope, event.Action, event.Path, event.Bytes)
	}
	response, file, ended, result := runWorkspaceNFSHelper(ctx, request, input, audit)
	if ended && !errors.Is(result, ErrWorkspaceNFSUncertain) && len(intents) > 0 {
		result = errors.Join(result, s.settleNFSIntents(ctx, intents))
	}
	if result != nil && file != nil {
		file.Close()
		file = nil
	}
	return response, file, result
}

func (s *workspaceFileService) settleNFSIntents(ctx context.Context, intents []models.WorkspaceWriteIntent) error {
	settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var errs []error
	for _, intent := range intents {
		if err := s.nfsWrites.Complete(settle, intent); err != nil {
			errs = append(errs, fmt.Errorf("workspace write completion unconfirmed (%s): %w", intent.Operation, err))
		}
	}
	return errors.Join(errs...)
}

func validNFSAudit(request workspaceNFSRequest, event workspaceNFSEvent) bool {
	if event.Kind != "audit" || event.Bytes < 0 {
		return false
	}
	name, err := cleanWorkspaceRelativePath(request.Path)
	if err != nil {
		return false
	}
	expectedAction := request.Action
	switch request.Action {
	case "upload":
		filename, err := sanitizeWorkspaceFilename(request.Name)
		if err != nil {
			return false
		}
		name = joinWorkspaceRelative(name, filename)
		if event.Bytes != request.Size {
			return false
		}
	case "rename":
		next, err := cleanWorkspaceRelativePath(request.Next)
		if err != nil {
			return false
		}
		name += " -> " + next
	case "save_text":
		expectedAction = "edit_attempt"
		if event.Bytes != int64(len(request.Text)) {
			return false
		}
	case "download":
		if event.Action == "input_file" && strings.HasPrefix(strings.TrimSpace(request.Scope.AuditActionPrefix), "llm_") {
			expectedAction = "input_file"
		}
	case "mkdir", "delete":
	default:
		return false
	}
	return event.Action == expectedAction && event.Path == name
}

var workspaceNFSErrors = []error{ErrWorkspaceDirectoryExpected, ErrWorkspaceFileExpected, ErrWorkspacePreviewTooLarge, ErrWorkspaceUploadTooLarge, ErrWorkspaceRootOperation, ErrWorkspaceFileNameInvalid, ErrWorkspaceEntryExists, ErrWorkspacePathInvalid, ErrWorkspacePathEscape, ErrWorkspacePathNotFound, ErrWorkspaceTextConflict, ErrWorkspaceTextUnsupported, os.ErrPermission, os.ErrNotExist, os.ErrExist, io.ErrUnexpectedEOF, ErrWorkspaceUpgradeBusy}

func workspaceNFSErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if workspaceNFSTransportUncertain(err) {
		return "uncertain"
	}
	for i, e := range workspaceNFSErrors {
		if errors.Is(err, e) {
			return fmt.Sprint(i + 1)
		}
	}
	return "failed"
}
func workspaceNFSError(code string) error {
	if code == "" {
		return nil
	}
	if code == "uncertain" {
		return ErrWorkspaceNFSUncertain
	}
	for i, e := range workspaceNFSErrors {
		if code == fmt.Sprint(i+1) {
			return e
		}
	}
	return fmt.Errorf("credential-scoped workspace operation failed")
}

type exactWorkspaceUpload struct {
	reader    io.Reader
	remaining int64
}

func (r *exactWorkspaceUpload) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n != 0 {
			return 0, fmt.Errorf("workspace upload size mismatch")
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	if err == io.EOF && r.remaining != 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
