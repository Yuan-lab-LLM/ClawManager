package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFaultRecoveryAgentOverlayOnlyAcceptsUnchangedNativeImage(t *testing.T) {
	for _, scenario := range []string{"agent", "native-file", "whiteout", "different-base", "changed-entrypoint", "wrong-digest"} {
		t.Run(scenario, func(t *testing.T) {
			var layer bytes.Buffer
			zipped := gzip.NewWriter(&layer)
			archive := tar.NewWriter(zipped)
			files := []string{"usr/local/bin/clawmanager-agent", "usr/local/libexec/clawmanager-agent-image"}
			if scenario == "native-file" {
				files = append(files, "opt/hermes-agent/gateway/run.py")
			} else if scenario == "whiteout" {
				files = append(files, "usr/local/bin/.wh.clawmanager-agent")
			}
			for _, name := range files {
				if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: 1}); err != nil {
					t.Fatal(err)
				}
				if _, err := archive.Write([]byte("a")); err != nil {
					t.Fatal(err)
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := zipped.Close(); err != nil {
				t.Fatal(err)
			}
			const repository = "/v2/runtime/hermes-lite"
			contents := map[string][]byte{}
			add := func(path string, data []byte) string {
				digest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
				contents[repository+path+digest] = data
				return digest
			}
			manifest := func(config string, files ...recoveryImageDescriptor) string {
				data, err := json.Marshal(recoveryImageManifest{Config: recoveryImageDescriptor{Digest: config}, Layers: files})
				if err != nil {
					t.Fatal(err)
				}
				return add("/manifests/", data)
			}
			const media = "application/vnd.docker.image.rootfs.diff.tar.gzip"
			base := recoveryImageDescriptor{Digest: add("/blobs/", []byte("business environment")), MediaType: media}
			beforeConfig := add("/blobs/", []byte(`{"os":"linux","architecture":"amd64","config":{"Entrypoint":["native"]}}`))
			before := manifest(beforeConfig, base)
			afterConfig := beforeConfig
			if scenario == "changed-entrypoint" {
				afterConfig = add("/blobs/", []byte(`{"os":"linux","architecture":"amd64","config":{"Entrypoint":["other"]}}`))
			}
			if scenario == "different-base" {
				base.Digest = add("/blobs/", []byte("different business environment"))
			}
			added := add("/blobs/", layer.Bytes())
			if scenario == "wrong-digest" {
				contents[repository+"/blobs/"+added] = []byte("changed after publication")
			}
			after := manifest(afterConfig, base, recoveryImageDescriptor{Digest: added, MediaType: media})
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if data, ok := contents[r.URL.Path]; ok {
					_, _ = w.Write(data)
				} else {
					http.NotFound(w, r)
				}
			}))
			defer registry.Close()
			prefix := strings.TrimPrefix(registry.URL, "http://") + "/runtime/hermes-lite@"
			err := verifyFaultRecoveryAgentOverlay(t.Context(), RuntimeTypeHermes, prefix+before, prefix+after)
			if (err == nil) != (scenario == "agent") {
				t.Fatalf("unexpected compatibility result: %v", err)
			}
		})
	}
}
