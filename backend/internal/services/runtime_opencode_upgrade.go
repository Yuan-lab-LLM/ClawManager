package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const RuntimeUpgradeStrategyOpenCodePreserve = "opencode_native_preserve_v1"
const openCodeNativeVersion = "1.18.13"
const openCodeNativeRevision = "a105350812f05f914c768e468559dbd6bd508d8e"
const openCodeNativeBinarySHA256 = "bcebc898d22419417b8dbec0f5e3f00f5b67b5a38c1de37c77c699d0e31fe0dd"
const openCodeNativeJournalSHA256 = "2ef42ffb09410c0f91535d25fce5b4107c46ee3fd7a4725e521c197ad4f5949c"
const openCodeNativeProtocol = "opencode-preserve-v1"
const openCodeLegacyImage = "clawmanager.local/opencode-lite@sha256:7ba6888e43480496704a10d9a496b1398863515166acab5c544ccc9c9d91ed31"

var openCodeUpgradeCapabilities = []string{"opencode.native-preserve-v1", "opencode.workspace.writer-lease", "opencode.session-continuity-v1", "opencode.runtime-standby-v1", "opencode.native-payload-poduid-v1"}

type openCodeNativeContract struct {
	Version       string `json:"version"`
	Revision      string `json:"revision"`
	BinarySHA256  string `json:"binary_sha256"`
	JournalSHA256 string `json:"journal_sha256"`
	BaseImage     string `json:"base_image"`
}

func validOpenCodeSHA(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == 32 && value == hex.EncodeToString(b)
}
func validateOpenCodeNativeContract(labels, env map[string]string) (openCodeNativeContract, error) {
	c := openCodeNativeContract{Version: labels["io.clawmanager.opencode.version"], Revision: labels["io.clawmanager.opencode.revision"], BinarySHA256: labels["io.clawmanager.opencode.binary-sha256"], JournalSHA256: labels["io.clawmanager.opencode.journal-sha256"], BaseImage: labels["io.clawmanager.opencode.base-image"]}
	if labels["io.clawmanager.runtime.type"] != "opencode" || labels["io.clawmanager.upgrade.strategy"] != "opencode-native-preserve-v1" || labels["io.clawmanager.upgrade.protocol"] != openCodeNativeProtocol || c.Version != openCodeNativeVersion || c.Revision != openCodeNativeRevision || c.BinarySHA256 != openCodeNativeBinarySHA256 || c.JournalSHA256 != openCodeNativeJournalSHA256 || !IsImmutableRuntimeImage(c.BaseImage) {
		return c, fmt.Errorf("OpenCode target lacks the pinned 1.18.13 native preservation contract")
	}
	if err := validateOpenCodeNativeEnvironment(env); err != nil {
		return c, err
	}
	return c, nil
}
func validateOpenCodeNativeEnvironment(env map[string]string) error {
	for _, key := range []string{"OPENCODE_DB", "OPENCODE_CHANNEL", "OPENCODE_DATA_DIR", "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		if strings.TrimSpace(env[key]) != "" {
			return fmt.Errorf("OpenCode native preservation does not support %s override", key)
		}
	}
	return nil
}
func validateOpenCodeCapsule(id int, r *RuntimeAgentSessionSQLiteMigration) error {
	if r == nil || r.InstanceID != id || r.Status != "validated" || !validOpenCodeSHA(r.OutputSHA256) || !validOpenCodeSHA(r.PreservationSHA256) || !validOpenCodeSHA(r.SessionCatalogSHA256) || !r.DataPreserved || !r.RollbackAvailable {
		return fmt.Errorf("OpenCode native capsule lacks complete data/session preservation and rollback proofs")
	}
	return nil
}
func validateOpenCodeCompatibility(id int, r *RuntimeAgentUpgradeCompatibility) error {
	if r == nil || r.InstanceID != id || r.Status != "compatible" || !r.ConfigValidated || !r.DoctorValidated || !r.SessionDryRunValid || !validOpenCodeSHA(r.ConfigOriginalSHA256) || !validOpenCodeSHA(r.ConfigTargetSHA256) || !validOpenCodeSHA(r.ProbeOutputSHA256) {
		return fmt.Errorf("OpenCode native compatibility proof is incomplete")
	}
	return nil
}

