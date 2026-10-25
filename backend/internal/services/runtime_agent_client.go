package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrRuntimeAgentConflict    = errors.New("runtime agent conflict")
	ErrRuntimeAgentNotFound    = errors.New("runtime agent resource not found")
	ErrRuntimeAgentUnsupported = errors.New("runtime agent operation unsupported")
	ErrRuntimeUpgradeBusy      = errors.New("runtime upgrade operation is still in flight")
)

type RuntimeAgentClient interface {
	Health(ctx context.Context, endpoint string) error
	CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error)
	DeleteGateway(ctx context.Context, endpoint, gatewayID string) error
	Drain(ctx context.Context, endpoint string) error
	ResyncInstanceSkills(ctx context.Context, endpoint string, instanceID int, mode string) error
}

type RuntimeUpgradeAgentClient interface {
	GatewayState(ctx context.Context, endpoint, gatewayID string) (*RuntimeAgentGatewayState, error)
	AcquireWriterLease(ctx context.Context, endpoint string, req RuntimeAgentWriterLeaseRequest) error
	ReleaseWriterLease(ctx context.Context, endpoint string, req RuntimeAgentWriterLeaseRequest) error
	PreflightWorkspace(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentWorkspacePreflight, error)
	CreateWorkspaceSnapshot(ctx context.Context, endpoint string, req RuntimeAgentSnapshotRequest) (*RuntimeAgentWorkspaceSnapshot, error)
	VerifyWorkspaceSnapshot(ctx context.Context, endpoint string, req RuntimeAgentSnapshotVerifyRequest) error
	RestoreWorkspaceSnapshot(ctx context.Context, endpoint string, req RuntimeAgentRestoreRequest) (*RuntimeAgentRestoreResponse, error)
	MigrateSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error)
	SessionSQLiteMigrationStatus(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error)
	PreflightUpgradeCompatibility(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentUpgradeCompatibility, error)
	RestoreSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error)
	SessionSQLiteRestoreStatus(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error)
	ActivateUpgrade(ctx context.Context, endpoint, rolloutID string) error
}

type RuntimeAgentWorkspaceRequest struct {
	ManagedPluginPlanSHA256 string `json:"managed_plugin_plan_sha256,omitempty"`
	RolloutID               string `json:"rollout_id"`
	SnapshotID              string `json:"snapshot_id,omitempty"`
	UserID                  int    `json:"user_id"`
	InstanceID              int    `json:"instance_id"`
	Generation              int    `json:"generation"`
	LeaseToken              string `json:"lease_token,omitempty"`
	OfficialDBCheck         bool   `json:"official_database_check,omitempty"`
	PreserveSessionSQLite   bool   `json:"preserve_session_sqlite,omitempty"`
	UID                     int    `json:"uid,omitempty"`
	GID                     int    `json:"gid,omitempty"`
	RecoveryMode            string `json:"recovery_mode,omitempty"`
	ResumeRolloutID         string `json:"resume_rollout_id,omitempty"`
	ResumeGeneration        int    `json:"resume_generation,omitempty"`
}

type RuntimeAgentWriterLeaseRequest struct {
	RuntimeAgentWorkspaceRequest
	Token      string `json:"token"`
	TTLSeconds int    `json:"ttl_seconds"`
}

type RuntimeAgentWorkspacePreflight struct {
	WorkspacePath  string `json:"workspace_path"`
	FileCount      int64  `json:"file_count"`
	DirectoryCount int64  `json:"directory_count"`
	SymlinkCount   int64  `json:"symlink_count"`
	TotalBytes     int64  `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	DatabaseFiles  []struct {
		RelativePath string `json:"relative_path"`
		SQLiteHeader bool   `json:"sqlite_header"`
		OfficialOK   bool   `json:"official_ok"`
	} `json:"database_files"`
}

type RuntimeAgentSnapshotRequest struct {
	RuntimeAgentWorkspaceRequest
}

type RuntimeAgentWorkspaceSnapshot struct {
	SnapshotID    string `json:"snapshot_id"`
	ArchivePath   string `json:"archive_path"`
	ArchiveSHA256 string `json:"archive_sha256"`
	ArchiveBytes  int64  `json:"archive_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
}

type RuntimeAgentSnapshotVerifyRequest struct {
	RuntimeAgentWorkspaceRequest
}

type RuntimeAgentRestoreRequest struct {
	RuntimeAgentSnapshotVerifyRequest
}

type RuntimeAgentRestoreResponse struct {
	WorkspacePath  string `json:"workspace_path"`
	PreservedPath  string `json:"preserved_path"`
	SnapshotSHA256 string `json:"snapshot_sha256"`
}

