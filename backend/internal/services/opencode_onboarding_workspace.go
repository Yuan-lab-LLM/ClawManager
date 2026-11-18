package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"clawreef/internal/models"
)

const (
	openCodeOnboardingProjectDirectoryPath = "/__clawmanager/onboarding/project-directory"
	openCodeOnboardingRequestHeader        = "X-ClawManager-Onboarding"
	openCodeOnboardingMaxRequestBytes      = 4096
	openCodeOnboardingMaxProjectNameRunes  = 128
)

type openCodeOnboardingProjectDirectoryRequest struct {
	Name string `json:"name"`
}

// WithOpenCodeOnboardingWorkspace gives the dedicated OpenCode origin a
// narrowly scoped, audited mkdir capability. It never creates a directory
// during instance startup; the browser must make an explicit user-triggered
// request through the authenticated instance proxy.
func WithOpenCodeOnboardingWorkspace(files WorkspaceFileService, externalAccess InstanceExternalAccessService) InstanceProxyServiceOption {
	return func(s *InstanceProxyService) {
		s.openCodeWorkspace = files
		s.externalAccess = externalAccess
	}
}

func (s *InstanceProxyService) openCodeOnboardingWorkspaceWriteAllowed(ctx context.Context, token *AccessToken) (bool, error) {
	if s == nil || s.openCodeWorkspace == nil || token == nil {
		return false, nil
	}
	binding := strings.TrimSpace(token.SessionBinding)
	if binding == "" || strings.HasPrefix(binding, "iei:") {
		return true, nil
	}
	if s.externalAccess == nil {
		return false, nil
	}
	access, err := s.externalAccess.Get(ctx, token.InstanceID)
	if err != nil {
		return false, err
	}
	if access == nil || !access.Enabled || access.PublicSlug == nil {
		return false, nil
	}
	code := strings.TrimSpace(*access.PublicSlug)
	if code == "" || binding != ExternalAccessSessionBinding(code, access) {
		return false, nil
	}
	workspaceAccess, err := NormalizeExternalWorkspaceAccess(access.WorkspaceAccess)
	if err != nil {
		return false, err
	}
	return workspaceAccess == ExternalWorkspaceAccessWrite, nil
}

func (s *InstanceProxyService) handleOpenCodeOnboardingProjectDirectory(ctx context.Context, w http.ResponseWriter, r *http.Request, token *AccessToken) error {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOpenCodeOnboardingJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return nil
	}
	mediaType, _, mediaTypeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Header.Get(openCodeOnboardingRequestHeader) != "1" || mediaTypeErr != nil || !strings.EqualFold(mediaType, "application/json") {
		writeOpenCodeOnboardingJSON(w, http.StatusForbidden, map[string]any{"error": "invalid onboarding request"})
		return nil
	}
	allowed, err := s.openCodeOnboardingWorkspaceWriteAllowed(ctx, token)
	if err != nil {
		writeOpenCodeOnboardingJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "workspace access is temporarily unavailable"})
		return nil
	}
	if !allowed {
		writeOpenCodeOnboardingJSON(w, http.StatusForbidden, map[string]any{"error": "this link does not allow workspace changes"})
		return nil
	}

	r.Body = http.MaxBytesReader(w, r.Body, openCodeOnboardingMaxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request openCodeOnboardingProjectDirectoryRequest
	if err := decoder.Decode(&request); err != nil {
		writeOpenCodeOnboardingJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid project folder request"})
		return nil
	}
	if err := ensureOpenCodeOnboardingJSONEOF(decoder); err != nil {
		writeOpenCodeOnboardingJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid project folder request"})
		return nil
	}

	name := strings.TrimSpace(request.Name)
	cleanName, err := sanitizeWorkspaceFilename(name)
	if err != nil || cleanName != name || utf8.RuneCountInString(name) > openCodeOnboardingMaxProjectNameRunes || strings.HasPrefix(name, ".") || strings.EqualFold(name, "home") {
		writeOpenCodeOnboardingJSON(w, http.StatusBadRequest, map[string]any{"error": "project folder name is invalid"})
		return nil
	}

	instance, err := s.instanceRepo.GetByID(token.InstanceID)
	if err != nil || instance == nil || instance.WorkspacePath == nil || strings.TrimSpace(*instance.WorkspacePath) == "" {
		writeOpenCodeOnboardingJSON(w, http.StatusNotFound, map[string]any{"error": "workspace is not available"})
		return nil
	}
	if instance.UserID != token.UserID {
		writeOpenCodeOnboardingJSON(w, http.StatusForbidden, map[string]any{"error": "instance access does not match workspace owner"})
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
		writeOpenCodeOnboardingJSON(w, http.StatusConflict, map[string]any{"error": "instance workspace is not running", "code": "INSTANCE_NOT_RUNNING"})
		return nil
	}
	entry, err := s.openCodeWorkspace.Mkdir(ctx, WorkspaceFileScope{
		InstanceID:        instance.ID,
		UserID:            instance.UserID,
		RuntimeGeneration: instance.RuntimeGeneration,
		WorkspacePath:     strings.TrimSpace(*instance.WorkspacePath),
		AuditActionPrefix: "onboarding_",
	}, cleanName)
	if err != nil {
		status, message, code := openCodeOnboardingWorkspaceError(err)
		writeOpenCodeOnboardingJSON(w, status, map[string]any{"error": message, "code": code})
		return nil
	}
	writeOpenCodeOnboardingJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{"name": entry.Name, "path": entry.Path},
	})
	return nil
}

func ensureOpenCodeOnboardingJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func openCodeOnboardingWorkspaceError(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrWorkspaceEntryExists):
		return http.StatusConflict, "project folder already exists", "PROJECT_EXISTS"
	case errors.Is(err, ErrWorkspacePathInvalid),
		errors.Is(err, ErrWorkspacePathEscape),
		errors.Is(err, ErrWorkspaceRootOperation),
		errors.Is(err, ErrWorkspaceFileNameInvalid):
		return http.StatusBadRequest, "project folder name is invalid", "INVALID_PROJECT_NAME"
	case errors.Is(err, models.ErrRuntimeUpgradeConflict):
		return http.StatusConflict, "workspace is temporarily unavailable during an instance update", "WORKSPACE_UPGRADING"
	default:
		return http.StatusInternalServerError, "failed to create project folder", "WORKSPACE_ERROR"
	}
}

func writeOpenCodeOnboardingJSON(w http.ResponseWriter, status int, payload any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
