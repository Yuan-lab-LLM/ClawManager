package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"clawreef/internal/models"
)

var errOpenCodeBootstrapNotJSON = errors.New("OpenCode bootstrap response is not JSON")
var openCodeSessionIDPattern = regexp.MustCompile(`^ses_[A-Za-z0-9_-]+$`)

type openCodeBootstrapCall struct {
	done      chan struct{}
	sessionID string
	err       error
}

type openCodeInitialSession struct {
	ID       string `json:"id"`
	ParentID string `json:"parentID"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
	Time struct {
		Created  int64 `json:"created"`
		Updated  int64 `json:"updated"`
		Archived int64 `json:"archived"`
	} `json:"time"`
}

func WithOpenCodeLiteSessionBootstrap() InstanceProxyServiceOption {
	return func(s *InstanceProxyService) { s.openCodeBootstrapEnabled = true }
}

// OpenCode owns the durable session record. Using a creation-scoped ID makes
// a lost HTTP response, a control-plane restart, or concurrent replicas reuse
// the same record instead of creating another empty conversation.
func (s *InstanceProxyService) ensureOpenCodeInitialSession(ctx context.Context, instanceID int, upstream *url.URL, password string) (string, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil || instance == nil || instance.WorkspacePath == nil || !path.IsAbs(*instance.WorkspacePath) {
		return "", fmt.Errorf("instance workspace is unavailable")
	}
	if path.Clean(*instance.WorkspacePath) == "/" {
		return "", fmt.Errorf("instance workspace cannot be the filesystem root")
	}
	scope := openCodeBrowserStorageScope(instance)
	s.openCodeBootstrapMu.Lock()
	if s.openCodeBootstraps == nil {
		s.openCodeBootstraps = make(map[string]*openCodeBootstrapCall)
	}
	if pending := s.openCodeBootstraps[scope]; pending != nil {
		s.openCodeBootstrapMu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-pending.done:
			return pending.sessionID, pending.err
		}
	}
	call := &openCodeBootstrapCall{done: make(chan struct{})}
	s.openCodeBootstraps[scope] = call
	s.openCodeBootstrapMu.Unlock()
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	call.sessionID, call.err = s.resolveOpenCodeInitialSession(requestCtx, instance, upstream, password, scope)
	s.openCodeBootstrapMu.Lock()
	delete(s.openCodeBootstraps, scope)
	close(call.done)
	s.openCodeBootstrapMu.Unlock()
	return call.sessionID, call.err
}

func (s *InstanceProxyService) resolveOpenCodeInitialSession(ctx context.Context, instance *models.Instance, upstream *url.URL, password, scope string) (string, error) {
	var info struct {
		Version string `json:"version"`
	}
	status, err := s.openCodeBootstrapAPI(ctx, upstream, password, http.MethodGet, "/api/info", nil, &info)
	if status == http.StatusNotFound || (status == http.StatusOK && errors.Is(err, errOpenCodeBootstrapNotJSON)) {
		return "", nil
	} // v1 still uses its native landing page.
	if err != nil {
		return "", err
	}
	version := strings.TrimPrefix(strings.TrimSpace(info.Version), "opencode v")
	if strings.HasPrefix(version, "1.") {
		return "", nil
	}
	if !strings.HasPrefix(version, "2.") {
		return "", fmt.Errorf("unsupported OpenCode info response")
	}
	root := path.Clean(*instance.WorkspacePath)
	initialID := "ses_" + scope
	var saved struct {
		Data openCodeInitialSession `json:"data"`
	}
	status, err = s.openCodeBootstrapAPI(ctx, upstream, password, http.MethodGet, "/api/session/"+initialID, nil, &saved)
	if status != http.StatusNotFound {
		if err != nil {
			return "", err
		}
		if saved.Data.ID != initialID || !openCodeSessionWithinWorkspace(saved.Data, root) {
			return "", fmt.Errorf("invalid persisted initial session")
		}
		if saved.Data.Time.Archived != 0 {
			return "", nil
		}
		return saved.Data.ID, nil
	}
	// Adopt a real existing conversation before creating the managed starter.
	// Follow pagination so older instances never get another starter merely
	// because their first page consists of archived/child sessions.
	cursor := ""
	seenCursors := make(map[string]bool)
	for {
		var list struct {
			Data   []openCodeInitialSession `json:"data"`
			Cursor struct {
				Next *string `json:"next"`
			} `json:"cursor"`
		}
		query := url.Values{"limit": {"100"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		_, err = s.openCodeBootstrapAPI(ctx, upstream, password, http.MethodGet, "/api/session?"+query.Encode(), nil, &list)
		if err != nil {
			return "", err
		}
		var selected *openCodeInitialSession
		for i := range list.Data {
			candidate := &list.Data[i]
			if candidate.ParentID != "" || candidate.Time.Archived != 0 || !openCodeSessionWithinWorkspace(*candidate, root) {
				continue
			}
			if selected == nil || candidate.Time.Updated > selected.Time.Updated {
				selected = candidate
			}
		}
		if selected != nil {
			return selected.ID, nil
		}
		if list.Cursor.Next == nil || *list.Cursor.Next == "" {
			break
		}
		if seenCursors[*list.Cursor.Next] {
			return "", fmt.Errorf("OpenCode session pagination repeated a cursor")
		}
		cursor = *list.Cursor.Next
		seenCursors[cursor] = true
	}
	payload := map[string]any{
		"id": initialID, "title": "开始使用",
		"location": map[string]string{"directory": path.Join(root, "project")},
		"metadata": map[string]string{"clawmanager.bootstrap": scope},
	}
	_, createErr := s.openCodeBootstrapAPI(ctx, upstream, password, http.MethodPost, "/api/session", payload, &saved)
	if createErr != nil {
		// Another replica, or a previous request whose response was lost, may
		// already have committed the deterministic ID. Never retry with a new ID.
		_, lookupErr := s.openCodeBootstrapAPI(ctx, upstream, password, http.MethodGet, "/api/session/"+initialID, nil, &saved)
		if lookupErr != nil {
			return "", createErr
		}
	}
	if saved.Data.ID != initialID || !openCodeSessionWithinWorkspace(saved.Data, root) {
		return "", fmt.Errorf("invalid initial session response")
	}
	return saved.Data.ID, nil
}

func openCodeSessionWithinWorkspace(session openCodeInitialSession, root string) bool {
	if !openCodeSessionIDPattern.MatchString(session.ID) {
		return false
	}
	if !path.IsAbs(session.Location.Directory) {
		return false
	}
	directory := path.Clean(session.Location.Directory)
	return directory == root || strings.HasPrefix(directory, root+"/")
}

func (s *InstanceProxyService) openCodeBootstrapAPI(ctx context.Context, upstream *url.URL, password, method, target string, payload, out any) (int, error) {
	relative, err := url.Parse(target)
	if err != nil {
		return 0, err
	}
	endpoint := *upstream
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery = relative.Path, "", relative.RawQuery
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return 0, err
	}
	setOpenCodeServerBasicAuthHeaders(req.Header, password)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("OpenCode bootstrap request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return response.StatusCode, fmt.Errorf("OpenCode %s returned HTTP %d", relative.Path, response.StatusCode)
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		return response.StatusCode, errOpenCodeBootstrapNotJSON
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(out); err != nil {
		return response.StatusCode, fmt.Errorf("invalid OpenCode bootstrap JSON: %w", err)
	}
	return response.StatusCode, nil
}

func openCodeSessionBrowserRoute(r *http.Request, sessionID string) string {
	origin := requestScheme(r) + "://" + r.Host
	return "/server/" + base64.RawURLEncoding.EncodeToString([]byte(origin)) + "/session/" + url.PathEscape(sessionID)
}
