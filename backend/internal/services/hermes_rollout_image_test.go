package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestHermesRolloutImagePinsTag(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/hermes-lite/manifests/release" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Docker-Content-Digest", digest)
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	}))
	defer server.Close()
	base := strings.TrimPrefix(server.URL, "http://") + "/hermes-lite"
	ref, got, err := ResolveHermesRolloutImage(context.Background(), base+":release")
	if err != nil || got != digest || ref != base+"@"+digest {
		t.Fatalf("%s %s %v", ref, got, err)
	}
	ref2, got2, err := ResolveHermesRolloutImage(context.Background(), ref)
	if err != nil || ref2 != ref || got2 != got {
		t.Fatalf("digest changed: %s %s %v", ref2, got2, err)
	}
}

func TestHermesRolloutImageRegistryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer server.Close()
	if _, _, err := ResolveHermesRolloutImage(context.Background(), strings.TrimPrefix(server.URL, "http://")+"/hermes:missing"); err == nil {
		t.Fatal("missing image accepted")
	}
}

func TestHermesWebTrustDetectionUsesImageConfig(t *testing.T) {
	for _, web := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/manifests/") {
				_, _ = w.Write([]byte(`{"schemaVersion":2,"config":{"digest":"sha256:` + strings.Repeat("b", 64) + `"}}`))
				return
			}
			if web {
				_, _ = w.Write([]byte(`{"config":{"Env":["CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED=true"]}}`))
			} else {
				_, _ = w.Write([]byte(`{"config":{"Env":[]}}`))
			}
		}))
		got, err := HermesImageNeedsWebTrust(context.Background(), strings.TrimPrefix(server.URL, "http://")+"/hermes@sha256:"+strings.Repeat("a", 64))
		server.Close()
		if err != nil || got != web {
			t.Fatalf("web=%t got=%t err=%v", web, got, err)
		}
	}
}

func TestLegacyRolloutMissingAgentHasDeadline(t *testing.T) {
	for _, runtimeType := range []string{RuntimeTypeHermes, RuntimeTypeOpenClaw, "opencode", "deepseek"} {
		t.Run(runtimeType, func(t *testing.T) {
			repo := &fakeRuntimeRolloutRepo{rollouts: map[int64]*models.RuntimeRollout{}}
			s := &RuntimeScheduler{rolloutRepo: repo, podRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{}}}
			start := time.Now().Add(-16 * time.Minute)
			r := models.RuntimeRollout{ID: 1, RuntimeType: runtimeType, TargetImageRef: "registry/image:tag", Status: "running", StartedAt: &start}
			repo.rollouts[1] = &r
			if err := s.finishRolloutIfReady(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if len(repo.statuses) != 1 || repo.statuses[0].status != "error" {
				t.Fatalf("not terminated: %+v", repo.statuses)
			}
		})
	}
}