// Inspect only the caller's immutable digest on the current registry. An OCI
// label is an admission claim; the target maintenance helper independently
// checks its real native binary and exact journal before touching user state.
func inspectOpenCodeRegistryContract(ctx context.Context, target string, sources map[string]string) (openCodeNativeContract, error) {
	var empty openCodeNativeContract
	if !IsImmutableRuntimeImage(target) {
		return empty, fmt.Errorf("OpenCode target requires an immutable repository@sha256 digest")
	}
	host, _, _, err := splitRegistryImage(target)
	if err != nil {
		return empty, err
	}
	allowed := false
	for _, source := range sources {
		h, _, _, e := splitRegistryImage(source)
		if e == nil && h == host {
			allowed = true
		}
	}
	if !allowed {
		return empty, fmt.Errorf("OpenCode target registry differs from the frozen source registry")
	}
	manifest, cfg, err := loadOpenCodeRegistryImage(ctx, target)
	if err != nil {
		return empty, err
	}
	env := map[string]string{}
	for _, v := range cfg.Config.Env {
		k, value, ok := strings.Cut(v, "=")
		if ok {
			env[k] = value
		}
	}
	contract, err := validateOpenCodeNativeContract(cfg.Config.Labels, env)
	if err != nil {
		return empty, err
	}
	if contract.BaseImage != openCodeLegacyImage {
		return empty, fmt.Errorf("OpenCode target native base is not the audited legacy baseline")
	}
	baseline, baselineConfig, err := loadOpenCodeRegistryImage(ctx, contract.BaseImage)
	if err != nil {
		return empty, fmt.Errorf("verify OpenCode audited base OCI: %w", err)
	}
	if len(baseline.Layers) == 0 || len(manifest.Layers) < len(baseline.Layers) || baselineConfig.RootFS.Type != "layers" || cfg.RootFS.Type != "layers" || len(baselineConfig.RootFS.DiffIDs) == 0 || len(cfg.RootFS.DiffIDs) < len(baselineConfig.RootFS.DiffIDs) {
		return empty, fmt.Errorf("OpenCode target does not retain the audited base layer chain")
	}
	for i, layer := range baseline.Layers {
		if imageDigestFromReference("image@"+layer.Digest) == "" || manifest.Layers[i].Digest != layer.Digest {
			return empty, fmt.Errorf("OpenCode target base manifest layer chain changed")
		}
	}
	for i, diffID := range baselineConfig.RootFS.DiffIDs {
		if imageDigestFromReference("image@"+diffID) == "" || cfg.RootFS.DiffIDs[i] != diffID {
			return empty, fmt.Errorf("OpenCode target base rootfs identity changed")
		}
	}

	for _, source := range sources {
		if source == contract.BaseImage {
			continue
		}
		current, err := inspectOpenCodeRegistryContract(ctx, source, map[string]string{"baseline": contract.BaseImage})
		if err != nil {
			return empty, fmt.Errorf("OpenCode source native contract is unknown: %w", err)
		}
		if current != contract {
			return empty, fmt.Errorf("cross-native-version/binary/journal OpenCode migration is unsupported")
		}
	}
	return contract, nil
}

