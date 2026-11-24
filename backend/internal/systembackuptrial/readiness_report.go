package systembackuptrial

import (
	"context"
	"crypto/x509"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type readinessCheck struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Reference   string `json:"reference,omitempty"`
	Fingerprint string `json:"sha256,omitempty"`
}

type readinessDetails struct {
	Side   string           `json:"side"`
	Checks []readinessCheck `json:"checks"`
}

func newReadinessDetails(side string) *readinessDetails {
	ids := []string{"preparation", "database_ca", "source_credential", "source_read", "encryption_key", "source_storage_mounts", "source_storage_roundtrip", "source_hermes", "source_workspace"}
	if side == "target" {
		ids = []string{"preparation", "database_ca", "target_isolation", "target_placement", "target_writer_credential", "target_reader_credential",
			"target_writer", "target_writer_grants", "target_reader", "target_accounts", "target_schema", "encryption_key", "target_archive", "target_importer", "target_hermes", "target_workspace"}
	}
	details := &readinessDetails{Side: side, Checks: make([]readinessCheck, len(ids))}
	for i, id := range ids {
		details.Checks[i] = readinessCheck{ID: id, State: "skipped"}
	}
	return details
}

func (d *readinessDetails) set(id, state, reason, reference, fingerprint string) {
	if d == nil {
		return
	}
	for i, check := range d.Checks {
		if check.ID == id {
			d.Checks[i] = readinessCheck{ID: id, State: state, Reason: reason, Reference: reference, Fingerprint: fingerprint}
			return
		}
	}
}

func (d *readinessDetails) result(id, reason string) {
	if reason != "" {
		d.set(id, "failed", reason, "", "")
		return
	}
	d.set(id, "passed", "", "", "")
}

func (d *readinessDetails) key(key []byte, err error) {
	if d == nil {
		return
	}
	if err != nil {
		d.result("encryption_key", "encryption_key")
		return
	}
	d.set("encryption_key", "passed", "", "", sourceMaterialSHA256(key))
}

func (s *Service) addReadinessDetails(ctx context.Context, profile *trialEnvironment, d *readinessDetails) {
	// Optional checks have their own short budget. Their observations do not
	// change admission for legacy backups, restores or manually imported files.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	file := strings.TrimSpace(s.env("SSL_CERT_FILE"))
	if file == "" {
		d.set("database_ca", "not_configured", "database_system_trust", "", "")
	} else {
		pem, err := readSetting(file, 1<<20)
		if err != nil || !x509.NewCertPool().AppendCertsFromPEM(pem) {
			d.result("database_ca", "database_ca_invalid")
		} else {
			d.set("database_ca", "passed", "", filepath.Base(file), sourceMaterialSHA256(pem))
		}
	}
	if profile.Side == "source" {
		s.sourceReadinessDetails(ctx, profile, d)
		return
	}
	s.targetReadinessDetails(ctx, profile, d)
}

func (s *Service) sourceReadinessDetails(ctx context.Context, profile *trialEnvironment, d *readinessDetails) {
	if profile.Source.WorkspaceFiles == nil {
		d.set("source_workspace", "not_configured", "", "", "")
	} else {
		_, mountPath, err := s.workspaceSourceConfig(profile)
		if err == nil {
			mount, openErr := openRegisteredWorkspace(mountPath)
			err = openErr
			if mount != nil {
				mount.Close()
			}
		}
		if err != nil {
			d.result("source_workspace", "workspace_files")
		} else {
			d.result("source_workspace", "")
		}
	}
	if profile.Source.HermesHistory == nil {
		d.set("source_hermes", "not_configured", "", "", "")
		return
	}
	collector, err := s.historySourceConfig()
	if err == nil {
		_, err = collector.inventory(ctx, "")
	}
	if err != nil {
		d.result("source_hermes", "hermes_history")
	} else {
		d.result("source_hermes", "")
	}
}

func (s *Service) targetReadinessDetails(ctx context.Context, profile *trialEnvironment, d *readinessDetails) {
	switch {
	case s.value("IMPORT_SOCKET") == "":
		d.set("target_importer", "not_configured", "", "", "")
	case s.checkImportWrite(ctx):
		d.result("target_importer", "")
	default:
		d.result("target_importer", "trial_artifact_import_unavailable")
	}
	if profile.Target.WorkspaceClaim == "" {
		d.set("target_workspace", "not_configured", "", "", "")
	} else {
		mount, err := s.openWorkspaceTarget()
		if err == nil {
			err = checkWorkspacePublish(mount)
			mount.Close()
		}
		if err != nil {
			d.result("target_workspace", "workspace_files")
		} else {
			d.result("target_workspace", "")
		}
	}
	if profile.Source.HermesHistory == nil && s.value("HERMES_HISTORY_SOCKET") == "" {
		d.set("target_hermes", "not_configured", "", "", "")
		return
	}
	target, err := s.historyTarget()
	if err == nil {
		err = target.ready(ctx)
	}
	if err != nil {
		d.result("target_hermes", "hermes_history")
	} else {
		d.result("target_hermes", "")
	}
}

func (s *Service) checkImportWrite(ctx context.Context) bool {
	if !s.importSocketReady() {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://trial-import/healthz", nil)
	if err != nil {
		return false
	}
	response, err := privateSocketClient(s.importSocket()).Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusNoContent
}