type RuntimeAgentSessionSQLiteMigration struct {
	InstanceID           int              `json:"instance_id"`
	Status               string           `json:"status"`
	OutputSHA256         string           `json:"output_sha256"`
	ArchiveBytes         int64            `json:"archive_bytes"`
	ArchiveFiles         int64            `json:"archive_files"`
	RollbackAvailable    bool             `json:"rollback_available"`
	ConfigOriginalSHA256 string           `json:"config_original_sha256,omitempty"`
	ConfigTargetSHA256   string           `json:"config_target_sha256,omitempty"`
	StateCapsuleBytes    int64            `json:"state_capsule_bytes,omitempty"`
	SessionCount         int              `json:"session_count,omitempty"`
	SessionCatalogSHA256 string           `json:"session_catalog_sha256,omitempty"`
	PhaseDurationsMS     map[string]int64 `json:"phase_durations_ms,omitempty"`
	CompletedAt          time.Time        `json:"completed_at"`
	DataPreserved        bool             `json:"data_preserved,omitempty"`
	PreservationSHA256   string           `json:"preservation_sha256,omitempty"`
}

type RuntimeAgentUpgradeCompatibility struct {
	ManagedPluginPlanSHA256 string    `json:"managed_plugin_plan_sha256,omitempty"`
	InstanceID              int       `json:"instance_id"`
	Status                  string    `json:"status"`
	ConfigOriginalSHA256    string    `json:"config_original_sha256"`
	ConfigTargetSHA256      string    `json:"config_target_sha256"`
	ConfigBytes             int64     `json:"config_bytes"`
	SessionBytes            int64     `json:"session_bytes"`
	StateBytes              int64     `json:"state_bytes"`
	AvailableBytes          uint64    `json:"available_bytes"`
	ConfigValidated         bool      `json:"config_validated"`
	DoctorValidated         bool      `json:"doctor_validated"`
	SessionDryRunValid      bool      `json:"session_dry_run_valid"`
	ProbeOutputSHA256       string    `json:"probe_output_sha256"`
	CheckedAt               time.Time `json:"checked_at"`
}

type RuntimeAgentSessionSQLiteRestore struct {
	InstanceID             int              `json:"instance_id"`
	Status                 string           `json:"status"`
	OutputSHA256           string           `json:"output_sha256"`
	ConfigRestored         bool             `json:"config_restored"`
	StateRestored          bool             `json:"state_restored"`
	PreservedSessionSQLite bool             `json:"preserved_session_sqlite"`
	PhaseDurationsMS       map[string]int64 `json:"phase_durations_ms,omitempty"`
	CompletedAt            time.Time        `json:"completed_at"`
}

type RuntimeAgentGatewayState struct {
	GatewayID  string `json:"gateway_id"`
	InstanceID int    `json:"instance_id"`
	Generation int    `json:"generation"`
	State      string `json:"state"`
}

type RuntimeAgentPortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type RuntimeAgentCreateGatewayRequest struct {
	// GatewayPort is the exact primary port allocated by ClawManager. Runtime
	// agents use PortRange only for backwards-compatible callers that have not
	// yet been upgraded to control-plane port assignment.
	GatewayPort   int    `json:"gateway_port,omitempty"`
	InstanceID    int    `json:"instance_id"`
	UserID        int    `json:"user_id"`
	AgentType     string `json:"agent_type"`
	WorkspacePath string `json:"workspace_path"`
	// ProjectRelativePath lets compatible runtime images start their UI in a
	// project below WorkspacePath. Older agents safely ignore this optional field.
	ProjectRelativePath string                `json:"project_relative_path,omitempty"`
	PortRange           RuntimeAgentPortRange `json:"port_range"`
	UID                 int                   `json:"uid"`
	GID                 int                   `json:"gid"`
	CPUCores            float64               `json:"cpu_cores"`
	MemoryMB            int                   `json:"memory_mb"`
	DiskQuotaMB         int                   `json:"disk_quota_mb"`
	Generation          int                   `json:"generation"`
	UpgradeID           string                `json:"upgrade_id,omitempty"`
	Environment         map[string]string     `json:"environment,omitempty"`
}

type RuntimeAgentCreateGatewayResponse struct {
	GatewayID string `json:"gateway_id"`
	Port      int    `json:"port"`
	PID       *int   `json:"pid,omitempty"`
	Status    string `json:"status"`
}

type runtimeAgentHTTPClient struct {
	controlToken      string
	httpClient        *http.Client
	upgradeHTTPClient *http.Client
}

const (
	runtimeAgentControlTimeout = 30 * time.Second
	runtimeAgentUpgradeTimeout = 16 * time.Minute
)

func NewRuntimeAgentClient(controlToken string) RuntimeAgentClient {
	return NewRuntimeAgentClientWithHTTPClient(controlToken, nil)
}

func NewRuntimeAgentClientWithHTTPClient(controlToken string, httpClient *http.Client) RuntimeAgentClient {
	usingDefaults := httpClient == nil
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: runtimeAgentControlTimeout,
		}
	}
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	upgradeClientCopy := clientCopy
	if usingDefaults {
		upgradeClientCopy.Timeout = runtimeAgentUpgradeTimeout
	}
	return &runtimeAgentHTTPClient{
		controlToken:      controlToken,
		httpClient:        &clientCopy,
		upgradeHTTPClient: &upgradeClientCopy,
	}
}

func (c *runtimeAgentHTTPClient) Health(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodGet, endpoint, "/v1/health", nil, nil)
}