// Never trust a registry's digest header in place of hashing the bounded bytes.
func fetchOpenCodeRegistryJSON(ctx context.Context, client *http.Client, address, want string, limit int64, out any) error {
	if imageDigestFromReference("image@"+want) == "" {
		return fmt.Errorf("OpenCode OCI descriptor digest is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenCode registry returned %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return fmt.Errorf("OpenCode OCI object exceeds the bounded limit")
	}
	sum := sha256.Sum256(raw)
	actual := "sha256:" + hex.EncodeToString(sum[:])
	header := strings.TrimSpace(resp.Header.Get("Docker-Content-Digest"))
	if actual != want || header != "" && header != actual {
		return fmt.Errorf("OpenCode OCI body/header digest mismatch")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode OpenCode OCI object: %w", err)
	}
	return nil
}

type openCodeOCIManifest struct {
	registryManifestDocument
	Layers []registryDescriptor `json:"layers"`
}
type openCodeOCIConfig struct {
	registryImageConfig
	RootFS struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
}

func loadOpenCodeRegistryImage(ctx context.Context, ref string) (openCodeOCIManifest, openCodeOCIConfig, error) {
	var m openCodeOCIManifest
	var cfg openCodeOCIConfig
	host, repo, _, err := splitRegistryImage(ref)
	if err != nil {
		return m, cfg, err
	}
	client, base, err := safeRegistryClient(host, repo)
	if err != nil {
		return m, cfg, err
	}
	if err = fetchOpenCodeRegistryJSON(ctx, client, base+"/manifests/"+url.PathEscape(imageDigestFromReference(ref)), imageDigestFromReference(ref), 8<<20, &m); err != nil {
		return m, cfg, err
	}
	if len(m.Manifests) > 0 {
		selected := ""
		for _, entry := range m.Manifests {
			if entry.Platform != nil && entry.Platform.OS == "linux" && entry.Platform.Architecture == "amd64" {
				if selected != "" {
					return m, cfg, fmt.Errorf("OpenCode OCI linux/amd64 descriptor is ambiguous")
				}
				selected = entry.Digest
			}
		}
		if imageDigestFromReference("image@"+selected) == "" {
			return m, cfg, fmt.Errorf("OpenCode OCI linux/amd64 digest is missing")
		}
		m = openCodeOCIManifest{}
		if err = fetchOpenCodeRegistryJSON(ctx, client, base+"/manifests/"+url.PathEscape(selected), selected, 8<<20, &m); err != nil {
			return m, cfg, err
		}
	}
	if err = fetchOpenCodeRegistryJSON(ctx, client, base+"/blobs/"+url.PathEscape(m.Config.Digest), m.Config.Digest, 2<<20, &cfg); err != nil {
		return m, cfg, err
	}
	return m, cfg, nil
}

func validateOpenCodeCompatibilityReceipt(req RuntimeAgentWorkspaceRequest, result *RuntimeAgentUpgradeCompatibility) error {
	if err := validateOpenCodeCompatibility(req.InstanceID, result); err != nil {
		return err
	}
	if result.RolloutID != req.RolloutID || result.Generation != req.Generation || result.ConfigOriginalSHA256 != result.ConfigTargetSHA256 || result.ConfigBytes < 0 || result.SessionBytes < 1 || result.StateBytes < result.SessionBytes || result.AvailableBytes == 0 || result.CheckedAt.IsZero() {
		return fmt.Errorf("OpenCode compatibility receipt identity/config/native-size proof mismatch")
	}
	return nil
}
func validateOpenCodeCapsuleReceipt(req RuntimeAgentWorkspaceRequest, result *RuntimeAgentSessionSQLiteMigration, compat *RuntimeAgentUpgradeCompatibility) error {
	if err := validateOpenCodeCapsule(req.InstanceID, result); err != nil {
		return err
	}
	if result.RolloutID != req.RolloutID || result.Generation != req.Generation || result.ArchiveBytes < 1 || result.ArchiveFiles < 1 || result.SessionCount < 0 || result.CompletedAt.IsZero() || !validOpenCodeSHA(result.ConfigOriginalSHA256) || !validOpenCodeSHA(result.ConfigTargetSHA256) || result.ConfigOriginalSHA256 != result.ConfigTargetSHA256 {
		return fmt.Errorf("OpenCode capsule receipt identity/config/archive proof mismatch")
	}
	if compat == nil || result.ConfigOriginalSHA256 != compat.ConfigOriginalSHA256 || result.ConfigTargetSHA256 != compat.ConfigTargetSHA256 {
		return fmt.Errorf("OpenCode capsule changed since validated compatibility")
	}
	return nil
}
func sameOpenCodeCapsuleProof(a, b *RuntimeAgentSessionSQLiteMigration) bool {
	return a != nil && b != nil && a.RolloutID == b.RolloutID && a.InstanceID == b.InstanceID && a.Generation == b.Generation && a.OutputSHA256 == b.OutputSHA256 && a.PreservationSHA256 == b.PreservationSHA256 && a.SessionCatalogSHA256 == b.SessionCatalogSHA256 && a.SessionCount == b.SessionCount && a.ArchiveBytes == b.ArchiveBytes && a.ArchiveFiles == b.ArchiveFiles && a.StateCapsuleBytes == b.StateCapsuleBytes && a.ConfigOriginalSHA256 == b.ConfigOriginalSHA256 && a.ConfigTargetSHA256 == b.ConfigTargetSHA256 && a.DataPreserved == b.DataPreserved && a.RollbackAvailable == b.RollbackAvailable
}
func validateOpenCodeRestoreReceipt(req RuntimeAgentWorkspaceRequest, result *RuntimeAgentSessionSQLiteRestore) error {
	if err := validateOpenCodeRestore(req.InstanceID, result); err != nil {
		return err
	}
	if result.RolloutID != req.RolloutID || result.Generation != req.Generation || result.CompletedAt.IsZero() {
		return fmt.Errorf("OpenCode restore receipt rollout/generation identity mismatch")
	}
	return nil
}
