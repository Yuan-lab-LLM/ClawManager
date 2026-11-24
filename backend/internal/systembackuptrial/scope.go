package systembackuptrial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	domain "clawreef/internal/systembackuptrial/records"

	"github.com/gin-gonic/gin"
)

type backupSelection struct {
	Mode string `json:"mode"`
	domain.TrialScope
	HistorySHA256   string                   `json:"hermes_history_sha256,omitempty"`
	HistorySessions *historySessionSelection `json:"hermes_sessions,omitempty"`
	WorkspaceSHA256 string                   `json:"workspace_selection_sha256,omitempty"`
	WorkspaceDir    *workspaceSelection      `json:"workspace_directory,omitempty"`
}

func (s backupSelection) valid() bool {
	return (s.Mode == "all" && len(s.UserIDs)+len(s.Instances) == 0 || s.Mode == "selected" && s.TrialScope.Valid()) &&
		(s.HistorySHA256 == "" || ciphertextDigest.MatchString(s.HistorySHA256)) &&
		(s.HistorySessions == nil || s.HistorySHA256 == "" && s.HistorySessions.valid()) &&
		(s.WorkspaceSHA256 == "" || ciphertextDigest.MatchString(s.WorkspaceSHA256)) &&
		(s.WorkspaceDir == nil || s.WorkspaceSHA256 == "" && s.WorkspaceDir.valid())
}

func readBackupSelection(c *gin.Context) (*backupSelection, error) {
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var selection backupSelection
	if decoder.Decode(&selection) != nil || decoder.Decode(new(any)) != io.EOF || !selection.valid() {
		return nil, domain.ErrTrialScope
	}
	return &selection, nil
}

type preparedBackupContent struct {
	UserID      int    `json:"user_id"`
	InstanceID  int    `json:"instance_id"`
	SHA256      string `json:"sha256"`
	Sessions    int    `json:"sessions,omitempty"`
	RelativeDir string `json:"relative_dir,omitempty"`
}

type backupScopeOptions struct {
	domain.TrialOptions
	Hermes          *preparedBackupContent `json:"hermes_history,omitempty"`
	HermesSource    *historySourceBinding  `json:"hermes_source,omitempty"`
	Workspace       *preparedBackupContent `json:"workspace_files,omitempty"`
	WorkspaceSource *workspaceSource       `json:"workspace_source,omitempty"`
}

func sourceMaterialSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (s *Service) preparedHistoryOption() *preparedBackupContent {
	raw, err := readSetting(s.value("HERMES_HISTORY_FILE"), maxHermesHistory)
	if err != nil {
		return nil
	}
	defer clear(raw)
	var payload struct {
		Format     string     `json:"format"`
		UserID     int        `json:"user_id"`
		InstanceID int        `json:"instance_id"`
		Sessions   []struct{} `json:"sessions"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Format != hermesHistoryFormat ||
		payload.UserID <= 0 || payload.InstanceID <= 0 || len(payload.Sessions) == 0 {
		return nil
	}
	return &preparedBackupContent{
		UserID: payload.UserID, InstanceID: payload.InstanceID,
		SHA256: sourceMaterialSHA256(raw), Sessions: len(payload.Sessions),
	}
}

func (s *Service) preparedWorkspaceOption() *preparedBackupContent {
	raw, err := readSetting(s.value("WORKSPACE_FILES_SELECTION_FILE"), 4096)
	if err != nil || s.value("WORKSPACE_FILES_SOURCE_ROOT") == "" {
		return nil
	}
	var selection workspaceSelection
	if json.Unmarshal(raw, &selection) != nil || selection.UserID <= 0 || selection.InstanceID <= 0 || !validWorkspaceSelection(selection.RelativeDir) {
		return nil
	}
	return &preparedBackupContent{
		UserID: selection.UserID, InstanceID: selection.InstanceID, SHA256: sourceMaterialSHA256(raw), RelativeDir: selection.RelativeDir,
	}
}

func parseBackupOptionsCursor(values url.Values, key string) (int64, error) {
	input := values[key]
	if len(input) == 0 {
		return 0, nil
	}
	if len(input) != 1 {
		return 0, domain.ErrTrialScope
	}
	value, err := strconv.ParseInt(input[0], 10, 64)
	if err != nil || value < 0 || strconv.FormatInt(value, 10) != input[0] {
		return 0, domain.ErrTrialScope
	}
	return value, nil
}

func (s *Service) handleBackupOptions(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	afterUser, userErr := parseBackupOptionsCursor(query, "after_user")
	afterInstance, instanceErr := parseBackupOptionsCursor(query, "after_instance")
	if err != nil || userErr != nil || instanceErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_backup_scope"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	environment := s.environment(ctx, false)
	if environment.Profile == nil || environment.missing("source") != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "trial_environment_source"})
		return
	}
	source, database, err := s.database("SOURCE")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "source_credential"})
		return
	}
	defer source.Close()
	options, err := domain.ListTrialOptions(ctx, source, database, s.value("SOURCE_UUID"), afterUser, afterInstance)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "source_" + category(err)})
		return
	}
	result := backupScopeOptions{TrialOptions: options}
	if afterUser == 0 && afterInstance == 0 {
		result.Hermes, result.Workspace = s.preparedHistoryOption(), s.preparedWorkspaceOption()
		if configured, err := s.historySourceConfig(); err == nil {
			result.HermesSource = &configured.binding
		}
		if configured, _, err := s.workspaceSourceConfig(environment.Profile); err == nil {
			result.WorkspaceSource = configured
		}
	}
	c.JSON(http.StatusOK, result)
}