func (c *runtimeAgentHTTPClient) CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	var resp RuntimeAgentCreateGatewayResponse
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/gateways", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *runtimeAgentHTTPClient) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	return c.do(ctx, http.MethodDelete, endpoint, "/v1/gateways/"+url.PathEscape(gatewayID), nil, nil)
}

func (c *runtimeAgentHTTPClient) Drain(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/drain", map[string]bool{"draining": true}, nil)
}

func (c *runtimeAgentHTTPClient) Undrain(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/drain", map[string]bool{"draining": false}, nil)
}

func (c *runtimeAgentHTTPClient) ResyncInstanceSkills(ctx context.Context, endpoint string, instanceID int, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "full"
	}
	body := map[string]any{
		"instance_id": instanceID,
		"mode":        mode,
		"trigger":     "manual",
	}
	return c.do(ctx, http.MethodPost, endpoint, "/v1/skills/resync", body, nil)
}

func (c *runtimeAgentHTTPClient) GatewayState(ctx context.Context, endpoint, gatewayID string) (*RuntimeAgentGatewayState, error) {
	var response RuntimeAgentGatewayState
	if err := c.do(ctx, http.MethodGet, endpoint, "/v1/gateways/"+url.PathEscape(gatewayID), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) AcquireWriterLease(ctx context.Context, endpoint string, req RuntimeAgentWriterLeaseRequest) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/writer-leases/acquire", req, nil)
}

func (c *runtimeAgentHTTPClient) ReleaseWriterLease(ctx context.Context, endpoint string, req RuntimeAgentWriterLeaseRequest) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/writer-leases/release", req, nil)
}

func (c *runtimeAgentHTTPClient) PreflightWorkspace(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentWorkspacePreflight, error) {
	var response RuntimeAgentWorkspacePreflight
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/preflight", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) CreateWorkspaceSnapshot(ctx context.Context, endpoint string, req RuntimeAgentSnapshotRequest) (*RuntimeAgentWorkspaceSnapshot, error) {
	var response RuntimeAgentWorkspaceSnapshot
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/snapshots", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) VerifyWorkspaceSnapshot(ctx context.Context, endpoint string, req RuntimeAgentSnapshotVerifyRequest) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/snapshots/verify", req, nil)
}

func (c *runtimeAgentHTTPClient) RestoreWorkspaceSnapshot(ctx context.Context, endpoint string, req RuntimeAgentRestoreRequest) (*RuntimeAgentRestoreResponse, error) {
	var response RuntimeAgentRestoreResponse
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/restore", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) MigrateSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	var response RuntimeAgentSessionSQLiteMigration
	if err := c.doUpgrade(ctx, http.MethodPost, endpoint, "/v1/openclaw/session-sqlite/migrate", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) SessionSQLiteMigrationStatus(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	var response RuntimeAgentSessionSQLiteMigration
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/session-sqlite/migrate/status", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) PreflightUpgradeCompatibility(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentUpgradeCompatibility, error) {
	var response RuntimeAgentUpgradeCompatibility
	if err := c.doUpgrade(ctx, http.MethodPost, endpoint, "/v1/openclaw/upgrade/preflight", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) RestoreSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error) {
	var response RuntimeAgentSessionSQLiteRestore
	if err := c.doUpgrade(ctx, http.MethodPost, endpoint, "/v1/openclaw/session-sqlite/restore", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) SessionSQLiteRestoreStatus(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error) {
	var response RuntimeAgentSessionSQLiteRestore
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/session-sqlite/restore/status", req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *runtimeAgentHTTPClient) ActivateUpgrade(ctx context.Context, endpoint, rolloutID string) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/upgrade/activate", map[string]string{"rollout_id": rolloutID}, nil)
}

func (c *runtimeAgentHTTPClient) do(ctx context.Context, method, endpoint, path string, body any, out any) error {
	return c.doWithClient(ctx, c.httpClient, method, endpoint, path, body, out)
}

func (c *runtimeAgentHTTPClient) doUpgrade(ctx context.Context, method, endpoint, path string, body any, out any) error {
	return c.doWithClient(ctx, c.upgradeHTTPClient, method, endpoint, path, body, out)
}

func (c *runtimeAgentHTTPClient) doWithClient(ctx context.Context, httpClient *http.Client, method, endpoint, path string, body any, out any) error {
	endpoint = strings.TrimRight(endpoint, "/")
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-ClawManager-Control-Token", c.controlToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
			return fmt.Errorf("%w: runtime agent status %d: %s", ErrRuntimeAgentUnsupported, resp.StatusCode, string(msg))
		}
		if resp.StatusCode == http.StatusConflict {
			return fmt.Errorf("%w: %s", ErrRuntimeAgentConflict, string(msg))
		}
		if resp.StatusCode == http.StatusLocked {
			return fmt.Errorf("%w: %s", ErrRuntimeUpgradeBusy, string(msg))
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrRuntimeAgentNotFound, string(msg))
		}
		return fmt.Errorf("runtime agent status %d: %s", resp.StatusCode, string(msg))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
