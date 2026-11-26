package systembackuptrial

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	batch "k8s.io/api/batch/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	batchclient "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/rest"
)

func trialEnvironmentFixture(t *testing.T) (*Service, trialEnvironment, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "environment.json")
	source := trialPlacement{
		Cluster: "fixture", Namespace: "cm-drill-source", Image: "test.registry/clawmanager@sha256:" + strings.Repeat("a", 64),
		ServiceAccount: "source-api", ArtifactClaim: "export", KeySecret: "source-key", SourceReadSecret: "source-reader",
	}
	target := source
	target.Namespace, target.ServiceAccount, target.ArtifactClaim = "cm-drill-target", "target-api", "import"
	target.KeySecret, target.SourceReadSecret = "target-key", ""
	target.RestoreReadSecret, target.RestoreWriteSecret = "target-reader", "target-writer"
	target.InstanceStorageClass, target.WorkspaceClaim = "trial", "restored-workspaces"
	profile := trialEnvironment{
		Revision: "fixture-v1", Side: "target", Source: source, Target: target,
		Job: &trialJob{Name: "prepare-trial-v1", UID: "11111111-2222-4333-8444-555555555555"},
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"SYSTEM_BACKUP_TRIAL_ENVIRONMENT_PROFILE_FILE": file,
		"SYSTEM_BACKUP_TRIAL_RESTORE_NAMESPACE":        target.Namespace,
		"SYSTEM_BACKUP_TRIAL_RESTORE_STORAGE_CLASS":    target.InstanceStorageClass,
	}
	return New(func(key string) string { return env[key] }), profile, file
}

func TestTrialEnvironmentRejectsInvalidRegistration(t *testing.T) {
	service, profile, file := trialEnvironmentFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*trialEnvironment) []byte
	}{
		{"unknown credential value", func(p *trialEnvironment) []byte {
			raw, _ := json.Marshal(p)
			return append(raw[:len(raw)-1], []byte(`,"credential_value":"fixture-only"}`)...)
		}},
		{"same namespace", func(p *trialEnvironment) []byte { p.Source.Namespace = p.Target.Namespace; return nil }},
		{"target outside isolation", func(p *trialEnvironment) []byte { p.Target.Namespace = "default"; p.Side = "source"; return nil }},
		{"target namespace differs from restore", func(p *trialEnvironment) []byte { p.Target.Namespace = "cm-drill-other"; return nil }},
		{"shared database credentials", func(p *trialEnvironment) []byte { p.Target.RestoreReadSecret = p.Target.RestoreWriteSecret; return nil }},
		{"image not pinned", func(p *trialEnvironment) []byte { p.Target.Image = "test.registry/clawmanager:latest"; return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := profile
			data := tc.change(&p)
			if data == nil {
				data, _ = json.Marshal(p)
			}
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			state := service.environment(context.Background(), false)
			if state.State != "invalid" || state.Profile != nil || state.missing("target") == "" {
				t.Fatalf("invalid registration was exposed or allowed: %+v", state)
			}
		})
	}
}

func TestTrialAdminStartsOnlyTheRegisteredJob(t *testing.T) {
	service, profile, _ := trialEnvironmentFixture(t)
	var mu sync.Mutex
	patches := 0
	suspended := true
	job := batch.Job{
		TypeMeta: meta.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: meta.ObjectMeta{
			Name: profile.Job.Name, Namespace: profile.Target.Namespace, UID: profile.Job.UID, ResourceVersion: "100",
			Annotations: map[string]string{trialEnvironmentRevision: profile.Revision},
		},
		Spec: batch.JobSpec{Suspend: &suspended},
	}
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		wantPath := "/apis/batch/v1/namespaces/" + job.Namespace + "/jobs/" + job.Name
		if req.URL.Path != wantPath {
			t.Errorf("unexpected Kubernetes resource %q", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Method == http.MethodPatch {
			patches++
			raw, err := io.ReadAll(req.Body)
			want := fmt.Sprintf(`[{"op":"test","path":"/metadata/uid","value":"%s"},{"op":"test","path":"/metadata/resourceVersion","value":"%s"},{"op":"test","path":"/spec/suspend","value":true},{"op":"replace","path":"/spec/suspend","value":false}]`, job.UID, job.ResourceVersion)
			if err != nil || req.Header.Get("Content-Type") != "application/json-patch+json" || string(raw) != want {
				t.Errorf("unexpected Job patch %s: %v", raw, err)
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			suspended = false
			job.ResourceVersion = "101"
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if req.Method != http.MethodGet {
			t.Errorf("unexpected Kubernetes method %s", req.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(job)
	}))
	defer kube.Close()
	client, err := batchclient.NewForConfig(&rest.Config{Host: kube.URL})
	if err != nil {
		t.Fatal(err)
	}
	service.environmentJobs = func(namespace string) (environmentJobs, error) { return client.Jobs(namespace), nil }
	const secret = "trial-environment-fixture-jwt"
	t.Setenv("JWT_SECRET", secret)
	router := gin.New()
	service.Register(router.Group("/api/v1"), trialTestUsers{})
	request := func(method, path string, user int, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/admin/system-backup/trial"+path, strings.NewReader(body))
		if user != 0 {
			token, err := utils.GenerateToken(utils.TokenClaims{UserID: user, TokenType: "access"}, secret, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	for user, want := range map[int]int{0: http.StatusUnauthorized, 2: http.StatusForbidden, 3: http.StatusForbidden} {
		if rec := request(http.MethodPost, "/environment/prepare", user, ""); rec.Code != want {
			t.Fatalf("user %d got %d, want %d", user, rec.Code, want)
		}
	}
	if rec := request(http.MethodPost, "/environment/prepare", 1, `{"name":"another-job"}`); rec.Code != http.StatusBadRequest {
		t.Fatal("request was allowed to specify a preparation job")
	}
	ready := request(http.MethodGet, "/readiness", 1, "")
	if ready.Code != http.StatusOK || ready.Header().Get("Cache-Control") != "no-store" || !strings.Contains(ready.Body.String(), `"state":"waiting"`) {
		t.Fatalf("unexpected environment readiness: %d %s", ready.Code, ready.Body.String())
	}
	if rec := request(http.MethodPost, "/backups/"+strings.Repeat("a", 32)+"/restore", 1, ""); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "trial_environment_preparation") {
		t.Fatal("restore was not gated while the registered job was waiting")
	}
	for range 2 {
		if rec := request(http.MethodPost, "/environment/prepare", 1, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"running"`) {
			t.Fatalf("could not observe the already-started job: %d %s", rec.Code, rec.Body.String())
		}
	}
	mu.Lock()
	job.Status.Conditions = []batch.JobCondition{{Type: batch.JobComplete, Status: "True"}}
	mu.Unlock()
	if done := service.environment(context.Background(), false); done.State != "prepared" || done.missing("target") != "" || done.missing("source") == "" {
		t.Fatalf("unexpected completed target: %+v", done)
	}
	mu.Lock()
	job.UID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	job.Status.Conditions = nil
	suspended = true
	mu.Unlock()
	if rec := request(http.MethodPost, "/environment/prepare", 1, ""); !strings.Contains(rec.Body.String(), `"state":"unavailable"`) {
		t.Fatal("a job recreated under the same name was accepted")
	}
	mu.Lock()
	defer mu.Unlock()
	if patches != 1 || !suspended {
		t.Fatal("duplicate request or replacement modified a Job")
	}
}
