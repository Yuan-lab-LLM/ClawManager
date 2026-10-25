package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ResolveHermesRolloutImage pins the manifest before changing any deployment.
// Both old and desktop-web Hermes accept digests; the latter requires one.
func ResolveHermesRolloutImage(ctx context.Context, target string) (string, string, error) {
	host, repository, reference, err := splitRegistryImage(target)
	if err != nil {
		return "", "", err
	}
	if digest := imageDigestFromReference(target); digest != "" {
		return host + "/" + repository + "@" + digest, digest, nil
	}
	if reference == "" {
		return "", "", fmt.Errorf("Hermes image tag or digest is required")
	}
	client, baseURL, err := safeRegistryClient(host, repository)
	if err != nil {
		return "", "", err
	}
	_, digest, err := fetchRegistryManifest(ctx, client, baseURL, reference)
	if err != nil {
		return "", "", fmt.Errorf("resolve Hermes image: %w", err)
	}
	if imageDigestFromReference("image@"+digest) == "" {
		return "", "", fmt.Errorf("Hermes registry returned an invalid manifest digest")
	}
	return host + "/" + repository + "@" + digest, digest, nil
}

// HermesImageNeedsWebTrust reads the pinned image's actual configuration, never
// guesses a runtime generation from a mutable tag or repository name.
func HermesImageNeedsWebTrust(ctx context.Context, image string) (bool, error) {
	host, repository, _, err := splitRegistryImage(image)
	if err != nil {
		return false, err
	}
	client, base, err := safeRegistryClient(host, repository)
	if err != nil {
		return false, err
	}
	digest := imageDigestFromReference(image)
	if digest == "" {
		return false, fmt.Errorf("Hermes image inspection requires a pinned digest")
	}
	manifest, _, err := fetchRegistryManifest(ctx, client, base, digest)
	if err != nil {
		return false, err
	}
	if len(manifest.Manifests) > 0 {
		found := false
		for _, item := range manifest.Manifests {
			if item.Platform != nil && item.Platform.OS == "linux" && item.Platform.Architecture == "amd64" {
				manifest, _, err = fetchRegistryManifest(ctx, client, base, item.Digest)
				if err != nil {
					return false, err
				}
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Errorf("Hermes image has no supported linux/amd64 manifest")
		}
	}
	if imageDigestFromReference("image@"+manifest.Config.Digest) == "" {
		return false, fmt.Errorf("Hermes image is missing its config digest")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/blobs/"+url.PathEscape(manifest.Config.Digest), nil)
	if err != nil {
		return false, err
	}
	response, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Hermes config returned HTTP %d", response.StatusCode)
	}
	var config registryImageConfig
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&config); err != nil {
		return false, err
	}
	for _, env := range config.Config.Env {
		if strings.TrimSpace(env) == "CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED=true" {
			return true, nil
		}
	}
	return false, nil
}
