package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"clawreef/internal/models"
)

type recoveryImageDescriptor struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
}

type recoveryImageManifest struct {
	Config recoveryImageDescriptor   `json:"config"`
	Layers []recoveryImageDescriptor `json:"layers"`
}

type recoveryImageConfig struct {
	OS           string                     `json:"os"`
	Architecture string                     `json:"architecture"`
	Config       map[string]json.RawMessage `json:"config"`
}

// An exact image pin remains mandatory for migration. Only a verified recovery
// of a stopped Team member can use a management-Agent-only successor on the
// same pool, with identical business Runtime content and launch configuration.
func (s *RuntimeManagementService) compatibleFaultRecoveryAgentOverlay(ctx context.Context, source, target models.RuntimePod) bool {
	if source.RuntimeType != RuntimeTypeHermes && source.RuntimeType != RuntimeTypeDeepSeekHarness {
		return false
	}
	if source.DeploymentName == "" || source.DeploymentName != target.DeploymentName || source.ImageRef == target.ImageRef ||
		imageDigestFromReference(target.ImageRef) != stringValue(target.ImageDigest) || imageDigestFromReference(source.ImageRef) != stringValue(source.ImageDigest) {
		return false
	}
	// Preserve every ordinary target constraint after proving equivalent image
	// content. The live target's immutable digest is also checked by slot admission.
	compatibleSource := source
	compatibleSource.ImageRef, compatibleSource.ImageDigest = target.ImageRef, target.ImageDigest
	if validateRuntimeManagementTarget(compatibleSource, target) != nil {
		return false
	}
	key := [3]string{source.RuntimeType, source.ImageRef, target.ImageRef}
	if _, verified := s.verifiedFaultRecoveryOverlays.Load(key); verified {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := verifyFaultRecoveryAgentOverlay(ctx, source.RuntimeType, source.ImageRef, target.ImageRef); err != nil {
		return false
	}
	s.verifiedFaultRecoveryOverlays.Store(key, true)
	return true
}

func verifyFaultRecoveryAgentOverlay(ctx context.Context, kind, source, target string) error {
	sourceHost, repository, _, err := splitRegistryImage(source)
	if err != nil {
		return err
	}
	targetHost, targetRepository, _, err := splitRegistryImage(target)
	if err != nil || targetHost != sourceHost || targetRepository != repository {
		return fmt.Errorf("recovery image repository changed")
	}
	client, base, err := safeRegistryClient(sourceHost, repository)
	if err != nil {
		return err
	}
	read := func(path, digest string, limit int64) ([]byte, error) {
		if imageDigestFromReference("image@"+digest) == "" {
			return nil, fmt.Errorf("invalid recovery image descriptor")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+url.PathEscape(digest), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("recovery image registry returned status %d", response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
		sum := sha256.Sum256(data)
		if err != nil || int64(len(data)) > limit || "sha256:"+hex.EncodeToString(sum[:]) != digest {
			return nil, fmt.Errorf("recovery image content identity is invalid")
		}
		return data, nil
	}
	var before, after recoveryImageManifest
	for reference, destination := range map[string]*recoveryImageManifest{source: &before, target: &after} {
		data, err := read("/manifests/", imageDigestFromReference(reference), 1<<20)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, destination); err != nil {
			return err
		}
	}
	n := len(before.Layers)
	if n == 0 || len(after.Layers) != n+1 || !reflect.DeepEqual(before.Layers, after.Layers[:n]) {
		return fmt.Errorf("recovery image base layers differ")
	}
	var beforeConfig, afterConfig recoveryImageConfig
	for i, descriptor := range []recoveryImageDescriptor{before.Config, after.Config} {
		data, err := read("/blobs/", descriptor.Digest, 2<<20)
		if err != nil {
			return err
		}
		destination := []*recoveryImageConfig{&beforeConfig, &afterConfig}[i]
		if err := json.Unmarshal(data, destination); err != nil {
			return err
		}
	}
	if beforeConfig.OS != "linux" || beforeConfig.Architecture == "" || beforeConfig.Config == nil || !reflect.DeepEqual(beforeConfig, afterConfig) {
		return fmt.Errorf("recovery image launch settings changed")
	}
	added := after.Layers[n]
	if added.MediaType != "application/vnd.oci.image.layer.v1.tar+gzip" && added.MediaType != "application/vnd.docker.image.rootfs.diff.tar.gzip" {
		return fmt.Errorf("unsupported recovery image overlay")
	}
	layer, err := read("/blobs/", added.Digest, 16<<20)
	if err != nil {
		return err
	}
	return checkRecoveryAgentOverlayLayer(kind, layer)
}

func checkRecoveryAgentOverlayLayer(kind string, data []byte) error {
	zipped, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer zipped.Close()
	uncompressed := &io.LimitedReader{R: zipped, N: 64 << 20}
	archive := tar.NewReader(uncompressed)
	dirs := map[string]bool{"usr": true, "usr/local": true, "usr/local/bin": true, "usr/local/libexec": true}
	executables := map[string]bool{"usr/local/bin/clawmanager-agent": false, "usr/local/libexec/clawmanager-agent-image": false}
	const release = "usr/local/share/hermes-lite/release.json"
	if kind == RuntimeTypeHermes {
		dirs["usr/local/share"], dirs["usr/local/share/hermes-lite"] = true, true
	}
	seen := map[string]bool{}
	for {
		h, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if seen[name] || h.Uid != 0 || h.Gid != 0 || h.Linkname != "" || len(h.Xattrs) != 0 ||
			h.Mode&^0777 != 0 || h.Size < 0 {
			return fmt.Errorf("invalid recovery image overlay entry")
		}
		for key := range h.PAXRecords {
			if key != "mtime" && key != "atime" && key != "ctime" {
				return fmt.Errorf("unsupported recovery image overlay metadata")
			}
		}
		seen[name] = true
		_, executable := executables[name]
		switch {
		case h.Typeflag == tar.TypeDir && dirs[name] && h.Mode == 0755 && h.Size == 0:
		case h.Typeflag == tar.TypeReg && executable && h.Mode == 0755 && h.Size > 0:
			executables[name] = true
		case h.Typeflag == tar.TypeReg && kind == RuntimeTypeHermes && name == release && h.Mode == 0444 && h.Size > 0 && h.Size <= 4<<20:
		default:
			return fmt.Errorf("recovery image overlay changes business Runtime content")
		}
	}
	padding, err := io.ReadAll(io.LimitReader(uncompressed, 4097))
	if err != nil || uncompressed.N == 0 || len(padding) > 4096 || bytes.Count(padding, []byte{0}) != len(padding) {
		return fmt.Errorf("invalid recovery image overlay padding")
	}
	for _, present := range executables {
		if !present {
			return fmt.Errorf("recovery image overlay does not contain the management Agent")
		}
	}
	return nil
}
